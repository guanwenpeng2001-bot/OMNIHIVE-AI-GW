package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

func backfillAccountSecrets(ctx context.Context, db *sql.DB, enc service.SecretEncryptor) error {
	for {
		n, err := backfillAccountSecretBatch(ctx, db, enc)
		if err != nil {
			return fmt.Errorf("account credential migration: %w", err)
		}
		if n == 0 {
			return nil
		}
	}
}

func backfillAccountSecretBatch(ctx context.Context, db *sql.DB, enc service.SecretEncryptor) (int, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	// Waiting on the first pending row ensures that no replica declares the job
	// complete while another replica is still migrating its final batch.
	rows, err := tx.QueryContext(ctx, `SELECT a.id,a.workspace_id,a.credentials FROM account_secret_backfill b JOIN accounts a ON a.id=b.account_id ORDER BY b.account_id LIMIT 64 FOR UPDATE OF b,a`)
	if err != nil {
		return 0, err
	}
	type account struct {
		id, workspace int64
		credentials   []byte
	}
	var batch []account
	for rows.Next() {
		var a account
		if err := rows.Scan(&a.id, &a.workspace, &a.credentials); err != nil {
			rows.Close()
			return 0, err
		}
		batch = append(batch, a)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	for _, a := range batch {
		var values map[string]any
		if err = json.Unmarshal(a.credentials, &values); err != nil {
			return 0, err
		}
		if _, referenced := service.CredentialReference(values); referenced {
			m, cipher, err := readCredentialSecret(ctx, tx, a.id)
			if err != nil {
				return 0, err
			}
			if m.RevokedAt == nil {
				secrets, err := service.ResolveCredentialSecret(ctx, enc, *m, cipher)
				if err != nil {
					return 0, err
				}
				if _, err = tx.ExecContext(ctx, `UPDATE credential_secrets SET api_key_digest=$2 WHERE account_id=$1`, a.id, service.AccountAPIKeyDigest(secrets)); err != nil {
					return 0, err
				}
			}
		} else if hasCredentialSecrets(values) {
			inline, err := createManagementQueryReference(ctx, tx, enc, a.id, a.workspace, values)
			if err != nil {
				return 0, err
			}
			if err = writeReferencedAccount(ctx, tx, a.id, inline); err != nil {
				return 0, err
			}
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM account_secret_backfill WHERE account_id=$1", a.id); err != nil {
			return 0, err
		}
	}
	return len(batch), tx.Commit()
}
