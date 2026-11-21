package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	infraerrors "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/errors"
)

func (service *PaymentService) GetPublicOrderByResumeToken(ctx context.Context, token string) (*dbent.PaymentOrder, error) {
	claims, err := service.paymentResume().ParseToken(strings.TrimSpace(token))
	if err != nil {
		return nil, err
	}

	order, err := service.entClient.PaymentOrder.Get(ctx, claims.OrderID)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, infraerrors.NotFound("NOT_FOUND", "order not found")
		}
		return nil, fmt.Errorf("get order by resume token: %w", err)
	}
	if claims.UserID > 0 && order.UserID != claims.UserID {
		return nil, invalidResumeTokenMatchError()
	}
	snapshot := psOrderProviderSnapshot(order)
	orderProviderInstanceID := strings.TrimSpace(psStringValue(order.ProviderInstanceID))
	orderProviderKey := strings.TrimSpace(psStringValue(order.ProviderKey))
	if snapshot != nil {
		if snapshot.ProviderInstanceID != "" {
			orderProviderInstanceID = snapshot.ProviderInstanceID
		}
		if snapshot.ProviderKey != "" {
			orderProviderKey = snapshot.ProviderKey
		}
	}
	if claims.ProviderInstanceID != "" && orderProviderInstanceID != claims.ProviderInstanceID {
		return nil, invalidResumeTokenMatchError()
	}
	if claims.ProviderKey != "" && !strings.EqualFold(orderProviderKey, claims.ProviderKey) {
		return nil, invalidResumeTokenMatchError()
	}
	if claims.PaymentType != "" && NormalizeVisibleMethod(order.PaymentType) != NormalizeVisibleMethod(claims.PaymentType) {
		return nil, invalidResumeTokenMatchError()
	}

	// Keep the unique nonce claim uncommitted until the lookup succeeds. A
	// competing request cannot query upstream while this transaction owns the
	// key; rollback also releases it on transient failures or lost connections.
	transaction, err := service.entClient.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin resume consumption: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	digest := sha256.Sum256([]byte(claims.Nonce))
	fingerprint := hex.EncodeToString(digest[:])
	err = transaction.IdempotencyRecord.Create().
		SetScope("payment_resume").SetIdempotencyKeyHash(fingerprint).
		SetRequestFingerprint(fingerprint).SetStatus("succeeded").
		SetExpiresAt(time.Unix(claims.ExpiresAt, 0)).Exec(ctx)
	if dbent.IsConstraintError(err) {
		return nil, infraerrors.BadRequest("INVALID_RESUME_TOKEN", "resume token has already been consumed")
	}
	if err != nil {
		return nil, fmt.Errorf("claim payment resume token: %w", err)
	}
	if order.Status == OrderStatusPending || order.Status == OrderStatusExpired {
		result := service.reconcilePaid(ctx, order)
		if result == checkPaidResultAlreadyPaid {
			order, err = service.entClient.PaymentOrder.Get(ctx, order.ID)
			if err != nil {
				return nil, fmt.Errorf("reload order by resume token: %w", err)
			}
		}
	}

	if err := transaction.Commit(); err != nil {
		return nil, fmt.Errorf("finish resume consumption: %w", err)
	}
	return order, nil
}

// RenewPaymentResumeToken preserves the checkout's original expiry while
// issuing a distinct, single-use continuation for result-page polling.
func (s *PaymentService) RenewPaymentResumeToken(token string) (string, error) {
	claims, err := s.paymentResume().ParseToken(strings.TrimSpace(token))
	if err != nil {
		return "", err
	}
	return s.paymentResume().CreateToken(*claims)
}

func invalidResumeTokenMatchError() error {
	return infraerrors.BadRequest("INVALID_RESUME_TOKEN", "resume token does not match the payment order")
}

func (s *PaymentService) ParseWeChatPaymentResumeToken(token string) (*WeChatPaymentResumeClaims, error) {
	return s.paymentResume().ParseWeChatPaymentResumeToken(strings.TrimSpace(token))
}
