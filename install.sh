#!/bin/sh
# Per-user GitHub release installer.
# Only installs the executable; setup and desktop/process changes stay explicit.
set -eu

REPO="samsaffron/hyprland-computer-use"
BINARY="hyprland-computer-use"
INSTALL_DIR=${COMPUTER_USE_INSTALL_DIR:-$HOME/.local/bin}
VERSION=""
TMP=""
STAGED=""
REQUIRE_SIGNATURE=0

fail() { printf 'Error: %s\n' "$1" >&2; exit 1; }
cleanup() {
    [ -z "$STAGED" ] || rm -f -- "$STAGED"
    [ -z "$TMP" ] || rm -rf -- "$TMP"
    return 0
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM

usage() {
    cat <<'USAGE'
Usage: install.sh [--version <tag>] [--install-dir <path>] [--require-signature]

Install the latest Linux release, or select a tag such as v0.1.0.
Default destination: ~/.local/bin (override with COMPUTER_USE_INSTALL_DIR).
Use --require-signature to refuse installation unless cosign verifies provenance.
No sudo, packages, compositor changes, or services are invoked.
USAGE
}

while [ "$#" -gt 0 ]; do
    case "$1" in
        --version)
            [ "$#" -ge 2 ] && [ -n "$2" ] || fail "--version requires a tag"
            VERSION=$2; shift 2 ;;
        --install-dir)
            [ "$#" -ge 2 ] && [ -n "$2" ] || fail "--install-dir requires a path"
            INSTALL_DIR=$2; shift 2 ;;
        --require-signature) REQUIRE_SIGNATURE=1; shift ;;
        -h|--help) usage; exit 0 ;;
        *) fail "Unknown option: $1" ;;
    esac
done

case "$INSTALL_DIR" in
    \~) INSTALL_DIR=$HOME ;;
    \~/*) INSTALL_DIR="$HOME/${INSTALL_DIR#\~/}" ;;
esac
case "$(uname -s)" in
    Linux) OS=linux ;;
    *) fail "Only Linux with Hyprland is supported" ;;
esac
case "$(uname -m)" in
    x86_64|amd64) ARCH=amd64 ;;
    aarch64|arm64) ARCH=arm64 ;;
    *) fail "Only amd64 and arm64 release binaries are available" ;;
esac
for tool in curl tar sha256sum awk grep mktemp; do
    command -v "$tool" >/dev/null 2>&1 || fail "Required command not found: $tool"
done

if [ "$REQUIRE_SIGNATURE" = 1 ]; then
    command -v cosign >/dev/null 2>&1 || fail "--require-signature requires cosign; nothing installed"
fi

if [ -z "$VERSION" ]; then
    # Follow GitHub's release redirect without consuming API rate limits.
    RELEASE_URL=$(curl -fsSL -o /dev/null -w '%{url_effective}' \
        "https://github.com/$REPO/releases/latest") || fail "No release found; check GitHub releases or build from source"
    NORMALIZED_URL=$(printf '%s' "$RELEASE_URL" | tr '[:upper:]' '[:lower:]')
    case "$NORMALIZED_URL" in
        "https://github.com/$REPO/releases/tag/"*) VERSION=${RELEASE_URL##*/} ;;
        *) fail "Unable to determine the latest release tag" ;;
    esac
fi
case "$VERSION" in v*) ;; *) VERSION="v$VERSION" ;; esac
printf '%s\n' "$VERSION" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z][0-9A-Za-z.-]*)?$' || fail "Invalid release tag"

ASSET="${BINARY}_${VERSION#v}_${OS}_${ARCH}.tar.gz"
BASE="https://github.com/$REPO/releases/download/$VERSION"
TMP=$(mktemp -d)
printf 'Downloading %s\n' "$ASSET"
curl -fsSL -o "$TMP/$ASSET" "$BASE/$ASSET" || fail "Release download failed"
curl -fsSL -o "$TMP/checksums.txt" "$BASE/checksums.txt" || fail "Checksum download failed"
if command -v cosign >/dev/null 2>&1; then
    curl -fsSL -o "$TMP/checksums.txt.sig" "$BASE/checksums.txt.sig" || fail "Signature download failed"
    curl -fsSL -o "$TMP/checksums.txt.pem" "$BASE/checksums.txt.pem" || fail "Signing certificate download failed"
    IDENTITY="https://github.com/samsaffron/hyprland-computer-use/.github/workflows/release.yml@refs/tags/$VERSION"
    # Releases use detached signatures, not cosign 3's default bundle format.
    cosign verify-blob \
        --new-bundle-format=false \
        --certificate "$TMP/checksums.txt.pem" \
        --signature "$TMP/checksums.txt.sig" \
        --certificate-identity "$IDENTITY" \
        --certificate-oidc-issuer https://token.actions.githubusercontent.com \
        "$TMP/checksums.txt" >/dev/null \
        || fail "Release signature verification failed; nothing installed"
    printf 'Verified checksums.txt with Sigstore/cosign\n'
else
    printf '%s\n' 'Warning: cosign not found; verifying checksum only, not release provenance.' >&2
fi
EXPECTED=$(awk -v name="$ASSET" '$2 == name { count++; sum=$1 } END { if (count != 1) exit 1; print sum }' "$TMP/checksums.txt") || fail "Missing or duplicate archive checksum"
printf '%s\n' "$EXPECTED" | grep -Eq '^[0-9a-f]{64}$' || fail "Invalid archive checksum"
ACTUAL=$(sha256sum "$TMP/$ASSET" | awk '{print $1}')
[ "$ACTUAL" = "$EXPECTED" ] || fail "Archive checksum mismatch; nothing installed"

# Extract only the binary to a chosen file, never arbitrary archive paths.
tar -xzOf "$TMP/$ASSET" "$BINARY" > "$TMP/binary" || fail "Binary missing from release archive"
[ -s "$TMP/binary" ] || fail "Empty release binary"
mkdir -p -- "$INSTALL_DIR"
# Atomic replacement also works while the old executable is running.
STAGED=$(mktemp "$INSTALL_DIR/.${BINARY}.XXXXXX")
cat "$TMP/binary" > "$STAGED"
chmod 755 "$STAGED"
mv -fT -- "$STAGED" "$INSTALL_DIR/$BINARY"
STAGED=""
printf 'Installed %s\n' "$INSTALL_DIR/$BINARY"
# Print a literal $PATH for the user to paste into their shell.
# shellcheck disable=SC2016
case ":$PATH:" in
    *:"$INSTALL_DIR":*) ;;
    *) printf 'Add to your shell PATH: export PATH="%s:$PATH"\n' "$INSTALL_DIR" ;;
esac
printf 'Next, inside Hyprland: %s setup\n' "$BINARY"
