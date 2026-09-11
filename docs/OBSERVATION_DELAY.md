# Post-action observation

`input_window` acknowledges compositor input transactions, not application
rendering. Its immediate screenshot can still contain the pre-action UI. Do not
blindly replay a batch because the image looks unchanged.

## Give the application time to render

```json
{
  "window_id": "18000005",
  "revision": "60,120,1100,700",
  "actions": [
    {"type": "key", "key": "CTRL+A"},
    {"type": "text", "text": "Release ready\nAll checks passed.\n"}
  ],
  "then": "screenshot",
  "observation": {"delay_ms": 200}
}
```

- `delay_ms` is an integer **0–5000 milliseconds**. Omitted or **0** preserves
  immediate capture. There is no new global sleep or default latency.
- It is valid only inside `input_window.observation`, with `then: "screenshot"`.
  Standalone `view_window`, recording and `then: "state"` are unchanged.
- It composes with the existing observation `region`, `max_width`, and
  `max_height`; input coordinates remain window/surface-local logical pixels.
- Invalid delay/crop options are rejected before any input. A failed or
  approval-required input batch does not wait or capture.
- The wait starts after a completed input batch, after releasing the broker
  input mutex and any synthetic input state. It runs in the broker, **never
  inside a compositor callback**. Other input/revocation work is not locked out.
- Cancellation/deadline during the wait produces a failed observation while
  retaining the completed input status/count. Retry observation, not input.
  A disconnected client may not receive any result; do not assume cancellation
  undid the already delivered input.
- Capture after the wait uses fresh permission, session, window and geometry
  checks. Revocation/expiry does not make a delayed screenshot authorized.
  A resized window is observed at its new geometry; subsequent input must use
  that new revision. Closing the target never retargets another window.
- The bounded wait is additional to the existing input execution budget. It
  neither extends grants nor disables normal capture timeouts.

**A delay is not a settled-frame guarantee.** A slow app or network operation
may need more time; animation may never become static. Even two identical
screenshots might both show the old frame. Pick a bounded delay appropriate to
the operation, inspect the result, and request another observation if needed.

## Real-application test

The opt-in harness opens its own unmodified Mousepad process and local test
note. All editor input and screenshots use MCP; a separate same-user UI
connection simulates local human approval for that one window. It does not give
a model access to the evaluator channel. Never run it on a personal desktop.

Inside the disposable, running Hyprland session, with the matching broker,
permission console, Python and Mousepad available:

```sh
COMPUTER_USE_DISPOSABLE=1 \
CU_DELAY_EVIDENCE=/absolute/private/path/mousepad-delay \
python scripts/live-observation-delay-test.py
```

Defaults: two warmup pairs, then ten pairs, alternating **0ms / 200ms** order.
Each trial resets the same note, types the same replacement text for both arms,
saves the exact post-action returned PNG, then saves an evaluator-only later
MCP reference image. There is no harness sleep inserted before the measured
post-action screenshot: the candidate broker implements `delay_ms`.

For an older broker that lacks the option, run `CU_DELAY_VALUES=0`; the harness
refuses to test a nonzero delay unless the live MCP schema advertises it.
`CU_DELAY_REPEATS` changes the number of scored repetitions.

Copy the evidence directory to a machine with Tesseract and score the actual
images (warmups are excluded):

```sh
python scripts/live-observation-delay-test.py --score /path/to/mousepad-delay
```

The scorer checks both complete expected lines in each returned image and the
later reference. An incorrect reference invalidates the comparison. Review
actual images if OCR is ambiguous. This measures **returned-frame freshness**,
not application completion time or LLM task success. The harness closes only
its own editor and MCP connection, restores an initially paused broker, and
retains the note, traces and PNGs as evidence.
