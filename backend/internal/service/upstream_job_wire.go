package service

import (
	"context"
	"log/slog"
	"time"
)

func ProvideUpstreamJobService(repo UpstreamJobRepository, gateway *OpenAIGatewayService, wheel *TimingWheelService, billing *BillingCacheService, keys *APIKeyService) *UpstreamJobService {
	svc := NewUpstreamJobService(repo, gateway.PollQianwenAsyncJob, QianwenAsyncJobCharge)
	svc.afterSettle = func(ctx context.Context, j *UpstreamJob) {
		if err := billing.InvalidateUserBalance(ctx, j.UserID); err != nil {
			logJobEvent(ctx, j, "cache_invalidation_failed")
		}
		if err := billing.InvalidateAPIKeyRateLimit(ctx, j.APIKeyID); err != nil {
			logJobEvent(ctx, j, "cache_invalidation_failed")
		}
		if j.Billing.SubscriptionID != nil && j.GroupID != nil {
			if err := billing.InvalidateSubscription(ctx, j.UserID, *j.GroupID); err != nil {
				logJobEvent(ctx, j, "cache_invalidation_failed")
			}
		}
		keys.InvalidateAuthCacheByUserID(ctx, j.UserID)
		// The transaction has recorded the quota event. Only invalidate here;
		// durable replay retries this if the process exits or Redis is down.
		if billing.cache != nil {
			if err := billing.cache.DeleteUserPlatformQuotaCache(ctx, j.UserID, j.Platform); err != nil {
				logJobEvent(ctx, j, "cache_invalidation_failed")
			}
		}
	}
	svc.stopPolling = func() { wheel.Cancel("upstream-jobs") }
	wheel.ScheduleRecurring("upstream-jobs", 5*time.Second, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := svc.PollDue(ctx, 20); err != nil {
			slog.Error("upstream job poll failed", "failure_category", billingFailureCategory(err))
		}
	})
	return svc
}
