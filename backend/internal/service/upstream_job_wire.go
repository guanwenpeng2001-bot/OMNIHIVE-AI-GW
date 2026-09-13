package service

import (
	"context"
	"log/slog"
	"time"
)

func ProvideUpstreamJobService(repo UpstreamJobRepository, gateway *OpenAIGatewayService, wheel *TimingWheelService, billing *BillingCacheService, keys *APIKeyService, platformQuotas UserPlatformQuotaRepository) *UpstreamJobService {
	svc := NewUpstreamJobService(repo, gateway.PollQianwenAsyncJob, QianwenAsyncJobCharge)
	svc.afterSettle = func(ctx context.Context, j *UpstreamJob) {
		_ = billing.InvalidateUserBalance(ctx, j.UserID)
		_ = billing.InvalidateAPIKeyRateLimit(ctx, j.APIKeyID)
		if j.Billing.SubscriptionID != nil && j.GroupID != nil {
			_ = billing.InvalidateSubscription(ctx, j.UserID, *j.GroupID)
		}
		keys.InvalidateAuthCacheByUserID(ctx, j.UserID)
		// Match the existing gateway's optional platform-quota cache/flusher path.
		// Monetary ledger writes above remain atomic; this derived quota cache has
		// the same post-commit crash window as the synchronous gateway.
		if j.Billing.SubscriptionID == nil && billing.HasUserPlatformQuotaLimit(ctx, j.UserID, j.Platform) {
			log, _, err := QianwenAsyncJobCharge(j)
			if err == nil {
				billing.IncrementUserPlatformQuotaUsage(j.UserID, j.Platform, log.ActualCost)
				if billing.cfg == nil || !billing.cfg.Database.UserPlatformQuotaFlusherEnabled {
					if err := platformQuotas.IncrementUsageWithReset(ctx, j.UserID, j.Platform, log.ActualCost, time.Now().UTC()); err != nil {
						slog.Error("upstream job platform quota update failed", "job", j.PublicRequestID, "error", err)
					}
				}
			}
		}
	}
	wheel.ScheduleRecurring("upstream-jobs", 5*time.Second, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := svc.PollDue(ctx, 20); err != nil {
			slog.Error("upstream job poll failed", "error", err)
		}
	})
	return svc
}
