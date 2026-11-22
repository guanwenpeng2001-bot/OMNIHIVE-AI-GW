-- Membership enables Default workspace/project discovery only. Project-scoped
-- keys, requests, billing, tasks and credentials can contain other users' data;
-- never seed project_grants for regular members of this shared workspace.
-- This is a one-time backfill, deliberately not a users INSERT trigger.
INSERT INTO workspace_members(workspace_id,user_id,role)
SELECT w.id,u.id,CASE WHEN u.role='admin' THEN 'admin' ELSE 'member' END
FROM workspaces w CROSS JOIN users u
WHERE w.id=1 AND w.is_default AND u.status='active' AND u.deleted_at IS NULL
  AND u.role IN ('admin','user')
ON CONFLICT(workspace_id,user_id) DO UPDATE
SET role='admin',updated_at=NOW()
WHERE EXCLUDED.role='admin' AND workspace_members.role<>'admin';
