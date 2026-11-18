#!/bin/sh
set -e

if [ "$(id -u)" = 0 ]; then
    data_dir=${OMNIHIVE_DATA_DIR-${DATA_DIR-}}
    data_dir=${data_dir:-/app/data}
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

    chown -R omnihive:omnihive /app/data "$data_dir" 2>/dev/null || true
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
