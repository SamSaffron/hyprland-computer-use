# Contributing

Thank you for helping improve Hyprland Computer Use. Changes should preserve the project's default-deny security model and describe claims in terms of evidence actually collected.

## Development setup

The Go executable requires Go **1.26.6 or newer**. On Arch Linux, install the full development and test dependencies:

```sh
sudo pacman -S --needed go make gcc pkgconf hyprland nlohmann-json \
  wayland libxkbcommon libei
```

Hyprland plugins use private compositor APIs. Native builds therefore require headers from the exact Hyprland build being targeted. Arch Linux is currently the only tested distribution; ports and dependency instructions for other distributions are welcome when accompanied by reproducible test results.

Run the standard checks from the repository root:

```sh
make test                              # formatting, vet, native unit tests, Go race tests
make build/hyprland-computer-use      # distributable Go executable
make native-build-check               # both guard variants, helpers, and native tests
```

`make native-build-check` needs Hyprland, libei, Wayland, and xkbcommon development metadata. CI runs it against the repository's pinned Hyprland 0.56.2 Arch environment. See [Development](docs/DEVELOPMENT.md) for source layout and targeted commands.

## Security invariants

Before changing a transport, permission, capture, or input path, read [SECURITY.md](SECURITY.md). In particular:

- MCP starts denied. Granting, revoking, pausing, and selecting YOLO remain local-UI decisions.
- MCP and local UI transports remain separate; do not expose arbitrary shell, path, clipboard, approval, or policy-changing MCP tools.
- Window authority is instance-bound. Titles, application names, and PIDs do not establish authority.
- Window capture uses actual foreign-toplevel capture, never a desktop crop presented as window isolation.
- Window-scoped input requires the compositor guard and never falls back to global injection.
- Same-UID processes and compositor plugins are trusted. A controlled terminal transitively grants shell authority; this is not an OS sandbox.

Tests and documentation must distinguish automated coverage, live observations, unsupported behavior, and unverified behavior. Do not broaden security or compatibility claims solely because code compiles.

## Disposable live tests

The scripts under `scripts/` are **lab harnesses**, not normal user tools:

- `live-test.py` performs destructive end-to-end checks using a separate local UI connection as the simulated human.
- `live-sharing-test.py` depends on a fixed disposable desktop layout and external human-input helpers.
- `mcp-probe.py` exposes a test-only same-user command socket for driving an MCP demo client; it cannot grant approval, but must not be run on a shared or untrusted desktop.

Run live harnesses only in a disposable compositor after reading [Testing status](docs/TESTING.md). Never use a desktop containing personal work, credentials, or unrelated windows. Record the exact Hyprland version and commit, package versions, output layout, tested applications, and observed results.

## Pull requests

A pull request should:

1. Explain the user-visible behavior and security-boundary impact.
2. Include the tests run and their results.
3. Identify anything that was not live-validated.
4. Update user and security documentation when guarantees or limitations change.
5. Avoid generated build output, captures, recordings, credentials, and lab evidence containing private data.

Report suspected vulnerabilities privately using the instructions in [SECURITY.md](SECURITY.md), not through a public issue.
