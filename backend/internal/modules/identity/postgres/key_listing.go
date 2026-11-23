package postgres

import (
	"context"
	"database/sql"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
)

const projectKeyProjection = `SELECT k.id,k.name,CASE WHEN issued.api_key_id IS NULL THEN '' ELSE 'oh_p_' END,
 k.user_id,1,k.status,k.expires_at,k.group_id,
 CASE WHEN issued.api_key_id IS NULL THEN 'legacy_group' ELSE 'project_policy' END,
 now(),k.quota>0 AND k.quota_used>=k.quota,
 EXISTS (SELECT 1 FROM api_key_credentials c WHERE ` + identity.CurrentKeyCredential + `)
 FROM api_keys k` + identity.KeyAttributionJoins + `
 WHERE k.workspace_id=$1 AND key_project.id=$2 AND k.deleted_at IS NULL AND ` + identity.ValidKeyAttribution

type keyRowScanner interface {
	Scan(...any) error
}

func scanProjectKey(row keyRowScanner) (identity.ProjectKeyRow, error) {
	var k identity.ProjectKeyRow
	var now time.Time
	var expires sql.NullTime
	var exhausted, credential bool
	err := row.Scan(&k.ID, &k.Name, &k.Prefix, &k.OwnerID, &k.Version, &k.Status, &expires, &k.GroupID, &k.Kind, &now, &exhausted, &credential)
	if err != nil {
		return k, err
	}
	if expires.Valid {
		utc := expires.Time.UTC()
		k.ExpiresAt = &utc
	}
	k.State = identity.KeyLifecycle(k.Status, k.ExpiresAt, now, exhausted, credential)
	return k, nil
}

// ListProjectKeys takes its authorization and page from the same read snapshot.
func (s *Store) ListProjectKeys(ctx context.Context, actor, w, p int64, page identity.Page) ([]identity.ProjectKeyRow, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err = evaluate(ctx, tx, actor, w, p, "keys.read"); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, projectKeyProjection+` AND k.id>$3 AND k.created_at<=$4 ORDER BY k.id LIMIT $5`,
		w, p, page.After, page.AsOf, page.Limit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []identity.ProjectKeyRow{}
	for rows.Next() {
		key, err := scanProjectKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, key)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, tx.Commit()
}

// createProjectKeyOnce preserves the existing receipt format. The issuance
// closure keeps the one-time secret outside the value persisted in a receipt.
func createProjectKeyOnce(ctx context.Context, tx *sql.Tx, a identity.Access, token string, input identity.CreateKey, create func() (identity.Key, error)) (identity.ProjectKeyRow, error) {
	key, err := createOnce(ctx, tx, a, "POST keys", token, input, create)
	if err != nil {
		return identity.ProjectKeyRow{}, err
	}
	row, err := scanProjectKey(tx.QueryRowContext(ctx, projectKeyProjection+` AND k.id=$3`, a.WorkspaceID, a.ProjectID, key.ID))
	return row, found(err)
}
