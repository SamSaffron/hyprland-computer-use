# Setup and troubleshooting

[Quick start](../README.md) · [Setup](SETUP.md) · [Usage](USAGE.md) · [Tool reference](REFERENCE.md) · [Development](DEVELOPMENT.md)

## Install or update the executable

Once a release is published, install without Go or a source checkout:

```sh
curl -fsSL https://raw.githubusercontent.com/samsaffron/hyprland-computer-use/main/install.sh | sh
export PATH="$HOME/.local/bin:$PATH"
hyprland-computer-use version
```

To inspect the installer first, download it instead of piping it to `sh`. It needs `curl`, `tar`, and the usual Linux core utilities. It verifies the archive's SHA-256 against the release manifest, then atomically replaces only the executable. No desktop components or services are changed.

Optional installer arguments (the version below is an example, not a published-release claim):

```sh
sh install.sh --version v0.1.0 --install-dir "$HOME/.local/bin"
```

`COMPUTER_USE_INSTALL_DIR` also overrides the default destination. Rerun the installer to update, then run `hyprland-computer-use setup` inside the desktop session. Setup remains explicit because it replaces the compositor plugin and restarts an existing broker. For a pinned version, use `--version` again.

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

This builds, checks, and when necessary loads or updates the bundled plugin. **Setup is idempotent:** if the loaded guard and running broker already match the requested build, it leaves both running, preserves permissions, and reports `RESULT: EVERYTHING IS ALREADY OK`. If only the broker executable changed, setup restarts only the broker. If the active independent-seat guard differs, setup reports that a full Hyprland session restart is required before rerunning setup; a config reload is not enough. No package installation, autostart service, or compositor-config edit is performed. Use `setup --build-only` for an offline build.

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

For remote access, see [HTTP / OAuth guide](AUTH.md).

## Setup details

- `setup` is a local, idempotent check/repair/update command. It builds first and verifies the header commit against the running Hyprland before stopping anything. It inspects the exact loaded guard path and compares the active guard and broker executables with the requested artifacts. Already-current components remain running and permissions are preserved. A stale broker is restarted independently; a safely unloadable stale guard is replaced and checked before broker restart. A differing active independent-seat guard produces explicit full-session restart instructions instead of being hot-unloaded. `setup --build-only` does no compositor/process management; the old `--load` option remains an alias for the default behavior.
- Broker discovery uses the MCP socket's kernel peer credentials, verifies the executable's Go module and `serve` arguments, and pins the process with a Linux pidfd. It preserves arguments, environment, and working directory, except obsolete keyboard-helper arguments and non-reusable inherited descriptors. A broker lifecycle lock prevents concurrent new brokers during replacement. No PID-file guessing or forced kill of the old broker is used.
- A small bundled, version-matched **temporary inspector plugin** reads the compositor's actual plugin metadata and is removed before the guard is replaced. This handles legacy guards too; Hyprland 0.56.2's normal plugin list does not report their paths. Other plugins are not unloaded. Ambiguous guards, cross-session sockets, service-managed brokers, and guards loaded from compositor configuration are refused rather than modified behind their manager's back.
- Build or version failures leave the running broker/guard alone. After a broker has been stopped, a replacement failure leaves control stopped and reports the error; setup never restores old grants or silently rolls back to an incompatible guard. Reconnect and approve permissions again after a successful restart.
- Native builds live under `$XDG_DATA_HOME/hyprland-computer-use/native/` (default `~/.local/share/hyprland-computer-use/native/`). Each setup uses a fresh private directory and publishes a `current` symlink only after success. Old builds are retained because a plugin may still be loaded from them. Stop the broker and unload the plugin before deleting old builds.
- `serve` creates and maintains the virtual keyboard/pointer directly in Go, with a self-contained US shortcut keymap. Unicode text maps are generated inside the guard and sent only to the target client, never installed on this virtual device; see [text input](TEXT_INPUT.md). It waits for compositor initialization before advertising readiness and stops if the device connection fails. There is **no `computer-use-keyboard` executable**, native keyboard build, or helper-path lookup. Remove the old `serve --keyboard` flag from existing commands.
- `hyprland-computer-use keyboard` runs the same device-lifecycle code standalone for diagnostics: it prints `ready` after initialization and stays connected until Ctrl+C. It accepts no input commands and is not an MCP tool. Do not run it alongside `serve`; normal operation needs only `serve`. The Go executable still builds with `CGO_ENABLED=0`.
- `console` extracts embedded QML into a private temporary runtime directory and removes it on normal exit. Quickshell remains an external runtime dependency; `grim` and `ffmpeg` remain external capture/recording dependencies.
- After a Hyprland upgrade, install matching development headers and rerun `setup`. Loading is session-local; repeat after a compositor restart. No autostart is installed.

This is a **single distributable**, not a statically self-contained desktop stack. A local compiler is needed because the compositor plugin is coupled to the exact Hyprland build. Setup has been build-tested against the lab's 0.56.2 headers; other releases may require source changes.

## Startup troubleshooting

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
