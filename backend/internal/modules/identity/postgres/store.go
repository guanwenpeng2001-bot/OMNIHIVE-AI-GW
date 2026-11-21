package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/scope"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	"github.com/lib/pq"
)

type Store struct {
	db      *sql.DB
	catalog catalog.CatalogReader
}

func New(db *sql.DB) *Store { return &Store{db: db} }

func NewWithCatalog(db *sql.DB, reader catalog.CatalogReader) *Store {
	return &Store{db: db, catalog: reader}
}

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func found(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return fault.NotFound
	}
	return err
}

const membership = ` FROM workspaces w JOIN users u ON u.id=$1 AND u.status='active' AND u.deleted_at IS NULL
 LEFT JOIN workspace_members m ON m.workspace_id=w.id AND m.user_id=u.id `
const memberFilter = `(u.role='admin' OR m.user_id IS NOT NULL)`
const administrator = `(u.role='admin' OR m.role='admin')`
const visibleProject = `(p.is_default OR ` + administrator + ` OR EXISTS (SELECT 1 FROM project_grants g WHERE g.workspace_id=w.id AND g.user_id=u.id AND g.project_id=p.id))`

func evaluate(ctx context.Context, q queryer, actor, w, p int64, action string) (identity.Access, error) {
	var a identity.Access
	err := q.QueryRowContext(ctx, `SELECT u.id,w.id,coalesce(p.id,0),w.scope_version,now()+interval '60 seconds',
 ARRAY(SELECT action FROM identity_actions a WHERE
 (a.category='member' OR u.role='admin' OR (`+administrator+` AND a.category<>'platform') OR
 EXISTS(SELECT 1 FROM project_grants g JOIN projects granted ON granted.workspace_id=g.workspace_id AND granted.id=g.project_id AND granted.archived_at IS NULL
 WHERE g.workspace_id=w.id AND g.user_id=u.id AND g.action=a.action AND
 (g.project_id=p.id OR ($3=0 AND a.category='project' AND
 (a.action LIKE 'connections.%' OR a.action LIKE 'pools.%' OR a.action LIKE 'credentials.%')))))
 ORDER BY action)`+membership+` LEFT JOIN projects p ON p.workspace_id=w.id AND p.id=$3 AND p.archived_at IS NULL
 WHERE w.id=$2 AND `+memberFilter+` AND ($3=0 OR (p.id IS NOT NULL AND `+visibleProject+`))`, actor, w, p).Scan(&a.ActorID, &a.WorkspaceID, &a.ProjectID, &a.Version, &a.ExpiresAt, pq.Array(&a.Actions))
	if err != nil {
		return a, found(err)
	}
	a.ExpiresAt = a.ExpiresAt.UTC()
	return a, a.Require(action)
}

func (s *Store) Evaluate(ctx context.Context, actor, w, p int64, action string) (identity.Access, error) {
	return evaluate(ctx, s.db, actor, w, p, action)
}

