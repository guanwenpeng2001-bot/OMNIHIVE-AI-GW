package service

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
)

const UsageSettlementRecoveryBoundary = "Recovery starts only after the first receipt commits; process death or database failure before that point can lose upstream usage and requires manual review."

// UsageSettlementService queues only persisted identities. It has no upstream
// client or price resolver: recovery cannot perform inference or reprice usage.
type UsageSettlementService struct {
	repo                 UsageSettlementReceiptRepository
	pool                 *UsageRecordWorkerPool
	cache                *BillingCacheService
	authCacheInvalidator APIKeyAuthCacheInvalidator
	cancel               context.CancelFunc
	wg                   sync.WaitGroup
	stop                 sync.Once
	persistFailures      atomic.Uint64
}

func (s *UsageSettlementService) Status(ctx context.Context, requestID string, afterID int64) (*UsageSettlementStatus, error) {
	r, ok := s.repo.(UsageSettlementStatusRepository)
	if !ok {
		return nil, fmt.Errorf("settlement status repository unavailable")
	}
	status, err := r.SettlementStatus(ctx, requestID, afterID)
	if err != nil {
		return nil, err
	}
	status.PersistenceUnconfirmed = s.persistFailures.Load()
	if stats, ok := s.repo.(interface {
		SettlementContention() *UsageSettlementContention
	}); ok {
		status.Contention = stats.SettlementContention()
	}
	status.RecoveryBoundary = UsageSettlementRecoveryBoundary
	return status, nil
}

func NewUsageSettlementService(repo UsageSettlementReceiptRepository, pool *UsageRecordWorkerPool, cache *BillingCacheService) *UsageSettlementService {
	return &UsageSettlementService{repo: repo, pool: pool, cache: cache}
}

func ProvideUsageSettlementService(repo UsageSettlementReceiptRepository, pool *UsageRecordWorkerPool, cache *BillingCacheService, keys *APIKeyService) *UsageSettlementService {
	s := NewUsageSettlementService(repo, pool, cache)
	s.authCacheInvalidator = keys
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			if err := s.Recover(ctx); err != nil && ctx.Err() == nil {
				slog.Error("usage settlement recovery", "failure_category", billingFailureCategory(err))
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return s
}

func (s *UsageSettlementService) Stop() {
	s.stop.Do(func() {
		if s.cancel != nil {
			s.cancel()
		}
		s.wg.Wait()
	})
}

// Submit returns only after the complete frozen receipt has committed. A failed
// acknowledgement is resolved by the original identity, never by a new request.
func (s *UsageSettlementService) Submit(ctx context.Context, input *UsageSettlementReceipt, after func(context.Context, *UsageSettlementResult)) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if input == nil {
		return s.persistenceError(UsageSettlementKey{}, fmt.Errorf("usage settlement receipt is nil"))
	}
	prepared, err := PrepareUsageSettlementReceipt(input)
	if err != nil {
		return s.persistenceError(input.Key, err)
	}
	writeCtx, cancel := detachedBillingContext(ctx)
	stored, err := s.repo.Persist(writeCtx, prepared)
	cancel()
	if err != nil {
		persistErr := err
		readCtx, readCancel := detachedBillingContext(ctx)
		stored, err = s.repo.Get(readCtx, prepared.Key)
		readCancel()
		if err != nil {
			return s.persistenceError(prepared.Key, fmt.Errorf("persist: %v; lookup: %w", persistErr, err))
		}
	}
	if stored == nil {
		return s.persistenceError(prepared.Key, fmt.Errorf("receipt acknowledgement is empty"))
	}
	if stored.Key != prepared.Key || stored.Command.RequestFingerprint != prepared.Command.RequestFingerprint {
		slog.Error("usage settlement conflict", "request_id", prepared.Key.RequestID, "failure_category", "conflict")
		return ErrUsageBillingRequestConflict
	}
	if stored.State == UsageSettlementNeedsReview {
		return ErrUsageSettlementNeedsReview
	}
	if stored.State != UsageSettlementPending && stored.State != UsageSettlementSettled {
		return s.persistenceError(prepared.Key, fmt.Errorf("invalid persisted receipt state"))
	}
	requestID, _ := ctx.Value(ctxkey.RequestID).(string)
	slog.Info("usage receipt accepted", "component", "billing", "request_id", requestID, "client_request_id", stored.Key.RequestID)
	// Queue only durable identity as billing evidence, never a caller-owned
	// receipt pointer. The optional post-commit callback is best-effort only;
	// recovery and every financial effect depend exclusively on the repository.
	key, fingerprint := stored.Key, stored.Command.RequestFingerprint
	task := func(ctx context.Context) {
		result, err := s.settleIdentity(ctx, key, fingerprint)
		if err != nil {
			slog.Error("usage settlement pending", "request_id", key.RequestID, "api_key_id", key.APIKeyID, "failure_category", billingFailureCategory(err))
			return
		}
		if after != nil && result.Billing != nil && result.Billing.Applied {
			after(ctx, result)
		}
	}
	if s.pool != nil && !s.pool.Submit(task).Dropped() {
		return nil
	}
	// Mandatory fallback, including stopped workers and drop/sample overflow.
	settleCtx, settleCancel := detachedBillingContext(ctx)
	defer settleCancel()
	task(settleCtx)
	return nil
}

func (s *UsageSettlementService) persistenceError(key UsageSettlementKey, err error) error {
	s.persistFailures.Add(1)
	slog.Error("usage receipt persistence unconfirmed", "request_id", key.RequestID, "api_key_id", key.APIKeyID, "recovery_boundary", UsageSettlementRecoveryBoundary, "failure_category", billingFailureCategory(err))
	return billingObservedError{cause: err}
}

