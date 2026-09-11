package app

import (
	"context"
	"errors"
	"net"
	"net/url"
	"os/exec"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func tool[In any](s *mcp.Server, name, description string, f func(context.Context, In) (any, error)) {
	mcp.AddTool(s, &mcp.Tool{Name: name, Description: description}, func(ctx context.Context, _ *mcp.CallToolRequest, a In) (*mcp.CallToolResult, any, error) {
		v, e := f(ctx, a)
		return nil, v, e
	})
}
func (b *Broker) serveMCP(ctx context.Context, c net.Conn) {
	id, err := randomID()
	if err != nil {
		_ = c.Close()
		return
	}
	b.mu.Lock()
	b.clients[id] = "MCP " + id[:8]
	b.mu.Unlock()
	defer b.disconnect(id)
	s := b.newMCPServer(ctx, id, nil)
	_ = s.Run(ctx, &mcp.IOTransport{Reader: c, Writer: c})
}
func (b *Broker) newMCPServer(clientCtx context.Context, id string, opts *mcp.ServerOptions) *mcp.Server {
	if opts == nil {
		opts = &mcp.ServerOptions{}
	}
	opts.Instructions = "Permissioned Hyprland computer use. Approval decisions and modes belong exclusively to the local tray. An approval_required response is not a grant. Use wait_for_permission then retry. Capture and input are window scoped; transient toplevels need their own grant. No shell or arbitrary-file tool. Window metadata discovery is free; pixels still require permission. Local users can proactively grant access without a request; inspect computer_status for grants. Workspace observation includes new windows while they remain on that workspace. Terminal control is effectively shell authority."
	s := mcp.NewServer(&mcp.Implementation{Name: "computer-use", Version: "0.2.0"}, opts)
	tool(s, "computer_status", "Get this connection's mode and permission status including proactively shared windows.", func(ctx context.Context, a struct{}) (any, error) {
		b.mu.Lock()
		defer b.mu.Unlock()
		gs := []Grant{}
		rs := []Request{}
		for _, g := range b.grants {
			if g.Client == id && b.now().Before(g.Expires) {
				v := *g
				v.Remaining = max(0, int(g.Expires.Sub(b.now()).Seconds()))
				gs = append(gs, v)
			}
		}
		for _, r := range b.requests {
			if r.Client == id {
				rs = append(rs, *r)
			}
		}
		inputMode := "focus-borrowing"
		limitations := []string{"native Wayland windows only", "surface-tree input; active grabs and cross-surface drags refused", "Unicode text via target-client keymaps; toolkit behavior needs live validation", "no privilege broker"}
		if b.backend != nil && b.backend.independentSeat.Load() {
			inputMode = "independent-seat"
			limitations = []string{"native Wayland windows only; separate application processes recommended", "Kitty 0.48.2 binds only the first seat and is unsupported; GNOME Terminal typing was tested", "in terminals, send one command and Enter at a time, then observe its output; multiline bursts can garble terminal echo", "agent popup grabs require a valid agent-seat serial; GTK3 default-seat serials are refused", "same-process menus can drop human input; IME, clipboard and DnD are not independently integrated", "app-created windows may still change desktop activation", "popup pixels are not guaranteed in toplevel capture; observe contents before targeting", "cross-surface drags refused; no global fallback or privilege broker"}
		}
		if b.backend != nil && b.backend.automaticFallback.Load() {
			inputMode = "automatic"
			limitations[1] = "independent seat preferred; clients without both seat devices use guarded focus borrowing (temporary native focus transitions, idle input required)"
		}
		return map[string]any{"mode": b.mode, "input_mode": inputMode, "paused": b.paused, "revocation_unconfirmed": len(b.pendingRevocations) > 0, "supervisor_connected": b.uiCount > 0, "client_id": id, "grants": gs, "requests": rs, "limitations": limitations}, nil
	})
	type PermissionArgs struct {
		Capability string `json:"capability" jsonschema:"observe, control, record, or launch"`
		Scope      Scope  `json:"scope"`
		Reason     string `json:"reason" jsonschema:"Short human-readable reason, shown as untrusted agent text in the local tray"`
	}
	tool(s, "request_permission", "Ask the local user for a timed permission. This tool cannot grant permissions.", func(ctx context.Context, a PermissionArgs) (any, error) {
		var w *Window
		if a.Scope.Kind == "window" {
			v, e := b.backend.window(ctx, a.Scope.ID)
			if e != nil {
				return nil, e
			}
			w = &v
		}
		g, r, e := b.permit(id, a.Capability, a.Scope, w, cleanReason(a.Reason))
		if e != nil {
			return nil, e
		}
		if r != "" {
			return map[string]any{"status": "approval_required", "request_id": r}, nil
		}
		return map[string]any{"status": "granted", "grant": g}, nil
	})
	tool(s, "wait_for_permission", "Wait for a decision made in the local tray; does not approve anything. Then retry the original tool.", func(ctx context.Context, a struct {
		Request string `json:"request_id"`
		Seconds int    `json:"seconds,omitempty"`
	}) (any, error) {
		if a.Seconds == 0 {
			a.Seconds = 60
		}
		if a.Seconds < 1 || a.Seconds > 120 {
			return nil, errors.New("seconds must be 1–120")
		}
		ctx, cancel := context.WithTimeout(ctx, time.Duration(a.Seconds)*time.Second)
		defer cancel()
		t := time.NewTicker(100 * time.Millisecond)
		defer t.Stop()
		for {
			b.mu.Lock()
			r := b.requests[a.Request]
			state := "unknown"
			if r != nil && r.Client == id {
				state = r.State
			}
			b.mu.Unlock()
			if state != "pending" {
				return map[string]any{"status": state}, nil
			}
			select {
			case <-ctx.Done():
				return map[string]any{"status": "pending"}, nil
			case <-t.C:
			}
		}
	})
	tool(s, "list_windows", "Discover window metadata for free, even while paused: IDs, titles, app, workspace, geometry and revisions. Omit workspace (or use 0) for all workspaces. No pixels or input authority.", func(ctx context.Context, a struct {
		Workspace int `json:"workspace,omitempty"`
	}) (any, error) {
		if a.Workspace < 0 || a.Workspace > 10000 {
			return nil, errors.New("invalid workspace")
		}
		ws, e := b.backend.windows(ctx)
		if e != nil {
			return nil, e
		}
		out := []Window{}
		for _, w := range ws {
			if a.Workspace == 0 || w.Workspace.ID == a.Workspace {
				out = append(out, w)
			}
		}
		return map[string]any{"status": "ok", "windows": out}, nil
	})
	tool(s, "window_state", "Discover bounded compositor-owned popup/subsurface geometry and explicit transient-parent/child window IDs. Metadata only: no pixels, semantic contents or grants. Input to a new toplevel needs a separate grant. Use surface_id and its revision for popup-local input; active seat grabs remain refused.", func(ctx context.Context, a struct {
		Window string `json:"window_id"`
	}) (any, error) {
		return b.windowStateResult(ctx, a.Window)
	})
	type ViewArgs struct {
		Window string `json:"window_id" jsonschema:"Instance-bound window ID returned by list_windows"`
		CaptureOptions
	}
	mcp.AddTool(s, &mcp.Tool{Name: "view_window", Description: "Capture this actual toplevel only, never a desktop crop. Requires observation permission. Returns PNG, actual image dimensions, image-to-window transform, geometry revision, content frame ID and capture completion timestamp. Optional region crops the full-resolution toplevel before resizing (up to 4x zoom); max_width/max_height preserve aspect ratio. Crop transforms include offsets. Frame IDs are not input freshness tokens."}, func(ctx context.Context, _ *mcp.CallToolRequest, a ViewArgs) (*mcp.CallToolResult, any, error) {
		meta, data, err := b.observeWithOptions(ctx, id, a.Window, a.CaptureOptions)
		if err != nil {
			return nil, nil, err
		}
		return resultContent(meta, data, false), nil, nil
	})
	mcp.AddTool(s, &mcp.Tool{Name: "input_window", InputSchema: inputWindowSchema(), Description: "Perform up to 128 fully prevalidated window-scoped actions with a 60-second execution budget. Unicode text is limited to 262144 UTF-8 bytes per batch and delivered in 48-scalar chunks without clipboard access. Window-local logical coordinates and exact geometry revision required by default. Optional surface_id plus surface_revision from window_state selects surface-local coordinates, including popups. Pointer input hit-tests subsurfaces; crossing surfaces within a drag is refused. Ordinary input restores focus without cursor warp; explicit focus activates. No global fallback. Runtime failures report acknowledged action/character counts; the failed transaction may still have effects, so re-observe before retrying. Optional then=state returns surface/dialog metadata; then=screenshot returns a permission-checked capture (optional observation crop/max_width/max_height) only after a completed batch. Observation failure never means replay the batch."}, func(ctx context.Context, _ *mcp.CallToolRequest, a InputArgs) (*mcp.CallToolResult, any, error) {
		return b.inputTool(ctx, id, a)
	})
	tool(s, "record_window", "Start a local, window-only MP4 recording. Separate record permission required. Stops on revoke, expiry, disconnect or 10-minute cap.", func(ctx context.Context, a struct {
		Window string `json:"window_id"`
	}) (any, error) {
		w, e := b.backend.window(ctx, a.Window)
		if e != nil {
			return nil, e
		}
		return b.record(clientCtx, id, w)
	})
	tool(s, "stop_recording", "Stop one recording belonging to this MCP connection.", func(ctx context.Context, a struct {
		ID string `json:"recording_id"`
	}) (any, error) {
		b.mu.Lock()
		defer b.mu.Unlock()
		r := b.recordings[a.ID]
		if r == nil || r.Info.Client != id {
			return nil, errors.New("unknown recording")
		}
		r.cancel()
		return map[string]any{"status": "stopping"}, nil
	})
	tool(s, "list_recordings", "List this connection's recordings and local output paths. No arbitrary file read.", func(ctx context.Context, a struct{}) (any, error) {
		b.mu.Lock()
		defer b.mu.Unlock()
		out := []RecordingInfo{}
		for _, r := range b.recordings {
			if r.Info.Client == id {
				out = append(out, r.Info)
			}
		}
		return out, nil
	})
	tool(s, "launch_application", "Launch an allowlisted application: chromium or pinta. Optional http(s) URL for Chromium only. New windows do not inherit input permission.", func(ctx context.Context, a struct {
		Application string `json:"application"`
		URL         string `json:"url,omitempty"`
	}) (any, error) {
		if a.Application != "chromium" && a.Application != "pinta" {
			return nil, errors.New("application is not allowlisted")
		}
		args := []string{}
		if a.URL != "" {
			u, e := url.Parse(a.URL)
			if e != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || a.Application != "chromium" {
				return nil, errors.New("invalid browser URL")
			}
			args = append(args, a.URL)
		}
		_, r, e := b.permit(id, "launch", Scope{"application", a.Application}, nil, "Launch "+a.Application)
		if e != nil {
			return nil, e
		}
		if r != "" {
			return map[string]any{"status": "approval_required", "request_id": r}, nil
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		cmd := exec.CommandContext(clientCtx, a.Application, args...)
		if e = cmd.Start(); e != nil {
			return nil, e
		}
		go cmd.Wait()
		return map[string]any{"status": "launched", "pid": cmd.Process.Pid}, nil
	})
	return s
}
