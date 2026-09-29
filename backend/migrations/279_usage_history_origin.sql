-- Independent evidence of an empty prefix, never inferred from surviving logs.
ALTER TABLE usage_dashboard_aggregation_watermark
    ADD COLUMN history_known_empty_before TIMESTAMPTZ NULL;

CREATE OR REPLACE FUNCTION invalidate_usage_dashboard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    affected TIMESTAMPTZ;
    group_published_before DATE;
    dashboard_published_before TIMESTAMPTZ;
    known_empty_before TIMESTAMPTZ;
BEGIN
    IF TG_OP = 'INSERT' THEN affected := NEW.created_at;
    ELSIF TG_OP = 'DELETE' THEN affected := OLD.created_at;
    ELSE affected := LEAST(OLD.created_at, NEW.created_at);
    END IF;
    -- Also protect direct writes to a partition, whose statement trigger differs.
    SELECT closed_before INTO group_published_before
      FROM usage_group_rollup_state WHERE id = 1 FOR KEY SHARE;
    SELECT last_aggregated_at, history_known_empty_before INTO dashboard_published_before, known_empty_before
      FROM usage_dashboard_aggregation_watermark WHERE id = 1 FOR KEY SHARE;
    -- A correction can change both states (the group INSERT trigger runs after
    -- these row triggers). Serialize corrections group-first before either
    -- non-key update, including transactions containing multiple statements.
    IF affected < known_empty_before OR affected < dashboard_published_before
       OR (affected AT TIME ZONE current_setting('TimeZone'))::date < group_published_before THEN
        PERFORM id FROM usage_group_rollup_state WHERE id = 1 FOR NO KEY UPDATE;
    END IF;
    IF affected < known_empty_before THEN
        UPDATE usage_dashboard_aggregation_watermark
           SET history_known_empty_before = NULL, updated_at = NOW()
         WHERE id = 1 AND affected < history_known_empty_before;
    END IF;
    -- Retention is a verified projection-preserving operation, not a correction.
    IF TG_OP = 'DELETE' AND current_setting('sub2api.usage_retention', true) = 'on' THEN
        RETURN OLD;
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
