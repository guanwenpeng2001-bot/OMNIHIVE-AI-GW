package service

import (
	"context"
	"errors"
	"log"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/util/logredact"
)

type idempotencyExecutionIdentityKey struct{}

type idempotencyExecutionIdentity struct {
	Scope       string
	KeyHash     string
	Fingerprint string
	NewClaim    bool
}

type bulkSubscriptionReceiptTxKey struct{}

type SubscriptionBulkReceiptItem struct {
	Result  BulkSubscriptionActionItemResult `json:"result"`
	UserID  int64                            `json:"user_id,omitempty"`
	GroupID int64                            `json:"group_id,omitempty"`
}

type SubscriptionBulkReceipt struct {
	ID       int64
	Items    []SubscriptionBulkReceiptItem
	Complete bool
}

type SubscriptionBulkReceiptRepository interface {
	PrepareBulkReceipt(ctx context.Context, scope, keyHash, fingerprint string, allowCreate bool) error
	LockBulkReceipt(ctx context.Context, scope, keyHash, fingerprint string) (*SubscriptionBulkReceipt, error)
	SaveBulkReceipt(ctx context.Context, receipt *SubscriptionBulkReceipt) error
}

func bulkSubscriptionCacheDeferred(ctx context.Context) bool {
	deferred, _ := ctx.Value(bulkSubscriptionReceiptTxKey{}).(bool)
	return deferred
}

func (s *SubscriptionService) applySubscriptionBulkAction(ctx context.Context, id int64, input *BulkSubscriptionActionInput) (*UserSubscription, error) {
	switch input.Action {
	case "extend":
		return s.ExtendSubscription(ctx, id, input.Days)
	case "reset_quota":
		return s.AdminResetQuota(ctx, id, input.Daily, input.Weekly, input.Monthly)
	case "revoke":
		subscription, err := s.userSubRepo.GetByID(ctx, id)
		if err != nil {
			return nil, err
		}
		return subscription, s.RevokeSubscription(ctx, id)
	case "restore":
		return s.RestoreSubscription(ctx, id)
	}
	return nil, ErrInvalidInput
}

func (s *SubscriptionService) bulkSubscriptionActionWithReceipts(ctx context.Context, input *BulkSubscriptionActionInput, store SubscriptionBulkReceiptRepository, identity idempotencyExecutionIdentity) (*BulkSubscriptionActionResult, error) {
	if s.entClient == nil || dbent.TxFromContext(ctx) != nil {
		return nil, ErrIdempotencyStoreUnavail
	}
	if err := store.PrepareBulkReceipt(ctx, identity.Scope+".receipt", identity.KeyHash, identity.Fingerprint, identity.NewClaim); err != nil {
		if errors.Is(err, ErrIdempotencyKeyConflict) {
			return nil, err
		}
		return nil, ErrIdempotencyStoreUnavail.WithCause(err)
	}
	ids := make([]int64, 0, len(input.SubscriptionIDs))
	seen := make(map[int64]bool, len(input.SubscriptionIDs))
	for _, id := range input.SubscriptionIDs {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	result := &BulkSubscriptionActionResult{Results: make([]BulkSubscriptionActionItemResult, 0, len(ids))}
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var item SubscriptionBulkReceiptItem
		err := s.withSubscriptionUpdateTx(ctx, func(txCtx context.Context) error {
			receipt, err := store.LockBulkReceipt(txCtx, identity.Scope+".receipt", identity.KeyHash, identity.Fingerprint)
			if err != nil {
				return err
			}
			for _, saved := range receipt.Items {
				if saved.Result.SubscriptionID == id {
					item = saved
					return nil
				}
			}
			transaction := dbent.TxFromContext(txCtx)
			if _, err := transaction.Client().ExecContext(txCtx, "SAVEPOINT bulk_subscription_item"); err != nil {
				return err
			}
			mutationCtx := context.WithValue(txCtx, bulkSubscriptionReceiptTxKey{}, true)
			changed, mutationErr := s.applySubscriptionBulkAction(mutationCtx, id, input)
			item.Result = BulkSubscriptionActionItemResult{SubscriptionID: id, Success: mutationErr == nil}
			if mutationErr != nil {
				if _, err := transaction.Client().ExecContext(txCtx, "ROLLBACK TO SAVEPOINT bulk_subscription_item"); err != nil {
					return err
				}
				item.Result.Error = infraerrors.Message(mutationErr)
				if errors.Is(mutationErr, context.Canceled) || errors.Is(mutationErr, context.DeadlineExceeded) {
					return mutationErr
				}
				if infraerrors.Code(mutationErr) >= 500 {
					log.Printf("[SubscriptionBulkReceipt] action=%s subscription_id=%d error=%s", input.Action, id, logredact.RedactText(mutationErr.Error()))
				}
			} else if changed != nil {
				item.UserID, item.GroupID = changed.UserID, changed.GroupID
			}
			if _, err := transaction.Client().ExecContext(txCtx, "RELEASE SAVEPOINT bulk_subscription_item"); err != nil {
				return err
			}
			receipt.Items = append(receipt.Items, item)
			receipt.Complete = len(receipt.Items) == len(ids)
			return store.SaveBulkReceipt(txCtx, receipt)
		})
		if err != nil {
			if errors.Is(err, ErrIdempotencyKeyConflict) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, err
			}
			return nil, ErrIdempotencyStoreUnavail.WithCause(err)
		}
		if item.Result.Success {
			result.SuccessCount++
			if item.UserID != 0 && item.GroupID != 0 {
				if err := s.invalidateSubscriptionCaches(item.UserID, item.GroupID); err != nil {
					log.Printf("[SubscriptionBulkReceipt] committed subscription_id=%d cache_error=%s", id, logredact.RedactText(err.Error()))
				}
			}
		} else {
			result.FailedCount++
		}
		result.Results = append(result.Results, item.Result)
	}
	return result, nil
}
