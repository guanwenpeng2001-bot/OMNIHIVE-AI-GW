//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUpstreamJobFingerprintCanonicalAndTenantScoped(t *testing.T) {
	j := &UpstreamJob{UserID: 1, APIKeyID: 2, AccountID: 3, Platform: PlatformQianwen, Family: "video", Billing: UpstreamJobBilling{Model: "wan2.6-t2v"}}
	first, err := UpstreamJobFingerprint(j, []byte(`{"model":"wan2.6-t2v","input":{"prompt":"cat"}}`))
	require.NoError(t, err)
	same, err := UpstreamJobFingerprint(j, []byte(`{ "input": {"prompt":"cat"}, "model":"wan2.6-t2v" }`))
	require.NoError(t, err)
	require.Equal(t, first, same)
	for _, mutate := range []func(*UpstreamJob){func(j *UpstreamJob) { j.UserID++ }, func(j *UpstreamJob) { j.APIKeyID++ }, func(j *UpstreamJob) { j.AccountID++ }, func(j *UpstreamJob) { j.Family = "asr" }} {
		clone := *j
		mutate(&clone)
		other, err := UpstreamJobFingerprint(&clone, []byte(`{"model":"wan2.6-t2v","input":{"prompt":"cat"}}`))
		require.NoError(t, err)
		require.NotEqual(t, first, other)
	}
	_, err = UpstreamJobFingerprint(j, []byte(`{} {}`))
	require.Error(t, err)
}

func TestUpstreamJobVideoRequiresExplicitFrozenTariff(t *testing.T) {
	for _, platform := range []string{PlatformOpenRouter, PlatformKaggle} {
		t.Run(platform, func(t *testing.T) {
			job := &UpstreamJob{Platform: platform, Family: "video", UserID: 1, APIKeyID: 2, AccountID: 3,
				Billing: UpstreamJobBilling{Model: "video-model", PricingState: PricingStateUnpriced}}
			calls := 0
			service := NewUpstreamJobService(nil, nil, nil)
			_, err := service.Submit(t.Context(), job, []byte(`{"model":"video-model"}`), func(context.Context) (UpstreamJobUpdate, error) {
				calls++
				return UpstreamJobUpdate{}, nil
			})
			require.ErrorIs(t, err, ErrUpstreamJobTariff)
			require.Zero(t, calls)

			free := calculateUnitCost("video-model", "", string(BillingModePerRequest), PricingSourceGroup, 1, 0, 1)
			job.Billing.PricingState, job.Billing.PricingSnapshot = free.PricingState, free.PricingSnapshot
			cost, err := frozenPerRequestVideoCost(job.Billing)
			require.NoError(t, err)
			require.Equal(t, PricingStateFree, cost.PricingState)
			require.Zero(t, cost.ActualCost)
		})
	}
}
