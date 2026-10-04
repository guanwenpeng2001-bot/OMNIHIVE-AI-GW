package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Both official accounts/get.md pages use this response, including a zero
// available balance despite a cumulative promotional total of 26.
const stepfunAccountFixture = `{"object":"account","type":"prepaid","balance":0.00,"total_cash_balance":0.00,"total_voucher_balance":26.00}`

func TestStepFunBalanceRegionalRequestAndSnapshot(test *testing.T) {
	for provider, target := range map[string]string{
		"stepfun_api":      "https://api.stepfun.com/v1/accounts",
		"stepfun_api_intl": "https://api.stepfun.ai/v1/accounts",
	} {
		test.Run(provider, func(test *testing.T) {
			account := compatibleBalanceAccount(provider)
			account.Credentials["account_mode"] = "payg"
			account.Credentials["base_url"] = strings.TrimSuffix(target, "/accounts")
			account.Credentials["api_base_urls"] = map[string]any{"chat_completions": strings.TrimSuffix(target, "/accounts")}
			proxyID := int64(9)
			account.ProxyID = &proxyID
			account.Proxy = &Proxy{Protocol: "http", Host: "proxy.example", Port: 8080}
			repo := &compatibleBalanceRepo{account: account}
			calls := 0
			upstream := compatibleBalanceHTTP{send: func(request *http.Request, proxy string, identity int64, concurrency int) (*http.Response, error) {
				calls++
				require.Equal(test, http.MethodGet, request.Method)
				require.Equal(test, target, request.URL.String())
				require.Nil(test, request.Body)
				require.Equal(test, "Bearer fixture-only-key", request.Header.Get("Authorization"))
				require.Equal(test, "http://proxy.example:8080", proxy)
				require.Equal(test, account.ID, identity)
				require.Equal(test, 1, concurrency)
				require.True(test, HTTPUpstreamRedirectsDisabled(request.Context()))
				deadline, exists := request.Context().Deadline()
				require.True(test, exists)
				require.LessOrEqual(test, time.Until(deadline), cnBalanceUpstreamTimeout)
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(stepfunAccountFixture))}, nil
			}}
			value, err := NewCNProviderBalanceService(repo, nil, upstream, nil).QueryAccountBalance(context.Background(), account.ID)
			require.NoError(test, err)
			result := requireCompatibleBalanceResult(test, value)
			require.Equal(test, 1, calls)
			require.True(test, result.Success)
			require.True(test, result.Persisted)
			require.Equal(test, "wallet_balance", result.Kind)
			require.Equal(test, "account", result.Scope)
			require.NotNil(test, result.Balance)
			require.Zero(test, *result.Balance)
			require.Empty(test, result.Currency)
			require.Equal(test, []CNProviderBalanceEntry{{Currency: "", Balance: 0}}, result.Balances)
			require.Len(test, repo.writes, 1)
			require.Equal(test, float64(0), repo.writes[0][provider+"_balance"])
			require.Equal(test, "", repo.writes[0][provider+"_balance_currency"])
			encoded, err := json.Marshal(result)
			require.NoError(test, err)
			require.NotContains(test, string(encoded), "USD")
			require.NotContains(test, string(encoded), "CNY")
			account.Extra = repo.writes[0]
			ClearAccountRuntimeObservations(account)
			require.Empty(test, account.Extra)
		})
	}
}

func TestStepFunBalanceContract(test *testing.T) {
	for _, provider := range []string{"stepfun_api", "stepfun_api_intl"} {
		for _, scenario := range []struct {
			name, body string
			amount     float64
			valid      bool
		}{
			{"official zero", stepfunAccountFixture, 0, true},
			{"prepaid", `{"object":"account","type":"prepaid","balance":12.5,"total_cash_balance":900,"total_voucher_balance":26}`, 12.5, true},
			{"postpaid", `{"object":"account","type":"postpaid","balance":-2.5}`, -2.5, true},
			{"missing", `{"object":"account","type":"prepaid","total_cash_balance":90,"total_voucher_balance":26}`, 0, false},
			{"null", `{"object":"account","type":"prepaid","balance":null}`, 0, false},
			{"string", `{"object":"account","type":"prepaid","balance":"0"}`, 0, false},
			{"overflow", `{"object":"account","type":"prepaid","balance":1e400}`, 0, false},
			{"nan", `{"object":"account","type":"prepaid","balance":NaN}`, 0, false},
			{"wrong object", `{"object":"balance","type":"prepaid","balance":0}`, 0, false},
			{"wrong type", `{"object":"account","type":"plan","balance":0}`, 0, false},
			{"missing type", `{"object":"account","balance":0}`, 0, false},
			{"error", `{"object":"account","type":"prepaid","balance":0,"error":{"message":"denied"}}`, 0, false},
			{"false success", `{"object":"account","type":"prepaid","balance":0,"success":false}`, 0, false},
		} {
			test.Run(provider+"/"+scenario.name, func(test *testing.T) {
				entries, err := parseCompatibleBalance(provider, []byte(scenario.body))
				if !scenario.valid {
					require.Error(test, err)
					require.Empty(test, entries)
					return
				}
				require.NoError(test, err)
				require.Equal(test, []CNProviderBalanceEntry{{Currency: "", Balance: scenario.amount}}, entries)
			})
		}
	}
}

