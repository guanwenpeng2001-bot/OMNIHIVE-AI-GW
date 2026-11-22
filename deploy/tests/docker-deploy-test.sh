#!/bin/bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
test_dir=$(mktemp -d)
trap 'rm -rf "$test_dir"' EXIT
mock_bin="$test_dir/bin"
mkdir -p "$mock_bin"

# The one-click deployment template, README command and Compose image fallback
# must all stay pinned to the repository VERSION so a release cannot drift.
release_version=$(tr -d '\r\n' < "$repo_root/backend/cmd/server/VERSION")
default_tag="v${release_version}"
grep -Fq "DEFAULT_RELEASE_TAG=\"${default_tag}\"" "$repo_root/deploy/docker-deploy.sh" || {
    echo "FAIL: docker-deploy.sh DEFAULT_RELEASE_TAG must equal backend/cmd/server/VERSION (${default_tag})" >&2
    exit 1
}
grep -Fq "${default_tag}/deploy/docker-deploy.sh" "$repo_root/deploy/README.md" || {
    echo "FAIL: deploy/README.md one-click commands must use the repository tag ${default_tag}" >&2
    exit 1
}
for compose_file in \
    "$repo_root/deploy/docker-compose.yml" \
    "$repo_root/deploy/docker-compose.local.yml" \
    "$repo_root/deploy/docker-compose.standalone.yml"
do
    grep -Fq "omnihive-ai-gw:\${OMNIHIVE_IMAGE_TAG:-${default_tag}}" "$compose_file" || {
        echo "FAIL: ${compose_file} application image must default to \${OMNIHIVE_IMAGE_TAG:-${default_tag}}" >&2
        exit 1
    }
done

cat > "$mock_bin/curl" <<'EOF'
#!/bin/bash
set -euo pipefail
output=
url=
has_fail=false
while (($#)); do
    case "$1" in
        -*f*) has_fail=true; shift ;;
        -o) output=$2; shift 2 ;;
        -*) shift ;;
        *) url=$1; shift ;;
    esac
done
[[ "$has_fail" == true && -n "$output" && -n "$url" ]]
printf '%s\n' "$url" >> "$CURL_LOG"
if [[ "${FAIL_ENV_DOWNLOAD:-false}" == true && "$url" == */.env.example ]]; then
    printf 'HTTP error page\n' > "$output"
    exit 22
fi
if [[ "$url" != "$EXPECTED_BASE"/* ]]; then
    printf 'unexpected download URL: %s\n' "$url" >&2
    exit 23
fi
case "$url" in
    */docker-compose.local.yml) cp "$REPO_ROOT/deploy/docker-compose.local.yml" "$output" ;;
    */.env.example) cp "$REPO_ROOT/deploy/.env.example" "$output" ;;
    *) exit 24 ;;
esac
EOF
cat > "$mock_bin/openssl" <<'EOF'
#!/bin/sh
printf '%064d\n' 0
EOF
chmod +x "$mock_bin/curl" "$mock_bin/openssl"

run_deploy() {
    local directory=$1
    shift
    mkdir -p "$directory"
    (
        cd "$directory"
        PATH="$mock_bin:$PATH" \
            REPO_ROOT="$repo_root" \
            EXPECTED_BASE="$EXPECTED_BASE" \
            CURL_LOG="$CURL_LOG" \
            ${FAIL_ENV_DOWNLOAD:+FAIL_ENV_DOWNLOAD="$FAIL_ENV_DOWNLOAD"} \
            bash "$repo_root/deploy/docker-deploy.sh" "$@"
    )
}

CURL_LOG="$test_dir/curl.log"
: > "$CURL_LOG"
EXPECTED_BASE="https://raw.githubusercontent.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/${default_tag}/deploy"
run_deploy "$test_dir/default" > /dev/null
cmp "$repo_root/deploy/docker-compose.local.yml" "$test_dir/default/docker-compose.yml"
cmp "$repo_root/deploy/.env.example" "$test_dir/default/.env.example"
[[ "$(ls -l "$test_dir/default/.env" | cut -c1-10)" == "-rw-------" ]]
[[ $(wc -l < "$CURL_LOG") -eq 2 ]]
! grep -q '/main/' "$CURL_LOG"
[[ -z $(find "$test_dir/default" -maxdepth 1 -name '.omnihive-deploy.*' -print -quit) ]]
echo 'PASS: default downloads use the release tag and curl fail-on-error'

: > "$CURL_LOG"
EXPECTED_BASE="https://raw.githubusercontent.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/v9.8.7/deploy"
OMNIHIVE_RELEASE_TAG=v9.8.7 run_deploy "$test_dir/override" > /dev/null
echo 'PASS: release tag can be overridden'

printf 'keep compose\n' > "$test_dir/compose-only.yml"
mkdir "$test_dir/compose-only"
cp "$test_dir/compose-only.yml" "$test_dir/compose-only/docker-compose.yml"
if run_deploy "$test_dir/compose-only" > /dev/null 2>&1; then
    echo 'FAIL: existing Compose file was overwritten' >&2
    exit 1
fi
cmp "$test_dir/compose-only.yml" "$test_dir/compose-only/docker-compose.yml"
[[ ! -e "$test_dir/compose-only/.env" ]]
echo 'PASS: an existing Compose file is protected independently'

printf 'keep env\n' > "$test_dir/env-only.txt"
mkdir "$test_dir/env-only"
cp "$test_dir/env-only.txt" "$test_dir/env-only/.env"
if run_deploy "$test_dir/env-only" > /dev/null 2>&1; then
    echo 'FAIL: existing env file was overwritten' >&2
    exit 1
fi
cmp "$test_dir/env-only.txt" "$test_dir/env-only/.env"
[[ ! -e "$test_dir/env-only/docker-compose.yml" ]]
echo 'PASS: an existing env file is protected independently'

EXPECTED_BASE="https://raw.githubusercontent.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/${default_tag}/deploy"
FAIL_ENV_DOWNLOAD=true
export FAIL_ENV_DOWNLOAD
if run_deploy "$test_dir/http-error" > /dev/null 2>&1; then
    echo 'FAIL: HTTP download error was accepted' >&2
    exit 1
fi
[[ ! -e "$test_dir/http-error/docker-compose.yml" ]]
[[ ! -e "$test_dir/http-error/.env.example" ]]
[[ ! -e "$test_dir/http-error/.env" ]]
[[ -z $(find "$test_dir/http-error" -maxdepth 1 -name '.omnihive-deploy.*' -print -quit) ]]
echo 'PASS: failed downloads leave no partial deployment files'
