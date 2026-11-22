#!/bin/sh
set -eu

if [ "$(id -u)" = 0 ]; then
    data_dir=${OMNIHIVE_DATA_DIR-${DATA_DIR-}}
    data_dir=${data_dir:-/app/data}

    # Only /app/data and its subdirectories are safe to manage as root. Reject
    # empty, relative, traversal or escape-prone values before any chown.
    case "$data_dir" in
        /app/data) ;;
        /app/data/*) ;;
        *) echo "Refusing unsafe OMNIHIVE_DATA_DIR: $data_dir" >&2; exit 1 ;;
    esac
    case "$data_dir" in
        *'..'*|*//*|*/.) echo "Refusing unsafe OMNIHIVE_DATA_DIR: $data_dir" >&2; exit 1 ;;
    esac

    mkdir -p /app/data "$data_dir"

    staged_binary="$data_dir/.updates/current/omnihive"
    if [ -f "$staged_binary" ]; then
        staged_copy=$(mktemp /app/.omnihive.XXXXXX)
        trap 'rm -f -- "$staged_copy"' EXIT
        su-exec omnihive cat "$staged_binary" > "$staged_copy"
        chmod 0755 "$staged_copy"
        chown omnihive:omnihive "$staged_copy"
        mv -f "$staged_copy" /app/omnihive
        trap - EXIT
    elif [ -f "$data_dir/.updates/current/sub2api" ]; then
        echo 'OmniHive found a deprecated staged executable. Keeping the image executable and retaining the old file for recovery.' >&2
    fi

    # Bounded ownership repair: fix the top-level data directories and only the
    # small writable subdirectories the runtime needs. Never recursively chown
    # an arbitrarily large mounted tree.
    app_uid=$(id -u omnihive 2>/dev/null || echo 1000)
    for dir in /app/data "$data_dir"; do
        [ -d "$dir" ] || continue
        owner=$(stat -c '%u' "$dir" 2>/dev/null || echo '')
        [ "$owner" = "$app_uid" ] || chown "$app_uid:$app_uid" "$dir"
    done
    for dir in "$data_dir/logs" "$data_dir/.updates" "$data_dir/.updates/current"; do
        [ -d "$dir" ] || continue
        owner=$(stat -c '%u' "$dir" 2>/dev/null || echo '')
        [ "$owner" = "$app_uid" ] || chown -R "$app_uid:$app_uid" "$dir"
    done
    exec su-exec omnihive "$0" "$@"
fi

case "${1:-}" in
    /app/sub2api)
        echo 'Warning: /app/sub2api is deprecated. Use /app/omnihive.' >&2
        shift
        set -- /app/omnihive "$@"
        ;;
    -*|'') set -- /app/omnihive "$@" ;;
esac

exec "$@"
