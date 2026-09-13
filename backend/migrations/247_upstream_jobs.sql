-- Durable async ownership and settlement. Claims deliberately survive process
-- death before the upstream task ID is known; they must never be resubmitted.
CREATE TABLE IF NOT EXISTS upstream_jobs (
    id BIGSERIAL PRIMARY KEY,
    public_request_id TEXT NOT NULL UNIQUE,
    request_fingerprint TEXT NOT NULL UNIQUE,
    submission_key TEXT NOT NULL UNIQUE,
    platform TEXT NOT NULL,
    family TEXT NOT NULL CHECK (family IN ('video', '3d', 'asr', 'batch')),
    account_id BIGINT NOT NULL REFERENCES accounts(id),
    user_id BIGINT NOT NULL REFERENCES users(id),
    api_key_id BIGINT NOT NULL REFERENCES api_keys(id),
    group_id BIGINT REFERENCES groups(id),
    upstream_task_id TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'submitted'
        CHECK (status IN ('submitted', 'running', 'succeeded', 'failed', 'expired')),
    attempt_count INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    last_polled_at TIMESTAMPTZ,
    next_poll_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    submitted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ NOT NULL,
    result_ref TEXT NOT NULL DEFAULT '',
    usage_json JSONB,
    charged_usage_log_id BIGINT,
    error_code TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    billing_snapshot JSONB NOT NULL,
    CHECK (charged_usage_log_id IS NULL OR status = 'succeeded'),
    CHECK (status <> 'succeeded' OR charged_usage_log_id IS NOT NULL),
    CHECK ((status IN ('succeeded', 'failed', 'expired')) = (finished_at IS NOT NULL))
);
-- usage_logs may be partitioned/retained independently: the anchor is not an FK.
CREATE INDEX IF NOT EXISTS idx_upstream_jobs_due ON upstream_jobs(status, next_poll_at);
CREATE INDEX IF NOT EXISTS idx_upstream_jobs_upstream_task ON upstream_jobs(upstream_task_id);
