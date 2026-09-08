# Hyprland Computer Use

A standalone **Hyprland computer-use MCP server** with a **Quickshell permission console** and a real system-tray item.

Built and tested in an isolated GPU-backed Hyprland session. See [SECURITY.md](SECURITY.md) for the trust model and [TESTING.md](TESTING.md) for verified behavior and current limitations.

## Quick start: one binary

**You only need to copy `hyprland-computer-use` to the target desktop.** It bundles the native compositor-plugin source, build recipes, Wayland protocol XML and Quickshell UI. You do not need the source checkout or separate QML files there. System dependencies are still required.

If you are building the executable yourself, install Go and make, then run `make build/hyprland-computer-use` in this checkout. The distributable is `build/hyprland-computer-use`.

### 1. Install system dependencies

On Arch:

```sh
sudo pacman -S --needed make gcc pkgconf hyprland nlohmann-json \
  wayland libxkbcommon libei quickshell grim ffmpeg
```

The plugin needs headers matching the **exact running Hyprland build**. This project is tested against Hyprland **0.56.2**, not every release; see [TESTING.md](TESTING.md).

### 2. Build and load the bundled plugin

Open a terminal **inside your Hyprland desktop session**, as your normal desktop user—not root. The commands below assume the executable is on your `PATH`; otherwise replace `hyprland-computer-use` with its path, such as `./build/hyprland-computer-use`.

```sh
hyprland-computer-use setup
```

This builds and loads the bundled plugin. **For updates, run the same command:** it replaces an already-loaded guard and restarts an existing broker with its previous options. Grants are cleared; reconnect your MCP client afterward. No package installation, autostart service, or compositor-config edit is performed. Use `setup --build-only` for an offline build.

### 3. Start the broker and permission console

On first installation (skip this if setup reported that it restarted your broker):

```sh
hyprland-computer-use serve
```

Leave it running. Look for **`Computer Use ready; approve mode`**. **Click the monitor-and-pointer tray icon to open the permission console**; the broker launches the bundled UI if it is not already connected. Right-click opens a menu with **Open permission console** (some tray hosts open the console directly).

If your desktop has no tray host, or you prefer launching it manually, use a second terminal in the same desktop session:

```sh
hyprland-computer-use console
```

Keep the console running while using computer control. It uses installed Quickshell; the broker logs a launch error if Quickshell is missing. A tray-launched console is stopped with the broker. The broker and console need the same desktop user, `XDG_RUNTIME_DIR`, and session D-Bus environment. Closing the last permission supervisor pauses the broker and clears grants. Opening the console never resumes control or approves requests automatically.

### 4. Connect your MCP client

Add this to your client's MCP server configuration, replacing the command with the **absolute path to your executable**:

```json
{
  "mcpServers": {
    "computer-use": {
      "command": "/absolute/path/to/hyprland-computer-use",
      "args": ["mcp"]
    }
  }
}
```

Have the client start or reconnect this MCP server. It must run as the same desktop user with the broker's `XDG_RUNTIME_DIR`. Client-specific configuration file locations vary.

