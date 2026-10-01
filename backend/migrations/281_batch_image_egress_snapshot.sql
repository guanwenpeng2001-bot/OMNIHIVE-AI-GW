-- Server-owned egress identifiers/mode only. Legacy jobs remain NULL;
-- credentials, proxy URLs and submission/billing fingerprints are not stored here.
ALTER TABLE batch_image_jobs ADD COLUMN IF NOT EXISTS egress_snapshot JSONB;
