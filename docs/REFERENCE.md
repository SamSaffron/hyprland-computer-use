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

The tray item uses the StatusNotifierItem protocol and works with a tray host such as Waybar or Quickshell. Opening the permission console from the tray places it beside the toolbar on that monitor, inward from its top, bottom, left or right edge. Activation coordinates and the containing thin layer-surface rectangle guide placement; hosts that omit coordinates use a one-time cursor-position query. If toolbar geometry is unavailable, the nearest monitor edge is used. Placement is clamped to the screen and does not follow the cursor afterward. A manual console launch without tray metadata retains the top-right default. `COMPUTER_USE_DEMO_TRAY=1` enables a small Quickshell tray host for an otherwise bare test compositor. Normal desktop use should use its existing tray host.

### Why a compositor plugin?

“Focus window A, then inject global input” has a race. The compositor guard instead validates a short-lived lease, live window object, root surface, visibility, and geometry revision **inside Hyprland's event loop**, then delivers input to that surface. It verifies the seat accepted the intended focus before sending events. Ordinary input prefers an independent seat; clients without both agent-seat devices use **guarded focus-borrowing transactions**: complete keypresses, clicks, scrolls and bounded drag paths restore the previous seat focus before returning to the event loop. Pointer actions do not warp the desktop cursor. There is **no global-input fallback**.

The plugin has a trusted same-UID local control channel used by the broker; it does not authenticate one particular process. It checks expiry independently, rejects stale geometry/out-of-bounds pointer coordinates, and invalidates leases when the bound window/surface disappears. Synthetic presses are released inside each transaction, including exception cleanup; no held input spans broker requests. Its exact Hyprland build hash must match the headers used to build it.

The Quickshell outline is an **advisory reflection of broker state**, not the security enforcement mechanism. It is an output-level overlay keyed by the exact stable window ID and gated by Quickshell's live active-workspace state; it is hidden on inactive or unknown workspaces so it cannot mark an unrelated window occupying the same coordinates. Enforcement remains in the broker and plugin. See [SECURITY.md](../SECURITY.md) for the trust boundary and important exclusions.

The executable is **`hyprland-computer-use`**. Use **`computer-use`** as the MCP server name in your client configuration.

## MCP tools

| Tool | Purpose |
|---|---|
| `computer_status` | Mode, pause state, this client's grants and requests |
| `request_permission` | Request a specific capability/scope; never grants it |
| `wait_for_permission` | Wait up to 120 seconds for a local decision, then retry |
| `list_windows` | Free metadata discovery; optional workspace filter, no pixels |
| `window_state` | Surface-object IDs/geometry and declared transient toplevel relationships; metadata only |
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

For `view_window` only, optional `max_width` may be 0 (default 1280) through 1920; negative values are rejected. `input_window` has no capture-size argument because input coordinates are always logical window coordinates, and its optional post-action screenshot uses the standard 1280 maximum. PNG transport remains bounded to 16 MiB. No crop, JPEG/WebP, or semantic observation is implemented. The client must update coordinates after geometry changes. A revision protects against compositor geometry changes, **not arbitrary in-app content changes**.

### Batch recovery and action-and-observe

Set `"then": "screenshot"` on `input_window` for an immediate post-batch PNG at the standard capture size. The screenshot runs only after a completed batch and uses a fresh observation check and fresh geometry. Under the existing grant model, control and record grants include observation; observation alone never includes control or recording. There is no new permission bypass.

The result metadata is available in both MCP `structuredContent` and a JSON text block, including runtime failures and approval responses. Images are additional content blocks. The result retains `status: completed`, `actions` and `window_id`, with nested `observation` metadata and PNG content. If capture fails or permission has gone away, `observation.status` is `failed` (or `approval_required` with a request ID), but the **input remains completed**. Retry observation, not the batch. This captures immediately; it does not wait for application rendering or promise a settled frame.

Runtime input errors return MCP `isError: true` with JSON text containing `status: failed`, zero-based `failed_action`, `completed_actions`, and `completed_characters` in the failed text action (zero for other actions). Counts describe **acknowledged compositor transactions**, not confirmed app effects. The failed transaction may have delivered events before a lost reply or restoration error; `failed_transaction_may_have_effects` is therefore always true. Later actions are not attempted. Prevalidation/authorization failures still use ordinary tool errors or approval responses. Do not blindly replay interrupted text. Successful observations and failed-batch counts are audited without typed text.

### Unicode and bulk text

Text actions accept UTF-8 Unicode, with an aggregate limit of 262,144 UTF-8 bytes per batch and a 60-second execution budget. Delivery uses bounded 48-scalar compositor transactions, without clipboard access or per-character sleeps. LF and TAB send Return and Tab; other C0/C1 controls (including CR/CRLF) are rejected before any batch effects. Convert line endings to LF. Text is keyboard input, not guaranteed literal insertion: app shortcuts, autoindent and form submission still apply.

Temporary text maps go only to the target client's keyboard resources and are explicitly restored; they are never installed as the global seat keyboard. Failure progress counts Unicode scalars, not graphemes or bytes, and a lost chunk reply can leave up to 48 scalars uncertain. A text-containing batch requires the guard's `unicode_text` feature before executing any actions. Run local `setup` after updating the executable. See [Unicode/bulk text and manual test](TEXT_INPUT.md) for exact semantics and live-validation limits.

### Surface routing and dialog transitions

