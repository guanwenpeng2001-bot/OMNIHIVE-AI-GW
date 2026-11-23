#!/usr/bin/env bash
set -euo pipefail

script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
scratch=$(mktemp -d)
trap 'rm -rf -- "$scratch"' EXIT

# Load the production installer without executing its entry point.
source <(sed '$d' "$script_dir/install.sh")
if [[ -z "${BE8_RELEASE_CHECKER:-}" ]]; then
    BE8_RELEASE_CHECKER="$scratch/omnihivectl"
    (cd "$script_dir/../backend" && go build -o "$BE8_RELEASE_CHECKER" ./cmd/omnihivectl/refactor/standalone)
fi
export BE8_RELEASE_CHECKER
OS=linux
ARCH=amd64
if ! command -v sha256sum >/dev/null 2>&1; then
    sha256sum() { shasum -a 256 "$@"; }
fi

curl() {
    local name="${2##*/}"
    printf '%s\n' "$name" >> "$case_dir/downloads"
    if [ ! -f "$release/$name" ]; then
        return 22
    fi
    cp "$release/$name" "$4"
}

write_checksums() {
    (cd "$release" && sha256sum "$asset_name" "$manifest_name" > checksums.txt)
}

run_case() {
    local test_case="$1" expected="${2:-}"
    local case_dir="$scratch/$test_case" install_log
    (
        release="$case_dir/release"
        INSTALL_DIR="$case_dir/installed"
        LATEST_VERSION=v0.2.5
        manifest_name=omnihive-release-manifest.json
        mkdir -p "$release" "$INSTALL_DIR" "$case_dir/archive/resources/model-pricing"
        printf 'installed binary\n' > "$INSTALL_DIR/omnihive"
        printf 'new binary\n' > "$case_dir/archive/omnihive"
        # Shell fixtures have no DB. Exercise the real Go manifest policy with
        # an explicit fresh flag; database behavior is covered by Go tests.
        cat > "$case_dir/archive/omnihivectl" <<'CHECKER'
#!/bin/bash
if [[ "${3:-}" == --help ]]; then exec "$BE8_RELEASE_CHECKER" "$@"; fi
if [[ "${BE8_TEST_DENY:-}" == yes ]]; then echo 'database rollback floor is unavailable' >&2; exit 1; fi
exec "$BE8_RELEASE_CHECKER" "$@" --fresh
CHECKER
        chmod +x "$case_dir/archive/omnihivectl"
        printf '{}\n' > "$case_dir/archive/resources/model-pricing/example.json"

        if [ "$test_case" = below_schema_floor ]; then
            LATEST_VERSION=v0.2.4
        fi
        asset_name="omnihive_${LATEST_VERSION#v}_linux_amd64.tar.gz"
        tar -czf "$release/$asset_name" -C "$case_dir/archive" .
        printf '{"product":"omnihive","version":"%s","minimum_rollback_version":"0.2.5"}\n' "$LATEST_VERSION" > "$release/$manifest_name"
        write_checksums

        case "$test_case" in
            installed_checker)
                cp "$case_dir/archive/omnihivectl" "$INSTALL_DIR/omnihivectl"
                ;;
            installed_db_unavailable)
                cp "$case_dir/archive/omnihivectl" "$INSTALL_DIR/omnihivectl"
                export BE8_TEST_DENY=yes
                ;;
            missing_checker)
                rm "$case_dir/archive/omnihivectl"
                tar -czf "$release/$asset_name" -C "$case_dir/archive" .
                write_checksums
                ;;
            db_unavailable)
                export BE8_TEST_DENY=yes
                ;;
            wrong_asset)
                wrong_asset="${LEGACY_SERVICE_NAME}_${LATEST_VERSION#v}_linux_amd64.tar.gz"
                mv "$release/$asset_name" "$release/$wrong_asset"
                asset_name="$wrong_asset"
                write_checksums
                ;;
            missing_checksums)
                rm "$release/checksums.txt"
                ;;
            missing_entry)
                (cd "$release" && sha256sum "$manifest_name" > checksums.txt)
                ;;
            checksum_command_failed)
                sha256sum() { return 1; }
                ;;
            tampered_archive)
                printf 'modified archive\n' >> "$release/$asset_name"
                ;;
            missing_manifest)
                rm "$release/$manifest_name"
                ;;
            tampered_manifest)
                printf 'modified manifest\n' >> "$release/$manifest_name"
                ;;
            wrong_product)
                printf '{"product":"%s","version":"%s","minimum_rollback_version":"0.2.5"}\n' \
                    "$LEGACY_SERVICE_NAME" "$LATEST_VERSION" > "$release/$manifest_name"
                write_checksums
                ;;
            wrong_binary)
                mv "$case_dir/archive/omnihive" "$case_dir/archive/$LEGACY_SERVICE_NAME"
                tar -czf "$release/$asset_name" -C "$case_dir/archive" .
                write_checksums
                ;;
            below_manifest_floor)
                printf '{"product":"omnihive","version":"%s","minimum_rollback_version":"0.2.6"}\n' \
                    "$LATEST_VERSION" > "$release/$manifest_name"
                write_checksums
                ;;
        esac

        install_log="$case_dir/install.log"
        if [ -z "$expected" ]; then
            if ! download_and_extract "$INSTALL_DIR/omnihive.backup" "$INSTALL_DIR/omnihive" > "$install_log" 2>&1; then
                cat "$install_log" >&2
                exit 1
            fi
            [[ "$(cat "$INSTALL_DIR/omnihive")" == "new binary" ]]
            [[ "$(cat "$INSTALL_DIR/omnihive.backup")" == "installed binary" ]]
            [[ -x "$INSTALL_DIR/omnihive" ]]
            cmp "$case_dir/archive/resources/model-pricing/example.json" "$INSTALL_DIR/resources/model-pricing/example.json"
        else
            if download_and_extract "$INSTALL_DIR/omnihive.backup" "$INSTALL_DIR/omnihive" > "$install_log" 2>&1; then
                echo "Unexpected installer success for $test_case" >&2
                exit 1
            fi
            if [ "$test_case" = installed_db_unavailable ] && grep -Fxq "$asset_name" "$case_dir/downloads"; then
                echo 'Rejected target downloaded executable bytes' >&2
                exit 1
            fi
            grep -Fq "$expected" "$install_log" || { cat "$install_log" >&2; exit 1; }
            [[ "$(cat "$INSTALL_DIR/omnihive")" == "installed binary" ]]
            [[ ! -e "$INSTALL_DIR/omnihive.backup" ]]
            [[ ! -e "$INSTALL_DIR/resources" ]]
        fi
    )
    printf 'PASS: %s\n' "$test_case"
}

LANG_CHOICE=en
run_case verified_release
run_case installed_checker
run_case wrong_asset 'checksum for the OmniHive release asset'
run_case missing_checksums 'checksums.txt not found'
run_case missing_entry 'checksum for the OmniHive release asset'
run_case checksum_command_failed 'release manifest checksum'
run_case tampered_archive 'Checksum verification failed'
run_case missing_manifest 'release manifest checksum'
run_case tampered_manifest 'release manifest checksum'
run_case wrong_product 'wrong product'
run_case wrong_binary 'Wrong product executable'
run_case below_schema_floor 'database rollback compatibility floor'
run_case below_manifest_floor 'database rollback compatibility floor'
run_case db_unavailable 'database rollback floor is unavailable'
run_case installed_db_unavailable 'database rollback floor is unavailable'
run_case missing_checker 'checker unavailable'
