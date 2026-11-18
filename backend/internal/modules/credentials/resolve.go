package credentials

import (
	"context"
	"database/sql"
	"errors"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

type Queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func Resolve(ctx context.Context, q Queryer, encryptor service.SecretEncryptor, w, id, version int64) (map[string]any, error) {
	m := service.CredentialSecret{WorkspaceID: w, AccountID: id, Version: version}
	var cipher string
	var current int64
	err := q.QueryRowContext(ctx, `SELECT k.reference,k.ciphertext,k.revoked_at,k.version FROM credential_secrets k JOIN accounts a ON a.id=k.account_id AND a.workspace_id=k.workspace_id AND a.deleted_at IS NULL WHERE k.workspace_id=$1 AND k.account_id=$2`, w, id).Scan(&m.Reference, &cipher, &m.RevokedAt, &current)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fault.NotFound
	}
	if err != nil {
		return nil, err
	}
	if m.RevokedAt != nil {
		return nil, service.ErrCredentialSecretRevoked
	}
	if current != version {
		return nil, fault.Stale(current)
	}
	return service.ResolveCredentialSecret(ctx, encryptor, m, cipher)
}
