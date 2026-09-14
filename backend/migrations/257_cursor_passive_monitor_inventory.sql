-- Import only a model inventory, never executable probes or traffic facts.
-- Keep a Cursor-only receipt so rollback cannot overwrite other platforms.
ALTER TABLE channel_monitor_v2_config
    ADD COLUMN IF NOT EXISTS cursor_legacy_import JSONB;

DO $$
DECLARE
    config_row channel_monitor_v2_config%ROWTYPE;
    previous_cursor JSONB;
    imported_cursor JSONB;
    imported_models JSONB;
    migrated_platforms JSONB;
BEGIN
    SELECT * INTO config_row FROM channel_monitor_v2_config WHERE id = 1 FOR UPDATE;
    IF NOT FOUND OR config_row.cursor_legacy_import IS NOT NULL THEN
        RETURN;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM channel_monitors WHERE provider = 'cursor')
       AND NOT EXISTS (SELECT 1 FROM channel_monitor_request_templates WHERE provider = 'cursor') THEN
        RETURN;
    END IF;

    SELECT platform_entry INTO previous_cursor
    FROM jsonb_array_elements(config_row.platforms) AS platform_entry
    WHERE platform_entry->>'platform' = 'cursor';

    -- Existing administrator choices retain their order and enabled state.
    -- Template bodies are inert: only an explicit string model is a dimension.
    WITH legacy_models AS (
        SELECT primary_model AS model FROM channel_monitors WHERE provider = 'cursor'
        UNION
        SELECT model_value #>> '{}' FROM channel_monitors
        CROSS JOIN LATERAL jsonb_array_elements(extra_models) AS model_value
        WHERE provider = 'cursor' AND jsonb_typeof(model_value) = 'string'
        UNION
        SELECT body_override->>'model' FROM channel_monitor_request_templates
        WHERE provider = 'cursor' AND body_override_mode IN ('merge', 'replace')
          AND jsonb_typeof(body_override->'model') = 'string'
        UNION
        SELECT body_override->>'model' FROM channel_monitors
        WHERE provider = 'cursor' AND body_override_mode IN ('merge', 'replace')
          AND jsonb_typeof(body_override->'model') = 'string'
    ), new_models AS (
        SELECT DISTINCT btrim(model) AS model FROM legacy_models
        WHERE btrim(model) <> ''
          AND NOT COALESCE(previous_cursor->'models', '[]'::jsonb) ? btrim(model)
    )
    SELECT COALESCE(jsonb_agg(model ORDER BY model), '[]'::jsonb)
    INTO imported_models FROM new_models;

    imported_cursor := COALESCE(previous_cursor,
        '{"platform":"cursor","enabled":false,"models":[]}'::jsonb);
    -- An existing empty list means all models; narrowing it would lose scope.
    IF previous_cursor->'models' = '[]'::jsonb THEN
        imported_models := '[]'::jsonb;
    END IF;
    imported_cursor := jsonb_set(imported_cursor, '{models}',
        COALESCE(imported_cursor->'models', '[]'::jsonb) || imported_models);
    SELECT COALESCE(jsonb_agg(
        CASE WHEN platform_entry->>'platform' = 'cursor' THEN imported_cursor ELSE platform_entry END
        ORDER BY position), '[]'::jsonb)
    INTO migrated_platforms
    FROM jsonb_array_elements(config_row.platforms) WITH ORDINALITY AS entries(platform_entry, position);
    IF previous_cursor IS NULL THEN
        migrated_platforms := migrated_platforms || jsonb_build_array(imported_cursor);
    END IF;

    UPDATE channel_monitor_v2_config
    SET platforms = migrated_platforms,
        version = version + 1,
        updated_at = NOW(),
        cursor_legacy_import = jsonb_build_object('before', previous_cursor, 'after', imported_cursor)
    WHERE id = 1;
END $$;
