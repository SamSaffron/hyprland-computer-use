package scripts

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// Reproduce an upgraded compositor's unlinked executable without modifying
// any installed binary: the disposable fixture deletes only its own test file.
func TestPopupLookupFromDeletedExecutable(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux /proc regression")
	}
	compiler, err := exec.LookPath("g++")
	if err != nil {
		t.Skip("g++ unavailable")
	}
	dir := t.TempDir()
	header, err := os.ReadFile("../native/popup_hook.hpp")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "popup_hook.hpp"), header, 0600); err != nil {
		t.Fatal(err)
	}
	source := `#include "popup_hook.hpp"
#include <filesystem>
#include <unistd.h>
extern "C" void fixture() asm("_ZN17CXDGShellProtocol14addOrStartGrabEN9Hyprutils6Memory14CSharedPointerI17CXDGPopupResourceEE");
extern "C" void fixture() {}
int main(int, char** argv) {
 if (unlink(argv[0])) return 2;
 std::error_code error;
 std::filesystem::canonical("/proc/self/exe", error);
 if (!error) return 3;
 return independentPopupGrabAddress() == reinterpret_cast<void*>(fixture) ? 0 : 4;
}
`
	src := filepath.Join(dir, "fixture.cpp")
	bin := filepath.Join(dir, "fixture")
	if err = os.WriteFile(src, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, compiler, "-std=c++17", "-Wl,--export-dynamic", src, "-o", bin, "-ldl").CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, out)
	}
	if out, err := exec.CommandContext(ctx, bin).CombinedOutput(); err != nil {
		t.Fatalf("deleted executable lookup: %v\n%s", err, out)
	}
	if _, err = os.Stat(bin); !os.IsNotExist(err) {
		t.Fatalf("fixture did not unlink itself: %v", err)
	}
}
