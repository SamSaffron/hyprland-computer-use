package app

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"github.com/godbus/dbus/v5/prop"
)

const itemInterface = "org.kde.StatusNotifierItem"

type tray struct {
	b              *Broker
	ctx            context.Context
	launch         func(context.Context) error
	position       func(context.Context, int32, int32) *TrayAnchor
	mu             sync.Mutex
	consoleRunning bool
	consoleDone    chan struct{}
}

func (t *tray) Activate(x, y int32) *dbus.Error {
	var anchor *TrayAnchor
	if t.position != nil {
		ctx, cancel := context.WithTimeout(t.ctx, 300*time.Millisecond)
		anchor = t.position(ctx, x, y)
		cancel()
	}
	t.b.mu.Lock()
	t.b.open++
	t.b.trayAnchor = anchor
	connected := t.b.uiCount > 0
	t.b.mu.Unlock()
	if connected {
		return nil
	}
	// Coalesce repeated clicks while Quickshell starts or reconnects. Never hold
	// the broker lock across process startup, and never grant or resume here.
	t.mu.Lock()
	if !t.consoleRunning && t.ctx.Err() == nil {
		t.consoleRunning = true
		t.consoleDone = make(chan struct{})
		go func() {
			defer func() { t.mu.Lock(); t.consoleRunning = false; close(t.consoleDone); t.mu.Unlock() }()
			if err := t.launch(t.ctx); err != nil {
				fmt.Fprintln(os.Stderr, "tray: cannot open permission console:", err)
				t.b.mu.Lock()
				t.b.noteLocked("console_launch_failed", err.Error())
				t.b.mu.Unlock()
			}
		}()
	}
	t.mu.Unlock()
	return nil
}
func (t *tray) SecondaryActivate(x, y int32) *dbus.Error           { return t.Activate(x, y) }
func (t *tray) ContextMenu(x, y int32) *dbus.Error                 { return t.Activate(x, y) }
func (t *tray) Scroll(delta int32, orientation string) *dbus.Error { return nil }

type iconPixmap struct {
	Width  int32
	Height int32
	Data   []byte
}
type tooltip struct {
	IconName    string
	IconPixmap  []iconPixmap
	Title       string
	Description string
}

func (b *Broker) runTray(ctx context.Context) {
	c, e := dbus.ConnectSessionBus()
	if e != nil {
		fmt.Fprintln(os.Stderr, "tray:", e)
		return
	}
	defer c.Close()
	name := fmt.Sprintf("org.kde.StatusNotifierItem-%d-1", os.Getpid())
	_, e = c.RequestName(name, dbus.NameFlagDoNotQueue)
	if e != nil {
		return
	}
	path := dbus.ObjectPath("/StatusNotifierItem")
	consoleCtx, cancelConsole := context.WithCancel(ctx)
	item := &tray{b: b, ctx: consoleCtx, launch: runConsoleContext, position: trayAnchor}
	defer func() {
		cancelConsole()
		item.mu.Lock()
		done := item.consoleDone
		item.mu.Unlock()
		if done != nil {
			<-done
		}
	}()
	if e = c.Export(item, path, itemInterface); e != nil {
		return
	}
	menuPath := dbus.ObjectPath("/Menu")
	if e = exportTrayMenu(c, menuPath, item); e != nil {
		fmt.Fprintln(os.Stderr, "tray menu:", e)
		return
	}
	properties := map[string]*prop.Prop{"Category": {Value: "Hardware"}, "Id": {Value: "computer-use"}, "Title": {Value: "Computer Use"}, "Status": {Value: "Active"}, "WindowId": {Value: uint32(0)}, "IconName": {Value: ""}, "IconPixmap": {Value: icon(101, 215, 178), Emit: prop.EmitTrue}, "OverlayIconName": {Value: ""}, "AttentionIconName": {Value: ""}, "AttentionIconPixmap": {Value: icon(220, 145, 50)}, "ItemIsMenu": {Value: false}, "Menu": {Value: menuPath}, "ToolTip": {Value: tooltip{"", nil, "Computer Use", "Approve mode"}, Emit: prop.EmitTrue}}
	p, e := prop.Export(c, path, map[string]map[string]*prop.Prop{itemInterface: properties})
	if e != nil {
		return
	}
	_ = c.Export(introspect.NewIntrospectable(&introspect.Node{Name: string(path), Interfaces: []introspect.Interface{{Name: itemInterface, Methods: introspect.Methods(item)}, prop.IntrospectData}}), path, "org.freedesktop.DBus.Introspectable")
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	last := ""
	lastOwner := ""
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			var owner string
			_ = c.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.GetNameOwner", 0, "org.kde.StatusNotifierWatcher").Store(&owner)
			if owner != "" && owner != lastOwner {
				if call := c.Object("org.kde.StatusNotifierWatcher", "/StatusNotifierWatcher").CallWithContext(ctx, "org.kde.StatusNotifierWatcher.RegisterStatusNotifierItem", 0, name); call.Err == nil {
					lastOwner = owner
				}
			}
			if owner == "" {
				lastOwner = ""
			}
			s := b.state("")
			status := fmt.Sprintf("%s · %d pending · %d grants", s.Mode, len(s.Requests), len(s.Grants))
			if s.Paused {
				status = "Paused — all control stopped"
			}
			if status != last {
				rgb := []byte{101, 215, 178}
				if s.Paused {
					rgb = []byte{148, 163, 184}
				} else if s.Mode == "yolo" {
					rgb = []byte{209, 78, 71}
				} else if len(s.Requests) > 0 {
					rgb = []byte{207, 142, 45}
				} else if len(s.Grants) > 0 {
					rgb = []byte{37, 149, 115}
				}
				p.SetMust(itemInterface, "IconPixmap", icon(rgb[0], rgb[1], rgb[2]))
				p.SetMust(itemInterface, "ToolTip", tooltip{"", nil, "Computer Use", status})
				_ = c.Emit(path, itemInterface+".NewIcon")
				_ = c.Emit(path, itemInterface+".NewToolTip")
				last = status
			}
		}
	}
}
