package app

import (
	"context"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// A controlled child of this test binary, not a desktop broker or service.
func TestSetupRestartChildProcess(t *testing.T) {
	if os.Getenv("COMPUTER_USE_TEST_RESTART_CHILD") != "1" {
		return
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if cwd != os.Getenv("COMPUTER_USE_TEST_CWD") || os.Getenv("COMPUTER_USE_TEST_KEEP") != "preserved" {
		t.Fatal("lost restart context")
	}
	if err := os.WriteFile(os.Getenv("COMPUTER_USE_TEST_PID"), []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
		t.Fatal(err)
	}
	ready, err := brokerReadyFile()
	if err != nil || ready == nil {
		t.Fatal("no readiness descriptor", err)
	}
	if _, err := io.WriteString(ready, "ready\n"); err != nil {
		t.Fatal(err)
	}
	ready.Close()
	// Bounded even if the parent test fails before cleaning up its own child.
	time.Sleep(3 * time.Second)
}
func TestBrokerRestartReadinessAndContext(t *testing.T) {
	root := t.TempDir()
	cwd := t.TempDir()
	pidPath := filepath.Join(root, "child.pid")
	env := withoutEnv(os.Environ(), brokerReadyEnv, "WAYLAND_SOCKET")
	env = append(env, "COMPUTER_USE_TEST_RESTART_CHILD=1", "COMPUTER_USE_TEST_CWD="+cwd, "COMPUTER_USE_TEST_KEEP=preserved", "COMPUTER_USE_TEST_PID="+pidPath)
	p := &brokerProcess{args: []string{"-test.run=^TestSetupRestartChildProcess$"}, env: env, dir: cwd}
	if err := p.restart(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		t.Fatal(err)
	}
	// This PID came from the child this test just launched, not desktop discovery.
	fd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	defer unix.Wait4(pid, nil, 0, nil)
	if err := unix.PidfdSendSignal(fd, unix.SIGTERM, nil, 0); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(root, "broker.log"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("non-private restart log: %v", err)
	}
}
func TestBrokerStopUsesPinnedProcess(t *testing.T) {
	child := exec.Command("sleep", "30")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = child.Process.Kill(); _ = child.Wait() }()
	fd, err := unix.PidfdOpen(child.Process.Pid, 0)
	if err != nil {
		t.Fatal(err)
	}
	p := &brokerProcess{pid: child.Process.Pid, pidfd: fd}
	defer p.close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := p.stop(ctx); err != nil {
		t.Fatal(err)
	}
	if exited, err := p.exited(); err != nil || !exited {
		t.Fatalf("child did not exit: %v", err)
	}
}
func TestBrokerReadyFileValidation(t *testing.T) {
	t.Setenv(brokerReadyEnv, "1")
	if _, err := brokerReadyFile(); err == nil {
		t.Fatal("stdio accepted as readiness pipe")
	}
	if os.Getenv(brokerReadyEnv) != "" {
		t.Fatal("readiness descriptor leaked into subprocess environment")
	}
}
func TestSetupDoesNotStopUnrelatedSocketOwner(t *testing.T) {
	// The kernel credentials point at this test process. Discovery must refuse
	// self/non-broker ownership before opening a pidfd or issuing any signal.
	dir := t.TempDir()
	listener, err := net.Listen("unix", filepath.Join(dir, "mcp.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if _, err := findBroker(dir); err == nil || !strings.Contains(err.Error(), "another desktop-user broker") {
		t.Fatalf("unexpected discovery result: %v", err)
	}
}
