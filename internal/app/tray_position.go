package app

import (
	"context"
	"encoding/json"
)

// TrayAnchor is local presentation metadata, never input authority. Coordinates
// are compositor-global logical pixels, like Quickshell screen geometry.
type TrayAnchor struct {
	X      float64  `json:"x"`
	Y      float64  `json:"y"`
	Screen string   `json:"screen,omitempty"`
	Bar    *TrayBar `json:"bar,omitempty"`
}
type TrayBar struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"w"`
	Height float64 `json:"h"`
}

func trayAnchor(ctx context.Context, x, y int32) *TrayAnchor {
	a := &TrayAnchor{X: float64(x), Y: float64(y)}
	// Wayland tray hosts and DBusMenu often supply no activation coordinates.
	// Read the pointer only at activation, never continuously while the UI is open.
	if (x == 0 && y == 0) || (x == -1 && y == -1) {
		data, err := command(ctx, "hyprctl", "-j", "cursorpos")
		var point struct{ X, Y *float64 }
		if err != nil || json.Unmarshal(data, &point) != nil || point.X == nil || point.Y == nil {
			return nil
		}
		a.X, a.Y = *point.X, *point.Y
	}
	data, err := command(ctx, "hyprctl", "-j", "layers")
	if err == nil {
		attachTrayBar(a, data)
	}
	return a
}

func attachTrayBar(a *TrayAnchor, data []byte) {
	var monitors map[string]struct {
		Levels map[string][]struct {
			TrayBar
			Namespace string `json:"namespace"`
		} `json:"levels"`
	}
	if json.Unmarshal(data, &monitors) != nil {
		return
	}
	best := 0.0
	for screen, monitor := range monitors {
		// Background layers cannot host a tray. Prefer the smallest thin panel
		// under the activation point, excluding our permission/target overlays.
		for _, level := range []string{"2", "3"} {
			for _, layer := range monitor.Levels[level] {
				b := layer.TrayBar
				if layer.Namespace == "computer-use-permissions" || layer.Namespace == "computer-use-target" {
					continue
				}
				if b.Width <= 0 || b.Height <= 0 || min(b.Width, b.Height) > 160 || max(b.Width, b.Height) < 3*min(b.Width, b.Height) {
					continue
				}
				if a.X < b.X || a.Y < b.Y || a.X >= b.X+b.Width || a.Y >= b.Y+b.Height {
					continue
				}
				area := b.Width * b.Height
				if best == 0 || area < best {
					best = area
					a.Bar = &b
					a.Screen = screen
				}
			}
		}
	}
}
