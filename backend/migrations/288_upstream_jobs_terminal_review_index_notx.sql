-- Match the Ops terminal review gauge without scanning all historical successes.
-- This is separate from receipt review, not a combined count of billable usage.
-- Concurrent construction avoids blocking normal writes, but still scans the
-- table and consumes I/O. Schedule deployment accordingly on large installations.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_upstream_jobs_terminal_review
    ON upstream_jobs (id)
    WHERE status IN ('succeeded','failed','expired')
      AND error_code='settlement_needs_review'
      AND charged_usage_log_id IS NULL
      AND family<>'realtime_session';