func (s *UsageSettlementService) settle(ctx context.Context, receipt *UsageSettlementReceipt) (*UsageSettlementResult, error) {
	return s.settleIdentity(ctx, receipt.Key, receipt.Command.RequestFingerprint)
}

func (s *UsageSettlementService) settleIdentity(ctx context.Context, key UsageSettlementKey, fingerprint string) (*UsageSettlementResult, error) {
	result, err := s.repo.Settle(ctx, key, fingerprint)
	if err != nil {
		return nil, err
	}
	if result == nil || result.Receipt == nil || result.Billing == nil {
		return nil, fmt.Errorf("settlement acknowledgement is incomplete")
	}
	receipt := result.Receipt
	if receipt.Key != key || receipt.Command.RequestFingerprint != fingerprint {
		return nil, ErrUsageBillingRequestConflict
	}
	// Recovery has no original request callback. Refresh quota-bearing auth
	// snapshots here, including replay after a lost settlement acknowledgement.
	if s.authCacheInvalidator != nil && receipt.Command.APIKeyQuotaCost > 0 &&
		(result.Billing.APIKeyQuotaExhausted || !result.Billing.Applied) {
		s.authCacheInvalidator.InvalidateAuthCacheByUserID(ctx, receipt.Key.UserID)
	}
	// Invalidate views on replay too; never replay additive cache updates.
	if s.cache != nil {
		if err := s.cache.InvalidateUserBalance(ctx, receipt.Key.UserID); err != nil {
			slog.Warn("receipt balance cache invalidation", "request_id", receipt.Key.RequestID, "failure_category", billingFailureCategory(err))
		}
		if err := s.cache.InvalidateAPIKeyRateLimit(ctx, receipt.Key.APIKeyID); err != nil {
			slog.Warn("receipt key cache invalidation", "request_id", receipt.Key.RequestID, "failure_category", billingFailureCategory(err))
		}
		if receipt.Usage.GroupID != nil && receipt.Usage.SubscriptionID != nil {
			if err := s.cache.InvalidateSubscription(ctx, receipt.Key.UserID, *receipt.Usage.GroupID); err != nil {
				slog.Warn("receipt subscription cache invalidation", "request_id", receipt.Key.RequestID, "failure_category", billingFailureCategory(err))
			}
		}
	}
	return result, nil
}

type usageSettlementRecoveryContextKey struct{}

// IsUsageSettlementRecovery allows repository admission to reserve bounded
// capacity for durable backlog even while new submissions saturate a payer.
func IsUsageSettlementRecovery(ctx context.Context) bool {
	recovery, _ := ctx.Value(usageSettlementRecoveryContextKey{}).(bool)
	return recovery
}

func (s *UsageSettlementService) Recover(ctx context.Context) error {
	ctx = context.WithValue(ctx, usageSettlementRecoveryContextKey{}, true)
	var cursor int64
	for {
		rows, err := s.repo.ListPending(ctx, cursor, 100)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		for _, receipt := range rows {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			cursor = receipt.ID
			settleCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			_, err := s.settle(settleCtx, receipt)
			cancel()
			if err != nil {
				slog.Error("usage settlement recovery pending", "request_id", receipt.Key.RequestID, "api_key_id", receipt.Key.APIKeyID, "failure_category", billingFailureCategory(err))
			}
		}
	}
}

func submitUsageSettlement(ctx context.Context, s *UsageSettlementService, usage *UsageLog, p *postUsageBillingParams, deps *billingDeps, interrupted bool) error {
	if p.Cost.PricingState != PricingStateUnpriced && p.Cost.PricingState != PricingStateError {
		if err := FreezeUsagePricing(p.Cost, firstNonEmpty(optionalStringValue(usage.UpstreamModel), usage.Model), p.Account.Platform, p.AccountRateMultiplier, usage.AccountStatsCost); err != nil {
			p.Cost.PricingState = PricingStateError
		}
	}
	cmd := buildUsageBillingCommand(usage.RequestID, usage, p)
	if cmd == nil {
		return ErrUsageBillingRequestIDRequired
	}
	completion := "complete"
	if interrupted {
		completion = "interrupted"
	}
	receipt := &UsageSettlementReceipt{Key: UsageSettlementKey{RequestID: cmd.RequestID, APIKeyID: cmd.APIKeyID, UserID: cmd.UserID}, Command: *cmd, Usage: *usage, ResolutionState: "resolved", CompletionState: completion}
	// Invalid frozen input is review evidence, never a successful free usage row.
	if _, err := PrepareUsageSettlementReceipt(receipt); err != nil {
		receipt.Usage.PricingState = PricingStateError
		receipt.ResolutionState = "unresolved"
	}
	// Only the production provider with the quota hook may reach this branch.
	p.QuotaDerived = true
	p.Cost.ActualCost = QuantizeUsageBillingAmount(p.Cost.ActualCost)
	return s.Submit(ctx, receipt, func(ctx context.Context, result *UsageSettlementResult) {
		// Preserve explicit constructor callers without the production invalidator.
		if s.authCacheInvalidator == nil && result.Billing.APIKeyQuotaExhausted {
			if invalidator, ok := p.APIKeyService.(apiKeyAuthCacheInvalidator); ok && p.APIKey.Key != "" {
				invalidator.InvalidateAuthCacheByKey(ctx, p.APIKey.Key)
			}
		}
		finalizePostUsageBilling(ctx, p, deps, result.Billing)
	})
}
