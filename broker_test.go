package main

import (
	"context"
	"encoding/json"
	"math"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func fixture() (*Broker, *time.Time) {
	now := time.Now()
	b := newBroker(nil)
	b.uiCount = 1
	b.now = func() time.Time { return now }
	return b, &now
}
func TestDefaultDeny(t *testing.T) {
	b, _ := fixture()
	g, id, e := b.permit("a", "observe", Scope{"workspace", "1"}, nil, "test")
	if e != nil || id == "" || g != nil {
		t.Fatalf("%v %q %v", g, id, e)
	}
	if _, ok := b.allowedLocked("a", "observe", Scope{"workspace", "1"}, nil); ok {
		t.Fatal("request granted itself")
	}
}
func TestRequestsDeduplicate(t *testing.T) {
	b, _ := fixture()
	_, a, _ := b.permit("a", "observe", Scope{"workspace", "1"}, nil, "first")
	_, c, _ := b.permit("a", "observe", Scope{"workspace", "1"}, nil, "second")
	if a != c || len(b.requests) != 1 {
		t.Fatal("duplicate request")
	}
}
func TestLocalGrantScopeAndExpiry(t *testing.T) {
	b, now := fixture()
	_, id, _ := b.permit("a", "observe", Scope{"workspace", "1"}, nil, "")
	if e := b.decide(id, 60, true); e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		client string
		scope  Scope
		want   bool
	}{{"a", Scope{"workspace", "1"}, true}, {"b", Scope{"workspace", "1"}, false}, {"a", Scope{"workspace", "2"}, false}} {
		_, ok := b.allowedLocked(tc.client, "observe", tc.scope, nil)
		if ok != tc.want {
			t.Fatalf("%+v: %v", tc, ok)
		}
	}
	*now = now.Add(time.Minute)
	if _, ok := b.allowedLocked("a", "observe", Scope{"workspace", "1"}, nil); ok {
		t.Fatal("expired grant accepted")
	}
}
func TestWorkspaceObservationFollowsMembership(t *testing.T) {
	g := &Grant{Client: "a", Capability: "observe", Scope: Scope{"workspace", "1"}}
	w := Window{ID: "abc"}
	w.Workspace.ID = 1
	if !grantMatches(g, "a", "observe", Scope{"window", "abc"}, &w) {
		t.Fatal("new member not observable")
	}
	w.Workspace.ID = 2
	if grantMatches(g, "a", "observe", Scope{"window", "abc"}, &w) {
		t.Fatal("window leaving workspace retained authority")
	}
}
func TestCapabilitiesDoNotExpand(t *testing.T) {
	for _, tc := range []struct {
		grant, want string
		ok          bool
	}{{"observe", "control", false}, {"observe", "record", false}, {"control", "observe", true}, {"control", "record", false}, {"record", "control", false}, {"record", "observe", true}} {
		g := &Grant{Client: "a", Capability: tc.grant, Scope: Scope{"window", "abc"}}
		if ok := grantMatches(g, "a", tc.want, Scope{"window", "abc"}, nil); ok != tc.ok {
			t.Fatalf("%+v", tc)
		}
		if grantMatches(g, "a", tc.want, Scope{"window", "def"}, nil) {
			t.Fatal("window grant widened")
		}
	}
}
func TestDenyIsNotGrant(t *testing.T) {
	b, _ := fixture()
	_, id, _ := b.permit("a", "observe", Scope{"workspace", "1"}, nil, "")
	if e := b.decide(id, 60, false); e != nil {
		t.Fatal(e)
	}
	if len(b.grants) != 0 || b.requests[id].State != "denied" {
		t.Fatal("denial failed")
	}
	if b.decide(id, 60, true) == nil {
		t.Fatal("denied request resurrected")
	}
}
func TestGrantDurationBounds(t *testing.T) {
	for _, seconds := range []int{-1, 0, 3601} {
		b, _ := fixture()
		_, id, _ := b.permit("a", "observe", Scope{"workspace", "1"}, nil, "")
		if b.decide(id, seconds, true) == nil {
			t.Fatalf("accepted %d", seconds)
		}
	}
}
func TestPauseAndUIRequiredEvenInYolo(t *testing.T) {
	b, _ := fixture()
	b.mode = "yolo"
	if _, ok := b.allowedLocked("a", "control", Scope{"window", "abc"}, nil); !ok {
		t.Fatal("yolo denied")
	}
	b.paused = true
	if _, ok := b.allowedLocked("a", "control", Scope{"window", "abc"}, nil); ok {
		t.Fatal("paused yolo accepted")
	}
	b.paused = false
	b.uiCount = 0
	if _, _, e := b.permit("a", "observe", Scope{"workspace", "1"}, nil, ""); e == nil {
		t.Fatal("missing supervisor accepted")
	}
}
func TestRevokeAndDisconnect(t *testing.T) {
	b, _ := fixture()
	for _, client := range []string{"a", "b"} {
		_, id, _ := b.permit(client, "observe", Scope{"workspace", "1"}, nil, "")
		if e := b.decide(id, 60, true); e != nil {
			t.Fatal(e)
		}
	}
	b.disconnect("a")
	if len(b.grants) != 1 {
		t.Fatal("wrong grants revoked")
	}
	if _, ok := b.allowedLocked("b", "observe", Scope{"workspace", "1"}, nil); !ok {
		t.Fatal("other client revoked")
	}
	b.mu.Lock()
	b.clearLocked()
	b.mu.Unlock()
	if len(b.grants) != 0 {
		t.Fatal("clear failed")
	}
}
func TestClearCancelsRecordings(t *testing.T) {
	b, _ := fixture()
	ctx, cancel := context.WithCancel(context.Background())
	b.recordings["r"] = &Recording{cancel: cancel}
	b.clearLocked()
	if ctx.Err() == nil {
		t.Fatal("recorder not cancelled")
	}
}
func TestScopeValidation(t *testing.T) {
	for _, tc := range []struct {
		cap string
		s   Scope
	}{{"approve", Scope{"window", "a"}}, {"control", Scope{"workspace", "1"}}, {"record", Scope{"workspace", "1"}}, {"observe", Scope{"all", "*"}}, {"launch", Scope{"window", "a"}}, {"observe", Scope{"window", ""}}} {
		if validScope(tc.cap, tc.s) == nil {
			t.Fatalf("accepted %+v", tc)
		}
	}
}
func TestKeys(t *testing.T) {
	for r := rune(32); r <= 126; r++ {
		if _, _, e := runeKey(r); e != nil {
			t.Fatal(e)
		}
	}
	if _, _, e := runeKey('א'); e == nil {
		t.Fatal("silently accepted unsupported layout")
	}
	k, m, e := keySpec("CTRL+SHIFT+L")
	if e != nil || k != 38 || m != 5 {
		t.Fatalf("%v %v %v", k, m, e)
	}
	if _, _, e := keySpec("SUPER+Q"); e == nil {
		t.Fatal("global shortcut allowed")
	}
}
func TestPointerBounds(t *testing.T) {
	size := [2]int{100, 100}
	for _, a := range []Action{{Type: "click", X: -1}, {Type: "move", X: 100}, {Type: "drag", X: 1, Y: 1, ToX: 101}, {Type: "click", X: math.NaN()}, {Type: "scroll", X: 1, Y: 1, Delta: math.Inf(1)}} {
		if validatePointer(a, size) == nil {
			t.Fatalf("accepted %+v", a)
		}
	}
	if e := validatePointer(Action{Type: "drag", X: 0, Y: 0, ToX: 99, ToY: 99}, size); e != nil {
		t.Fatal(e)
	}
}
func TestSocketPermissionsAndAlreadyRunning(t *testing.T) {
	p := filepath.Join(t.TempDir(), "m.sock")
	l, e := listenUnix(p)
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	info, e := os.Stat(p)
	if e != nil || info.Mode().Perm() != 0600 {
		t.Fatal("socket not private")
	}
	if l2, e := listenUnix(p); e == nil {
		l2.Close()
		t.Fatal("replaced live socket")
	}
}
func TestInvalidDecisions(t *testing.T) {
	b, _ := fixture()
	if b.decide("unknown", 60, true) == nil {
		t.Fatal("unknown approved")
	}
	_, id, _ := b.permit("a", "observe", Scope{"workspace", "1"}, nil, "")
	b.paused = true
	if b.decide(id, 60, true) == nil {
		t.Fatal("approved while paused")
	}
}

func TestSupervisorDisconnectRevokes(t *testing.T) {
	b := newBroker(nil)
	b.grants["g"] = &Grant{ID: "g", Client: "a", Capability: "observe", Scope: Scope{"workspace", "1"}, Expires: time.Now().Add(time.Hour)}
	server, client := net.Pipe()
	done := make(chan struct{})
	go func() { b.handleUI(server); close(done) }()
	if e := json.NewEncoder(client).Encode(map[string]string{"op": "state"}); e != nil {
		t.Fatal(e)
	}
	var state UIState
	if e := json.NewDecoder(client).Decode(&state); e != nil {
		t.Fatal(e)
	}
	client.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("UI disconnect did not finish")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.paused || len(b.grants) != 0 || b.uiCount != 0 {
		t.Fatal("UI loss did not fail closed")
	}
}
func TestSocketDoesNotReplaceRegularFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "m.sock")
	if e := os.WriteFile(p, []byte("keep"), 0600); e != nil {
		t.Fatal(e)
	}
	if l, e := listenUnix(p); e == nil {
		l.Close()
		t.Fatal("replaced regular file")
	}
	v, _ := os.ReadFile(p)
	if string(v) != "keep" {
		t.Fatal("modified existing file")
	}
}
