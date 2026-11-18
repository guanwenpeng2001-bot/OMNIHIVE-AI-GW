package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

// Audit action names for API key credential lifecycle events. They land on the
// existing audit channel; the diff records field presence only, never values.
const (
	auditActionAPIKeyCredentialRotate    = "api_key.credential.rotate"
	auditActionAPIKeyCredentialRevoke    = "api_key.credential.revoke"
	auditActionAPIKeyCredentialRevokeAll = "api_key.credential.revoke_all"
	auditActionAPIKeyCredentialExpire    = "api_key.credential.expire"

	// auditActorRoleSystem labels credential events that no interactive actor
	// triggered, such as a graceful credential reaching its deadline.
	auditActorRoleSystem = "system"
)

type apiKeyCredentialRepository struct {
	db *sql.DB
}

// NewAPIKeyCredentialRepository 创建 API Key 凭据仓储。
func NewAPIKeyCredentialRepository(db *sql.DB) service.APIKeyCredentialRepository {
	return &apiKeyCredentialRepository{db: db}
}

func (r *apiKeyCredentialRepository) ListDigestsByAPIKeyID(ctx context.Context, apiKeyID int64) ([]string, error) {
	if r == nil || r.db == nil {
		return nil, nil
	}
	return queryStringColumn(ctx, r.db, `
		SELECT credential_digest FROM api_key_credentials
		WHERE api_key_id = $1`, apiKeyID)
}

type stringColumnQueryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func queryStringColumn(ctx context.Context, db stringColumnQueryer, query string, args ...any) ([]string, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := make([]string, 0)
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, rows.Err()
}

func (r *apiKeyCredentialRepository) Rotate(ctx context.Context, rotation service.APIKeyCredentialRotation) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("nil api key credential repository")
	}
	newCredential := rotation.NewCredential
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC()
	if rotation.GraceUntil.IsZero() {
		rotation.GraceUntil = now
	}

	// Serialize concurrent rotations of the same key so at most one 'current'
	// credential can exist.
	var keyID int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM api_keys WHERE id = $1 FOR UPDATE`, rotation.APIKeyID).Scan(&keyID); err != nil {
		return err
	}

	// Retire any graceful credential older than the previous one: a key keeps at
	// most the current credential plus one bounded grace credential.
	if _, err := tx.ExecContext(ctx, `
		UPDATE api_key_credentials
		SET status = $2, revoked_at = $3, updated_at = $3
		WHERE api_key_id = $1 AND status = $4 AND revoked_at IS NULL`,
		rotation.APIKeyID, domain.CredentialStatusRevoked, now, domain.CredentialStatusGrace); err != nil {
		return err
	}

	if rotation.RevokePrevious {
		if _, err := tx.ExecContext(ctx, `
			UPDATE api_key_credentials
			SET status = $2, revoked_at = $3, updated_at = $3
			WHERE api_key_id = $1 AND status = $4 AND revoked_at IS NULL`,
			rotation.APIKeyID, domain.CredentialStatusRevoked, now, domain.CredentialStatusCurrent); err != nil {
			return err
		}
	} else {
		if _, err := tx.ExecContext(ctx, `
			UPDATE api_key_credentials
			SET status = $2, valid_until = $3, updated_at = $4
			WHERE api_key_id = $1 AND status = $5 AND revoked_at IS NULL`,
			rotation.APIKeyID, domain.CredentialStatusGrace, rotation.GraceUntil, now, domain.CredentialStatusCurrent); err != nil {
			return err
		}
	}

	workspaceID := rotation.WorkspaceID
	if workspaceID <= 0 {
		workspaceID = domain.DefaultWorkspaceID
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO api_key_credentials (workspace_id, api_key_id, credential_digest, display_suffix, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $6)`,
		workspaceID, rotation.APIKeyID, newCredential.Digest, newCredential.DisplaySuffix, domain.CredentialStatusCurrent, now); err != nil {
		return err
	}

	if err := insertAPIKeyCredentialAudit(ctx, tx, rotation.Actor, auditActionAPIKeyCredentialRotate, rotation.APIKeyID,
		map[string]any{
			"api_key_id":          rotation.APIKeyID,
			"grace_until":         rotation.GraceUntil.Unix(),
			"revoke_previous":     rotation.RevokePrevious,
			"previous_credential": "current",
			"new_credential":      "current",
		}); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *apiKeyCredentialRepository) RevokeGrace(ctx context.Context, apiKeyID int64, now time.Time, actor service.ConfigurationAuditActor) (int64, error) {
	if r == nil || r.db == nil {
		return 0, fmt.Errorf("nil api key credential repository")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, `
		UPDATE api_key_credentials
		SET status = $2, revoked_at = $3, updated_at = $3
		WHERE api_key_id = $1 AND status = $4 AND revoked_at IS NULL`,
		apiKeyID, domain.CredentialStatusRevoked, now, domain.CredentialStatusGrace)
	if err != nil {
		return 0, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if affected > 0 {
		if err := insertAPIKeyCredentialAudit(ctx, tx, actor, auditActionAPIKeyCredentialRevoke, apiKeyID,
			map[string]any{"api_key_id": apiKeyID, "revoked_credentials": affected}); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return affected, nil
}

func (r *apiKeyCredentialRepository) RevokeAll(ctx context.Context, apiKeyID int64, now time.Time, actor service.ConfigurationAuditActor) (int64, error) {
	if r == nil || r.db == nil {
		return 0, fmt.Errorf("nil api key credential repository")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, `
		UPDATE api_key_credentials
		SET status = $2, revoked_at = $3, updated_at = $3
		WHERE api_key_id = $1 AND revoked_at IS NULL`,
		apiKeyID, domain.CredentialStatusRevoked, now)
	if err != nil {
		return 0, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if affected > 0 {
		if err := insertAPIKeyCredentialAudit(ctx, tx, actor, auditActionAPIKeyCredentialRevokeAll, apiKeyID,
			map[string]any{"api_key_id": apiKeyID, "revoked_credentials": affected}); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return affected, nil
}

func insertAPIKeyCredentialAudit(ctx context.Context, exec auditLogExecer, actor service.ConfigurationAuditActor, action string, apiKeyID int64, extra map[string]any) error {
	extra["diff"] = service.ConfigurationDiff(
		map[string]any{"credential": "current"},
		map[string]any{"credential": "rotated"},
	)
	log := &service.AuditLog{
		CreatedAt:   time.Now().UTC(),
		ActorUserID: nullableActorID(actor.UserID),
		ActorRole:   actor.Role,
		Action:      action,
		Method:      "POST",
		Path:        fmt.Sprintf("/api/v1/api-keys/%d/credentials", apiKeyID),
		RequestID:   actor.RequestID,
		StatusCode:  200,
		Extra:       extra,
	}
	_, err := exec.ExecContext(ctx, `INSERT INTO audit_logs (`+auditLogInsertColumns+`) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`, auditLogInsertValues(log)...)
	return err
}

// auditLogExecer is the minimal execution surface the audit writer needs, so it can
// run inside an *sql.Tx, an Ent transaction client, or a plain connection.
type auditLogExecer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func nullableActorID(userID int64) *int64 {
	if userID <= 0 {
		return nil
	}
	return &userID
}
