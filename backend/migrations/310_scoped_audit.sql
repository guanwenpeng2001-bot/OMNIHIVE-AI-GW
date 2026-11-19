INSERT INTO identity_actions(action,category) VALUES
 ('audit.read','project'),('audit.export','project'),('audit.retention.write','project')
ON CONFLICT DO NOTHING;

-- Store only typed metadata. Source IDs are server-generated and contain no free text.
CREATE TABLE scoped_audit_events (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 workspace_id bigint NOT NULL, project_id bigint NOT NULL, actor_id bigint CHECK(actor_id>0),
 resource text NOT NULL CHECK(resource IN ('request_export','artifact','billing_command','retention','audit_event','project')),
 resource_id text CHECK(resource_id ~ '^(?:[1-9][0-9]{0,18}|[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})$'), action text NOT NULL CHECK(action IN
 ('requests.export','tasks.artifacts.revoke','billing.adjust','billing.reconcile','audit.retention.set','audit.retention.purge','audit.export','scope.read','scope.write')),
 effect text NOT NULL CHECK(effect IN ('committed','observed')),
 at timestamptz NOT NULL DEFAULT clock_timestamp(), affected bigint CHECK(affected>=0),
 -- An indefinite hold is the conservative default for financial effects.
 evidence_until timestamptz,
 CHECK(action NOT IN ('billing.adjust','billing.reconcile') OR evidence_until IS NOT NULL),
 FOREIGN KEY(workspace_id,project_id) REFERENCES projects(workspace_id,id),
 UNIQUE(workspace_id,project_id,id)
);
CREATE INDEX scoped_audit_seek ON scoped_audit_events(workspace_id,project_id,at DESC,id DESC);
CREATE TABLE scoped_audit_retention (
 workspace_id bigint NOT NULL, project_id bigint NOT NULL, days integer NOT NULL CHECK(days BETWEEN 1 AND 3650),
 trace_days integer NOT NULL CHECK(trace_days BETWEEN 1 AND 3650), content_hours integer NOT NULL CHECK(content_hours BETWEEN 0 AND 24),
 PRIMARY KEY(workspace_id,project_id), FOREIGN KEY(workspace_id,project_id) REFERENCES projects(workspace_id,id)
);
CREATE TABLE scoped_audit_exports (
 id uuid PRIMARY KEY, workspace_id bigint NOT NULL, project_id bigint NOT NULL, actor_id bigint NOT NULL,
 event_id bigint NOT NULL, key_hash bytea NOT NULL CHECK(octet_length(key_hash)=32),
 expires_at timestamptz NOT NULL DEFAULT now()+interval '15 minutes',
 UNIQUE(workspace_id,project_id,actor_id,event_id,key_hash),
 FOREIGN KEY(workspace_id,project_id,event_id) REFERENCES scoped_audit_events(workspace_id,project_id,id) ON DELETE CASCADE
);
CREATE INDEX scoped_audit_export_expiry ON scoped_audit_exports(expires_at);

CREATE FUNCTION scoped_audit_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP <> 'DELETE' OR OLD.evidence_until > clock_timestamp() THEN
  RAISE EXCEPTION 'audit evidence is immutable or held';
 END IF;
 RETURN OLD;
END $$;
CREATE TRIGGER scoped_audit_history_guard BEFORE UPDATE OR DELETE ON scoped_audit_events
 FOR EACH ROW EXECUTE FUNCTION scoped_audit_guard();
CREATE TRIGGER scoped_audit_no_truncate BEFORE TRUNCATE ON scoped_audit_events
 FOR EACH STATEMENT EXECUTE FUNCTION billing_reject_history_change();

