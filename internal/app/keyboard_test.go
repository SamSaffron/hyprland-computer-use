package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func waylandPair(t *testing.T) (*net.UnixConn, *net.UnixConn) {
	t.Helper()
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	conns := make([]*net.UnixConn, 2)
	for i, fd := range fds {
		f := os.NewFile(uintptr(fd), "test-wayland")
		c, err := net.FileConn(f)
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
		conns[i] = c.(*net.UnixConn)
		t.Cleanup(func() { c.Close() })
		c.SetDeadline(time.Now().Add(10 * time.Second))
	}
	return conns[0], conns[1]
}
func expectWL(c *net.UnixConn, id uint32, op uint16, want []byte) error {
	gotID, gotOp, args, err := readWaylandMessage(c)
	if err != nil {
		return err
	}
	if gotID != id || gotOp != op || !bytes.Equal(args, want) {
		return fmt.Errorf("got request %d/%d %v; want %d/%d %v", gotID, gotOp, args, id, op, want)
	}
	return nil
}
func fakeWaylandInit(c *net.UnixConn, missing bool) error {
	peer := &waylandDevices{conn: c}
	if err := expectWL(c, 1, 1, wlWords(2)); err != nil {
		return err
	}
	if err := expectWL(c, 1, 0, wlWords(3)); err != nil {
		return err
	}
	names := []string{"wl_seat", "zwp_virtual_keyboard_manager_v1", "zwlr_virtual_pointer_manager_v1"}
	for i, name := range names {
		if missing && i == 2 {
			continue
		}
		args := append(wlWords(uint32(10+i)), wlString(name)...)
		args = append(args, wlWords(2)...)
		if err := peer.request(2, 0, args, nil); err != nil {
			return err
		}
	}
	if err := peer.request(3, 0, wlWords(1), nil); err != nil {
		return err
	}
	if missing {
		return nil
	}
	if err := peer.request(1, 1, wlWords(3), nil); err != nil {
		return err
	}
	for i, name := range names {
		args := append(wlWords(uint32(10+i)), wlString(name)...)
		args = append(args, wlWords(1, uint32(4+i))...)
		if err := expectWL(c, 2, 0, args); err != nil {
			return err
		}
	}
	if err := expectWL(c, 6, 0, wlWords(4, 7)); err != nil {
		return err
	}
	if err := expectWL(c, 5, 0, wlWords(4, 8)); err != nil {
		return err
	}
	// SCM_RIGHTS travels with the keymap request, without an fd word in its body.
	header := make([]byte, 8)
	oob := make([]byte, unix.CmsgSpace(4))
	n, oobn, flags, _, err := c.ReadMsgUnix(header, oob)
	if err != nil {
		return err
	}
	if flags&unix.MSG_CTRUNC != 0 {
		return errors.New("truncated keymap fd")
	}
	if n < 8 {
		if _, err := io.ReadFull(c, header[n:]); err != nil {
			return err
		}
	}
	messages, err := unix.ParseSocketControlMessage(oob[:oobn])
	if err != nil {
		return err
	}
	var rights []int
	for _, m := range messages {
		fds, e := unix.ParseUnixRights(&m)
		if e != nil {
			return e
		}
		rights = append(rights, fds...)
	}
	defer func() {
		for _, fd := range rights {
			unix.Close(fd)
		}
	}()
	if len(rights) != 1 {
		return fmt.Errorf("expected one keymap fd, got %v", rights)
	}
	if !bytes.Equal(header, wlWords(8, 16<<16)) {
		return fmt.Errorf("bad keymap header: %v", header)
	}
	args := make([]byte, 8)
	if _, err := io.ReadFull(c, args); err != nil {
		return err
	}
	if !bytes.Equal(args, wlWords(1, uint32(len(keyboardKeymap())))) {
		return errors.New("bad keymap format/size")
	}
	keymap := make([]byte, len(keyboardKeymap()))
	n, err = unix.Pread(rights[0], keymap, 0)
	if err != nil {
		return err
	}
	if n != len(keymap) || string(keymap) != keyboardKeymap() {
		return errors.New("incorrect keymap fd contents")
	}
	seals, err := unix.FcntlInt(uintptr(rights[0]), unix.F_GET_SEALS, 0)
	if err != nil || seals&unix.F_SEAL_WRITE == 0 {
		return errors.New("keymap is not sealed")
	}
	if err := expectWL(c, 1, 0, wlWords(9)); err != nil {
		return err
	}
	if err := peer.request(4, 0, wlWords(3), nil); err != nil {
		return err
	}
	return peer.request(9, 0, wlWords(2), nil)
}
func TestWaylandDevicesLifecycle(t *testing.T) {
	client, server := waylandPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fakeDone := make(chan error, 1)
	go func() { fakeDone <- fakeWaylandInit(server, false) }()
	d, err := initWaylandDevices(ctx, client)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := <-fakeDone; err != nil {
		t.Fatal(err)
	}
	// Once ready, the client must send no input requests at all.
	server.SetReadDeadline(time.Now().Add(20 * time.Millisecond))
	var b [1]byte
	if n, err := server.Read(b[:]); n != 0 || err == nil {
		t.Fatal("unexpected request after readiness")
	}
	server.SetReadDeadline(time.Now().Add(time.Second))
	cancel()
	select {
	case <-d.done:
	case <-time.After(time.Second):
		t.Fatal("cancellation did not release connection")
	}
	if _, err := server.Read(b[:]); !errors.Is(err, io.EOF) {
		t.Fatalf("Wayland connection retained: %v", err)
	}
}
func TestWaylandMissingInterface(t *testing.T) {
	client, server := waylandPair(t)
	fakeDone := make(chan error, 1)
	go func() { fakeDone <- fakeWaylandInit(server, true) }()
	if _, err := initWaylandDevices(context.Background(), client); err == nil || !strings.Contains(err.Error(), "zwlr_virtual_pointer_manager_v1 unavailable") {
		t.Fatalf("missing interface not rejected: %v", err)
	}
	if err := <-fakeDone; err != nil {
		t.Fatal(err)
	}
}
func TestWaylandProtocolFailure(t *testing.T) {
	client, server := waylandPair(t)
	fakeDone := make(chan error, 1)
	go func() {
		if err := fakeWaylandInit(server, false); err != nil {
			fakeDone <- err
			return
		}
		args := append(wlWords(8, 0), wlString("denied")...)
		fakeDone <- (&waylandDevices{conn: server}).request(1, 0, args, nil)
	}()
	d, err := initWaylandDevices(context.Background(), client)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	select {
	case err := <-d.done:
		if !strings.Contains(err.Error(), "denied") {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("protocol failure ignored")
	}
	if err := <-fakeDone; err != nil {
		t.Fatal(err)
	}
}
func TestWaylandDecoding(t *testing.T) {
	for _, s := range []string{"", "a", "abcd", "wl_seat"} {
		got, rest, err := takeWLString(wlString(s))
		if err != nil || got != s || len(rest) != 0 {
			t.Fatalf("string roundtrip %q: %q %v", s, got, err)
		}
	}
	for _, b := range [][]byte{nil, wlWords(0), wlWords(^uint32(0)), append(wlWords(2), []byte{'a', 'b', 0, 0}...)} {
		if _, _, err := takeWLString(b); err == nil {
			t.Fatalf("accepted bad string: %v", b)
		}
	}
	for _, b := range [][]byte{nil, wlWords(1, 4<<16), wlWords(1, 10<<16), wlWords(0, 8<<16), wlWords(1, 12<<16)} {
		if _, _, _, err := readWaylandMessage(bytes.NewReader(b)); err == nil {
			t.Fatalf("accepted bad frame: %v", b)
		}
	}
}
func TestKeyboardKeymap(t *testing.T) {
	keymap := keyboardKeymap()
	if !strings.HasSuffix(keymap, "\x00") || strings.Contains(keymap, "include") {
		t.Fatal("keymap must be NUL-terminated and self contained")
	}
	for r := rune(32); r <= 126; r++ {
		code, _, err := runeKey(r)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(keymap, fmt.Sprintf("<K%03d> = %d;", code, code+8)) {
			t.Fatalf("missing keycode for %q", r)
		}
	}
}

// Optional real parser/keysym verification; Python and libxkbcommon are test
// dependencies only. The production binary never loads either.
func TestKeyboardKeymapXKB(t *testing.T) {
	if os.Getenv("COMPUTER_USE_TEST_XKB") != "1" {
		t.Skip("set COMPUTER_USE_TEST_XKB=1 for libxkbcommon validation")
	}
	var cases [][3]uint32
	for r := rune(32); r <= 126; r++ {
		code, mods, _ := runeKey(r)
		cases = append(cases, [3]uint32{code + 8, mods, uint32(r)})
	}
	for name, sym := range map[string]uint32{"ENTER": 0xff0d, "TAB": 0xff09, "ESC": 0xff1b, "DELETE": 0xffff, "LEFT": 0xff51, "F12": 0xffc9} {
		code, mods, _ := keySpec(name)
		cases = append(cases, [3]uint32{code + 8, mods, sym})
	}
	input, _ := json.Marshal(map[string]any{"keymap": keyboardKeymap(), "cases": cases})
	script := `import ctypes as c, json, sys
x=c.CDLL('libxkbcommon.so.0')
def fn(n, restype, *args):
 f=getattr(x,n); f.restype=restype; f.argtypes=list(args); return f
ptr=c.c_void_p; u=c.c_uint32
ctx=fn('xkb_context_new',ptr,c.c_int)(0)
data=json.load(sys.stdin)
k=fn('xkb_keymap_new_from_string',ptr,ptr,c.c_char_p,c.c_int,c.c_int)(ctx,data['keymap'].encode(),1,0)
assert k, 'keymap did not compile'
mod=fn('xkb_keymap_mod_get_index',u,ptr,c.c_char_p)
for i,name in enumerate(['Shift','Lock','Control','Mod1','Mod2','Mod3','Mod4','Mod5']): assert mod(k,name.encode())==i
s=fn('xkb_state_new',ptr,ptr)(k)
update=fn('xkb_state_update_mask',u,ptr,u,u,u,u,u,u)
sym=fn('xkb_state_key_get_one_sym',u,ptr,u)
for code,mods,want in data['cases']:
 update(s,mods,0,0,0,0,0)
 got=sym(s,code)
 assert got==want, (code,mods,hex(want),hex(got))
fn('xkb_state_unref',None,ptr)(s)
fn('xkb_keymap_unref',None,ptr)(k)
fn('xkb_context_unref',None,ptr)(ctx)
print('US ASCII, special keys and modifier indices verified')
`
	cmd := exec.Command("python3", "-c", script)
	cmd.Stdin = bytes.NewReader(input)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("XKB verification: %v\n%s", err, out)
	}
	t.Log(string(out))
}

