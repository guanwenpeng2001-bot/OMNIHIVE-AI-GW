//go:build unit

package service

import (
	"context"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestCursorRelayBillingContract(t *testing.T) {
	require.True(t, IsUpstreamBillingProbeIdentity(PlatformCursor, AccountTypeUpstream))
	for _, kind := range []string{AccountTypeAPIKey, AccountTypeOAuth, AccountTypeBedrock} {
		require.False(t, IsUpstreamBillingProbeIdentity(PlatformCursor, kind))
	}
	for _, target := range []string{"https://relay.example/v1", "https://api2.cursor.sh", "https://API.CURSOR.COM.:443", "https://relay.example/cursor.com"} {
		t.Run(target, func(t *testing.T) {
			account := &Account{ID: 12, Platform: PlatformCursor, Type: AccountTypeUpstream, Status: StatusActive, Credentials: map[string]any{"base_url": target, "api_key": "private-key"}, Extra: map[string]any{UpstreamBillingProbeEnabledExtraKey: true, UpstreamBillingRateSyncEnabledExtraKey: true}}
			repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{12: account}}
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"object":"sub2api.key_billing","schema_version":1,"billing_scope":"token","group_rate_multiplier":0.6,"peak_rate_enabled":false,"observed_at":"2026-09-13T00:00:00Z","resolved_rate_multiplier":0.6,"effective_rate_multiplier":0.6}`))}}
			snapshot, err := newUpstreamBillingProbeTestService(repo, upstream, &upstreamBillingProbeSettingRepo{}).ProbeAccount(context.Background(), 12)
			require.NoError(t, err)
			if strings.Contains(target, "relay.example") {
				require.Equal(t, UpstreamBillingProbeStatusOK, snapshot.Status)
				require.NotNil(t, upstream.lastReq)
				require.Equal(t, "Bearer private-key", upstream.lastReq.Header.Get("Authorization"))
				require.Equal(t, 0.6, *account.RateMultiplier)
			} else {
				require.Equal(t, UpstreamBillingProbeStatusUnsupported, snapshot.Status)
				require.Nil(t, upstream.lastReq)
			}
		})
	}
}
