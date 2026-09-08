package app

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func inputTransactionFixture(t *testing.T, reject string) (*Broker, chan map[string]any) {
	return transactionFixture(t, func(q map[string]any) map[string]any {
		if q["op"] == reject {
			return map[string]any{"ok": false, "error": "input_busy_keys_held"}
		}
		r := safeStatus()
		if q["op"] == "text_transaction" {
			r["completed_characters"] = len(q["scalars"].([]any))
		}
		return r
	})
}

func transactionFixture(t *testing.T, respond func(map[string]any) map[string]any) (*Broker, chan map[string]any) {
	t.Helper()
	dir, err := os.MkdirTemp("", "cu-input-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	windows := filepath.Join(dir, "windows.json")
	if err := os.WriteFile(windows, []byte(`[{"stableId":"abc","title":"Target","class":"kitty","mapped":true,"visible":true,"at":[20,40],"size":[600,800],"workspace":{"id":1}}]`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hyprctl"), []byte("#!/bin/sh\ncat \"$CU_TEST_WINDOWS\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	t.Setenv("CU_TEST_WINDOWS", windows)
	listener, err := net.Listen("unix", filepath.Join(dir, "guard.sock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	calls := make(chan map[string]any, 64)
	go func() {
		for {
			c, err := listener.Accept()
			if err != nil {
				return
			}
			var q map[string]any
			if err := json.NewDecoder(c).Decode(&q); err == nil {
				calls <- q
				json.NewEncoder(c).Encode(respond(q))
			}
			c.Close()
		}
	}()
	b, _ := fixture()
	b.backend = &Desktop{Dir: dir, Data: dir}
	b.mode = "yolo"
	return b, calls
}

func TestInputUsesCompleteTransactions(t *testing.T) {
	b, calls := inputTransactionFixture(t, "")
	_, err := b.input(context.Background(), "a", InputArgs{Window: "abc", Revision: "20,40,600,800", Actions: []Action{
		{Type: "key", Key: "CTRL+L"}, {Type: "text", Text: "Ab"},
		{Type: "click", X: 10, Y: 20}, {Type: "drag", X: 10, Y: 20, ToX: 30, ToY: 40},
		{Type: "move", X: 30, Y: 40}, {Type: "scroll", X: 30, Y: 40, Delta: 5},
	}})
	if err != nil {
		t.Fatal(err)
	}
	var requests []map[string]any
	for len(calls) > 0 {
		requests = append(requests, <-calls)
	}
	want := []string{"status", "authorize", "key_transaction", "text_transaction", "pointer_transaction", "pointer_transaction", "pointer_transaction", "pointer_transaction", "revoke"}
	if len(requests) != len(want) {
		t.Fatalf("unexpected guard calls: %+v", requests)
	}
	for i, q := range requests {
		if q["op"] != want[i] {
			t.Fatalf("request %d: %+v", i, q)
		}
		if _, ok := q["down"]; ok {
			t.Fatal("split key event escaped broker")
		}
		if _, ok := q["button_state"]; ok {
			t.Fatal("split button event escaped broker")
		}
		if i > 1 && i < len(requests)-1 && q["revision"] != "20,40,600,800" {
			t.Fatal("missing geometry guard")
		}
		if i > 0 && q["token"] != requests[1]["token"] {
			t.Fatal("lost lease binding")
		}
	}
	if requests[2]["mods"] != float64(4) || len(requests[3]["scalars"].([]any)) != 2 || requests[3]["scalars"].([]any)[0] != float64(65) || requests[3]["scalars"].([]any)[1] != float64(98) {
		t.Fatal("lost modifiers")
	}
	drag := requests[5]
	if drag["kind"] != "drag" || drag["to_x"] != float64(30) || drag["to_y"] != float64(40) {
		t.Fatal(drag)
	}
}

func TestTimedDragRejectedBeforeAnyInput(t *testing.T) {
	b, calls := inputTransactionFixture(t, "")
	_, err := b.input(context.Background(), "a", InputArgs{Window: "abc", Revision: "20,40,600,800", Actions: []Action{{Type: "key", Key: "ENTER"}, {Type: "drag", X: 10, Y: 20, ToX: 30, ToY: 40, DurationMS: 250}}})
	if err == nil || !strings.Contains(err.Error(), "timed drag") {
		t.Fatal(err)
	}
	if len(calls) != 0 {
		t.Fatal("input/authorization sent before full validation")
	}
}

func TestBusyInputDoesNotFallback(t *testing.T) {
	b, calls := inputTransactionFixture(t, "key_transaction")
	_, err := b.input(context.Background(), "a", InputArgs{Window: "abc", Revision: "20,40,600,800", Actions: []Action{{Type: "key", Key: "ENTER"}, {Type: "click", X: 10, Y: 20}}})
	if err == nil || !strings.Contains(err.Error(), "input_busy_keys_held") {
		t.Fatal(err)
	}
	for len(calls) > 0 {
		q := <-calls
		if q["op"] != "authorize" && q["op"] != "key_transaction" && q["op"] != "revoke" {
			t.Fatalf("fallback/later action after refusal: %v", q)
		}
	}
}

func TestGuardRequiresFocusPreservingProtocol(t *testing.T) {
	for _, version := range []int{0, 1, 2} {
		t.Run(string(rune('0'+version)), func(t *testing.T) {
			dir, err := os.MkdirTemp("", "cu-version-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(dir)
			listener, err := net.Listen("unix", filepath.Join(dir, "guard.sock"))
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			go func() {
				c, err := listener.Accept()
				if err != nil {
					return
				}
				defer c.Close()
				var q any
				json.NewDecoder(c).Decode(&q)
				json.NewEncoder(c).Encode(map[string]any{"ok": true, "version": version, "focus_preserving": version == 2})
			}()
			err = (&Desktop{Dir: dir}).guard(context.Background(), map[string]any{"op": "status"})
			if (err == nil) != (version == 2) {
				t.Fatalf("version %d: %v", version, err)
			}
		})
	}
}
