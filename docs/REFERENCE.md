# Architecture and MCP tool reference

[Quick start](../README.md) · [Setup](SETUP.md) · [Usage](USAGE.md) · [Tool reference](REFERENCE.md) · [Development](DEVELOPMENT.md)

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

The Quickshell outline is an **advisory reflection of broker state**, not the security enforcement mechanism. Enforcement is in the broker and plugin. See [SECURITY.md](../SECURITY.md) for the trust boundary and important exclusions.

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

Coordinates are **window-local logical pixels**, not scaled screenshot pixels. `view_window` returns:

- `image_size`: actual decoded PNG width/height; `logical_size`: window width/height.
- `image_to_window`: `window_x = image_x * scale_x + offset_x` (likewise Y), with zero offsets for the full toplevel. Coordinates remain bounded by the logical window.
- `revision` and its explicit alias `geometry_revision`: geometry only.
- `frame_id`: SHA-256 of the encoded PNG; equal encoded content has the same ID, even across captures. It is **not an accepted input precondition**, an accessibility snapshot, or evidence the app is still unchanged.
- `captured_at`: UTC broker timestamp after capture and its safety checks, not the compositor's presentation timestamp.

`max_width` may be 0 (default 1280) through 1920; negative values are rejected. PNG transport remains bounded to 16 MiB. No crop, JPEG/WebP, or semantic observation is implemented. The client must update coordinates after geometry changes. A revision protects against compositor geometry changes, **not arbitrary in-app content changes**.

### Batch recovery and action-and-observe

Set `"then": "screenshot"` on `input_window` for an immediate post-batch PNG, optionally with `max_width`. A nonzero `max_width` without `then` is rejected. All options are validated before input. The screenshot runs only after a completed batch and uses a fresh observation check and fresh geometry. Under the existing grant model, control and record grants include observation; observation alone never includes control or recording. There is no new permission bypass.

The result metadata is available in both MCP `structuredContent` and a JSON text block, including runtime failures and approval responses. Images are additional content blocks. The result retains `status: completed`, `actions` and `window_id`, with nested `observation` metadata and PNG content. If capture fails or permission has gone away, `observation.status` is `failed` (or `approval_required` with a request ID), but the **input remains completed**. Retry observation, not the batch. This captures immediately; it does not wait for application rendering or promise a settled frame.

Runtime input errors return MCP `isError: true` with JSON text containing `status: failed`, zero-based `failed_action`, `completed_actions`, and `completed_characters` in the failed text action (zero for other actions). Counts describe **acknowledged compositor transactions**, not confirmed app effects. The failed transaction may have delivered events before a lost reply or restoration error; `failed_transaction_may_have_effects` is therefore always true. Later actions are not attempted. Prevalidation/authorization failures still use ordinary tool errors or approval responses. Do not blindly replay interrupted text. Successful observations and failed-batch counts are audited without typed text.

### Observation safety boundary

All capture paths, including each recording frame, require a compatible compositor guard reporting an unlocked, active session both before and after `grim -T`. Missing/unknown lock status or guard failure refuses capture. Geometry, visibility and workspace changes during capture discard the frame; observation permission is checked again before releasing it. Recording stops on capture failure.

These are **sampled checks, not an atomic compositor capture fence**. A lock/unlock entirely between checks or a lock after the final check is not excluded (including before a recording frame is written). No complete screen-lock confidentiality guarantee or live lock-transition validation is claimed. See [TESTING.md](TESTING.md).

**Compatibility change:** capture previously could run without the guard; it now requires a reachable protocol-2 guard exposing lock status. An input-restoration fault also blocks observation conservatively until repair. Run `hyprland-computer-use setup` locally if the guard is missing, incompatible or faulted; MCP cannot repair or bypass it.

### Focus-preserving input (experimental)

Typing and mouse actions borrow the target's **protocol focus**, not desktop activation, and restore the original focus in the same compositor callback. The active keyboard/keymap and modifier state are restored after keyboard input; mouse actions preserve the desktop cursor position and restore pointer focus/local position. This is not a second seat: applications still receive focus transitions and may react to them. The compositor's keyboard switch also broadcasts temporary keymap changes to clients before restoring the original map.

- An explicit `focus` action **still activates** the permitted window. Omit that action when you want background input. Other actions do not implicitly activate it.
- The human takes priority: held keys/buttons, seat grabs, input capture, active pointer constraints, input-method grabs, touch focus, or an existing drag-and-drop cause a clear refusal. Unsupported targets never fall back to stealing desktop focus or moving the cursor. A refusal can follow earlier completed actions or characters; re-observe the target before retrying, rather than blindly duplicating a partially typed string.
- A drag is a bounded 20-step burst with press and release in one callback. Omit `duration_ms` or use `0`; timed drags are rejected before any actions execute. Cross-window drag-and-drop, long holds, durable hover/tooltips, and applications that require foreground activation are not supported by this mode.
- The broker's own virtual US keyboard is identified by its Wayland client process, not by the active physical keyboard. Changing physical layouts is not necessary. Multiple seats and full IME/toolkit compatibility remain unsupported/unverified.
- This requires **guard protocol 2**. Run `hyprland-computer-use setup` to replace an old/faulted guard and restart an existing broker automatically. Grants are cleared. An old guard is rejected explicitly; a restoration fault disables further input until setup repairs it.

Native mock restoration tests and exact 0.56.2 header compilation are covered; real human/agent interleaving and application behavior still require the disposable-session checks in [TESTING.md](TESTING.md). Do not treat focus preservation as a new isolation or security guarantee.

## Current limitations

- **Native Wayland root toplevels only for input.** XWayland input and popup/subsurface routing are not implemented. Separate transient toplevels require their own grants.
- The built-in Go Wayland client supplies virtual keyboard and pointer seat capabilities; the guard delivers input directly. It selects the dedicated US keyboard and an available trusted local virtual pointer, never a physical pointer.
- Text is **US-layout ASCII**. Unsupported Unicode is rejected rather than silently mistyped. Physical-keyboard/layout arbitration needs more work before everyday desktop use.
- No clipboard, arbitrary file, shell, accessibility-tree, or privileged-operation tool.
- A controlled terminal or browser retains the application's existing powers. This is not application sandboxing or semantic authorization of purchases/deletions/sudo. There is no sudo broker.
- `launch_application` relies on the application's normal launch/sandbox configuration. Chromium inside restrictive containers may need setup outside this tool; the MCP API does not offer `--no-sandbox`.
- Window recording samples actual toplevel images at approximately 5 fps with wall-clock timestamps; no audio. Maximum two concurrent recordings and 32 per broker run, with a ten-minute per-recording limit. Odd dimensions are padded for H.264. Resizing stops the recording rather than producing corrupt output.
- No secure indicator resistant to a compromised compositor or malicious same-UID process. No full multi-monitor/mixed-DPI, session-lock, or hostile-client audit has been completed.
