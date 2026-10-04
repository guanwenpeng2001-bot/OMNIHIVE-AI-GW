package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

func compatibleUsageFixtureWindow() CompatibleUsageWindow {
	return CompatibleUsageWindow{Granularity: "day", Start: "2026-09-01T00:00:00+08:00", End: "2026-09-08T00:00:00+08:00"}
}

const compatibleUsageFixtureBody = `{"status":true,"data":[{"id":"model-one","name":"Model One","items":[{"name":"Input","unit":"kToken","total":0},{"name":"Output","unit":"Token","total":12.5}]}]}`

func allowCompatibleUsageFixture(test *testing.T) {
	previous := compatibleQiniuQueries
	compatibleQiniuQueries = rate.NewLimiter(rate.Inf, 1)
	test.Cleanup(func() { compatibleQiniuQueries = previous })
}

func TestCompatibleUsageCapabilityAllRegisteredModes(test *testing.T) {
	providers := domain.CompatibleProviders()
	require.Len(test, providers, 75)
	wallets := map[string]bool{"novita": true, "ppio": true, "siliconflow_cn": true, "siliconflow_global": true, "soleapi": true, "shengsuanyun": true, "atlascloud": true}
	for _, provider := range providers {
		for _, mode := range provider.Modes {
			for _, endpoint := range mode.Endpoints {
				test.Run(provider.ID+"/"+mode.Mode+"/"+endpoint.Protocol, func(test *testing.T) {
					account := compatibleBalanceAccount(provider.ID)
					account.Credentials["account_mode"], account.Credentials["api_protocol"], account.Credentials["base_url"] = mode.Mode, endpoint.Protocol, endpoint.BaseURL
					target, kind, err := compatibleUsageTarget(account)
					if mode.Mode == "payg" && (wallets[provider.ID] || provider.ID == "qiniu") {
						require.NoError(test, err)
						require.NotEmpty(test, target)
						if provider.ID == "qiniu" {
							require.Equal(test, "consumed_usage", kind)
						} else {
							require.Equal(test, "wallet_balance", kind)
						}
					} else {
						require.Error(test, err)
						require.Empty(test, target)
						require.Equal(test, "unsupported", kind)
					}
				})
			}
		}
	}
}

func TestCompatibleUsageWindowValidation(test *testing.T) {
	for _, scenario := range []struct {
		name, start, end, zone, granularity string
		valid                               bool
	}{
		{"default", "2026-01-01T00:00:00+08:00", "2026-01-31T23:59:59+08:00", "", "day", true},
		{"exact 31 days", "2026-01-01T00:00:00Z", "2026-02-01T00:00:00Z", "UTC", "day", true},
		{"over 31 days", "2026-01-01T00:00:00Z", "2026-02-01T00:00:00.000000001Z", "UTC", "day", false},
		{"same instant different offsets", "2026-01-01T08:00:00+08:00", "2026-01-01T00:00:00Z", "UTC", "day", false},
		{"reverse", "2026-01-02T00:00:00Z", "2026-01-01T00:00:00Z", "UTC", "day", false},
		{"DST instants preserved", "2026-03-08T00:00:00-08:00", "2026-03-09T00:00:00-07:00", "America/Los_Angeles", "day", true},
		{"date only", "2026-01-01", "2026-01-02", "", "day", false},
		{"missing offset", "2026-01-01T00:00:00", "2026-01-02T00:00:00", "", "day", false},
		{"invalid offset hour", "2026-01-01T00:00:00+24:00", "2026-01-02T00:00:00+24:00", "", "day", false},
		{"invalid offset minute", "2026-01-01T00:00:00+08:60", "2026-01-02T00:00:00+08:60", "", "day", false},
		{"unknown timezone", "2026-01-01T00:00:00Z", "2026-01-02T00:00:00Z", "Mars/Invalid", "day", false},
		{"local timezone", "2026-01-01T00:00:00Z", "2026-01-02T00:00:00Z", "Local", "day", false},
		{"hour excluded", "2026-01-01T00:00:00Z", "2026-01-02T00:00:00Z", "UTC", "hour", false},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			window, err := ValidateCompatibleUsageWindow(CompatibleUsageWindow{Start: scenario.start, End: scenario.end, Timezone: scenario.zone, Granularity: scenario.granularity})
			if !scenario.valid {
				require.Error(test, err)
				return
			}
			require.NoError(test, err)
			require.Equal(test, scenario.start, window.Start)
			require.Equal(test, scenario.end, window.End)
			if scenario.zone == "" {
				require.Equal(test, "Asia/Shanghai", window.Timezone)
			}
		})
	}
}

