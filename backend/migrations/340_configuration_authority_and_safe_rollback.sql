-- Existing installations keep the previously implicit v2 plaza enablement.
-- A new database is migrated before settings/admin bootstrap and remains false.
WITH inherited AS (
    INSERT INTO settings (key, value, updated_at)
    SELECT 'model_plaza_enabled', 'true', NOW()
    WHERE (EXISTS (SELECT 1 FROM users)
           OR EXISTS (SELECT 1 FROM settings WHERE key = 'registration_enabled'))
      AND NOT EXISTS (SELECT 1 FROM settings WHERE key = 'model_plaza_enabled')
    ON CONFLICT (key) DO NOTHING
    RETURNING key
)
INSERT INTO settings (key, value, updated_at)
SELECT 'model_plaza_enabled_source', 'migration_snapshot', NOW()
FROM inherited
ON CONFLICT (key) DO NOTHING;

-- Preserve the schema floor separately. The legacy compatibility field is also
-- raised: old readers must not ignore the semantic floor of versioned secrets.
ALTER TABLE release_schema_compatibility
    ADD COLUMN IF NOT EXISTS schema_rollback_floor text,
    ADD COLUMN IF NOT EXISTS semantic_rollback_floor text;

UPDATE release_schema_compatibility
SET schema_rollback_floor = COALESCE(schema_rollback_floor, minimum_rollback_version),
    semantic_rollback_floor = CASE
      WHEN semantic_rollback_floor IS NOT NULL
       AND string_to_array(regexp_replace(ltrim(semantic_rollback_floor, 'v'), '[-+].*$', ''), '.')::integer[] >= ARRAY[0,3,5]
      THEN semantic_rollback_floor ELSE '0.3.5' END,
    minimum_rollback_version = CASE
      WHEN string_to_array(regexp_replace(ltrim(minimum_rollback_version, 'v'), '[-+].*$', ''), '.')::integer[] >= ARRAY[0,3,5]
      THEN minimum_rollback_version ELSE '0.3.5' END
WHERE singleton;
