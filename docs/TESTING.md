# Testing

Pointer/capture extensions have a [dated real term-llm red/green comparison and
Seat/Fallback regression report](POINTER_CAPTURE_TESTING.md). Its GTK raw-motion
fixture requirement is explicit; it does not expand arbitrary-app drag guarantees.

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


## Post-action observation delay — September 11, 2026

A real **Mousepad 0.7.0 / GTK 3.24.52** editor on the existing pinned
Hyprland 0.56.2 GPU lab reproduced stale post-input images. The old broker
returned the previous note in **3/3** scored reproductions, while later
captures showed the replacement text. The application had no injected sleeps,
mock UI or modified rendering behavior.

The delay candidate was then tested with **10 paired trials**, alternating
0ms/200ms order, after two warmup pairs. Both arms ran on the same broker and
compositor, with identical Ctrl+A + text actions, note content, geometry and
capture settings. Only `observation.delay_ms` varied. OCR of the actual returned
PNG and a later MCP-captured reference gave:

| Observation | Returned screenshot contains complete new note | Later reference correct | Median input-and-capture time |
|---|---|---|---|
| Default 0ms | 2/10 | 10/10 | 98ms |
| Explicit 200ms | 10/10 | 10/10 | 299ms |

This demonstrates improved returned-frame freshness for this workflow, **not**
a general 200ms rendering guarantee or a model task-success benchmark. Fresh
screenshots cost approximately the requested 200ms here. Earlier synthetic
native-versus-MCP pilot results are not included.

Reproduce with `scripts/live-observation-delay-test.py` in an explicitly
disposable session; [the recipe](OBSERVATION_DELAY.md#real-application-test)
includes separate OCR scoring. Raw screenshots, MCP traces and build provenance
are retained as private evaluation artifacts, not checked into the repository.
Unit/race coverage includes delay bounds and prevalidation, cancellation and
deadlines, input-lock release, pause/revocation/window-close/lock refusal,
fresh post-wait geometry, no wait on failed input, unchanged standalone capture,
and MCP result serialization.
