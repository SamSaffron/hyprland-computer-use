package app

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestVersionWithoutDesktop(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("HYPRLAND_INSTANCE_SIGNATURE", "")
	args := os.Args
	t.Cleanup(func() { os.Args = args })
	for _, name := range []string{"version", "--version"} {
		os.Args = []string{"hyprland-computer-use", name}
		if err := run(); err != nil {
			t.Fatal(err)
		}
	}
	os.Args = []string{"hyprland-computer-use", "version", "unexpected"}
	if err := run(); err == nil || !strings.Contains(err.Error(), "no arguments") {
		t.Fatalf("unexpected version arguments accepted: %v", err)
	}
	var b bytes.Buffer
	printVersion(&b)
	for _, part := range []string{"hyprland-computer-use ", Version, Commit, Date} {
		if !strings.Contains(b.String(), part) {
			t.Fatalf("missing version metadata %q: %s", part, b.String())
		}
	}
}
