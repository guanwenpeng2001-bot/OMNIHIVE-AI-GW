ALTER TABLE workspaces ADD COLUMN IF NOT EXISTS scope_version bigint NOT NULL DEFAULT 1;
ALTER TABLE workspace_members ADD COLUMN IF NOT EXISTS grant_version bigint NOT NULL DEFAULT 1;

CREATE TABLE IF NOT EXISTS projects (
 id bigserial PRIMARY KEY, workspace_id bigint NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
 name varchar(100) NOT NULL CHECK (length(trim(name)) > 0), is_default boolean NOT NULL DEFAULT false,
 version bigint NOT NULL DEFAULT 1, created_at timestamptz NOT NULL DEFAULT now(), archived_at timestamptz,
 UNIQUE(workspace_id,id), CHECK (NOT is_default OR archived_at IS NULL)
);
CREATE UNIQUE INDEX IF NOT EXISTS projects_one_default ON projects(workspace_id) WHERE is_default;

CREATE OR REPLACE FUNCTION identity_default_project(w bigint) RETURNS bigint LANGUAGE plpgsql AS $$
DECLARE p bigint;
BEGIN
 SELECT id INTO p FROM projects WHERE workspace_id=w AND is_default;
 IF FOUND THEN RETURN p; END IF;
 INSERT INTO projects(workspace_id,name,is_default) VALUES (w,'Default',true)
 ON CONFLICT(workspace_id) WHERE is_default DO UPDATE SET is_default=true RETURNING id INTO p;
 RETURN p;
END $$;
CREATE OR REPLACE FUNCTION identity_seed_workspace() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN PERFORM identity_default_project(NEW.id); RETURN NEW; END $$;
DROP TRIGGER IF EXISTS identity_default_project ON workspaces;
CREATE TRIGGER identity_default_project AFTER INSERT ON workspaces FOR EACH ROW EXECUTE FUNCTION identity_seed_workspace();
INSERT INTO projects(workspace_id,name,is_default) SELECT id,'Default',true FROM workspaces
 ON CONFLICT(workspace_id) WHERE is_default DO NOTHING;

CREATE TABLE IF NOT EXISTS identity_actions (action text PRIMARY KEY, category text NOT NULL);
INSERT INTO identity_actions(action,category) VALUES
 ('projects.create','workspace'),
 ('members.grants.write','workspace'),
 ('projects.read','member'),
 ('models.read','member'),
 ('ops.restore.preview','platform'),
 ('ops.restore.execute','platform'),
 ('ops.incidents.publish','platform'),
 ('ops.export.configure','platform'),
 ('catalog.verify','platform'),
 ('extensions.read','platform'),
 ('projects.edit','project'),
 ('projects.delete','project'),
 ('keys.read','project'),
 ('keys.create','project'),
 ('credentials.byok.manage','project'),
 ('credentials.read_metadata','project'),
 ('credentials.rotate','project'),
 ('credentials.revoke','project'),
 ('credentials.write','project'),
 ('billing.adjust','project'),
 ('billing.budgets.write','project'),
 ('billing.export','project'),
 ('billing.ledger.read','project'),
 ('billing.read','project'),
 ('billing.reconcile.read','project'),
 ('billing.reconcile.write','project'),
 ('connections.activate','project'),
 ('connections.edit','project'),
 ('connections.read','project'),
 ('connections.verify','project'),
 ('playground.cancel','project'),
 ('playground.read','project'),
 ('playground.run','project'),
 ('pools.assign','project'),
 ('requests.content.read','project'),
 ('requests.export','project'),
 ('requests.read','project'),
 ('requests.replay','project'),
 ('routing.edit','project'),
 ('routing.publish','project'),
 ('routing.read','project'),
 ('routing.simulate','project'),
 ('tasks.artifacts.read','project'),
 ('tasks.cancel','project'),
 ('tasks.create','project'),
 ('tasks.retry','project'),
 ('operations.read','project'),
 ('text.chat','key'),
 ('text.embeddings','key'),
 ('image.generate','key'),
 ('usage.read','key'),
 ('tasks.read','key')
 ON CONFLICT(action) DO NOTHING;

