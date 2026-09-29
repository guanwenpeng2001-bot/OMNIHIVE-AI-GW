package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRedeemReductionPreservesRemainingTime(t *testing.T) {
	now := time.Now().UTC()
	for _, test := range []struct {
		name      string
		remaining time.Duration
		days      int
		expired   bool
	}{
		{"partial day", 36 * time.Hour, 1, false},
		{"one second left", 24*time.Hour + time.Second, 1, false},
		{"exact exhaustion", 24 * time.Hour, 1, true},
		{"excess deduction", 12 * time.Hour, 1, true},
		{"already expired", -time.Hour, 1, true},
		{"multiple days", 84 * time.Hour, 2, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			subscription := UserSubscription{ID: 7, ExpiresAt: now.Add(test.remaining), Status: SubscriptionStatusActive, Notes: "original"}
			repo := &lockingRenewalRepo{stale: subscription, current: subscription}
			subscriptions := NewSubscriptionService(nil, repo, nil, nil, nil)
			subscriptions.now = func() time.Time { return now }
			service := &RedeemService{subscriptionService: subscriptions}
			require.NoError(t, service.reduceOrCancelSubscription(context.Background(), 11, 13, test.days, "deduct"))
			expectedExpiry := subscription.ExpiresAt.AddDate(0, 0, -test.days)
			expectedStatus := SubscriptionStatusActive
			if test.expired {
				expectedExpiry = now
				expectedStatus = SubscriptionStatusExpired
			}
			require.Equal(t, expectedExpiry, repo.current.ExpiresAt)
			require.Equal(t, expectedStatus, repo.current.Status)
			require.Contains(t, repo.current.Notes, "original")
			require.Contains(t, repo.current.Notes, "deduct")
			require.Equal(t, 1, repo.lockReads)
		})
	}
}

func TestRedeemReductionUsesLatestRenewalAndAccumulatesDeductions(t *testing.T) {
	now := time.Now().UTC()
	stale := UserSubscription{ID: 7, ExpiresAt: now.AddDate(0, 0, 10), Status: SubscriptionStatusActive, Notes: "stale"}
	current := stale
	current.ExpiresAt = now.AddDate(0, 0, 20)
	current.Notes = "renewal"
	repo := &lockingRenewalRepo{stale: stale, current: current}
	subscriptions := NewSubscriptionService(nil, repo, nil, nil, nil)
	subscriptions.now = func() time.Time { return now }
	service := &RedeemService{subscriptionService: subscriptions}
	for _, code := range []string{"first-deduction", "second-deduction"} {
		require.NoError(t, service.reduceOrCancelSubscription(context.Background(), 11, 13, 1, code))
	}
	require.Equal(t, current.ExpiresAt.AddDate(0, 0, -2), repo.current.ExpiresAt)
	require.Equal(t, 2, repo.lockReads)
	require.Contains(t, repo.current.Notes, "renewal")
	require.Contains(t, repo.current.Notes, "first-deduction")
	require.Contains(t, repo.current.Notes, "second-deduction")
}
