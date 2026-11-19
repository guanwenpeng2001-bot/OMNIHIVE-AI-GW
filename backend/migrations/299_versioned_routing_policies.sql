-- A single commit fence gives gateway outbox readers a gap-free high water mark.
CREATE TABLE IF NOT EXISTS routing_policy_clock (
 singleton boolean PRIMARY KEY CHECK (singleton),
 epoch bigint NOT NULL CHECK(epoch >= 0)
);
INSERT INTO routing_policy_clock VALUES (true,0) ON CONFLICT DO NOTHING;

CREATE TABLE IF NOT EXISTS routing_policies (
 id uuid PRIMARY KEY,
 workspace_id bigint REFERENCES workspaces(id),
 scope_type text NOT NULL, scope_id text NOT NULL,
 project_id bigint,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 draft_version uuid, active_version uuid, etag bigint NOT NULL DEFAULT 1,
 FOREIGN KEY(workspace_id,project_id) REFERENCES projects(workspace_id,id),
 CHECK (CASE scope_type
  WHEN 'platform' THEN workspace_id IS NULL AND scope_id='platform' AND project_id IS NULL
  WHEN 'workspace' THEN workspace_id IS NOT NULL AND scope_id=workspace_id::text AND project_id IS NULL
  WHEN 'project' THEN workspace_id IS NOT NULL AND project_id IS NOT NULL AND scope_id=project_id::text
  WHEN 'api_key' THEN workspace_id IS NOT NULL AND project_id IS NOT NULL AND scope_id ~ '^[1-9][0-9]*$'
  ELSE false END)
);
CREATE UNIQUE INDEX IF NOT EXISTS routing_policy_assignment ON routing_policies(coalesce(workspace_id,0),scope_type,scope_id);
CREATE INDEX IF NOT EXISTS routing_policy_list ON routing_policies(workspace_id,project_id,created_at,id);
CREATE TABLE IF NOT EXISTS routing_policy_versions (
 id uuid PRIMARY KEY, policy_id uuid NOT NULL REFERENCES routing_policies(id),
 kind text NOT NULL CHECK(kind IN ('draft','published')), base_version uuid,
 actor_id bigint NOT NULL, created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 definition jsonb NOT NULL, inherited_layers jsonb NOT NULL, change_note text NOT NULL,
 UNIQUE(policy_id,id), FOREIGN KEY(policy_id,base_version) REFERENCES routing_policy_versions(policy_id,id)
);
ALTER TABLE routing_policies DROP CONSTRAINT IF EXISTS routing_active_version,
 DROP CONSTRAINT IF EXISTS routing_draft_version,
 ADD CONSTRAINT routing_active_version FOREIGN KEY(id,active_version) REFERENCES routing_policy_versions(policy_id,id),
 ADD CONSTRAINT routing_draft_version FOREIGN KEY(id,draft_version) REFERENCES routing_policy_versions(policy_id,id);
CREATE INDEX IF NOT EXISTS routing_policy_history ON routing_policy_versions(policy_id,kind,created_at,id);
CREATE TABLE IF NOT EXISTS routing_policy_outbox (
 revocation_epoch bigint PRIMARY KEY, policy_id uuid NOT NULL, version uuid NOT NULL UNIQUE,
 published_at timestamptz NOT NULL, previous_version uuid, impact jsonb NOT NULL,
 FOREIGN KEY(policy_id,version) REFERENCES routing_policy_versions(policy_id,id)
);
CREATE TABLE IF NOT EXISTS routing_policy_receipts (
 workspace_id bigint NOT NULL, actor_id bigint NOT NULL, route text NOT NULL, request_key text NOT NULL,
 fingerprint bytea NOT NULL, response jsonb, PRIMARY KEY(workspace_id,actor_id,route,request_key)
);

-- Producer-approved read models. The simulator connects only to their reader.
CREATE TABLE IF NOT EXISTS routing_candidate_snapshots (
 id text PRIMARY KEY, workspace_id bigint NOT NULL, project_id bigint NOT NULL,
 actor_id bigint NOT NULL, key_id bigint, scope_version bigint NOT NULL,
 scenario_digest bytea NOT NULL,
 as_of timestamptz NOT NULL, expires_at timestamptz NOT NULL CHECK(expires_at>as_of),
 graph jsonb NOT NULL,
 FOREIGN KEY(workspace_id,project_id) REFERENCES projects(workspace_id,id),
 FOREIGN KEY(workspace_id,key_id) REFERENCES api_keys(workspace_id,id)
);
CREATE INDEX IF NOT EXISTS routing_snapshot_scope ON routing_candidate_snapshots(workspace_id,project_id,actor_id,coalesce(key_id,0),scope_version,scenario_digest,as_of DESC,id);
CREATE OR REPLACE FUNCTION routing_policy_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'immutable routing record' USING ERRCODE='23514'; END $$;
DROP TRIGGER IF EXISTS routing_policy_immutable_version ON routing_policy_versions;
CREATE TRIGGER routing_policy_immutable_version BEFORE UPDATE OR DELETE ON routing_policy_versions
 FOR EACH ROW EXECUTE FUNCTION routing_policy_immutable();
DROP TRIGGER IF EXISTS routing_policy_immutable_snapshot ON routing_candidate_snapshots;
CREATE TRIGGER routing_policy_immutable_snapshot BEFORE UPDATE ON routing_candidate_snapshots
 FOR EACH ROW EXECUTE FUNCTION routing_policy_immutable();

INSERT INTO identity_actions(action,category) VALUES ('keys.act_as','project') ON CONFLICT DO NOTHING;

-- Only explicitly reviewed ownership can bring a legacy account preference into a project.
CREATE TABLE IF NOT EXISTS routing_account_preference_owners (
 user_id bigint PRIMARY KEY REFERENCES users(id), workspace_id bigint NOT NULL, project_id bigint NOT NULL,
 reviewed_by bigint NOT NULL REFERENCES users(id), reviewed_at timestamptz NOT NULL DEFAULT now(),
 FOREIGN KEY(workspace_id,project_id) REFERENCES projects(workspace_id,id),
 FOREIGN KEY(workspace_id,user_id) REFERENCES workspace_members(workspace_id,user_id)
);
CREATE TABLE IF NOT EXISTS routing_legacy_intents (
 source_kind text NOT NULL, source_id bigint NOT NULL, workspace_id bigint NOT NULL, project_id bigint NOT NULL,
 format_version integer NOT NULL DEFAULT 1, fields jsonb NOT NULL,
 captured_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(source_kind,source_id),
 FOREIGN KEY(workspace_id,project_id) REFERENCES projects(workspace_id,id)
);
