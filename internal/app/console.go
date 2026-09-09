package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"

	assets "github.com/samsaffron/hyprland-computer-use"
)

var consoleBundle = assets.Console

func extractConsole(dir string) error {
	entries, err := consoleBundle.ReadDir("quickshell")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		data, err := consoleBundle.ReadFile("quickshell/" + entry.Name())
		if err != nil {
			return err
		}
		if err = os.WriteFile(filepath.Join(dir, entry.Name()), data, 0600); err != nil {
			return err
		}
	}
	return nil
}

func runConsole(args []string) error {
	f := flag.NewFlagSet("console", flag.ContinueOnError)
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() != 0 {
		return errors.New("unexpected console arguments")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	return runConsoleContext(ctx)
}

// Shared by the explicit CLI command and local tray activation. The broker's
// context owns tray-launched consoles, including temporary QML cleanup.
func runConsoleContext(ctx context.Context) error {
	qs, err := exec.LookPath("qs")
	if err != nil {
		return fmt.Errorf("the permission console requires Quickshell (on Arch: sudo pacman -S --needed quickshell): %w", err)
	}
	runtime, err := runtimeDir()
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp(runtime, "console-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	if err = extractConsole(dir); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, qs, "-p", dir)
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err = cmd.Run(); err != nil && ctx.Err() == nil {
		return fmt.Errorf("permission console exited: %w", err)
	}
	return nil
}
