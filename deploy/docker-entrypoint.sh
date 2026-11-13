#!/bin/sh
set -e

# Fix data directory permissions when running as root.
# Docker named volumes / host bind-mounts may be owned by root,
# preventing the non-root sub2api user from writing files.
if [ "$(id -u)" = "0" ]; then
    mkdir -p /app/data
    if [ "${OMNIHIVE_DATA_DIR:-/app/data}" = /app/data ] && [ -f /app/data/.updates/current/sub2api ]; then
        cp -f /app/data/.updates/current/sub2api /app/sub2api
        chmod 0755 /app/sub2api
        chown sub2api:sub2api /app/sub2api
    fi
    # A custom persistent data volume uses the same staging layout.
    if [ -n "${OMNIHIVE_DATA_DIR:-}" ] && [ "$OMNIHIVE_DATA_DIR" != /app/data ]; then
        if [ -f "$OMNIHIVE_DATA_DIR/.updates/current/sub2api" ]; then
            cp -f "$OMNIHIVE_DATA_DIR/.updates/current/sub2api" /app/sub2api
            chmod 0755 /app/sub2api
            chown sub2api:sub2api /app/sub2api
        fi
        mkdir -p "$OMNIHIVE_DATA_DIR"
        chown -R sub2api:sub2api "$OMNIHIVE_DATA_DIR" 2>/dev/null || true
    fi
    # Use || true to avoid failure on read-only mounted files (e.g. config.yaml:ro)
    chown -R sub2api:sub2api /app/data 2>/dev/null || true
    # Re-invoke this script as sub2api so the flag-detection below
    # also runs under the correct user.
    exec su-exec sub2api "$0" "$@"
fi

# Compatibility: if the first arg looks like a flag (e.g. --help),
# prepend the default binary so it behaves the same as the old
# ENTRYPOINT ["/app/sub2api"] style.
if [ "${1#-}" != "$1" ]; then
    set -- /app/sub2api "$@"
fi

exec "$@"
