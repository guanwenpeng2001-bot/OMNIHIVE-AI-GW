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
    -- Same order as group publication, dashboard publication and retention.
    PERFORM id FROM usage_group_rollup_state WHERE id = 1 FOR UPDATE;
    PERFORM id FROM usage_dashboard_aggregation_watermark WHERE id = 1 FOR UPDATE;
    RETURN NULL;
END;
$$;

CREATE TRIGGER usage_logs_dashboard_writer_lock
BEFORE INSERT OR UPDATE OR DELETE ON usage_logs
FOR EACH STATEMENT EXECUTE FUNCTION lock_usage_dashboard_writer();

CREATE FUNCTION invalidate_usage_dashboard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE affected TIMESTAMPTZ;
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
    PERFORM id FROM usage_group_rollup_state WHERE id = 1 FOR UPDATE;
    UPDATE usage_dashboard_aggregation_watermark
       SET dirty_from = LEAST(dirty_from, affected), updated_at = NOW()
     WHERE id = 1 AND affected < last_aggregated_at;
    IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER usage_logs_dashboard_invalidate
AFTER INSERT OR UPDATE OR DELETE ON usage_logs
FOR EACH ROW EXECUTE FUNCTION invalidate_usage_dashboard();
