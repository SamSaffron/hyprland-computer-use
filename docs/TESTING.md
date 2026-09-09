# Verification — 7–8 September 2026

[Quick start](../README.md) · [Setup](SETUP.md) · [Usage](USAGE.md) · [Tool reference](REFERENCE.md) · [Development](DEVELOPMENT.md)

This is evidence from one disposable configuration, not a general security audit.

## Configuration

- Arch Linux; Hyprland **0.56.2**, build `efb50993780079460b0cbed1363e2166a2de1d9f`.
- Nested GPU-backed compositor with a **1400 × 1200** headless output; NVIDIA RTX 4090, driver **610.57.04**.
- Quickshell **0.3.1**, native Wayland Kitty and Pinta.
- The lab's Aquamarine **0.15.0** build has local nested-backend compatibility patches (xdg_wm_base version clamp and backend-supported buffer modifiers). This was not a stock physical-monitor deployment.
- Persistent helper supplies a US virtual keyboard and virtual pointer. Guard built against the exact running compositor's headers.

## Surface routing and dialog transitions — 9 September 2026

Implemented `window_state`, root-scoped live surface selectors, mapped subsurface stacking/input-region routing, selected-popup coordinates, and explicit direct XDG transient parent/child metadata. `then: state` preserves completed-input status if post-discovery fails. No PID/title authority inheritance, desktop-crop capture or global input fallback was added. **Active seat grabs remain refused**; this does not establish support for grabbed toolkit menus.

