package app

import (
	"context"
	"errors"
	"fmt"
	"math"
)

type SurfaceInfo struct {
	ID       string     `json:"surface_id"`
	Kind     string     `json:"kind"`
	Revision string     `json:"revision"`
	Offset   [2]float64 `json:"offset"`
	Size     [2]float64 `json:"size"`
}
type RelatedWindow struct {
	Window                string `json:"window_id"`
	Relationship          string `json:"relationship"`
	SeparateGrantRequired bool   `json:"separate_grant_required"`
}
type WindowSurfaceState struct {
	Window           string          `json:"window_id"`
	Revision         string          `json:"revision"`
	Surfaces         []SurfaceInfo   `json:"surfaces"`
	Related          []RelatedWindow `json:"related_windows"`
	RelatedTruncated bool            `json:"related_windows_truncated"`
	SeatGrabActive   bool            `json:"seat_grab_active"`
	SessionLocked    bool            `json:"session_locked"`
	Version          int             `json:"surface_tree_version"`
}

func (d *Desktop) windowState(ctx context.Context, window string) (*WindowSurfaceState, error) {
	if window == "" || len(window) > 128 {
		return nil, errors.New("invalid window_id")
	}
	var reply struct {
		OK    bool                `json:"ok"`
		Error string              `json:"error"`
		State *WindowSurfaceState `json:"state"`
	}
	if err := d.guardExchange(ctx, map[string]any{"op": "window_state", "window": window}, &reply); err != nil {
		return nil, err
	}
	if !reply.OK {
		return nil, fmt.Errorf("window_state unavailable (update the guard with local setup if unsupported): %s", reply.Error)
	}
	state := reply.State
	if state == nil || state.Version != 1 {
		return nil, errors.New("surface_tree_guard_unavailable: run local setup to update the guard")
	}
	if state.Window != window || state.Revision == "" || len(state.Surfaces) == 0 || len(state.Surfaces) > 128 || len(state.Related) > 64 {
		return nil, errors.New("invalid_window_state")
	}
	seen := map[string]bool{}
	for _, surface := range state.Surfaces {
		if surface.ID == "" || len(surface.ID) > 128 || surface.Revision == "" || seen[surface.ID] {
			return nil, errors.New("invalid_surface_state")
		}
		seen[surface.ID] = true
		if surface.Kind != "toplevel" && surface.Kind != "popup" && surface.Kind != "subsurface" {
			return nil, errors.New("invalid_surface_kind")
		}
		for i := range 2 {
			if math.IsNaN(surface.Size[i]) || math.IsInf(surface.Size[i], 0) || surface.Size[i] <= 0 || math.IsNaN(surface.Offset[i]) || math.IsInf(surface.Offset[i], 0) {
				return nil, errors.New("invalid_surface_geometry")
			}
		}
	}
	relatedSeen := map[string]bool{}
	for _, related := range state.Related {
		if related.Window == "" || len(related.Window) > 128 || related.Window == window || relatedSeen[related.Window] || !related.SeparateGrantRequired || (related.Relationship != "parent" && related.Relationship != "transient_child") {
			return nil, errors.New("invalid_related_window_state")
		}
		relatedSeen[related.Window] = true
	}
	return state, nil
}

func (b *Broker) windowStateResult(ctx context.Context, window string) (map[string]any, error) {
	state, err := b.backend.windowState(ctx, window)
	if err != nil {
		return nil, err
	}
	return map[string]any{"status": "ok", "state": state, "note": "Metadata only, not a grant or a semantic snapshot. New toplevels need their own grant. Surface coordinates are local to the selected surface. Active seat grabs remain refused. Popup pixels in view_window are not guaranteed."}, nil
}
