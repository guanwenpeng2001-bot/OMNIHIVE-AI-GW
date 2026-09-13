-- Files use the same ownership and atomic zero-cost audit anchor as jobs.
ALTER TABLE upstream_jobs DROP CONSTRAINT IF EXISTS upstream_jobs_family_check;
ALTER TABLE upstream_jobs ADD CONSTRAINT upstream_jobs_family_check
    CHECK (family IN ('video', '3d', 'asr', 'batch', 'files'));
