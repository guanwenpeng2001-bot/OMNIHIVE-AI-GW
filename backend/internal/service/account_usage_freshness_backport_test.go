package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestQuotaFreshnessBackportReadKeepsObservationTime(t *testing.T) {
	observed := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	for _, raw := range []any{observed.Format(time.RFC3339), observed, nil, "bad timestamp", time.Now().Add(time.Hour).Format(time.RFC3339)} {
		t.Run("observation", func(t *testing.T) {
			extra := map[string]any{"codex_5h_used_percent": 83.0, "codex_7d_used_percent": 42.0}
			if raw != nil {
				extra["codex_usage_updated_at"] = raw
			}
			reset := time.Now().Add(time.Hour)
			account := &Account{ID: 8801, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: extra, RateLimitResetAt: &reset}
			// No access token: a forced probe fails without touching any network.
			result, err := (&AccountUsageService{}).getOpenAIUsage(context.Background(), account, true)
			require.NoError(t, err)
			if raw == observed || raw == observed.Format(time.RFC3339) {
				require.NotNil(t, result.UpdatedAt)
				require.Equal(t, observed, *result.UpdatedAt)
			} else {
				require.Nil(t, result.UpdatedAt)
			}
			require.Equal(t, 83.0, result.FiveHour.Utilization)
			require.Equal(t, 42.0, result.SevenDay.Utilization)
			require.Equal(t, reset, *account.RateLimitResetAt, "reading usage must not alter the runtime cooldown")
		})
	}
}

func TestQuotaFreshnessBackportHeadersNeedUtilization(t *testing.T) {
	for _, tc := range []struct {
		name     string
		headers  http.Header
		observed bool
	}{
		{"empty", http.Header{}, false},
		{"reset_only", http.Header{"X-Codex-Primary-Reset-After-Seconds": {"100"}}, false},
		{"window_only", http.Header{"X-Codex-Primary-Window-Minutes": {"300"}}, false},
		{"zero_is_observed", http.Header{"X-Codex-Primary-Used-Percent": {"0"}, "X-Codex-Primary-Window-Minutes": {"300"}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			updates, err := extractOpenAICodexProbeUpdates(&http.Response{StatusCode: 200, Header: tc.headers})
			require.NoError(t, err)
			require.Equal(t, tc.observed, len(updates) > 0)
		})
	}
}

func TestQuotaFreshnessBackportSparkEmptyResponseDoesNotRefresh(t *testing.T) {
	for _, empty := range []bool{true, false} {
		t.Run(fmtBool(empty), func(t *testing.T) {
			observed := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
			parentID := int64(100)
			shadow := &Account{ID: 200, ParentAccountID: &parentID, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, QuotaDimension: QuotaDimensionSpark,
				Extra: map[string]any{"codex_5h_used_percent": 77.0, "codex_7d_used_percent": 55.0, "codex_usage_updated_at": observed.Format(time.RFC3339)}}
			parent := &Account{ID: 100, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Credentials: map[string]any{"access_token": "synthetic-token", "chatgpt_account_id": "synthetic-parent"}}
			ch := make(chan map[string]any, 1)
			repo := &sparkShadowUsageTestRepo{accounts: map[int64]*Account{100: parent, 200: shadow}, updateExtraCh: ch}
			token := NewOpenAITokenProvider(repo, nil, nil)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				value := OpenAIQuotaUsage{AdditionalRateLimits: []OpenAIAdditionalRateLimit{{MeteredFeature: "codex_bengalfox", RateLimit: &OpenAIRateLimit{}}}}
				if !empty {
					value.AdditionalRateLimits[0].RateLimit.PrimaryWindow = &OpenAIRateLimitWindow{UsedPercent: 0, LimitWindowSeconds: 18000, ResetAfterSeconds: 3600}
				}
				_ = json.NewEncoder(w).Encode(value)
			}))
			defer server.Close()
			quota := NewOpenAIQuotaService(repo, nil, token, newQuotaRedirectingFactory(server))
			usage, err := (&AccountUsageService{accountRepo: repo, openAIQuotaService: quota}).getOpenAIUsage(context.Background(), shadow, true)
			require.NoError(t, err)
			require.NotNil(t, usage.UpdatedAt)
			if empty {
				require.Equal(t, observed, *usage.UpdatedAt)
				require.Equal(t, 77.0, usage.FiveHour.Utilization)
				select {
				case <-ch:
					t.Fatal("empty Spark observation must not persist")
				case <-time.After(20 * time.Millisecond):
				}
			} else {
				require.True(t, usage.UpdatedAt.After(observed))
				require.Zero(t, usage.FiveHour.Utilization)
				select {
				case <-ch:
				case <-time.After(time.Second):
					t.Fatal("valid observation must persist")
				}
			}
		})
	}
}
func fmtBool(v bool) string {
	if v {
		return "empty"
	}
	return "explicit_zero"
}
