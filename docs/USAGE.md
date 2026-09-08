# Permissions and window sharing

[Quick start](../README.md) · [Setup](SETUP.md) · [Usage](USAGE.md) · [Tool reference](REFERENCE.md) · [Development](DEVELOPMENT.md)

## The experience

- **Window metadata is free:** IDs, titles, app, workspace, geometry and revisions. Listing does not grant access to pixels or input; it works even while paused. Titles can contain sensitive document names—this is an intentional usability trade-off.
- **Approve** is the startup default. An MCP request creates a local approval card, not authority.
- Grant **observation of workspace 1**, or **control of one window**, for a duration you choose (1–60 minutes in the UI).
- Observation of a workspace explicitly includes newly opened windows while they remain on that workspace. Window control never inherits to another toplevel.
- Active grants show a countdown, a target-window outline, and a revoke button.
- **YOLO** is a deliberate, two-click local selection. It skips approval prompts for the exposed tools; it does not bypass pause, supervisor liveness, validation, or compositor targeting.
- **Pause / revoke all** stop input and recording. Losing the last supervisor connection or its heartbeat pauses the broker and clears grants.
- No MCP tool directly approves requests, changes modes, exposes UI RPC, or executes arbitrary shell commands. **Controlling a terminal still grants shell authority** and can transitively bypass this boundary; see [SECURITY.md](../SECURITY.md).

## Proactively share a window

The local user can grant **viewing + control for five minutes**, without an agent asking first:

```sh
hyprland-computer-use share
hyprland-computer-use share --seconds 600
hyprland-computer-use share --view-only
hyprland-computer-use share --client CONNECTION_ID --seconds 300
```

Click a labelled window in the local picker. **Escape cancels**; opening the picker does not grant anything. The console also has a **Share** button. One connected MCP client is selected automatically; with multiple clients the user must choose the recipient. No connected client means no share—nothing is left waiting for an unknown future connection. The MCP client learns its grants through `computer_status`; this does not inject a screenshot or message into its conversation.

The picker expires after 60 seconds and checks the selected window's identity, visibility and geometry again before granting. Pause, revocation, expiry and disconnect apply normally. Recording is still separate. `--view-only` grants no input. The picker currently covers the first Quickshell screen; multi-monitor picker behavior is not claimed.

Define the shortcut in Hyprland's Lua config (tested on 0.56.2):

```lua
hl.bind("SUPER + CTRL + S", hl.dsp.exec_cmd("/absolute/path/to/hyprland-computer-use/build/hyprland-computer-use share"))
```

For older Hyprland text configuration, the equivalent is `bind = SUPER CTRL, S, exec, /absolute/path/to/hyprland-computer-use/build/hyprland-computer-use share`; that syntax is not the tested Lua configuration.

### Optional Waybar example

The permission console and window picker use **Quickshell**. Use your existing desktop bar and StatusNotifierItem tray; Waybar is not required.

For a bare test session, `examples/waybar/` provides an optional bar with workspaces, a clock, a **Share window** button, and a tray.

```sh
sudo pacman -S --needed waybar ttf-jetbrains-mono-nerd
export PATH="$PWD/build:$PATH"
waybar -c "$PWD/examples/waybar/config.jsonc" -s "$PWD/examples/waybar/style.css"
```

Run it on the same session D-Bus as the broker. Leave `COMPUTER_USE_DEMO_TRAY` unset when using Waybar; the small built-in demo tray is not needed. No host autostart or shortcut is installed by `make`.
