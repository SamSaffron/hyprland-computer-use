package app

import (
	"os"
	"strings"
	"testing"
)

func TestNativeGuardChecksRuntimeAndOwnsSocketLock(t *testing.T) {
	raw, err := os.ReadFile("../../native/guard.cpp")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	for _, required := range []string{
		`const char *runtime = getenv("XDG_RUNTIME_DIR")`,
		`if (!runtime || !*runtime)`,
		`flock(candidateLock, LOCK_EX | LOCK_NB)`,
		`if (lockFD >= 0 && !socketPath.empty())`,
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("native guard is missing %q", required)
		}
	}
	if strings.Contains(source, `std::string(getenv("XDG_RUNTIME_DIR"))`) {
		t.Fatal("native guard constructs std::string from an unchecked getenv result")
	}
}
