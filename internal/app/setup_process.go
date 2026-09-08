package app

import (
	"bufio"
	"context"
	"debug/buildinfo"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const brokerReadyEnv = "COMPUTER_USE_INTERNAL_READY_FD"
const projectModule = "github.com/sam-saffron-jarvis/hyprland-computer-use"

func lifecycleLock(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil, fmt.Errorf("invalid lifecycle lock file %s", path)
	}
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("another broker/setup holds %s: %w", path, err)
	}
	return f, nil
}

func peerCredentials(c *net.UnixConn) (*unix.Ucred, error) {
	raw, err := c.SyscallConn()
	if err != nil {
		return nil, err
	}
	var cred *unix.Ucred
	var sockErr error
	err = raw.Control(func(fd uintptr) { cred, sockErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED) })
	if err != nil {
		return nil, err
	}
	return cred, sockErr
}

func socketPeer(path string) (*unix.Ucred, error) {
	c, err := net.DialTimeout("unix", path, time.Second)
	if socketUnavailable(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer c.Close()
	return peerCredentials(c.(*net.UnixConn))
}

type brokerProcess struct {
	pid       int
	pidfd     int
	args, env []string
	dir       string
}

// Use kernel socket credentials and a pidfd, never a stale PID file or a
// process-name search. The original executable must be this Go project.
func findBroker(dir string) (*brokerProcess, error) {
	cred, err := socketPeer(filepath.Join(dir, "mcp.sock"))
	if err != nil || cred == nil {
		return nil, err
	}
	if cred.Uid != uint32(os.Getuid()) || cred.Pid <= 0 || cred.Pid == int32(os.Getpid()) {
		return nil, errors.New("MCP socket is not owned by another desktop-user broker")
	}
	fd, err := unix.PidfdOpen(int(cred.Pid), 0)
	if err != nil {
		return nil, fmt.Errorf("pin broker process: %w", err)
	}
	p := &brokerProcess{pid: int(cred.Pid), pidfd: fd}
	success := false
	defer func() {
		if !success {
			p.close()
		}
	}()
	proc := filepath.Join("/proc", strconv.Itoa(p.pid))
	info, err := buildinfo.ReadFile(filepath.Join(proc, "exe"))
	if err != nil || info.Main.Path != projectModule {
		return nil, errors.New("refusing to stop MCP socket owner: executable is not a recognized computer-use broker")
	}
	cmdline, err := readProcFile(filepath.Join(proc, "cmdline"))
	if err != nil {
		return nil, err
	}
	args := splitNUL(cmdline)
	if len(args) < 2 || args[1] != "serve" {
		return nil, errors.New("refusing to stop socket owner: it was not started with serve")
	}
	p.args = restartArgs(args[1:])
	environ, err := readProcFile(filepath.Join(proc, "environ"))
	if err != nil {
		return nil, fmt.Errorf("read broker environment: %w", err)
	}
	p.env = splitNUL(environ)
	if envValue(p.env, "HYPRLAND_INSTANCE_SIGNATURE") != os.Getenv("HYPRLAND_INSTANCE_SIGNATURE") || envValue(p.env, "XDG_RUNTIME_DIR") != os.Getenv("XDG_RUNTIME_DIR") {
		return nil, errors.New("broker belongs to a different desktop session; refusing automatic restart")
	}
	cgroup, err := readProcFile(filepath.Join(proc, "cgroup"))
	if err != nil {
		return nil, err
	}
	if serviceManaged(p.env, string(cgroup)) {
		return nil, errors.New("broker is service-managed; stop it with its service manager before setup (setup does not manage host services)")
	}
	// Inherited Wayland/readiness descriptors do not survive replacement.
	p.env = withoutEnv(p.env, "WAYLAND_SOCKET", brokerReadyEnv)
	p.dir, err = os.Readlink(filepath.Join(proc, "cwd"))
	if err != nil {
		return nil, fmt.Errorf("read broker working directory: %w", err)
	}
	if info, err := os.Stat(p.dir); err != nil || !info.IsDir() {
		return nil, errors.New("broker working directory is no longer available; cannot safely restart it")
	}
	if exited, err := p.exited(); err != nil || exited {
		return nil, errors.New("broker exited during discovery; rerun setup")
	}
	success = true
	return p, nil
}
func readProcFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 4*1024*1024+1))
	if err == nil && len(b) > 4*1024*1024 {
		return nil, errors.New("process metadata too large")
	}
	return b, err
}
func splitNUL(b []byte) []string { return strings.Split(strings.TrimSuffix(string(b), "\x00"), "\x00") }
func envValue(env []string, key string) string {
	for _, s := range env {
		if v, ok := strings.CutPrefix(s, key+"="); ok {
			return v
		}
	}
	return ""
}
func withoutEnv(env []string, keys ...string) []string {
	result := []string{}
	for _, s := range env {
		drop := false
		for _, k := range keys {
			if strings.HasPrefix(s, k+"=") {
				drop = true
				break
			}
		}
		if !drop {
			result = append(result, s)
		}
	}
	return result
}
func serviceManaged(env []string, cgroup string) bool {
	if envValue(env, "NOTIFY_SOCKET") != "" || envValue(env, "LISTEN_FDS") != "" {
		return true
	}
	for _, line := range strings.Split(cgroup, "\n") {
		if strings.HasSuffix(filepath.Base(line), ".service") {
			return true
		}
	}
	return false
}
func restartArgs(args []string) []string {
	result := []string{}
	for i := 0; i < len(args); i++ {
		// Migrate the obsolete helper override to the built-in Go devices.
		if args[i] == "--keyboard" || args[i] == "-keyboard" {
			i++
			continue
		}
		if strings.HasPrefix(args[i], "--keyboard=") || strings.HasPrefix(args[i], "-keyboard=") {
			continue
		}
		result = append(result, args[i])
	}
	return result
}
func (p *brokerProcess) close() { _ = unix.Close(p.pidfd) }
func (p *brokerProcess) exited() (bool, error) {
	fds := []unix.PollFd{{Fd: int32(p.pidfd), Events: unix.POLLIN}}
	n, err := unix.Poll(fds, 0)
	if err == unix.EINTR {
		return false, nil
	}
	return n > 0, err
}
func (p *brokerProcess) stop(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Stopping broker %d; existing grants and MCP connections will be dropped.\n", p.pid)
	if err := unix.PidfdSendSignal(p.pidfd, unix.SIGTERM, nil, 0); err != nil && !errors.Is(err, unix.ESRCH) {
		return err
	}
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		if exited, err := p.exited(); err != nil {
			return err
		} else if exited {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("broker did not stop gracefully; guard left loaded (no forced kill)")
		case <-ticker.C:
		}
	}
}

