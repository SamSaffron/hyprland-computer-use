package app

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func shareFixture(t *testing.T) (*Broker, *time.Time, string) {
	t.Helper()
	dir, e := os.MkdirTemp("", "cu-share-")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	file := filepath.Join(dir, "windows.json")
	if e = os.WriteFile(file, []byte(`[{"stableId":"abc","title":"Target","class":"kitty","mapped":true,"visible":true,"at":[20,40],"size":[600,800],"workspace":{"id":1}},{"stableId":"def","title":"Other","class":"pinta","mapped":true,"visible":true,"at":[650,40],"size":[600,800],"workspace":{"id":1}}]`), 0600); e != nil {
		t.Fatal(e)
	}
	os.WriteFile(filepath.Join(dir, "hyprctl"), []byte("#!/bin/sh\ncat \"$CU_TEST_WINDOWS\"\n"), 0700)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	t.Setenv("CU_TEST_WINDOWS", file)
	l, e := net.Listen("unix", filepath.Join(dir, "guard.sock"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, e := l.Accept()
			if e != nil {
				return
			}
			var q map[string]any
			json.NewDecoder(c).Decode(&q)
			json.NewEncoder(c).Encode(map[string]any{"ok": true})
			c.Close()
		}
	}()
	b, now := fixture()
	b.backend = &Desktop{Dir: dir, Data: dir}
	b.clients["a"] = "Agent A"
	return b, now, file
}
func TestProactiveControlAndRecipientIsolation(t *testing.T) {
	b, _, _ := shareFixture(t)
	if e := b.beginShare("", "control", 300); e != nil {
		t.Fatal(e)
	}
	if len(b.grants) != 0 || len(b.requests) != 0 {
		t.Fatal("picker itself granted or created an agent request")
	}
	id := b.picker.ID
	if e := b.selectShare(id, "a", "abc"); e != nil {
		t.Fatal(e)
	}
	if b.picker != nil || len(b.requests) != 0 {
		t.Fatal("proactive sharing depends on requests")
	}
	for _, tc := range []struct {
		client, cap, window string
		want                bool
	}{{"a", "control", "abc", true}, {"a", "observe", "abc", true}, {"a", "record", "abc", false}, {"a", "control", "def", false}, {"b", "control", "abc", false}} {
		if _, ok := b.allowedLocked(tc.client, tc.cap, Scope{"window", tc.window}, nil); ok != tc.want {
			t.Fatalf("%+v", tc)
		}
	}
	if b.selectShare(id, "a", "abc") == nil {
		t.Fatal("replayed picker accepted")
	}
	b.disconnect("a")
	if len(b.grants) != 0 {
		t.Fatal("disconnect retained share")
	}
}
func TestProactiveViewOnly(t *testing.T) {
	b, _, _ := shareFixture(t)
	if e := b.beginShare("a", "observe", 60); e != nil {
		t.Fatal(e)
	}
	if e := b.selectShare(b.picker.ID, "a", "abc"); e != nil {
		t.Fatal(e)
	}
	if _, ok := b.allowedLocked("a", "control", Scope{"window", "abc"}, nil); ok {
		t.Fatal("view-only permits control")
	}
}
func TestProactiveInvalidSelection(t *testing.T) {
	for _, kind := range []string{"recipient", "window", "expired", "paused", "moved", "closed", "cancelled", "disconnected"} {
		t.Run(kind, func(t *testing.T) {
			b, now, file := shareFixture(t)
			if e := b.beginShare("a", "control", 300); e != nil {
				t.Fatal(e)
			}
			id := b.picker.ID
			client, window := "a", "abc"
			switch kind {
			case "recipient":
				b.clients["b"] = "B"
				client = "b"
			case "window":
				window = "not-offered"
			case "expired":
				*now = now.Add(time.Minute)
			case "paused":
				b.paused = true
			case "moved":
				os.WriteFile(file, []byte(`[{"stableId":"abc","mapped":true,"visible":true,"at":[99,99],"size":[600,800]}]`), 0600)
			case "closed":
				os.WriteFile(file, []byte(`[]`), 0600)
			case "cancelled":
				b.clearLocked()
			case "disconnected":
				b.disconnect("a")
			}
			if b.selectShare(id, client, window) == nil || len(b.grants) > 0 {
				t.Fatal("invalid selection granted")
			}
		})
	}
}
func TestProactiveMultipleClientsRequireChoice(t *testing.T) {
	b, _, _ := shareFixture(t)
	b.clients["b"] = "B"
	if e := b.beginShare("", "control", 300); e != nil {
		t.Fatal(e)
	}
	if b.picker.Client != "" {
		t.Fatal("silently selected a recipient")
	}
	if b.selectShare(b.picker.ID, "", "abc") == nil {
		t.Fatal("empty recipient granted")
	}
	if e := b.selectShare(b.picker.ID, "b", "abc"); e != nil {
		t.Fatal(e)
	}
	if _, ok := b.allowedLocked("a", "control", Scope{"window", "abc"}, nil); ok {
		t.Fatal("shared to all clients")
	}
}
func TestProactiveStartValidation(t *testing.T) {
	b, _, _ := shareFixture(t)
	for _, n := range []int{-1, 0, 3601} {
		if b.beginShare("a", "control", n) == nil {
			t.Fatal("invalid duration")
		}
	}
	for _, cap := range []string{"record", "launch", "yolo"} {
		if b.beginShare("a", cap, 300) == nil {
			t.Fatal("invalid capability")
		}
	}
	if b.beginShare("missing", "control", 300) == nil {
		t.Fatal("unknown recipient")
	}
	b.paused = true
	if b.beginShare("a", "control", 300) == nil {
		t.Fatal("paused sharing")
	}
	b.paused = false
	delete(b.clients, "a")
	if b.beginShare("", "control", 300) == nil {
		t.Fatal("sharing to a future client")
	}
}

