package app

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
	outdated            bool
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
func (b *fakeRepairBroker) currentExecutable() (bool, error) { return !b.outdated, nil }
func (b *fakeRepairBroker) close()                           {}

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
	restartRequired       bool
	missingHook           bool
	sameGuard             bool
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
					guards = append(guards, guardLocation{Path: f.old, Configured: f.configured, RestartRequired: f.restartRequired})
				}
				return json.Marshal(inspectorResult{Inspector: helper, PID: 2468, Guards: guards, IndependentSeatHookAvailable: !f.missingHook})
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
		equivalent: func(_, _ string) (bool, error) { return f.sameGuard, nil },
	}
	return f
}
func TestSameFileContents(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first")
	second := filepath.Join(dir, "second")
	if err := os.WriteFile(first, []byte("same bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("same bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	if same, err := sameFileContents(first, second); err != nil || !same {
		t.Fatalf("equal files: %v %v", same, err)
	}
	if err := os.WriteFile(second, []byte("different!"), 0600); err != nil {
		t.Fatal(err)
	}
	if same, err := sameFileContents(first, second); err != nil || same {
		t.Fatalf("different files: %v %v", same, err)
	}
	if same, err := sameFileContents(first, filepath.Join(dir, "missing")); err != nil || same {
		t.Fatalf("missing file: %v %v", same, err)
	}
}

func TestSetupRepairsAndRestarts(t *testing.T) {
	f := newRepairFixture(t, true)
	result, err := repairNative(context.Background(), f.stage, f.root, f.ops)
	if err != nil || !result.brokerRestarted || !result.brokerRunning || result.guardUnchanged {
		t.Fatalf("repair: %+v %v", result, err)
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
	result, err := repairNative(context.Background(), f.stage, f.root, f.ops)
	if err != nil || result.brokerRestarted || result.brokerRunning {
		t.Fatalf("first setup: %+v %v", result, err)
	}
	for _, e := range f.events {
		if e == "restart" || e == "stop" || e == "unload-old" {
			t.Fatal(f.events)
		}
	}
}
func TestSetupAlreadyCurrentDoesNotDisruptRunningBroker(t *testing.T) {
	f := newRepairFixture(t, true)
	f.sameGuard = true
	f.restartRequired = true
	result, err := repairNative(context.Background(), f.stage, f.root, f.ops)
	if err != nil || !result.guardUnchanged || !result.brokerRunning || result.brokerRestarted {
		t.Fatalf("idempotent setup: %+v %v", result, err)
	}
	want := []string{"inspect-load", "inspect-unload", "status", "publish"}
	if !reflect.DeepEqual(f.events, want) {
		t.Fatalf("already-current setup was disruptive: %v", f.events)
	}
	if f.broker.stopped || !f.guard {
		t.Fatal("already-current state changed")
	}
}

func TestSetupAlreadyCurrentStillReportsMissingBroker(t *testing.T) {
	f := newRepairFixture(t, false)
	f.sameGuard = true
	result, err := repairNative(context.Background(), f.stage, f.root, f.ops)
	if err != nil || !result.guardUnchanged || result.brokerRunning || result.brokerRestarted {
		t.Fatalf("current guard without broker: %+v %v", result, err)
	}
	want := []string{"inspect-load", "inspect-unload", "status", "publish"}
	if !reflect.DeepEqual(f.events, want) {
		t.Fatal(f.events)
	}
}

func TestSetupRestartsOnlyOutdatedBrokerWhenGuardIsCurrent(t *testing.T) {
	f := newRepairFixture(t, true)
	f.sameGuard = true
	f.broker.outdated = true
	result, err := repairNative(context.Background(), f.stage, f.root, f.ops)
	if err != nil || !result.guardUnchanged || !result.brokerRestarted || !result.brokerRunning {
		t.Fatalf("broker-only update: %+v %v", result, err)
	}
	want := []string{"inspect-load", "inspect-unload", "status", "stop", "lock", "publish", "unlock", "restart"}
	if !reflect.DeepEqual(f.events, want) {
		t.Fatalf("guard was unnecessarily replaced: %v", f.events)
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
func TestAutomaticSetupSelectsBeforeDisruption(t *testing.T) {
	for _, available := range []bool{false, true} {
		f := newRepairFixture(t, true)
		f.missingHook = !available
		selected := false
		f.ops.selectInput = func(got bool) error {
			if got != available || f.broker.stopped {
				t.Fatal("selection was wrong or too late")
			}
			selected = true
			return nil
		}
		if _, err := repairNative(context.Background(), f.stage, f.root, f.ops); err != nil || !selected {
			t.Fatal(err)
		}
	}
}

func TestAutomaticSelectionFailureLeavesSessionAlone(t *testing.T) {
	f := newRepairFixture(t, true)
	f.ops.selectInput = func(bool) error { return errors.New("stage failed") }
	if _, err := repairNative(context.Background(), f.stage, f.root, f.ops); err == nil {
		t.Fatal("accepted failed staging")
	}
	if f.broker.stopped || !f.guard || f.inspector {
		t.Fatal(f.events)
	}
}

func TestSetupRefusesHotUnloadOfIndependentSeat(t *testing.T) {
	f := newRepairFixture(t, true)
	f.restartRequired = true
	_, err := repairNative(context.Background(), f.stage, f.root, f.ops)
	if err == nil {
		t.Fatal("unsafe replacement accepted")
	}
	for _, want := range []string{
		"RESULT: HYPRLAND RESTART REQUIRED",
		"Active guard: unchanged and still loaded",
		"Broker: not stopped by setup",
		"Fully exit your Hyprland session and log back in",
		"A config reload is not enough",
		"Rerun the same hyprland-computer-use setup command",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q in:\n%s", want, err)
		}
	}
	if f.broker.stopped || !f.guard || f.inspector {
		t.Fatalf("unexpected side effects: %v", f.events)
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
	if result, err := repairNative(context.Background(), f.stage, f.root, f.ops); result.brokerRestarted || err == nil || !strings.Contains(err.Error(), "broker was stopped") {
		t.Fatalf("restart failure hidden: %+v %v", result, err)
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
