package app

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

func socketUnavailable(err error) bool {
	return errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ECONNREFUSED)
}

func brokerConnectionError(socket string, err error) error {
	hint := "Check socket permissions and run as the same desktop user with the same XDG_RUNTIME_DIR as the broker."
	if socketUnavailable(err) {
		hint = "The broker may not be running. `mcp` only connects to an existing broker; it does not start one.\nStart `hyprland-computer-use serve` in another terminal in your Hyprland session, then retry. If serve fails, follow its startup diagnostics.\nIf the broker is already running, check XDG_RUNTIME_DIR and the --socket path."
	}
	return fmt.Errorf("cannot connect to the computer-use broker at %q: %w\n%s", socket, err, hint)
}

func guardStartupError(err error) error {
	hint := "Check the plugin status with `hyprctl plugin list` and ensure the broker is running as your desktop user in the same Hyprland session."
	if socketUnavailable(err) {
		hint = "The required compositor guard plugin may not be loaded in this session.\nRun `hyprland-computer-use setup` to build/load or repair the bundled plugin, then retry serve.\nBuild against headers matching the running Hyprland build exactly; do not load a plugin built for a different version.\nRun as your desktop user in the same Hyprland session (including XDG_RUNTIME_DIR and HYPRLAND_INSTANCE_SIGNATURE)."
	}
	return fmt.Errorf("cannot start broker: compositor guard check failed: %w\n%s\nThe compositor plugin is required; global input fallback is disabled.", err, hint)
}
