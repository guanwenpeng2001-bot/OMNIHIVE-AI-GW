package requests

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/lib/pq"
)

type Export struct {
	Scope
	ID, RequestID string
	Sections      []string
	ExpiresAt     time.Time
}

func (s *Store) StartExport(ctx context.Context, scope Scope, actor int64, id, key string, sections []string) (Export, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Export{}, err
	}
	defer tx.Rollback()
	var exists int
	if err := tx.QueryRowContext(ctx, `
SELECT 1 FROM request_anchors WHERE workspace_id = $1 AND project_id = $2 AND id = $3`,
		scope.WorkspaceID, scope.ProjectID, id).Scan(&exists); err != nil {
		return Export{}, notFound(err)
	}

	digest := sha256.Sum256([]byte(key))
	out := Export{Scope: scope, RequestID: id, Sections: sections}
	err = tx.QueryRowContext(ctx, `
INSERT INTO request_metadata_exports (id, workspace_id, project_id, actor_id, request_id, key_hash, sections)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (workspace_id, project_id, actor_id, request_id, key_hash)
DO UPDATE SET key_hash = EXCLUDED.key_hash
WHERE request_metadata_exports.sections = EXCLUDED.sections
RETURNING id::text, expires_at`, uuid.NewString(), scope.WorkspaceID, scope.ProjectID, actor, id, digest[:], pq.Array(sections)).
		Scan(&out.ID, &out.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Export{}, fault.Conflict
	}
	if err != nil {
		return Export{}, err
	}
	out.ExpiresAt = out.ExpiresAt.UTC()
	return out, tx.Commit()
}

func (s *Store) ExportOperation(ctx context.Context, workspace, actor int64, id string) (Export, error) {
	return s.export(ctx, workspace, actor, id, nil, nil)
}

func (s *Store) ScopedExport(ctx context.Context, scope Scope, actor int64, requestID, id string) (Export, error) {
	return s.export(ctx, scope.WorkspaceID, actor, id, &scope.ProjectID, &requestID)
}

func (s *Store) export(ctx context.Context, workspace, actor int64, id string, project *int64, request *string) (Export, error) {
	out := Export{Scope: Scope{WorkspaceID: workspace}, ID: id}
	err := s.db.QueryRowContext(ctx, `
SELECT x.project_id, x.request_id, x.sections, x.expires_at
FROM request_metadata_exports x
WHERE x.workspace_id = $1 AND x.actor_id = $2 AND x.id = $3::uuid
  AND ($4::bigint IS NULL OR (x.project_id = $4 AND x.request_id = $5))
  AND EXISTS (SELECT 1 FROM request_anchors a
              WHERE a.workspace_id = x.workspace_id AND a.project_id = x.project_id AND a.id = x.request_id)`,
		workspace, actor, id, project, request).Scan(&out.ProjectID, &out.RequestID, pq.Array(&out.Sections), &out.ExpiresAt)
	out.ExpiresAt = out.ExpiresAt.UTC()
	return out, notFound(err)
}
