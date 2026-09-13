-- A conversation owns many independently anchored turn jobs. Session rows are
-- expiring distributed admission leases; they never carry a monetary anchor.
ALTER TABLE upstream_jobs DROP CONSTRAINT IF EXISTS upstream_jobs_family_check;
ALTER TABLE upstream_jobs ADD CONSTRAINT upstream_jobs_family_check
    CHECK (family IN ('video','3d','asr','batch','files','realtime_session','realtime_turn'));
CREATE INDEX IF NOT EXISTS idx_upstream_jobs_realtime_sessions ON upstream_jobs(expires_at,user_id,account_id)
    WHERE family='realtime_session' AND status IN ('submitted','running');
