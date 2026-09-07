# Computer Use

Standalone Hyprland computer-use MCP server with a Quickshell permission console. Local experimental project; no term-llm integration or live-desktop deployment is implied.

## Working rules
- Work inline: no background agent jobs.
- Default deny / approve mode. Only the local UI may grant, revoke, pause or select YOLO.
- Keep MCP and local UI transports separate. No arbitrary shell, path, clipboard or policy-changing MCP tool.
- Window IDs are instance-bound; no title/app-name based authority. Capture must use actual toplevel capture, never a desktop crop masquerading as window isolation.
- Window-scoped input requires the companion compositor plugin. Never silently fall back to global injection.
- Same-UID processes and compositor plugins are trusted. A controlled terminal can transitively grant shell authority; this is not an OS sandbox.
- Test the exact lab version and label unsupported cases and guarantees honestly.
- No commits/push/services on the host unless separately requested.
