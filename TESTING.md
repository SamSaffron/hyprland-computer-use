# Prototype verification — 7 September 2026

This is evidence from one disposable configuration, not a general security audit.

## Configuration

- Arch Linux; Hyprland **0.56.2**, build `efb50993780079460b0cbed1363e2166a2de1d9f`.
- Nested GPU-backed compositor with a **1400 × 1200** headless output; NVIDIA RTX 4090, driver **610.57.04**.
- Quickshell **0.3.1**, native Wayland Kitty and Pinta.
- The lab's Aquamarine **0.15.0** build has local nested-backend compatibility patches (xdg_wm_base version clamp and backend-supported buffer modifiers). This was not a stock physical-monitor deployment.
- Persistent helper supplies a US virtual keyboard and virtual pointer. Guard built against the exact running compositor's headers.

## Automated and live checks

`go test -race ./...`: **17 tests passed**. `go vet ./...`: passed. Native plugin/helper compilation: passed.

The opt-in `scripts/live-test.py` passed **15 checks** against the actual MCP transport and compositor:

1. Window metadata withheld until a separate local observation grant.
2. MCP cannot directly invoke approval/mode-changing tools.
3. Native toplevel capture matches the target dimensions.
4. Approved keyboard actions reach Kitty.
5. One window's control grant does not authorize another window.
6. Stale geometry rejected.
7. Negative/out-of-window coordinates rejected by the broker.
8. Compositor independently rejects out-of-window coordinates.
9. Expiry enforced by broker and compositor.
10. YOLO auto-allows exposed operations.
11. Pause overrides YOLO.
12. Pointer click/drag/scroll complete and change Pinta pixels.
13. Window recording finalizes on mode revocation; ffprobe accepts the MP4.
14. Client disconnect revokes its grants.
15. Destroying a window invalidates its compositor lease.

Pixel evidence was also visually inspected: a real brush stroke appeared in Pinta's canvas/history, and Kitty executed the test commands. Protocol success alone was not treated as proof of pointer delivery. The stricter seat-focus check caught a missing-pointer-capability bug, fixed by the persistent helper and explicit virtual-device selection.

## Demonstration

A **56-second desktop recording** exercises workspace observation approval, a separate five-minute Kitty control grant, countdown/outline, real MCP keyboard input, revocation and a blocked retry, two-click local YOLO selection, drawing a red heart onto an existing image in Pinta, window recording, real tray activation, and Pause overriding YOLO. The lab is left in Approve mode, paused.

The old global pointer harness is used **only to simulate the human's permission-console/tray clicks**. Agent operations use a persistent real stdio MCP client. Calls are scripted, not represented as autonomous model reasoning. The existing cat artwork was not created by this MCP demonstration. The video labels these boundaries explicitly.

## Not verified

Physical-desktop device arbitration, mixed DPI/multiple monitors/seats, all lock-screen transitions, popup/subsurface input, XWayland input, transparent/protected capture, and Chromium launch inside this container are not covered by these results. Same-UID processes and controlled terminals retain their local authority. See [SECURITY.md](SECURITY.md).
