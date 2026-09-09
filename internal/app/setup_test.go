package app

import (
	"bytes"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeBundleExtraction(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "extracted")
	if err := extractNative(dir); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"Makefile", "LICENSE", "THIRD_PARTY.md", "native/guard.cpp", "native/independent_seat.hpp", "native/seat_policy.hpp", "native/input_transaction.hpp", "native/surface_routing.hpp", "native/surface_tree.hpp", "native/text_transaction.hpp", "native/text_keymap.hpp", "native/text_keyboard.hpp", "native/setup_inspector.cpp", "native/version.cpp", "native/virtual-keyboard-unstable-v1.xml", "native/wlr-virtual-pointer-unstable-v1.xml"} {
		want, err := os.ReadFile(filepath.Join("..", "..", path))
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(dir, path))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("bundle differs from source %s: %v", path, err)
		}
	}
	if err := fs.WalkDir(os.DirFS(dir), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err == nil && info.Mode().Perm()&0077 != 0 {
			t.Errorf("nonprivate extraction: %s", path)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestCheckHyprlandVersion(t *testing.T) {
	for _, tc := range []struct {
		header, running string
		valid           bool
	}{
		{"abc\n", `{"commit":"abc"}`, true},
		{"abc", `{"commit":"def"}`, false},
		{"", `{"commit":""}`, false},
		{"abc", `{}`, false},
		{"abc", `not JSON`, false},
	} {
		if err := checkHyprlandVersion([]byte(tc.header), []byte(tc.running)); (err == nil) != tc.valid {
			t.Errorf("%+v: %v", tc, err)
		}
	}
}

func TestSetupBuildAndFailurePreservesCurrent(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("setup intentionally rejects root")
	}
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("HYPRLAND_INSTANCE_SIGNATURE", "")
	tools := t.TempDir()
	for _, tool := range []string{"g++", "pkg-config"} {
		if err := os.WriteFile(filepath.Join(tools, tool), []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	// Mock the toolchain, not the installer; no compositor command is available.
	makeScript := "#!/bin/sh\n[ -z \"$FAIL_BUILD\" ] || exit 1\n[ \"$1\" = setup-native ] || exit 2\n/bin/mkdir build\n: > build/guard.so\n"
	if err := os.WriteFile(filepath.Join(tools, "make"), []byte(makeScript), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tools)
	if err := runSetup([]string{"--build-only"}); err != nil {
		t.Fatal(err)
	}
	root, _ := nativeInstallRoot()
	current := filepath.Join(root, "current")
	first, err := os.Readlink(current)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAIL_BUILD", "1")
	if err := runSetup([]string{"--build-only"}); err == nil {
		t.Fatal("expected failed build")
	}
	if got, err := os.Readlink(current); err != nil || got != first {
		t.Fatalf("changed current after failure: %s %v", got, err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 3 { // old build, current symlink, setup lock
		t.Fatalf("failed staging directory not removed: %v %v", entries, err)
	}
	t.Setenv("FAIL_BUILD", "")
	if err := runSetup([]string{"--build-only"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.Readlink(current); got == first {
		t.Fatal("successful rebuild did not publish")
	}
	if _, err := os.Stat(filepath.Join(first, "build", "guard.so")); err != nil {
		t.Fatal("old plugin build removed", err)
	}
}

func TestAutomaticSetupStagesIndependentVariant(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("setup rejects root")
	}
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	tools := t.TempDir()
	for _, tool := range []string{"g++", "pkg-config"} {
		if err := os.WriteFile(filepath.Join(tools, tool), []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	script := "#!/bin/sh\n/bin/mkdir -p build\ncase $1 in\nsetup-native) printf legacy > build/guard.so;;\nindependent-seat) [ -z \"$FAIL_SEAT\" ] || exit 2; printf independent > build/guard-seat.so;;\n*) exit 1;;\nesac\n"
	if err := os.WriteFile(filepath.Join(tools, "make"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tools)
	for _, tc := range []struct{ name, mode, fail, want string }{
		{"preferred", "auto", "", "independent"},
		{"build unavailable", "auto", "1", "legacy"},
		{"explicit fallback", "focus-borrowing", "", "legacy"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("FAIL_SEAT", tc.fail)
			if err := runSetup([]string{"--build-only", "--input-mode=" + tc.mode}); err != nil {
				t.Fatal(err)
			}
			root, err := nativeInstallRoot()
			if err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(filepath.Join(root, "current", "build", "guard.so"))
			if err != nil || string(got) != tc.want {
				t.Fatalf("wrong staged guard: %q %v", got, err)
			}
		})
	}
}

func TestSetupHelpWithoutSession(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "")
	old := os.Args
	t.Cleanup(func() { os.Args = old })
	os.Args = []string{"hyprland-computer-use", "setup", "--help"}
	if err := run(); err == nil || strings.Contains(err.Error(), "XDG_RUNTIME_DIR") {
		t.Fatalf("help needs desktop: %v", err)
	}
}

func TestSetupLoadRejectsMismatchedBuild(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("setup intentionally rejects root")
	}
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("HYPRLAND_INSTANCE_SIGNATURE", "test")
	t.Setenv("WAYLAND_DISPLAY", "test")
	tools := t.TempDir()
	marker := filepath.Join(tools, "loaded")
	t.Setenv("LOAD_TEST_MARKER", marker)
	scripts := map[string]string{
		"make":    "/bin/mkdir build\nprintf '#!/bin/sh\\necho header-commit\\n' > build/header-version\n/bin/chmod 700 build/header-version\n",
		"hyprctl": "if [ \"$1\" = -j ]; then echo '{\"commit\":\"different-commit\"}'; else : > \"$LOAD_TEST_MARKER\"; fi\n",
		"g++":     "exit 0\n", "pkg-config": "exit 0\n",
	}
	for name, script := range scripts {
		if err := os.WriteFile(filepath.Join(tools, name), []byte("#!/bin/sh\n"+script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", tools)
	if err := runSetup([]string{"--load"}); err == nil || !strings.Contains(err.Error(), "refusing to load plugin") {
		t.Fatalf("expected mismatch: %v", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("attempted to load mismatched plugin")
	}
	root, _ := nativeInstallRoot()
	if _, err := os.Lstat(filepath.Join(root, "current")); !os.IsNotExist(err) {
		t.Fatal("published mismatched build")
	}
}

func TestSetupLeavesActiveGuardAloneWhenBuildToolsMissing(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("setup intentionally rejects root")
	}
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("HYPRLAND_INSTANCE_SIGNATURE", "test")
	t.Setenv("WAYLAND_DISPLAY", "test")
	dir, err := runtimeDir()
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", filepath.Join(dir, "guard.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	tools := t.TempDir()
	if err := os.WriteFile(filepath.Join(tools, "hyprctl"), []byte("#!/bin/sh\nexit 99\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tools)
	if err := runSetup([]string{"--load"}); err == nil || !strings.Contains(err.Error(), "missing build tool") {
		t.Fatalf("expected build preflight refusal before disturbing the active guard: %v", err)
	}
	c, err := net.Dial("unix", filepath.Join(dir, "guard.sock"))
	if err != nil {
		t.Fatalf("active guard was disturbed: %v", err)
	}
	c.Close()
}
