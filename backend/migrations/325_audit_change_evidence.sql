ALTER TABLE audit_logs ADD COLUMN project_id bigint;
ALTER TABLE audit_logs ADD COLUMN resource_id text;
CREATE INDEX audit_logs_project ON audit_logs(project_id,created_at DESC,id DESC) WHERE project_id IS NOT NULL;

ALTER TABLE scoped_audit_events ADD COLUMN request_id text;
ALTER TABLE scoped_audit_events ADD COLUMN route text;
ALTER TABLE scoped_audit_events ADD COLUMN method text;
ALTER TABLE scoped_audit_events ADD COLUMN actor_email text;
ALTER TABLE scoped_audit_events ADD COLUMN actor_role text;
ALTER TABLE scoped_audit_events ADD COLUMN status_code integer;
ALTER TABLE scoped_audit_events ADD COLUMN changes jsonb NOT NULL DEFAULT '[]'::jsonb CHECK(jsonb_typeof(changes)='array');
ALTER TABLE scoped_audit_events ADD COLUMN reason text;

ALTER TABLE scoped_audit_events DROP CONSTRAINT scoped_audit_events_resource_check;
ALTER TABLE scoped_audit_events DROP CONSTRAINT scoped_audit_events_resource_id_check;
ALTER TABLE scoped_audit_events DROP CONSTRAINT scoped_audit_events_action_check;
ALTER TABLE scoped_audit_events ADD CONSTRAINT scoped_audit_events_resource_id_check
 CHECK(resource_id IS NULL OR octet_length(resource_id) BETWEEN 1 AND 256);

-- Existing rows are intentionally not backfilled: their original values were
-- discarded by the old writer and cannot be reconstructed safely.