Native tests exercise the actual handle registry and hit/path-planning code: root binding, plugin-epoch separation, destroyed/foreign selectors, nested and negative offsets, stacking/input-region holes, popup-tree separation, bounds, and cross-surface drag refusal before delivery. Go race/SDK tests cover free metadata without authority, surface-local request propagation including Unicode, paired selectors, stale/closed selectors, full prevalidation, separate dialog approval, old-guard refusal, and post-state success/failure wire contracts. The real guard compiles against Hyprland 0.56.2. The compositor tree adapter and application effects still require live verification; no new plugin was loaded or desktop operated. Use [SURFACES.md](SURFACES.md#manual-acceptance-test).

**User-reported Unicode result:** the user reported that all steps of the preceding Unicode/bulk manual test passed. Editor names/versions and bulk timings were not supplied, and this was not independently observed by the test harness. It is not evidence for the newer surface-routing path.

## Unicode/bulk text — 9 September 2026

Target-scoped Unicode text now uses 48-scalar compositor transactions, without per-character sleeps/round trips or clipboard access. The broker prevalidates all text and enforces a 256 KiB aggregate UTF-8 budget plus a 60-second batch execution budget. Native code validates scalars again and sends a self-contained map only to the target's keyboard resources; the text-map carrier is never registered or set as the global keyboard.

Automated checks cover real-XKB compilation/Unicode decoding of the actual native-generated map; native paired events, target-only map dispatch, normal/same-device restoration, map setup/restoration faults, invalid scalars/chunk bounds, and partial chunk progress; Go batching beyond the old 4,096-byte cap, aggregate budget rejection before effects, old-guard feature refusal, and revocation between chunks. Existing native and Go input/capture/permission regressions still apply. Run:

```sh
make test
COMPUTER_USE_TEST_XKB=1 go test -race -run 'Test(KeyboardKeymapXKB|TextKeymapXKB)' -v ./internal/app
make native
make native-text-wire-test  # installed libwayland-server, private socketpair only
```

The optional XKB test compiles `native/input_transaction_test.cpp` in temporary storage and asks its real keymap generator for a map, then checks it using Python ctypes/libxkbcommon. It does not operate a desktop. Linux carrier tests verify sealed/NUL-terminated map contents, zero file offset and descriptor cleanup. A private-socketpair test against the installed libwayland-server verifies that a queued keymap FD survives destruction of its original carrier before flush; no compositor/session is involved. Native tests also cover multiple target resources, missing-keyboard refusal and prohibition of even transient global text-map installation. Go tests reject malformed progress and preserve prior chunk counts on cancellation. The guard compiles against lab **Hyprland 0.56.2** headers. **No new live plugin was loaded and Unicode/bulk delivery is not yet live-validated.** Use the [two-editor manual acceptance test](TEXT_INPUT.md#manual-test-two-disposable-editor-windows), including actual application content, the next physical keystroke, same-app windows, refusal and revocation. These results are not universal toolkit, IME, hidden-window or literal paste guarantees.

## Source layout and release packaging — 8 September

Application code/tests now live in `internal/app`, with the executable under `cmd/hyprland-computer-use`. The root asset package embeds the original native/QML sources and license notices. `make test`, Go vet/builds, recursive formatting rejection/acceptance checks, and optional XKB, offscreen QML, and private-bus tray tests passed after the move. Local Markdown links and fragments were checked.

Linux amd64 and arm64 `CGO_ENABLED=0` builds passed; ARM64 was cross-compiled only. A copied amd64 executable reported its embedded version and completed `setup --build-only` from a scratch directory with desktop environment variables unset. It built the bundled native components against the lab's 0.56.2 headers without loading them.

Installer tests use fixture archives and mocked curl/uname to cover latest/pinned versions, amd64/arm64 selection, checksum mismatch/missing/duplicate checksums, failed downloads, unsupported platforms, missing archive binaries, path handling, and atomic symlink replacement. The release-helper test mocks git/gh, including tag creation/push and workflow waiting; no real repository mutation or publication occurs. Shell checks and YAML parsing passed.

The initial public `v0.0.1` release completed the hosted test and release workflows. GoReleaser published amd64/arm64 archives plus `checksums.txt`; both downloaded archives matched the manifest and contained the executable, documentation, notices, and protocol XML. A clean Go 1.25 container successfully ran `go install github.com/samsaffron/hyprland-computer-use/cmd/hyprland-computer-use@latest`, resolving the canonical module as `v0.0.1`. Public installer validation downloaded the latest release and reported the expected embedded version. Live plugin replacement and `serve` startup remain part of the disposable-session gate, not container evidence.

## Automatic local setup repair — 8 September

**Current usage:** `hyprland-computer-use setup` idempotently checks and repairs the loaded guard and broker. Matching components remain running without clearing grants; a stale broker can restart independently, and a differing active independent-seat guard requires a full Hyprland session restart. Use `setup --build-only` for offline compilation. Earlier entries below used the former meaning of plain `setup` (build-only).

Repair tests use mocked compositor operations and cover exact-path discovery, temporary-inspector cleanup, build-before-disruption, byte-equivalent guard no-op behavior (including the active independent-seat variant), broker-only updates, stop/unload/load/readiness/publication/restart ordering, failed unloads (including hyprctl errors with exit status 0), failed loads/readiness/publication, wrong-session sockets, and config-managed guards. Process tests use only test-owned children to check pidfd-based graceful stopping, restart readiness, preserved working directory/environment, private logs, and refusal to stop an unrelated socket owner. Lifecycle locks are checked for exclusion.

The temporary read-only inspector compiles against the exact **Hyprland 0.56.2** headers. No live plugin hot-swap or real desktop broker restart was performed in this session. Real repair still needs a disposable-session check, particularly compositor permissions/config reload behavior, legacy broker detection, HTTP/OAuth option preservation, and MCP/UI reconnection. Setup does not install or manage host services and does not preserve grants.

## Single-distributable setup — 8 September

The Go binary now embeds native build inputs and the Quickshell console. Automated tests cover byte-for-byte extraction, helper lookup, failed-build rollback, retained old plugin builds, commit-mismatch refusal before plugin loading, refusal to replace a listening guard, and console launch/temporary-file cleanup with a mocked Quickshell executable.

`go test -race ./...`, `go vet ./...`, and the `CGO_ENABLED=0` executable build passed. A copied executable was run from a separate scratch directory with the desktop environment variables unset and a scratch `XDG_DATA_HOME`. Plain `setup` successfully compiled the embedded plugin, keyboard/pointer helper, and version probe without reading a source checkout. The probe reported the lab header commit `efb50993780079460b0cbed1363e2166a2de1d9f` (Hyprland 0.56.2).

This setup check did **not** load a plugin, launch real Quickshell, install system packages, or start host services. The new `setup --load` orchestration and bundled-console launcher have automated coverage, not a new live-desktop verification. The earlier live checks below concern the existing native code and console behavior.

## Tray icon and activation — 8 September

The tray now uses transparent, antialiased monitor-and-pointer pixmaps at 16, 22, 32, 48 and 64 pixels. Left-click opens the local console, launching bundled QML through Quickshell if no supervisor is connected. A DBusMenu exposes **Open permission console** for hosts that use a menu rather than calling `ContextMenu`.

Unit/race checks cover coalescing repeated clicks, reusing an existing supervisor, console cancellation, menu layout/events, ARGB pixmap shape, and unchanged pause/approval authority. An isolated `dbus-run-session -- env COMPUTER_USE_TEST_DBUS=1 go test -race -run TestTrayPrivateBus -v ./internal/app` passed real D-Bus layout decoding, menu click dispatch, and SNI activation without touching the desktop bus. The icon preview was visually inspected. This is not a new live Waybar/Quickshell click test; host-specific popup positioning and real console startup from the tray remain to be checked in the disposable lab.

## Built-in keyboard and stable outlines — 8 September

The separate C `computer-use-keyboard` helper has been removed. `serve` now owns a Go Wayland client, and the optional `keyboard` diagnostic subcommand runs that same implementation. It creates the virtual devices and sends a self-contained, sealed US-ASCII keymap; it has no key, motion, or button injection path. Readiness requires a compositor roundtrip after keymap delivery; required-global removal or connection failure stops the broker. Multiple seats are explicitly unsupported. The earlier live checks below used the former C helper and do **not** constitute live validation of this replacement.

The Go race tests exercise a fake compositor over real Unix stream sockets, including exact device-creation messages, `SCM_RIGHTS` transfer and sealed keymap contents, no requests after readiness, missing-interface rejection, malformed framing, protocol errors, seat removal, and cancellation. A copied standalone binary also completed `setup` in scratch storage against the lab's Hyprland 0.56.2 headers, producing the plugin and version probe with **no keyboard executable**. `file` confirmed that the Go binary is statically linked. No plugin was loaded for this check. `COMPUTER_USE_TEST_XKB=1 go test -run TestKeyboardKeymapXKB -v ./internal/app` additionally passed against the installed libxkbcommon parser: all printable US-ASCII characters, sampled special keys, and the guard's modifier indices were checked. This parser is a test dependency only; the Go binary remains `CGO_ENABLED=0`.

Target overlays are keyed by stable window IDs rather than marker objects containing changing countdowns and are gated by Quickshell's live active-workspace state. Inactive and unknown workspaces hide the output-level overlay; pinned windows remain eligible. `COMPUTER_USE_TEST_QML=1 go test -run 'Test(WorkspaceVisibility|OutlineDelegateStability)' -v ./internal/app` passed with real Quickshell on offscreen Qt and a private D-Bus/runtime: active/inactive/unknown/pinned workspace cases passed, countdown and geometry updates kept the same delegate, and revocation destroyed it. The lifecycle test uses the production model/binding snippets with QtObject delegates, not live compositor surfaces. Live visual confirmation across workspaces remains to be checked.

## Focus-preserving input prototype — 8 September

Guard protocol 2 replaces split key/button operations with complete compositor-thread transactions. Ordinary input no longer calls `fullWindowFocus` or warps the cursor. The guard borrows protocol focus, releases synthetic keys/buttons, restores the previous keyboard/pointer device, focus and keyboard modifiers, then returns. Keyboard switching temporarily broadcasts keymap changes, and applications still observe focus leave/enter events. Explicit `focus` retains its intentional activation semantics. These are experimental restoration guarantees, not independent seats or invisible input.

The native guard compiled against the exact lab **Hyprland 0.56.2** headers. `make native-test` compiles the actual transaction implementation against a fake compositor API and checks successful keyboard/pointer restoration, modifier preservation, exception cleanup of presses, busy-state refusals, broker-process keyboard selection, missing restoration geometry, and restoration-fault lockout. It does not validate the compositor's or toolkit's side effects. Go tests check complete single-request input transactions, lease/revision propagation, refusal without fallback, full prevalidation of unsupported timed drags, and rejection of old guard protocols. `make test` now requires the native unit tests as well as formatting, vet and Go race checks; it needs a C++23 compiler but not Hyprland headers. These tests also passed with AddressSanitizer/UndefinedBehaviorSanitizer. The bundled-source `setup` build passed in scratch storage without loading a plugin. Hyprland's input-capture header requires the `libeis-1.0` include flags (Arch package `libei`), which setup now checks explicitly.

**Not live-tested:** no new plugin was loaded into the desktop. Earlier live-test results below predate this change. A drag is now a bounded 20-step burst with no compositor sleeps; timed drags are rejected rather than silently approximated. The opt-in `scripts/live-test.py` has been updated to use that mode but has not been rerun.

Before claiming everyday human/agent coexistence, run this acceptance checklist in the disposable compositor (not a personal desktop):

1. Load the rebuilt guard and broker. Put two separate native Wayland applications side by side; keep human keyboard/pointer focus on A and grant the agent only B.
2. Through real MCP, type a distinctive string and shortcut into B **without a focus action**. Verify actual B content, unchanged A content, unchanged `hyprctl -j activewindow`, and that the next human keystroke still reaches A.
3. Click, scroll and perform an atomic in-window drag in B. Verify actual B changes, unchanged `hyprctl -j cursorpos` and desktop activation, and that human pointer input still reaches A afterward.
4. Repeat with held physical modifiers/keys/buttons and active grabs/constraints: input must fail without changing focus or sending partial actions. Repeat with human typing between transactions, Caps Lock and a non-US physical layout; check no lost keys or modifier leakage.
5. Test same-application windows separately, nullable pointer focus, geometry changes, focus-dependent applications, revocation, target closure and disconnect. Recheck local Pause and stale/window-scope denial. Do not infer correctness for popups, IMEs, cross-window drag-and-drop, cursor-shape requests or mixed-DPI setups from basic success.

## Observation and recovery hardening — 9 September

Automated regression coverage now checks screenshot metadata against decoded PNG dimensions, deterministic content IDs, true `grim -T` invocation, rejection of negative/oversized width options before input, lock refusal before and after capture, missing lock status, workspace transitions, post-capture pause, partial text acknowledgements, no later actions after failure, and completed-input preservation when post-action observation fails. These use fake `hyprctl`/`grim` and a private mock guard socket: **not live desktop evidence**.

Current-version release gate (run only with explicit authorization in a disposable compositor):

| Mode or scenario | Current claim / acceptance evidence required |
|---|---|
| Visible, unfocused native window | Experimental restoration; rerun the full keyboard/pointer checklist below with actual app changes and next physical input |
| Fully occluded window | Unverified; confirm isolated capture and delivery without exposing another window or changing activation |
| Inactive workspace | Not a supported guarantee; current visibility/input checks may refuse. No workspace switching fallback |
| Hidden/minimized window | Capture rejects hidden/nonvisible targets; no unhide fallback |
| Same-application windows | Unverified current-version toolkit behavior; verify both windows' content and next human input |
| Held modifiers, keys/buttons, grabs | Mock refusal tested; physical arbitration remains a live gate |
| Popup/subsurface, new dialogs | Surface routing implemented, live validation pending; active grabs refused and no transient-toplevel grant inheritance. See SURFACES.md |
| Mixed-DPI / resized target | Validate decoded image-to-logical transforms against real pixels and reject stale geometry |
| Lock/session deactivation | Begin screenshot and recording, lock before/during capture, then unlock; no new locked frames may be returned/written. Check errors and recording termination. Repeat guard loss and rapid lock/unlock; polling is not an atomic fence |
| Revoke/pause/supervisor loss/disconnect | Interrupt long text and capture through real MCP; verify no subsequent transactions/frames, inspect reported acknowledgements, and verify next physical input |
| App render latency | `then=screenshot` is immediate, not settled; verify clients recover by observing again rather than replaying input |

Record the exact Hyprland build hash, guard/broker revision, toolkit/app version, scale/output layout and per-case results. Passing RPCs alone is insufficient; inspect app content and the next physical input. Older demos below do not satisfy this gate. No host plugin was loaded or desktop operated for this hardening work.

## Go quality checks

`make fmt` applies `go fmt ./...`; `make fmt-check` rejects unformatted Go files without rewriting them; `make vet` runs `go vet ./...`. `make test` requires both Go checks and the standalone native transaction unit tests before running `go test -race ./...`. The GitHub Actions Go workflow runs these checks and builds the standalone executable on pushes and pull requests. Its pinned Arch job also compiles both real guard variants, setup helpers, and native tests against the documented Hyprland 0.56.2 headers, then exercises embedded `setup --build-only`. XKB, offscreen Quickshell, private-bus tray tests, and live desktop tests remain separately invoked checks. The hosted Go and native jobs passed for the initial public release.

## Automated and live checks

`go test -race ./...`: **35 top-level tests passed** (including proactive-sharing subcases). `go vet ./...`: passed. Native plugin/helper compilation: passed.

The opt-in `scripts/live-test.py` passed **15 checks** against the actual MCP transport and compositor:

1. Window metadata is free; pixel observation still requires a separate grant.
2. MCP cannot directly invoke approval/mode-changing tools.
3. Native toplevel capture matches the target dimensions.
4. Approved keyboard actions reach Kitty.
5. One window's control grant does not authorize another window.
6. Stale geometry rejected.
7. Negative/out-of-window coordinates rejected by the broker.
8. Compositor independently rejects out-of-window coordinates.
9. Expiry enforced by broker and compositor.
10. YOLO auto-allows exposed operations.
11. Pause overrides YOLO.
12. Pointer click/drag/scroll complete and change Pinta pixels.
13. Window recording finalizes on mode revocation; ffprobe accepts the MP4.
14. Client disconnect revokes its grants.
15. Destroying a window invalidates its compositor lease.

Pixel evidence was also visually inspected: a real brush stroke appeared in Pinta's canvas/history, and Kitty executed the test commands. Protocol success alone was not treated as proof of pointer delivery. The stricter seat-focus check caught a missing-pointer-capability bug, fixed by the persistent helper and explicit virtual-device selection.

## Demonstration

A **56-second desktop recording** exercises workspace observation approval, a separate five-minute Kitty control grant, countdown/outline, real MCP keyboard input, revocation and a blocked retry, two-click local YOLO selection, drawing a red heart onto an existing image in Pinta, window recording, real tray activation, and Pause overriding YOLO. The lab is left in Approve mode, paused.

The old global pointer harness is used **only to simulate the human's permission-console/tray clicks**. Agent operations use a persistent real stdio MCP client. Calls are scripted, not represented as autonomous model reasoning. The existing cat artwork was not created by this MCP demonstration. The video labels these boundaries explicitly.

## Not verified

Physical-desktop device arbitration, mixed DPI/multiple monitors/seats, all lock-screen transitions, popup/subsurface input, XWayland input, transparent/protected capture, and Chromium launch inside this container are not covered by these results. Same-UID processes and controlled terminals retain their local authority. See [SECURITY.md](../SECURITY.md).

## Proactive sharing and real toolbar — 8 September

Waybar0.15.0 and JetBrains Mono Nerd Font were installed in the disposable lab. `scripts/live-sharing-test.py` passed **15 additional live checks** using real stdio MCP plus simulated-human pointer/keyboard input: no-request CLI sharing, actual window click, pixel/input access, separate recording gate, cross-window denial, revoke, Escape cancellation, actual Super+Ctrl+S, Waybar Share button, view-only mode, explicit multi-client selection including the real dropdown, recipient isolation/expiry, disconnect/reconnect, paused metadata discovery, and tray activation. Some assertions are grouped into one check.

The lab-only wtype device uses `resolve_binds_by_sym=true` because wtype supplies a custom keymap; this is not a requirement for an ordinary physical keyboard. The picker/target overlays explicitly ignore Waybar's reserved zone so their coordinates stay aligned. The console uses on-demand keyboard focus rather than an exclusive grab. Final lab state: paused, no grants, test clients disconnected.

## Optional built-in OAuth — 8 September

Automated coverage includes resource/issuer discovery and 401 challenges, public and confidential DCR, exact callback checks, PKCE downgrade/mismatch, resource mismatch, one-use codes, browser-cookie binding and denial, registry persistence without plaintext client secrets, Host/Origin rejection, refresh rotation/replay revocation/consent lifetime, expiry, cross-token session hijacking, OAuth revocation clearing desktop grants, HTTP with OAuth disabled, and the **official Go SDK OAuth-capable MCP client** completing discovery → DCR → authorization → token exchange → tool calls over verified TLS.

The same official client was then run against the actual lab broker's **loopback HTTPS listener**, using the lab certificate as an explicitly trusted CA (no certificate-verification bypass). Real Quickshell clicks approved its OAuth connection, then proactively shared Kitty. Window metadata was free after authentication, pixel capture still required a local desktop grant, and `input_window` executed `echo OAUTH_MCP_CONTROL_OK`. Clicking **Revoke connection** made the prior bearer token return **401**. A layout change caused an initial click to select Pinta; that grant was revoked before selecting the correctly labelled Kitty target. This was a test-harness targeting correction, not silently reassigned authority.

The SDK makes an initialize probe during OAuth retry; initialize-only sessions are not offered as sharing recipients. A real recipient becomes visible when it starts tool discovery/use. Disconnect tests check the actual client session, not leftover initialization probes.

OAuth refresh rotation is additionally verified in automated public/confidential-client tests; the live UI run used the authorization-code flow. No independent OAuth security certification is claimed. See [AUTH.md](AUTH.md) for unsupported CIMD and deployment constraints.
