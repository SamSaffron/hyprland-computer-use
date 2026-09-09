package app

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Scope struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}
type Grant struct {
	ID         string    `json:"id"`
	Client     string    `json:"client"`
	Capability string    `json:"capability"`
	Scope      Scope     `json:"scope"`
	Label      string    `json:"label"`
	Expires    time.Time `json:"expires"`
	Remaining  int       `json:"remaining_seconds"`
}
type Request struct {
	ID         string    `json:"id"`
	Client     string    `json:"client"`
	Capability string    `json:"capability"`
	Scope      Scope     `json:"scope"`
	Label      string    `json:"label"`
	Reason     string    `json:"reason"`
	State      string    `json:"state"`
	Created    time.Time `json:"created"`
}
type Audit struct {
	Time   time.Time `json:"time"`
	Event  string    `json:"event"`
	Detail string    `json:"detail"`
}
type Marker struct {
	Window    Window `json:"window"`
	Remaining int    `json:"remaining_seconds"`
	Label     string `json:"label"`
	InputMode string `json:"input_mode"`
}
type UIState struct {
	TrayAnchor       *TrayAnchor       `json:"tray_anchor,omitempty"`
	OAuthEnabled     bool              `json:"oauth_enabled"`
	OAuthPending     []OAuthPending    `json:"oauth_pending"`
	OAuthConnections []OAuthConnection `json:"oauth_connections"`
	Clients          []UIClient        `json:"clients"`
	Picker           *SharePicker      `json:"picker,omitempty"`
	Targets          []Marker          `json:"targets"`
	Mode             string            `json:"mode"`
	Paused           bool              `json:"paused"`
	Connected        bool              `json:"connected"`
	Requests         []Request         `json:"requests"`
	Grants           []Grant           `json:"grants"`
	Audit            []Audit           `json:"audit"`
	Recordings       []RecordingInfo   `json:"recordings"`
	Open             uint64            `json:"open"`
	Backend          string            `json:"backend"`
	Now              time.Time         `json:"now"`
	Error            string            `json:"error,omitempty"`
}
type Broker struct {
	trayAnchor      *TrayAnchor
	oauth           *OAuthProvider
	picker          *SharePicker
	mu              sync.Mutex
	lastInputWindow string
	lastInputUntil  time.Time
	inputMu         sync.Mutex
	mode            string
	paused          bool
	uiCount         int
	open            uint64
	clients         map[string]string
	requests        map[string]*Request
	grants          map[string]*Grant
	audit           []Audit
	backend         *Desktop
	recordings      map[string]*Recording
	now             func() time.Time
	log             *os.File
}

