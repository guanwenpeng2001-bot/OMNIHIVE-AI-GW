-- Keep ownership, billing anchors and submission claims after remote deletion.
ALTER TABLE upstream_jobs ADD COLUMN IF NOT EXISTS resource_state JSONB NOT NULL DEFAULT '{}';
CREATE INDEX IF NOT EXISTS idx_upstream_jobs_owned_resources
    ON upstream_jobs (user_id, api_key_id, group_id, family, id DESC);
CREATE INDEX IF NOT EXISTS idx_upstream_jobs_input_file
    ON upstream_jobs ((billing_snapshot->'file_batch'->>'input_file_id')) WHERE family='batch';
