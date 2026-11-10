-- Unpriced OpenRouter and Kaggle video may finish as succeeded with
-- error_code=settlement_needs_review and no usage charge. The original
-- 247 check required charged_usage_log_id on every succeeded row, so the
-- poller rolled back after a completed upstream poll and left the job running.

ALTER TABLE upstream_jobs DROP CONSTRAINT IF EXISTS upstream_jobs_check1;
ALTER TABLE upstream_jobs DROP CONSTRAINT IF EXISTS upstream_jobs_succeeded_settlement_check;

ALTER TABLE upstream_jobs
    ADD CONSTRAINT upstream_jobs_succeeded_settlement_check
    CHECK (
        status <> 'succeeded'
        OR charged_usage_log_id IS NOT NULL
        OR error_code = 'settlement_needs_review'
    );