CREATE TABLE IF NOT EXISTS project_grants (
 workspace_id bigint NOT NULL, user_id bigint NOT NULL, project_id bigint NOT NULL,
 action text NOT NULL REFERENCES identity_actions(action), PRIMARY KEY(workspace_id,user_id,project_id,action),
 FOREIGN KEY(workspace_id,user_id) REFERENCES workspace_members(workspace_id,user_id) ON DELETE CASCADE,
 FOREIGN KEY(workspace_id,project_id) REFERENCES projects(workspace_id,id) ON DELETE CASCADE
);
CREATE OR REPLACE FUNCTION identity_advance_scope_version() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 UPDATE workspaces SET scope_version=scope_version+1 WHERE id=COALESCE(NEW.workspace_id,OLD.workspace_id);
 RETURN NULL;
END $$;
DROP TRIGGER IF EXISTS identity_member_epoch ON workspace_members;
CREATE TRIGGER identity_member_epoch AFTER INSERT OR UPDATE OR DELETE ON workspace_members FOR EACH ROW EXECUTE FUNCTION identity_advance_scope_version();
DROP TRIGGER IF EXISTS identity_project_epoch ON projects;
CREATE TRIGGER identity_project_epoch AFTER INSERT OR UPDATE OF name,archived_at OR DELETE ON projects FOR EACH ROW EXECUTE FUNCTION identity_advance_scope_version();
DROP TRIGGER IF EXISTS identity_grant_epoch ON project_grants;
CREATE TRIGGER identity_grant_epoch AFTER INSERT OR UPDATE OR DELETE ON project_grants FOR EACH ROW EXECUTE FUNCTION identity_advance_scope_version();

CREATE TABLE IF NOT EXISTS project_resources (
 kind text NOT NULL, resource_id bigint NOT NULL, workspace_id bigint NOT NULL, project_id bigint NOT NULL,
 PRIMARY KEY(kind,resource_id), FOREIGN KEY(workspace_id,project_id) REFERENCES projects(workspace_id,id)
);
CREATE INDEX IF NOT EXISTS project_resources_lookup ON project_resources(workspace_id,project_id,kind,resource_id);
CREATE UNIQUE INDEX IF NOT EXISTS api_keys_workspace_identity ON api_keys(workspace_id,id);
CREATE UNIQUE INDEX IF NOT EXISTS workspace_members_workspace_identity ON workspace_members(workspace_id,id);
CREATE TABLE IF NOT EXISTS project_key_policies (
 api_key_id bigint PRIMARY KEY, workspace_id bigint NOT NULL, project_id bigint NOT NULL,
 member_bound boolean NOT NULL, issued_membership bigint REFERENCES workspace_members(id) ON DELETE SET NULL,
 actions text[] NOT NULL CHECK(cardinality(actions)>0),
 model_mode text NOT NULL CHECK(model_mode IN ('inherit','allow_list','deny_all')), models text[] NOT NULL,
 FOREIGN KEY(workspace_id,issued_membership) REFERENCES workspace_members(workspace_id,id) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(workspace_id,api_key_id) REFERENCES api_keys(workspace_id,id) ON DELETE CASCADE,
 FOREIGN KEY(workspace_id,project_id) REFERENCES projects(workspace_id,id)
);
CREATE OR REPLACE FUNCTION identity_guard_project_credential() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.status IN ('current','grace') AND EXISTS(SELECT 1 FROM project_key_policies WHERE api_key_id=NEW.api_key_id) THEN
  RAISE EXCEPTION 'restricted key requires a project credential' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS identity_project_credential ON api_key_credentials;
CREATE TRIGGER identity_project_credential BEFORE INSERT OR UPDATE ON api_key_credentials FOR EACH ROW EXECUTE FUNCTION identity_guard_project_credential();

CREATE TABLE IF NOT EXISTS identity_create_receipts (
 workspace_id bigint NOT NULL REFERENCES workspaces(id), project_scope bigint NOT NULL, actor_id bigint NOT NULL,
 route text NOT NULL, request_key text NOT NULL, fingerprint bytea NOT NULL, result jsonb,
 PRIMARY KEY(workspace_id,project_scope,actor_id,route,request_key)
);
CREATE TABLE IF NOT EXISTS identity_backfill_checkpoints (kind text PRIMARY KEY, last_id bigint NOT NULL DEFAULT 0);
CREATE TABLE IF NOT EXISTS identity_scope_quarantine (kind text NOT NULL, resource_id bigint NOT NULL, reason text NOT NULL, PRIMARY KEY(kind,resource_id));

