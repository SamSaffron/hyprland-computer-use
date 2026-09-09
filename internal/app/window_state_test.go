package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func surfaceStateFixture() WindowSurfaceState {
	return WindowSurfaceState{Window: "abc", Revision: "20,40,600,800", Version: 1,
		Surfaces: []SurfaceInfo{
			{ID: "root-handle", Kind: "toplevel", Revision: "0,0,600,800", Size: [2]float64{600, 800}},
			{ID: "popup-handle", Kind: "popup", Revision: "-20,790,200,90", Offset: [2]float64{-20, 790}, Size: [2]float64{200, 90}},
		}, Related: []RelatedWindow{{Window: "dialog", Relationship: "transient_child", SeparateGrantRequired: true}}}
}
func surfaceBrokerFixture(t *testing.T) (*Broker, chan map[string]any) {
	return transactionFixture(t, func(q map[string]any) map[string]any {
		if q["op"] == "window_state" {
			return map[string]any{"ok": true, "state": surfaceStateFixture()}
		}
		r := safeStatus()
		if q["op"] == "text_transaction" {
			r["completed_characters"] = len(q["scalars"].([]any))
		}
		return r
	})
}
func TestWindowStateIsFreeMetadataNotAuthority(t *testing.T) {
	b, calls := surfaceBrokerFixture(t)
	b.mode = "approve"
	b.paused = true
	b.uiCount = 0
	state, err := b.windowStateResult(context.Background(), "abc")
	if err != nil || state["status"] != "ok" {
		t.Fatal(state, err)
	}
	if len(b.grants) != 0 || len(b.requests) != 0 {
		t.Fatal("metadata created authority")
	}
	if q := <-calls; q["op"] != "window_state" {
		t.Fatal(q)
	}
	_, err = b.input(context.Background(), "a", InputArgs{Window: "abc", Revision: "20,40,600,800", Surface: "popup-handle", SurfaceRevision: "-20,790,200,90", Actions: []Action{{Type: "click", X: 10, Y: 10}}})
	if err == nil {
		t.Fatal("metadata bypassed pause/supervisor loss")
	}
	for len(calls) > 0 {
		if q := <-calls; q["op"] != "window_state" {
			t.Fatal("input without authority", q)
		}
	}
}
func TestSurfaceInputCarriesBindingAndLocalCoordinates(t *testing.T) {
	b, calls := surfaceBrokerFixture(t)
	_, err := b.input(context.Background(), "a", InputArgs{Window: "abc", Revision: "20,40,600,800", Surface: "popup-handle", SurfaceRevision: "-20,790,200,90", Actions: []Action{{Type: "click", X: 10, Y: 10}, {Type: "text", Text: "界"}}})
	if err != nil {
		t.Fatal(err)
	}
	inputs := 0
	for len(calls) > 0 {
		q := <-calls
		if q["op"] == "pointer_transaction" || q["op"] == "text_transaction" {
			inputs++
			if q["surface_id"] != "popup-handle" || q["surface_revision"] != "-20,790,200,90" || q["revision"] != "20,40,600,800" {
				t.Fatal(q)
			}
			if q["op"] == "pointer_transaction" && (q["x"] != float64(10) || q["y"] != float64(10)) {
				t.Fatal("broker applied desktop offsets", q)
			}
		}
	}
	if inputs != 2 {
		t.Fatal(inputs)
	}
}
func TestSurfacePrevalidationNoInput(t *testing.T) {
	for _, tc := range []struct {
		name, id, revision string
		action             Action
	}{
		{"missing_revision", "popup-handle", "", Action{Type: "click", X: 1, Y: 1}},
		{"missing_id", "", "-20,790,200,90", Action{Type: "click", X: 1, Y: 1}},
		{"unknown", "other-window-handle", "-20,790,200,90", Action{Type: "click", X: 1, Y: 1}},
		{"stale", "popup-handle", "stale", Action{Type: "click", X: 1, Y: 1}},
		{"outside_popup", "popup-handle", "-20,790,200,90", Action{Type: "click", X: 201, Y: 1}},
		{"negative", "popup-handle", "-20,790,200,90", Action{Type: "click", X: -1, Y: 1}},
		{"focus", "popup-handle", "-20,790,200,90", Action{Type: "focus"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, calls := surfaceBrokerFixture(t)
			_, err := b.input(context.Background(), "a", InputArgs{Window: "abc", Revision: "20,40,600,800", Surface: tc.id, SurfaceRevision: tc.revision, Actions: []Action{{Type: "key", Key: "ENTER"}, tc.action}})
			if err == nil {
				t.Fatal("invalid surface accepted")
			}
			for len(calls) > 0 {
				if q := <-calls; q["op"] != "window_state" {
					t.Fatal("effects before full validation", q)
				}
			}
		})
	}
}
func TestClosedSurfaceDoesNotFallBack(t *testing.T) {
	b, calls := transactionFixture(t, func(q map[string]any) map[string]any {
		if q["op"] == "window_state" {
			return map[string]any{"ok": true, "state": surfaceStateFixture()}
		}
		if q["op"] == "pointer_transaction" {
			return map[string]any{"ok": false, "error": "surface_unavailable_rediscover"}
		}
		return safeStatus()
	})
	_, err := b.input(context.Background(), "a", InputArgs{Window: "abc", Revision: "20,40,600,800", Surface: "popup-handle", SurfaceRevision: "-20,790,200,90", Actions: []Action{{Type: "click", X: 1, Y: 1}, {Type: "key", Key: "ENTER"}}})
	var failure *InputFailure
	if !errors.As(err, &failure) || failure.CompletedActions != 0 || !strings.Contains(err.Error(), "surface_unavailable") {
		t.Fatal(err)
	}
	for len(calls) > 0 {
		q := <-calls
		if q["op"] == "key_transaction" {
			t.Fatal("continued after surface loss")
		}
	}
}
func TestDialogDoesNotInheritRootGrant(t *testing.T) {
	b, _ := surfaceBrokerFixture(t)
	b.mode = "approve"
	b.grants["root"] = &Grant{ID: "root", Client: "a", Capability: "control", Scope: Scope{"window", "abc"}, Expires: b.now().Add(time.Minute)}
	if _, err := b.windowStateResult(context.Background(), "abc"); err != nil {
		t.Fatal(err)
	}
	path := os.Getenv("CU_TEST_WINDOWS")
	if err := os.WriteFile(path, []byte(`[{"stableId":"dialog","mapped":true,"visible":true,"at":[1,2],"size":[300,200],"workspace":{"id":1}}]`), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := b.input(context.Background(), "a", InputArgs{Window: "dialog", Revision: "1,2,300,200", Actions: []Action{{Type: "key", Key: "ENTER"}}})
	if err != nil || result["status"] != "approval_required" || len(b.grants) != 1 {
		t.Fatal(result, err)
	}
}
func TestPostStateMCPContract(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "state", true: "state_failed"}[fail], func(t *testing.T) {
			b, _ := transactionFixture(t, func(q map[string]any) map[string]any {
				if q["op"] == "window_state" {
					if fail {
						return map[string]any{"ok": false, "error": "target_closed"}
					}
					return map[string]any{"ok": true, "state": surfaceStateFixture()}
				}
				return safeStatus()
			})
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			st, ct := mcp.NewInMemoryTransports()
			server, err := b.newMCPServer(ctx, "a", nil).Connect(ctx, st, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer server.Close()
			client, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, ct, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			result, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "input_window", Arguments: map[string]any{"window_id": "abc", "revision": "20,40,600,800", "then": "state", "actions": []Action{{Type: "key", Key: "ENTER"}}}})
			if err != nil || result.IsError {
				t.Fatal(result, err)
			}
			encoded, _ := json.Marshal(result.StructuredContent)
			var meta map[string]any
			if err := json.Unmarshal(encoded, &meta); err != nil {
				t.Fatal(err)
			}
			if meta["status"] != "completed" {
				t.Fatal(meta)
			}
			want := "ok"
			if fail {
				want = "failed"
			}
			if meta["window_state"].(map[string]any)["status"] != want {
				t.Fatal(meta)
			}
		})
	}
}
func TestWindowStateRequiresCompatibleGuard(t *testing.T) {
	b, _ := inputTransactionFixture(t, "")
	if _, err := b.backend.windowState(context.Background(), "abc"); err == nil || !strings.Contains(err.Error(), "setup") {
		t.Fatal(err)
	}
}

