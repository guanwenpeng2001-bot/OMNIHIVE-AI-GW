package service

import (
	"context"
	"github.com/stretchr/testify/require"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestCommandCodeUsageParsing(t *testing.T) {
	for _, tc := range []struct {
		name, window string
		count        int
		percent      float64
	}{
		{"normal", `"used":2.8,"cap":14,"resetAt":1789288541747`, 1, 20},
		{"zero", `"used":0,"cap":14`, 1, 0}, {"over cap", `"used":21,"cap":14`, 1, 150},
		{"missing used", `"cap":14`, 0, 0}, {"missing cap", `"used":2`, 0, 0}, {"zero cap", `"used":0,"cap":0`, 0, 0},
		{"negative", `"used":-1,"cap":14`, 0, 0}, {"null", `"used":null,"cap":14`, 0, 0},
		{"nan", `"used":"NaN","cap":14`, 0, 0}, {"infinite", `"used":1,"cap":"Inf"`, 0, 0},
		{"invalid reset", `"used":"7","cap":"14","resetAt":"bad"`, 1, 50},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tiers := parseCommandCodeUsageTiers([]byte(`{"windowLimits":{"limited":true,"fiveHour":{` + tc.window + `}}}`))
			require.Len(t, tiers, tc.count)
			if tc.count > 0 {
				require.InDelta(t, tc.percent, tiers[0].UsedPercent, 0.0001)
				if tc.name == "normal" {
					require.Equal(t, "2026-09-13T08:35:41Z", tiers[0].ResetAt)
				} else {
					require.Empty(t, tiers[0].ResetAt)
				}
			}
		})
	}
	for _, body := range []string{`{}`, `null`, `{broken`, `{"windowLimits":{"limited":false,"fiveHour":{"used":0,"cap":14}}}`} {
		require.Empty(t, parseCommandCodeUsageTiers([]byte(body)))
	}
}
func TestCommandCodeAlphaURL(t *testing.T) {
	u, err := commandCodeAlphaURL("https://proxy.example/prefix/provider/v1?secret=ignore", "billing/credits")
	require.NoError(t, err)
	require.Equal(t, "https://proxy.example/prefix/alpha/billing/credits", u)
}
func TestCommandCodeUnsupportedDoesNotInventQuotaOrChangeHealth(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
	}{{401, `{}`}, {403, `{}`}, {404, `{}`}, {200, `{}`}, {200, `{"windowLimits":{"limited":true,"fiveHour":{"cap":14}}}`}, {200, strings.Repeat(" ", cnQuotaMaxBodyBytes+1)}} {
		account := &Account{ID: 20, Platform: PlatformCommandCode, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test", "base_url": "https://proxy.example/provider/v1"}}
		repo := &cnBalanceProbeRepo{account: account}
		svc := NewCNProviderQuotaService(repo, nil, &cnBalanceResponseUpstream{statusCode: tc.status, body: tc.body}, nil)
		result, err := svc.QueryUsage(context.Background(), 20)
		require.NoError(t, err)
		require.False(t, result.Success)
		require.True(t, result.Unsupported)
		require.Empty(t, result.Tiers)
		require.Len(t, repo.extraWrites, 1)
		require.Equal(t, false, repo.extraWrites[0]["commandcode_usage_supported"])
		require.NotContains(t, repo.extraWrites[0], "commandcode_5h_used_percent")
	}
}
func TestCommandCodePersistsOnlyObservedWindows(t *testing.T) {
	account := &Account{ID: 20, Platform: PlatformCommandCode, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test", "base_url": "https://proxy.example/provider/v1"}}
	repo := &cnBalanceProbeRepo{account: account}
	svc := NewCNProviderQuotaService(repo, nil, &cnBalanceResponseUpstream{statusCode: 200, body: `{"credits":{"monthlyCredits":57},"windowLimits":{"limited":true,"weekly":{"used":7,"cap":35}}}`}, nil)
	result, err := svc.QueryUsage(context.Background(), 20)
	require.NoError(t, err)
	require.True(t, result.Success)
	require.True(t, result.Persisted)
	require.Equal(t, float64(20), repo.extraWrites[0]["commandcode_weekly_used_percent"])
	require.Nil(t, repo.extraWrites[0]["commandcode_5h_used_percent"])
	require.Nil(t, repo.extraWrites[0]["commandcode_weekly_reset_at"])
	require.Equal(t, map[string]float64{"monthlyCredits": 57}, result.Credits)
	require.Empty(t, result.PlanLevel)
}

// A transport timeout is a usage availability failure, not an account failure.
type commandCodeTimeoutUpstream struct {
	cnBalanceResponseUpstream
	t *testing.T
}

func (u *commandCodeTimeoutUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	require.Equal(u.t, "https://proxy.example/alpha/billing/credits", req.URL.String())
	require.Equal(u.t, http.MethodGet, req.Method)
	require.Equal(u.t, "test", req.Header.Get("x-api-key"))
	deadline, ok := req.Context().Deadline()
	require.True(u.t, ok)
	require.LessOrEqual(u.t, time.Until(deadline), cnQuotaUpstreamTimeout)
	return nil, context.DeadlineExceeded
}
func TestCommandCodeTransportTimeoutIsUnsupported(t *testing.T) {
	account := &Account{ID: 20, Platform: PlatformCommandCode, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test", "base_url": "https://proxy.example/provider/v1"}}
	repo := &cnBalanceProbeRepo{account: account}
	svc := NewCNProviderQuotaService(repo, nil, &commandCodeTimeoutUpstream{t: t}, nil)
	result, err := svc.QueryUsage(context.Background(), 20)
	require.NoError(t, err)
	require.True(t, result.Unsupported)
	require.False(t, result.Success)
	require.Empty(t, result.Tiers)
	require.Equal(t, false, repo.extraWrites[0]["commandcode_usage_supported"])
}
