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
		// The transaction has recorded the quota event. Only invalidate here;
		// durable replay retries this if the process exits or Redis is down.
		if billing.cache != nil {
			if err := billing.cache.DeleteUserPlatformQuotaCache(ctx, j.UserID, j.Platform); err != nil {
				slog.Error("upstream job quota cache invalidation failed", "job", j.PublicRequestID, "error", err)
			}
		}
	}
	svc.stopPolling = func() { wheel.Cancel("upstream-jobs") }
	wheel.ScheduleRecurring("upstream-jobs", 5*time.Second, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := svc.PollDue(ctx, 20); err != nil {
			slog.Error("upstream job poll failed", "error", err)
		}
	})
	return svc
}
