# Compatibility

**Experimental / alpha.** Private Hyprland APIs require an exact match between
running compositor and plugin headers. A successful build is not proof of runtime
isolation or application compatibility. Arch Linux is the only tested distribution.

| Target | Build coverage | Runtime status |
| --- | --- | --- |
| Hyprland 0.56.2, `efb50993780079460b0cbed1363e2166a2de1d9f` | Required CI, Arch archive 2026-08-06, both plugin variants | Limited disposable-session observations; see input limitations below |
| Current Arch snapshot (next packaged Hyprland, including 0.57 when available) | Weekly and PR rolling CI, non-blocking early warning | Unsupported until exact-version validation is completed |
| Other commits/distributions | Not validated | Unsupported; never bypass setup's mismatch checks |
| Linux ARM64 release executable | Cross-compiled | Not desktop-tested |

The rolling lane uses current Arch repositories, prints resolved package versions,
and intentionally moves with Hyprland's dependencies. The pinned lane remains the
release build gate. See [Contributing](../CONTRIBUTING.md#adding-a-hyprland-version)
for promotion criteria. Setup can build against installed headers; this table is
not an invitation to substitute a version hash or bypass private-hook checks.

## Input and application limits

- Native Wayland only; XWayland input is unsupported.
- Kitty binds the native seat only and uses guarded focus borrowing. Multi-seat
  clients can use the independent seat. Titles and application names never select
  authority or routing; live protocol resources do.
- GTK3 can submit native-seat popup serials during agent-seat grabs; these are
  refused, not replayed through another backend. Qt multi-seat behavior is not a
  guarantee of same-process menu independence.
- IME, clipboard, primary selection, DnD, touch/tablets, constraints and activation
  are not independently implemented. Applications can change desktop activation
  or swallow human input internally.
- Capture uses actual toplevel capture. Popup pixels are not guaranteed. No output
  crop is substituted for window isolation.
- Seat-owning plugin replacement requires a full Hyprland restart, not a config
  reload or hot unload. Save work first; setup never restarts Hyprland itself.

[Setup](SETUP.md) describes installation, [Input architecture](INDEPENDENT_SEAT.md)
describes routing, [Security](../SECURITY.md) defines the trust boundary, and
[Testing](TESTING.md) describes reproducible checks and missing evidence.