CREATE OR REPLACE VIEW identity_resource_owners AS
SELECT 'accounts'::text AS kind, r.id, r.workspace_id, NULL::bigint AS key_id FROM accounts r
UNION ALL
SELECT 'groups'::text AS kind, r.id, r.workspace_id, NULL::bigint AS key_id FROM groups r
UNION ALL
SELECT 'channels'::text AS kind, r.id, r.workspace_id, NULL::bigint AS key_id FROM channels r
UNION ALL
SELECT 'proxies'::text AS kind, r.id, r.workspace_id, NULL::bigint AS key_id FROM proxies r
UNION ALL
SELECT 'proxy_groups'::text AS kind, r.id, r.workspace_id, NULL::bigint AS key_id FROM proxy_groups r
UNION ALL
SELECT 'api_keys'::text AS kind, r.id, r.workspace_id, r.id AS key_id FROM api_keys r
UNION ALL
SELECT 'channel_monitors'::text AS kind, r.id, r.workspace_id, NULL::bigint AS key_id FROM channel_monitors r
UNION ALL
SELECT 'channel_monitor_request_templates'::text AS kind, r.id, r.workspace_id, NULL::bigint AS key_id FROM channel_monitor_request_templates r
UNION ALL
SELECT 'model_access_groups'::text AS kind, r.id, r.workspace_id, NULL::bigint AS key_id FROM model_access_groups r
UNION ALL
SELECT 'scheduled_test_plans', r.id, parent.workspace_id, NULL FROM scheduled_test_plans r LEFT JOIN accounts parent ON parent.id=r.account_id
UNION ALL
SELECT 'composite_model_routes', r.id, parent.workspace_id, NULL FROM composite_model_routes r LEFT JOIN groups parent ON parent.id=r.group_id
UNION ALL
SELECT 'upstream_jobs', r.id, k.workspace_id, k.id FROM upstream_jobs r LEFT JOIN api_keys k ON k.id=r.api_key_id AND k.user_id=r.user_id AND k.workspace_id=r.workspace_id AND (r.account_id IS NULL OR EXISTS (SELECT 1 FROM accounts a WHERE a.id=r.account_id AND a.workspace_id=k.workspace_id))
UNION ALL
SELECT 'usage_logs', r.id, k.workspace_id, k.id FROM usage_logs r LEFT JOIN api_keys k ON k.id=r.api_key_id AND k.user_id=r.user_id AND (r.account_id IS NULL OR EXISTS (SELECT 1 FROM accounts a WHERE a.id=r.account_id AND a.workspace_id=k.workspace_id))
UNION ALL
SELECT 'batch_image_jobs', r.id, k.workspace_id, k.id FROM batch_image_jobs r LEFT JOIN api_keys k ON k.id=r.api_key_id AND k.user_id=r.user_id AND (r.account_id IS NULL OR EXISTS (SELECT 1 FROM accounts a WHERE a.id=r.account_id AND a.workspace_id=k.workspace_id))
UNION ALL
SELECT 'usage_settlement_receipts', r.id, k.workspace_id, k.id FROM usage_settlement_receipts r LEFT JOIN api_keys k ON k.id=r.api_key_id AND k.user_id=r.user_id AND (r.account_id IS NULL OR EXISTS (SELECT 1 FROM accounts a WHERE a.id=r.account_id AND a.workspace_id=k.workspace_id))
UNION ALL
SELECT 'prompt_audit_jobs', r.id, k.workspace_id, k.id FROM prompt_audit_jobs r LEFT JOIN api_keys k ON k.id=r.api_key_id AND k.user_id=r.user_id AND (r.group_id IS NULL OR EXISTS (SELECT 1 FROM groups g WHERE g.id=r.group_id AND g.workspace_id=k.workspace_id))
UNION ALL
SELECT 'prompt_audit_events', r.id, k.workspace_id, k.id FROM prompt_audit_events r LEFT JOIN api_keys k ON k.id=r.api_key_id AND k.user_id=r.user_id AND (r.group_id IS NULL OR EXISTS (SELECT 1 FROM groups g WHERE g.id=r.group_id AND g.workspace_id=k.workspace_id));

