#!/bin/bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
TEST_DIR=$(mktemp -d)
trap 'rm -rf "$TEST_DIR"' EXIT

# Load the real installer without running its entry point.
source <(sed '$d' "$ROOT_DIR/deploy/install.sh")
LATEST_VERSION=v0.2.5
OS=linux
ARCH=amd64
if ! command -v sha256sum >/dev/null 2>&1; then
    sha256sum() { shasum -a 256 "$@"; }
fi
print_info() { :; }
print_success() { :; }
print_warning() { :; }
print_error() { :; }

# Serve a local archive through the installer's download path, without network.
curl() {
    case "$2" in
        */checksums.txt)
            {
                printf '%s  %s\n' "$(sha256sum "$TEST_DIR/release.tar.gz" | awk '{print $1}')" "$archive_name_expected"
                printf '%s  %s\n' "$(sha256sum "$TEST_DIR/omnihive-release-manifest.json" | awk '{print $1}')" "omnihive-release-manifest.json"
            } > "$4"
            ;;
        */omnihive-release-manifest.json) cp "$TEST_DIR/omnihive-release-manifest.json" "$4" ;;
        */"$archive_name_expected") cp "$TEST_DIR/release.tar.gz" "$4" ;;
        *) return 1 ;;
    esac
}

printf '{"product":"omnihive","version":"%s","minimum_rollback_version":"0.2.5"}\n' "$LATEST_VERSION" > "$TEST_DIR/omnihive-release-manifest.json"

archive_name_expected=omnihive_0.2.5_linux_amd64.tar.gz
mkdir -p "$TEST_DIR/archive/resources/model-pricing"
printf '#!/bin/sh\nexit 0\n' > "$TEST_DIR/archive/omnihive"
cp "$ROOT_DIR/backend/resources/model-pricing/"*.json "$TEST_DIR/archive/resources/model-pricing/"
tar -czf "$TEST_DIR/release.tar.gz" -C "$TEST_DIR/archive" .
INSTALL_DIR="$TEST_DIR/installed"
download_and_extract
for pricing_file in "$ROOT_DIR/backend/resources/model-pricing/"*.json; do
    cmp "$pricing_file" "$INSTALL_DIR/resources/model-pricing/$(basename "$pricing_file")"
done
test -x "$INSTALL_DIR/omnihive"
rm -rf "$TEMP_DIR"

# Upgrading a legacy host identity with a verified OmniHive archive without resources creates the compatibility symlink.
rm -rf "$TEST_DIR/archive/resources"
tar -czf "$TEST_DIR/release.tar.gz" -C "$TEST_DIR/archive" .
LEGACY_INSTALL_DIR="$TEST_DIR/legacy-installed"
mkdir -p "$LEGACY_INSTALL_DIR"
cp "$TEST_DIR/archive/omnihive" "$LEGACY_INSTALL_DIR/sub2api"
INSTALL_DIR="$TEST_DIR/current-install"
select_installed_identity
[[ "$SERVICE_NAME" == sub2api ]]
download_and_extract
[[ -L "$INSTALL_DIR/sub2api" && "$INSTALL_DIR/sub2api" -ef "$INSTALL_DIR/omnihive" ]]
test -x "$INSTALL_DIR/omnihive"
test ! -e "$INSTALL_DIR/resources"
rm -rf "$TEMP_DIR"

# Legacy product archive names must be rejected fail-closed.
archive_name_expected=sub2api_0.2.5_linux_amd64.tar.gz
if download_and_extract; then
    echo "FAIL: legacy product archive should be rejected" >&2
    exit 1
fi
rm -rf "$TEMP_DIR"
trap 'rm -rf "$TEST_DIR"' EXIT
echo "PASS: native installer copies pricing resources, preserves legacy host symlink, and rejects legacy archives"
