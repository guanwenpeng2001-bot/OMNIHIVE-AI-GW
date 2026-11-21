#!/usr/bin/env bash
set -euo pipefail

# libpq selects the database from PGHOST, PGDATABASE, PGUSER and PGSERVICE.
psql_args=(-X -qAt -v ON_ERROR_STOP=1)
kinds=$(psql "${psql_args[@]}" -c 'SELECT kind FROM identity_backfill_checkpoints ORDER BY kind')
while IFS= read -r kind; do
    while true; do
        count=$(psql "${psql_args[@]}" -v kind="$kind" <<'SQL'
SELECT identity_backfill_batch(:'kind', 500);
SQL
)
        [[ "$count" == 0 ]] && break
    done
done <<< "$kinds"

psql -X -v ON_ERROR_STOP=1 -f "$(dirname "$0")/coverage.sql"