func TestMCPMetadataFreeWithoutSupervisorOrGrant(t *testing.T) {
	b, _, _ := shareFixture(t)
	b.paused = true
	b.uiCount = 0
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client, server := net.Pipe()
	defer client.Close()
	done := make(chan struct{})
	go func() { defer close(done); b.serveMCP(ctx, server); server.Close() }()
	defer func() { client.Close(); cancel(); <-done }()
	enc, dec := json.NewEncoder(client), json.NewDecoder(client)
	seq := 0
	rpc := func(method string, params any) map[string]any {
		t.Helper()
		seq++
		client.SetDeadline(time.Now().Add(3 * time.Second))
		if e := enc.Encode(map[string]any{"jsonrpc": "2.0", "id": seq, "method": method, "params": params}); e != nil {
			t.Fatal(e)
		}
		for {
			var r map[string]any
			if e := dec.Decode(&r); e != nil {
				t.Fatal(e)
			}
			if r["id"] == float64(seq) {
				return r
			}
		}
	}
	rpc("initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "test", "version": "1"}})
	enc.Encode(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	r := rpc("tools/call", map[string]any{"name": "list_windows", "arguments": map[string]any{}})
	result, ok := r["result"].(map[string]any)
	if !ok || result["isError"] == true {
		t.Fatalf("free discovery failed: %v", r)
	}
	out := result["structuredContent"].(map[string]any)
	if len(out["windows"].([]any)) != 2 {
		t.Fatalf("%v", out)
	}
	b.mu.Lock()
	n := len(b.requests) + len(b.grants)
	b.mu.Unlock()
	if n != 0 {
		t.Fatal("discovery created authority")
	}
	r = rpc("tools/call", map[string]any{"name": "view_window", "arguments": map[string]any{"window_id": "abc"}})
	if r["result"].(map[string]any)["isError"] != true {
		t.Fatal("free discovery released pixels")
	}
	r = rpc("tools/call", map[string]any{"name": "begin_share", "arguments": map[string]any{}})
	if r["error"] == nil && r["result"].(map[string]any)["isError"] != true {
		t.Fatal("MCP can invoke local sharing")
	}
}
