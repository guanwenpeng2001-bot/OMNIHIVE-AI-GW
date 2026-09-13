//go:build unit

package service

import (
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
