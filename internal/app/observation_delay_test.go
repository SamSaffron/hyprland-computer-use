package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func delayedInput(delay int) InputArgs {
	return InputArgs{Window: "abc", Revision: "20,40,600,800", Then: "screenshot", Observation: &PostInputObservationOptions{DelayMS: delay}, Actions: []Action{{Type: "key", Key: "ENTER"}}}
}

func TestObservationDelayPrevalidation(t *testing.T) {
	for _, args := range []InputArgs{
		delayedInput(-1), delayedInput(maxObservationDelayMS + 1),
		{Then: "state", Observation: &PostInputObservationOptions{DelayMS: 20}},
		{Observation: &PostInputObservationOptions{DelayMS: 20}},
	} {
		b, calls := inputTransactionFixture(t, "")
		if _, err := b.input(context.Background(), "a", args); err == nil || len(calls) != 0 {
			t.Fatal("invalid delay/options caused effects", args, err)
		}
	}
	for _, delay := range []int{0, maxObservationDelayMS} {
		if err := (PostInputObservationOptions{DelayMS: delay}).validate(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestWaitForObservation(t *testing.T) {
	start := time.Now()
	if err := waitForObservation(context.Background(), 25); err != nil || time.Since(start) < 25*time.Millisecond {
		t.Fatal("wait returned early", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, delay := range []int{0, maxObservationDelayMS} {
		start = time.Now()
		if err := waitForObservation(ctx, delay); !errors.Is(err, context.Canceled) || time.Since(start) > time.Second {
			t.Fatal("wait did not honor cancellation", err)
		}
	}
}

func TestObservationDelayIsPostInput(t *testing.T) {
	var released atomic.Int64
	b, _ := transactionFixture(t, func(q map[string]any) map[string]any {
		if q["op"] == "revoke" {
			released.Store(time.Now().UnixNano())
		}
		if q["observation_check"] == true && time.Since(time.Unix(0, released.Load())) < 40*time.Millisecond {
			return map[string]any{"ok": false, "error": "captured_before_delay"}
		}
		return safeStatus()
	})
	pngFixture(t, b, "")
	r, _, err := b.inputTool(context.Background(), "a", delayedInput(40))
	if err != nil || r.IsError {
		t.Fatal(err)
	}
	meta := toolMetadata(t, r)
	if meta["status"] != "completed" || meta["observation"].(map[string]any)["status"] != "ok" || len(r.Content) != 2 {
		t.Fatal(meta)
	}
}

func TestObservationDelayCancellationPreservesCompletedInput(t *testing.T) {
	for _, timeout := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancel", true: "deadline"}[timeout], func(t *testing.T) {
			released := make(chan struct{})
			b, _ := transactionFixture(t, func(q map[string]any) map[string]any {
				if q["op"] == "revoke" {
					close(released)
				}
				return safeStatus()
			})
			pngFixture(t, b, "")
			ctx, cancel := context.WithCancel(context.Background())
			if timeout {
				cancel()
				ctx, cancel = context.WithTimeout(context.Background(), time.Second)
			}
			defer cancel()
			unlocked := make(chan bool, 1)
			go func() {
				<-released
				// The post-input wait must not hold the broker input mutex.
				deadline := time.Now().Add(500 * time.Millisecond)
				for time.Now().Before(deadline) {
					if b.inputMu.TryLock() {
						b.inputMu.Unlock()
						unlocked <- true
						if !timeout {
							cancel()
						}
						return
					}
					time.Sleep(time.Millisecond)
				}
				unlocked <- false
				cancel()
			}()
			start := time.Now()
			r, _, err := b.inputTool(ctx, "a", delayedInput(maxObservationDelayMS))
			if err != nil || r.IsError || time.Since(start) > 2*time.Second || !<-unlocked {
				t.Fatal("cancellation lost completed input or retained input lock", err)
			}
			meta := toolMetadata(t, r)
			obs := meta["observation"].(map[string]any)
			want := context.Canceled.Error()
			if timeout {
				want = context.DeadlineExceeded.Error()
			}
			if meta["status"] != "completed" || meta["actions"] != float64(1) || obs["status"] != "failed" || obs["error"] != want || len(r.Content) != 1 {
				t.Fatal(meta)
			}
			if _, err := os.Stat(filepath.Join(b.backend.Dir, "frame.png.args")); !os.IsNotExist(err) {
				t.Fatal("capture ran after canceled wait")
			}
		})
	}
}

func TestObservationDelayRechecksAuthorityAndGeometry(t *testing.T) {
	for _, change := range []string{"pause", "revoke", "resize", "close", "lock"} {
		t.Run(change, func(t *testing.T) {
			released := make(chan struct{})
			var locked atomic.Bool
			b, _ := transactionFixture(t, func(q map[string]any) map[string]any {
				if q["op"] == "revoke" {
					close(released)
				}
				r := safeStatus()
				r["locked"] = locked.Load()
				return r
			})
			pngFixture(t, b, "")
			changed := make(chan struct{})
			go func() {
				defer close(changed)
				<-released
				time.Sleep(10 * time.Millisecond)
				switch change {
				case "pause":
					b.mu.Lock()
					b.paused = true
					b.mu.Unlock()
				case "revoke":
					b.mu.Lock()
					b.mode = "approve" // No grants remain: observation must ask, not capture.
					b.mu.Unlock()
				case "lock":
					locked.Store(true)
				default:
					path := filepath.Join(b.backend.Dir, "windows.json")
					data, _ := os.ReadFile(path)
					if change == "close" {
						data = []byte("[]")
					} else {
						data = []byte(strings.ReplaceAll(string(data), "600,800", "620,800"))
					}
					os.WriteFile(path, data, 0600)
				}
			}()
			r, _, err := b.inputTool(context.Background(), "a", delayedInput(100))
			<-changed
			if err != nil || r.IsError {
				t.Fatal(err)
			}
			meta := toolMetadata(t, r)
			obs := meta["observation"].(map[string]any)
			if meta["status"] != "completed" || meta["actions"] != float64(1) {
				t.Fatal(meta)
			}
			if change == "resize" {
				// Observation intentionally uses fresh geometry, not input's old revision.
				if obs["status"] != "ok" || obs["revision"] != "20,40,620,800" || len(r.Content) != 2 {
					t.Fatal(obs)
				}
			} else {
				want := "failed"
				if change == "revoke" {
					want = "approval_required"
				}
				if obs["status"] != want || len(r.Content) != 1 {
					t.Fatal(obs)
				}
				if _, err := os.Stat(filepath.Join(b.backend.Dir, "frame.png.args")); !os.IsNotExist(err) {
					t.Fatal("captured after authority/session/window loss")
				}
			}
		})
	}
}

func TestObservationDelaySchema(t *testing.T) {
	data, err := json.Marshal(inputWindowSchema())
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	json.Unmarshal(data, &root)
	props := root["properties"].(map[string]any)
	obs := props["observation"].(map[string]any)["properties"].(map[string]any)
	delay := obs["delay_ms"].(map[string]any)
	if delay["minimum"] != float64(0) || delay["maximum"] != float64(maxObservationDelayMS) {
		t.Fatal(delay)
	}
	if _, exists := props["delay_ms"]; exists {
		t.Fatal("delay must be nested, not a general input sleep")
	}
}

func TestFailedInputDoesNotWaitForObservation(t *testing.T) {
	b, _ := inputTransactionFixture(t, "key_transaction")
	start := time.Now()
	r, _, err := b.inputTool(context.Background(), "a", delayedInput(maxObservationDelayMS))
	if err != nil || !r.IsError || time.Since(start) > time.Second {
		t.Fatal("failed input should return without post-action wait", err)
	}
	meta := toolMetadata(t, r)
	if meta["status"] != "failed" || meta["observation"] != nil {
		t.Fatal(meta)
	}
}
