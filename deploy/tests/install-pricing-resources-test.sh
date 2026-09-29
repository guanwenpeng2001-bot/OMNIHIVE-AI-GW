#!/bin/bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
TEST_DIR=$(mktemp -d)
trap 'rm -rf "$TEST_DIR"' EXIT

# Load the real installer without running its entry point.
source <(sed '$d' "$ROOT_DIR/deploy/install.sh")
LATEST_VERSION=v0.0.0
OS=linux
ARCH=amd64
print_info() { :; }
print_success() { :; }
print_warning() { :; }

# Serve a local archive through the installer's download path, without network.
curl() {
    if [[ "$2" == */checksums.txt ]]; then
        return 1
    fi
    cp "$TEST_DIR/release.tar.gz" "$4"
}

mkdir -p "$TEST_DIR/archive/resources/model-pricing"
printf '#!/bin/sh\nexit 0\n' > "$TEST_DIR/archive/sub2api"
cp "$ROOT_DIR/backend/resources/model-pricing/"*.json "$TEST_DIR/archive/resources/model-pricing/"
tar -czf "$TEST_DIR/release.tar.gz" -C "$TEST_DIR/archive" .
INSTALL_DIR="$TEST_DIR/installed"
download_and_extract
cmp "$ROOT_DIR/backend/resources/model-pricing/cache-base-overrides.json" "$INSTALL_DIR/resources/model-pricing/cache-base-overrides.json"
cmp "$ROOT_DIR/backend/resources/model-pricing/model_prices_and_context_window.json" "$INSTALL_DIR/resources/model-pricing/model_prices_and_context_window.json"
test -x "$INSTALL_DIR/sub2api"
rm -rf "$TEMP_DIR"

# Installing an older archive without resources remains supported.
tar -czf "$TEST_DIR/release.tar.gz" -C "$TEST_DIR/archive" sub2api
INSTALL_DIR="$TEST_DIR/legacy-installed"
download_and_extract
test -x "$INSTALL_DIR/sub2api"
test ! -e "$INSTALL_DIR/resources"
rm -rf "$TEMP_DIR"
trap 'rm -rf "$TEST_DIR"' EXIT
echo "PASS: native installer copies pricing resources and accepts legacy archives"
