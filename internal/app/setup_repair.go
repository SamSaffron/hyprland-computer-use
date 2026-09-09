package app

import (
	"context"
	"crypto/sha256"
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
	currentExecutable() (bool, error)
	close()
}
type repairResult struct {
	brokerRestarted bool
	guardUnchanged  bool
	brokerRunning   bool
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
	equivalent  func(string, string) (bool, error)
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
		publish:    func() error { return publishNative(stage, root) },
		equivalent: sameFileContents,
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
		if g.Configured {
			return result, errors.New("guard is loaded from Hyprland configuration; remove that plugin entry before setup (setup will not edit your compositor config)")
		}
	}
	return result, nil
}

func sameFileContents(a, b string) (bool, error) {
	infoA, err := os.Stat(a)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	infoB, err := os.Stat(b)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !infoA.Mode().IsRegular() || !infoB.Mode().IsRegular() || infoA.Size() != infoB.Size() {
		return false, nil
	}
	digest := func(path string) ([sha256.Size]byte, error) {
		file, err := os.Open(path)
		if err != nil {
			return [sha256.Size]byte{}, err
		}
		defer file.Close()
		hash := sha256.New()
		if _, err := io.Copy(hash, file); err != nil {
			return [sha256.Size]byte{}, err
		}
		var sum [sha256.Size]byte
		copy(sum[:], hash.Sum(nil))
		return sum, nil
	}
	digestA, err := digest(a)
	if err != nil {
		return false, err
	}
	digestB, err := digest(b)
	return digestA == digestB, err
}

// Build/hash validation happens before this function. No rollback loads an
// outdated plugin or revives old grants: after disruption, failures stay closed.
func repairNative(ctx context.Context, stage, root string, ops repairOps) (result repairResult, err error) {
	if err = ctx.Err(); err != nil {
		return result, err
	}
	existing, err := inspectGuard(ctx, stage, ops)
	if err != nil {
		return result, err
	}
	if ops.selectInput != nil {
		if err := ops.selectInput(existing.IndependentSeatHookAvailable); err != nil {
			return result, err
		}
	}

	plugin := filepath.Join(stage, "build", "guard.so")
	if len(existing.Guards) == 1 {
		result.guardUnchanged, err = ops.equivalent(existing.Guards[0].Path, plugin)
		if err != nil {
			return result, fmt.Errorf("compare active and desired compositor guards: %w", err)
		}
		if !result.guardUnchanged && existing.Guards[0].RestartRequired {
			return result, errors.New(`RESULT: HYPRLAND RESTART REQUIRED
  Reason: the active independent-seat guard differs from the requested build and cannot be safely replaced while Hyprland is running
  Active guard: unchanged and still loaded
  Broker: not stopped by setup
  New build: retained but not active

NEXT:
  1. Save your work.
  2. Fully exit your Hyprland session and log back in.
     A config reload is not enough; do not manually hot-unload the plugin.
  3. Rerun the same hyprland-computer-use setup command.
  4. Follow the RESULT and NEXT sections printed by that run.`)
		}
	}

	peer, err := ops.peer()
	if err != nil {
		return result, err
	}
	if peer != 0 && (peer != existing.PID || len(existing.Guards) == 0) {
		return result, errors.New("guard socket does not match this Hyprland session; nothing was stopped")
	}
	broker, err := ops.broker()
	if err != nil {
		return result, err
	}
	if broker != nil {
		defer broker.close()
	}

	if result.guardUnchanged {
		if err = ops.status(ctx); err != nil {
			return result, fmt.Errorf("active guard is not ready: %w", err)
		}
		if broker == nil {
			if err = ops.publish(); err != nil {
				return result, err
			}
			return result, nil
		}
		current, err := broker.currentExecutable()
		if err != nil {
			return result, fmt.Errorf("compare running and requested broker executables: %w", err)
		}
		if current {
			if err = ops.publish(); err != nil {
				return result, err
			}
			result.brokerRunning = true
			return result, nil
		}
	}

	if broker != nil {
		if err = broker.stop(ctx); err != nil {
			return result, err
		}
		defer func() {
			if err != nil {
				err = fmt.Errorf("setup incomplete; broker was stopped and grants cleared: %w", err)
			}
		}()
	}
	lock, err := ops.lock()
	if err != nil {
		return result, err
	}
	defer lock.Close()
	// Reject an uncoordinated legacy restart before replacing its guard.
	competing, err := ops.broker()
	if err != nil {
		return result, err
	}
	if competing != nil {
		competing.close()
		return result, errors.New("another broker appeared during repair; guard left unchanged")
	}
	if !result.guardUnchanged {
		if len(existing.Guards) == 1 {
			fmt.Fprintln(os.Stderr, "Replacing loaded compositor guard.")
			if err = pluginAction(ctx, ops, "unload", existing.Guards[0].Path); err != nil {
				return result, err
			}
		}
		present, err := pluginPresent(ctx, ops, "computer-use-guard")
		if err != nil {
			return result, err
		}
		if present {
			return result, errors.New("guard is still loaded; refusing to load a second copy")
		}
		if err = pluginAction(ctx, ops, "load", plugin); err != nil {
			return result, err
		}
		present, err = pluginPresent(ctx, ops, "computer-use-guard")
		if err != nil {
			return result, err
		}
		if !present {
			return result, errors.New("new guard was not registered")
		}
		peer, err = ops.peer()
		if err != nil {
			return result, err
		}
		if peer != existing.PID {
			return result, errors.New("new guard socket does not belong to the inspected compositor")
		}
		if err = ops.status(ctx); err != nil {
			return result, fmt.Errorf("new guard is not ready: %w", err)
		}
	}
	if err = ops.publish(); err != nil {
		return result, err
	}
	// New brokers acquire the same lock during their entire lifetime.
	if err = lock.Close(); err != nil {
		return result, err
	}
	if broker != nil {
		if err = broker.restart(ctx, root); err != nil {
			return result, err
		}
		result.brokerRestarted = true
		result.brokerRunning = true
	}
	return result, nil
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