func TestCompatibleUsageRequestAndNoPersistence(test *testing.T) {
	allowCompatibleUsageFixture(test)
	for _, grouped := range []bool{false, true} {
		account := compatibleBalanceAccount("qiniu")
		account.Credentials["api_key"] = "tk-fixture-only"
		account.Extra = map[string]any{"header_overrides": map[string]any{"Authorization": "leak", "X-Secret": "leak"}}
		proxyID := int64(9)
		if grouped {
			account.ProxyGroupID = &proxyID
			account.egress = &accountEgress{target: EgressTarget{Resolved: true, URL: "http://proxy.example:8080", Source: EgressSourceGroupBinding}}
		} else {
			account.ProxyID, account.Proxy = &proxyID, &Proxy{Protocol: "http", Host: "proxy.example", Port: 8080}
		}
		repo := &compatibleBalanceRepo{account: account}
		upstream := compatibleBalanceHTTP{send: func(request *http.Request, proxy string, identity int64, concurrency int) (*http.Response, error) {
			require.Equal(test, "GET", request.Method)
			require.Equal(test, "https", request.URL.Scheme)
			require.Equal(test, "api.qnaigc.com", request.URL.Host)
			require.Equal(test, "/v3/stat/usage", request.URL.Path)
			require.Equal(test, "day", request.URL.Query().Get("granularity"))
			require.Equal(test, compatibleUsageFixtureWindow().Start, request.URL.Query().Get("start"))
			require.Equal(test, "Asia/Shanghai", request.URL.Query().Get("timezone"))
			require.NotContains(test, request.URL.String(), "fixture-only")
			require.Equal(test, "Bearer tk-fixture-only", request.Header.Get("Authorization"))
			require.Empty(test, request.Header.Get("X-Secret"))
			require.Equal(test, account.ID, identity)
			require.Equal(test, 1, concurrency)
			require.Equal(test, "http://proxy.example:8080", proxy)
			require.True(test, HTTPUpstreamRedirectsDisabled(request.Context()))
			deadline, exists := request.Context().Deadline()
			require.True(test, exists)
			require.LessOrEqual(test, time.Until(deadline), cnBalanceUpstreamTimeout)
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(compatibleUsageFixtureBody))}, nil
		}}
		result, err := NewCNProviderBalanceService(repo, nil, upstream, nil).QueryAccountUsage(context.Background(), account.ID, compatibleUsageFixtureWindow())
		require.NoError(test, err)
		require.True(test, result.Success)
		require.Equal(test, "consumed_usage", result.Kind)
		require.Zero(test, result.Usage.Models[0].Items[0].Total)
		require.Equal(test, "kToken", result.Usage.Models[0].Items[0].Unit)
		require.Equal(test, 12.5, result.Usage.Models[0].Items[1].Total)
		require.False(test, result.Persisted)
		require.Empty(test, repo.writes)
	}
}

