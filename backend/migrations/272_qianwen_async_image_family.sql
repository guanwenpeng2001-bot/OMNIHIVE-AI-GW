-- 272_qianwen_async_image_family.sql
-- Task: actions-qianwen (Kimi worker, dispatch ctx_6173d74fe2b3). Extends the
-- upstream_jobs family constraint with 'image' for the Wan text-to-image
-- asynchronous family (official DashScope text2image protocol, evidence
-- 2026-09-22). The upstream_jobs table itself belongs to upstream-job-integrity
-- (migration 270); this migration only widens the family CHECK.
-- Rollback floor: code SHA before the actions-qianwen image family adapter
-- (qianwen_image_jobs.go); no data rewrite, no new semantic writes beyond
-- family='image' rows created by the new adapter, which pre-272 code rejects.

ALTER TABLE upstream_jobs DROP CONSTRAINT IF EXISTS upstream_jobs_family_check;
ALTER TABLE upstream_jobs ADD CONSTRAINT upstream_jobs_family_check
    CHECK (family IN ('video','3d','asr','batch','files','image','realtime_session','realtime_turn'));