// Setup only restarts a previously running broker. This is a regular user
// process, not a systemd service; stdout/stderr go to a private persistent log.
func (p *brokerProcess) restart(ctx context.Context, root string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	logPath := filepath.Join(root, "broker.log")
	fd, err := unix.Open(logPath, unix.O_CREAT|unix.O_WRONLY|unix.O_APPEND|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0600)
	if err != nil {
		return err
	}
	log := os.NewFile(uintptr(fd), logPath)
	defer log.Close()
	if info, err := log.Stat(); err != nil || !info.Mode().IsRegular() {
		return errors.New("restart log is not a regular file")
	}
	if err := log.Chmod(0600); err != nil {
		return err
	}
	read, write, err := os.Pipe()
	if err != nil {
		return err
	}
	defer read.Close()
	defer write.Close()
	cmd := exec.Command("/proc/self/exe", p.args...)
	cmd.Args[0] = exe
	cmd.Dir = p.dir
	cmd.Env = append(p.env, brokerReadyEnv+"=3")
	cmd.ExtraFiles = []*os.File{write}
	cmd.Stdout = log
	cmd.Stderr = log
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("restart broker: %w", err)
	}
	write.Close()
	_ = read.SetReadDeadline(time.Now().Add(15 * time.Second))
	stopRead := context.AfterFunc(ctx, func() { read.Close() })
	defer stopRead()
	line, err := bufio.NewReader(io.LimitReader(read, 64)).ReadString('\n')
	if err != nil || line != "ready\n" {
		// Only this newly spawned child can be killed here, never a discovered PID.
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fmt.Errorf("replacement broker did not become ready; see %s (start it manually with the updated binary if needed)", logPath)
	}
	fmt.Fprintf(os.Stderr, "Broker restarted as PID %d in approve mode. MCP clients must reconnect. Log: %s\n", cmd.Process.Pid, logPath)
	return cmd.Process.Release()
}

func brokerReadyFile() (*os.File, error) {
	value := os.Getenv(brokerReadyEnv)
	_ = os.Unsetenv(brokerReadyEnv)
	if value == "" {
		return nil, nil
	}
	fd, err := strconv.Atoi(value)
	if err != nil || fd < 3 {
		return nil, errors.New("invalid internal broker readiness descriptor")
	}
	return os.NewFile(uintptr(fd), "broker-ready"), nil
}