CREATE OR REPLACE FUNCTION identity_check_resource_owner() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE owner_workspace bigint; owner_count bigint;
BEGIN
 SELECT count(*),min(workspace_id) INTO owner_count,owner_workspace FROM identity_resource_owners WHERE kind=NEW.kind AND id=NEW.resource_id;
 IF owner_count<>1 OR owner_workspace IS DISTINCT FROM NEW.workspace_id THEN
  RAISE EXCEPTION 'project resource ownership is unproven' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS identity_resource_owner ON project_resources;
CREATE TRIGGER identity_resource_owner BEFORE INSERT OR UPDATE ON project_resources FOR EACH ROW EXECUTE FUNCTION identity_check_resource_owner();

INSERT INTO identity_backfill_checkpoints(kind) VALUES
 ('accounts'), ('groups'), ('channels'), ('proxies'), ('proxy_groups'), ('api_keys'), ('channel_monitors'), ('channel_monitor_request_templates'), ('model_access_groups'), ('scheduled_test_plans'), ('composite_model_routes'), ('upstream_jobs'), ('usage_logs'), ('batch_image_jobs'), ('usage_settlement_receipts'), ('prompt_audit_jobs'), ('prompt_audit_events') ON CONFLICT DO NOTHING;

CREATE OR REPLACE FUNCTION identity_backfill_batch(resource_kind text,batch_size integer) RETURNS integer LANGUAGE plpgsql AS $$
DECLARE last_seen bigint; first_gap bigint; processed integer; highest bigint;
BEGIN
 IF batch_size NOT BETWEEN 1 AND 10000 THEN RAISE EXCEPTION 'invalid batch size'; END IF;
 SELECT last_id INTO STRICT last_seen FROM identity_backfill_checkpoints WHERE kind=resource_kind FOR UPDATE;
 -- Sequence IDs can be committed out of order by older writers.
 IF last_seen>0 AND NOT EXISTS(SELECT 1 FROM identity_resource_owners WHERE kind=resource_kind AND id>last_seen) THEN
  SELECT min(o.id) INTO first_gap FROM identity_resource_owners o
  WHERE o.kind=resource_kind AND o.id<=last_seen
    AND NOT EXISTS(SELECT 1 FROM project_resources r WHERE r.kind=o.kind AND r.resource_id=o.id)
    AND NOT EXISTS(SELECT 1 FROM identity_scope_quarantine q WHERE q.kind=o.kind AND q.resource_id=o.id);
  IF first_gap IS NOT NULL THEN last_seen:=first_gap-1; END IF;
 END IF;
 WITH ids AS MATERIALIZED (
  SELECT DISTINCT o.id FROM identity_resource_owners o WHERE o.kind=resource_kind AND o.id>last_seen ORDER BY o.id LIMIT batch_size
 ), batch AS MATERIALIZED (
  SELECT resource_kind AS kind,ids.id,
    CASE WHEN count(*)=1 THEN min(p.workspace_id) END AS workspace_id,
    CASE WHEN count(*)=1 THEN min(coalesce(k.project_id,p.id)) END AS project_id
  FROM ids JOIN identity_resource_owners o ON o.id=ids.id AND o.kind=resource_kind
  LEFT JOIN projects p ON p.workspace_id=o.workspace_id AND p.is_default
  LEFT JOIN project_key_policies k ON k.api_key_id=o.key_id AND k.workspace_id=p.workspace_id GROUP BY ids.id
 ), mapped AS (
  INSERT INTO project_resources(kind,resource_id,workspace_id,project_id)
  SELECT kind,id,workspace_id,project_id FROM batch WHERE project_id IS NOT NULL ON CONFLICT DO NOTHING
 ), quarantine AS (
  INSERT INTO identity_scope_quarantine(kind,resource_id,reason)
  SELECT kind,id,'unproven_owner' FROM batch WHERE project_id IS NULL ON CONFLICT DO NOTHING
 )
 SELECT count(*),max(id) INTO processed,highest FROM batch;
 IF processed>0 THEN UPDATE identity_backfill_checkpoints SET last_id=highest WHERE kind=resource_kind; END IF;
 RETURN processed;
END $$;
