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
 WHERE a.category IN `+grantCategories+` AND g.workspace_id=w.id AND g.user_id=u.id AND g.action=a.action AND
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
	return s.ReadLegacyKeyScope(ctx, w, key, owner, digest)
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
	return s.ReadProjectCredential(ctx, digest)
}

func (s *Store) Workspaces(ctx context.Context, actor int64, page identity.Page) ([]identity.Workspace, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT w.id,w.name,w.is_default`+membership+`WHERE `+memberFilter+` AND w.id>$2 AND w.created_at<=$3 ORDER BY w.id LIMIT $4`, actor, page.After, page.AsOf, page.Limit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []identity.Workspace{}
	for rows.Next() {
		var v identity.Workspace
		if err := rows.Scan(&v.ID, &v.Name, &v.IsDefault); err != nil {
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
	rows, err := s.db.QueryContext(ctx, `SELECT p.id,w.id,p.name,p.version,p.is_default`+membership+`JOIN projects p ON p.workspace_id=w.id
 WHERE w.id=$2 AND `+memberFilter+` AND `+visibleProject+` AND p.archived_at IS NULL AND p.id>$3 AND p.created_at<=$4 ORDER BY p.id LIMIT $5`, actor, w, page.After, page.AsOf, page.Limit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []identity.Project{}
	for rows.Next() {
		p := identity.Project{State: "active"}
		if err := rows.Scan(&p.ID, &p.WorkspaceID, &p.Name, &p.Version, &p.IsDefault); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) Project(ctx context.Context, actor, w, p int64) (identity.Project, error) {
	row := identity.Project{State: "active"}
	err := s.db.QueryRowContext(ctx, `SELECT p.id,w.id,p.name,p.version,p.is_default`+membership+`JOIN projects p ON p.workspace_id=w.id
 WHERE w.id=$2 AND `+memberFilter+` AND `+visibleProject+` AND p.id=$3 AND p.archived_at IS NULL`, actor, w, p).Scan(&row.ID, &row.WorkspaceID, &row.Name, &row.Version, &row.IsDefault)
	return row, found(err)
}

func (s *Store) Keys(ctx context.Context, actor, w, p int64, page identity.Page) ([]identity.ProjectKeyRow, error) {
	return s.ListProjectKeys(ctx, actor, w, p, page)
}
