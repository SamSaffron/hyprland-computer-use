package app

import (
	"errors"
	"net"
	"path/filepath"
	"testing"
)

type failingRandomReader struct{}

func (failingRandomReader) Read([]byte) (int, error) { return 0, errors.New("entropy unavailable") }

func TestRandomFailureDoesNotCreatePermissionRequest(t *testing.T) {
	old := randomReader
	randomReader = failingRandomReader{}
	defer func() { randomReader = old }()
	b, _ := fixture()
	if _, id, err := b.permit("a", "observe", Scope{"workspace", "1"}, nil, ""); err == nil || id != "" {
		t.Fatalf("permit returned id=%q err=%v", id, err)
	}
	if len(b.requests) != 0 {
		t.Fatal("permission request survived entropy failure")
	}
}

func TestListenUnixReclaimsStaleSocketWhileHoldingLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stale.sock")
	addr, err := net.ResolveUnixAddr("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	stale, err := net.ListenUnix("unix", addr)
	if err != nil {
		t.Fatal(err)
	}
	stale.SetUnlinkOnClose(false)
	if err := stale.Close(); err != nil {
		t.Fatal(err)
	}
	listener, err := listenUnix(path)
	if err != nil {
		t.Fatalf("stale socket was not reclaimed: %v", err)
	}
	listener.Close()
}
