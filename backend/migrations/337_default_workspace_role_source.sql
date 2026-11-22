-- Manual workspace assignments are independent of the user's global role.
ALTER TABLE workspace_members ADD COLUMN IF NOT EXISTS inherits_global_role boolean NOT NULL DEFAULT false;

-- 334 and its migration receipt used NOW() in the same transaction. Both
-- inserted memberships and existing memberships promoted by 334 have this
-- timestamp. A subsequent explicit role write has a different updated_at.
-- Old installer and signup memberships were created alongside the user.
UPDATE workspace_members m
SET inherits_global_role=true, role=CASE WHEN u.role='admin' THEN 'admin' ELSE 'member' END
FROM users u
WHERE m.user_id=u.id AND m.workspace_id=1 AND NOT m.inherits_global_role
  AND (m.updated_at=(SELECT applied_at FROM schema_migrations WHERE filename='334_backfill_default_workspace_members.sql')
       OR (u.role='admin' AND m.xmin=u.xmin AND m.created_at=m.updated_at AND m.created_at>u.created_at));

DO $$
BEGIN
 IF NOT EXISTS (SELECT FROM workspaces WHERE is_default)
    AND EXISTS (SELECT FROM users WHERE status='active' AND deleted_at IS NULL AND role IN ('admin','user')) THEN
  RAISE EXCEPTION 'Default workspace is missing; restore it before backfilling memberships';
 END IF;
END $$;

-- 334 selected only workspace 1. Complete the backfill for restored defaults.
INSERT INTO workspace_members(workspace_id,user_id,role,inherits_global_role)
SELECT w.id,u.id,CASE WHEN u.role='admin' THEN 'admin' ELSE 'member' END,true
FROM (SELECT id FROM workspaces WHERE is_default ORDER BY (id=1) DESC,id LIMIT 1) w
CROSS JOIN users u
WHERE w.id<>1 AND u.status='active' AND u.deleted_at IS NULL AND u.role IN ('admin','user')
ON CONFLICT(workspace_id,user_id) DO NOTHING;
