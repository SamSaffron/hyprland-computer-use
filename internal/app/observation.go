package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Frame IDs identify encoded content, not UI freshness or input authority.
func captureMetadata(w Window, data []byte, capturedAt time.Time) (map[string]any, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || format != "png" || cfg.Width <= 0 || cfg.Height <= 0 || w.Size[0] <= 0 || w.Size[1] <= 0 {
		return nil, errors.New("invalid_capture")
	}
	sum := sha256.Sum256(data)
	return map[string]any{
		"status": "ok", "window_id": w.ID, "revision": w.Revision,
		"geometry_revision": w.Revision, "frame_id": hex.EncodeToString(sum[:]),
		"captured_at": capturedAt.UTC().Format(time.RFC3339Nano),
		"image_size":  [2]int{cfg.Width, cfg.Height}, "logical_size": w.Size,
		"image_to_window": map[string]any{"scale_x": float64(w.Size[0]) / float64(cfg.Width), "scale_y": float64(w.Size[1]) / float64(cfg.Height), "offset_x": 0, "offset_y": 0},
	}, nil
}

func resultContent(meta any, data []byte, failed bool) *mcp.CallToolResult {
	text, _ := json.Marshal(meta)
	result := &mcp.CallToolResult{IsError: failed, StructuredContent: meta, Content: []mcp.Content{&mcp.TextContent{Text: string(text)}}}
	if data != nil {
		result.Content = append(result.Content, &mcp.ImageContent{Data: data, MIMEType: "image/png"})
	}
	return result
}

func (b *Broker) observe(ctx context.Context, client, window string, maxWidth int) (map[string]any, []byte, error) {
	return b.observeWithOptions(ctx, client, window, CaptureOptions{MaxWidth: maxWidth})
}
func (b *Broker) observeWithOptions(ctx context.Context, client, window string, options CaptureOptions) (map[string]any, []byte, error) {
	if err := options.validate(); err != nil {
		return nil, nil, err
	}

	w, err := b.backend.window(ctx, window)
	if err != nil {
		return nil, nil, err
	}
	if _, err := options.bounds(w); err != nil {
		return nil, nil, err
	}
	_, request, err := b.permit(client, "observe", Scope{"window", w.ID}, &w, "View this window")
	if err != nil {
		return nil, nil, err
	}
	if request != "" {
		return map[string]any{"status": "approval_required", "request_id": request}, nil, nil
	}
	data, err := b.backend.captureWithOptions(ctx, w, options)
	if err != nil {
		return nil, nil, err
	}
	capturedAt := b.now()
	// Use fresh membership: a workspace grant must not release pixels after the
	// target leaves that workspace, even when geometry is unchanged.
	current, err := b.backend.window(ctx, w.ID)
	if err != nil {
		return nil, nil, err
	}
	if current.Revision != w.Revision || current.Size != w.Size || current.Workspace.ID != w.Workspace.ID || !current.Visible || current.Hidden {
		return nil, nil, errors.New("window_changed_during_capture")
	}
	meta, err := captureMetadata(current, data, capturedAt)
	if err != nil {
		return nil, nil, err
	}
	if options.Region != nil {
		r := *options.Region
		size := meta["image_size"].([2]int)
		meta["region"] = r
		meta["image_to_window"] = map[string]any{"scale_x": float64(r.Width) / float64(size[0]), "scale_y": float64(r.Height) / float64(size[1]), "offset_x": r.X, "offset_y": r.Y}
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	b.mu.Lock()
	_, allowed := b.allowedLocked(client, "observe", Scope{"window", current.ID}, &current)
	if allowed {
		b.noteLocked("observation", "window="+w.ID)
	}
	b.mu.Unlock()
	if !allowed {
		return nil, nil, errors.New("permission revoked during capture")
	}
	return meta, data, nil
}

func (b *Broker) inputTool(ctx context.Context, client string, a InputArgs) (*mcp.CallToolResult, any, error) {
	value, err := b.input(ctx, client, a)
	if err != nil {
		var failure *InputFailure
		if !errors.As(err, &failure) {
			return nil, nil, err
		}
		meta := map[string]any{"status": "failed", "window_id": a.Window, "completed_actions": failure.CompletedActions, "failed_action": failure.FailedAction, "completed_characters": failure.CompletedCharacters, "error": failure.Error(), "failed_transaction_may_have_effects": true, "retry_guidance": "Re-observe before retrying; counts are acknowledged transactions, not confirmed application changes."}
		return resultContent(meta, nil, true), nil, nil
	}
	meta := value
	if meta["status"] != "completed" || a.Then == "" {
		return resultContent(meta, nil, false), nil, nil
	}
	if a.Then == "state" {
		state, err := b.windowStateResult(ctx, a.Window)
		if err != nil {
			state = map[string]any{"status": "failed", "error": err.Error()}
		}
		meta["window_state"] = state
		return resultContent(meta, nil, false), nil, nil
	}
	options := CaptureOptions{}
	if a.Observation != nil {
		options = a.Observation.CaptureOptions
		// b.input has returned: no input mutex, borrowed focus or synthetic
		// press is held during this cancellable broker-side wait.
		if err := waitForObservation(ctx, a.Observation.DelayMS); err != nil {
			meta["observation"] = map[string]any{"status": "failed", "error": err.Error()}
			return resultContent(meta, nil, false), nil, nil
		}
	}
	observation, data, err := b.observeWithOptions(ctx, client, a.Window, options)
	if err != nil {
		// The batch completed: never turn an observation failure into a request to
		// replay input. Preserve its result and report the observation separately.
		meta["observation"] = map[string]any{"status": "failed", "error": err.Error()}
	} else {
		meta["observation"] = observation
	}
	return resultContent(meta, data, false), nil, nil
}