func FuzzWaylandString(f *testing.F) {
	f.Add(wlString("wl_seat"))
	f.Add(wlWords(0))
	f.Fuzz(func(t *testing.T, b []byte) { takeWLString(b) })
}

func TestWaylandStartupCancellation(t *testing.T) {
	client, _ := waylandPair(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := initWaylandDevices(ctx, client); err == nil {
		t.Fatal("stalled compositor initialization succeeded")
	}
}

func TestWaylandGlobalRemoval(t *testing.T) {
	client, server := waylandPair(t)
	fakeDone := make(chan error, 1)
	go func() {
		if err := fakeWaylandInit(server, false); err != nil {
			fakeDone <- err
			return
		}
		fakeDone <- (&waylandDevices{conn: server}).request(2, 1, wlWords(10), nil)
	}()
	d, err := initWaylandDevices(context.Background(), client)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	select {
	case err := <-d.done:
		if !strings.Contains(err.Error(), "required Wayland global removed: wl_seat") {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("seat removal ignored")
	}
	if err := <-fakeDone; err != nil {
		t.Fatal(err)
	}
}

func TestKeyboardCLIValidation(t *testing.T) {
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("WAYLAND_SOCKET", "")
	t.Setenv("XDG_RUNTIME_DIR", "")
	if err := runKeyboard([]string{"--help"}); !errors.Is(err, flag.ErrHelp) {
		t.Fatal(err)
	}
	if err := runKeyboard([]string{"type", "hello"}); err == nil || !strings.Contains(err.Error(), "does not inject input") {
		t.Fatal(err)
	}
	if _, err := startWaylandDevices(context.Background()); err == nil || !strings.Contains(err.Error(), "XDG_RUNTIME_DIR") {
		t.Fatal(err)
	}
}
