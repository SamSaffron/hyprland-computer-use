package app

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func pngFixture(t *testing.T, b *Broker, after string) []byte {
	t.Helper()
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 300, 400))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(b.backend.Dir, "frame.png"), data.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CU_TEST_FRAME", filepath.Join(b.backend.Dir, "frame.png"))
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$CU_TEST_FRAME.args\"\ncat \"$CU_TEST_FRAME\"\n" + after
	if err := os.WriteFile(filepath.Join(b.backend.Dir, "grim"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}
func toolMetadata(t *testing.T, r *mcp.CallToolResult) map[string]any {
	t.Helper()
	var meta map[string]any
	if err := json.Unmarshal([]byte(r.Content[0].(*mcp.TextContent).Text), &meta); err != nil {
		t.Fatal(err)
	}
	return meta
}
func safeStatus() map[string]any {
	return map[string]any{"ok": true, "version": 2, "focus_preserving": true, "locked": false, "unicode_text": true, "text_chunk_runes": textChunkRunes}
}

func TestCaptureMetadata(t *testing.T) {
	b, _ := inputTransactionFixture(t, "")
	data := pngFixture(t, b, "")
	w, _ := b.backend.window(context.Background(), "abc")
	meta, err := captureMetadata(w, data, time.Unix(100, 0))
	if err != nil {
		t.Fatal(err)
	}
	if meta["image_size"] != [2]int{300, 400} || meta["logical_size"] != [2]int{600, 800} {
		t.Fatal(meta)
	}
	transform := meta["image_to_window"].(map[string]any)
	if transform["scale_x"] != float64(2) || transform["scale_y"] != float64(2) {
		t.Fatal(transform)
	}
	again, _ := captureMetadata(w, data, time.Unix(101, 0))
	if meta["frame_id"] != again["frame_id"] || meta["captured_at"] == again["captured_at"] {
		t.Fatal("frame ID must identify content, not timestamp")
	}
	changed := append(append([]byte{}, data...), 0)
	other, err := captureMetadata(w, changed, time.Unix(100, 0))
	if err != nil {
		t.Fatal(err)
	}
	if meta["frame_id"] == other["frame_id"] {
		t.Fatal("changed encoded content retained ID")
	}
	if _, err := captureMetadata(w, []byte("invalid"), time.Now()); err == nil {
		t.Fatal("invalid PNG")
	}
}

func TestCaptureSafety(t *testing.T) {
	for _, tc := range []struct {
		name    string
		failAt  int
		unknown bool
	}{{"locked_before", 1, false}, {"locked_after", 2, false}, {"unknown_lock_state", 1, true}, {"unlocked", 0, false}} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			b, _ := transactionFixture(t, func(q map[string]any) map[string]any {
				r := safeStatus()
				if q["op"] == "status" {
					calls++
					if calls == tc.failAt {
						if tc.unknown {
							delete(r, "locked")
						} else {
							r["locked"] = true
						}
					}
				}
				return r
			})
			pngFixture(t, b, "")
			meta, data, err := b.observe(context.Background(), "a", "abc", 0)
			if tc.failAt != 0 {
				if err == nil || data != nil || meta != nil {
					t.Fatalf("released unsafe frame: %v", err)
				}
				wantError := "session_locked"
				if tc.unknown {
					wantError = "observation_guard_status_unavailable"
				}
				if !strings.Contains(err.Error(), wantError) {
					t.Fatal(err)
				}
				if tc.failAt == 2 {
					if _, err := os.Stat(filepath.Join(b.backend.Dir, "frame.png.args")); err != nil {
						t.Fatal("post-capture check not exercised", err)
					}
				}
				if tc.failAt == 1 {
					if _, err := os.Stat(filepath.Join(b.backend.Dir, "frame.png.args")); !os.IsNotExist(err) {
						t.Fatal("grim ran before safety check")
					}
				}
			} else {
				if err != nil || len(data) == 0 {
					t.Fatal(err)
				}
				args, _ := os.ReadFile(filepath.Join(b.backend.Dir, "frame.png.args"))
				if !strings.HasPrefix(string(args), "-T\nabc\n") || strings.Contains(string(args), "-g") {
					t.Fatal(string(args))
				}
			}
		})
	}
}