func TestCompatibleUsageRejectsWithoutSending(test *testing.T) {
	allowCompatibleUsageFixture(test)
	for _, scenario := range []struct {
		name   string
		change func(*Account)
		policy bool
	}{
		{"subscription", func(account *Account) { account.Credentials["account_mode"] = "subscription" }, false},
		{"unknown mode", func(account *Account) { account.Credentials["account_mode"] = "anything" }, false},
		{"wrong provider", func(account *Account) { account.Platform = "atlascloud" }, false},
		{"native provider", func(account *Account) { account.Platform = "deepseek" }, false},
		{"malformed key", func(account *Account) { account.Credentials["api_key"] = false }, false},
		{"null key", func(account *Account) { account.Credentials["api_key"] = nil }, false},
		{"management key", func(account *Account) { account.Credentials["api_key"] = "AKSKfixture" }, false},
		{"newline key", func(account *Account) { account.Credentials["api_key"] = "sk-fixture\nother" }, false},
		{"unknown protocol", func(account *Account) { account.Credentials["api_protocol"] = "unknown" }, false},
		{"custom origin", func(account *Account) { account.Credentials["base_url"] = "https://other.example/v1" }, false},
		{"custom relay", func(account *Account) { account.Extra = map[string]any{"custom_base_url_enabled": true} }, false},
		{"invalid override key", func(account *Account) {
			account.Credentials["api_base_urls"] = map[string]any{"unknown": "https://api.qnaigc.com/v1"}
		}, false},
		{"wrong protocol path", func(account *Account) {
			account.Credentials["api_base_urls"] = map[string]any{"responses": "https://api.qnaigc.com/v1"}
		}, false},
		{"null override", func(account *Account) { account.Credentials["api_base_urls"] = nil }, false},
		{"missing proxy", func(account *Account) { identity := int64(1); account.ProxyID = &identity }, false},
		{"missing group", func(account *Account) { identity := int64(1); account.ProxyGroupID = &identity }, false},
		{"URL policy", func(account *Account) {}, true},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			account := compatibleBalanceAccount("qiniu")
			account.Credentials["api_key"] = "sk-fixture-only"
			scenario.change(account)
			repo := &compatibleBalanceRepo{account: account}
			upstream := compatibleBalanceHTTP{send: func(*http.Request, string, int64, int) (*http.Response, error) {
				test.Fatal("credentials escaped validation")
				return nil, nil
			}}
			var cfg *config.Config
			if scenario.policy {
				cfg = &config.Config{}
				cfg.Security.URLAllowlist.Enabled = true
				cfg.Security.URLAllowlist.UpstreamHosts = []string{"other.example"}
			}
			result, err := NewCNProviderBalanceService(repo, nil, upstream, cfg).QueryAccountUsage(context.Background(), account.ID, compatibleUsageFixtureWindow())
			if err == nil {
				require.False(test, result.Success)
				require.Nil(test, result.Usage)
				require.NotEmpty(test, result.Reason)
			}
			require.Empty(test, repo.writes)
		})
	}
}

func TestCompatibleConsumedUsageParserRejectsMalformed(test *testing.T) {
	for _, body := range []string{
		`{}`, `null`, `[]`, `{"status":true}`, `{"status":true,"data":null}`, `{"status":false,"data":[]}`, `{"status":true,"data":[{}]}`,
		strings.Replace(compatibleUsageFixtureBody, `"total":0`, `"total":null`, 1),
		strings.Replace(compatibleUsageFixtureBody, `"total":0`, `"total":"0"`, 1),
		strings.Replace(compatibleUsageFixtureBody, `"total":0`, `"total":1e400`, 1),
		strings.Replace(compatibleUsageFixtureBody, `"total":0`, `"total":-1`, 1),
		strings.Replace(compatibleUsageFixtureBody, `"total":0`, `"missing":0`, 1),
		strings.Replace(compatibleUsageFixtureBody, `"unit":"kToken"`, `"unit":null`, 1),
		strings.Replace(compatibleUsageFixtureBody, `"items":[`, `"items":null,"other":[`, 1),
		`{"status":true,"data":[` + strings.Repeat(`{"id":"x","name":"X","items":[]},`, 256) + `{"id":"x","name":"X","items":[]}]}`,
		`{"status":true,"data":[{"id":"x","name":"X","items":[` + strings.Repeat(`{"name":"x","unit":"kToken","total":0},`, 2048) + `{"name":"x","unit":"kToken","total":0}]}]}`,
		strings.Repeat(" ", cnBalanceMaxBodyBytes) + compatibleUsageFixtureBody,
	} {
		models, err := parseCompatibleConsumedUsage([]byte(body))
		require.Error(test, err)
		require.Nil(test, models)
	}
	models, err := parseCompatibleConsumedUsage([]byte(`{"status":true,"data":[]}`))
	require.NoError(test, err)
	require.NotNil(test, models)
	require.Empty(test, models)
}