func TestStepFunBalanceRejectsRegionsAndPlansBeforeSending(test *testing.T) {
	for _, provider := range []string{"stepfun_api", "stepfun_api_intl", "stepfun_plan", "stepfun_plan_intl"} {
		otherRegion := "https://api.stepfun.ai/v1"
		if provider == "stepfun_api_intl" {
			otherRegion = "https://api.stepfun.com/v1"
		}
		blockedCredentials := []map[string]any{
			{"account_mode": "coding"},
			{"billing_plan": "step_plan"},
			{"base_url": "https://relay.example/v1"},
			{"base_url": "https://api.stepfun.com/step_plan/v1"},
			{"base_url": "https://api.stepfun.ai/step_plan/v1"},
			{"base_url": otherRegion},
			{"api_base_urls": map[string]any{"chat_completions": otherRegion}},
		}
		if provider == "stepfun_plan" || provider == "stepfun_plan_intl" {
			blockedCredentials = append(blockedCredentials, map[string]any{}, map[string]any{"account_mode": "payg"})
		}
		for _, credentials := range blockedCredentials {
			account := compatibleBalanceAccount(provider)
			for field, value := range credentials {
				account.Credentials[field] = value
			}
			repo := &compatibleBalanceRepo{account: account}
			upstream := compatibleBalanceHTTP{send: func(*http.Request, string, int64, int) (*http.Response, error) {
				test.Fatalf("forbidden StepFun request: %s %v", provider, credentials)
				return nil, nil
			}}
			value, err := NewCNProviderBalanceService(repo, nil, upstream, nil).QueryAccountBalance(context.Background(), account.ID)
			require.NoError(test, err)
			result := requireCompatibleBalanceResult(test, value)
			require.True(test, result.Unsupported)
			require.False(test, result.Success)
			require.Nil(test, result.Balance)
			require.Empty(test, repo.writes)
		}
	}
}

func TestStepFunBalanceFailuresDoNotOverwriteSnapshot(test *testing.T) {
	for _, provider := range []string{"stepfun_api", "stepfun_api_intl"} {
		for _, scenario := range []struct {
			status       int
			body, reason string
		}{
			{200, `{}`, "invalid_response"},
			{200, strings.Repeat(" ", cnBalanceMaxBodyBytes+1), "invalid_response"},
			{302, "fixture-only-key", "upstream_error"},
			{401, "fixture-only-key", "invalid_credentials"},
			{403, "fixture-only-key", "permission_denied"},
			{429, "fixture-only-key", "rate_limited"},
			{500, "fixture-only-key", "upstream_error"},
		} {
			account := compatibleBalanceAccount(provider)
			account.Extra = map[string]any{provider + "_balance": 12.5, provider + "_balance_currency": ""}
			repo := &compatibleBalanceRepo{account: account}
			upstream := compatibleBalanceHTTP{send: func(*http.Request, string, int64, int) (*http.Response, error) {
				return &http.Response{StatusCode: scenario.status, Body: io.NopCloser(strings.NewReader(scenario.body))}, nil
			}}
			value, err := NewCNProviderBalanceService(repo, nil, upstream, nil).QueryAccountBalance(context.Background(), account.ID)
			require.NoError(test, err)
			result := requireCompatibleBalanceResult(test, value)
			require.False(test, result.Success)
			require.Nil(test, result.Balance)
			require.Empty(test, result.Balances)
			require.Equal(test, scenario.reason, result.Reason)
			require.NotContains(test, result.Error, "fixture-only-key")
			require.Empty(test, repo.writes)
			require.Equal(test, 12.5, account.Extra[provider+"_balance"])
		}
	}
}