func TestCaptureRejectsWorkspaceTransition(t *testing.T) {
	b, _ := inputTransactionFixture(t, "")
	pngFixture(t, b, "sed -i 's/\"id\":1/\"id\":2/' \"$CU_TEST_WINDOWS\"\n")
	_, data, err := b.observe(context.Background(), "a", "abc", 0)
	if err == nil || data != nil {
		t.Fatal("released frame after workspace change")
	}
}

func TestObservationRechecksPermission(t *testing.T) {
	var broker atomic.Pointer[Broker]
	checks := 0
	b, _ := transactionFixture(t, func(q map[string]any) map[string]any {
		if q["op"] == "status" {
			checks++
			if checks == 2 {
				b := broker.Load()
				b.mu.Lock()
				b.paused = true
				b.mu.Unlock()
			}
		}
		return safeStatus()
	})
	broker.Store(b)
	pngFixture(t, b, "")
	_, data, err := b.observe(context.Background(), "a", "abc", 0)
	if err == nil || !strings.Contains(err.Error(), "revoked") || data != nil {
		t.Fatal("released frame after pause", err)
	}
}

func TestPartialTextResult(t *testing.T) {
	var keys, pointers atomic.Int64
	b, _ := transactionFixture(t, func(q map[string]any) map[string]any {
		if q["op"] == "pointer_transaction" {
			pointers.Add(1)
		}
		if q["op"] == "key_transaction" {
			keys.Add(1)
		}
		if q["op"] == "text_transaction" {
			keys.Add(1)
			return map[string]any{"ok": false, "error": "input_busy_keys_held", "completed_characters": 1}
		}
		return safeStatus()
	})
	r, _, err := b.inputTool(context.Background(), "a", InputArgs{Window: "abc", Revision: "20,40,600,800", Actions: []Action{{Type: "key", Key: "ENTER"}, {Type: "text", Text: "abc"}, {Type: "click", X: 1, Y: 1}}})
	if err != nil || !r.IsError {
		t.Fatal(err)
	}
	meta := toolMetadata(t, r)
	if meta["completed_actions"] != float64(1) || meta["failed_action"] != float64(1) || meta["completed_characters"] != float64(1) || meta["failed_transaction_may_have_effects"] != true || keys.Load() != 2 || pointers.Load() != 0 {
		t.Fatal(meta, keys.Load(), pointers.Load())
	}
}

func TestActionAndObserve(t *testing.T) {
	for _, captureFails := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "observation_failed"}[captureFails], func(t *testing.T) {
			reject := ""
			if captureFails {
				reject = "status"
			}
			b, _ := inputTransactionFixture(t, reject)
			pngFixture(t, b, "")
			r, _, err := b.inputTool(context.Background(), "a", InputArgs{Window: "abc", Revision: "20,40,600,800", Then: "screenshot", Actions: []Action{{Type: "key", Key: "ENTER"}}})
			if err != nil || r.IsError {
				t.Fatal(err)
			}
			meta := toolMetadata(t, r)
			if meta["status"] != "completed" || meta["actions"] != float64(1) {
				t.Fatal(meta)
			}
			observation := meta["observation"].(map[string]any)
			if captureFails {
				if observation["status"] != "failed" || len(r.Content) != 1 {
					t.Fatal(meta)
				}
			} else if observation["status"] != "ok" || len(r.Content) != 2 {
				t.Fatal(meta)
			}
		})
	}
}

func TestInvalidObservationOptionsHaveNoEffects(t *testing.T) {
	for _, a := range []InputArgs{{Then: "ui"}, {Then: "screenshot", MaxWidth: -1}, {Then: "screenshot", MaxWidth: 1921}, {MaxWidth: 100}} {
		b, calls := inputTransactionFixture(t, "")
		a.Window = "abc"
		a.Revision = "20,40,600,800"
		a.Actions = []Action{{Type: "key", Key: "ENTER"}}
		if _, err := b.input(context.Background(), "a", a); err == nil || len(calls) != 0 {
			t.Fatal("options not prevalidated")
		}
	}
}

