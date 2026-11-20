package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/stretchr/testify/require"
)

// Synthetic fixture copied from MiniMax-AI/cli 06e47c70 test/sdk/quota.test.ts;
// it is not a capture from either regional host.
const miniMaxBalanceFixture = `{"available_amount":"98.00","cash_balance":"0.00","voucher_balance":"98.00","credit_balance":"0.00","owed_amount":"0.00","balance_alert_switch":false,"balance_alert_threshold":"","base_resp":{"status_code":0,"status_msg":"success"}}`

func miniMaxPayGAccount(root string) *Account {
	return &Account{ID: 42, Platform: PlatformMiniMax, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"account_mode": AccountModePayG, "base_url": root, "api_key": "sk-api-fixture"},
		Extra:       map[string]any{"minimax_balance_low": true}}
}

type miniMaxBalanceRepo struct {
	*cnBalanceProbeRepo
	persistErr error
}

func (repo *miniMaxBalanceRepo) UpdateAccountRuntimeSnapshot(ctx context.Context, expected *Account, updates map[string]any, end *time.Time) error {
	if repo.persistErr != nil {
		return repo.persistErr
	}
	return repo.cnBalanceProbeRepo.UpdateAccountRuntimeSnapshot(ctx, expected, updates, end)
}

type miniMaxBalanceTransport struct {
	HTTPUpstream
	body    string
	status  int
	calls   int
	request *http.Request
	proxy   string
	readErr bool
}

func (upstream *miniMaxBalanceTransport) Do(request *http.Request, proxy string, _ int64, _ int) (*http.Response, error) {
	upstream.calls++
	upstream.request, upstream.proxy = request, proxy
	body := io.ReadCloser(io.NopCloser(strings.NewReader(upstream.body)))
	if upstream.readErr {
		body = &miniMaxFailedReader{}
	}
	return &http.Response{StatusCode: upstream.status, Body: body}, nil
}

type miniMaxFailedReader struct{}