func randomID() string {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b[:])
}
func newBroker(d *Desktop) *Broker {
	return &Broker{mode: "approve", clients: map[string]string{}, requests: map[string]*Request{}, grants: map[string]*Grant{}, backend: d, recordings: map[string]*Recording{}, now: time.Now}
}
func (b *Broker) noteLocked(event, detail string) {
	a := Audit{b.now(), event, detail}
	b.audit = append(b.audit, a)
	if len(b.audit) > 100 {
		b.audit = b.audit[len(b.audit)-100:]
	}
	if b.log != nil {
		_ = json.NewEncoder(b.log).Encode(a)
	}
}
func validScope(cap string, s Scope) error {
	if len(s.ID) == 0 || len(s.ID) > 80 {
		return errors.New("invalid scope ID")
	}
	switch cap {
	case "observe":
		if s.Kind != "workspace" && s.Kind != "window" {
			return errors.New("observe requires workspace or window")
		}
	case "control", "record":
		if s.Kind != "window" {
			return errors.New("control/record require one explicit window")
		}
	case "launch":
		if s.Kind != "application" {
			return errors.New("launch requires application")
		}
	default:
		return errors.New("unknown capability")
	}
	return nil
}
func grantMatches(g *Grant, client, cap string, scope Scope, w *Window) bool {
	if g.Client != client {
		return false
	}
	if g.Capability != cap && !(cap == "observe" && (g.Capability == "control" || g.Capability == "record")) {
		return false
	}
	if g.Scope == scope {
		return true
	}
	return cap == "observe" && scope.Kind == "window" && g.Scope.Kind == "workspace" && w != nil && fmt.Sprint(w.Workspace.ID) == g.Scope.ID
}
func (b *Broker) allowedLocked(client, cap string, s Scope, w *Window) (*Grant, bool) {
	if b.paused || b.uiCount == 0 {
		return nil, false
	}
	if b.mode == "yolo" {
		return nil, true
	}
	for _, g := range b.grants {
		if b.now().Before(g.Expires) && grantMatches(g, client, cap, s, w) {
			return g, true
		}
	}
	return nil, false
}
func (b *Broker) permit(client, cap string, s Scope, w *Window, reason string) (*Grant, string, error) {
	if e := validScope(cap, s); e != nil {
		return nil, "", e
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.uiCount == 0 {
		return nil, "", errors.New("supervisor_unavailable: start the local Quickshell console")
	}
	if b.paused {
		return nil, "", errors.New("paused_by_user")
	}
	if g, ok := b.allowedLocked(client, cap, s, w); ok {
		return g, "", nil
	}
	for _, r := range b.requests {
		if r.Client == client && r.Capability == cap && r.Scope == s && r.State == "pending" {
			return nil, r.ID, nil
		}
	}
	if len(b.requests) >= 100 {
		return nil, "", errors.New("too_many_requests")
	}
	if len(reason) > 400 {
		reason = reason[:400]
	}
	label := cap + " " + s.Kind + " " + s.ID
	if w != nil {
		label = cap + " window: " + w.Title + " (" + w.Class + ")"
	}
	if s.Kind == "workspace" {
		label = "See workspace " + s.ID + " — including new windows"
	}
	if cap == "control" {
		label += " · viewing + input; no recording"
		if w != nil && (strings.Contains(strings.ToLower(w.Class), "kitty") || strings.Contains(strings.ToLower(w.Class), "terminal") || w.Class == "foot" || strings.Contains(strings.ToLower(w.Class), "alacritty")) {
			label += " · WARNING: terminal control is shell authority"
		}
	}
	id := randomID()
	b.requests[id] = &Request{id, client, cap, s, label, reason, "pending", b.now()}
	b.open++
	b.noteLocked("requested", label)
	return nil, id, nil
}
func (b *Broker) decide(id string, seconds int, approve bool) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	r := b.requests[id]
	if r == nil || r.State != "pending" {
		return errors.New("request is no longer pending")
	}
	if !approve {
		r.State = "denied"
		b.noteLocked("denied", r.Label)
		return nil
	}
	if b.paused || b.uiCount == 0 || b.mode != "approve" {
		return errors.New("cannot grant in current state")
	}
	if seconds < 1 || seconds > 3600 {
		return errors.New("duration must be 1–3600 seconds")
	}
	if r.Capability == "control" || r.Scope.Kind == "window" {
		if _, e := b.backend.window(context.Background(), r.Scope.ID); e != nil {
			return errors.New("target window closed")
		}
	}
	g := &Grant{ID: randomID(), Client: r.Client, Capability: r.Capability, Scope: r.Scope, Label: r.Label, Expires: b.now().Add(time.Duration(seconds) * time.Second)}
	if r.Capability == "control" {
		if e := b.backend.guard(context.Background(), map[string]any{"op": "authorize", "token": g.ID, "window": g.Scope.ID, "milliseconds": seconds * 1000}); e != nil {
			return e
		}
	}
	b.grants[g.ID] = g
	r.State = "granted"
	b.noteLocked("granted", fmt.Sprintf("%s for %ds", r.Label, seconds))
	return nil
}
func (b *Broker) revokeLocked(id string) {
	g := b.grants[id]
	if g == nil {
		return
	}
	delete(b.grants, id)
	if g.Capability == "control" && b.backend != nil {
		_ = b.backend.guard(context.Background(), map[string]any{"op": "revoke", "token": id})
	}
	b.noteLocked("revoked", g.Label)
}
func (b *Broker) clearLocked() {
	b.picker = nil
	for id := range b.grants {
		b.revokeLocked(id)
	}
	if b.backend != nil {
		_ = b.backend.guard(context.Background(), map[string]any{"op": "clear"})
	}
	for _, r := range b.requests {
		if r.State == "pending" {
			r.State = "cancelled"
		}
	}
	for _, r := range b.recordings {
		r.cancel()
	}
}
func (b *Broker) disconnect(client string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for id, g := range b.grants {
		if g.Client == client {
			b.revokeLocked(id)
		}
	}
	for _, r := range b.requests {
		if r.Client == client && r.State == "pending" {
			r.State = "cancelled"
		}
	}
	for _, r := range b.recordings {
		if r.Info.Client == client {
			r.cancel()
		}
	}
	delete(b.clients, client)
	if b.picker != nil && b.picker.Client == client {
		b.picker = nil
	}
	b.noteLocked("client_disconnected", client)
}
func (b *Broker) state(err string) UIState {
	b.mu.Lock()
	s := UIState{TrayAnchor: b.trayAnchor, Mode: b.mode, Paused: b.paused, Connected: b.uiCount > 0, Open: b.open, Now: b.now(), Requests: []Request{}, Grants: []Grant{}, Audit: append([]Audit{}, b.audit...), Recordings: []RecordingInfo{}, Backend: "Compositor-scoped input · native toplevel capture", Error: err}
	s.Clients = []UIClient{}
	for id, label := range b.clients {
		s.Clients = append(s.Clients, UIClient{id, label})
	}
	sort.Slice(s.Clients, func(i, j int) bool { return s.Clients[i].ID < s.Clients[j].ID })
	if b.picker != nil && b.now().Before(b.picker.Expires) {
		p := *b.picker
		s.Picker = &p
	}
	for _, r := range b.requests {
		if r.State == "pending" {
			s.Requests = append(s.Requests, *r)
		}
	}
	for _, g := range b.grants {
		v := *g
		v.Remaining = max(0, int(g.Expires.Sub(b.now()).Seconds()))
		s.Grants = append(s.Grants, v)
	}
	for _, r := range b.recordings {
		s.Recordings = append(s.Recordings, r.Info)
	}
	oauth := b.oauth
	lastWindow, lastUntil := b.lastInputWindow, b.lastInputUntil
	b.mu.Unlock()
	s.OAuthPending = []OAuthPending{}
	s.OAuthConnections = []OAuthConnection{}
	if oauth != nil {
		s.OAuthEnabled = true
		s.OAuthPending, s.OAuthConnections = oauth.state()
	}
	sort.Slice(s.Requests, func(i, j int) bool { return s.Requests[i].Created.Before(s.Requests[j].Created) })
	sort.Slice(s.Grants, func(i, j int) bool { return s.Grants[i].Expires.Before(s.Grants[j].Expires) })
	s.Targets = []Marker{}
	wanted := map[string]Marker{}
	if !s.Paused && s.Connected {
		for _, g := range s.Grants {
			if g.Capability == "control" && g.Remaining > 0 {
				wanted[g.Scope.ID] = Marker{Remaining: g.Remaining, Label: "CONTROL GRANTED"}
			}
		}
		if s.Mode == "yolo" && s.Now.Before(lastUntil) {
			wanted[lastWindow] = Marker{Label: "YOLO CONTROL"}
		}
	}
	if len(wanted) > 0 && b.backend != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		ws, e := b.backend.windows(ctx)
		var modes struct {
			OK    bool              `json:"ok"`
			Modes map[string]string `json:"modes"`
		}
		if e == nil && b.backend.automaticFallback.Load() {
			if err := b.backend.guardExchange(ctx, map[string]any{"op": "input_modes"}, &modes); err != nil {
				modes.OK = false
			}
		}
		cancel()
		if e == nil {
			for _, w := range ws {
				if m, ok := wanted[w.ID]; ok && w.Visible {
					m.InputMode = "Fallback"
					if b.backend.independentSeat.Load() {
						m.InputMode = "Seat"
					}
					if b.backend.automaticFallback.Load() {
						m.InputMode = "Unknown"
						if modes.OK && (modes.Modes[w.ID] == "Seat" || modes.Modes[w.ID] == "Fallback") {
							m.InputMode = modes.Modes[w.ID]
						}
					}
					m.Window = w
					s.Targets = append(s.Targets, m)
				}
			}
		}
	}
	return s
}
func (b *Broker) maintenance(ctx context.Context) {
	t := time.NewTicker(100 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			b.mu.Lock()
			if b.picker != nil && !b.now().Before(b.picker.Expires) {
				b.picker = nil
			}
			for id, g := range b.grants {
				if !b.now().Before(g.Expires) {
					b.revokeLocked(id)
				}
			}
			for id, r := range b.requests {
				if b.now().Sub(r.Created) > 5*time.Minute {
					delete(b.requests, id)
				}
			}
			b.mu.Unlock()
		}
	}
}
func (b *Broker) handleUI(conn net.Conn) {
	defer conn.Close()
	b.mu.Lock()
	b.uiCount++
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		b.uiCount--
		if b.uiCount == 0 {
			b.paused = true
			b.clearLocked()
			b.noteLocked("supervisor_disconnected", "all activity stopped")
		}
		b.mu.Unlock()
	}()
	scan := bufio.NewScanner(conn)
	scan.Buffer(make([]byte, 4096), 16384)
	enc := json.NewEncoder(conn)
	for {
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		if !scan.Scan() {
			return
		}
		var q struct {
			Op         string `json:"op"`
			Client     string `json:"client"`
			Capability string `json:"capability"`
			Window     string `json:"window_id"`
			ID         string `json:"id"`
			Seconds    int    `json:"seconds"`
			Mode       string `json:"mode"`
			Paused     bool   `json:"paused"`
		}
		e := json.Unmarshal(scan.Bytes(), &q)
		if e == nil {
			switch q.Op {
			case "state":
			case "oauth_approve", "oauth_deny", "oauth_revoke":
				b.mu.Lock()
				p := b.oauth
				b.mu.Unlock()
				if p == nil {
					e = errors.New("OAuth provider is not enabled")
				} else if q.Op == "oauth_revoke" {
					p.revokeConnection(q.ID)
				} else {
					e = p.decide(q.ID, q.Op == "oauth_approve")
				}
			case "begin_share":
				e = b.beginShare(q.Client, q.Capability, q.Seconds)
			case "select_share":
				e = b.selectShare(q.ID, q.Client, q.Window)
			case "cancel_share":
				b.mu.Lock()
				if b.picker != nil && b.picker.ID == q.ID {
					b.picker = nil
				}
				b.mu.Unlock()
			case "approve":
				e = b.decide(q.ID, q.Seconds, true)
			case "deny":
				e = b.decide(q.ID, 0, false)
			case "revoke":
				b.mu.Lock()
				b.revokeLocked(q.ID)
				b.mu.Unlock()
			case "revoke_all":
				b.mu.Lock()
				b.clearLocked()
				b.mu.Unlock()
			case "pause":
				b.mu.Lock()
				b.paused = q.Paused
				if b.paused {
					b.clearLocked()
				}
				b.noteLocked("pause", fmt.Sprint(q.Paused))
				b.mu.Unlock()
			case "mode":
				if q.Mode != "approve" && q.Mode != "yolo" {
					e = errors.New("invalid mode")
				} else {
					b.mu.Lock()
					b.clearLocked()
					b.mode = q.Mode
					b.noteLocked("mode", q.Mode)
					b.mu.Unlock()
				}
			default:
				e = errors.New("unknown UI command")
			}
		}
		msg := ""
		if e != nil {
			msg = e.Error()
		}
		_ = conn.SetWriteDeadline(time.Now().Add(time.Second))
		if e = enc.Encode(b.state(msg)); e != nil {
			return
		}
	}
}
func listenUnix(path string) (net.Listener, error) {
	if len(path) > 100 {
		return nil, errors.New("Unix socket path too long")
	}
	if info, e := os.Lstat(path); e == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("refusing to replace non-socket: %s", path)
		}
		c, e := net.DialTimeout("unix", path, 200*time.Millisecond)
		if e == nil {
			c.Close()
			return nil, fmt.Errorf("already listening: %s", path)
		}
		if e = os.Remove(path); e != nil {
			return nil, e
		}
	}
	l, e := net.Listen("unix", path)
	if e == nil {
		e = os.Chmod(path, 0600)
	}
	return l, e
}
func runtimeDir() (string, error) {
	r := os.Getenv("XDG_RUNTIME_DIR")
	if r == "" {
		return "", errors.New("XDG_RUNTIME_DIR is required")
	}
	p := filepath.Join(r, "computer-use")
	if e := os.MkdirAll(p, 0700); e != nil {
		return "", e
	}
	info, e := os.Lstat(p)
	if e != nil {
		return "", e
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("unsafe runtime directory")
	}
	return p, os.Chmod(p, 0700)
}
func cleanReason(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 32 && r != '\n' {
			return -1
		}
		return r
	}, s)
}
