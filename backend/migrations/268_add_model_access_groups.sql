-- Owner: model-access-groups. Rollback floor: code enforcing model access rules;
-- retain these tables and never turn an explicit empty policy into inheritance.
CREATE TABLE IF NOT EXISTS model_access_groups (
 id BIGSERIAL PRIMARY KEY,
 workspace_id BIGINT NOT NULL REFERENCES workspaces(id),
 name VARCHAR(100) NOT NULL,
 models JSONB NOT NULL CHECK (jsonb_typeof(models) = 'array'),
 revision BIGINT NOT NULL DEFAULT 1,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 UNIQUE(workspace_id, name), UNIQUE(id, workspace_id)
);
CREATE TABLE IF NOT EXISTS model_access_rules (
 id BIGSERIAL PRIMARY KEY,
 workspace_id BIGINT NOT NULL REFERENCES workspaces(id),
 account_id BIGINT UNIQUE REFERENCES accounts(id) ON DELETE CASCADE,
 api_key_id BIGINT UNIQUE REFERENCES api_keys(id) ON DELETE CASCADE,
 access_group_id BIGINT,
 models JSONB CHECK (models IS NULL OR jsonb_typeof(models) = 'array'),
 exclude JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(exclude) = 'array'),
 revision BIGINT NOT NULL DEFAULT 1,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 CHECK ((account_id IS NULL) <> (api_key_id IS NULL)),
 FOREIGN KEY(access_group_id,workspace_id) REFERENCES model_access_groups(id,workspace_id) ON DELETE RESTRICT
);
CREATE INDEX IF NOT EXISTS model_access_rules_group ON model_access_rules(access_group_id);
DROP TRIGGER IF EXISTS model_access_account_scope ON model_access_rules;
CREATE TRIGGER model_access_account_scope BEFORE INSERT OR UPDATE ON model_access_rules FOR EACH ROW EXECUTE FUNCTION check_workspace_resource_reference('workspace_id','','account_id','accounts');
DROP TRIGGER IF EXISTS model_access_key_scope ON model_access_rules;
CREATE TRIGGER model_access_key_scope BEFORE INSERT OR UPDATE ON model_access_rules FOR EACH ROW EXECUTE FUNCTION check_workspace_resource_reference('workspace_id','','api_key_id','api_keys');
CREATE OR REPLACE FUNCTION propagate_model_access_change() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 PERFORM advance_model_catalog_generation('catalog',jsonb_build_object('model_access',true,'deleted',TG_OP='DELETE'));
 RETURN COALESCE(NEW,OLD);
END;
$$;
DROP TRIGGER IF EXISTS model_access_change ON model_access_groups;
CREATE TRIGGER model_access_change AFTER INSERT OR UPDATE OR DELETE ON model_access_groups FOR EACH ROW EXECUTE FUNCTION propagate_model_access_change();
DROP TRIGGER IF EXISTS model_access_change ON model_access_rules;
CREATE TRIGGER model_access_change AFTER INSERT OR UPDATE OR DELETE ON model_access_rules FOR EACH ROW EXECUTE FUNCTION propagate_model_access_change();