**`mcp` is only the protocol bridge. It does not start the broker or perform setup.** A missing `mcp.sock` usually means step 3 has not completed; see [startup troubleshooting](#startup-troubleshooting). Normally your MCP client runs this command—you do not need to run it in a third terminal yourself.

### 5. Approve the first task

Ask your agent to list the open windows, then request access to the one you want it to use. Window metadata is available without a grant; viewing pixels and sending input are not. Approve the request in the local console and choose its duration. If the console shows **PAUSED**, click **Resume** first.

Alternatively, once an MCP client is connected, click **Share** in the console (or run `hyprland-computer-use share`), then select a window to grant viewing and control for five minutes. **Escape cancels.** Use **Pause** or revoke the grant to stop access. Only share a terminal if you intend to give the agent shell authority.

### Stopping and starting again

```sh
hyprland-computer-use stop   # stop the broker and clear grants
hyprland-computer-use serve  # start it again
```

Ctrl+C also stops a manually launched broker/console. A setup-restarted broker runs as a regular background user process; its log is `$XDG_DATA_HOME/hyprland-computer-use/native/broker.log` (default `~/.local/share/hyprland-computer-use/native/broker.log`).

After replacing the executable or restarting/upgrading Hyprland, run `hyprland-computer-use setup`. It handles plugin replacement; you do not need to find or unload its path. If no broker was running, start `serve` afterward. No autostart service is installed.

For remote access, see [optional HTTP / built-in OAuth](#optional-http--built-in-oauth) and [AUTH.md](AUTH.md).

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

“Focus window A, then inject global input” has a race. The compositor guard instead validates a short-lived lease, live window object, root surface, visibility, and geometry revision **inside Hyprland's event loop**, then delivers input to that surface. It verifies the seat accepted the intended focus before sending events. Ordinary input now uses **experimental focus-preserving transactions**: complete keypresses, clicks, scrolls and bounded drag paths restore the previous seat focus before returning to the event loop. Pointer actions do not warp the desktop cursor. There is **no global-input fallback**.

The plugin has a trusted same-UID local control channel used by the broker; it does not authenticate one particular process. It checks expiry independently, rejects stale geometry/out-of-bounds pointer coordinates, and invalidates leases when the bound window/surface disappears. Synthetic presses are released inside each transaction, including exception cleanup; no held input spans broker requests. Its exact Hyprland build hash must match the headers used to build it.

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

### Focus-preserving input (experimental)

Typing and mouse actions borrow the target's **protocol focus**, not desktop activation, and restore the original focus in the same compositor callback. The active keyboard/keymap and modifier state are restored after keyboard input; mouse actions preserve the desktop cursor position and restore pointer focus/local position. This is not a second seat: applications still receive focus transitions and may react to them. The compositor's keyboard switch also broadcasts temporary keymap changes to clients before restoring the original map.

- An explicit `focus` action **still activates** the permitted window. Omit that action when you want background input. Other actions do not implicitly activate it.
- The human takes priority: held keys/buttons, seat grabs, input capture, active pointer constraints, input-method grabs, touch focus, or an existing drag-and-drop cause a clear refusal. Unsupported targets never fall back to stealing desktop focus or moving the cursor. A refusal can follow earlier completed actions or characters; re-observe the target before retrying, rather than blindly duplicating a partially typed string.
- A drag is a bounded 20-step burst with press and release in one callback. Omit `duration_ms` or use `0`; timed drags are rejected before any actions execute. Cross-window drag-and-drop, long holds, durable hover/tooltips, and applications that require foreground activation are not supported by this mode.
- The broker's own virtual US keyboard is identified by its Wayland client process, not by the active physical keyboard. Changing physical layouts is not necessary. Multiple seats and full IME/toolkit compatibility remain unsupported/unverified.
- This requires **guard protocol 2**. Run `hyprland-computer-use setup` to replace an old/faulted guard and restart an existing broker automatically. Grants are cleared. An old guard is rejected explicitly; a restoration fault disables further input until setup repairs it.

Native mock restoration tests and exact 0.56.2 header compilation are covered; real human/agent interleaving and application behavior still require the disposable-session checks in [TESTING.md](TESTING.md). Do not treat focus preservation as a new isolation or security guarantee.


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

### Optional Waybar example

The permission console and window picker use **Quickshell**. Use your existing desktop bar and StatusNotifierItem tray; Waybar is not required.

For a bare test session, `examples/waybar/` provides an optional bar with workspaces, a clock, a **Share window** button, and a tray.

```sh
sudo pacman -S --needed waybar ttf-jetbrains-mono-nerd
export PATH="$PWD/build:$PATH"
waybar -c "$PWD/examples/waybar/config.jsonc" -s "$PWD/examples/waybar/style.css"
```

Run it on the same session D-Bus as the broker. Leave `COMPUTER_USE_DEMO_TRAY` unset when using Waybar; the small built-in demo tray is not needed. No host autostart or shortcut is installed by `make`.

## Setup details

- `setup` is a local repair/update command. It builds first and verifies the header commit against the running Hyprland before stopping anything. It then inspects the exact loaded guard path, stops a recognized broker gracefully, replaces the guard, checks readiness, and restarts that broker using the updated executable. `setup --build-only` does no compositor/process management; the old `--load` option remains an alias for the default behavior.
- Broker discovery uses the MCP socket's kernel peer credentials, verifies the executable's Go module and `serve` arguments, and pins the process with a Linux pidfd. It preserves arguments, environment, and working directory, except obsolete keyboard-helper arguments and non-reusable inherited descriptors. A broker lifecycle lock prevents concurrent new brokers during replacement. No PID-file guessing or forced kill of the old broker is used.
- A small bundled, version-matched **temporary inspector plugin** reads the compositor's actual plugin metadata and is removed before the guard is replaced. This handles legacy guards too; Hyprland 0.56.2's normal plugin list does not report their paths. Other plugins are not unloaded. Ambiguous guards, cross-session sockets, service-managed brokers, and guards loaded from compositor configuration are refused rather than modified behind their manager's back.
- Build or version failures leave the running broker/guard alone. After a broker has been stopped, a replacement failure leaves control stopped and reports the error; setup never restores old grants or silently rolls back to an incompatible guard. Reconnect and approve permissions again after a successful restart.
- Native builds live under `$XDG_DATA_HOME/hyprland-computer-use/native/` (default `~/.local/share/hyprland-computer-use/native/`). Each setup uses a fresh private directory and publishes a `current` symlink only after success. Old builds are retained because a plugin may still be loaded from them. Stop the broker and unload the plugin before deleting old builds.
- `serve` creates and maintains the virtual keyboard/pointer directly in Go, with a self-contained US-ASCII keymap. It waits for compositor initialization before advertising readiness and stops if the device connection fails. There is **no `computer-use-keyboard` executable**, native keyboard build, or helper-path lookup. Remove the old `serve --keyboard` flag from existing commands.
- `hyprland-computer-use keyboard` runs the same device-lifecycle code standalone for diagnostics: it prints `ready` after initialization and stays connected until Ctrl+C. It accepts no input commands and is not an MCP tool. Do not run it alongside `serve`; normal operation needs only `serve`. The Go executable still builds with `CGO_ENABLED=0`.
- `console` extracts embedded QML into a private temporary runtime directory and removes it on normal exit. Quickshell remains an external runtime dependency; `grim` and `ffmpeg` remain external capture/recording dependencies.
- After a Hyprland upgrade, install matching development headers and rerun `setup`. Loading is session-local; repeat after a compositor restart. No autostart is installed.

This is a **single distributable**, not a statically self-contained desktop stack. A local compiler is needed because the compositor plugin is coupled to the exact Hyprland build. Setup has been build-tested against the lab's 0.56.2 headers; other releases may require source changes.

## Build from source on Arch

The compositor plugin is intentionally version-coupled. Build against the headers/libraries belonging to the running Hyprland package.

```sh
sudo pacman -S --needed go make gcc pkgconf hyprland nlohmann-json \
  wayland libxkbcommon libei quickshell grim ffmpeg
make
make test
```

Native binaries are built locally, not vendored. The Go executable embeds the project native sources, build recipes, console QML, and the small Wayland keyboard/pointer protocol XML files with their original licenses. Go dependencies are pinned by `go.mod` / `go.sum`. To build only the distributable executable, run `make build/hyprland-computer-use`.

## Alternative: manual source-checkout workflow

The [quick start](#quick-start-one-binary) is the recommended single-binary workflow. The commands here are for developers who built native artifacts with `make` and want to load them manually.

Run as your desktop user, with its `XDG_RUNTIME_DIR`, `WAYLAND_DISPLAY`, `HYPRLAND_INSTANCE_SIGNATURE`, and session D-Bus environment. **Do not run the broker as root.**

```sh
hyprctl plugin load "$PWD/build/guard.so"
./build/hyprland-computer-use serve
```

In another terminal in the same desktop session:

```sh
qs -p "$PWD/quickshell"
```

Configure your client as in [step 4](#4-connect-your-mcp-client), using the absolute path to `build/hyprland-computer-use`.

The bridge and broker must have access to the same private runtime socket. For a remote agent, forward stdio over SSH to an authorized desktop-user command; non-loopback HTTP requires OAuth and an HTTPS public origin; see [AUTH.md](AUTH.md).

### Startup troubleshooting

- **`mcp`: cannot connect to the broker / missing `mcp.sock`** — `mcp` is only a stdio bridge, not the server. Start `hyprland-computer-use serve` in another terminal first. If it exits, follow its startup error. A missing broker socket alone does **not** mean the plugin is missing. If the broker is already running, check the desktop user, `XDG_RUNTIME_DIR`, and any `mcp --socket` override.
- **`serve`: compositor guard check failed** — run `hyprland-computer-use setup` to build and repair the bundled plugin. Headers must match the running Hyprland build exactly, and setup must run as your desktop user in the intended session. For service/config-managed installations, follow setup's explicit refusal rather than launching a competing broker.
- **`serve`: keyboard/pointer initialization failed** — check `WAYLAND_DISPLAY` and `XDG_RUNTIME_DIR`, and run inside the intended Hyprland session. The compositor must expose `wl_seat`, `zwp_virtual_keyboard_manager_v1`, and `zwlr_virtual_pointer_manager_v1`. Multiple seats are explicitly unsupported. There is no external keyboard helper to install. For isolated diagnosis, stop the broker and run `hyprland-computer-use keyboard`.

Startup diagnostics go to stderr, leaving MCP stdout for protocol traffic. The bridge does not prompt, install dependencies, load compositor plugins, or start services automatically. Use the explicit local `setup` command to build its embedded native sources; the version-matched native build is still required.

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

Go checks run automatically on pushes and pull requests through [GitHub Actions](.github/workflows/go.yml). CI uses the Go version in `go.mod`, rejects unformatted code, runs vet, native transaction unit tests and Go race tests, and builds the static Go executable. Native plugin compilation and desktop-dependent checks are separate lab checks.

```sh
make fmt        # apply go fmt to project packages
make fmt-check  # fail if Go source formatting needs changes; writes nothing
make vet        # go vet ./...
make test       # formatting + vet + C++ transaction tests + Go race tests
```

- `make native-test`: standalone C++ tests of transaction cleanup/restoration using a fake compositor API; no Hyprland headers or desktop required.
- `make test`: formatting, vet, native transaction tests, and Go unit/race tests covering permission separation, scope, duration, revocation, UI loss, YOLO pause semantics, pointer bounds, keys, socket safety, and the built-in Wayland device lifecycle.
- `scripts/live-test.py`: **destructive, opt-in disposable-session tests**, exercising the actual MCP protocol, compositor guard, toplevel capture, pointer/key input, recording finalization, expiry and disconnect. Requires the broker, Quickshell, a Kitty window and a Pinta window. It uses a separate local UI connection to simulate the human. Set `COMPUTER_USE_DISPOSABLE=1` only in a disposable compositor.
- `scripts/mcp-probe.py`: a persistent real stdio MCP client used to make the demo. Its test-only command socket cannot grant approvals. `scripts/probe-call.py` sends it a tool call.

The demonstration uses the old global input harness **only to simulate the local human clicking approval controls**. Computer control is issued by the new MCP client. Test approvals are not represented as real user approvals on a live desktop.

## Current limitations

- **Native Wayland root toplevels only for input.** XWayland input and popup/subsurface routing are not implemented. Separate transient toplevels require their own grants.
- The built-in Go Wayland client supplies virtual keyboard and pointer seat capabilities; the guard delivers input directly. It selects the dedicated US keyboard and an available trusted local virtual pointer, never a physical pointer.
- Text is **US-layout ASCII**. Unsupported Unicode is rejected rather than silently mistyped. Physical-keyboard/layout arbitration needs more work before everyday desktop use.
- No clipboard, arbitrary file, shell, accessibility-tree, or privileged-operation tool.
- A controlled terminal or browser retains the application's existing powers. This is not application sandboxing or semantic authorization of purchases/deletions/sudo. There is no sudo broker.
- `launch_application` relies on the application's normal launch/sandbox configuration. Chromium inside restrictive containers may need setup outside this tool; the MCP API does not offer `--no-sandbox`.
- Window recording samples actual toplevel images at approximately 5 fps with wall-clock timestamps; no audio. Maximum two concurrent recordings and 32 per broker run, with a ten-minute per-recording limit. Odd dimensions are padded for H.264. Resizing stops the recording rather than producing corrupt output.
- No secure indicator resistant to a compromised compositor or malicious same-UID process. No full multi-monitor/mixed-DPI, session-lock, or hostile-client audit has been completed.

## License

[MIT](LICENSE), copyright Sam Saffron. Vendored protocol notices are retained in the XML; see [THIRD_PARTY.md](THIRD_PARTY.md).
