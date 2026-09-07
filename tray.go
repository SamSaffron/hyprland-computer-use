package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"github.com/godbus/dbus/v5/prop"
)

const itemInterface = "org.kde.StatusNotifierItem"

type tray struct{ b *Broker }

func (t *tray) Activate(x, y int32) *dbus.Error {
	t.b.mu.Lock()
	t.b.open++
	t.b.mu.Unlock()
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

func icon(r, g, b byte) []iconPixmap {
	data := make([]byte, 32*32*4)
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			i := (y*32 + x) * 4
			if (x-16)*(x-16)+(y-16)*(y-16) < 225 {
				data[i] = 255
				data[i+1] = r
				data[i+2] = g
				data[i+3] = b
			}
			if ((x == 10 || x == 11) && y >= 10 && y <= 21) || ((y == 10 || y == 11 || y == 20 || y == 21) && x >= 10 && x <= 16) || ((x == 20 || x == 21) && y >= 10 && y <= 17) || ((x == 20 || x == 21) && y >= 20 && y <= 22) {
				data[i] = 255
				data[i+1] = 250
				data[i+2] = 250
				data[i+3] = 250
			}
		}
	}
	return []iconPixmap{{32, 32, data}}
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
	item := &tray{b}
	if e = c.Export(item, path, itemInterface); e != nil {
		return
	}
	properties := map[string]*prop.Prop{"Category": {Value: "Hardware"}, "Id": {Value: "computer-use"}, "Title": {Value: "Computer Use"}, "Status": {Value: "Active"}, "WindowId": {Value: uint32(0)}, "IconName": {Value: ""}, "IconPixmap": {Value: icon(70, 90, 105), Emit: prop.EmitTrue}, "OverlayIconName": {Value: ""}, "AttentionIconName": {Value: ""}, "AttentionIconPixmap": {Value: icon(220, 145, 50)}, "ItemIsMenu": {Value: false}, "Menu": {Value: dbus.ObjectPath("/")}, "ToolTip": {Value: tooltip{"", nil, "Computer Use", "Approve mode"}, Emit: prop.EmitTrue}}
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
			_ = c.BusObject().Call("org.freedesktop.DBus.GetNameOwner", 0, "org.kde.StatusNotifierWatcher").Store(&owner)
			if owner != "" && owner != lastOwner {
				if call := c.Object("org.kde.StatusNotifierWatcher", "/StatusNotifierWatcher").Call("org.kde.StatusNotifierWatcher.RegisterStatusNotifierItem", 0, name); call.Err == nil {
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
				rgb := []byte{65, 95, 110}
				if s.Mode == "yolo" {
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
