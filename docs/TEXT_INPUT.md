# Unicode and bulk text

`input_window` accepts Unicode through the existing `text` action. It uses no clipboard, shell, global typing utility, accessibility mutation or new permission capability.

## Contract

- UTF-8 Unicode scalar sequences are delivered in order, without normalization. Combining marks, variation selectors, ZWJ emoji sequences and supplementary-plane characters are preserved at the protocol level. Chunk boundaries can split grapheme/ZWJ sequences; fonts, shaping and actual insertion remain application responsibilities.
- LF (`\n`) sends Return; TAB (`\t`) sends Tab. These can submit a form, execute a terminal command or move focus **inside the application**. This is keyboard delivery, not a promise of literal document insertion or paste semantics.
- Other C0/C1 controls, including NUL, Escape, DEL and CR, are rejected before **any action in the batch** executes. Convert CR/CRLF line endings to LF; use explicit `key` actions for intentional control keys.
- The aggregate text budget is **262,144 UTF-8 bytes per batch**, across all text actions. A batch still has at most 128 actions, with a 60-second execution budget. Empty text is a no-op.
- Each native transaction delivers at most **48 Unicode scalars**, restoring the prior keymap/focus/modifiers before yielding. Slots use ordinary printable-key positions, not raw Copy/Paste/Cut or modifier keycodes. There are no per-character IPC calls or deliberate per-character sleeps. Grant, geometry, lock and idle-input checks apply to each chunk. Revocation cannot interrupt a chunk already admitted to the compositor callback.
- `completed_characters` on failure counts acknowledged **Unicode scalars** in the failed text action, not UTF-8 bytes or user-perceived graphemes. Prior successful chunks are included. A lost reply leaves up to one chunk uncertain; a reported partial chunk can also include an uncertain failing key. Re-observe before retrying. Completed delivery does not prove the application inserted all characters. The authorized target receives the whole chunk map before its keypresses, so progress counts are not a bound on text already disclosed to that application.
- `key` actions retain the existing US shortcut layout. Text does not depend on the physical keyboard layout or an IME compose sequence. Active IME grabs, held physical input, unsupported targets and restoration faults are refused rather than bypassed.

## How targeting works

For each chunk, the compositor validates its live window/root-surface lease and creates a small self-contained XKB map in a sealed anonymous file. It borrows the existing broker keyboard, sends the text-specific map **only to the target client's keyboard resources used by that transaction**, delivers paired press/release events, then explicitly restores the previous map and seat state.

The text-map carrier is never registered as a device or installed as the seat keyboard. This is important: installing a text-bearing map globally could broadcast the text to unrelated clients. The ordinary broker US-map switch still has the previously documented focus/keymap side effects. Same-client windows are not independent Wayland clients; application-internal behavior remains trusted, and same-application live tests are required.

The Go Wayland client is unchanged and still sends no key events. No clipboard reading, clipboard ownership changes, process/title-based authority inheritance or global-input fallback has been added.

## Upgrade and compatibility

Use a newly built executable and run its local `setup` command to rebuild/replace the guard and restart the broker. Reconnect the MCP client and approve fresh grants. A text-containing batch checks the guard's `unicode_text` feature and exact chunk limit before executing any actions. An old protocol-2 guard without this feature gives `unicode_text_guard_unavailable` with setup guidance; it does not silently revert to the old ASCII path.

The native implementation compiles against **Hyprland 0.56.2** headers. Native mock tests exercise the actual transaction code; optional real-libxkbcommon tests compile the actual native generator and check Unicode keysyms/UTF-32. This is **not live app/toolkit evidence**. XWayland, popup/subsurface routing, universal IME compatibility and literal paste semantics remain unsupported. See [TESTING.md](TESTING.md).

## Manual test: two disposable editor windows

Do this only when you are ready to update the running broker/plugin. From the checkout:

```sh
make build/hyprland-computer-use
./build/hyprland-computer-use setup
```

If setup reports that it restarted the broker, do not start a second one. Otherwise start `./build/hyprland-computer-use serve`. Ensure your MCP client uses this executable, reconnect it, and keep the local permission console connected in Approve mode.

1. Open **two disposable native Wayland plain-text editor windows**, A and B. Prefer different applications for the first run. Disable automatic indentation/completion in B for exact multiline comparisons. Place B's caret in an empty document, then leave human focus in A.
2. Locally grant the agent control of **B only**, for five minutes. Ask it to list windows and use B's exact ID and revision. Tell it **not to use a `focus` action, clipboard, shell, or alternative input backend**.
3. Ask it to send this as **one `text` action**, followed by `then: "screenshot"`:

   ```text
   ASCII: Hello, world! 123 [] {} !@#$%
   Accents: Café naïve résumé — € £
   Greek: Ελληνικά
   Cyrillic: Привет мир
   Hebrew: שלום עולם
   Arabic: مرحبا بالعالم
   Hindi: नमस्ते दुनिया
   CJK: 你好世界 日本語 한국어
   Emoji: 🚀 🐈 👩‍💻 ❤️
   Combining: é ä
   ```

   The final line intentionally uses combining marks. Check B's actual content, not merely RPC success or the first immediate screenshot. Missing glyph boxes may be a font issue; incorrect/missing characters are a delivery issue. RTL visual order is not logical storage order.
4. Type `HUMAN-STILL-HERE` physically. It must reach **A**, not B. Verify neither desktop activation nor cursor position moved. Click B yourself and type a normal character to check its keyboard map was restored, then return to A.
5. **Bulk test:** in a fresh empty B document, ask for one text action with 200 numbered lines, each in this form (001 through 200):

   ```text
   001 | Café — Ελληνικά — 你好世界 — 🚀 — é
   ```

   Check first/last line, all 200 lines, and no missing/duplicated content. Report elapsed seconds; correctness means all 200 lines match, not meeting an unmeasured speed threshold. There is no old deliberate 8 ms delay per character. Large single actions now exceed the former 4,096-byte limit legitimately.
6. **Refusal and recovery:** hold a physical modifier while requesting a short text action. It should refuse without typing. Release it and explicitly request a new action. For a longer request, use local Pause/revoke while it runs: no later chunks should be admitted. A request may finish before you can click; that is not a revocation failure. Never replay an interrupted batch blindly.
7. **Prevalidation:** request a batch whose first action types `SHOULD-NOT-APPEAR`, and whose second text action contains `\r` or `\u0000` as an actual JSON control escape. The entire batch must fail before that marker appears. Separately confirm input to unshared A is not authorized.
8. Optionally copy a harmless clipboard sentinel yourself before testing, then paste manually afterward to confirm text delivery did not take clipboard ownership. Repeat the focus/restoration test with two windows of the same app, and with your usual physical layout/Caps Lock state.

Please report the exact Hyprland build, editor names/versions, whether they are native Wayland, successful versus missing text, bulk elapsed time, and whether the next physical keystroke went to A. Do not test this first in a terminal, password field, purchase form or valuable document.
