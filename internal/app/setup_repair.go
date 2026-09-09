package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type repairBroker interface {
	stop(context.Context) error
	restart(context.Context, string) error
	close()
}
type loadedPlugin struct {
	Name   string `json:"name"`
	Author string `json:"author"`
}
type guardLocation struct {
	Path            string `json:"path"`
	Configured      bool   `json:"configured"`
	RestartRequired bool   `json:"restart_required"`
}
type inspectorResult struct {
	Inspector                    string          `json:"inspector"`
	PID                          int             `json:"pid"`
	Guards                       []guardLocation `json:"guards"`
	IndependentSeatHookAvailable bool            `json:"independent_seat_hook_available"`
}

// Injectable boundaries keep repair tests entirely off the real desktop.
type repairOps struct {
	selectInput func(bool) error
	call        func(context.Context, ...string) ([]byte, error)
	broker      func() (repairBroker, error)
	peer        func() (int, error)
	lock        func() (io.Closer, error)
	status      func(context.Context) error
	publish     func() error
}

func localRepairOps(dir, stage, root string) repairOps {
	return repairOps{
		call: func(ctx context.Context, args ...string) ([]byte, error) { return command(ctx, "hyprctl", args...) },
		broker: func() (repairBroker, error) {
			p, err := findBroker(dir)
			if p == nil {
				return nil, err
			}
			return p, err
		},
		peer: func() (int, error) {
			c, err := socketPeer(filepath.Join(dir, "guard.sock"))
			if c == nil {
				return 0, err
			}
			if c.Uid != uint32(os.Getuid()) {
				return 0, errors.New("guard socket belongs to another user")
			}
			return int(c.Pid), err
		},
		lock: func() (io.Closer, error) { return lifecycleLock(filepath.Join(dir, "broker.lock")) },
		status: func(ctx context.Context) error {
			return (&Desktop{Dir: dir}).guard(ctx, map[string]any{"op": "status"})
		},
		publish: func() error { return publishNative(stage, root) },
	}
}
func pluginAction(ctx context.Context, ops repairOps, action, path string) error {
	out, err := ops.call(ctx, "plugin", action, path)
	if err != nil {
		return fmt.Errorf("plugin %s failed: %w", action, err)
	}
	// hyprctl sometimes returns status 0 even when the operation failed.
	if strings.TrimSpace(string(out)) != "ok" {
		return fmt.Errorf("plugin %s failed: %s", action, strings.TrimSpace(string(out)))
	}
	return nil
}
func pluginPresent(ctx context.Context, ops repairOps, name string) (bool, error) {
	out, err := ops.call(ctx, "-j", "plugin", "list")
	if err != nil {
		return false, err
	}
	var plugins []loadedPlugin
	if err := json.Unmarshal(out, &plugins); err != nil {
		return false, err
	}
	if plugins == nil {
		return false, errors.New("invalid plugin list")
	}
	count := 0
	for _, p := range plugins {
		if p.Name == name && p.Author == "Computer Use" {
			count++
		}
	}
	if count > 1 {
		return false, fmt.Errorf("multiple %s plugins loaded; refusing ambiguous repair", name)
	}
	return count == 1, nil
}

func inspectGuard(ctx context.Context, stage string, ops repairOps) (result inspectorResult, err error) {
	path := filepath.Join(stage, "build", "setup-inspector.so")
	name := "computer-use-setup-inspect-" + filepath.Base(stage)
	// Always attempt cleanup, including cancellation/load failure. The staged
	// files are retained by the caller once any compositor load is attempted.
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		present, e := pluginPresent(cleanup, ops, name)
		if e == nil && present {
			e = pluginAction(cleanup, ops, "unload", path)
		}
		if e == nil {
			present, e = pluginPresent(cleanup, ops, name)
			if e == nil && present {
				e = errors.New("inspector still loaded")
			}
		}
		if e != nil {
			err = errors.Join(err, fmt.Errorf("temporary inspector cleanup failed; retained at %s: %w", path, e))
		}
	}()
	if err = pluginAction(ctx, ops, "load", path); err != nil {
		return result, err
	}
	out, err := ops.call(ctx, name)
	if err != nil {
		return result, err
	}
	if err = json.Unmarshal(out, &result); err != nil {
		return result, fmt.Errorf("read exact guard path: %w", err)
	}
	if result.Inspector != path || result.PID <= 0 || result.Guards == nil {
		return result, errors.New("invalid setup inspector response")
	}
	if len(result.Guards) > 1 {
		return result, errors.New("multiple compositor guards loaded; refusing ambiguous repair")
	}
	for _, g := range result.Guards {
		if g.Path == "" || g.Path == path {
			return result, errors.New("invalid loaded guard path")
		}
		if g.RestartRequired {
			return result, errors.New("independent seat is loaded: save your work and restart Hyprland before updating or rolling back; do not hot-unload this plugin")
		}
		if g.Configured {
			return result, errors.New("guard is loaded from Hyprland configuration; remove that plugin entry before setup (setup will not edit your compositor config)")
		}
	}
	return result, nil
}