func TestCompatibleUsageFailuresAndRateGate(test *testing.T) {
	allowCompatibleUsageFixture(test)
	for _, scenario := range []struct {
		status       int
		body, reason string
		transport    error
	}{
		{401, "sk-fixture-only", "invalid_credentials", nil}, {403, "sk-fixture-only", "permission_denied", nil},
		{429, "sk-fixture-only", "rate_limited", nil}, {500, "sk-fixture-only", "upstream_error", nil},
		{302, "sk-fixture-only", "upstream_error", nil}, {200, `{}`, "invalid_response", nil},
		{200, strings.Repeat("x", cnBalanceMaxBodyBytes+1), "invalid_response", nil},
		{0, "", "upstream_error", errors.New("sk-fixture-only")},
	} {
		account := compatibleBalanceAccount("qiniu")
		account.Credentials["api_key"] = "sk-fixture-only"
		repo := &compatibleBalanceRepo{account: account}
		upstream := compatibleBalanceHTTP{send: func(*http.Request, string, int64, int) (*http.Response, error) {
			return &http.Response{StatusCode: scenario.status, Body: io.NopCloser(strings.NewReader(scenario.body))}, scenario.transport
		}}
		result, err := NewCNProviderBalanceService(repo, nil, upstream, nil).QueryAccountUsage(context.Background(), account.ID, compatibleUsageFixtureWindow())
		require.NoError(test, err)
		require.False(test, result.Success)
		require.Equal(test, scenario.reason, result.Reason)
		encoded, err := json.Marshal(result)
		require.NoError(test, err)
		require.NotContains(test, string(encoded), "sk-fixture-only")
		require.Empty(test, repo.writes)
	}
	compatibleQiniuQueries = rate.NewLimiter(rate.Every(time.Hour), 1)
	require.True(test, compatibleQiniuQueries.Allow())
	account := compatibleBalanceAccount("qiniu")
	account.Credentials["api_key"] = "sk-fixture-only"
	upstream := compatibleBalanceHTTP{send: func(*http.Request, string, int64, int) (*http.Response, error) {
		test.Fatal("throttled query sent")
		return nil, nil
	}}
	result, err := NewCNProviderBalanceService(&compatibleBalanceRepo{account: account}, nil, upstream, nil).QueryAccountUsage(context.Background(), account.ID, compatibleUsageFixtureWindow())
	require.NoError(test, err)
	require.Equal(test, "rate_limited", result.Reason)
}

func TestCompatibleUsageIdentitySingleflightAndCancellation(test *testing.T) {
	allowCompatibleUsageFixture(test)
	account := compatibleBalanceAccount("qiniu")
	account.Credentials["api_key"] = "sk-fixture-only"
	started, release := make(chan struct{}, 8), make(chan struct{})
	var calls atomic.Int32
	upstream := compatibleBalanceHTTP{send: func(*http.Request, string, int64, int) (*http.Response, error) {
		calls.Add(1)
		started <- struct{}{}
		<-release
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(compatibleUsageFixtureBody))}, nil
	}}
	repo := &compatibleBalanceRepo{account: account}
	service := NewCNProviderBalanceService(repo, nil, upstream, nil)
	finished := make(chan error, 8)
	go func() {
		_, err := service.QueryAccountUsage(context.Background(), account.ID, compatibleUsageFixtureWindow())
		finished <- err
	}()
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := service.QueryAccountUsage(ctx, account.ID, compatibleUsageFixtureWindow())
	require.ErrorIs(test, err, context.DeadlineExceeded)
	require.Equal(test, int32(1), calls.Load())
	// A different window must start a distinct request while the first is live.
	window := compatibleUsageFixtureWindow()
	window.End = "2026-09-09T00:00:00+08:00"
	go func() { _, err := service.QueryAccountUsage(context.Background(), account.ID, window); finished <- err }()
	<-started
	// A rotated credential is also distinct; replace the loaded snapshot.
	rotated := compatibleBalanceAccount("qiniu")
	rotated.Credentials["api_key"] = "sk-rotated-fixture"
	repo.account = rotated
	go func() {
		_, err := service.QueryAccountUsage(context.Background(), account.ID, compatibleUsageFixtureWindow())
		finished <- err
	}()
	<-started
	require.Equal(test, int32(3), calls.Load())
	close(release)
	for index := 0; index < 3; index++ {
		require.NoError(test, <-finished)
	}
	canceled, cancelNow := context.WithCancel(context.Background())
	cancelNow()
	_, err = service.QueryAccountUsage(canceled, account.ID, compatibleUsageFixtureWindow())
	require.ErrorIs(test, err, context.Canceled)
	require.Equal(test, int32(3), calls.Load())
}
