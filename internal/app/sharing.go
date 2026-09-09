package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"
)

type SharePicker struct {
	ID         string    `json:"id"`
	Client     string    `json:"client"`
	Capability string    `json:"capability"`
	Seconds    int       `json:"seconds"`
	Expires    time.Time `json:"expires"`
	Windows    []Window  `json:"windows"`
}
type UIClient struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

func (b *Broker) beginShare(client, capability string, seconds int) error {
	if capability == "" {
		capability = "control"
	}
	if capability != "observe" && capability != "control" {
		return errors.New("share supports observe or control only")
	}
	if seconds < 1 || seconds > 3600 {
		return errors.New("duration must be 1–3600 seconds")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	ws, e := b.backend.windows(ctx)
	cancel()
	if e != nil {
		return e
	}
	candidates := []Window{}
	for _, w := range ws {
		if w.Visible && !w.Hidden && !w.XWayland && w.Class != "org.quickshell" && w.Class != "computer-use" {
			candidates = append(candidates, w)
		}
	}
	// Oldest focused first: recent windows are drawn above them in the picker.
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].FocusHistory > candidates[j].FocusHistory })
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.paused || b.uiCount == 0 || len(b.pendingRevocations) > 0 {
		return errors.New("resume the local console before sharing")
	}
	if len(b.clients) == 0 {
		return errors.New("connect an MCP client before sharing a window")
	}
	if client != "" {
		if _, ok := b.clients[client]; !ok {
			return errors.New("unknown MCP client")
		}
	} else if len(b.clients) == 1 {
		for id := range b.clients {
			client = id
		}
	}
	if len(candidates) == 0 {
		return errors.New("no visible native windows to share")
	}
	id, e := randomID()
	if e != nil {
		return e
	}
	b.picker = &SharePicker{id, client, capability, seconds, b.now().Add(time.Minute), candidates}
	b.noteLocked("share_picker", "local user started window selection")
	return nil
}

func (b *Broker) selectShare(pickerID, client, windowID string) error {
	b.mu.Lock()
	p := b.picker
	if p == nil || p.ID != pickerID || !b.now().Before(p.Expires) {
		b.mu.Unlock()
		return errors.New("window picker expired or cancelled")
	}
	if b.paused || b.uiCount == 0 || len(b.pendingRevocations) > 0 {
		b.mu.Unlock()
		return errors.New("sharing is paused")
	}
	if p.Client != "" && p.Client != client {
		b.mu.Unlock()
		return errors.New("picker recipient changed")
	}
	if _, ok := b.clients[client]; !ok {
		b.mu.Unlock()
		return errors.New("select a connected MCP client")
	}
	var selected *Window
	for i := range p.Windows {
		if p.Windows[i].ID == windowID {
			copy := p.Windows[i]
			selected = &copy
			break
		}
	}
	if selected == nil {
		b.mu.Unlock()
		return errors.New("window was not offered by this picker")
	}
	authority := b.authority
	expires := b.now().Add(time.Duration(p.Seconds) * time.Second)
	capability, seconds := p.Capability, p.Seconds
	b.mu.Unlock()

	grantID, e := randomID()
	if e != nil {
		return e
	}
	b.backendMu.Lock()
	defer b.backendMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	w, e := b.backend.window(ctx, windowID)
	if e != nil || !w.Visible || w.Hidden || w.XWayland || w.Revision != selected.Revision {
		return errors.New("window closed or moved; cancel and start sharing again")
	}
	label := capability + " window: " + w.Title + " (" + w.Class + ") · proactively shared"
	if capability == "control" {
		label += " · viewing + input; no recording · application retains its existing powers"
	}
	g := &Grant{ID: grantID, Client: client, Capability: capability, Scope: Scope{"window", w.ID}, Label: label, Expires: expires}
	authorized := false
	if capability == "control" {
		if e = b.backend.guard(ctx, map[string]any{"op": "authorize", "token": g.ID, "window": w.ID, "milliseconds": seconds * 1000}); e != nil {
			return e
		}
		authorized = true
	}
	b.mu.Lock()
	_, clientConnected := b.clients[client]
	stillCurrent := b.picker == p && b.authority == authority && !b.paused && len(b.pendingRevocations) == 0 && b.uiCount > 0 && clientConnected && b.now().Before(expires)
	if stillCurrent {
		b.grants[g.ID] = g
		for _, request := range b.requests {
			if request.State == "pending" && grantMatches(g, request.Client, request.Capability, request.Scope, &w) {
				request.State = "granted"
			}
		}
		b.picker = nil
		b.open++
		b.noteLocked("shared", fmt.Sprintf("%s → %s for %ds", label, client, seconds))
	}
	b.mu.Unlock()
	if !stillCurrent {
		if authorized {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 2*time.Second)
			b.cleanupGuard(cleanupCtx, map[string]any{"op": "revoke", "token": g.ID})
			cleanupCancel()
		}
		return errors.New("window picker changed while grant was being authorized")
	}
	return nil
}
