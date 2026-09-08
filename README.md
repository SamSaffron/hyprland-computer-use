# Hyprland Computer Use

A standalone **Hyprland computer-use MCP server** with a **Quickshell permission console** and a real system-tray item.

Built and tested in an isolated GPU-backed Hyprland session. See [SECURITY.md](SECURITY.md) for the trust model and [TESTING.md](TESTING.md) for verified behavior and current limitations.

## The experience

- **Window metadata is free:** IDs, titles, app, workspace, geometry and revisions. Listing does not grant access to pixels or input; it works even while paused. Titles can contain sensitive document names—this is an intentional usability trade-off.
- **Approve** is the startup default. An MCP request creates a local approval card, not authority.
- Grant **observation of workspace 1**, or **control of one window**, for a duration you choose (1–60 minutes in the UI).
- Observation of a workspace explicitly includes newly opened windows while they remain on that workspace. Window control never inherits to another toplevel.
- Active grants show a countdown, a target-window outline, and a revoke button.
- **YOLO** is a deliberate, two-click local selection. It skips approval prompts for the exposed tools; it does not bypass pause, supervisor liveness, validation, or compositor targeting.
- **Pause / revoke all** stop input and recording. Losing the last supervisor connection or its heartbeat pauses the broker and clears grants.
- No MCP tool directly approves requests, changes modes, exposes UI RPC, or executes arbitrary shell commands. **Controlling a terminal still grants shell authority** and can transitively bypass this boundary; see [SECURITY.md](SECURITY.md).

## Components

```text
MCP client ── stdio bridge ── private MCP socket ── Go broker
                                                    │
                  Quickshell console ── UI socket ───┤
                  existing system tray ── D-Bus ─────┤
                                                    ├─ grim: actual toplevel screenshots
                                                    ├─ ffmpeg: window-only recording
                                                    └─ Hyprland plugin: scoped seat input
```

The tray item uses the StatusNotifierItem protocol and works with a tray host such as Waybar or Quickshell. `COMPUTER_USE_DEMO_TRAY=1` enables a small Quickshell tray host for an otherwise bare test compositor. Normal desktop use should use its existing tray host.

### Why a compositor plugin?

“Focus window A, then inject global input” has a race. The compositor guard instead validates a short-lived lease, live window object, root surface, visibility, and geometry revision **inside Hyprland's event loop**, then delivers input to that surface. It verifies the seat accepted the intended focus before sending events. There is **no global-input fallback**.

The plugin has a trusted same-UID local control channel used by the broker; it does not authenticate one particular process. It checks expiry independently, releases its tracked held inputs on revocation, rejects stale geometry/out-of-bounds pointer coordinates, and invalidates leases when the bound window/surface disappears. Its exact Hyprland build hash must match the headers used to build it.

The Quickshell outline is an **advisory reflection of broker state**, not the security enforcement mechanism. Enforcement is in the broker and plugin. See [SECURITY.md](SECURITY.md) for the trust boundary and important exclusions.

The executable is **`hyprland-computer-use`**. Use **`computer-use`** as the MCP server name in your client configuration.

## MCP tools

| Tool | Purpose |
|---|---|
| `computer_status` | Mode, pause state, this client's grants and requests |
| `request_permission` | Request a specific capability/scope; never grants it |
| `wait_for_permission` | Wait up to 120 seconds for a local decision, then retry |
| `list_windows` | Free metadata discovery; optional workspace filter, no pixels |
| `view_window` | PNG of the actual toplevel, plus its geometry revision |
| `input_window` | Focus, move, click, drag, scroll, key chords, text; max 128 actions |
| `record_window` | Separately approved, window-only local MP4 recording |
| `stop_recording` | Stop a recording owned by this MCP client |
| `list_recordings` | This client's recording status and local output paths |
| `launch_application` | Allowlisted Chromium/Pinta launch; no inherited control grant |

Example request:

```json
{
  "capability": "observe",
  "scope": { "kind": "workspace", "id": "1" },
  "reason": "Find the browser window for this task"
}
```

Example input after a control grant:

```json
{
  "window_id": "18000002",
  "revision": "21,21,673,1158",
  "actions": [
    { "type": "key", "key": "CTRL+L" },
    { "type": "text", "text": "https://example.com" },
    { "type": "key", "key": "ENTER" }
  ]
}
```

Coordinates are **window-local logical pixels**, not scaled screenshot pixels. Read the `logical_size` and revision returned by `view_window`. The client must update its coordinates after geometry changes. A revision protects against compositor geometry changes, **not arbitrary in-app content changes**.

## Optional HTTP / built-in OAuth

Local stdio remains the default—no OAuth setup and no network listener. HTTP and the built-in provider are **opt-in modes**:

```sh
# Local stdio (unchanged)
hyprland-computer-use serve

# Loopback-only HTTP, no OAuth
hyprland-computer-use serve --http 127.0.0.1:8099

# OAuth behind your HTTPS reverse proxy; no external identity provider
hyprland-computer-use serve --http 127.0.0.1:8099 --oauth --public-url https://desktop.example.com
```

OAuth clients discover the provider, dynamically register, and use authorization code + PKCE S256. The desktop user approves the connection in Quickshell. **Authentication allows MCP access, not window viewing/control.** Free metadata is available only after connection authentication when OAuth is enabled. The provider supports public/confidential clients and rotating refresh tokens, bounded by the one-hour locally approved connection lifetime.

