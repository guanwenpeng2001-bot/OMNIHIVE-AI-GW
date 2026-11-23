CREATE TABLE IF NOT EXISTS backup_jobs (
    id TEXT PRIMARY KEY,
    triggered_by TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('pending', 'running', 'completed', 'failed', 'skipped_not_leader', 'deleted')),
    -- attempt_id/revision fence every worker update. A deleted row is retained
    -- as a tombstone so a delayed worker cannot recreate a verified record for
    -- an object that retention or an operator has already removed.
    attempt_id TEXT NOT NULL,
    revision BIGINT NOT NULL DEFAULT 1,
    deleted_at TIMESTAMPTZ,
    progress TEXT,
    started_at TIMESTAMPTZ NOT NULL,
    finished_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ,
    error_message TEXT,
    object_manifest JSONB NOT NULL DEFAULT '{}'::jsonb,
    verified_at TIMESTAMPTZ,
    record JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_backup_jobs_started_at ON backup_jobs (started_at DESC);
CREATE INDEX IF NOT EXISTS idx_backup_jobs_status ON backup_jobs (status, started_at DESC);

CREATE TABLE IF NOT EXISTS backup_scheduler_state (
    state_key TEXT PRIMARY KEY,
    loaded BOOLEAN NOT NULL DEFAULT false,
    health TEXT NOT NULL DEFAULT 'unknown' CHECK (health IN ('healthy', 'degraded', 'error', 'unknown')),
    next_run_at TIMESTAMPTZ,
    last_attempt_at TIMESTAMPTZ,
    last_success_at TIMESTAMPTZ,
    last_verified_at TIMESTAMPTZ,
    last_error TEXT,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (state_key = 'backup_scheduler')
);
INSERT INTO backup_scheduler_state (state_key) VALUES ('backup_scheduler') ON CONFLICT DO NOTHING;

-- Import legacy records once. IDs and restore-journal references remain inside
-- record JSON, avoiding an irreversible rewrite of historical metadata.
INSERT INTO backup_jobs (id, triggered_by, status, attempt_id, revision, progress, started_at, finished_at, expires_at, error_message, object_manifest, verified_at, record, updated_at)
SELECT
    item->>'id',
    COALESCE(item->>'triggered_by', 'manual'),
    CASE WHEN item->>'status' IN ('pending', 'running', 'completed', 'failed') THEN item->>'status' ELSE 'failed' END,
    'legacy:' || (item->>'id'),
    1,
    NULLIF(item->>'progress', ''),
    COALESCE(NULLIF(item->>'started_at', '')::timestamptz, now()),
    NULLIF(item->>'finished_at', '')::timestamptz,
    NULLIF(item->>'expires_at', '')::timestamptz,
    NULLIF(item->>'error_message', ''),
    jsonb_build_object('s3_key', item->>'s3_key', 'parts', COALESCE(item->'parts', '[]'::jsonb)),
    NULLIF(item->>'verified_at', '')::timestamptz,
    item,
    now()
FROM settings s CROSS JOIN LATERAL jsonb_array_elements(CASE WHEN jsonb_typeof(s.value::jsonb) = 'array' THEN s.value::jsonb ELSE '[]'::jsonb END) item
WHERE s.key = 'backup_records' AND item ? 'id'
ON CONFLICT (id) DO NOTHING;
