package repository

import (
	"context"
	"encoding/json"
	"errors"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *userSubscriptionRepository) PrepareBulkReceipt(ctx context.Context, scope, keyHash, fingerprint string, allowCreate bool) error {
	if dbent.TxFromContext(ctx) != nil {
		return errors.New("subscription receipt identity must precede item transactions")
	}
	if allowCreate {
		_, err := r.client.ExecContext(ctx, `
		INSERT INTO idempotency_records(scope,idempotency_key_hash,request_fingerprint,status,response_body,expires_at)
		VALUES($1,$2,$3,'processing','[]','infinity'::timestamptz)
		ON CONFLICT(scope,idempotency_key_hash) DO NOTHING`, scope, keyHash, fingerprint)
		if err != nil {
			return err
		}
	}
	var storedFingerprint string
	if err := scanSingleRow(ctx, r.client, `SELECT request_fingerprint FROM idempotency_records WHERE scope=$1 AND idempotency_key_hash=$2`, []any{scope, keyHash}, &storedFingerprint); err != nil {
		return err
	}
	if storedFingerprint != fingerprint {
		return service.ErrIdempotencyKeyConflict
	}
	return nil
}

func (r *userSubscriptionRepository) LockBulkReceipt(ctx context.Context, scope, keyHash, fingerprint string) (*service.SubscriptionBulkReceipt, error) {
	if dbent.TxFromContext(ctx) == nil {
		return nil, errors.New("subscription receipt requires an item transaction")
	}
	client := clientFromContext(ctx, r.client)
	receipt := &service.SubscriptionBulkReceipt{}
	var storedFingerprint, body string
	err := scanSingleRow(ctx, client, `
		SELECT id,request_fingerprint,response_body
		FROM idempotency_records
		WHERE scope=$1 AND idempotency_key_hash=$2
		FOR UPDATE`, []any{scope, keyHash}, &receipt.ID, &storedFingerprint, &body)
	if err != nil {
		return nil, err
	}
	if storedFingerprint != fingerprint {
		return nil, service.ErrIdempotencyKeyConflict
	}
	if err := json.Unmarshal([]byte(body), &receipt.Items); err != nil {
		return nil, err
	}
	return receipt, nil
}

func (r *userSubscriptionRepository) SaveBulkReceipt(ctx context.Context, receipt *service.SubscriptionBulkReceipt) error {
	if dbent.TxFromContext(ctx) == nil {
		return errors.New("subscription receipt requires an item transaction")
	}
	body, err := json.Marshal(receipt.Items)
	if err != nil {
		return err
	}
	status := service.IdempotencyStatusProcessing
	if receipt.Complete {
		status = service.IdempotencyStatusSucceeded
	}
	result, err := clientFromContext(ctx, r.client).ExecContext(ctx, `
		UPDATE idempotency_records
		SET response_body=$2,status=$3,updated_at=NOW()
		WHERE id=$1`, receipt.ID, string(body), status)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return errors.New("subscription receipt disappeared before commit")
	}
	return nil
}
