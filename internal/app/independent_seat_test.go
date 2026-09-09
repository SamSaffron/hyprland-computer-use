package app

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestAgentSeatFailureDoesNotPromiseRestart(t *testing.T) {
	for _, op := range []string{"key_transaction", "text_transaction"} {
		t.Run(op, func(t *testing.T) {
			b, _ := transactionFixture(t, func(map[string]any) map[string]any {
				return map[string]any{"ok": false, "error": "application_did_not_bind_agent_seat_restart_application", "completed_characters": 0}
			})
			q := map[string]any{"op": op}
			if op == "text_transaction" {
				q["scalars"] = []rune("echo")
			}
			err := b.backend.guard(context.Background(), q)
			if err == nil || strings.Contains(err.Error(), "restart_application") || !strings.Contains(err.Error(), "not a guaranteed fix") {
				t.Fatal(err)
			}
		})
	}
}

func TestIndependentSeatStatusDoesNotGrantAuthority(t *testing.T) {
	for _, mode := range []string{"focus-borrowing", "independent-seat", "automatic"} {
		enabled := mode != "focus-borrowing"
		b, _ := fixture()
		b.backend = &Desktop{}
		b.backend.independentSeat.Store(enabled)
		b.backend.automaticFallback.Store(mode == "automatic")
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		st, ct := mcp.NewInMemoryTransports()
		server, err := b.newMCPServer(ctx, "a", nil).Connect(ctx, st, nil)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		client, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, ct, nil)
		if err != nil {
			server.Close()
			cancel()
			t.Fatal(err)
		}
		result, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "computer_status", Arguments: map[string]any{}})
		if err != nil || result.IsError {
			t.Fatal(result, err)
		}
		meta := toolMetadata(t, result)
		want := "focus-borrowing"
		if enabled {
			want = mode
		}
		if meta["input_mode"] != want || meta["mode"] != "approve" || len(b.grants) != 0 {
			t.Fatal(meta)
		}
		client.Close()
		server.Close()
		cancel()
	}
}

func TestGuardInstanceCannotChangeOrDisappear(t *testing.T) {
	for _, replacement := range []string{"instance-B", ""} {
		t.Run("replacement="+replacement, func(t *testing.T) {
			count := 0
			b, calls := transactionFixture(t, func(q map[string]any) map[string]any {
				count++
				instance := "instance-A"
				if count > 1 {
					instance = replacement
				}
				return map[string]any{"ok": true, "version": 3, "independent_seat": true, "focus_preserving": true, "instance": instance}
			})
			if err := b.backend.guard(context.Background(), map[string]any{"op": "status"}); err != nil {
				t.Fatal(err)
			}
			<-calls
			err := b.backend.guard(context.Background(), map[string]any{"op": "clear"})
			if err == nil || !strings.Contains(err.Error(), "instance_changed") {
				t.Fatal(err)
			}
			if q := <-calls; q["instance"] != "instance-A" {
				t.Fatal("request not bound to original guard", q)
			}
			if p := b.backend.guardInstance.Load(); p == nil || *p != "instance-A" {
				t.Fatal("guard identity was replaced")
			}
		})
	}
}

func TestIndependentSeatProtocolNegotiation(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		version                 int
		seat, preserving, valid bool
	}{
		{"legacy", 2, false, true, true}, {"independent", 3, true, true, true},
		{"missing-capability", 3, false, true, false}, {"wrong-version", 2, true, true, false},
		{"missing-instance", 3, true, true, false},
		{"future-version", 4, true, true, false}, {"not-preserving", 3, true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, err := os.MkdirTemp("", "cu-seat-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(dir)
			l, err := net.Listen("unix", filepath.Join(dir, "guard.sock"))
			if err != nil {
				t.Fatal(err)
			}
			defer l.Close()
			done := make(chan struct{})
			go func() {
				defer close(done)
				c, err := l.Accept()
				if err != nil {
					return
				}
				defer c.Close()
				var q any
				json.NewDecoder(c).Decode(&q)
				reply := map[string]any{"ok": true, "version": tc.version, "independent_seat": tc.seat, "focus_preserving": tc.preserving, "instance": "test-instance"}
				if tc.name == "missing-instance" {
					delete(reply, "instance")
				}
				json.NewEncoder(c).Encode(reply)
			}()
			d := &Desktop{Dir: dir}
			err = d.guard(context.Background(), map[string]any{"op": "status"})
			<-done
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
			if d.independentSeat.Load() != (tc.valid && tc.seat) {
				t.Fatal("invalid independent-seat selection")
			}
		})
	}
}
