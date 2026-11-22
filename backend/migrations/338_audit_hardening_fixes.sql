-- Repair pre-337 installer/signup memberships even when a later user UPDATE
-- changed the user's xmin. Successful explicit membership writes remain independent.
UPDATE workspace_members AS m
SET inherits_global_role = true
FROM users AS u, workspaces AS w
WHERE m.user_id = u.id AND m.workspace_id = w.id AND w.is_default
  AND u.role = 'admin' AND u.deleted_at IS NULL
  AND m.role = 'admin' AND NOT m.inherits_global_role
  AND m.created_at = m.updated_at AND m.created_at > u.created_at
  AND m.created_at < (SELECT applied_at FROM schema_migrations WHERE filename = '337_default_workspace_role_source.sql')
  AND NOT EXISTS (
      SELECT 1 FROM audit_logs AS a
      WHERE a.method = 'PUT' AND a.path = '/api/v1/workspaces/:workspace_id/members/:user_id'
        AND a.status_code BETWEEN 200 AND 299
        AND ltrim(a.extra #>> '{params,workspace_id}', '0+') = m.workspace_id::text
        AND ltrim(a.extra #>> '{params,user_id}', '0+') = m.user_id::text
  );

CREATE INDEX IF NOT EXISTS idx_workspace_members_user_workspace
    ON workspace_members (user_id, workspace_id);
