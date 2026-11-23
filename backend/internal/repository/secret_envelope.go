package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

// BackupSecretPayloadRepository is SQL-only on purpose: 342 is an additive
// operational table and must be written in the same transaction as the legacy
// settings row. It is not an Ent entity and does not participate in auto-migrate.
type BackupSecretPayloadRepository struct{ db *sql.DB }

func NewBackupSecretPayloadRepository(db *sql.DB) *BackupSecretPayloadRepository {
	return &BackupSecretPayloadRepository{db: db}
}

func (r *BackupSecretPayloadRepository) Get(ctx context.Context, settingKey string) (*service.BackupSecretEnvelope, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("backup secret payload repository is unavailable")
	}
	var raw []byte
	err := r.db.QueryRowContext(ctx, `SELECT envelope FROM backup_secret_payloads WHERE setting_key = $1`, settingKey).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load backup secret envelope: %w", err)
	}
	var envelope service.BackupSecretEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("decode backup secret envelope: %w", err)
	}
	return &envelope, nil
}

func (r *BackupSecretPayloadRepository) Save(ctx context.Context, settingKey, legacySettingValue string, envelope service.BackupSecretEnvelope) error {
	if r == nil || r.db == nil {
		return errors.New("backup secret payload repository is unavailable")
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("encode backup secret envelope: %w", err)
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin backup secret save: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `
INSERT INTO settings (key, value, updated_at) VALUES ($1, $2, now())
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = EXCLUDED.updated_at`, settingKey, legacySettingValue); err != nil {
		return fmt.Errorf("save legacy backup secret: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO backup_secret_payloads (setting_key, envelope, key_id, updated_at)
VALUES ($1, $2::jsonb, $3, now())
ON CONFLICT (setting_key) DO UPDATE SET envelope = EXCLUDED.envelope, key_id = EXCLUDED.key_id, updated_at = EXCLUDED.updated_at`, settingKey, raw, envelope.KeyID); err != nil {
		return fmt.Errorf("save backup secret envelope: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO backup_secret_migrations (setting_key, state, key_id, updated_at)
VALUES ($1, 'migrated', $2, now())
ON CONFLICT (setting_key) DO UPDATE SET state = 'migrated', key_id = EXCLUDED.key_id, error_message = NULL, updated_at = EXCLUDED.updated_at`, settingKey, envelope.KeyID); err != nil {
		return fmt.Errorf("mark backup secret migrated: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit backup secret save: %w", err)
	}
	return nil
}

func (r *BackupSecretPayloadRepository) Delete(ctx context.Context, settingKey string) error {
	if r == nil || r.db == nil {
		return errors.New("backup secret payload repository is unavailable")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM backup_secret_payloads WHERE setting_key = $1`, settingKey); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM settings WHERE key = $1`, settingKey); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *BackupSecretPayloadRepository) SetMigrationState(ctx context.Context, settingKey, state, keyID, message string) error {
	if r == nil || r.db == nil {
		return errors.New("backup secret payload repository is unavailable")
	}
	_, err := r.db.ExecContext(ctx, `
INSERT INTO backup_secret_migrations (setting_key, state, key_id, error_message, updated_at)
VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), now())
ON CONFLICT (setting_key) DO UPDATE SET state = EXCLUDED.state, key_id = EXCLUDED.key_id, error_message = EXCLUDED.error_message, updated_at = EXCLUDED.updated_at`, settingKey, state, keyID, message)
	return err
}
