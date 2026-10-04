package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompatibleRouterBalanceRequestAndContract(test *testing.T) {
	for _, scenario := range []struct {
		provider, base, target, body, currency string
		amount                                 float64
	}{
		{"teamorouter", "https://api.teamorouter.cn/v1", "https://teamorouter.cn/v1/billing/balance", `{"balance":{"value":"85.320000","currency":"USD"}}`, "USD", 85.32},
		{"teamorouter_com", "https://api.teamorouter.com/v1", "https://teamorouter.com/v1/billing/balance", `{"balance":{"value":"85.320000","currency":"USD"}}`, "USD", 85.32},
		// SudoCode documents these paths rather than a complete JSON example.
		// Deliberately conflicting other amounts demonstrate the selected scope.
		{"sudocode_chat", "https://api.sudocode.chat/v1", "https://api.sudocode.chat/api/account/balance", `{"success":true,"data":{"remaining":300,"token":{"remaining":100},"ccswitch":{"remaining":12.5,"used":7.5,"total":20,"unit":"CNY"}}}`, "CNY", 12.5},
	} {
		test.Run(scenario.provider, func(test *testing.T) {
			for _, explicitBase := range []bool{false, true} {
				account := compatibleBalanceAccount(scenario.provider)
				if explicitBase {
					account.Credentials["base_url"] = scenario.base
					protocol := "anthropic"
					if scenario.provider == "teamorouter_com" {
						protocol = "chat_completions"
					}
					account.Credentials["api_base_urls"] = map[string]any{protocol: scenario.base}
				}
				repo := &compatibleBalanceRepo{account: account}
				calls := 0
				upstream := compatibleBalanceHTTP{send: func(request *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
					calls++
					require.Equal(test, scenario.target, request.URL.String())
					require.Equal(test, http.MethodGet, request.Method)
					require.Equal(test, "Bearer fixture-only-key", request.Header.Get("Authorization"))
					require.True(test, HTTPUpstreamRedirectsDisabled(request.Context()))
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(scenario.body))}, nil
				}}
				value, err := NewCNProviderBalanceService(repo, nil, upstream, nil).QueryAccountBalance(context.Background(), account.ID)
				require.NoError(test, err)
				result := requireCompatibleBalanceResult(test, value)
				require.Equal(test, 1, calls)
				require.True(test, result.Success)
				require.True(test, result.Persisted)
				require.Equal(test, scenario.amount, *result.Balance)
				require.Equal(test, scenario.currency, result.Currency)
				require.Len(test, repo.writes, 1)
				account.Extra = repo.writes[0]
				ClearAccountRuntimeObservations(account)
				require.Empty(test, account.Extra)
			}
		})
	}
}

func TestCompatibleRouterBalanceMalformedAndZero(test *testing.T) {
	for _, provider := range []string{"teamorouter", "teamorouter_com", "sudocode_chat"} {
		validBody := `{"balance":{"value":"0.000000","currency":"EUR"}}`
		currency := "EUR"
		invalidBodies := []string{
			`{}`, `{"balance":{"value":"0"}}`, `{"balance":{"currency":"USD"}}`,
			`{"balance":{"value":0,"currency":"USD"}}`, `{"balance":{"value":null,"currency":"USD"}}`,
			`{"balance":{"value":"NaN","currency":"USD"}}`, `{"balance":{"value":"1e400","currency":"USD"}}`,
			`{"balance":{"value":"0","currency":""}}`, `{"balance":{"value":"0","currency":123}}`,
			`{"balance":{"value":"0","currency":"USD"},"error":{"code":"invalid_api_key"}}`,
		}
		if provider == "sudocode_chat" {
			validBody = `{"success":true,"data":{"ccswitch":{"remaining":0,"unit":"Credits","used":20,"total":20}}}`
			currency = "Credits"
			invalidBodies = []string{
				`{}`, `{"success":true,"data":{"remaining":5,"ccswitch":{"unit":"CNY"}}}`,
				`{"success":true,"data":{"ccswitch":{"remaining":null,"unit":"CNY"}}}`,
				`{"success":true,"data":{"ccswitch":{"remaining":"0","unit":"CNY"}}}`,
				`{"success":true,"data":{"ccswitch":{"remaining":1e400,"unit":"CNY"}}}`,
				`{"success":true,"data":{"ccswitch":{"remaining":0}}}`,
				`{"success":true,"data":{"ccswitch":{"remaining":0,"unit":""}}}`,
				`{"success":false,"data":{"ccswitch":{"remaining":0,"unit":"CNY"}}}`,
				`{"data":{"ccswitch":{"remaining":0,"unit":"CNY"}}}`,
			}
		}
		entries, err := parseCompatibleBalance(provider, []byte(validBody))
		require.NoError(test, err)
		require.Equal(test, []CNProviderBalanceEntry{{Currency: currency, Balance: 0}}, entries)
		for _, body := range invalidBodies {
			entries, err := parseCompatibleBalance(provider, []byte(body))
			require.Error(test, err, "%s %s", provider, body)
			require.Empty(test, entries)
		}
		for _, status := range []int{200, 401, 403, 429, 500} {
			repo := &compatibleBalanceRepo{account: compatibleBalanceAccount(provider)}
			upstream := compatibleBalanceHTTP{send: func(*http.Request, string, int64, int) (*http.Response, error) {
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
			}}
			value, err := NewCNProviderBalanceService(repo, nil, upstream, nil).QueryAccountBalance(context.Background(), repo.account.ID)
			require.NoError(test, err)
			result := requireCompatibleBalanceResult(test, value)
			require.False(test, result.Success)
			require.Nil(test, result.Balance)
			require.Empty(test, repo.writes)
		}
	}
}

func TestCompatibleRouterBalanceRejectsWrongOriginBeforeSending(test *testing.T) {
	for _, provider := range []string{"teamorouter", "teamorouter_com", "sudocode_chat", "sudocode_us"} {
		wrongBases := []string{"https://relay.example/v1", "https://api.sudocode.us/v1"}
		switch provider {
		case "teamorouter":
			wrongBases = append(wrongBases, "https://api.teamorouter.com/v1", "https://teamorouter.cn/v1")
		case "teamorouter_com":
			wrongBases = append(wrongBases, "https://api.teamorouter.cn/v1", "https://teamorouter.com/v1")
		case "sudocode_chat":
			wrongBases = append(wrongBases, "https://api.sudocode.chat/api/account/balance")
		case "sudocode_us":
			wrongBases = append(wrongBases, "", "https://api.sudocode.chat/v1")
		}
		for _, base := range wrongBases {
			for _, field := range []string{"base_url", "api_base_urls"} {
				account := compatibleBalanceAccount(provider)
				account.Credentials[field] = base
				if field == "api_base_urls" {
					protocol := "anthropic"
					if provider == "teamorouter_com" {
						protocol = "chat_completions"
					}
					account.Credentials[field] = map[string]any{protocol: base}
				}
				repo := &compatibleBalanceRepo{account: account}
				upstream := compatibleBalanceHTTP{send: func(*http.Request, string, int64, int) (*http.Response, error) {
					test.Fatalf("untrusted origin sent: %s %s %s", provider, field, base)
					return nil, nil
				}}
				value, err := NewCNProviderBalanceService(repo, nil, upstream, nil).QueryAccountBalance(context.Background(), account.ID)
				require.NoError(test, err)
				result := requireCompatibleBalanceResult(test, value)
				require.True(test, result.Unsupported)
				require.Nil(test, result.Balance)
				require.Empty(test, repo.writes)
			}
		}
	}
}
