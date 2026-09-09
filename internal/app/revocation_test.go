package app

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestRevocationFailureRemainsVisibleUntilRetryConfirmed(t *testing.T) {
	for _, op := range []string{"revoke", "clear"} {
		t.Run(op, func(t *testing.T) {
			var failing atomic.Bool
			failing.Store(true)
			b, calls := transactionFixture(t, func(q map[string]any) map[string]any {
				if failing.Load() {
					return map[string]any{"ok": false, "error": "plugin_unavailable"}
				}
				return safeStatus()
			})
			b.grants["lease"] = &Grant{ID: "lease", Client: "a", Capability: "control", Scope: Scope{"window", "abc"}, Expires: b.now().Add(time.Minute)}
			if op == "clear" {
				b.clear()
			} else {
				b.revoke("lease")
			}
			if len(b.grants) != 0 || !b.state("").RevocationUnconfirmed {
				t.Fatal("cleanup failure hidden or local grant retained")
			}
			if _, ok := b.allowedLocked("a", "control", Scope{"window", "abc"}, nil); ok {
				t.Fatal("YOLO bypassed unconfirmed revocation")
			}
			if b.audit[len(b.audit)-1].Event != "revocation_unconfirmed" {
				t.Fatal(b.audit)
			}
			b.retryRevocations()
			if len(calls) != 1 {
				t.Fatal("retry did not respect backoff")
			}
			failing.Store(false)
			b.nextRevocationRetry = b.now()
			b.retryRevocations()
			if len(calls) != 2 || b.state("").RevocationUnconfirmed {
				t.Fatal("successful retry did not confirm cleanup")
			}
			if b.audit[len(b.audit)-1].Event != "revocation_confirmed" {
				t.Fatal(b.audit)
			}
		})
	}
}

func TestClearConfirmsAllPendingTokens(t *testing.T) {
	b, _ := inputTransactionFixture(t, "")
	b.pendingRevocations = map[string]map[string]any{
		"revoke:a": {"op": "revoke", "token": "a"},
		"revoke:b": {"op": "revoke", "token": "b"},
	}
	b.cleanupGuard(context.Background(), map[string]any{"op": "clear"})
	if len(b.pendingRevocations) != 0 {
		t.Fatal("clear left stale warnings")
	}
}

func TestUnconfirmedRevocationPreventsGrantAndSharing(t *testing.T) {
	b, _, _ := shareFixture(t)
	_, id, err := b.permit("a", "observe", Scope{"workspace", "1"}, nil, "test")
	if err != nil {
		t.Fatal(err)
	}
	b.pendingRevocations = map[string]map[string]any{"clear:": {"op": "clear"}}
	if _, _, err := b.permit("a", "observe", Scope{"workspace", "1"}, nil, "test"); err == nil {
		t.Fatal("permissioned activity allowed while cleanup pending")
	}
	if b.decide(id, 60, true) == nil {
		t.Fatal("granted while cleanup pending")
	}
	if b.beginShare("a", "control", 60) == nil {
		t.Fatal("sharing allowed while cleanup pending")
	}
}
