package app

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	assets "github.com/sam-saffron-jarvis/hyprland-computer-use"
)

var nativeBundle = assets.Native

func nativeInstallRoot() (string, error) {
	home := os.Getenv("XDG_DATA_HOME")
	if home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		home = filepath.Join(userHome, ".local", "share")
	}
	if !filepath.IsAbs(home) {
		return "", errors.New("XDG_DATA_HOME must be an absolute path")
	}
	return filepath.Join(home, "hyprland-computer-use", "native"), nil
}

func extractNative(dir string) error {
	return fs.WalkDir(nativeBundle, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		dest := filepath.Join(dir, path)
		if entry.IsDir() {
			return os.MkdirAll(dest, 0700)
		}
		data, err := nativeBundle.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dest, data, 0600)
	})
}

const nativeDependencies = "Install native build dependencies first. On Arch:\n  sudo pacman -S --needed make gcc pkgconf hyprland nlohmann-json wayland libxkbcommon libei\nHyprland headers must match the running compositor exactly. Setup does not install system packages."

func runSetup(args []string) error {
	f := flag.NewFlagSet("setup", flag.ContinueOnError)
	inputMode := f.String("input-mode", "auto", "auto (prefer independent seat) or focus-borrowing")
	legacySeat := f.Bool("experimental-seat", false, "deprecated alias for --input-mode=auto")
	buildOnly := f.Bool("build-only", false, "build/install without touching the compositor or broker")
	load := f.Bool("load", true, "load/repair the plugin (default; --load=false is a legacy alias for --build-only)")
	if err := f.Parse(args); err != nil {
		return err
	}
	if *legacySeat {
		*inputMode = "auto"
	}
	if *inputMode != "auto" && *inputMode != "focus-borrowing" {
		return errors.New("input-mode must be auto or focus-borrowing")
	}
	if f.NArg() != 0 {
		return errors.New("unexpected setup arguments")
	}
	if os.Geteuid() == 0 {
		return errors.New("run setup as your desktop user, not root")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	var dir string
	if !*buildOnly && *load {
		if os.Getenv("HYPRLAND_INSTANCE_SIGNATURE") == "" || os.Getenv("WAYLAND_DISPLAY") == "" {
			return errors.New("setup must run inside your Hyprland desktop session; use --build-only for an offline build")
		}
		var err error
		dir, err = runtimeDir()
		if err != nil {
			return err
		}
		if _, err := exec.LookPath("hyprctl"); err != nil {
			return fmt.Errorf("setup requires hyprctl (or use --build-only): %w", err)
		}
	}
	for _, tool := range []string{"make", "g++", "pkg-config"} {
		if _, err := exec.LookPath(tool); err != nil {
			return fmt.Errorf("missing build tool %s: %w\n%s", tool, err, nativeDependencies)
		}
	}
	if _, err := command(ctx, "pkg-config", "--exists", "hyprland", "libeis-1.0"); err != nil {
		return fmt.Errorf("native development packages unavailable: %w\n%s", err, nativeDependencies)
	}
	root, err := nativeInstallRoot()
	if err != nil {
		return err
	}
	if err = os.MkdirAll(root, 0700); err != nil {
		return err
	}
	setupLock, err := lifecycleLock(filepath.Join(root, "setup.lock"))
	if err != nil {
		return err
	}
	defer setupLock.Close()
	// Never overwrite a shared library that may still be loaded in Hyprland.
	stage, err := os.MkdirTemp(root, "build-")
	if err != nil {
		return err
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(stage)
		}
	}()
	if err = extractNative(stage); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "Building bundled compositor components...")
	build := func(target string) error {
		cmd := exec.CommandContext(ctx, "make", target, "--silent")
		cmd.Dir = stage
		cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
		return cmd.Run()
	}
	if err = build("setup-native"); err != nil {
		return fmt.Errorf("native build failed: %w\n%s", err, nativeDependencies)
	}
	preferSeat := false
	if *inputMode == "auto" {
		if err = build("independent-seat"); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			fmt.Fprintln(os.Stderr, "Independent seat build unavailable; using guarded focus borrowing:", err)
		} else {
			preferSeat = true
		}
	}
	plugin := filepath.Join(stage, "build", "guard.so")
	selectPlugin := func(available bool) error {
		if preferSeat && available {
			if err := os.Rename(filepath.Join(stage, "build", "guard-seat.so"), plugin); err != nil {
				return err
			}
			fmt.Fprintln(os.Stderr, "Automatic input: independent seat preferred, guarded focus borrowing for unsupported clients. Plugin updates require a Hyprland restart; never hot-unload.")
		} else {
			fmt.Fprintln(os.Stderr, "Using guarded focus borrowing; independent seat unavailable or disabled.")
		}
		return nil
	}
	loaded, restarted := !*buildOnly && *load, false
	if loaded {
		header, err := command(ctx, filepath.Join(stage, "build", "header-version"))
		if err != nil {
			return err
		}
		version, err := command(ctx, "hyprctl", "-j", "version")
		if err != nil {
			return err
		}
		if err = checkHyprlandVersion(header, version); err != nil {
			return err
		}
		// Once loading is attempted, never delete a file that might still be mapped.
		keep = true
		ops := localRepairOps(dir, stage, root)
		ops.selectInput = selectPlugin
		restarted, err = repairNative(ctx, stage, root, ops)
		if err != nil {
			return fmt.Errorf("%w\nBuild retained at %s", err, stage)
		}
	} else {
		if err = selectPlugin(true); err != nil {
			return err
		}
		if err = publishNative(stage, root); err != nil {
			return err
		}
	}
	keep = true
	printSetupNextSteps(os.Stderr, plugin, loaded, restarted)

	return nil
}

func checkHyprlandVersion(header, version []byte) error {
	var running struct {
		Commit string `json:"commit"`
	}
	if err := json.Unmarshal(version, &running); err != nil {
		return fmt.Errorf("cannot read running Hyprland version: %w", err)
	}
	hash := strings.TrimSpace(string(header))
	if hash == "" || running.Commit == "" || hash != running.Commit {
		return fmt.Errorf("refusing to load plugin: header build %q does not match running Hyprland %q; install matching headers or restart into the matching compositor, then rerun setup", hash, running.Commit)
	}
	return nil
}

func printSetupNextSteps(w io.Writer, plugin string, loaded, restarted bool) {
	if !loaded {
		fmt.Fprintf(w, "Build installed. Load/repair in your desktop session with: hyprland-computer-use setup\nManual load: hyprctl plugin load %s\n", shellQuote(plugin))
		return
	}
	if restarted {
		fmt.Fprintln(w, "Setup complete. Reconnect your MCP client; open the console from the tray. New permission approval is required.")
	} else {
		fmt.Fprintln(w, "Setup complete. Start the broker: hyprland-computer-use serve\nThen click its tray icon to open the permission console.")
	}

}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
