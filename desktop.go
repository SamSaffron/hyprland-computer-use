package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/png"
	"io"
	"math"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Window struct {
	FocusHistory int    `json:"focusHistoryID"`
	ID           string `json:"id"`
	StableID     string `json:"stableId,omitempty"`
	Address      string `json:"address,omitempty"`
	Title        string `json:"title"`
	Class        string `json:"class"`
	At           [2]int `json:"at"`
	Size         [2]int `json:"size"`
	Workspace    struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"workspace"`
	Mapped   bool   `json:"mapped"`
	Hidden   bool   `json:"hidden"`
	Visible  bool   `json:"visible"`
	XWayland bool   `json:"xwayland"`
	Revision string `json:"revision"`
}
type Desktop struct {
	Dir  string
	Data string
}

func command(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var errbuf bytes.Buffer
	cmd.Stderr = &errbuf
	out, e := cmd.Output()
	if e != nil {
		return nil, fmt.Errorf("%s: %w: %s", name, e, strings.TrimSpace(errbuf.String()))
	}
	return out, nil
}
func (d *Desktop) windows(ctx context.Context) ([]Window, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, e := command(ctx, "hyprctl", "-j", "clients")
	if e != nil {
		return nil, e
	}
	var ws []Window
	if e = json.Unmarshal(out, &ws); e != nil {
		return nil, e
	}
	result := []Window{}
	for _, w := range ws {
		if !w.Mapped || w.StableID == "" {
			continue
		}
		w.ID = w.StableID
		w.Revision = fmt.Sprintf("%d,%d,%d,%d", w.At[0], w.At[1], w.Size[0], w.Size[1])
		w.Address = ""
		w.StableID = ""
		result = append(result, w)
	}
	return result, nil
}
func (d *Desktop) window(ctx context.Context, id string) (Window, error) {
	ws, e := d.windows(ctx)
	if e != nil {
		return Window{}, e
	}
	for _, w := range ws {
		if w.ID == id {
			return w, nil
		}
	}
	return Window{}, errors.New("window_unavailable")
}
func (d *Desktop) guard(ctx context.Context, q map[string]any) error {
	c, e := (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "unix", filepath.Join(d.Dir, "guard.sock"))
	if e != nil {
		return fmt.Errorf("compositor_guard_unavailable: %w", e)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(2 * time.Second))
	if e = json.NewEncoder(c).Encode(q); e != nil {
		return e
	}
	var r struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if e = json.NewDecoder(io.LimitReader(c, 16384)).Decode(&r); e != nil {
		return e
	}
	if !r.OK {
		return errors.New(r.Error)
	}
	return nil
}
func (d *Desktop) capture(ctx context.Context, w Window, maxWidth int) ([]byte, error) {
	if !w.Visible || w.Hidden || w.Size[0] <= 0 || w.Size[1] <= 0 {
		return nil, errors.New("window_not_visible")
	}
	if maxWidth <= 0 {
		maxWidth = 1280
	}
	if maxWidth > 1920 {
		return nil, errors.New("max_width exceeds 1920")
	}
	scale := min(1, float64(maxWidth)/float64(w.Size[0]))
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, e := command(ctx, "grim", "-T", w.ID, "-s", strconv.FormatFloat(scale, 'f', 4, 64), "-")
	if e != nil {
		return nil, e
	}
	if len(out) > 16<<20 {
		return nil, errors.New("capture_too_large")
	}
	cfg, _, e := image.DecodeConfig(bytes.NewReader(out))
	if e != nil || cfg.Width < 1 || cfg.Height < 1 {
		return nil, errors.New("invalid_capture")
	}
	after, e := d.window(ctx, w.ID)
	if e != nil || after.Revision != w.Revision {
		return nil, errors.New("geometry_changed_during_capture")
	}
	return out, nil
}

type InputArgs struct {
	Window   string   `json:"window_id" jsonschema:"Window ID returned by list_windows"`
	Revision string   `json:"revision" jsonschema:"Exact geometry revision from list_windows or view_window"`
	Actions  []Action `json:"actions" jsonschema:"Ordered actions, maximum 128; coordinates are window-local logical pixels"`
}
type Action struct {
	Type       string  `json:"type" jsonschema:"focus, move, click, drag, scroll, key, or text"`
	X          float64 `json:"x,omitempty"`
	Y          float64 `json:"y,omitempty"`
	ToX        float64 `json:"to_x,omitempty"`
	ToY        float64 `json:"to_y,omitempty"`
	Button     string  `json:"button,omitempty"`
	Delta      float64 `json:"delta,omitempty"`
	Key        string  `json:"key,omitempty" jsonschema:"e.g. ENTER, CTRL+L, ALT+LEFT; SUPER/global compositor shortcuts are not supported"`
	Text       string  `json:"text,omitempty"`
	DurationMS int     `json:"duration_ms,omitempty"`
}

func keySpec(name string) (uint32, uint32, error) {
	parts := strings.Split(strings.ToUpper(name), "+")
	var mods uint32
	for _, s := range parts[:len(parts)-1] {
		switch s {
		case "CTRL", "CONTROL":
			mods |= 4
		case "SHIFT":
			mods |= 1
		case "ALT":
			mods |= 8
		default:
			return 0, 0, errors.New("unsupported modifier")
		}
	}
	n := parts[len(parts)-1]
	codes := map[string]uint32{"ENTER": 28, "RETURN": 28, "TAB": 15, "ESC": 1, "ESCAPE": 1, "BACKSPACE": 14, "DELETE": 111, "LEFT": 105, "RIGHT": 106, "UP": 103, "DOWN": 108, "HOME": 102, "END": 107, "PAGEUP": 104, "PAGEDOWN": 109, "SPACE": 57, "F1": 59, "F2": 60, "F3": 61, "F4": 62, "F5": 63, "F6": 64, "F7": 65, "F8": 66, "F9": 67, "F10": 68, "F11": 87, "F12": 88}
	if c, ok := codes[n]; ok {
		return c, mods, nil
	}
	if len(n) == 1 {
		c, _, e := runeKey(rune(strings.ToLower(n)[0]))
		return c, mods, e
	}
	return 0, 0, errors.New("unsupported key")
}
func runeKey(r rune) (uint32, uint32, error) {
	rows := []struct {
		s     string
		start uint32
	}{{"1234567890-=", 2}, {"qwertyuiop[]", 16}, {"asdfghjkl;'", 30}, {"zxcvbnm,./", 44}}
	var mods uint32
	if r >= 'A' && r <= 'Z' {
		r += 32
		mods = 1
	}
	shifted := "!@#$%^&*()_+{}:\"<>?~|"
	plain := "1234567890-=[];',./`\\"
	if i := strings.IndexRune(shifted, r); i >= 0 {
		r = rune(plain[i])
		mods = 1
	}
	for _, row := range rows {
		if i := strings.IndexRune(row.s, r); i >= 0 {
			return row.start + uint32(i), mods, nil
		}
	}
	switch r {
	case ' ':
		return 57, mods, nil
	case '\n', '\r':
		return 28, mods, nil
	case '\t':
		return 15, mods, nil
	case '`':
		return 41, mods, nil
	case '\\':
		return 43, mods, nil
	}
	return 0, 0, fmt.Errorf("unsupported text character %U; prototype keyboard layout is US ASCII", r)
}
func validatePointer(a Action, size [2]int) error {
	if a.Type != "move" && a.Type != "click" && a.Type != "drag" && a.Type != "scroll" {
		return nil
	}
	valid := func(x, y float64) bool {
		return !math.IsNaN(x) && !math.IsNaN(y) && !math.IsInf(x, 0) && !math.IsInf(y, 0) && x >= 0 && y >= 0 && x < float64(size[0]) && y < float64(size[1])
	}
	if !valid(a.X, a.Y) || (a.Type == "drag" && !valid(a.ToX, a.ToY)) {
		return errors.New("outside_window")
	}
	if math.IsNaN(a.Delta) || math.IsInf(a.Delta, 0) || math.Abs(a.Delta) > 1200 {
		return errors.New("invalid_scroll")
	}
	return nil
}

