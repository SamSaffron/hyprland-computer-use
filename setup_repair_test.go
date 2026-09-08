package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type fakeRepairBroker struct {
	events              *[]string
	stopErr, restartErr error
	stopped             bool
}

func (b *fakeRepairBroker) stop(context.Context) error {
	*b.events = append(*b.events, "stop")
	if b.stopErr == nil {
		b.stopped = true
	}
	return b.stopErr
}
func (b *fakeRepairBroker) restart(context.Context, string) error {
	*b.events = append(*b.events, "restart")
	return b.restartErr
}
func (b *fakeRepairBroker) close() {}

type fakeRepairLock struct {
	events *[]string
	closed bool
}

func (l *fakeRepairLock) Close() error {
	if !l.closed {
		*l.events = append(*l.events, "unlock")
		l.closed = true
	}
	return nil
}

type repairFixture struct {
	ops                   repairOps
	events                []string
	broker                *fakeRepairBroker
	stage, root, old      string
	guard, inspector      bool
	configured, wrongPeer bool
	fail, garbled         string
}

func newRepairFixture(t *testing.T, activeBroker bool) *repairFixture {
	t.Helper()
	f := &repairFixture{stage: filepath.Join(t.TempDir(), "build-new"), root: t.TempDir(), old: "/old path/guard's.so", guard: true}
	if activeBroker {
		f.broker = &fakeRepairBroker{events: &f.events}
	}
	helper := filepath.Join(f.stage, "build", "setup-inspector.so")
	name := "computer-use-setup-inspect-" + filepath.Base(f.stage)
	f.ops = repairOps{
		call: func(_ context.Context, args ...string) ([]byte, error) {
			if len(args) == 3 && args[0] == "-j" && args[1] == "plugin" && args[2] == "list" {
				list := []loadedPlugin{}
				if f.guard {
					list = append(list, loadedPlugin{"computer-use-guard", "Computer Use"})
				}
				if f.inspector {
					list = append(list, loadedPlugin{name, "Computer Use"})
				}
				// Other plugins must never be unloaded.
				list = append(list, loadedPlugin{"hyprbars", "Someone Else"})
				return json.Marshal(list)
			}
			if len(args) == 1 && args[0] == name {
				guards := []guardLocation{}
				if f.guard {
					guards = append(guards, guardLocation{f.old, f.configured})
				}
				return json.Marshal(inspectorResult{helper, 2468, guards})
			}
			if len(args) != 3 || args[0] != "plugin" {
				return nil, errors.New("unexpected hyprctl call")
			}
			action, path := args[1], args[2]
			what := ""
			switch {
			case path == helper && action == "load":
				what = "inspect-load"
			case path == helper && action == "unload":
				what = "inspect-unload"
			case path == f.old && action == "unload":
				what = "unload-old"
			case path == filepath.Join(f.stage, "build", "guard.so") && action == "load":
				what = "load-new"
			default:
				return nil, errors.New("attempt to mutate an unrelated plugin")
			}
			f.events = append(f.events, what)
			if f.fail == what {
				return nil, errors.New("injected " + what + " failure")
			}
			if f.garbled == what {
				return []byte("plugin not loaded"), nil
			}
			switch what {
			case "inspect-load":
				f.inspector = true
			case "inspect-unload":
				f.inspector = false
			case "unload-old":
				f.guard = false
			case "load-new":
				f.guard = true
			}
			return []byte("ok\n"), nil
		},
		broker: func() (repairBroker, error) {
			if f.broker == nil || f.broker.stopped {
				return nil, nil
			}
			return f.broker, nil
		},
		peer: func() (int, error) {
			if !f.guard {
				return 0, nil
			}
			if f.wrongPeer {
				return 9876, nil
			}
			return 2468, nil
		},
		lock: func() (io.Closer, error) {
			f.events = append(f.events, "lock")
			if f.fail == "lock" {
				return nil, errors.New("locked")
			}
			return &fakeRepairLock{events: &f.events}, nil
		},
		status: func(context.Context) error {
			f.events = append(f.events, "status")
			if f.fail == "status" {
				return errors.New("not ready")
			}
			return nil
		},
		publish: func() error {
			f.events = append(f.events, "publish")
			if f.fail == "publish" {
				return errors.New("cannot publish")
			}
			return nil
		},
	}
	return f
}
func TestSetupRepairsAndRestarts(t *testing.T) {
	f := newRepairFixture(t, true)
	restarted, err := repairNative(context.Background(), f.stage, f.root, f.ops)
	if err != nil || !restarted {
		t.Fatalf("repair: %v %v", restarted, err)
	}
	want := []string{"inspect-load", "inspect-unload", "stop", "lock", "unload-old", "load-new", "status", "publish", "unlock", "restart"}
	if !reflect.DeepEqual(f.events, want) {
		t.Fatalf("order: %v", f.events)
	}
	if f.inspector || !f.guard {
		t.Fatal("incorrect final plugin state")
	}
}
func TestSetupFirstInstallDoesNotStartBroker(t *testing.T) {
	f := newRepairFixture(t, false)
	f.guard = false
	restarted, err := repairNative(context.Background(), f.stage, f.root, f.ops)
	if err != nil || restarted {
		t.Fatalf("first setup: %v %v", restarted, err)
	}
	for _, e := range f.events {
		if e == "restart" || e == "stop" || e == "unload-old" {
			t.Fatal(f.events)
		}
	}
}
func TestSetupRepairFailsClosed(t *testing.T) {
	for _, failure := range []string{"inspect-load", "inspect-unload", "lock", "unload-old", "load-new", "status", "publish"} {
		t.Run(failure, func(t *testing.T) {
			f := newRepairFixture(t, true)
			f.fail = failure
			if _, err := repairNative(context.Background(), f.stage, f.root, f.ops); err == nil {
				t.Fatal("failure ignored")
			}
			for _, e := range f.events {
				if e == "restart" {
					t.Fatalf("restarted after failure: %v", f.events)
				}
			}
			if strings.HasPrefix(failure, "inspect-") && f.broker.stopped {
				t.Fatal("stopped before inspection/cleanup succeeded")
			}
			if failure == "unload-old" {
				for _, e := range f.events {
					if e == "load-new" {
						t.Fatal("loaded second guard after unload failure")
					}
				}
			}
		})
	}
}
func TestSetupRejectsHyprctlErrorWithSuccessExit(t *testing.T) {
	f := newRepairFixture(t, true)
	f.garbled = "unload-old"
	if _, err := repairNative(context.Background(), f.stage, f.root, f.ops); err == nil {
		t.Fatal("hyprctl text error ignored")
	}
	if !f.guard {
		t.Fatal("old guard should remain loaded")
	}
}
func TestSetupRefusesConfiguredOrWrongSessionGuard(t *testing.T) {
	for _, configured := range []bool{false, true} {
		f := newRepairFixture(t, true)
		f.configured = configured
		f.wrongPeer = !configured
		if _, err := repairNative(context.Background(), f.stage, f.root, f.ops); err == nil {
			t.Fatal("unsafe replacement accepted")
		}
		if f.broker.stopped || !f.guard || f.inspector {
			t.Fatalf("unsafe side effects: %v", f.events)
		}
	}
}
func TestSetupDoesNotForceKillOrUnloadAfterStopFailure(t *testing.T) {
	f := newRepairFixture(t, true)
	f.broker.stopErr = errors.New("did not stop")
	if _, err := repairNative(context.Background(), f.stage, f.root, f.ops); err == nil {
		t.Fatal("stop failure ignored")
	}
	for _, e := range f.events {
		if e == "unload-old" || e == "restart" {
			t.Fatal(f.events)
		}
	}
}
func TestSetupRefusesUnsafeBrokerIdentity(t *testing.T) {
	f := newRepairFixture(t, true)
	f.ops.broker = func() (repairBroker, error) { return nil, errors.New("service-managed or unrecognized executable") }
	if _, err := repairNative(context.Background(), f.stage, f.root, f.ops); err == nil {
		t.Fatal("unsafe broker identity accepted")
	}
	if f.broker.stopped || !f.guard || f.inspector {
		t.Fatal(f.events)
	}
}

