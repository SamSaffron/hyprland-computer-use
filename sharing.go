package main

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
	ws, e := b.backend.windows(context.Background())
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
	if b.paused || b.uiCount == 0 {
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
	b.picker = &SharePicker{randomID(), client, capability, seconds, b.now().Add(time.Minute), candidates}
	b.noteLocked("share_picker", "local user started window selection")
	return nil
}

func (b *Broker) selectShare(pickerID, client, windowID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	p := b.picker
	if p == nil || p.ID != pickerID || !b.now().Before(p.Expires) {
		return errors.New("window picker expired or cancelled")
	}
	if b.paused || b.uiCount == 0 {
		return errors.New("sharing is paused")
	}
	if p.Client != "" && p.Client != client {
		return errors.New("picker recipient changed")
	}
	if _, ok := b.clients[client]; !ok {
		return errors.New("select a connected MCP client")
	}
	var selected *Window
	for i := range p.Windows {
		if p.Windows[i].ID == windowID {
			selected = &p.Windows[i]
			break
		}
	}
	if selected == nil {
		return errors.New("window was not offered by this picker")
	}
	w, e := b.backend.window(context.Background(), windowID)
	if e != nil || !w.Visible || w.Hidden || w.XWayland || w.Revision != selected.Revision {
		return errors.New("window closed or moved; cancel and start sharing again")
	}
	label := p.Capability + " window: " + w.Title + " (" + w.Class + ") · proactively shared"
	if p.Capability == "control" {
		label += " · viewing + input; no recording · application retains its existing powers"
	}
	g := &Grant{ID: randomID(), Client: client, Capability: p.Capability, Scope: Scope{"window", w.ID}, Label: label, Expires: b.now().Add(time.Duration(p.Seconds) * time.Second)}
	if p.Capability == "control" {
		if e = b.backend.guard(context.Background(), map[string]any{"op": "authorize", "token": g.ID, "window": w.ID, "milliseconds": p.Seconds * 1000}); e != nil {
			return e
		}
	}
	b.grants[g.ID] = g
	// A pre-existing matching request is now satisfied by the user's proactive grant.
	for _, r := range b.requests {
		if r.State == "pending" && grantMatches(g, r.Client, r.Capability, r.Scope, &w) {
			r.State = "granted"
		}
	}
	b.picker = nil
	b.open++
	b.noteLocked("shared", fmt.Sprintf("%s → %s for %ds", label, client, p.Seconds))
	return nil
}
