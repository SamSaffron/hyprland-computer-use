# Testing

Use **Go 1.26.6 or newer**. Install development dependencies from
[Contributing](../CONTRIBUTING.md); exact native targets and supported cases live
in the [compatibility table](COMPATIBILITY.md), not in dated run logs.

## Automated checks

```sh
make test                         # formatting, vet, native unit tests, Go race tests
go build ./...
make native-build-check           # exact installed Hyprland headers required
COMPUTER_USE_TEST_QML=1 COMPUTER_USE_TEST_XKB=1 COMPUTER_USE_TEST_DBUS=1 \
  dbus-run-session -- go test -race ./internal/app
```

The last command needs Quickshell, Qt Quick, XKB data/libxkbcommon, and DBus.
It uses offscreen QML and a private bus, never the desktop's session bus.
The required pinned Arch CI lane runs these integrations and the embedded
`setup --build-only` path. Release packaging depends on that lane. The rolling
Arch lane detects upcoming API breakage without declaring a new version supported.

Native unit tests cover transaction restoration, surface identity/routing, lease
policy helpers and sealed-keymap Wayland FD lifetime. Go tests cover broker
permissions, cleanup failure/retry reporting, transport/OAuth admission and setup.
These do not amount to full direct coverage of every callback in `guard.cpp`.

## Disposable compositor smoke

```sh
make setup-native build/hyprland-computer-use
dbus-run-session -- python3 scripts/headless-smoke.py
```

The harness creates a fresh runtime directory, removes inherited desktop socket
identifiers, and launches its own compositor. It drives `scripts/mcp-probe.py`
through request → simulated local approval on `ui.sock` → input → revoke → broker
refusal, then checks the old lease directly against the compositor. Its local
approval channel is a test human, **not evidence that same-UID agents cannot
self-approve**. It loads only the focus-borrowing guard in the disposable session;
it does not cover capture, independent-seat behavior or real human UI clicks.

**Current blocker:** stock Hyprland/Aquamarine may fail `CBackend::create()` with
a software-only headless backend. The smoke currently fails at this point on the
available 0.56.2 environment. CI runs it as an explicitly non-blocking experiment;
no successful end-to-end CI guarantee is claimed. A supported headless renderer
or an isolated GPU-backed runner is needed before making it required. Do not
silently patch compositor dependencies and describe the result as a stock pass.

For broader live checks, read `scripts/live-test.py` before enabling
`COMPUTER_USE_DISPOSABLE=1` in a disposable compositor. It needs Kitty and Pinta.
The fixed-layout sharing harness additionally needs reviewed coordinates and
human-input helpers; it is not a portable CI test. Never point these harnesses at
personal windows, credentials or unrelated work.

Record exact compositor commit, package versions, renderer/backend, target app,
commands and assertions for each new support claim. Keep dated run evidence
outside the user guides. Physical-monitor behavior, real lock-transition capture
confidentiality, universal toolkit multi-seat behavior and ARM64 desktops remain
unverified. A protocol delivery acknowledgement does not prove application output.
