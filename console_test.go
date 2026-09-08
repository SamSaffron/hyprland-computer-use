package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConsoleBundle(t *testing.T) {
	dir := t.TempDir()
	if err := extractConsole(dir); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"shell.qml", "Picker.qml"} {
		want, err := os.ReadFile(filepath.Join("quickshell", name))
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("console bundle differs: %s %v", name, err)
		}
	}
}

func TestConsoleLaunchAndCleanup(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	tools := t.TempDir()
	marker := filepath.Join(tools, "launched")
	t.Setenv("CONSOLE_TEST_MARKER", marker)
	script := "#!/bin/sh\n[ \"$1\" = -p ] || exit 1\n[ -s \"$2/shell.qml\" ] && [ -s \"$2/Picker.qml\" ] || exit 2\nprintf '%s' \"$2\" > \"$CONSOLE_TEST_MARKER\"\n"
	if err := os.WriteFile(filepath.Join(tools, "qs"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tools)
	if err := runConsole(nil); err != nil {
		t.Fatal(err)
	}
	path, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(path), filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "computer-use", "console-")) {
		t.Fatalf("unexpected extraction path: %s", path)
	}
	if _, err := os.Stat(string(path)); !os.IsNotExist(err) {
		t.Fatalf("console temp directory retained: %v", err)
	}
}