-- Each source and its redacted effect commit together, including during mixed-version writes.
CREATE FUNCTION scoped_audit_effect() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_TABLE_NAME='request_metadata_exports' THEN
  INSERT INTO scoped_audit_events(workspace_id,project_id,actor_id,resource,resource_id,action,effect,at)
  VALUES(NEW.workspace_id,NEW.project_id,NEW.actor_id,'request_export',NEW.id,'requests.export','committed',NEW.created_at);
 ELSIF TG_TABLE_NAME='image_artifact_revocations' THEN
  INSERT INTO scoped_audit_events(workspace_id,project_id,actor_id,resource,resource_id,action,effect,at)
  SELECT t.workspace_id,t.project_id,NEW.actor_id,'artifact',md5(jsonb_build_array(NEW.task_id,NEW.artifact_id)::text)::uuid::text,'tasks.artifacts.revoke','committed',NEW.revoked_at
  FROM image_tasks t WHERE t.id=NEW.task_id;
 ELSE
  INSERT INTO scoped_audit_events(workspace_id,project_id,actor_id,resource,resource_id,action,effect,at,evidence_until)
  SELECT c.workspace_id,c.project_id,c.actor_id,'billing_command',c.id,TG_ARGV[0],'committed',clock_timestamp(),'infinity'
  FROM billing_commands c WHERE (c.workspace_id,c.project_id,c.id)=(NEW.workspace_id,NEW.project_id,NEW.id);
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER scoped_audit_request_export AFTER INSERT ON request_metadata_exports FOR EACH ROW EXECUTE FUNCTION scoped_audit_effect();
CREATE TRIGGER scoped_audit_artifact_revoke AFTER INSERT ON image_artifact_revocations FOR EACH ROW EXECUTE FUNCTION scoped_audit_effect();
CREATE TRIGGER scoped_audit_adjust AFTER INSERT ON billing_adjustment_effects FOR EACH ROW EXECUTE FUNCTION scoped_audit_effect('billing.adjust');
CREATE TRIGGER scoped_audit_reconcile AFTER INSERT ON billing_case_resolutions FOR EACH ROW EXECUTE FUNCTION scoped_audit_effect('billing.reconcile');

INSERT INTO scoped_audit_events(workspace_id,project_id,actor_id,resource,resource_id,action,effect,at)
SELECT workspace_id,project_id,actor_id,'request_export',id,'requests.export','committed',created_at FROM request_metadata_exports;
INSERT INTO scoped_audit_events(workspace_id,project_id,actor_id,resource,resource_id,action,effect,at)
SELECT t.workspace_id,t.project_id,r.actor_id,'artifact',md5(jsonb_build_array(r.task_id,r.artifact_id)::text)::uuid::text,'tasks.artifacts.revoke','committed',r.revoked_at
 FROM image_artifact_revocations r JOIN image_tasks t ON t.id=r.task_id;
INSERT INTO scoped_audit_events(workspace_id,project_id,actor_id,resource,resource_id,action,effect,at,evidence_until)
SELECT c.workspace_id,c.project_id,c.actor_id,'billing_command',c.id,f.action,'committed',f.at,'infinity'
 FROM (SELECT workspace_id,project_id,id,'billing.adjust' AS action,posted_at AS at FROM billing_adjustment_effects
 UNION ALL SELECT workspace_id,project_id,id,'billing.reconcile',resolved_at FROM billing_case_resolutions) f
 JOIN billing_commands c USING(workspace_id,project_id,id);

-- A shorter scoped TTL applies at creation and to existing retry input. The
-- worker subsequently erases the ciphertext in bounded, audited batches.
CREATE FUNCTION scoped_audit_cap_replay_input() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE hours integer;
BEGIN
 SELECT content_hours INTO hours FROM scoped_audit_retention WHERE workspace_id=NEW.workspace_id AND project_id=NEW.project_id;
 NEW.input_until := least(NEW.input_until,NEW.created_at+make_interval(hours=>coalesce(hours,24)));
 RETURN NEW;
END $$;
CREATE TRIGGER scoped_audit_cap_replay_input BEFORE INSERT ON image_tasks
 FOR EACH ROW WHEN (NEW.replay_input IS NOT NULL AND NEW.input_until IS NOT NULL) EXECUTE FUNCTION scoped_audit_cap_replay_input();