`window_state(window_id)` returns bounded mapped subsurface/popup metadata and direct XDG transient parent/child window IDs. It is free metadata, not pixels or authority. `input_window` accepts optional paired `surface_id` and `surface_revision`; coordinates then become local to that surface, including popups outside the root rectangle. Without a selector, pointer input hit-tests root subsurfaces and keyboard input targets the root. Every transaction rechecks root authority, current tree membership and geometry. Closed/foreign/stale selectors never fall back to the root. Cross-surface drags are refused before pointer-enter/press.

`then: "state"` requests immediate post-batch metadata under nested `window_state`. Discovery failure does not change the completed input result, and this is not a lifecycle wait. A newly discovered toplevel still needs its own grant. **Active seat grabs remain refused**, so many grabbed menus are not yet supported. Popup capture is not guaranteed; no desktop-crop fallback exists. See [surface contract and manual test](SURFACES.md).

### Observation safety boundary

All capture paths, including each recording frame, require a compatible compositor guard reporting an unlocked, active session both before and after `grim -T`. Missing/unknown lock status or guard failure refuses capture. Geometry, visibility and workspace changes during capture discard the frame; observation permission is checked again before releasing it. Recording stops on capture failure.

These are **sampled checks, not an atomic compositor capture fence**. A lock/unlock entirely between checks or a lock after the final check is not excluded (including before a recording frame is written). No complete screen-lock confidentiality guarantee or live lock-transition validation is claimed. See [TESTING.md](TESTING.md).

**Compatibility change:** capture previously could run without the guard; it now requires a reachable supported guard exposing lock status. An input-restoration fault also blocks observation conservatively until repair. Run `hyprland-computer-use setup` locally if the guard is missing, incompatible or faulted; MCP cannot repair or bypass it.

### Automatic input and focus-borrowing fallback

The default combined guard prefers its own seat and chooses guarded focus borrowing only when the target client lacks agent-seat keyboard or pointer resources, before delivery. The green grant outline shows **Seat** or **Fallback**. Safety errors and partial deliveries are never replayed. See [backend selection and compatibility](INDEPENDENT_SEAT.md). The following describes the fallback path:

Typing and mouse actions borrow the target's **protocol focus**, not desktop activation, and restore the original focus in the same compositor callback. The active keyboard/keymap and modifier state are restored after keyboard input; mouse actions preserve the desktop cursor position and restore pointer focus/local position. This is not a second seat: applications still receive focus transitions and may react to them. The compositor's keyboard switch also broadcasts temporary keymap changes to clients before restoring the original map.

- An explicit `focus` action **still activates** the permitted window. Omit that action when you want background input. Other actions do not implicitly activate it.
- The human takes priority: held keys/buttons, seat grabs, input capture, active pointer constraints, input-method grabs, touch focus, or an existing drag-and-drop cause a clear refusal. Unsupported targets never fall back to stealing desktop focus or moving the cursor. A refusal can follow earlier completed actions or characters; re-observe the target before retrying, rather than blindly duplicating a partially typed string.
- A drag is a bounded 20-step burst with press and release in one callback. Omit `duration_ms` or use `0`; timed drags are rejected before any actions execute. Cross-window drag-and-drop, long holds, durable hover/tooltips, and applications that require foreground activation are not supported by this mode.
- The broker's own virtual US keyboard is identified by its Wayland client process, not by the active physical keyboard. Changing physical layouts is not necessary. The native seat is identified by protocol name, not registry order. Full IME/toolkit compatibility remains unverified.
- Focus borrowing is available in **guard protocols 2 and 3**. Run `hyprland-computer-use setup` to replace an old/faulted guard and restart an existing broker automatically. Grants are cleared. A loaded seat-owning guard requires a Hyprland restart before replacement; setup refuses hot unloading. An old guard is rejected explicitly; a restoration fault disables further input until setup repairs it.

Native mock restoration tests and exact 0.56.2 header compilation are covered; real human/agent interleaving and application behavior still require the disposable-session checks in [TESTING.md](TESTING.md). Do not treat focus preservation as a new isolation or security guarantee.

## Current limitations

- **Native Wayland window trees only for input.** Subsurface routing and explicit popup selection are implemented but not live-validated. Active grabs and cross-surface drags remain refused; XWayland is unsupported. Separate transient toplevels require their own grants.
- The built-in Go Wayland client supplies virtual keyboard and pointer seat capabilities; the guard delivers input directly. It selects the dedicated US keyboard and an available trusted local virtual pointer, never a physical pointer.
- Text supports Unicode scalars through target-client keymaps; shortcut `key` actions still use the US layout. Universal toolkit/IME behavior and physical-keyboard arbitration require live testing. See [text input](TEXT_INPUT.md).
- No clipboard, arbitrary file, shell, accessibility-tree, or privileged-operation tool.
- A controlled terminal or browser retains the application's existing powers. This is not application sandboxing or semantic authorization of purchases/deletions/sudo. There is no sudo broker.
- `launch_application` relies on the application's normal launch/sandbox configuration. Chromium inside restrictive containers may need setup outside this tool; the MCP API does not offer `--no-sandbox`.
- Window recording samples actual toplevel images at approximately 5 fps with wall-clock timestamps; no audio. Maximum two concurrent recordings and 32 per broker run, with a ten-minute per-recording limit. Odd dimensions are padded for H.264. Resizing stops the recording rather than producing corrupt output.
- No secure indicator resistant to a compromised compositor or malicious same-UID process. No full multi-monitor/mixed-DPI, session-lock, or hostile-client audit has been completed.