func (b *Broker) input(ctx context.Context, client string, a InputArgs) (any, error) {
	if len(a.Actions) == 0 || len(a.Actions) > 128 {
		return nil, errors.New("actions must contain 1–128 items")
	}
	w, e := b.backend.window(ctx, a.Window)
	if e != nil {
		return nil, e
	}
	if w.XWayland {
		return nil, errors.New("native Wayland window required")
	}
	if a.Revision != w.Revision {
		return nil, errors.New("stale_geometry")
	}
	// Validate all actions before any effects.
	for _, ac := range a.Actions {
		switch ac.Type {
		case "focus", "move", "click", "drag", "scroll":
		case "text":
			if len(ac.Text) > 4096 {
				return nil, errors.New("text too long")
			}
			for _, r := range ac.Text {
				if _, _, e := runeKey(r); e != nil {
					return nil, e
				}
			}
		case "key":
			if _, _, e := keySpec(ac.Key); e != nil {
				return nil, e
			}
		default:
			return nil, errors.New("unsupported action")
		}
		if err := validatePointer(ac, w.Size); err != nil {
			return nil, err
		}
		if ac.DurationMS < 0 || ac.DurationMS > 5000 {
			return nil, errors.New("duration must be 0–5000ms")
		}
		if ac.Button != "" && ac.Button != "left" && ac.Button != "right" && ac.Button != "middle" {
			return nil, errors.New("invalid button")
		}
	}
	grant, request, e := b.permit(client, "control", Scope{"window", w.ID}, &w, "Operate this window")
	if e != nil {
		return nil, e
	}
	if request != "" {
		return map[string]any{"status": "approval_required", "request_id": request}, nil
	}
	b.inputMu.Lock()
	defer b.inputMu.Unlock()
	token := ""
	if grant != nil {
		token = grant.ID
	} else {
		token = randomID()
		e = b.backend.guard(ctx, map[string]any{"op": "authorize", "token": token, "window": w.ID, "milliseconds": 300000})
		if e != nil {
			return nil, e
		}
		defer b.backend.guard(context.Background(), map[string]any{"op": "revoke", "token": token})
	}
	defer b.backend.guard(context.Background(), map[string]any{"op": "release", "token": token, "revision": a.Revision})
	check := func() error {
		if e := ctx.Err(); e != nil {
			return e
		}
		b.mu.Lock()
		defer b.mu.Unlock()
		if _, ok := b.allowedLocked(client, "control", Scope{"window", w.ID}, &w); !ok {
			return errors.New("permission_revoked_or_expired")
		}
		b.lastInputWindow = w.ID
		b.lastInputUntil = b.now().Add(2 * time.Second)
		return nil
	}
	send := func(q map[string]any) error {
		if e := check(); e != nil {
			return e
		}
		q["token"] = token
		q["revision"] = a.Revision
		return b.backend.guard(ctx, q)
	}
	pointer := func(x, y float64, state string, button uint32, scroll float64) error {
		q := map[string]any{"op": "pointer", "x": x, "y": y, "button_state": state, "button": button}
		if scroll != 0 {
			q["scroll"] = scroll
		}
		return send(q)
	}
	key := func(c, mods uint32) error {
		if e := send(map[string]any{"op": "key", "key": c, "mods": mods, "down": true}); e != nil {
			return e
		}
		time.Sleep(12 * time.Millisecond)
		return send(map[string]any{"op": "key", "key": c, "mods": mods, "down": false})
	}
	for i, ac := range a.Actions {
		button := uint32(272)
		if ac.Button == "right" {
			button = 273
		}
		if ac.Button == "middle" {
			button = 274
		}
		switch ac.Type {
		case "focus":
			e = send(map[string]any{"op": "focus"})
		case "move":
			e = pointer(ac.X, ac.Y, "", button, 0)
		case "scroll":
			e = pointer(ac.X, ac.Y, "", button, ac.Delta)
		case "click":
			e = pointer(ac.X, ac.Y, "down", button, 0)
			if e == nil {
				time.Sleep(65 * time.Millisecond)
				e = pointer(ac.X, ac.Y, "up", button, 0)
			}
		case "drag":
			e = pointer(ac.X, ac.Y, "down", button, 0)
			steps := max(2, ac.DurationMS/16)
			if ac.DurationMS == 0 {
				steps = 20
			}
			for j := 1; j <= steps && e == nil; j++ {
				t := float64(j) / float64(steps)
				e = pointer(ac.X+(ac.ToX-ac.X)*t, ac.Y+(ac.ToY-ac.Y)*t, "", button, 0)
				time.Sleep(16 * time.Millisecond)
			}
			if e == nil {
				e = pointer(ac.ToX, ac.ToY, "up", button, 0)
			}
		case "key":
			c, mods, _ := keySpec(ac.Key)
			e = key(c, mods)
		case "text":
			for _, r := range ac.Text {
				c, mods, _ := runeKey(r)
				if e = key(c, mods); e != nil {
					break
				}
				time.Sleep(8 * time.Millisecond)
			}
		}
		if e != nil {
			return nil, fmt.Errorf("action %d: %w", i, e)
		}
	}
	b.mu.Lock()
	b.noteLocked("input", fmt.Sprintf("%d actions → %s", len(a.Actions), w.Title))
	b.mu.Unlock()
	return map[string]any{"status": "completed", "actions": len(a.Actions), "window_id": w.ID}, nil
}

