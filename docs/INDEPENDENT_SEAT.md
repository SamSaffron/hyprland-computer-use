# Automatic input: seat first, guarded fallback

## Setup

```sh
make build/hyprland-computer-use
./build/hyprland-computer-use setup
```

Setup builds the focus-borrowing guard, then tries the independent-seat guard. If that optional build fails, or the running compositor lacks the required popup-hook symbol, it selects focus borrowing before stopping the broker. Cancellation, version mismatch, ambiguous ownership, unsafe unloading and other setup errors are **not** reasons to bypass checks or attempt a second plugin load.

`setup --input-mode=focus-borrowing` explicitly selects the compatibility build. `--experimental-seat` remains a deprecated alias for automatic selection; it is no longer required. `--build-only` builds/publishes without touching the desktop; runtime hook availability can only be checked during live setup.

Once a seat-owning plugin has been loaded, **save work and fully restart the Hyprland session before replacing it with a different build**. A config reload is not enough. Setup compares the active plugin first, so rerunning with an already-current build is non-disruptive; when the build differs, setup prints explicit restart instructions before stopping the broker. It never restarts Hyprland for you or hot-unloads the seat-owning plugin. Do not use an older installer to bypass this check.

Use the new executable for serving and the MCP bridge. Setup may restart a verified broker and clear grants; reconnect MCP and approve again. No services or autostart entries are installed.

## Execution and the green outline

`computer_status.input_mode` reports `automatic` for the combined guard, `focus-borrowing` for the compatibility guard, or `independent-seat` for an older seat-only guard.

The combined guard selects a path from the live root client's resources before each transaction:

- **Seat:** the client bound both agent-seat keyboard and pointer resources. Input uses the separate seat without borrowing native focus.
- **Fallback:** the client lacks one or both resources. The existing compositor-guarded transaction temporarily borrows/restores native protocol focus. Human input must be idle; held keys/buttons and native grabs can cause refusal.

The green control-grant outline shows **Seat** or **Fallback**. It is keyed by the exact stable window ID and is shown only while that window's workspace is active (or the window is pinned); inactive and unknown workspaces fail closed so the output-level overlay cannot appear over an unrelated window at the same coordinates. It refreshes from live compositor metadata; if the input-mode query fails it says **Unknown**, not a guessed mode. Application identity strings and titles never select a backend or grant authority.

Fallback does not mean global injection. Both paths require the same live window/root lease, visible target, geometry revision, surface selectors and bounds. No failed or partially delivered transaction is retried. Permission denials, stale/foreign surfaces, constraints, IME grabs, invalid popup serials and other safety failures remain errors. An asynchronous menu failure cannot safely be replayed as a fallback click.

The broker creates protocol-only native virtual devices for the guarded fallback, selecting the native seat by its `Hyprland` name rather than registry order. It verifies the Wayland/guard compositor peer before creating devices and stops on compositor loss. Guard-instance IDs prevent authority from surviving a plugin replacement. Old seat-only guards retain their registry-only broker connection.

## Compatibility and limits

The exact native build target tested is **Hyprland 0.56.2**, commit `efb50993780079460b0cbed1363e2166a2de1d9f`. The seat popup hook uses a private exact-version API, not a stable plugin contract. Removing an opt-in label does not establish universal application support.

- **Kitty 0.48.2** binds only the first seat. It therefore selects **Fallback**, not Seat. Restarting Kitty cannot add multi-seat support.
- **GNOME Terminal 3.60.0 / VTE 0.84.1** supports agent-seat typing. Send one command plus Enter and observe its output before the next command. Multiline bursts can interleave terminal echo and shell output.
- **GTK3 3.24.52** can submit native/default-seat popup serials even for agent-seat grabs. Those are refused, not silently replayed through native focus borrowing.
- **Qt 6.11.2** separate-process popup and text cases passed the earlier seat-only lab. Same-process menu independence is not guaranteed: applications can swallow human input internally.
- IME, clipboard/primary selection, compositor DnD, constraints, touch/tablets and activation are not independently implemented. App-created windows can change desktop activation. Xwayland input, timed/cross-surface drags and hot unloading are unsupported.
- Capture remains actual toplevel capture. Popup pixels are not guaranteed; metadata is not proof that a menu was observed. No desktop-crop fallback exists.

Accepted agent popup serials are bounded, expiring, single-use and tied to the lease/root. Revocation, expiry, clear and destruction invalidate focus and serial authority. New toplevels need separate grants. Same-UID processes and plugins remain trusted; a controlled terminal transitively grants shell authority. This is not an OS sandbox.

## Evidence

Earlier seat-only integration tests used disposable Weston 15.0.1 → Hyprland 0.56.2 with a scratch Aquamarine 0.15.0 nested-backend patch. They covered independent Unicode, unchanged native focus/cursor, Qt popup routing, GTK invalid-serial refusal, instance/lifetime checks and revocation. They did not validate automatic fallback.

The earlier desktop-4 live run on 2026-09-09 (`/tmp/experiment/live-2f77821d`) proved fresh Kitty failed on the seat-only guard and GNOME Terminal executed three clean echo commands through real MCP/local approvals, with native focus/cursor preserved and input denied after revocation. That historical failure is why automatic fallback was added; it is not evidence of a post-update host Kitty pass.

The automatic-mode nested desktop-4 run passed 16 checks with the combined guard and real MCP/local approvals: Kitty executed `echo 'Automatic Kitty fallback OK'` through Fallback, GTK received text through Seat, both preserved/restored native focus and cursor, marker metadata matched each backend, and revocation denied further input. Artifacts: `/tmp/experiment/logs/auto-verification.json`, `auto-mcp.jsonl`, `auto-kitty-transcript.txt` and `auto-seat.log`. Nested toplevel capture timed out in an earlier attempt; this run verifies actual shell/application output, not screenshot delivery or a host-deployed update.

The real offscreen Quickshell test also verifies the production badge expression changes from Seat to Fallback without recreating the outline. It does not claim visual confirmation on a host window.

Repository checks:

```sh
make test
make native-text-wire-test
go build ./...
make independent-seat setup-native build/hyprland-computer-use
git diff --check
```

Scratch live/nested harnesses under `/tmp/experiment` are development artifacts, not installed MCP tools. Host tests must stay in the locally authorized workspace and use disposable windows and normal local approval.