func (*miniMaxFailedReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (*miniMaxFailedReader) Close() error             { return nil }

func TestMiniMaxPayGBalanceContract(test *testing.T) {
	for _, root := range []string{"https://api.minimax.cn", "https://api.minimax.io"} {
		for _, suffix := range []string{"", "/", "/v1", "/anthropic"} {
			test.Run(root+suffix, func(test *testing.T) {
				account := miniMaxPayGAccount(root + suffix)
				repo := &cnBalanceProbeRepo{account: account}
				upstream := &miniMaxBalanceTransport{body: miniMaxBalanceFixture, status: 200}
				result, err := NewCNProviderBalanceService(repo, nil, upstream, nil).QueryBalance(context.Background(), account.ID)
				require.NoError(test, err)
				require.True(test, result.Success)
				require.True(test, result.Persisted)
				require.Equal(test, 98.0, result.Balance)
				require.Empty(test, result.Currency)
				require.Equal(test, []CNProviderBalanceEntry{{Balance: 98}}, result.Balances)
				require.Equal(test, root+"/account/query_balance", upstream.request.URL.String())
				require.Equal(test, http.MethodGet, upstream.request.Method)
				require.Equal(test, "Bearer sk-api-fixture", upstream.request.Header.Get("Authorization"))
				require.True(test, HTTPUpstreamRedirectsDisabled(upstream.request.Context()))
				deadline, exists := upstream.request.Context().Deadline()
				require.True(test, exists)
				require.LessOrEqual(test, time.Until(deadline), cnBalanceUpstreamTimeout)
				require.Len(test, repo.extraWrites, 1)
				require.Len(test, repo.extraWrites[0], 4)
				require.NotContains(test, repo.extraWrites[0], "minimax_balance_available")
				require.NotContains(test, repo.extraWrites[0], "minimax_balance_low")
				require.Equal(test, true, account.Extra["minimax_balance_low"])
				require.Equal(test, StatusActive, account.Status)
				require.True(test, account.Schedulable)
			})
		}
	}
}

func TestMiniMaxPayGBalanceParsing(test *testing.T) {
	for _, scenario := range []struct {
		name, body string
		success    bool
		balance    float64
	}{
		{"official", miniMaxBalanceFixture, true, 98},
		{"zero", `{"available_amount":"0.00","base_resp":{"status_code":0}}`, true, 0},
		{"negative no health inference", `{"available_amount":"-1.25","base_resp":{"status_code":0}}`, true, -1.25},
		{"no component summing or scaling", `{"available_amount":"1234.56","cash_balance":"100","voucher_balance":"200","credit_balance":"300","owed_amount":"400","base_resp":{"status_code":0}}`, true, 1234.56},
		{"missing", `{"base_resp":{"status_code":0}}`, false, 0},
		{"null", `{"available_amount":null,"base_resp":{"status_code":0}}`, false, 0},
		{"number not official string", `{"available_amount":98,"base_resp":{"status_code":0}}`, false, 0},
		{"data not official shape", `{"data":{"available_amount":"98"},"base_resp":{"status_code":0}}`, false, 0},
		{"missing status", `{"available_amount":"98"}`, false, 0},
		{"business failure", `{"available_amount":"98","base_resp":{"status_code":1004}}`, false, 0},
		{"null status", `{"available_amount":"98","base_resp":{"status_code":null}}`, false, 0},
		{"status wrong type", `{"available_amount":"98","base_resp":{"status_code":"0"}}`, false, 0},
		{"nan", `{"available_amount":"NaN","base_resp":{"status_code":0}}`, false, 0},
		{"inf", `{"available_amount":"Inf","base_resp":{"status_code":0}}`, false, 0},
		{"empty", `{"available_amount":"","base_resp":{"status_code":0}}`, false, 0},
		{"malformed", miniMaxBalanceFixture + "broken", false, 0},
		{"html", `<html>error</html>`, false, 0},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			repo := &cnBalanceProbeRepo{account: miniMaxPayGAccount("https://api.minimax.cn")}
			upstream := &miniMaxBalanceTransport{status: 200, body: scenario.body}
			result, err := NewCNProviderBalanceService(repo, nil, upstream, nil).QueryBalance(context.Background(), 42)
			require.NoError(test, err)
			require.Equal(test, scenario.success, result.Success)
			if scenario.success {
				require.Equal(test, scenario.balance, result.Balance)
				require.Len(test, repo.extraWrites, 1)
			} else {
				require.NotEmpty(test, result.Error)
				require.Empty(test, result.Balances)
				require.Empty(test, repo.extraWrites)
			}
		})
	}
}

func TestMiniMaxPayGUnsupportedZeroOutbound(test *testing.T) {
	for _, scenario := range []struct {
		name   string
		change func(*Account)
	}{
		{"coding", func(account *Account) { account.Credentials["account_mode"] = "coding" }},
		{"plankey", func(account *Account) { account.Credentials["api_key"] = "sk-plan-fixture" }},
		{"mode missing", func(account *Account) { delete(account.Credentials, "account_mode") }},
		{"mode invalid", func(account *Account) { account.Credentials["account_mode"] = "unknown" }},
		{"oauth", func(account *Account) { account.Type = AccountTypeOAuth }},
		{"legacy", func(account *Account) { account.Credentials["base_url"] = "https://api.minimaxi.com/v1" }},
		{"unknown", func(account *Account) { account.Credentials["base_url"] = "https://relay.example/v1" }},
		{"lookalike", func(account *Account) { account.Credentials["base_url"] = "https://api.minimax.cn.evil.test" }},
		{"http", func(account *Account) { account.Credentials["base_url"] = "http://api.minimax.cn" }},
		{"port", func(account *Account) { account.Credentials["base_url"] = "https://api.minimax.cn:443" }},
		{"query", func(account *Account) { account.Credentials["base_url"] = "https://api.minimax.cn?x=y" }},
		{"missing root", func(account *Account) { delete(account.Credentials, "base_url") }},
		{"mixed region", func(account *Account) {
			account.Credentials["api_base_urls"] = map[string]any{APIProtocolAnthropic: "https://api.minimax.io/anthropic"}
		}},
		{"custom override", func(account *Account) {
			account.Credentials["api_base_urls"] = map[string]any{APIProtocolChatCompletions: "https://relay.example/v1"}
		}},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			account := miniMaxPayGAccount("https://api.minimax.cn/v1")
			scenario.change(account)
			repo := &cnBalanceProbeRepo{account: account}
			upstream := &miniMaxBalanceTransport{}
			result, err := NewCNProviderBalanceService(repo, nil, upstream, nil).QueryBalance(context.Background(), 42)
			require.Error(test, err)
			require.Nil(test, result)
			require.Zero(test, upstream.calls)
			require.Empty(test, repo.extraWrites)
		})
	}
}

