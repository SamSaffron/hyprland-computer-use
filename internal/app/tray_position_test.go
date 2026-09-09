package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestTrayBarGeometry(t *testing.T) {
	data := []byte(`{"left":{"levels":{"0":[{"x":-1920,"y":0,"w":1920,"h":1080}],"2":[{"x":-1920,"y":1040,"w":1920,"h":40,"namespace":"quickshell"}],"3":[{"x":-450,"y":1040,"w":440,"h":30,"namespace":"computer-use-permissions"}]}},"right":{"levels":{"2":[{"x":1920,"y":0,"w":48,"h":1080,"namespace":"waybar"}]}}}`)
	for _, tc := range []struct {
		x, y   float64
		screen string
		w, h   float64
	}{
		{-20, 1060, "left", 1920, 40},
		{1940, 300, "right", 48, 1080},
		{300, 300, "", 0, 0},
	} {
		a := &TrayAnchor{X: tc.x, Y: tc.y}
		attachTrayBar(a, data)
		if a.Screen != tc.screen {
			t.Fatalf("wrong screen: %+v", a)
		}
		if tc.screen != "" && (a.Bar == nil || a.Bar.Width != tc.w || a.Bar.Height != tc.h) {
			t.Fatalf("wrong toolbar: %+v", a.Bar)
		}
		if tc.screen == "" && a.Bar != nil {
			t.Fatal("invented toolbar")
		}
	}
}

func TestTrayPositionUsesCursorOnlyForMissingCoordinates(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\ncase $2 in\ncursorpos) echo '{\"x\":3100,\"y\":1330}';;\nlayers) echo '{\"DP-1\":{\"levels\":{\"2\":[{\"x\":0,\"y\":1310,\"w\":3200,\"h\":40}]}}}';;\nesac\n"
	if err := os.WriteFile(filepath.Join(dir, "hyprctl"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	for _, coordinate := range []int32{0, -1} {
		a := trayAnchor(context.Background(), coordinate, coordinate)
		if a == nil || a.X != 3100 || a.Y != 1330 || a.Screen != "DP-1" || a.Bar == nil {
			t.Fatalf("no bottom tray anchor: %+v", a)
		}
	}
	a := trayAnchor(context.Background(), -100, 10)
	if a == nil || a.X != -100 || a.Y != 10 {
		t.Fatal("overrode valid negative-monitor coordinates")
	}
	t.Setenv("PATH", t.TempDir())
	if trayAnchor(context.Background(), 0, 0) != nil {
		t.Fatal("invented coordinates after query failure")
	}
}

func TestTrayActivationPublishesPlacementWithoutAuthorityChanges(t *testing.T) {
	b := newBroker(nil)
	b.uiCount = 1
	b.paused = true
	anchor := &TrayAnchor{X: 500, Y: 1060}
	item := &tray{b: b, ctx: context.Background(), position: func(_ context.Context, x, y int32) *TrayAnchor {
		if x != 500 || y != 1060 {
			t.Fatal("lost activation coordinates")
		}
		return anchor
	}}
	item.Activate(500, 1060)
	state := b.state("")
	if state.TrayAnchor != anchor || state.Open != 1 || !state.Paused || state.Mode != "approve" || len(state.Grants) != 0 {
		t.Fatalf("bad activation state: %+v", state)
	}
}