// Build/hash validation happens before this function. No rollback loads an
// outdated plugin or revives old grants: after disruption, failures stay closed.
func repairNative(ctx context.Context, stage, root string, ops repairOps) (restarted bool, err error) {
	if err = ctx.Err(); err != nil {
		return false, err
	}
	existing, err := inspectGuard(ctx, stage, ops)
	if err != nil {
		return false, err
	}
	if ops.selectInput != nil {
		if err := ops.selectInput(existing.IndependentSeatHookAvailable); err != nil {
			return false, err
		}
	}
	peer, err := ops.peer()
	if err != nil {
		return false, err
	}
	if peer != 0 && (peer != existing.PID || len(existing.Guards) == 0) {
		return false, errors.New("guard socket does not match this Hyprland session; nothing was stopped")
	}
	broker, err := ops.broker()
	if err != nil {
		return false, err
	}
	if broker != nil {
		defer broker.close()
		if err = broker.stop(ctx); err != nil {
			return false, err
		}
		defer func() {
			if err != nil {
				err = fmt.Errorf("setup incomplete; broker was stopped and grants cleared: %w", err)
			}
		}()
	}
	lock, err := ops.lock()
	if err != nil {
		return false, err
	}
	defer lock.Close()
	// Reject an uncoordinated legacy restart before replacing its guard.
	competing, err := ops.broker()
	if err != nil {
		return false, err
	}
	if competing != nil {
		competing.close()
		return false, errors.New("another broker appeared during repair; guard left unchanged")
	}
	if len(existing.Guards) == 1 {
		fmt.Fprintln(os.Stderr, "Replacing loaded compositor guard.")
		if err = pluginAction(ctx, ops, "unload", existing.Guards[0].Path); err != nil {
			return false, err
		}
	}
	present, err := pluginPresent(ctx, ops, "computer-use-guard")
	if err != nil {
		return false, err
	}
	if present {
		return false, errors.New("guard is still loaded; refusing to load a second copy")
	}
	plugin := filepath.Join(stage, "build", "guard.so")
	if err = pluginAction(ctx, ops, "load", plugin); err != nil {
		return false, err
	}
	present, err = pluginPresent(ctx, ops, "computer-use-guard")
	if err != nil {
		return false, err
	}
	if !present {
		return false, errors.New("new guard was not registered")
	}
	peer, err = ops.peer()
	if err != nil {
		return false, err
	}
	if peer != existing.PID {
		return false, errors.New("new guard socket does not belong to the inspected compositor")
	}
	if err = ops.status(ctx); err != nil {
		return false, fmt.Errorf("new guard is not ready: %w", err)
	}
	if err = ops.publish(); err != nil {
		return false, err
	}
	// New brokers acquire the same lock during their entire lifetime.
	if err = lock.Close(); err != nil {
		return false, err
	}
	if broker != nil {
		if err = broker.restart(ctx, root); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}
func publishNative(stage, root string) error {
	link := filepath.Join(stage, "current-link")
	if err := os.Symlink(stage, link); err != nil {
		return err
	}
	if err := os.Rename(link, filepath.Join(root, "current")); err != nil {
		_ = os.Remove(link)
		return err
	}
	return nil
}