func TestMiniMaxPayGPersistence(test *testing.T) {
	for _, failure := range []error{errors.New("store unavailable"), fmt.Errorf("wrapped: %w", ErrAccountRuntimeIdentityChanged)} {
		test.Run(failure.Error(), func(test *testing.T) {
			repo := &miniMaxBalanceRepo{cnBalanceProbeRepo: &cnBalanceProbeRepo{account: miniMaxPayGAccount("https://api.minimax.cn")}, persistErr: failure}
			upstream := &miniMaxBalanceTransport{status: 200, body: miniMaxBalanceFixture}
			result, err := NewCNProviderBalanceService(repo, nil, upstream, nil).QueryBalance(context.Background(), 42)
			if errors.Is(failure, ErrAccountRuntimeIdentityChanged) {
				require.ErrorIs(test, err, ErrAccountRuntimeIdentityChanged)
				require.Nil(test, result, "stale observation must not be returned")
			} else {
				require.NoError(test, err)
				require.True(test, result.Success)
				require.False(test, result.Persisted)
				require.Equal(test, 98.0, result.Balance)
			}
			require.Empty(test, repo.extraWrites)
		})
	}
}

func TestMiniMaxPayGTransportFailures(test *testing.T) {
	for _, status := range []int{301, 302, 401, 403, 429, 500} {
		test.Run(fmt.Sprint(status), func(test *testing.T) {
			repo := &cnBalanceProbeRepo{account: miniMaxPayGAccount("https://api.minimax.io")}
			upstream := &miniMaxBalanceTransport{status: status, body: miniMaxBalanceFixture}
			result, err := NewCNProviderBalanceService(repo, nil, upstream, nil).QueryBalance(context.Background(), 42)
			require.NoError(test, err)
			require.False(test, result.Success)
			require.Empty(test, repo.extraWrites)
			require.Equal(test, 1, upstream.calls)
		})
	}
	test.Run("body read failure", func(test *testing.T) {
		repo := &cnBalanceProbeRepo{account: miniMaxPayGAccount("https://api.minimax.io")}
		upstream := &miniMaxBalanceTransport{status: 200, readErr: true}
		result, err := NewCNProviderBalanceService(repo, nil, upstream, nil).QueryBalance(context.Background(), 42)
		require.NoError(test, err)
		require.False(test, result.Success)
		require.Empty(test, repo.extraWrites)
	})
	test.Run("oversized body", func(test *testing.T) {
		repo := &cnBalanceProbeRepo{account: miniMaxPayGAccount("https://api.minimax.io")}
		upstream := &miniMaxBalanceTransport{status: 200, body: miniMaxBalanceFixture + strings.Repeat(" ", cnBalanceMaxBodyBytes)}
		result, err := NewCNProviderBalanceService(repo, nil, upstream, nil).QueryBalance(context.Background(), 42)
		require.NoError(test, err)
		require.False(test, result.Success)
		require.Empty(test, repo.extraWrites)
	})
	test.Run("operator allowlist", func(test *testing.T) {
		repo := &cnBalanceProbeRepo{account: miniMaxPayGAccount("https://api.minimax.cn")}
		upstream := &miniMaxBalanceTransport{}
		cfg := &config.Config{}
		cfg.Security.URLAllowlist.Enabled = true
		cfg.Security.URLAllowlist.UpstreamHosts = []string{"api.deepseek.com"}
		_, err := NewCNProviderBalanceService(repo, nil, upstream, cfg).QueryBalance(context.Background(), 42)
		require.Error(test, err)
		require.Zero(test, upstream.calls)
	})
}

