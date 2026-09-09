# Development

[Quick start](../README.md) · [Setup](SETUP.md) · [Usage](USAGE.md) · [Tool reference](REFERENCE.md) · [Development](DEVELOPMENT.md)

## Repository layout

```text
cmd/hyprland-computer-use/  executable entry point
internal/app/              broker, CLI, transports, permissions, and Go tests
assets.go                  embeds the canonical resources below
native/                    compositor plugins, C++ tests, and protocol XML
quickshell/                permission console and window picker
examples/                  optional desktop integrations
scripts/                   opt-in live tests and demo helpers
docs/                      setup, usage, reference, auth, and testing guides
install.sh                 checksum-verified per-user release installer
.goreleaser.yml            Linux release binaries and archives
```

Application code stays in one internal package for now; this is a layout change,
not a rewrite of the broker's boundaries. The small root `assets` package lets
`go:embed` include native/QML sources and license files without duplicated copies
or generated assets. No application logic lives at the repository root.

Commands below run from the repository root. Go-only equivalents:

```sh
go build ./...                 # compile all packages
go test -race ./...             # test all packages
go run ./cmd/hyprland-computer-use setup --help
CGO_ENABLED=0 go build -trimpath -o build/hyprland-computer-use ./cmd/hyprland-computer-use
```

Use `./internal/app` for targeted application tests, not `.`. Installer and release-helper tests live in `scripts/`. For example:

```sh
go test -race -run TestSetup ./internal/app
go test -race ./scripts
```

`make build/hyprland-computer-use` also embeds the Git version, commit, and build date; `hyprland-computer-use version` works without a desktop. Plain Go builds report `dev`. For archives and publishing, see [Releasing](RELEASING.md).

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

The [quick start](../README.md#quick-start) is the recommended single-binary workflow. The commands here are for developers who built native artifacts with `make` and want to load them manually.

Run as your desktop user, with its `XDG_RUNTIME_DIR`, `WAYLAND_DISPLAY`, `HYPRLAND_INSTANCE_SIGNATURE`, and session D-Bus environment. **Do not run the broker as root.**

```sh
hyprctl plugin load "$PWD/build/guard.so"
./build/hyprland-computer-use serve
```

In another terminal in the same desktop session:

```sh
qs -p "$PWD/quickshell"
```

Configure your client as in the [MCP connection step](../README.md#3-connect-your-mcp-client), using the absolute path to `build/hyprland-computer-use`.

The bridge and broker must have access to the same private runtime socket. For a remote agent, forward stdio over SSH to an authorized desktop-user command; non-loopback HTTP requires OAuth and an HTTPS public origin; see [AUTH.md](AUTH.md).

Stop the broker/Quickshell, then unload the plugin when finished:

```sh
hyprctl plugin unload "$PWD/build/guard.so"
```

## Tests and demo

Go and native checks run automatically on pushes and pull requests through [GitHub Actions](../.github/workflows/go.yml). CI uses the Go version in `go.mod`, rejects unformatted code, runs vet, native transaction unit tests and Go race tests, and builds the static Go executable. A separate Arch job pins a complete package snapshot and compiles both guard variants, setup helpers, and native tests against the [pinned native target](COMPATIBILITY.md); it also runs the real embedded `setup --build-only` path. Required offscreen QML/XKB/private-DBus checks run there too. The headless compositor smoke is currently non-blocking; see [Testing](TESTING.md).

```sh
make fmt        # apply go fmt to project packages
make fmt-check  # fail if Go source formatting needs changes; writes nothing
make vet        # go vet ./...
make test       # formatting + vet + C++ transaction tests + Go race tests
```

- `make native-test`: standalone C++ tests of transaction cleanup/restoration using a fake compositor API; no Hyprland headers or desktop required.
- `make test`: formatting, vet, native transaction tests, and Go unit/race tests covering permission separation, scope, duration, revocation, UI loss, YOLO pause semantics, pointer bounds, keys, socket safety, and the built-in Wayland device lifecycle.
- `scripts/live-test.py`: **destructive, opt-in disposable-session tests**, exercising the actual MCP protocol, compositor guard, toplevel capture, pointer/key input, recording finalization, expiry and disconnect. Requires the broker, Quickshell, a Kitty window and a Pinta window. It uses a separate local UI connection to simulate the human. Set `COMPUTER_USE_DISPOSABLE=1` only in a disposable compositor. Artifacts default to ignored `evidence/`; set `COMPUTER_USE_ARTIFACT_DIR` to use another directory.
- `scripts/live-sharing-test.py`: a fixed-layout disposable-lab harness for proactive sharing, recipient selection, the picker, shortcut, Waybar button, and tray. Its coordinates assume the documented 1400 × 1200 lab layout and must be reviewed before each run. It uses the same artifact-directory setting as `live-test.py`.
- `scripts/mcp-probe.py`: a persistent real stdio MCP client used to make the demo. Its test-only same-user command socket cannot grant approvals, but still exposes MCP calls to any process that can reach it; never run it on a shared or untrusted desktop. `scripts/probe-call.py` sends it a tool call.

The demonstration uses the old global input harness **only to simulate the local human clicking approval controls**. Computer control is issued by the new MCP client. Test approvals are not represented as real user approvals on a live desktop.

See [Testing](TESTING.md) for required QML/XKB/DBus checks, the experimental headless smoke and unverified cases; exact targets live in [Compatibility](COMPATIBILITY.md).