See [AUTH.md](AUTH.md) for direct TLS, endpoints, persistence, revocation, compatibility and limits. No auth is bolted onto the stdio bridge.

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

### Omarchy-style bar

`examples/waybar/` supplies a charcoal, JetBrains Mono bar with workspaces, clock, **Share window**, and a real StatusNotifierItem tray. It is an inspired theme, not an Omarchy installation.

```sh
sudo pacman -S --needed waybar ttf-jetbrains-mono-nerd
export PATH="$PWD/build:$PATH"
waybar -c "$PWD/examples/waybar/config.jsonc" -s "$PWD/examples/waybar/style.css"
```

Run it on the same session D-Bus as the broker. Leave `COMPUTER_USE_DEMO_TRAY` unset when using Waybar; the small built-in demo tray is not needed. No host autostart or shortcut is installed by `make`.

## Build on Arch

The compositor plugin is intentionally version-coupled. Build against the headers/libraries belonging to the running Hyprland package.

```sh
sudo pacman -S --needed go make gcc pkgconf hyprland nlohmann-json \
  wayland libxkbcommon quickshell grim ffmpeg
make
make test
```

No binaries or dependencies are vendored except the small Wayland keyboard/pointer protocol XML files with its original license. Go dependencies are pinned by `go.mod` / `go.sum`.

## Run in an existing test session

Run as your desktop user, with its `XDG_RUNTIME_DIR`, `WAYLAND_DISPLAY`, `HYPRLAND_INSTANCE_SIGNATURE`, and session D-Bus environment. **Do not run the broker as root.**

```sh
hyprctl plugin load "$PWD/build/guard.so"
./build/hyprland-computer-use serve
```

In another terminal in the same desktop session:

```sh
qs -p "$PWD/quickshell"
```

Configure an MCP client to use:

```json
{
  "mcpServers": {
    "computer-use": {
      "command": "/absolute/path/to/hyprland-computer-use/build/hyprland-computer-use",
      "args": ["mcp"]
    }
  }
}
```

The bridge and broker must have access to the same private runtime socket. For a remote agent, forward stdio over SSH to an authorized desktop-user command; non-loopback HTTP requires OAuth and an HTTPS public origin; see [AUTH.md](AUTH.md).

Runtime sockets are under `$XDG_RUNTIME_DIR/computer-use/` (directory `0700`, sockets `0600`). Recordings and JSONL audit entries default to `$XDG_STATE_HOME/computer-use/`, or `~/.local/state/computer-use/`. Recordings are sensitive: no automatic upload and no automatic deletion/retention service is provided.

Local emergency/admin command, **not an MCP tool**:

```sh
./build/hyprland-computer-use ui '{"op":"pause","paused":true}'
```

A desktop owner can bind that command to an emergency shortcut. No global binding or autostart service is installed automatically.

Stop the broker/Quickshell, then unload the plugin when finished:

```sh
hyprctl plugin unload "$PWD/build/guard.so"
```

## Tests and demo

- `make test`: unit/race tests covering permission separation, scope, duration, revocation, UI loss, YOLO pause semantics, pointer bounds, keys, and socket safety; plus `go vet`.
- `scripts/live-test.py`: **destructive, opt-in disposable-session tests**, exercising the actual MCP protocol, compositor guard, toplevel capture, pointer/key input, recording finalization, expiry and disconnect. Requires the broker, Quickshell, a Kitty window and a Pinta window. It uses a separate local UI connection to simulate the human. Set `COMPUTER_USE_DISPOSABLE=1` only in a disposable compositor.
- `scripts/mcp-probe.py`: a persistent real stdio MCP client used to make the demo. Its test-only command socket cannot grant approvals. `scripts/probe-call.py` sends it a tool call.

The demonstration uses the old global input harness **only to simulate the local human clicking approval controls**. Computer control is issued by the new MCP client. Test approvals are not represented as real user approvals on a live desktop.

## Current limitations

- **Native Wayland root toplevels only for input.** XWayland input and popup/subsurface routing are not implemented. Separate transient toplevels require their own grants.
- The persistent helper supplies virtual keyboard and pointer seat capabilities; the guard delivers input directly. It selects the dedicated US keyboard and an available trusted local virtual pointer, never a physical pointer.
- Text is **US-layout ASCII**. Unsupported Unicode is rejected rather than silently mistyped. Physical-keyboard/layout arbitration needs more work before everyday desktop use.
- No clipboard, arbitrary file, shell, accessibility-tree, or privileged-operation tool.
- A controlled terminal or browser retains the application's existing powers. This is not application sandboxing or semantic authorization of purchases/deletions/sudo. There is no sudo broker.
- `launch_application` relies on the application's normal launch/sandbox configuration. Chromium inside restrictive containers may need setup outside this tool; the MCP API does not offer `--no-sandbox`.
- Window recording samples actual toplevel images at approximately 5 fps with wall-clock timestamps; no audio. Maximum two concurrent recordings and 32 per broker run, with a ten-minute per-recording limit. Odd dimensions are padded for H.264. Resizing stops the recording rather than producing corrupt output.
- No secure indicator resistant to a compromised compositor or malicious same-UID process. No full multi-monitor/mixed-DPI, session-lock, or hostile-client audit has been completed.

## License

[MIT](LICENSE), copyright Sam Saffron. Vendored protocol notices are retained in the XML; see [THIRD_PARTY.md](THIRD_PARTY.md).
