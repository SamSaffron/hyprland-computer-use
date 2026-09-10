package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestExtendedPointerContract(t *testing.T) {
	cases := []string{
		`{"type":"click","x":20,"y":30,"modifiers":["CTRL"],"click_count":2}`,
		`{"type":"scroll","x":20,"y":30,"delta_x":3,"delta_y":0,"unit":"wheel_steps"}`,
		`{"type":"drag","path":[{"x":20,"y":30},{"x":50,"y":60},{"x":90,"y":30}]}`,
	}
	for _, raw := range cases {
		t.Run(raw, func(t *testing.T) {
			var actions []Action
			if err := json.Unmarshal([]byte("["+raw+"]"), &actions); err != nil {
				t.Fatal(err)
			}
			b, calls := inputTransactionFixture(t, "")
			_, err := b.input(context.Background(), "a", InputArgs{Window: "abc", Revision: "20,40,600,800", Actions: actions})
			if err != nil {
				t.Fatal(err)
			}
			var found map[string]any
			for len(calls) > 0 {
				q := <-calls
				if q["op"] == "pointer_transaction" {
					found = q
				}
			}
			data, _ := json.Marshal(found)
			if strings.Contains(raw, "modifiers") && !strings.Contains(string(data), `"mods":4`) {
				t.Fatalf("modifier lost: %s", data)
			}
			if strings.Contains(raw, "delta_x") && !strings.Contains(string(data), `"scroll_x":3`) {
				t.Fatalf("horizontal scroll lost: %s", data)
			}
			if strings.Contains(raw, "path") && !strings.Contains(string(data), `"path":[`) {
				t.Fatalf("path lost: %s", data)
			}
		})
	}
}

func TestActionShapeRefusals(t *testing.T) {
	for _, raw := range []string{
		`{"type":"drag","path":[]}`, `{"type":"click","x":0,"y":0,"click_count":0}`, `{"type":"click","y":1}`, `{"type":"click","x":null,"y":1}`,
		`{"type":"click","x":1,"y":1,"key":"CTRL"}`,
		`{"type":"click","x":1,"y":1,"unknown":true}`,
		`{"type":"drag","x":0,"y":0,"to_x":1}`,
		`{"type":"drag","path":[{"x":0},{"x":2,"y":2}]}`,
		`{"type":"drag","path":[{"x":0,"y":0},{"x":2,"y":2}],"x":0}`,
		`{"type":"scroll","x":0,"y":0}`, `{"type":"scroll","x":0,"y":0,"unit":"wheel_steps"}`,
		`{"type":"scroll","x":0,"y":0,"delta":0,"delta_x":1,"unit":"wheel_steps"}`,
		`{"type":"key"}`, `{"type":"text","text":null}`, `{"type":"focus","x":0}`,
	} {
		var a Action
		if err := json.Unmarshal([]byte(raw), &a); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	var a Action
	if err := json.Unmarshal([]byte(`{"type":"click","x":0,"y":0}`), &a); err != nil {
		t.Fatal("explicit origin must work", err)
	}
}

func TestExtendedPrevalidationNoEffects(t *testing.T) {
	for _, bad := range []Action{
		{Type: "click", X: 1, Y: 1, Modifiers: []string{"SUPER"}},
		{Type: "click", X: 1, Y: 1, Modifiers: []string{"CTRL", "CONTROL"}},
		{Type: "click", X: 1, Y: 1, ClickCount: 4},
		{Type: "drag", Path: []Point{{1, 1}}},
		{Type: "drag", Path: []Point{{1, 1}, {600, 1}}},
		{Type: "drag", Path: make([]Point, 65)},
		{Type: "scroll", Unit: "wheel_steps", DeltaX: .5},
		{Type: "scroll", Unit: "wheel_steps", DeltaX: 101},
		{Type: "scroll", Unit: "logical_pixels", DeltaY: 1201},
		{Type: "scroll", Unit: "pixels", DeltaY: 1},
		{Type: "scroll", Unit: "wheel_steps", DeltaY: 1, Delta: 1},
		{Type: "drag", X: 1, Y: 1, ToX: 2, ToY: 2, DurationMS: 1},
	} {
		t.Run(bad.Type, func(t *testing.T) {
			b, calls := inputTransactionFixture(t, "")
			_, err := b.input(context.Background(), "a", InputArgs{Window: "abc", Revision: "20,40,600,800", Actions: []Action{{Type: "key", Key: "ENTER"}, bad}})
			if err == nil || len(calls) != 0 {
				t.Fatalf("bad action had effects: %+v %v (%d guard calls)", bad, err, len(calls))
			}
		})
	}
}

func TestExtendedGuardFeatureRequiredBeforeAnyInput(t *testing.T) {
	b, calls := transactionFixture(t, func(q map[string]any) map[string]any {
		r := safeStatus()
		delete(r, "pointer_actions_version")
		return r
	})
	_, err := b.input(context.Background(), "a", InputArgs{Window: "abc", Revision: "20,40,600,800", Actions: []Action{{Type: "key", Key: "ENTER"}, {Type: "click", X: 1, Y: 1, ClickCount: 2}}})
	if err == nil || !strings.Contains(err.Error(), "extended_pointer_guard_unavailable") {
		t.Fatal(err)
	}
	for len(calls) > 0 {
		q := <-calls
		if q["op"] != "status" {
			t.Fatal("old guard caused effects", q)
		}
	}
}
