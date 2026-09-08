package app

import (
	"context"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// This is deliberately a tiny Wayland client, not an input injector. Only
// get_registry, bind, sync, create-device and keymap requests are implemented.
// The wire protocol is native endian, 32-bit aligned, with SCM_RIGHTS for the
// keymap fd. No bound object has events containing file descriptors.
type waylandDevices struct {
	conn    *net.UnixConn
	done    chan error
	stop    func() bool
	globals map[string]uint32
}

func dialWayland(ctx context.Context) (*net.UnixConn, error) {
	if inherited := os.Getenv("WAYLAND_SOCKET"); inherited != "" {
		fd, err := strconv.Atoi(inherited)
		if err != nil || fd < 0 {
			return nil, errors.New("invalid WAYLAND_SOCKET")
		}
		f := os.NewFile(uintptr(fd), "WAYLAND_SOCKET")
		if f == nil {
			return nil, errors.New("invalid WAYLAND_SOCKET fd")
		}
		c, err := net.FileConn(f)
		_ = f.Close()
		if err != nil {
			return nil, err
		}
		u, ok := c.(*net.UnixConn)
		if !ok || u.LocalAddr().Network() != "unix" {
			c.Close()
			return nil, errors.New("WAYLAND_SOCKET must be a Unix stream socket")
		}
		// The inherited fd has been consumed; don't pass a stale descriptor
		// number to a subsequently launched console.
		_ = os.Unsetenv("WAYLAND_SOCKET")
		return u, nil
	}
	display := os.Getenv("WAYLAND_DISPLAY")
	if display == "" {
		display = "wayland-0"
	}
	if !filepath.IsAbs(display) {
		runtime := os.Getenv("XDG_RUNTIME_DIR")
		if runtime == "" {
			return nil, errors.New("XDG_RUNTIME_DIR is required for a relative WAYLAND_DISPLAY")
		}
		display = filepath.Join(runtime, display)
	}
	c, err := (&net.Dialer{}).DialContext(ctx, "unix", display)
	if err != nil {
		return nil, fmt.Errorf("connect to Wayland %q: %w", display, err)
	}
	return c.(*net.UnixConn), nil
}

func startWaylandDevices(ctx context.Context) (*waylandDevices, error) {
	conn, err := dialWayland(ctx)
	if err != nil {
		return nil, err
	}
	return initWaylandDevices(ctx, conn)
}
func initWaylandDevices(ctx context.Context, conn *net.UnixConn) (*waylandDevices, error) {
	d := &waylandDevices{conn: conn, done: make(chan error, 1), globals: map[string]uint32{}}
	d.stop = context.AfterFunc(ctx, func() { _ = conn.Close() })
	success := false
	defer func() {
		if !success {
			d.Close()
		}
	}()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if err := d.request(1, 1, wlWords(2), nil); err != nil {
		return nil, err
	} // display.get_registry
	if err := d.sync(3); err != nil {
		return nil, err
	}
	for i, name := range []string{"wl_seat", "zwp_virtual_keyboard_manager_v1", "zwlr_virtual_pointer_manager_v1"} {
		global, ok := d.globals[name]
		if !ok {
			return nil, fmt.Errorf("required Wayland interface %s unavailable; no global input fallback", name)
		}
		args := append(wlWords(global), wlString(name)...)
		args = append(args, wlWords(1, uint32(4+i))...)
		if err := d.request(2, 0, args, nil); err != nil {
			return nil, err
		}
	}
	if err := d.request(6, 0, wlWords(4, 7), nil); err != nil {
		return nil, err
	} // create_virtual_pointer
	if err := d.request(5, 0, wlWords(4, 8), nil); err != nil {
		return nil, err
	} // create_virtual_keyboard
	if err := d.sendKeymap(); err != nil {
		return nil, err
	}
	if err := d.sync(9); err != nil {
		return nil, err
	} // ready only after compositor processed the keymap
	_ = conn.SetDeadline(time.Time{})
	success = true
	go func() {
		defer d.Close()
		for {
			if _, err := d.event(); err != nil {
				d.done <- err
				return
			}
		}
	}()
	return d, nil
}
func (d *waylandDevices) Close() { d.stop(); _ = d.conn.Close() }
func (d *waylandDevices) sendKeymap() error {
	fd, err := unix.MemfdCreate("computer-use-keymap", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(fd), "computer-use-keymap")
	defer f.Close()
	keymap := []byte(keyboardKeymap())
	if n, err := f.Write(keymap); err != nil {
		return err
	} else if n != len(keymap) {
		return io.ErrShortWrite
	}
	if _, err := unix.FcntlInt(f.Fd(), unix.F_ADD_SEALS, unix.F_SEAL_SHRINK|unix.F_SEAL_GROW|unix.F_SEAL_WRITE|unix.F_SEAL_SEAL); err != nil {
		return err
	}
	return d.request(8, 0, wlWords(1, uint32(len(keymap))), unix.UnixRights(fd))
}
func wlWords(values ...uint32) []byte {
	b := make([]byte, len(values)*4)
	for i, v := range values {
		binary.NativeEndian.PutUint32(b[i*4:], v)
	}
	return b
}
func wlString(s string) []byte {
	b := make([]byte, 4+(len(s)+1+3)&^3)
	binary.NativeEndian.PutUint32(b, uint32(len(s)+1))
	copy(b[4:], s)
	return b
}
func (d *waylandDevices) request(id uint32, opcode uint16, args, oob []byte) error {
	if len(args)%4 != 0 || len(args) > 65524 {
		return errors.New("invalid Wayland request size")
	}
	message := append(wlWords(id, uint32(len(args)+8)<<16|uint32(opcode)), args...)
	n, _, err := d.conn.WriteMsgUnix(message, oob, nil)
	if err != nil {
		return err
	}
	if n != len(message) {
		return io.ErrShortWrite
	}
	return nil
}
func readWaylandMessage(r io.Reader) (uint32, uint16, []byte, error) {
	header := make([]byte, 8)
	if _, err := io.ReadFull(r, header); err != nil {
		return 0, 0, nil, err
	}
	id, word := binary.NativeEndian.Uint32(header), binary.NativeEndian.Uint32(header[4:])
	size := int(word >> 16)
	if id == 0 || size < 8 || size%4 != 0 {
		return 0, 0, nil, errors.New("invalid Wayland event header")
	}
	args := make([]byte, size-8)
	_, err := io.ReadFull(r, args)
	return id, uint16(word), args, err
}
func takeWLString(args []byte) (string, []byte, error) {
	if len(args) < 4 {
		return "", nil, io.ErrUnexpectedEOF
	}
	n := uint64(binary.NativeEndian.Uint32(args))
	padded := (n + 3) &^ 3
	if n == 0 || padded > uint64(len(args)-4) || args[4+n-1] != 0 {
		return "", nil, errors.New("invalid Wayland string")
	}
	return string(args[4 : 4+n-1]), args[4+padded:], nil
}
func (d *waylandDevices) sync(callback uint32) error {
	if err := d.request(1, 0, wlWords(callback), nil); err != nil {
		return err
	}
	for {
		done, err := d.event()
		if err != nil {
			return err
		}
		if done == callback {
			return nil
		}
	}
}
func (d *waylandDevices) event() (uint32, error) {
	id, op, args, err := readWaylandMessage(d.conn)
	if err != nil {
		return 0, err
	}
	word := func() uint32 { return binary.NativeEndian.Uint32(args) }
	switch {
	case id == 1 && op == 0: // wl_display.error
		if len(args) < 12 {
			break
		}
		message, rest, err := takeWLString(args[8:])
		if err != nil || len(rest) != 0 {
			break
		}
		return 0, fmt.Errorf("Wayland protocol error on object %d (code %d): %s", word(), binary.NativeEndian.Uint32(args[4:]), message)
	case id == 1 && op == 1 && len(args) == 4: // delete_id: our two one-shot callbacks only
		if word() == 3 || word() == 9 {
			return 0, nil
		}
	case id == 2 && op == 0: // registry.global
		if len(args) < 12 {
			break
		}
		name, rest, err := takeWLString(args[4:])
		if err != nil || len(rest) != 4 {
			break
		}
		if name == "wl_seat" || name == "zwp_virtual_keyboard_manager_v1" || name == "zwlr_virtual_pointer_manager_v1" {
			if binary.NativeEndian.Uint32(rest) < 1 {
				return 0, fmt.Errorf("unsupported %s version", name)
			}
			if _, ok := d.globals[name]; ok {
				return 0, fmt.Errorf("multiple %s globals are unsupported", name)
			}
			d.globals[name] = word()
		}
		return 0, nil
	case id == 2 && op == 1 && len(args) == 4: // registry.global_remove
		for name, global := range d.globals {
			if word() == global {
				return 0, fmt.Errorf("required Wayland global removed: %s", name)
			}
		}
		return 0, nil
	case id == 4 && op == 0 && len(args) == 4: // wl_seat v1 capabilities
		return 0, nil
	case (id == 3 || id == 9) && op == 0 && len(args) == 4:
		return id, nil
	}
	return 0, fmt.Errorf("unexpected or malformed Wayland event %d/%d", id, op)
}

func runKeyboard(args []string) error {
	f := flag.NewFlagSet("keyboard", flag.ContinueOnError)
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() != 0 {
		return errors.New("keyboard accepts no arguments; it does not inject input")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	d, err := startWaylandDevices(ctx)
	if err != nil {
		return fmt.Errorf("keyboard/pointer initialization failed: %w", err)
	}
	defer d.Close()
	fmt.Fprintln(os.Stdout, "ready")
	select {
	case <-ctx.Done():
		return nil
	case err := <-d.done:
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("keyboard/pointer connection lost: %w", err)
	}
}