func TestSurfaceSelectorDoesNotCrossConnectionGrant(t *testing.T) {
	b, calls := surfaceBrokerFixture(t)
	b.mode = "approve"
	b.grants["root"] = &Grant{ID: "root", Client: "a", Capability: "control", Scope: Scope{"window", "abc"}, Expires: b.now().Add(time.Minute)}
	result, err := b.input(context.Background(), "b", InputArgs{Window: "abc", Revision: "20,40,600,800", Surface: "popup-handle", SurfaceRevision: "-20,790,200,90", Actions: []Action{{Type: "click", X: 1, Y: 1}}})
	if err != nil || result["status"] != "approval_required" || len(b.grants) != 1 {
		t.Fatal(result, err)
	}
	for len(calls) > 0 {
		if q := <-calls; q["op"] != "window_state" {
			t.Fatal("input under another connection's grant", q)
		}
	}
}

func TestMalformedWindowStateRefused(t *testing.T) {
	for _, change := range []func(*WindowSurfaceState){
		func(s *WindowSurfaceState) { s.Window = "other" },
		func(s *WindowSurfaceState) { s.Surfaces[1].ID = s.Surfaces[0].ID },
		func(s *WindowSurfaceState) { s.Surfaces[1].Size[0] = 0 },
		func(s *WindowSurfaceState) { s.Related[0].SeparateGrantRequired = false },
	} {
		b, _ := transactionFixture(t, func(map[string]any) map[string]any {
			s := surfaceStateFixture()
			change(&s)
			return map[string]any{"ok": true, "state": s}
		})
		if _, err := b.backend.windowState(context.Background(), "abc"); err == nil {
			t.Fatal("malformed state accepted")
		}
	}
}
