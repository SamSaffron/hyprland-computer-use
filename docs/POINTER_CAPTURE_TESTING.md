# Pointer/capture live verification — September 11, 2026

[API and limitations](POINTER_CAPTURE.md) · [General testing status](TESTING.md)

## Model-driven red → green

A real `term-llm ask` process operated the visible native Wayland GTK workbench
through the project's MCP transport. No scripted pointer input, app state,
source code, shell, files or browser automation were supplied to that model.
A separate local UI client simulated human approval of the fixture window only.
The evaluator read the fixture's event/state JSON to check the visible result.

| Card | Unchanged upstream `7901ac885016035042f87d7e55aff248e19a7f84` | Candidate |
|---|---|---|
| Ctrl + click | Blocked by missing parameter | PASS |
| Horizontal wheel scroll | Blocked by missing axis | PASS |
| Double click | PASS via repeated clicks | PASS via `click_count: 2` |
| Triple click | PASS via repeated clicks | PASS via `click_count: 3` |
| One continuous A → B → C stroke | Blocked by endpoint-only drag | PASS via `path` |
| Tiny label entry | PASS from full image; no crop available | PASS after explicit crop/zoom |
| **Total** | **3/6** | **6/6** |

The baseline correctly explained why gestures 1, 2 and 5 could not be expressed.
The candidate actually issued these representative calls (identity/revision
fields omitted here only for readability):

```json
{"type":"click","x":300,"y":190,"button":"left","modifiers":["CTRL"]}
{"type":"scroll","x":900,"y":190,"delta_x":1,"delta_y":0,"unit":"wheel_steps"}
{"type":"click","x":300,"y":375,"button":"left","click_count":2}
{"type":"click","x":900,"y":375,"button":"left","click_count":3}
{"type":"drag","button":"left","path":[{"x":110,"y":690},{"x":570,"y":580},{"x":1090,"y":690}]}
```

Its `view_window` crop was:

```json
{"region":{"height":100,"width":340,"x":1240,"y":220},"max_height":400,"max_width":1360}
```

### Comparable inputs and limits

- term-llm **v0.9.37**, commit `e756fe42`; provider `chatgpt:gpt-6-astra`.
- Same model alias, system message, task prompt, 24-turn limit, 12,000 maximum
  output tokens, provider defaults, and no local/search/skill tools.
- Same finalized fixture source and fresh state, window position `(40,90)`,
  logical size **1800×880**, output **1920×1080 at scale 1**.
- Hyprland **0.56.2**, commit `efb50993780079460b0cbed1363e2166a2de1d9f`;
  GTK **3.24.52** / native Wayland. Model runs used the automatic build's Seat
  path. The lab's private Aquamarine 0.15.0 rendering compatibility patch was
  unchanged between runs; it is not a project patch or general deployment claim.
- Tool schemas, input implementation and screenshot processing deliberately
  changed. The baseline exposed native 3600×1760 buffer captures despite logical
  sizing; candidate output bounds are enforced and detailed crops are available.
  These are not identical-raster inference comparisons or a latency benchmark.
- This is one feature regression task, **not a statistical model-quality result**.
  The baseline also read the code correctly in the matched run: do not attribute
  a recognition improvement to crop/zoom. Its actual invocation/output is what
  the green run verifies.

### Discovery runs retained, not counted as the matched pair

The initial fixture used GTK's default motion compression. The exploratory
baseline scored **2/6**, including a misread code; the first candidate scored
**5/6**, because GTK discarded intermediate burst motion and rendered A→C.

The final drawing fixture explicitly disables motion compression on its drawing
window. This is a fixture requirement for observing raw motion, not a change to
pass/fail criteria or paced delivery in the guard. **Both** baseline and candidate
were rerun with that same fixture. The final prompt also explicitly requested a
magnified label view when available, identically for both runs. Original logs,
screenshots and state files remain in private lab evidence, not overwritten.
This test does not prove arbitrary applications preserve an unpaced drag path.

## Deterministic backend regression

`scripts/live-pointer-test.py` independently passed against both the automatic
build with a **Seat** target and the **focus-borrowing** build with a **Fallback**
target. These are scripted MCP regression checks, not additional model runs:

- All six fixture cards, including one continuous waypoint stroke.
- Modifier delivery and no modifier leakage into later ordinary clicks.
- Both scroll axes, both units and opposing signs.
- Post-action cropped screenshot and crop-to-window offsets.
- Malformed later actions and out-of-window paths rejected without earlier
  batch effects; stale geometry refused.
- Revocation requires new approval; pause refuses capture.

An initial deterministic probe correctly failed closed on a GTK configure race
(`stale_geometry`, zero acknowledged actions). The harness now observes settled
geometry before constructing its inputs; the rejected run is retained.

Automated checks additionally cover schema shape/presence, extension-version
preflight against old guards, crop bounds and aspect ratio, 1×/1.5×/2× buffer
scaling, native path planning/cross-surface refusal, and pointer exception
cleanup. Run `make test` and `make native-build-check` on the pinned environment.

## Reproduce without private evidence

1. Use a disposable, visible Hyprland session with matching headers. Do not run
   these scripts on a desktop with personal work. Build and install each revision
   separately; replacing a seat-owning guard requires a **full session restart**.
2. Start the broker and local console. Install GTK3, Python GObject and Cairo.
3. Run `python scripts/interaction-workbench.py /absolute/path/to/state.json`.
   Set the window to 1800×880 and allow its configure to settle. Use identical
   fixture code for old/new binaries; do not give its state/source to the model.
4. Connect term-llm's `computer-use-pr` MCP entry to the broker and run:

```sh
term-llm ask --provider chatgpt:gpt-6-astra --mcp computer-use-pr \
  --tools '' --skills none --no-search --max-turns 24 \
  --max-output-tokens 12000 --yolo --json \
  --system-message 'You are testing permissioned GUI interaction. Only the supplied MCP tools may operate or inspect the desktop. The fixture state files and source are not available to you.' \
  "$(cat prompt.txt)" > term.jsonl 2> term.stderr
```

Approve only the fixture window from a separate human/local-UI connection.
The exact shared task prompt is:

> Use only the computer-use-pr MCP tools to operate the window titled "Computer-use interaction workbench". Request control of that window; the lab's separate simulated-human approver will approve it. Complete all six visible cards using the requested mouse gestures and enter the tiny printed code. Before transcribing the tiny label, obtain a magnified view of that label if your tools support it; otherwise state that magnification is unavailable and do your best with the full image. Read instructions and coordinates from screenshots; use their image-to-window mapping. Do not use a terminal, shell, browser automation, files, clipboard, application launch or any non-MCP workaround. You have at most 24 turns. If the available tools cannot express a gesture, explain that limitation and continue with the other cards rather than repeating failed workarounds. Finish by taking a screenshot and reporting which cards passed and which remain blocked. Do not claim success from tool acknowledgments; check the actual visible result.

For deterministic checks, reset the fixture and run:

```sh
COMPUTER_USE_DISPOSABLE=1 \
COMPUTER_USE_WORKBENCH_STATE=/absolute/path/to/state.json \
COMPUTER_USE_ARTIFACT_DIR=/absolute/path/to/evidence \
python scripts/live-pointer-test.py
```

That script simulates approval through the separate UI transport and revokes /
pauses on exit. Disable any concurrent automated approver before testing revoke.
It saves a screenshot and image-free MCP trace and checks evaluator-only state.
No provider-native adapter, shell tool, timed hold, clipboard mechanism or wider
sandbox/security guarantee is introduced by this work.
