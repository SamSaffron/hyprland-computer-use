# Changelog

All notable user-visible changes are recorded here. This project follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Security

- Fail compositor-plugin initialization safely when `XDG_RUNTIME_DIR` is unavailable.
- Protect broker and guard Unix sockets with process-lifetime lock files instead of reclaiming a socket after a timeout.
- Bound compositor authorization and cleanup calls without holding the broker state mutex.
- Add OAuth endpoint rate limits, persisted-client-registry validation, and explicit HTTP MCP overload responses.
- Sign release checksum manifests with keyless Sigstore/cosign identities and verify them during installation when cosign is available.
- Require Go 1.26.6 or newer so builds include current standard-library security fixes.

### Changed

- Record video through private temporary files, publish it atomically only after successful finalization, and clean up failed output.
- Display the local window picker on every monitor.

## [0.1.0] - 2026-09-09

First public release:

- Default-deny MCP broker with a separate local Quickshell permission console.
- Instance-bound native Wayland window capture and compositor-plugin scoped input.
- Timed observation, control, recording, and allowlisted application-launch permissions.
- Per-user release installer and exact-Hyprland-header local plugin build.

[Unreleased]: https://github.com/samsaffron/hyprland-computer-use/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/samsaffron/hyprland-computer-use/releases/tag/v0.1.0
