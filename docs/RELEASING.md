# Releasing

[Quick start](../README.md) · [Setup](SETUP.md) · [Development](DEVELOPMENT.md)

This uses a **GoReleaser + tagged GitHub Actions release + per-user installer** pipeline. The initial public release verified archive and checksum publication; follow the same process for subsequent releases.

## What gets shipped

- Linux `amd64` and `arm64` archives, named `hyprland-computer-use_<version>_linux_<arch>.tar.gz`.
- One `CGO_ENABLED=0` executable in each archive, with version, commit, and build date embedded.
- README, guides, security/license notices, and license-bearing Wayland XML.
- `checksums.txt` containing archive SHA-256 hashes.

The executable embeds native source and Quickshell QML. **Do not ship a precompiled guard:** the target's `setup` compiles it against that desktop's exact Hyprland headers. Go is not required on the target, but the system compiler and runtime dependencies still are.

These are Linux builds, not a claim of broad compositor compatibility. The lab version is Hyprland 0.56.2; ARM64 is cross-compiled, not desktop-tested. See [testing status](TESTING.md).

## Check a release locally

With GoReleaser v2 installed:

```sh
make test
goreleaser check
goreleaser release --snapshot --clean --skip=publish
```

Snapshot archives go into ignored `dist/`. Nothing is tagged, pushed, or published by these commands. CI runs the non-publishing snapshot check too.

## Publish intentionally

From a clean, committed checkout on `main`, with the intended changes already pushed:

```sh
scripts/release.sh v0.1.0 --wait
```

**This command creates and pushes an annotated tag.** It is a maintainer action, not part of installation or setup. `--wait` requires the GitHub CLI and follows the release workflow to completion. Later releases can use `scripts/release.sh --auto --wait` to bump the latest tag's patch version.

The tag triggers [release.yml](../.github/workflows/release.yml): tests run first, then GoReleaser builds both architectures and uploads archives/checksums to the GitHub release. It uses the workflow's `GITHUB_TOKEN` with `contents: write`; no Homebrew tap or additional deployment key is needed. Prerelease tags are marked automatically by GoReleaser; the convenience script accepts stable `vMAJOR.MINOR.PATCH` tags only.

After publication, verify both installer modes from a clean environment:

```sh
sh install.sh
sh install.sh --version v0.1.0
```

Check the installed version, run `setup`, and start `serve` inside a disposable supported Hyprland session. A compositor-free container can validate `setup --build-only`, but cannot substitute for the live plugin and broker check.

## Installer behavior

[`install.sh`](../install.sh) follows GitHub's latest-release redirect without the API, or takes an explicit `--version`. It:

1. Rejects non-Linux and unsupported architectures.
2. Downloads the matching archive and checksum manifest over HTTPS.
3. Requires exactly one matching checksum and verifies SHA-256 before extraction.
4. Extracts only the executable, then atomically replaces it in `~/.local/bin` (or the selected install directory), including when the old binary is running.
5. Prints the next `setup` command; it does **not** run it.

Checksums detect corruption against the published manifest; they are not independent release signatures. The installer trusts the repository's HTTPS release assets. It never installs system packages, uses sudo, edits compositor configuration, or starts/stops a broker or service.

Installer and release-script tests use local fixture archives and mocked curl/git/gh commands. They do not access the network, create real tags, or publish releases.
