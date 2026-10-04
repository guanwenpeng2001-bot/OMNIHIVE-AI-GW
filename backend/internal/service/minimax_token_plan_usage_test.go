package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Synthetic fixture based on MiniMax-AI/cli 06e47c70 test/sdk/quota.test.ts.
// This is not a captured response from api.minimax.cn and contains no real key.
const miniMaxTokenFixture = `{"model_remains":[{"model_name":"MiniMax-M3","start_time":0,"end_time":9999999999,"current_interval_total_count":1000,"current_interval_usage_count":500,"current_interval_remaining_percent":50,"current_weekly_total_count":5000,"current_weekly_usage_count":2000,"weekly_end_time":9999999999}]}`

func TestMiniMaxTokenPlanOfficialFixtures(test *testing.T) {
	tiers := parseMiniMaxTokenPlanTiers([]byte(miniMaxTokenFixture))
	require.Len(test, tiers, 2)
	require.Equal(test, float64(50), tiers[0].UsedPercent)
	// Official resolveQuotaCounts preserves remaining-count semantics without percent.
	require.Equal(test, float64(60), tiers[1].UsedPercent)
	require.Equal(test, "2286-11-20T17:46:39Z", tiers[1].ResetAt)
	// Official test/fixtures/quota-response.json uses MiniMax-M* and milliseconds.
	tiers = parseMiniMaxTokenPlanTiers([]byte(`{"model_remains":[{"model_name":"MiniMax-M*","current_interval_total_count":1500,"current_interval_usage_count":228,"end_time":1776373200000,"current_weekly_total_count":0,"current_weekly_usage_count":0}]}`))
	require.Len(test, tiers, 1)
	require.InDelta(test, 84.8, tiers[0].UsedPercent, 0.00001)
	require.Equal(test, time.UnixMilli(1776373200000).UTC().Format(time.RFC3339), tiers[0].ResetAt)
}

func TestMiniMaxTokenPlanObservedAndUnknown(test *testing.T) {
	for _, scenario := range []struct {
		name, fields string
		count        int
		used         float64
	}{
		{"explicit zero used", `"current_interval_remaining_percent":100,"current_interval_usage_count":0,"current_interval_total_count":5`, 1, 0},
		{"legacy zero used", `"current_interval_remaining_percent":100,"current_interval_usage_count":3,"current_interval_total_count":3`, 1, 0},
		{"zero remaining", `"current_interval_remaining_percent":0`, 1, 100},
		{"authoritative percent", `"current_interval_remaining_percent":40,"current_interval_usage_count":7,"current_interval_total_count":10`, 1, 60},
		{"legacy remaining count", `"current_interval_usage_count":4,"current_interval_total_count":10`, 1, 60},
		{"legacy full remaining", `"current_interval_usage_count":10,"current_interval_total_count":10`, 1, 0},
		{"missing count", `"current_interval_total_count":10`, 0, 0},
		{"zero total", `"current_interval_total_count":0,"current_interval_usage_count":0`, 0, 0},
		{"count above total", `"current_interval_total_count":10,"current_interval_usage_count":11`, 0, 0},
		{"null percent", `"current_interval_remaining_percent":null`, 0, 0},
		{"null percent with legacy counts", `"current_interval_remaining_percent":null,"current_interval_usage_count":4,"current_interval_total_count":10`, 1, 60},
		{"nan", `"current_interval_remaining_percent":"NaN"`, 0, 0},
		{"negative percent", `"current_interval_remaining_percent":-1`, 0, 0},
		{"infinite", `"current_interval_remaining_percent":"Inf"`, 0, 0},
		{"unlimited", `"current_interval_remaining_percent":100,"current_interval_status":3`, 0, 0},
		{"weekly exhausted", `"current_weekly_remaining_percent":0,"current_weekly_status":2`, 1, 100},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			tiers := parseMiniMaxTokenPlanTiers([]byte(`{"model_remains":[{"model_name":"MiniMax-M3",` + scenario.fields + `} ]}`))
			require.Len(test, tiers, scenario.count)
			if scenario.count > 0 {
				require.InDelta(test, scenario.used, tiers[0].UsedPercent, 0.00001)
			}
		})
	}
	for _, body := range []string{`{}`, `{broken`, `{"model_remains":[{"model_name":"video","current_interval_remaining_percent":0}]}`} {
		require.Empty(test, parseMiniMaxTokenPlanTiers([]byte(body)))
	}
}

