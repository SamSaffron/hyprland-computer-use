# Hyprland Computer Use

Let an MCP agent view and control **windows you approve**, with a local Quickshell permission console and tray icon. Access starts denied; you choose the windows and duration, and can pause or revoke it.

**Experimental.** Tested against Hyprland **0.56.2**; the plugin requires headers matching your exact running build. Use disposable windows first. Input supports native Wayland toplevels, with target-scoped Unicode/bulk text implemented but not yet live-validated; not XWayland. Focus-preserving input and automatic live plugin replacement still need live testing. **Sharing a terminal gives shell authority—this is not a sandbox.**

## Quick start

### 1. Get the executable and dependencies

On Arch, install the runtime and plugin-build dependencies:

```sh
sudo pacman -S --needed curl make gcc pkgconf hyprland nlohmann-json \
  wayland libxkbcommon libei quickshell grim ffmpeg
```

Install a release binary—no Go compiler needed:

```sh
curl -fsSL https://raw.githubusercontent.com/samsaffron/hyprland-computer-use/main/install.sh | sh
export PATH="$HOME/.local/bin:$PATH"
```

**Release publishing is newly configured, not yet verified.** Until a release is available, install Go and build from this checkout:

```sh
make build/hyprland-computer-use
export PATH="$PWD/build:$PATH"
```

Only the **executable** needs to be copied to another desktop; native sources and the UI are bundled. The target still needs the system dependencies above.

### 2. Start it inside your Hyprland session

Run as your normal desktop user, **not root**:

```sh
hyprland-computer-use setup
hyprland-computer-use serve
```

Leave `serve` running. **Click its tray icon** to open the permission console. No tray? Run `hyprland-computer-use console` in another terminal.

### 3. Connect your MCP client

Add this to your client's MCP configuration, using the **absolute path** to your executable:

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

The client must run as the same desktop user with the same `XDG_RUNTIME_DIR`. The `mcp` command is a bridge; it does not start the broker.

### 4. Approve a window

Ask the agent to list windows and request access. Approve in the local console, or click **Share** to pick a window yourself. If the console says **PAUSED**, click **Resume**. Use **Pause** or revoke the grant to stop access; closing the last console also pauses and clears grants.

## Update or stop

- **Update:** rerun the installer (or replace a source-built executable), then run `hyprland-computer-use setup`. It replaces the guard and restarts an existing broker. Skip `serve` if setup reports a restart. Grants reset; reconnect your MCP client.
- **Stop:** run `hyprland-computer-use stop`, or press Ctrl+C in the broker terminal.
- **Offline build:** `hyprland-computer-use setup --build-only` does not touch the compositor or broker.

No autostart service or compositor-config edit is installed. Service/config-managed installations require their own update procedure.

## Go deeper

| Guide | Contents |
|---|---|
| [Setup and troubleshooting](docs/SETUP.md) | Detailed installation, startup errors, updates, storage |
| [Permissions and sharing](docs/USAGE.md) | Grants, YOLO, window picker, shortcuts, Waybar |
| [Architecture and tool reference](docs/REFERENCE.md) | MCP tools, input examples, focus behavior, limitations |
| [Remote access / OAuth](docs/AUTH.md) | HTTP, TLS, authentication and connection approval |
| [Development](docs/DEVELOPMENT.md) | Source layout, builds, tests, manual plugin loading |
| [Releasing](docs/RELEASING.md) | GoReleaser archives, checksums, publishing workflow |
| [Unicode and bulk text](docs/TEXT_INPUT.md) | Text contract, upgrade requirements and two-window manual test |
| [Testing status](docs/TESTING.md) | Exact lab versions, verified and unverified behavior |
| [Security](SECURITY.md) | Trust boundaries and guarantees |

[MIT license](LICENSE) · [Third-party notices](THIRD_PARTY.md)