func TestSetupRestartFailureIsReported(t *testing.T) {
	f := newRepairFixture(t, true)
	f.broker.restartErr = errors.New("replacement failed readiness")
	if restarted, err := repairNative(context.Background(), f.stage, f.root, f.ops); restarted || err == nil || !strings.Contains(err.Error(), "broker was stopped") {
		t.Fatalf("restart failure hidden: %v %v", restarted, err)
	}
}

func TestCancelledRepairDoesNothing(t *testing.T) {
	f := newRepairFixture(t, true)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := repairNative(ctx, f.stage, f.root, f.ops); !errors.Is(err, context.Canceled) || len(f.events) != 0 {
		t.Fatalf("cancelled repair did work: %v %v", err, f.events)
	}
}

func TestSetupDefaultsToSessionRepair(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("setup intentionally rejects root")
	}
	t.Setenv("HYPRLAND_INSTANCE_SIGNATURE", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	if err := runSetup(nil); err == nil || !strings.Contains(err.Error(), "--build-only") {
		t.Fatalf("expected session guidance: %v", err)
	}
}
func TestBrokerRestartOptions(t *testing.T) {
	args := []string{"serve", "--http", "127.0.0.1:8099", "--oauth", "--data", "relative private data", "--keyboard", "obsolete", "--tls-key=key.pem"}
	want := []string{"serve", "--http", "127.0.0.1:8099", "--oauth", "--data", "relative private data", "--tls-key=key.pem"}
	if got := restartArgs(args); !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
	env := []string{"WAYLAND_SOCKET=9", "XDG_RUNTIME_DIR=/run/user/123", "TOKEN=secret", brokerReadyEnv + "=3"}
	if got := withoutEnv(env, "WAYLAND_SOCKET", brokerReadyEnv); !reflect.DeepEqual(got, env[1:3]) {
		t.Fatal("environment filtering failed")
	}
	if !serviceManaged(nil, "0::/user.slice/my-broker.service") || serviceManaged(nil, "0::/user.slice/user@1000.service/app.slice/app-terminal.scope") {
		t.Fatal("service detection failed")
	}
}
func TestLifecycleLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broker.lock")
	first, err := lifecycleLock(path)
	if err != nil {
		t.Fatal(err)
	}
	if second, err := lifecycleLock(path); err == nil {
		second.Close()
		t.Fatal("concurrent owner accepted")
	}
	first.Close()
	next, err := lifecycleLock(path)
	if err != nil {
		t.Fatal(err)
	}
	next.Close()
}
