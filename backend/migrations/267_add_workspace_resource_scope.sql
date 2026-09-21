-- Expand before delegation is enabled. Existing IDs and historical usage stay intact.
CREATE TABLE IF NOT EXISTS workspaces (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS workspaces_one_default ON workspaces(is_default) WHERE is_default;
INSERT INTO workspaces(id,name,is_default) VALUES(1,'Default',TRUE) ON CONFLICT(id) DO NOTHING;
SELECT setval(pg_get_serial_sequence('workspaces','id'), GREATEST((SELECT MAX(id) FROM workspaces),
    (SELECT last_value FROM workspaces_id_seq)), TRUE);

CREATE TABLE IF NOT EXISTS workspace_members (
    id BIGSERIAL PRIMARY KEY,
    workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role VARCHAR(16) NOT NULL CHECK(role IN ('member','admin')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(workspace_id,user_id)
);

-- Every update is conditional and bounded. Re-running never moves resources from
-- an explicitly assigned workspace, including when a previous run was interrupted.
DO $workspace_backfill$
DECLARE
    resource_table TEXT;
    updated_rows INTEGER;
BEGIN
    FOREACH resource_table IN ARRAY ARRAY['accounts','groups','channels','proxies','channel_monitors',
        'channel_monitor_request_templates','api_keys'] LOOP
        EXECUTE format('ALTER TABLE %I ADD COLUMN IF NOT EXISTS workspace_id BIGINT REFERENCES workspaces(id)', resource_table);
        EXECUTE format('ALTER TABLE %I ALTER COLUMN workspace_id SET DEFAULT 1', resource_table);
        LOOP
            EXECUTE format('WITH batch AS (SELECT id FROM %I WHERE workspace_id IS NULL ORDER BY id LIMIT 1000)
                UPDATE %I resource SET workspace_id=1 FROM batch WHERE resource.id=batch.id AND resource.workspace_id IS NULL',
                resource_table,resource_table);
            GET DIAGNOSTICS updated_rows = ROW_COUNT;
            EXIT WHEN updated_rows=0;
        END LOOP;
        EXECUTE format('ALTER TABLE %I ALTER COLUMN workspace_id SET NOT NULL', resource_table);
        EXECUTE format('CREATE INDEX IF NOT EXISTS %I ON %I(workspace_id,id)', resource_table || '_workspace_id_idx',resource_table);
    END LOOP;
END;
$workspace_backfill$;

DROP INDEX IF EXISTS groups_name_unique_active;
CREATE UNIQUE INDEX IF NOT EXISTS groups_workspace_name_unique_active ON groups(workspace_id,name) WHERE deleted_at IS NULL;
DROP INDEX IF EXISTS idx_channels_name;
CREATE UNIQUE INDEX IF NOT EXISTS channels_workspace_name_unique ON channels(workspace_id,name);
DROP INDEX IF EXISTS channel_monitor_request_templates_provider_name;
CREATE UNIQUE INDEX IF NOT EXISTS channel_monitor_templates_workspace_name ON channel_monitor_request_templates(workspace_id,provider,name);

-- Ownership cannot be reassigned by a legacy writer or by a stale request.
CREATE OR REPLACE FUNCTION prevent_workspace_resource_move() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.workspace_id IS DISTINCT FROM OLD.workspace_id THEN
        RAISE EXCEPTION 'resource ownership cannot be changed' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
DO $workspace_triggers$
DECLARE resource_table TEXT;
BEGIN
    FOREACH resource_table IN ARRAY ARRAY['accounts','groups','channels','proxies','channel_monitors',
        'channel_monitor_request_templates','api_keys'] LOOP
        EXECUTE format('DROP TRIGGER IF EXISTS workspace_resource_immutable ON %I',resource_table);
        EXECUTE format('CREATE TRIGGER workspace_resource_immutable BEFORE UPDATE OF workspace_id ON %I
            FOR EACH ROW EXECUTE FUNCTION prevent_workspace_resource_move()',resource_table);
    END LOOP;
END;
$workspace_triggers$;

-- Enforce associations for every writer, including background tasks and imports.
CREATE OR REPLACE FUNCTION check_workspace_resource_reference() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    source_workspace BIGINT;
    target_workspace BIGINT;
    reference_id BIGINT;
BEGIN
    IF TG_ARGV[0]='workspace_id' THEN
        source_workspace := (to_jsonb(NEW)->>'workspace_id')::BIGINT;
    ELSE
        EXECUTE format('SELECT workspace_id FROM %I WHERE id=$1',TG_ARGV[1])
            INTO source_workspace USING (to_jsonb(NEW)->>TG_ARGV[0])::BIGINT;
    END IF;
    reference_id := (to_jsonb(NEW)->>TG_ARGV[2])::BIGINT;
    IF reference_id IS NULL THEN RETURN NEW; END IF;
    EXECUTE format('SELECT workspace_id FROM %I WHERE id=$1',TG_ARGV[3]) INTO target_workspace USING reference_id;
    IF source_workspace IS NULL OR target_workspace IS NULL OR source_workspace<>target_workspace THEN
        RAISE EXCEPTION 'workspace resource not found' USING ERRCODE='23514',CONSTRAINT='workspace_resource_reference';
    END IF;
    RETURN NEW;
END;
$$;
DO $workspace_references$
DECLARE
    reference_spec TEXT[];
    trigger_name TEXT;
BEGIN
    FOREACH reference_spec SLICE 1 IN ARRAY ARRAY[
        ['accounts','workspace_id','','proxy_id','proxies'],
        ['accounts','workspace_id','','proxy_fallback_origin_id','proxies'],
        ['accounts','workspace_id','','parent_account_id','accounts'],
        ['proxies','workspace_id','','backup_proxy_id','proxies'],
        ['groups','workspace_id','','fallback_group_id','groups'],
        ['groups','workspace_id','','fallback_group_id_on_invalid_request','groups'],
        ['api_keys','workspace_id','','group_id','groups'],
        ['channel_monitors','workspace_id','','account_id','accounts'],
        ['channel_monitors','workspace_id','','template_id','channel_monitor_request_templates'],
        ['account_groups','account_id','accounts','group_id','groups'],
        ['channel_groups','channel_id','channels','group_id','groups']
    ] LOOP
        trigger_name := 'workspace_reference_' || reference_spec[4];
        EXECUTE format('DROP TRIGGER IF EXISTS %I ON %I',trigger_name,reference_spec[1]);
        EXECUTE format('CREATE TRIGGER %I BEFORE INSERT OR UPDATE ON %I FOR EACH ROW
            EXECUTE FUNCTION check_workspace_resource_reference(%L,%L,%L,%L)',
            trigger_name,reference_spec[1],reference_spec[2],reference_spec[3],reference_spec[4],reference_spec[5]);
    END LOOP;
END;
$workspace_references$;

CREATE OR REPLACE FUNCTION check_workspace_group_model_routing() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    routing_entry JSONB;
    account_identifier TEXT;
    account_workspace BIGINT;
BEGIN
    IF TG_OP='UPDATE' AND NEW.model_routing IS NOT DISTINCT FROM OLD.model_routing THEN RETURN NEW; END IF;
    FOR routing_entry IN SELECT value FROM jsonb_each(COALESCE(NEW.model_routing,'{}'::jsonb)) LOOP
        FOR account_identifier IN SELECT jsonb_array_elements_text(routing_entry) LOOP
            SELECT workspace_id INTO account_workspace FROM accounts WHERE id=account_identifier::BIGINT;
            IF account_workspace IS NULL OR account_workspace<>NEW.workspace_id THEN
                RAISE EXCEPTION 'workspace resource not found' USING ERRCODE='23514',CONSTRAINT='workspace_resource_reference';
            END IF;
        END LOOP;
    END LOOP;
    RETURN NEW;
END;
$$;
DROP TRIGGER IF EXISTS workspace_group_model_routing ON groups;
CREATE TRIGGER workspace_group_model_routing BEFORE INSERT OR UPDATE OF model_routing ON groups
    FOR EACH ROW EXECUTE FUNCTION check_workspace_group_model_routing();
