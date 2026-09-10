# Pointer actions and detailed observation

[Tool reference](REFERENCE.md) · [Live regression evidence](POINTER_CAPTURE_TESTING.md)

These extensions keep the same approved window, logical-coordinate, surface
selector and geometry-revision contract. They do not introduce desktop input,
clipboard access, provider-native tools, or a provider translation layer.

## Pointer actions

`input_window.actions` now has action-specific schemas. Supply only fields for
that action: omitted pointer coordinates are not implicitly `(0,0)`. Unknown or
inapplicable fields, null action fields, and malformed later actions fail before
any batch input. Existing key/text/focus actions are unchanged when well formed.

```json
{
  "window_id": "<instance-bound ID>",
  "revision": "<current geometry revision>",
  "actions": [
    {"type": "click", "x": 240, "y": 180, "modifiers": ["CTRL"]},
    {"type": "scroll", "x": 400, "y": 300, "delta_x": 3, "delta_y": 0, "unit": "wheel_steps"},
    {"type": "click", "x": 120, "y": 80, "click_count": 2},
    {"type": "drag", "path": [{"x": 20, "y": 20}, {"x": 60, "y": 45}, {"x": 100, "y": 20}]}
  ],
  "then": "screenshot"
}
```

| Parameter | Contract |
|---|---|
| `modifiers` | Optional `CTRL`, `SHIFT`, `ALT` on move/click/drag/scroll. Applied only within the pointer transaction; released on success and exception cleanup. No `SUPER` or global compositor shortcuts. |
| `click_count` | Click only; default 1, explicit range 1–3. Consecutive press/releases in one callback, no sleeps or configurable inter-click timing. Button remains left (default), right, or middle. |
| `delta_x`, `delta_y`, `unit` | Scroll only. Supply both axes (zero for unused). Positive X means right, positive Y means down. |
| `unit: "wheel_steps"` | Integer steps, each axis −100 through 100. Both backends send wheel-source axis data: 10 logical units and 120 high-resolution wheel units per step (or discrete steps for older pointer protocol resources). This describes input, not a guaranteed number of content pixels scrolled. |
| `unit: "logical_pixels"` | Continuous-source axis distances, each axis −1200 through 1200. No discrete wheel hint. Applications still choose their scrolling behavior. |
| `path` | Drag only, 2–64 `{x,y}` points. One press at the first point and one release at the last, with every segment planned before delivery. Each segment uses 20 subdivisions; at most 1261 emitted path points. Every delivered point must resolve to the same surface. |

Legacy straight drags (`x`, `y`, `to_x`, `to_y`) remain supported; do not mix them
with `path`. Legacy vertical `delta` retains its original backend-specific
semantics; do not combine it with the new scroll axes. `duration_ms: 0` remains a
deprecated compatibility no-op for drags. Nonzero durations, long holds, and
mouse-down state spanning requests remain unsupported.

**An unpaced path is not a timed gesture.** GTK and other toolkits can compress
queued motion samples. Drawing applications must consume raw motion if every
waypoint matters. The live GTK drawing fixture disables event compression in
both baseline and candidate runs; this is not a claim about arbitrary drawing
applications. Multiclick interpretation also remains application-dependent.

The guard advertises `pointer_actions_version: 1`. A batch using extensions is
refused before its first input if the guard is older; it never silently drops
modifiers, axes, click counts or waypoints. Run local `setup` after updating the
executable. Replacing an active seat-owning guard still requires a full Hyprland
session restart, not hot unloading.

## Crop and zoom

`view_window` accepts `region`, `max_width` and `max_height`:

```json
{
  "window_id": "<instance-bound ID>",
  "region": {"x": 1240, "y": 220, "width": 340, "height": 100},
  "max_width": 1360,
  "max_height": 400
}
```

- Region coordinates and dimensions are integer **window-local logical pixels**.
  The whole rectangle must be inside the approved toplevel. A surface selector
  on input does not change the observation region's coordinate system.
- Capture uses the actual toplevel at full buffer detail before sampling the
  region. It never captures the desktop and crops it for isolation.
- Aspect ratio is preserved. Each output bound accepts 0–1920; 0 means default
  width 1280 / height 1920. Full-window views are not enlarged. Regions may be
  magnified up to 4× source pixel resolution, using nearest-neighbour sampling.
  No generative enhancement is performed.
- Actual PNG buffer dimensions determine logical-to-image scaling, including
  HiDPI captures. Output size bounds are checked against the resulting image,
  rather than assuming `grim -s` returns logical-sized pixels.
- A crop's `image_to_window` includes its logical X/Y offsets and the ratio of
  region dimensions to output dimensions. `logical_size` still describes the
  whole window. `region` identifies the requested rectangle. Map the coordinates
  back before input; a zoom does not change input's coordinate system.
- Full-resolution crop/resample decoding is capped at 8192 pixels per edge and
  16,777,216 source pixels; encoded PNG transport remains capped at 16 MiB.
  Large source buffers can therefore be refused even for a small region.
- Existing geometry/visibility/workspace checks, sampled lock checks and final
  permission rechecks still apply. A crop does not establish a new security
  boundary within an approved window or make lock checks atomic.

Use the same options after input:

```json
{
  "window_id": "<instance-bound ID>",
  "revision": "<current geometry revision>",
  "actions": [{"type": "click", "x": 240, "y": 180}],
  "then": "screenshot",
  "observation": {
    "region": {"x": 200, "y": 140, "width": 120, "height": 100},
    "max_width": 480,
    "max_height": 400
  }
}
```

`observation` requires `then: "screenshot"`; invalid initial options are refused
before input. A later window change or capture failure is still reported as an
observation failure **without changing completed input into failed input**.
Re-observe; never replay the batch merely because the screenshot failed.