func miniMaxTokenAccount(base string) *Account {
	account := codingAccount(PlatformMiniMax)
	account.Credentials["base_url"] = base
	return account
}

func TestMiniMaxTokenPlanRequestAndSnapshot(test *testing.T) {
	for _, base := range []string{"https://api.minimax.cn/v1", "https://api.minimax.cn/anthropic"} {
		test.Run(base, func(test *testing.T) {
			account := miniMaxTokenAccount(base)
			repo := &cnBalanceProbeRepo{account: account}
			upstream := &cnBalanceResponseUpstream{statusCode: 200, body: miniMaxTokenFixture}
			service := NewCNProviderQuotaService(repo, nil, upstream, nil)
			result, err := service.QueryUsage(context.Background(), account.ID)
			require.NoError(test, err)
			require.True(test, result.Success)
			require.True(test, result.Persisted)
			require.Equal(test, miniMaxTokenPlanURL, upstream.request.URL.String())
			require.Equal(test, "GET", upstream.request.Method)
			require.Equal(test, "Bearer sk-test", upstream.request.Header.Get("Authorization"))
			require.True(test, HTTPUpstreamRedirectsDisabled(upstream.request.Context()))
			_, deadlineSet := upstream.request.Context().Deadline()
			require.True(test, deadlineSet)
			require.Len(test, repo.extraWrites, 1)
			require.Equal(test, float64(60), repo.extraWrites[0]["minimax_weekly_used_percent"])
		})
	}
}

func TestMiniMaxTokenPlanRejectsWrongProfileAndKey(test *testing.T) {
	for _, base := range []string{"https://api.minimax.cn", "https://api.minimax.cn/v1/custom", "http://api.minimax.cn/v1", "https://api.minimax.cn:443/v1", "https://api.minimax.cn/v1?region=cn", "https://api.minimax.cn.evil.test/v1", "https://user@api.minimax.cn/v1"} {
		account := miniMaxTokenAccount(base)
		upstream := &recordingHTTPUpstream{}
		service := NewCNProviderQuotaService(&cnBalanceProbeRepo{account: account}, nil, upstream, nil)
		_, err := service.QueryUsageForAccount(context.Background(), account)
		require.Error(test, err, base)
		require.Zero(test, upstream.calls)
	}
	for _, change := range []func(*Account){
		func(account *Account) { account.Credentials["api_key"] = " " },
		func(account *Account) { account.Credentials["api_key"] = "sk-api-payg" },
		func(account *Account) { account.Credentials["account_mode"] = "payg" },
		func(account *Account) { account.Type = AccountTypeOAuth },
		func(account *Account) {
			account.Credentials["api_base_urls"] = map[string]any{APIProtocolAnthropic: "https://api.minimax.io/anthropic"}
		},
	} {
		account := miniMaxTokenAccount("https://api.minimax.cn/v1")
		change(account)
		upstream := &recordingHTTPUpstream{}
		service := NewCNProviderQuotaService(&cnBalanceProbeRepo{account: account}, nil, upstream, nil)
		_, err := service.QueryUsageForAccount(context.Background(), account)
		require.Error(test, err)
		require.Zero(test, upstream.calls)
	}
}

func TestMiniMaxTokenPlanFailuresPreserveSnapshot(test *testing.T) {
	for _, scenario := range []struct {
		status int
		body   string
	}{
		{401, `{}`}, {403, `{}`}, {500, `{}`}, {302, `{}`},
		{200, `{"base_resp":{"status_code":1004,"status_msg":"invalid key"}}`},
		{200, `{}`}, {200, `{broken`}, {200, `{"model_remains":[]}`},
	} {
		account := miniMaxTokenAccount("https://api.minimax.cn/v1")
		repo := &cnBalanceProbeRepo{account: account}
		service := NewCNProviderQuotaService(repo, nil, &cnBalanceResponseUpstream{statusCode: scenario.status, body: scenario.body}, nil)
		result, err := service.QueryUsageForAccount(context.Background(), account)
		require.NoError(test, err)
		require.False(test, result.Success)
		require.False(test, result.Persisted)
		require.Empty(test, repo.extraWrites)
	}
	account := miniMaxTokenAccount("https://api.minimax.cn/v1")
	upstream := &recordingHTTPUpstream{}
	service := NewCNProviderQuotaService(&cnBalanceProbeRepo{account: account}, nil, upstream, cnProbeAllowlistConfig("api.minimaxi.com"))
	_, err := service.QueryUsageForAccount(context.Background(), account)
	requireReason(test, err, "CN_QUOTA_URL_REJECTED")
	require.Zero(test, upstream.calls)
}