func TestMiniMaxPayGProxyAndSameRegionOverrides(test *testing.T) {
	account := miniMaxPayGAccount("https://api.minimax.cn")
	account.Credentials["api_base_urls"] = map[string]any{APIProtocolChatCompletions: "https://api.minimax.cn/v1", APIProtocolAnthropic: "https://api.minimax.cn/anthropic"}
	proxyID := int64(12)
	account.ProxyID = &proxyID
	account.Proxy = &Proxy{ID: proxyID, Protocol: "http", Host: "proxy.example", Port: 8080}
	repo := &cnBalanceProbeRepo{account: account}
	upstream := &miniMaxBalanceTransport{status: 200, body: miniMaxBalanceFixture}
	result, err := NewCNProviderBalanceService(repo, nil, upstream, nil).QueryBalance(context.Background(), 42)
	require.NoError(test, err)
	require.True(test, result.Success)
	require.Equal(test, "http://proxy.example:8080", upstream.proxy)
	require.Equal(test, "https://api.minimax.cn/account/query_balance", upstream.request.URL.String())
}

func TestMiniMaxPayGStaleAccountDoesNotPublish(test *testing.T) {
	observed := miniMaxPayGAccount("https://api.minimax.cn")
	current := miniMaxPayGAccount("https://api.minimax.io")
	current.Credentials["api_key"] = "sk-api-rotated"
	repo := &cnBalanceProbeRepo{account: current}
	upstream := &miniMaxBalanceTransport{status: 200, body: miniMaxBalanceFixture}
	result, err := NewCNProviderBalanceService(repo, nil, upstream, nil).QueryBalanceForAccount(context.Background(), observed)
	require.ErrorIs(test, err, ErrAccountRuntimeIdentityChanged)
	require.Nil(test, result)
	require.Empty(test, repo.extraWrites)
}

func TestMiniMaxPayGBalanceDecimalRoundTrip(test *testing.T) {
	for _, scenario := range []struct {
		amount string
		valid  bool
	}{
		{"98.00", true}, {"1234.56789", true}, {"0.1", true}, {"-0.25", true}, {"0.00", true},
		{"9007199254740991", true}, {"-9007199254740991", true}, {"1e-20", true},
		{"9007199254740992", false}, {"9007199254740993", false}, {"-9007199254740992", false},
		{"9007199254740991.1", false}, {"1.00000000000000001", false}, {"1234.567890123456789", false},
		{"1e-400", false}, {"1e-999999999", false}, {"1e999999999", false},
		{strings.Repeat("0", 129), false}, {"0x1p2", false}, {"1/2", false},
	} {
		test.Run(scenario.amount, func(test *testing.T) {
			repo := &cnBalanceProbeRepo{account: miniMaxPayGAccount("https://api.minimax.cn")}
			upstream := &miniMaxBalanceTransport{status: 200, body: fmt.Sprintf(`{"available_amount":%q,"base_resp":{"status_code":0}}`, scenario.amount)}
			result, err := NewCNProviderBalanceService(repo, nil, upstream, nil).QueryBalance(context.Background(), 42)
			require.NoError(test, err)
			require.Equal(test, scenario.valid, result.Success)
			if scenario.valid {
				require.Len(test, repo.extraWrites, 1)
			} else {
				require.Empty(test, repo.extraWrites)
				require.Empty(test, result.Balances)
			}
		})
	}
}