func (s *Store) RequireResource(ctx context.Context, actor, w, p int64, action, kind string, id int64) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = evaluate(ctx, tx, actor, w, p, action); err != nil {
		return err
	}
	var exists bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM identity_resource_owners o JOIN projects d ON d.workspace_id=o.workspace_id AND d.is_default
 LEFT JOIN project_resources r ON r.kind=o.kind AND r.resource_id=o.id AND r.workspace_id=o.workspace_id
 LEFT JOIN project_key_policies k ON k.api_key_id=o.key_id AND k.workspace_id=o.workspace_id
 WHERE o.kind=$1 AND o.id=$2 AND o.workspace_id=$3 AND coalesce(k.project_id,r.project_id,d.id)=$4 AND
 (SELECT count(*) FROM identity_resource_owners all_owners WHERE all_owners.kind=o.kind AND all_owners.id=o.id)=1 AND NOT EXISTS(SELECT 1 FROM identity_scope_quarantine q WHERE q.kind=o.kind AND q.resource_id=o.id))`, kind, id, w, p).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		return fault.NotFound
	}
	return tx.Commit()
}

func (s *Store) LegacyScope(ctx context.Context, w, key, owner int64, digest string) (scope.Authorized, error) {
	a := scope.Authorized{WorkspaceID: w, KeyID: key, ActorID: owner, KeyOwnerID: owner, FundingUserID: owner}
	err := s.db.QueryRowContext(ctx, `SELECT p.id,w.scope_version FROM api_keys k JOIN workspaces w ON w.id=k.workspace_id
 JOIN projects d ON d.workspace_id=w.id AND d.is_default
 LEFT JOIN project_resources r ON r.workspace_id=w.id AND r.kind='api_keys' AND r.resource_id=k.id
 JOIN projects p ON p.workspace_id=w.id AND p.id=coalesce(r.project_id,d.id) AND p.archived_at IS NULL
 WHERE k.id=$1 AND k.workspace_id=$2 AND k.user_id=$3 AND k.deleted_at IS NULL
 AND (p.is_default OR EXISTS (SELECT 1 FROM users u
     LEFT JOIN workspace_members m ON m.workspace_id=w.id AND m.user_id=u.id
     WHERE u.id=k.user_id AND u.deleted_at IS NULL AND u.status='active' AND `+legacyProjectPermission+`))
 AND EXISTS (SELECT 1 FROM api_key_credentials c WHERE c.api_key_id=k.id AND c.workspace_id=k.workspace_id AND c.credential_digest=$4 AND c.revoked_at IS NULL AND (c.status='current' OR (c.status='grace' AND (c.valid_until IS NULL OR c.valid_until>now())))) AND NOT EXISTS(SELECT 1 FROM project_key_policies x WHERE x.api_key_id=k.id)`, key, w, owner, digest).Scan(&a.ProjectID, &a.Version)
	return a, found(err)
}

// Legacy credentials carry all gateway actions. Only users who can issue
// such a key in this project may bind one there. Recheck this predicate at
// authentication so revoking membership or grants also revokes access.
const legacyProjectPermission = `(u.role='admin' OR m.role='admin' OR (m.id IS NOT NULL AND
 EXISTS (SELECT 1 FROM project_grants g WHERE g.workspace_id=p.workspace_id AND g.project_id=p.id
     AND g.user_id=u.id AND g.action='keys.create')
 AND (p.is_default OR NOT EXISTS (SELECT 1 FROM identity_actions a WHERE a.category='key'
     AND NOT EXISTS (SELECT 1 FROM project_grants g WHERE g.workspace_id=p.workspace_id
         AND g.project_id=p.id AND g.user_id=u.id AND g.action=a.action)))))`

// LegacyKeyWorkspace resolves an explicitly selected project for a group-bound
// user key, sharing the same project ownership checks as the model catalog.
func LegacyKeyWorkspace(ctx context.Context, db interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, actor, project, group int64) (int64, error) {
	rows, err := db.QueryContext(ctx, `SELECT p.workspace_id FROM projects p
 JOIN users u ON u.id=$1 AND u.deleted_at IS NULL AND u.status='active'
 LEFT JOIN workspace_members m ON m.workspace_id=p.workspace_id AND m.user_id=u.id
 JOIN groups b ON b.id=$3 AND b.workspace_id=p.workspace_id AND b.deleted_at IS NULL AND b.status='active'
 LEFT JOIN project_resources r ON r.kind='groups' AND r.resource_id=b.id AND r.workspace_id=p.workspace_id
 WHERE p.id=$2 AND p.archived_at IS NULL AND (r.project_id=p.id OR (r.project_id IS NULL AND p.is_default))
 AND NOT EXISTS (SELECT 1 FROM identity_scope_quarantine q WHERE q.kind='groups' AND q.resource_id=b.id)
 AND `+legacyProjectPermission, actor, project, group)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err = rows.Err(); err != nil {
			return 0, err
		}
		return 0, fault.NotFound
	}
	var workspace int64
	err = rows.Scan(&workspace)
	return workspace, err
}

func (s *Store) ProjectCredential(ctx context.Context, digest string) (*identity.KeyPolicy, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var p identity.KeyPolicy
	a := &p.Scope
	err = tx.QueryRowContext(ctx, `SELECT k.id,k.user_id,k.workspace_id,x.project_id,x.actions,x.model_mode,x.models,
 ARRAY(SELECT ug.group_id FROM user_allowed_groups ug WHERE ug.user_id=k.user_id),
 coalesce(k.group_id,0),coalesce((SELECT platform FROM groups WHERE id=k.group_id AND workspace_id=k.workspace_id AND deleted_at IS NULL),''),(SELECT r.rpm_override FROM user_group_rate_multipliers r WHERE r.user_id=k.user_id AND r.group_id=k.group_id)
 FROM api_key_credentials c JOIN project_key_policies x ON x.api_key_id=c.api_key_id AND x.workspace_id=c.workspace_id
 JOIN api_keys k ON k.id=x.api_key_id AND k.workspace_id=x.workspace_id AND k.deleted_at IS NULL
 WHERE (NOT x.member_bound OR x.issued_membership IS NOT NULL) AND c.credential_digest=$1 AND c.status='project' AND c.revoked_at IS NULL AND (c.valid_until IS NULL OR c.valid_until>now())`, digest).
		Scan(&a.KeyID, &a.KeyOwnerID, &a.WorkspaceID, &a.ProjectID, pq.Array(&p.Actions), &p.Mode, pq.Array(&p.Models), pq.Array(&p.AllowedGroups), &p.GroupID, &p.Platform, &p.GroupRPMOverride)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	access, err := evaluate(ctx, tx, a.KeyOwnerID, a.WorkspaceID, a.ProjectID, "")
	if err != nil {
		return nil, err
	}
	a.ActorID, a.FundingUserID, a.Version = a.KeyOwnerID, a.KeyOwnerID, access.Version
	var allowed []string
	for _, action := range p.Actions {
		if access.Require(action) == nil {
			allowed = append(allowed, action)
		}
	}
	p.Actions = allowed
	return &p, tx.Commit()
}

func (s *Store) Workspaces(ctx context.Context, actor int64, page identity.Page) ([]identity.Workspace, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT w.id,w.name`+membership+`WHERE `+memberFilter+` AND w.id>$2 AND w.created_at<=$3 ORDER BY w.id LIMIT $4`, actor, page.After, page.AsOf, page.Limit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []identity.Workspace{}
	for rows.Next() {
		var v identity.Workspace
		if err := rows.Scan(&v.ID, &v.Name); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) Projects(ctx context.Context, actor, w int64, page identity.Page) ([]identity.Project, error) {
	if _, err := s.Evaluate(ctx, actor, w, 0, "projects.read"); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT p.id,w.id,p.name,p.version`+membership+`JOIN projects p ON p.workspace_id=w.id
 WHERE w.id=$2 AND `+memberFilter+` AND `+visibleProject+` AND p.archived_at IS NULL AND p.id>$3 AND p.created_at<=$4 ORDER BY p.id LIMIT $5`, actor, w, page.After, page.AsOf, page.Limit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []identity.Project{}
	for rows.Next() {
		var p identity.Project
		if err := rows.Scan(&p.ID, &p.WorkspaceID, &p.Name, &p.Version); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) Project(ctx context.Context, actor, w, p int64) (identity.Project, error) {
	var row identity.Project
	err := s.db.QueryRowContext(ctx, `SELECT p.id,w.id,p.name,p.version`+membership+`JOIN projects p ON p.workspace_id=w.id
 WHERE w.id=$2 AND `+memberFilter+` AND `+visibleProject+` AND p.id=$3 AND p.archived_at IS NULL`, actor, w, p).Scan(&row.ID, &row.WorkspaceID, &row.Name, &row.Version)
	return row, found(err)
}

func (s *Store) Keys(ctx context.Context, actor, w, p int64, page identity.Page) ([]identity.Key, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err = evaluate(ctx, tx, actor, w, p, "keys.read"); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT k.id,k.name,CASE WHEN x.api_key_id IS NULL THEN '' ELSE 'oh_p_' END,k.user_id,1
 FROM api_keys k JOIN projects d ON d.workspace_id=k.workspace_id AND d.is_default
 LEFT JOIN project_key_policies x ON x.api_key_id=k.id AND x.workspace_id=k.workspace_id
 LEFT JOIN project_resources r ON r.resource_id=k.id AND r.kind='api_keys' AND r.workspace_id=k.workspace_id
 WHERE k.workspace_id=$1 AND coalesce(x.project_id,r.project_id,d.id)=$2 AND k.deleted_at IS NULL AND k.id>$3 AND k.created_at<=$4 ORDER BY k.id LIMIT $5`, w, p, page.After, page.AsOf, page.Limit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []identity.Key{}
	for rows.Next() {
		var k identity.Key
		if err := rows.Scan(&k.ID, &k.Name, &k.Prefix, &k.OwnerID, &k.Version); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return out, tx.Commit()
}
