# Permission contract and security status

This document describes the trust boundary and known limitations. The intended untrusted party is a client that has only the exposed MCP tools. The desktop user, broker, Quickshell console, compositor, plugins, and other processes with equivalent local authority are trusted.

Window metadata (titles, IDs, app, workspace, geometry) is intentionally available without a desktop grant, including while paused. In OAuth mode this still requires a valid connection access token. This can reveal sensitive document names. Pixels, input, recording and application launch retain their permission gates.

Local proactive sharing is exposed only through the UI socket/CLI, not MCP. A click binds a timed grant to one existing MCP connection and one snapshot-validated native window. Multiple clients require a recipient choice; no future-client or broadcast grants.

`setup` and `stop` are explicit local administration commands, never MCP tools. Setup may gracefully stop a verified same-user broker, replace the guard, and restart it in approve mode with no inherited grants. Kernel socket credentials, executable/module/argument checks, session checks and pidfds prevent accidental process-name/PID-file targeting; they are not protection against hostile same-UID software. Setup uses a temporary version-matched, read-only inspector plugin to discover actual guard paths. Service-managed brokers and configuration-loaded guards are refused rather than silently overriding their manager. No host service or autostart configuration is installed.

## Enforced paths

- The default MCP transport is a private Unix socket, reached through a stdio bridge. Optional Streamable HTTP can use the built-in OAuth provider; see [AUTH.md](AUTH.md). It exposes no approve/mode/shell/arbitrary-path tool.
- A separate private UI socket receives local decisions. Quickshell must maintain its connection/heartbeat; losing the final supervisor pauses the broker and clears grants.
- Grants belong to an MCP connection, have a specific capability/scope, and expire using the server's in-process clock. Disconnect revokes that connection's grants and stops its recordings.
- Window control adds a compositor lease bound to the live window **object and root surface**, not just its title or PID. The plugin checks time, visibility, session-lock state, geometry and pointer bounds at delivery. It checks that the seat accepted the target focus before sending events.
- Input is delivered directly to the authorized root surface on the compositor thread, not injected globally after an external focus check. Unavailable guard means no input.
- Observation uses `grim -T` with the actual foreign-toplevel identifier. It is not output capture cropped by a window rectangle. Opaque Kitty/Pinta capture was tested with another window and the permission panel visible on the same output.
- Recording is independent of input permission. Revocation/cancellation prevents subsequent capture iterations, checks authorization again before writing captured frames, and finalizes the local video.
- Ordinary input is a complete synchronous transaction: synthetic presses are released and the prior seat focus/device/modifiers restored before returning to the compositor event loop. Cleanup only releases into the transaction's original target. No held synthetic state spans requests; expiry/revocation blocks later transactions but cannot interrupt an already admitted bounded callback.
- Keyboard/mouse transactions do not invoke desktop activation or cursor warping. An explicitly requested `focus` action intentionally changes desktop activation. Held user input, grabs, constraints and drag-and-drop are conservatively refused rather than silently falling back. A restoration fault disables further guard input until reload.

## What this does not claim

- **Same-user isolation.** A same-UID process can access private runtime sockets; a shell controlled through a terminal can transitively gain that authority. A controlled application may also spawn programs, access files or the network, and use previously cached credentials. The UI explicitly warns for common terminal classes, but class detection is not a sandbox.
- **Semantic safety.** Permission to click is not permission to purchase, delete, send a message, or elevate privileges. The compositor cannot infer those effects. There is no secure sudo/password handover or privileged-operation broker yet.
- **Popup/subsurface correctness.** The current injection path targets the root toplevel only. It deliberately does not imply permission for a new transient toplevel. Native menus/popups and input methods require further work.
- **Compositor compromise resistance.** The guard runs inside Hyprland. Other plugins and compositor bugs can defeat it. The Quickshell border is useful feedback, not an unspoofable compositor-owned secure-attention surface.
- **Capture confidentiality for every renderer effect.** Transparent/blurred windows, protected content and all renderer paths need further auditing. Successful opaque-window tests do not establish these properties.
- **Retroactive revocation.** Previously delivered pixels or recorded bytes cannot be recalled. An already executed application operation cannot be undone by revoking its grant.
- **Full device arbitration or invisible background input.** Focus preservation is experimental, not a second seat. Clients receive temporary protocol focus transitions and keyboard keymap changes; same-application windows, focus-dependent UIs, cursor-shape requests, mixed layouts, physical input interleaving and lock transitions need live testing. Pointer geometry restoration is refused when the previous surface cannot be mapped. Long/timed drags, compositor drag-and-drop, durable hover and IME grabs are unsupported. Earlier live input evidence predates these transactions.
- **Resource-exhaustion hardening.** Basic per-request, action, peer, recording-count and duration limits exist. This is not yet a hardened multi-tenant daemon; disk retention/quota administration remains the owner's responsibility.

## Fail-closed behavior to preserve

Do not add a fallback from toplevel capture to desktop cropping, or from guard input to `wtype`/`ydotool`/virtual-pointer global injection. Do not expose the local UI command channel as an MCP tool. Do not persist grants across broker/compositor restarts. Do not treat a UI outline as proof that a backend check ran.

For testing, a separate local harness may simulate human approval. That authority must stay outside the MCP client and be clearly labelled in recordings and reports.

## Optional OAuth boundary

Built-in OAuth is opt-in and authenticates HTTP MCP connections only. There is no remote approval endpoint, no token passthrough and no MCP tool that changes OAuth decisions. Local OAuth revocation also terminates that authorization grant's MCP sessions and desktop grants/recordings. Cookie-bound authorization continuation, exact redirect checks, S256 PKCE, resource validation, token-family refresh rotation/replay revocation, expiry and session ownership are covered by tests. Registered client names are untrusted labels; the UI displays callback addresses and the loopback impersonation caveat.

Client registrations persist privately with hashed secrets; tokens/consent are volatile. The provider does not fetch arbitrary client metadata URLs (CIMD is unsupported). Connection/registration/request limits are not a substitute for a hostile-network audit or deployment-level rate limiting. No public-internet deployment was made for the lab test.
