# Security policy

## Reporting a vulnerability

Please do **not** open a public issue for a suspected vulnerability. Use [GitHub private vulnerability reporting](https://github.com/samsaffron/hyprland-computer-use/security/advisories/new) to send the affected version, impact, reproduction steps, and any suggested mitigation to the maintainers. If private reporting is unavailable, email [sam.saffron@gmail.com](mailto:sam.saffron@gmail.com) before disclosing details publicly. The same explicit contacts and policy URL are published in [`security.txt`](security.txt).

Reports should avoid real desktop contents, access tokens, private keys, and other sensitive artifacts. No bounty or fixed acknowledgement/remediation timeline is currently promised.

## Permission contract and security status

This document describes the trust boundary and known limitations. The intended untrusted party is a client that has only the exposed MCP tools. The desktop user, broker, Quickshell console, compositor, plugins, and other processes with equivalent local authority are trusted.

> **Same-user self-approval:** an agent with a shell can approve its own requests. `ui.sock` has no Quickshell peer authentication; any same-UID process can send approve, pause or mode commands. Human-only approval is a convention for MCP-only clients, not an enforceable boundary against desktop-user software. Do not grant same-user shell access if you rely on this boundary.

Window metadata (titles, IDs, app, workspace, geometry) is intentionally available without a desktop grant, including while paused. In OAuth mode this still requires a valid connection access token. This can reveal sensitive document names. Pixels, input, recording and application launch retain their permission gates.

Local proactive sharing is exposed only through the UI socket/CLI, not MCP. A click binds a timed grant to one existing MCP connection and one snapshot-validated native window. Multiple clients require a recipient choice; no future-client or broadcast grants.

`setup` and `stop` are explicit local administration commands, never MCP tools. Setup may gracefully stop a verified same-user broker, replace the guard, and restart it in approve mode with no inherited grants. Kernel socket credentials, executable/module/argument checks, session checks and pidfds prevent accidental process-name/PID-file targeting; they are not protection against hostile same-UID software. Setup uses a temporary version-matched, read-only inspector plugin to discover actual guard paths. Service-managed brokers and configuration-loaded guards are refused rather than silently overriding their manager. No host service or autostart configuration is installed.

## Revocation uncertainty

Broker authority is removed immediately. Failed compositor revoke/clear calls are audited and retried with bounded calls. `computer_status.revocation_unconfirmed` and the console badge remain set until confirmation; new grants and input are blocked while uncertain. Compositor lease expiry is the backstop (ordinary grants up to one hour, temporary YOLO leases up to five minutes). Expiry does not itself clear the warning: the broker requires a successful cleanup reply. Already admitted bounded callbacks cannot be recalled.

## Automatic scoped input

`setup` prefers the independent-seat build, with guarded focus borrowing when its build or required hook is unavailable. This is local backend selection, not an MCP policy tool, and does not change grants. At execution time, the guard checks whether the live root client has both agent-seat keyboard and pointer resources **before delivery**. If not, it uses the existing synchronous focus-borrowing transaction, including idle-input, grab and restoration checks. It never retries an error or partially delivered action through another backend. Invalid selectors, permission failures, busy input, constraints, IME grabs and invalid popup serials remain refusals.

The broker creates native-seat virtual devices for the guarded fallback; its device protocol has no event-injection requests. It identifies the native seat by its `Hyprland` protocol name, not registry ordering, and verifies the compositor peer before creating devices. The guard alone performs scoped delivery. A per-load instance ID binds v3 requests/replies; compositor disconnection stops the broker. Revocation/clear/expiry invalidate agent focus and serial records. A bounded single-use input serial, matching lease/root, and live parent-tree membership gate agent popup grabs; invalid requests are dismissed without modifying native grabs.

The green grant marker reports **Seat** or **Fallback** from live compositor capability data (or **Unknown** if unavailable). This is a backend indicator, not proof of application completion. Fallback exposes temporary native protocol focus/keymap transitions and cannot promise independent simultaneous human input.

This is **not complete compositor or application multi-seat isolation**. GTK3 popup requests with default-seat serials are refused; same-process menus can swallow human input; IME, clipboard, DnD, constraints and activation are not independently implemented. App-created windows may still change desktop activation. The hook depends on an exact-version private compositor API, and capture is unchanged. See [INDEPENDENT_SEAT.md](docs/INDEPENDENT_SEAT.md) for tested cases and limitations.

Hot unload with connected clients is unsupported. Setup compares the requested guard with the active plugin: an identical build is left running, while a differing seat-owning build produces explicit full-session restart instructions before the broker is stopped. A config reload is not sufficient. Old setup executables do not enforce this restriction. No host deployment or policy change happens merely by building the variant.

## Enforced paths

- The default MCP transport is a private Unix socket, reached through a stdio bridge. Optional Streamable HTTP can use the built-in OAuth provider; see [AUTH.md](docs/AUTH.md). It exposes no approve/mode/shell/arbitrary-path tool.
- A separate private UI socket receives local decisions. Quickshell must maintain its connection/heartbeat; losing the final supervisor pauses the broker and clears grants.
- Grants belong to an MCP connection, have a specific capability/scope, and expire using the server's in-process clock. Disconnect revokes that connection's grants and stops its recordings.
- Window control adds a compositor lease bound to the live window **object and root surface**, not just its title or PID. Surface selectors are additionally bound to that root and a live surface object; current mapped tree membership is checked at each delivery. The plugin checks time, visibility, session-lock state, geometry and pointer bounds at delivery. The fallback checks native seat focus acceptance; the independent path requires the target client's agent-seat resources before delivery.
- Input is delivered directly to a surface in the authorized window tree on the compositor thread, not injected globally after an external focus check. Unavailable guard means no input.
- Observation uses `grim -T` with the actual foreign-toplevel identifier. It is not output capture cropped by a window rectangle. Opaque Kitty/Pinta capture was tested with another window and the permission panel visible on the same output. Screenshots and recording frames now require guard status reporting an active, unlocked session before and after capture; missing/unknown status fails closed. These are sampled checks, **not an atomic lock/capture fence**: a lock/unlock between samples or lock after the last check is not excluded. Live lock-transition confidentiality remains unverified.
- Recording is independent of input permission. Revocation/cancellation prevents subsequent capture iterations, checks authorization again before writing captured frames, and finalizes the local video.
- In the **focus-borrowing fallback**, ordinary input is a complete synchronous transaction: synthetic presses are released and the prior seat focus/device/modifiers restored before returning to the compositor event loop. Cleanup only releases into the transaction's original target. No held synthetic state spans requests; expiry/revocation blocks later transactions but cannot interrupt an already admitted bounded callback.
- Unicode text is delivered in bounded chunks of at most 48 scalars. Text-bearing keymaps are sealed and sent only to the target client's keyboard resources, never installed as a global seat/device map. The prior map is explicitly restored, including same-device transactions. Counts acknowledge protocol delivery, not application insertion; an already admitted chunk cannot be recalled. No clipboard access is used. Same-client application behavior remains trusted.
- Keyboard/mouse transactions do not invoke desktop activation or cursor warping. An explicitly requested `focus` action intentionally changes desktop activation. In the focus-borrowing fallback, held user input, grabs, constraints and drag-and-drop are conservatively refused rather than silently falling back. A restoration fault disables further guard input until reload.

## Pointer extensions and cropped observation

Modifiers and multiclick/waypoint gestures remain complete bounded compositor
transactions on one authorized surface. The guard validates modifiers, counts,
units and every planned path point before delivery. Cleanup attempts both button
release and modifier release on exceptions; uncertain cleanup faults input.
No held mouse/key state spans broker requests. New pointer features require a
versioned guard capability before any batch input, including earlier key/text
actions. Unsupported/old guards never silently degrade these gestures.

Crop/zoom reads the actual approved toplevel, not the desktop. It samples a
window-local logical region from the captured buffer with HiDPI-aware mapping.
Final permission/geometry/workspace checks still gate pixel release. Full-buffer
decoding for crop/resampling is bounded to 8192 pixels per edge and 16 Mi pixels;
encoded transport is bounded to 16 MiB. Region boundaries are not an additional
within-window security boundary, and sampled lock checks remain non-atomic.
See [API details and limitations](docs/POINTER_CAPTURE.md).

## What this does not claim

- **Same-user isolation.** A same-UID process can access private runtime sockets; a shell controlled through a terminal can transitively gain that authority. A controlled application may also spawn programs, access files or the network, and use previously cached credentials. The UI explicitly warns for common terminal classes, but class detection is not a sandbox.
- **Semantic safety.** Permission to click is not permission to purchase, delete, send a message, or elevate privileges. The compositor cannot infer those effects. There is no secure sudo/password handover or privileged-operation broker yet.
- **Universal popup/subsurface correctness.** Bounded surface-tree routing is implemented with explicit surface selectors, stacking/input-region checks and no cross-surface drag fallback. It has native unit/header compilation coverage, not live toolkit validation. Active seat grabs (including grabbed menus) remain refused in the default backend. New transient toplevels require separate grants; PID/title relationships confer none. Popup pixel inclusion in toplevel capture remains unverified. See [SURFACES.md](docs/SURFACES.md).
- **Compositor compromise resistance.** The guard runs inside Hyprland. Other plugins and compositor bugs can defeat it. The Quickshell border is useful feedback, not an unspoofable compositor-owned secure-attention surface.
- **Capture confidentiality for every renderer effect.** Transparent/blurred windows, protected content and all renderer paths need further auditing. Successful opaque-window tests do not establish these properties.
- **Retroactive revocation.** Previously delivered pixels or recorded bytes cannot be recalled. An already executed application operation cannot be undone by revoking its grant.
- **Full device arbitration or invisible background input.** Focus-borrowing preservation is not a second seat and has limited live coverage. Clients receive temporary protocol focus transitions and keyboard keymap changes; same-application windows, focus-dependent UIs, cursor-shape requests, mixed layouts, physical input interleaving and lock transitions need live testing. Pointer geometry restoration is refused when the previous surface cannot be mapped. Long/timed drags, compositor drag-and-drop, durable hover and IME grabs are unsupported. Earlier live input evidence predates these transactions.
- **Resource-exhaustion hardening.** Basic per-request, action, peer, recording-count and duration limits exist. This is not yet a hardened multi-tenant daemon; disk retention/quota administration remains the owner's responsibility.

## Fail-closed behavior to preserve

Do not add a fallback from toplevel capture to desktop cropping, or from guard input to `wtype`/`ydotool`/virtual-pointer global injection. Do not expose the local UI command channel as an MCP tool. Do not persist grants across broker/compositor restarts. Do not treat a UI outline as proof that a backend check ran.

For testing, a separate local harness may simulate human approval. That authority must stay outside the MCP client and be clearly labelled in recordings and reports.

## Optional OAuth boundary

Built-in OAuth is opt-in and authenticates HTTP MCP connections only. There is no remote approval endpoint, no token passthrough and no MCP tool that changes OAuth decisions. Local OAuth revocation also terminates that authorization grant's MCP sessions and desktop grants/recordings. Cookie-bound authorization continuation, exact redirect checks, S256 PKCE, resource validation, token-family refresh rotation/replay revocation, expiry and session ownership are covered by tests. Registered client names are untrusted labels; the UI displays callback addresses and the loopback impersonation caveat.

Client registrations persist privately with hashed secrets; the existing registry is loaded only when it is an owner-only regular file and every entry revalidates. Tokens/consent are volatile. The provider does not fetch arbitrary client metadata URLs (CIMD is unsupported). Bounded per-source-IP endpoint limits use the direct TCP peer address and deliberately ignore forwarding headers; deployments behind a reverse proxy therefore share one budget unless the proxy applies its own stricter limits. These controls and object caps are not a substitute for a hostile-network audit. No public-internet deployment was made for the lab test.
