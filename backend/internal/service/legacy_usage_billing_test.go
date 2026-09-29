package service

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"log/slog"
	"time"
)

// Historical billing helpers exist only to keep legacy amount/fingerprint regression fixtures runnable.
func postUsageBilling(ctx context.Context, p *postUsageBillingParams, deps *billingDeps) {
	billingCtx, cancel := detachedBillingContext(ctx)
	defer cancel()

	cost := p.Cost

	if p.IsSubscriptionBill {
		// Subscription usage tracked by ActualCost so group rate multiplier
		// consumes the quota at the expected speed.
		if cost.ActualCost > 0 {
			if err := deps.userSubRepo.IncrementUsage(billingCtx, p.Subscription.ID, cost.ActualCost); err != nil {
				slog.Error("increment subscription usage failed", "subscription_id", p.Subscription.ID, "error", err)
			}
		}
	} else {
		if cost.ActualCost > 0 {
			if err := deps.userRepo.DeductBalance(billingCtx, p.User.ID, cost.ActualCost); err != nil {
				slog.Error("deduct balance failed", "user_id", p.User.ID, "error", err)
			} else if deps.billingCacheService != nil {
				if err := deps.billingCacheService.InvalidateUserBalance(billingCtx, p.User.ID); err != nil {
					slog.Warn("invalidate balance cache after legacy deduction failed", "user_id", p.User.ID, "error", err)
				}
			}
		}
	}

	if p.shouldDeductAPIKeyQuota() {
		if err := p.APIKeyService.UpdateQuotaUsed(billingCtx, p.APIKey.ID, cost.ActualCost); err != nil {
			slog.Error("update api key quota failed", "api_key_id", p.APIKey.ID, "error", err)
		}
	}

	if p.shouldUpdateRateLimits() {
		if err := p.APIKeyService.UpdateRateLimitUsage(billingCtx, p.APIKey.ID, cost.ActualCost); err != nil {
			slog.Error("update api key rate limit usage failed", "api_key_id", p.APIKey.ID, "error", err)
		}
	}

	if p.shouldUpdateAccountQuota() {
		accountCost := QuantizeUsageBillingAmount(cost.TotalCost * p.AccountRateMultiplier)
		if err := deps.accountRepo.IncrementQuotaUsed(billingCtx, p.Account.ID, accountCost); err != nil {
			slog.Error("increment account quota used failed", "account_id", p.Account.ID, "cost", accountCost, "error", err)
		}
	}

	// Legacy callers still persist directly while receipt migration is in progress.
	// Receipt callers set QuotaDerived because their transaction owns the event.
	if !p.QuotaDerived && !p.IsSubscriptionBill && p.Platform != "" && cost.ActualCost > 0 && p.User != nil && deps.userPlatformQuotaRepo != nil {
		if deps.billingCacheService.HasUserPlatformQuotaLimit(billingCtx, p.User.ID, p.Platform) {
			deps.billingCacheService.IncrementUserPlatformQuotaUsage(p.User.ID, p.Platform, cost.ActualCost)
			// The replay flusher no longer persists Redis snapshots.
			if err := deps.userPlatformQuotaRepo.IncrementUsageWithReset(billingCtx, p.User.ID, p.Platform, cost.ActualCost, time.Now().UTC()); err != nil {
				userPlatformQuotaDBIncrLegacyErrorTotal.Add(1)
				logger.LegacyPrintf("service.gateway", "ALERT: legacy incr user platform quota DB failed user=%d platform=%s cost=%f: %v", p.User.ID, p.Platform, cost.ActualCost, err)
			}

		}
	}

	// NOTE: finalizePostUsageBilling is NOT called here to avoid double-queuing
	// cache updates. The legacy path does DB writes directly; the finalize path
	// does cache queue + notifications. Notifications are dispatched separately
	// by the caller after recording the usage log.
}

func applyUsageBilling(ctx context.Context, requestID string, usageLog *UsageLog, p *postUsageBillingParams, deps *billingDeps, repo UsageBillingRepository) (bool, error) {
	if p == nil || deps == nil {
		return false, nil
	}

	if p.Cost != nil {
		if usageLog != nil && p.Account != nil {
			if err := FreezeUsagePricing(p.Cost, firstNonEmpty(optionalStringValue(usageLog.UpstreamModel), usageLog.Model), p.Account.Platform, p.AccountRateMultiplier, usageLog.AccountStatsCost); err != nil {
				return false, err
			}
		}
		if usageLog != nil {
			usageLog.PricingState, usageLog.PricingSnapshot = p.Cost.PricingState, p.Cost.PricingSnapshot
		}
		if p.Cost.PricingState == PricingStateUnpriced {
			return false, ErrModelPricingUnavailable
		}
		if p.Cost.PricingState == PricingStateError {
			return false, ErrInvalidBillingAmount
		}
		if err := ValidateUsageBillingAmount(p.Cost.ActualCost); err != nil {
			return false, err
		}
	}
	cmd := buildUsageBillingCommand(requestID, usageLog, p)
	if cmd != nil {
		for _, amount := range []float64{cmd.BalanceCost, cmd.SubscriptionCost, cmd.APIKeyQuotaCost, cmd.APIKeyRateLimitCost, cmd.AccountQuotaCost} {
			if err := ValidateUsageBillingAmount(amount); err != nil {
				return false, err
			}
		}
	}
	// Keep raw inputs until Normalize has generated the historical fingerprint.
	if p.Cost != nil {
		p.Cost.ActualCost = QuantizeUsageBillingAmount(p.Cost.ActualCost)
	}
	if cmd == nil || cmd.RequestID == "" || repo == nil {
		postUsageBilling(ctx, p, deps)
		return true, nil
	}

	billingCtx, cancel := detachedBillingContext(ctx)
	defer cancel()

	result, err := repo.Apply(billingCtx, cmd)
	if err != nil {
		return false, err
	}

	if result == nil || !result.Applied {
		deps.deferredService.ScheduleLastUsedUpdate(p.Account.ID)
		return false, nil
	}

	if result.APIKeyQuotaExhausted {
		if invalidator, ok := p.APIKeyService.(apiKeyAuthCacheInvalidator); ok && p.APIKey != nil && p.APIKey.Key != "" {
			invalidator.InvalidateAuthCacheByKey(billingCtx, p.APIKey.Key)
		}
	}

	finalizePostUsageBilling(billingCtx, p, deps, result)
	return true, nil
}
