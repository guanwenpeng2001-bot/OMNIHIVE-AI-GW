package repository

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"strconv"

	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

type stagedImportReceiptRepository struct{}

func NewStagedImportReceiptRepository() service.StagedImportReceiptRepository {
	return &stagedImportReceiptRepository{}
}

func accountImportTxClient(ctx context.Context) (*dbent.Client, error) {
	tx := dbent.TxFromContext(ctx)
	if tx == nil {
		return nil, service.ErrStagedImportUnavailable
	}
	return tx.Client(), nil
}

func (r *stagedImportReceiptRepository) Lock(ctx context.Context, id service.StagedImportIdentity) error {
	client, err := accountImportTxClient(ctx)
	if err != nil {
		return err
	}
	// Transaction-local timeout also bounds waits on the unique constraint.
	if _, err = client.ExecContext(ctx, `SET LOCAL lock_timeout = '5s'`); err != nil {
		return err
	}
	// Shared by legacy mode reservation, staged mode reservation, and staged
	// account writes. Actor, payload and mode must never split this lock domain.
	key := sha256.Sum256([]byte("omnihive:account-import-key:v1:" + strconv.FormatInt(id.WorkspaceID, 10) + ":" + id.KeyHash))
	_, err = client.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, int64(binary.BigEndian.Uint64(key[:8])))
	return err
}

func (r *stagedImportReceiptRepository) Get(ctx context.Context, id service.StagedImportIdentity) (result *service.StagedImportReceipt, err error) {
	client, err := accountImportTxClient(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := client.QueryContext(ctx, `SELECT actor_id, receipt_version, sealed_receipt FROM staged_account_import_receipts WHERE workspace_id=$1 AND operation=$2 AND idempotency_key_hash=$3`, id.WorkspaceID, id.Operation, id.KeyHash)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); err == nil {
			err = closeErr
		}
	}()
	if !rows.Next() {
		err = rows.Err()
		return nil, err
	}
	var receipt service.StagedImportReceipt
	if err = rows.Scan(&receipt.ActorID, &receipt.Version, &receipt.Sealed); err != nil {
		return nil, err
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return &receipt, nil
}

func (r *stagedImportReceiptRepository) LegacyExists(ctx context.Context, id service.StagedImportIdentity) (bool, error) {
	_, exists, err := r.LegacyFingerprint(ctx, id)
	return exists, err
}

func (r *stagedImportReceiptRepository) LegacyFingerprint(ctx context.Context, id service.StagedImportIdentity) (fingerprint string, exists bool, err error) {
	client, err := accountImportTxClient(ctx)
	if err != nil {
		return "", false, err
	}
	scope := domain.WorkspaceActorScope(ctx, "admin.accounts.import_data")
	rows, err := client.QueryContext(ctx, `SELECT request_fingerprint FROM idempotency_records WHERE scope=$1 AND idempotency_key_hash=$2`, scope, id.KeyHash)
	if err != nil {
		return "", false, err
	}
	defer func() {
		if closeErr := rows.Close(); err == nil {
			err = closeErr
		}
	}()
	if !rows.Next() {
		err = rows.Err()
		return "", false, err
	}
	err = rows.Scan(&fingerprint)
	if err != nil {
		return "", false, err
	}
	err = rows.Err()
	return fingerprint, true, err
}

func (r *stagedImportReceiptRepository) Insert(ctx context.Context, id service.StagedImportIdentity, receipt service.StagedImportReceipt) error {
	if !service.IsStagedAccountImport(ctx) {
		return service.ErrStagedImportUnavailable
	}
	client, err := accountImportTxClient(ctx)
	if err != nil {
		return err
	}
	result, err := client.ExecContext(ctx, `INSERT INTO staged_account_import_receipts (workspace_id, operation, idempotency_key_hash, actor_id, receipt_version, sealed_receipt) VALUES ($1,$2,$3,$4,$5,$6)`, id.WorkspaceID, id.Operation, id.KeyHash, receipt.ActorID, receipt.Version, receipt.Sealed)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return errors.New("staged receipt insert did not persist one row")
	}
	return nil
}

func (r *stagedImportReceiptRepository) GetMode(ctx context.Context, id service.StagedImportIdentity) (result *service.AccountImportKeyMode, err error) {
	client, err := accountImportTxClient(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := client.QueryContext(ctx, `SELECT actor_id, import_mode FROM account_import_key_modes WHERE workspace_id=$1 AND idempotency_key_hash=$2`, id.WorkspaceID, id.KeyHash)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); err == nil {
			err = closeErr
		}
	}()
	if !rows.Next() {
		err = rows.Err()
		return nil, err
	}
	var mode service.AccountImportKeyMode
	if err = rows.Scan(&mode.ActorID, &mode.Mode); err != nil {
		return nil, err
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return &mode, nil
}

func (r *stagedImportReceiptRepository) InsertMode(ctx context.Context, id service.StagedImportIdentity, mode string) error {
	client, err := accountImportTxClient(ctx)
	if err != nil {
		return err
	}
	result, err := client.ExecContext(ctx, `INSERT INTO account_import_key_modes(workspace_id,idempotency_key_hash,actor_id,import_mode) VALUES($1,$2,$3,$4)`, id.WorkspaceID, id.KeyHash, id.ActorID, mode)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("import key mode insert did not persist one row")
	}
	return nil
}
