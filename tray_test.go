package main

import (
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

func TestTrayActivationStartsOneConsole(t *testing.T) {
	b := newBroker(nil)
	b.paused = true
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started, done := make(chan struct{}), make(chan struct{})
	var launches atomic.Int32
	item := &tray{b: b, ctx: ctx, launch: func(ctx context.Context) error {
		launches.Add(1)
		close(started)
		<-ctx.Done()
		close(done)
		return nil
	}}
	item.Activate(0, 0)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("console not started")
	}
	item.Activate(0, 0)
	item.ContextMenu(0, 0)
	item.SecondaryActivate(0, 0)
	if launches.Load() != 1 {
		t.Fatal("duplicate console launch")
	}
	b.mu.Lock()
	if b.open != 4 || !b.paused || b.mode != "approve" || len(b.grants) != 0 {
		t.Fatal("activation changed authority or did not open panel")
	}
	b.mu.Unlock()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("console did not receive shutdown")
	}
}

func TestTrayActivationReusesSupervisor(t *testing.T) {
	b := newBroker(nil)
	b.uiCount = 1
	item := &tray{b: b, ctx: context.Background(), launch: func(context.Context) error { t.Error("launched duplicate console"); return nil }}
	item.Activate(0, 0)
	if b.open != 1 {
		t.Fatal("existing UI was not signaled")
	}
}

func TestTrayMenu(t *testing.T) {
	b := newBroker(nil)
	b.uiCount = 1
	menu := &trayMenu{&tray{b: b, ctx: context.Background()}}
	_, layout, err := menu.GetLayout(0, -1, nil)
	if err != nil || len(layout.Children) != 1 || dbus.SignatureOf(layout).String() != "(ia{sv}av)" {
		t.Fatalf("bad layout: %+v %v", layout, err)
	}
	_, shallow, _ := menu.GetLayout(0, 0, nil)
	if len(shallow.Children) != 0 {
		t.Fatal("ignored depth")
	}
	_, child, _ := menu.GetLayout(1, -1, []string{"label"})
	if len(child.Properties) != 1 {
		t.Fatal("ignored property filter")
	}
	menu.Event(1, "hovered", dbus.MakeVariant(0), 0)
	if b.open != 0 {
		t.Fatal("hover activated console")
	}
	if err := menu.Event(1, "clicked", dbus.MakeVariant(0), 0); err != nil || b.open != 1 {
		t.Fatal("menu click did not activate")
	}
	if err := menu.Event(99, "clicked", dbus.MakeVariant(0), 0); err == nil {
		t.Fatal("unknown item accepted")
	}
}

func TestTrayIcon(t *testing.T) {
	icons := icon(101, 215, 178)
	if len(icons) != 5 || dbus.SignatureOf(icons).String() != "a(iiay)" {
		t.Fatal("invalid SNI pixmaps")
	}
	for _, p := range icons {
		if len(p.Data) != int(p.Width*p.Height*4) {
			t.Fatal("invalid pixel count")
		}
		transparent, opaque, antialiased := false, false, false
		for i := 0; i < len(p.Data); i += 4 {
			switch p.Data[i] {
			case 0:
				transparent = true
			case 255:
				opaque = true
			default:
				antialiased = true
			}
		}
		if !transparent || !opaque || !antialiased {
			t.Fatal("missing transparency or smooth edges")
		}
	}
	// Optional local visual check, not an assertion of desktop rendering.
	if path := os.Getenv("COMPUTER_USE_ICON_PREVIEW"); path != "" {
		preview := image.NewNRGBA(image.Rect(0, 0, 440, 100))
		for y := 0; y < 100; y++ {
			for x := 0; x < 440; x++ {
				preview.SetNRGBA(x, y, color.NRGBA{15, 23, 42, 255})
			}
		}
		xoff := 16
		for _, p := range icons {
			for y := 0; y < int(p.Height); y++ {
				for x := 0; x < int(p.Width); x++ {
					i := (y*int(p.Width) + x) * 4
					a := int(p.Data[i])
					bg := []int{15, 23, 42}
					preview.SetNRGBA(xoff+x, 18+y, color.NRGBA{byte((int(p.Data[i+1])*a + bg[0]*(255-a)) / 255), byte((int(p.Data[i+2])*a + bg[1]*(255-a)) / 255), byte((int(p.Data[i+3])*a + bg[2]*(255-a)) / 255), 255})
				}
			}
			xoff += int(p.Width) + 30
		}
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, preview); err != nil {
			t.Fatal(err)
		}
	}
}

// Opt-in: run under dbus-run-session, never against the desktop session bus.
func TestTrayPrivateBus(t *testing.T) {
	if os.Getenv("COMPUTER_USE_TEST_DBUS") != "1" {
		t.Skip("requires private dbus-run-session")
	}
	server, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	client, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	b := newBroker(nil)
	b.uiCount = 1
	item := &tray{b: b, ctx: context.Background()}
	if err := server.Export(item, "/StatusNotifierItem", itemInterface); err != nil {
		t.Fatal(err)
	}
	if err := exportTrayMenu(server, "/Menu", item); err != nil {
		t.Fatal(err)
	}
	remote := client.Object(server.Names()[0], "/Menu")
	var revision uint32
	var layout menuLayout
	if err := remote.Call(menuInterface+".GetLayout", 0, int32(0), int32(-1), []string{}).Store(&revision, &layout); err != nil {
		t.Fatal(err)
	}
	if revision != 1 || len(layout.Children) != 1 {
		t.Fatal("bad wire layout")
	}
	if err := remote.Call(menuInterface+".Event", 0, int32(1), "clicked", dbus.MakeVariant(int32(0)), uint32(0)).Err; err != nil {
		t.Fatal(err)
	}
	if err := client.Object(server.Names()[0], "/StatusNotifierItem").Call(itemInterface+".Activate", 0, int32(0), int32(0)).Err; err != nil {
		t.Fatal(err)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.open != 2 || len(b.grants) != 0 || b.mode != "approve" {
		t.Fatal("wire activation failed or changed policy")
	}
}

func TestTrayRetriesFailedConsoleLaunch(t *testing.T) {
	b := newBroker(nil)
	var launches atomic.Int32
	item := &tray{b: b, ctx: context.Background(), launch: func(context.Context) error {
		launches.Add(1)
		return errors.New("test: Quickshell unavailable")
	}}
	for i := 0; i < 2; i++ {
		item.Activate(0, 0)
		item.mu.Lock()
		done := item.consoleDone
		item.mu.Unlock()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("failed launch did not finish")
		}
	}
	if launches.Load() != 2 {
		t.Fatal("failed launch blocked retry")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.audit) != 2 || b.audit[0].Event != "console_launch_failed" {
		t.Fatal("launch failure not audited")
	}
}