type RecordingInfo struct {
	ID      string    `json:"id"`
	Client  string    `json:"client"`
	Window  string    `json:"window_id"`
	Status  string    `json:"status"`
	Path    string    `json:"path,omitempty"`
	Frames  int       `json:"frames"`
	Started time.Time `json:"started"`
	Error   string    `json:"error,omitempty"`
}
type Recording struct {
	Info   RecordingInfo
	cancel context.CancelFunc
}

func (b *Broker) record(client string, w Window) (any, error) {
	_, rid, e := b.permit(client, "record", Scope{"window", w.ID}, &w, "Record only this window to a local video")
	if e != nil {
		return nil, e
	}
	if rid != "" {
		return map[string]any{"status": "approval_required", "request_id": rid}, nil
	}
	b.mu.Lock()
	if len(b.recordings) >= 32 {
		b.mu.Unlock()
		return nil, errors.New("recording limit reached (32 per server run)")
	}
	active := 0
	for _, r := range b.recordings {
		if r.Info.Status == "recording" {
			active++
		}
	}
	if active >= 2 {
		b.mu.Unlock()
		return nil, errors.New("maximum two recordings")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	id := randomID()
	r := &Recording{Info: RecordingInfo{ID: id, Client: client, Window: w.ID, Status: "recording", Started: b.now()}, cancel: cancel}
	b.recordings[id] = r
	b.mu.Unlock()
	go func() {
		path := filepath.Join(b.backend.Data, id+".mp4")
		cmd := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-y", "-use_wallclock_as_timestamps", "1", "-f", "image2pipe", "-framerate", "5", "-i", "-", "-fps_mode", "vfr", "-vf", "pad=ceil(iw/2)*2:ceil(ih/2)*2", "-c:v", "libx264", "-preset", "veryfast", "-pix_fmt", "yuv420p", "-movflags", "+faststart", path)
		pipe, e := cmd.StdinPipe()
		var log bytes.Buffer
		cmd.Stderr = &log
		if e == nil {
			e = cmd.Start()
		}
		if e == nil {
			ticker := time.NewTicker(200 * time.Millisecond)
			for {
				select {
				case <-ctx.Done():
					goto done
				case <-ticker.C:
					cur, we := b.backend.window(ctx, w.ID)
					if we != nil {
						if ctx.Err() == nil {
							e = we
						}
						goto done
					}
					if cur.Size != w.Size {
						e = errors.New("recording stopped: window resized")
						goto done
					}
					b.mu.Lock()
					_, ok := b.allowedLocked(client, "record", Scope{"window", w.ID}, &cur)
					b.mu.Unlock()
					if !ok {
						goto done
					}
					var frame []byte
					frame, e = b.backend.capture(ctx, cur, 1280)
					if e != nil {
						if ctx.Err() != nil {
							e = nil
						}
						goto done
					}
					b.mu.Lock()
					_, stillAllowed := b.allowedLocked(client, "record", Scope{"window", w.ID}, &cur)
					b.mu.Unlock()
					if !stillAllowed {
						goto done
					}
					if _, e = pipe.Write(frame); e != nil {
						goto done
					}
					b.mu.Lock()
					r.Info.Frames++
					b.mu.Unlock()
				}
			}
		done:
			ticker.Stop()
			_ = pipe.Close()
			wait := make(chan error, 1)
			go func() { wait <- cmd.Wait() }()
			select {
			case we := <-wait:
				if e == nil {
					e = we
				}
			case <-time.After(5 * time.Second):
				_ = cmd.Process.Kill()
				<-wait
				e = errors.New("recorder finalize timeout")
			}
		}
		cancel()
		b.mu.Lock()
		defer b.mu.Unlock()
		r.Info.Status = "stopped"
		if e != nil {
			r.Info.Error = e.Error() + ": " + log.String()
		}
		if r.Info.Frames > 0 && log.Len() == 0 {
			r.Info.Path = path
			_ = os.Chmod(path, 0600)
		}
		b.noteLocked("recording_stopped", id)
	}()
	return map[string]any{"recording_id": id, "status": "recording", "note": "Window-only, sampled at 5fps with wall-clock timestamps; no audio"}, nil
}
func readLine(c net.Conn) ([]byte, error) {
	return bufio.NewReader(io.LimitReader(c, 1<<20)).ReadBytes('\n')
}
