-- Calibration entry: with all publication/cleanup jobs stopped, an audited
-- operator may update raw_retained_from under group-state -> dashboard-state
-- locks only while last_aggregated_at is epoch, timezone_name is empty and both
-- projection floors are NULL. Compare the original raw floor before updating;
-- never derive approval from surviving log MIN/MAX or clear dirty_from here.
-- After calibration AggregateRangeAndPublish rebuilds consecutive calendar days.
-- Old deployments did not record raw retention. Do not infer completeness from
-- MIN(created_at): the first surviving row cannot prove that earlier rows exist.
ALTER TABLE usage_dashboard_aggregation_watermark
    ADD COLUMN raw_retained_from TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    ADD COLUMN hourly_retained_from TIMESTAMPTZ,
    ADD COLUMN daily_retained_from TIMESTAMPTZ,
    ADD COLUMN timezone_name TEXT NOT NULL DEFAULT '',
    ADD COLUMN projection_version INTEGER NOT NULL DEFAULT 2,
    ADD COLUMN dirty_from TIMESTAMPTZ;

-- Existing projections have neither a known calendar nor proven coverage.
UPDATE usage_dashboard_aggregation_watermark
SET last_aggregated_at = '1970-01-01', updated_at = NOW();

CREATE FUNCTION lock_usage_dashboard_writer() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    -- Writers coexist; publication and retention still take FOR UPDATE in this
    -- order. KEY SHARE also permits non-key dirty-watermark updates, avoiding
    -- a shared-to-exclusive lock upgrade between concurrent corrections.
    PERFORM id FROM usage_group_rollup_state WHERE id = 1 FOR KEY SHARE;
    PERFORM id FROM usage_dashboard_aggregation_watermark WHERE id = 1 FOR KEY SHARE;
    RETURN NULL;
END;
$$;

CREATE TRIGGER usage_logs_dashboard_writer_lock
BEFORE INSERT OR UPDATE OR DELETE ON usage_logs
FOR EACH STATEMENT EXECUTE FUNCTION lock_usage_dashboard_writer();

CREATE FUNCTION invalidate_usage_dashboard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    affected TIMESTAMPTZ;
    group_published_before DATE;
    dashboard_published_before TIMESTAMPTZ;
BEGIN
    -- Retention is a verified projection-preserving operation, not a correction.
    IF TG_OP = 'DELETE' AND current_setting('sub2api.usage_retention', true) = 'on' THEN
        RETURN OLD;
    END IF;
    IF TG_OP = 'INSERT' THEN affected := NEW.created_at;
    ELSIF TG_OP = 'DELETE' THEN affected := OLD.created_at;
    ELSE affected := LEAST(OLD.created_at, NEW.created_at);
    END IF;
    -- Also protect direct writes to a partition, whose statement trigger differs.
    SELECT closed_before INTO group_published_before
      FROM usage_group_rollup_state WHERE id = 1 FOR KEY SHARE;
    SELECT last_aggregated_at INTO dashboard_published_before
      FROM usage_dashboard_aggregation_watermark WHERE id = 1 FOR KEY SHARE;
    -- A correction can change both states (the group INSERT trigger runs after
    -- these row triggers). Serialize corrections group-first before either
    -- non-key update, including transactions containing multiple statements.
    IF affected < dashboard_published_before
       OR (affected AT TIME ZONE current_setting('TimeZone'))::date < group_published_before THEN
        PERFORM id FROM usage_group_rollup_state WHERE id = 1 FOR NO KEY UPDATE;
    END IF;
    IF affected < dashboard_published_before THEN
        UPDATE usage_dashboard_aggregation_watermark
           SET dirty_from = LEAST(dirty_from, affected), updated_at = NOW()
         WHERE id = 1 AND affected < last_aggregated_at;
    END IF;
    IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER usage_logs_dashboard_invalidate
AFTER INSERT OR UPDATE OR DELETE ON usage_logs
FOR EACH ROW EXECUTE FUNCTION invalidate_usage_dashboard();

-- Keep the pre-existing group UPDATE/DELETE trigger on the same writer lock
-- protocol. FOR UPDATE here would upgrade a shared writer lock and deadlock
-- with another correction that already holds its own KEY SHARE lock.
CREATE OR REPLACE FUNCTION invalidate_group_usage_rollup_state()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    affected_date DATE;
    published_before DATE;
    configured_timezone TEXT := current_setting('TimeZone');
BEGIN
    IF TG_OP = 'DELETE' THEN
        affected_date := (OLD.created_at AT TIME ZONE configured_timezone)::date;
    ELSIF OLD.group_id IS NULL THEN
        affected_date := (NEW.created_at AT TIME ZONE configured_timezone)::date;
    ELSIF NEW.group_id IS NULL THEN
        affected_date := (OLD.created_at AT TIME ZONE configured_timezone)::date;
    ELSE
        affected_date := LEAST(
            (OLD.created_at AT TIME ZONE configured_timezone)::date,
            (NEW.created_at AT TIME ZONE configured_timezone)::date
        );
    END IF;
    SELECT closed_before INTO published_before
      FROM usage_group_rollup_state WHERE id = 1 FOR KEY SHARE;
    IF published_before > affected_date THEN
        UPDATE usage_group_rollup_state
           SET closed_before = LEAST(closed_before, affected_date), updated_at = NOW()
         WHERE id = 1;
    END IF;
    IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END;
$$;
