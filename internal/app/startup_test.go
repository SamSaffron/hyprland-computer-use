package app

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"syscall"
	"testing"
)

func TestStartupDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name        string
		cause       error
		unavailable bool
	}{
		{"missing", os.ErrNotExist, true},
		{"refused", syscall.ECONNREFUSED, true},
		{"permission", os.ErrPermission, false},
		{"protocol", errors.New("guard rejected status"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wrapped := fmt.Errorf("startup: %w", tc.cause)
			broker := brokerConnectionError("/custom/mcp.sock", wrapped)
			guard := guardStartupError(wrapped)
			for _, err := range []error{broker, guard} {
				if !errors.Is(err, tc.cause) {
					t.Fatalf("lost underlying error: %v", err)
				}
			}
			if !strings.Contains(broker.Error(), "/custom/mcp.sock") {
				t.Fatal("missing socket path")
			}
			if strings.Contains(broker.Error(), "does not start one") != tc.unavailable {
				t.Fatalf("incorrect broker hint: %v", broker)
			}
			if strings.Contains(guard.Error(), "hyprland-computer-use setup") != tc.unavailable {
				t.Fatalf("incorrect plugin hint: %v", guard)
			}
			if !strings.Contains(guard.Error(), "global input fallback is disabled") {
				t.Fatal("missing fail-closed explanation")
			}
		})
	}
}

func TestMCPMissingBroker(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	args := os.Args
	t.Cleanup(func() { os.Args = args })
	os.Args = []string{"hyprland-computer-use", "mcp"}
	err := run()
	if err == nil || !strings.Contains(err.Error(), "hyprland-computer-use serve") {
		t.Fatalf("expected actionable startup error, got %v", err)
	}
	var op *net.OpError
	if !errors.As(err, &op) || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected preserved dial error, got %v", err)
	}
}
