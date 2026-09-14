-- PostgreSQL owns derived usage; Redis snapshots cannot overwrite an event-owned row.
ALTER TABLE user_platform_quotas
 ADD COLUMN derived_usage BOOLEAN NOT NULL DEFAULT FALSE,
 ADD COLUMN daily_generation BIGINT NOT NULL DEFAULT 0,
 ADD COLUMN weekly_generation BIGINT NOT NULL DEFAULT 0,
 ADD COLUMN monthly_generation BIGINT NOT NULL DEFAULT 0,
 ADD COLUMN daily_reset_at TIMESTAMPTZ,
 ADD COLUMN weekly_reset_at TIMESTAMPTZ,
 ADD COLUMN monthly_reset_at TIMESTAMPTZ;
CREATE TABLE user_platform_quota_events (
 id BIGSERIAL PRIMARY KEY,
 usage_log_id BIGINT NOT NULL,
 effect_kind TEXT NOT NULL DEFAULT 'platform_usage',
 quota_id BIGINT NOT NULL,
 user_id BIGINT NOT NULL,
 platform TEXT NOT NULL,
 cost NUMERIC(20,8) NOT NULL CHECK (cost > 0),
 consumed_at TIMESTAMPTZ NOT NULL,
 daily_start TIMESTAMPTZ NOT NULL,
 weekly_start TIMESTAMPTZ NOT NULL,
 monthly_start TIMESTAMPTZ NOT NULL,
 daily_generation BIGINT NOT NULL,
 weekly_generation BIGINT NOT NULL,
 monthly_generation BIGINT NOT NULL,
 applied_at TIMESTAMPTZ,
 cache_invalidated_at TIMESTAMPTZ,
 UNIQUE (usage_log_id,effect_kind)
);
CREATE INDEX user_platform_quota_events_pending ON user_platform_quota_events(id)
 WHERE cache_invalidated_at IS NULL;
CREATE INDEX user_platform_quota_events_month_anchor ON user_platform_quota_events(quota_id,monthly_generation,monthly_start DESC);