// Exercise actual SDK serialization, not just handler return values: metadata
// must remain available in structuredContent as well as JSON text.
func TestObservationMCPWireContract(t *testing.T) {
	for _, name := range []string{"view", "approval", "plain_input", "then", "then_failed", "partial", "then_approval"} {
		t.Run(name, func(t *testing.T) {
			var broker atomic.Pointer[Broker]
			b, _ := transactionFixture(t, func(q map[string]any) map[string]any {
				if name == "partial" && q["op"] == "key_transaction" {
					return map[string]any{"ok": false, "error": "busy"}
				}
				if name == "then_failed" && q["observation_check"] == true {
					return map[string]any{"ok": false, "error": "session_locked"}
				}
				// End YOLO after the input token is revoked, before observation begins.
				if name == "then_approval" && q["op"] == "revoke" {
					b := broker.Load()
					b.mu.Lock()
					b.mode = "approve"
					b.mu.Unlock()
				}
				return safeStatus()
			})
			broker.Store(b)
			if name == "approval" {
				b.mode = "approve"
			}
			pngFixture(t, b, "")
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			serverTransport, clientTransport := mcp.NewInMemoryTransports()
			serverSession, err := b.newMCPServer("a", nil).Connect(ctx, serverTransport, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer serverSession.Close()
			session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, clientTransport, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			tool := "input_window"
			args := map[string]any{"window_id": "abc", "revision": "20,40,600,800", "actions": []Action{{Type: "key", Key: "ENTER"}}}
			if name == "view" || name == "approval" {
				tool = "view_window"
				args = map[string]any{"window_id": "abc"}
			}
			if strings.HasPrefix(name, "then") {
				args["then"] = "screenshot"
			}
			result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: args})
			if err != nil {
				t.Fatal(err)
			}
			if result.IsError != (name == "partial") {
				t.Fatalf("unexpected isError: %+v", result)
			}
			wire, err := json.Marshal(result.StructuredContent)
			if err != nil {
				t.Fatal(err)
			}
			var structured map[string]any
			if err := json.Unmarshal(wire, &structured); err != nil || structured == nil {
				t.Fatalf("missing structured content: %s (%v)", wire, err)
			}
			text := toolMetadata(t, result)
			if text["status"] != structured["status"] {
				t.Fatal("text/structured mismatch")
			}
			if name == "approval" && (structured["status"] != "approval_required" || structured["request_id"] == nil) {
				t.Fatal(structured)
			}
			if strings.HasPrefix(name, "then") {
				if structured["status"] != "completed" {
					t.Fatal(structured)
				}
				want := map[string]string{"then": "ok", "then_failed": "failed", "then_approval": "approval_required"}[name]
				if structured["observation"].(map[string]any)["status"] != want {
					t.Fatal(structured)
				}
			}
			wantBlocks := 1
			if name == "view" || name == "then" {
				wantBlocks = 2
			}
			if len(result.Content) != wantBlocks {
				t.Fatalf("got %d blocks, want %d", len(result.Content), wantBlocks)
			}
		})
	}
}

func TestCaptureRequiresReachableGuard(t *testing.T) {
	b, _ := inputTransactionFixture(t, "")
	pngFixture(t, b, "")
	b.backend.Dir = filepath.Join(b.backend.Dir, "missing-guard")
	_, data, err := b.observe(context.Background(), "a", "abc", 0)
	if err == nil || !strings.Contains(err.Error(), "compositor_guard_unavailable") || data != nil {
		t.Fatal("capture without guard", err)
	}
}

func TestRecordingStopsOnLockedCapture(t *testing.T) {
	b, _ := transactionFixture(t, func(q map[string]any) map[string]any { r := safeStatus(); r["locked"] = true; return r })
	pngFixture(t, b, "")
	if err := os.WriteFile(filepath.Join(b.backend.Dir, "ffmpeg"), []byte("#!/bin/sh\ncat >/dev/null\n"), 0700); err != nil {
		t.Fatal(err)
	}
	w, err := b.backend.window(context.Background(), "abc")
	if err != nil {
		t.Fatal(err)
	}
	value, err := b.record("a", w)
	if err != nil {
		t.Fatal(err)
	}
	id := value.(map[string]any)["recording_id"].(string)
	t.Cleanup(func() { b.mu.Lock(); b.recordings[id].cancel(); b.mu.Unlock() })
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		b.mu.Lock()
		info := b.recordings[id].Info
		b.mu.Unlock()
		if info.Status == "stopped" {
			if info.Frames != 0 || !strings.Contains(info.Error, "session_locked") {
				t.Fatal(info)
			}
			if _, err := os.Stat(filepath.Join(b.backend.Dir, "frame.png.args")); !os.IsNotExist(err) {
				t.Fatal("recording captured while locked")
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("recording did not stop")
}
