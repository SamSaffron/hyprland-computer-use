# Popups, subsurfaces and dialog transitions

This implementation expands input **inside a shared native window's surface tree**, not authority over other windows. It is source/unit tested and compiled against Hyprland 0.56.2; live toolkit validation is still required.

## What works and what does not

- Default window-local pointer input hit-tests mapped subsurfaces, including nested offsets, stacking and input regions. It does not blindly send every coordinate to the root surface.
- `window_state` discovers mapped native XDG popups and subsurfaces. An explicit surface ID plus geometry revision selects a popup/subsurface for pointer, key or text input. Coordinates then become **selected-surface-local logical pixels**; a popup may extend outside the root rectangle.
- Each input transaction rechecks the root-window lease, live surface object, current tree membership and surface geometry. A closed/foreign/stale surface is refused, never silently replaced with the root. Surface IDs are selectors, **not grants**.
- **Active seat grabs remain refused**, even when the grabbed popup belongs to the shared window. Many toolkit menus use these grabs; this change is not universal menu support. Safely handling a popup grab without disturbing human focus is a separate unresolved item. Do not bypass this refusal with global input or automatic focus actions.
- A drag must stay on one actual surface for its entire bounded path. All path points are resolved before pointer-enter/press. Cross-surface or cross-window drag-and-drop remains unsupported.
- A new transient **toplevel** is a separate window. It needs its own control grant, even if it has the same PID, title or application as the shared parent.
- Capture is unchanged: `view_window` still uses actual `grim -T` toplevel capture. **Popup pixels are not guaranteed to be included.** Surface geometry is not a substitute for observing menu contents. No desktop crop or separate popup-capture backend is introduced.

## Discover state

```json
{"window_id":"18000002"}
```

Call that with `window_state`. Example response shape (illustrative):

```json
{
  "status": "ok",
  "state": {
    "window_id": "18000002",
    "revision": "20,40,600,800",
    "surface_tree_version": 1,
    "surfaces": [
      {
        "surface_id": "opaque-live-surface-handle",
        "kind": "popup",
        "revision": "-20,790,200,90",
        "offset": [-20,790],
        "size": [200,90]
      }
    ],
    "related_windows": [
      {
        "window_id": "18000003",
        "relationship": "transient_child",
        "separate_grant_required": true
      }
    ],
    "related_windows_truncated": false,
    "seat_grab_active": false,
    "session_locked": false
  }
}
```

`kind` is `toplevel`, `subsurface` or `popup`. Offsets are relative to the root window's logical origin; for a known window-local point, subtract the selected surface's offset to obtain surface-local coordinates. Do not add desktop coordinates to `input_window`. Mixed DPI and client-side-decoration offsets need live verification in the target toolkit.

Like `list_windows`, this tool is **free metadata discovery**, including while paused or without a supervisor. It returns no pixels, control names, editable text or permission decisions. The guard supplies direct native XDG parent/child relationships, not PID/title heuristics. Portal/independent dialogs that do not declare such a relationship may be absent; use `list_windows` and local approval rather than guessing authority.

Surface output is capped at 128 mapped nodes; an oversized/deep/ambiguous tree fails closed rather than returning a partially routable tree. Related toplevels are capped at 64 with an explicit truncation flag. The guard retains at most 4,096 live object-pair handles. Handles change across plugin reloads and do not authorize access on another MCP connection. A remapped **same** surface object can retain its handle; rediscover state after closing/reopening menus rather than assuming a handle is an appearance/snapshot ID.

## Input and transitions

```json
{
  "window_id": "18000002",
  "revision": "20,40,600,800",
  "surface_id": "opaque-live-surface-handle",
  "surface_revision": "-20,790,200,90",
  "actions": [{"type":"click","x":20,"y":15}],
  "then": "state"
}
```

Both selector fields are required together. A `focus` action with a surface selector is rejected; explicit window activation retains its existing separate meaning. Without selector fields, pointer coordinates retain the original window-local convention and hit-test only the root's subsurfaces—popups are not silently selected by an overlapping coordinate. Without a selector, keyboard/text input still targets the root.

Surface revisions describe bounds, not app content or descendant-tree freshness. Each delivery resolves the current tree. Do not treat a surface revision as an accessibility locator or UI snapshot token.

`then: "state"` returns a nested `window_state` result after a completed batch. It does not require pixel-observation permission. If discovery fails because the parent closes or the guard is unavailable, the outer input result remains **completed** and the nested result is **failed**. Retry discovery, not the input batch. `max_width` is valid only with `then: "screenshot"`.

Post-state is immediate, not a lifecycle/render wait. A menu or dialog may appear after it returns. Call `window_state` again if needed. To operate a reported transient child, request/obtain a grant for the child's own `window_id`, then use the child's geometry revision. After closing it, rediscover the surviving parent and its surfaces. If the parent has closed, use `list_windows`; do not redirect an old selector to another window.

## Manual acceptance test

Use disposable native Wayland windows and the local Approve console. Update using the newly built executable's local `setup`, reconnect MCP, and approve a fresh grant for one window B. No host plugin was loaded during implementation.

1. Ask the agent for `window_state(B)` before input. Verify the root and any existing subsurfaces. Merely discovering state must not grant control or pixels; try discovery while paused, then resume locally.
2. In a toolkit with subsurface content, click/scroll inside that content using the ordinary B-local coordinates. Check actual app effects and that the next physical input still goes to the previously focused human window. Repeat with nonzero nested offsets and your display scaling.
3. Open a disposable dropdown/popover, then rediscover state. If `seat_grab_active` is true, input refusal is the expected **unsupported case**, not a pass for menu interaction. Report the app/toolkit and do not use a global fallback.
4. For a non-grabbed popup with observable contents, use its ID/revision and local coordinates to click a harmless item; verify the actual result. Test a popup extending past the root rectangle, and verify that an out-of-popup coordinate is refused. If popup pixels are absent from capture, report that observation limitation instead of guessing an item coordinate.
5. Close the popup and attempt to use its old selector **while closed**. It must fail without sending input to the parent. Reopen and rediscover instead of reusing old state. Reposition/resize and confirm an old surface revision is rejected.
6. Open a separate native dialog (for example, an editor's preferences or Save As). Check `related_windows` if the app declares an XDG parent. Attempt input before local approval: the parent grant must not authorize it. Grant the dialog separately, operate a harmless control, close it, then rediscover the parent.
7. Revoke B's grant while its popup is open. Later popup input must be denied. An ID discovered by another MCP connection must likewise confer no authority. Cross-surface drag attempts should refuse before any press, not approximate a global drag.

Please report the exact Hyprland build, app/toolkit versions, whether the menu uses a grab, whether popup pixels were captured, actual click/scroll results, and the next physical-input destination. These tests do not establish universal toolkit/IME behavior, hidden-workspace operation or independent seats.