func TestMiniMaxTokenPlanLegacyRoutesUnchanged(test *testing.T) {
	for _, host := range []string{"api.minimaxi.com", "api.minimax.io"} {
		account := miniMaxTokenAccount("https://" + host + "/v1")
		require.False(test, isMiniMaxCNTokenPlan(account))
		upstream := &cnBalanceResponseUpstream{statusCode: 200, body: `{"model_remains":[{"model_name":"general","current_interval_remaining_percent":75}]}`}
		service := NewCNProviderQuotaService(&cnBalanceProbeRepo{account: account}, nil, upstream, nil)
		result, err := service.QueryUsageForAccount(context.Background(), account)
		require.NoError(test, err)
		require.True(test, result.Success)
		require.Equal(test, "https://"+host+"/v1/api/openplatform/coding_plan/remains", upstream.request.URL.String())
		require.Equal(test, float64(25), result.Tiers[0].UsedPercent)
	}
}

type miniMaxTokenTransport struct {
	cnBalanceResponseUpstream
	proxy     string
	onRequest func()
}

func (transport *miniMaxTokenTransport) Do(request *http.Request, proxy string, accountID int64, concurrency int) (*http.Response, error) {
	transport.proxy = proxy
	if transport.onRequest != nil {
		transport.onRequest()
	}
	return transport.cnBalanceResponseUpstream.Do(request, proxy, accountID, concurrency)
}

func TestMiniMaxTokenPlanProxyAndIdentityCAS(test *testing.T) {
	account := miniMaxTokenAccount("https://api.minimax.cn/v1")
	proxyID := int64(21)
	account.ProxyID = &proxyID
	account.Proxy = &Proxy{Protocol: "http", Host: "proxy.example", Port: 8080}
	repo := &cnBalanceProbeRepo{account: account}
	upstream := &miniMaxTokenTransport{cnBalanceResponseUpstream: cnBalanceResponseUpstream{statusCode: 200, body: miniMaxTokenFixture}}
	upstream.onRequest = func() {
		current := miniMaxTokenAccount("https://api.minimax.cn/v1")
		current.Credentials["api_key"] = "rotated-fixture-key"
		repo.account = current
	}
	service := NewCNProviderQuotaService(repo, nil, upstream, nil)
	result, err := service.QueryUsageForAccount(context.Background(), account)
	require.NoError(test, err)
	require.True(test, result.Success)
	require.False(test, result.Persisted)
	require.Empty(test, repo.extraWrites)
	require.Equal(test, "http://proxy.example:8080", upstream.proxy)
}

func TestMiniMaxTokenPlanPartialObservationClearsStaleWindows(test *testing.T) {
	account := miniMaxTokenAccount("https://api.minimax.cn/v1")
	account.Extra = map[string]any{"minimax_weekly_used_percent": 99.0, "minimax_5h_reset_at": "old"}
	repo := &cnBalanceProbeRepo{account: account}
	upstream := &cnBalanceResponseUpstream{statusCode: 200, body: `{"model_remains":[{"model_name":"MiniMax-M3","current_interval_remaining_percent":100}]}`}
	service := NewCNProviderQuotaService(repo, nil, upstream, nil)
	result, err := service.QueryUsageForAccount(context.Background(), account)
	require.NoError(test, err)
	require.True(test, result.Persisted)
	require.Len(test, result.Tiers, 1)
	require.Equal(test, float64(0), repo.extraWrites[0]["minimax_5h_used_percent"])
	for _, key := range []string{"minimax_5h_reset_at", "minimax_weekly_used_percent", "minimax_weekly_reset_at"} {
		require.Contains(test, repo.extraWrites[0], key)
		require.Nil(test, repo.extraWrites[0][key])
	}
}

func TestMiniMaxTokenPlanMultipleCodingBuckets(test *testing.T) {
	tiers := parseMiniMaxTokenPlanTiers([]byte(`{"model_remains":[
		{"model_name":"speech-hd","current_interval_remaining_percent":0},
		{"model_name":"MiniMax-M3","current_interval_remaining_percent":90,"end_time":1776373200000},
		{"model_name":"MiniMax-M*","current_interval_remaining_percent":70,"end_time":1776376800000}
	]}`))
	require.Len(test, tiers, 1)
	require.Equal(test, float64(30), tiers[0].UsedPercent)
	require.Equal(test, cnMillisToRFC3339(1776376800000), tiers[0].ResetAt)
}
