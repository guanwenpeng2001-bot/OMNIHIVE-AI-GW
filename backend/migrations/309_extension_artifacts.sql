-- Native and rebuilt artifacts share the installed-package authority. SHA-256
-- is a PostgreSQL core function; no network trust store or extension is needed.
CREATE VIEW omnihive_extension_inventory AS
SELECT id, plugin_key, version, state,
       (manifest->>'schema_version')::integer AS sdk_version,
       manifest->>'publisher_id' AS publisher_id,
       encode(sha256(artifact_data), 'hex') AS digest,
       manifest->'origin' AS verified_predecessor,
       manifest->'capabilities' AS capabilities
  FROM sub2api_plugin_installations;

-- The same provider scope cannot be enabled across protocol generations.
CREATE UNIQUE INDEX omnihive_extension_one_oauth_provider
    ON sub2api_plugin_bindings (platform, account_type)
    WHERE enabled AND capability IN ('openai.oauth.outbound_transport.v1', 'provider');

-- UI/task grants belong to individual plugins. Keep v1 singleton binding
-- semantics for every legacy scope during rolling deployments.
DROP INDEX idx_sub2api_plugin_bindings_one_enabled_scope;
CREATE UNIQUE INDEX idx_sub2api_plugin_bindings_one_enabled_scope
    ON sub2api_plugin_bindings (capability, platform, account_type)
    WHERE enabled AND capability NOT IN ('ui', 'task');

-- Preserve the old/new verified identity after an installation is replaced or
-- uninstalled. Original archives remain in the operator's rollback inventory.
CREATE TABLE omnihive_extension_migrations (
    target_digest text PRIMARY KEY,
    source_digest text NOT NULL,
    source_manifest_digest text NOT NULL,
    source_plugin_id text NOT NULL,
    source_key_id text NOT NULL,
    target_plugin_id text NOT NULL,
    target_key_id text NOT NULL,
    capabilities jsonb NOT NULL,
    verified_at timestamptz NOT NULL DEFAULT now()
);

CREATE FUNCTION omnihive_record_extension_migration() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO omnihive_extension_migrations (
        target_digest, source_digest, source_manifest_digest, source_plugin_id,
        source_key_id, target_plugin_id, target_key_id, capabilities
    ) VALUES (
        encode(sha256(NEW.artifact_data), 'hex'), NEW.manifest #>> '{origin,archive_sha256}',
        NEW.manifest #>> '{origin,manifest_sha256}', NEW.manifest #>> '{origin,plugin_id}',
        NEW.manifest #>> '{origin,key_id}', NEW.plugin_key,
        NEW.manifest->>'publisher_id', NEW.manifest->'capabilities'
    ) ON CONFLICT (target_digest) DO NOTHING;
    RETURN NEW;
END;
$$;

CREATE TRIGGER omnihive_extension_migration_record
AFTER INSERT OR UPDATE OF artifact_data, manifest ON sub2api_plugin_installations
FOR EACH ROW WHEN ((NEW.manifest->>'schema_version') = '2' AND NEW.manifest ? 'origin')
EXECUTE FUNCTION omnihive_record_extension_migration();
