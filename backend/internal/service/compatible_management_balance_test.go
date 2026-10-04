package service

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func managementAccount(provider string) *Account {
	a := compatibleBalanceAccount(provider)
	a.Credentials["usage_query_token"] = "fixture-management-only"
	a.Credentials["usage_query_user_id"] = "42"
	return a
}
func managementResponse(body string, status int) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}
}
func TestCompatibleManagementRequests(test *testing.T) {
	for _, tc := range []struct {
		provider, target, auth, userHeader, currency, body string
		balance, used                                      float64
	}{
		{"aihubmix", "https://aihubmix.com/api/user/self", "fixture-management-only", "", "USD", `{"success":true,"data":{"quota":6250000,"used_quota":625000}}`, 12.5, 1.25},
		{"dmxapi", "https://www.dmxapi.cn/api/user/self", "Bearer fixture-management-only", "Dmx-Api-User", "CNY", `{"success":true,"data":{"quota":6250000}}`, 12.5, 0},
		{"crazyrouter", "https://crazyrouter.com/api/user/self", "Bearer fixture-management-only", "New-Api-User", "USD", `{"success":true,"data":{"quota":6250000,"used_quota":625000}}`, 25, 2.5},
		{"crazyrouter_intl", "https://crazyrouter.com/api/user/self", "Bearer fixture-management-only", "New-Api-User", "USD", `{"success":true,"data":{"quota":6250000,"used_quota":625000}}`, 25, 2.5},
		{"therouter", "https://api.therouter.ai/api/v1/credits", "Bearer fixture-management-only", "", "", `{"data":{"total_credits":950.5,"total_usage":49.5}}`, 0, 0},
	} {
		test.Run(tc.provider, func(test *testing.T) {
			a := managementAccount(tc.provider)
			// Management querying does not require even a valid inference key.
			a.Credentials["api_key"] = "inference-key-must-never-escape"
			a.Credentials["header_overrides"] = map[string]any{"Authorization": "override-must-not-escape"}
			proxyID := int64(9)
			a.ProxyID = &proxyID
			a.Proxy = &Proxy{Protocol: "http", Host: "proxy.example", Port: 8080}
			repo := &compatibleBalanceRepo{account: a}
			calls := 0
			upstream := compatibleBalanceHTTP{send: func(req *http.Request, proxy string, id int64, concurrency int) (*http.Response, error) {
				calls++
				require.Equal(test, "GET", req.Method)
				require.Equal(test, "http://proxy.example:8080", proxy)
				require.Equal(test, a.ID, id)
				require.True(test, HTTPUpstreamRedirectsDisabled(req.Context()))
				deadline, ok := req.Context().Deadline()
				require.True(test, ok)
				require.LessOrEqual(test, time.Until(deadline), cnBalanceUpstreamTimeout)
				require.NotContains(test, req.URL.String(), "inference-key")
				headers, _ := json.Marshal(req.Header)
				require.NotContains(test, string(headers), "inference-key")
				require.NotContains(test, string(headers), "override-must")
				if calls == 2 {
					require.Equal(test, "https://crazyrouter.com/api/status", req.URL.String())
					require.Empty(test, req.Header.Get("Authorization"))
					require.Empty(test, req.Header.Get("New-Api-User"))
					return managementResponse(`{"success":true,"data":{"quota_per_unit":250000,"quota_display_type":"USD"}}`, 200), nil
				}
				require.Equal(test, tc.target, req.URL.String())
				require.Equal(test, tc.auth, req.Header.Get("Authorization"))
				require.Equal(test, "application/json", req.Header.Get("Content-Type"))
				if tc.userHeader != "" {
					require.Equal(test, "42", req.Header.Get(tc.userHeader))
				}
				return managementResponse(tc.body, 200), nil
			}}
			value, err := NewCNProviderBalanceService(repo, nil, upstream, nil).QueryAccountBalance(context.Background(), a.ID)
			require.NoError(test, err)
			result := requireCompatibleBalanceResult(test, value)
			require.True(test, result.Success, result.Error)
			require.Equal(test, "account", result.Scope)
			if tc.provider == "therouter" {
				require.Equal(test, "account_credits", result.Kind)
				require.Nil(test, result.Balance)
				require.Empty(test, result.Balances)
				require.Empty(test, result.Currency)
				require.Equal(test, &CompatibleAccountCredits{TotalCredits: 950.5, TotalUsage: 49.5, Unit: "credits"}, result.Credits)
				require.False(test, result.Persisted)
				require.Empty(test, repo.writes)
			} else {
				require.Equal(test, tc.balance, *result.Balance)
				require.Equal(test, tc.currency, result.Currency)
				require.True(test, result.Persisted)
				require.Len(test, repo.writes, 1)
				require.Len(test, repo.writes[0], 4)
				require.Equal(test, tc.balance, repo.writes[0][tc.provider+"_balance"])
				if tc.provider == "dmxapi" {
					require.Nil(test, result.Used)
				} else {
					require.Equal(test, tc.used, *result.Used)
				}
			}
			if strings.HasPrefix(tc.provider, "crazyrouter") {
				require.Equal(test, 2, calls)
			} else {
				require.Equal(test, 1, calls)
			}
		})
	}
}

func TestCompatibleManagementEligibility(test *testing.T) {
	for _, provider := range []string{"aihubmix", "dmxapi", "crazyrouter", "crazyrouter_intl", "therouter"} {
		for _, tc := range []struct {
			name, reason string
			mutate       func(*Account)
		}{
			{"missing token", "missing_query_credentials", func(a *Account) { delete(a.Credentials, "usage_query_token") }},
			{"empty token", "missing_query_credentials", func(a *Account) { a.Credentials["usage_query_token"] = "" }},
			{"null token", "missing_query_credentials", func(a *Account) { a.Credentials["usage_query_token"] = nil }},
			{"bad token", "invalid_credentials", func(a *Account) { a.Credentials["usage_query_token"] = "token\r\nvalue" }},
			{"token space", "invalid_credentials", func(a *Account) { a.Credentials["usage_query_token"] = " token" }},
			{"token type", "invalid_credentials", func(a *Account) { a.Credentials["usage_query_token"] = 42 }},
			{"subscription", "mode_unsupported", func(a *Account) { a.Credentials["account_mode"] = "coding" }},
			{"non payg plan", "invalid_credentials", func(a *Account) { a.Credentials["billing_plan"] = "unknown-plan" }},
			{"custom base", "custom_origin", func(a *Account) { a.Credentials["base_url"] = "https://relay.example/v1" }},
			{"custom protocol base", "custom_origin", func(a *Account) {
				a.Credentials["api_base_urls"] = map[string]any{"chat_completions": "https://relay.example/v1"}
			}},
			{"custom relay", "custom_origin", func(a *Account) { a.Extra = map[string]any{"custom_base_url_enabled": true} }},
		} {
			test.Run(provider+"/"+tc.name, func(test *testing.T) {
				a := managementAccount(provider)
				tc.mutate(a)
				repo := &compatibleBalanceRepo{account: a}
				upstream := compatibleBalanceHTTP{send: func(*http.Request, string, int64, int) (*http.Response, error) {
					test.Fatal("unexpected upstream connection")
					return nil, nil
				}}
				value, err := NewCNProviderBalanceService(repo, nil, upstream, nil).QueryAccountBalance(context.Background(), a.ID)
				require.NoError(test, err)
				result := requireCompatibleBalanceResult(test, value)
				require.False(test, result.Success)
				require.Equal(test, tc.reason, result.Reason)
				require.Nil(test, result.Balance)
				require.Nil(test, result.Credits)
				require.Empty(test, repo.writes)
				if tc.reason == "missing_query_credentials" {
					require.False(test, result.Unsupported)
					if provider == "therouter" {
						require.Equal(test, "account_credits", result.Kind)
					} else {
						require.Equal(test, "wallet_balance", result.Kind)
					}
				}
			})
		}
	}
	for _, provider := range []string{"dmxapi", "crazyrouter", "crazyrouter_intl"} {
		for _, user := range []any{nil, "", "  ", "bad id", "42\n", 42} {
			a := managementAccount(provider)
			a.Credentials["usage_query_user_id"] = user
			_, err := managementQueryHeaders(a)
			require.Error(test, err)
			expected := "invalid_credentials"
			if user == nil || user == "" || user == "  " {
				expected = "missing_query_credentials"
			}
			require.Equal(test, expected, compatibleQueryReason(err))
		}
	}
	for _, provider := range []string{"dmxapi_com", "dmxapi_ssvip"} {
		a := managementAccount(provider)
		repo := &compatibleBalanceRepo{account: a}
		upstream := compatibleBalanceHTTP{send: func(*http.Request, string, int64, int) (*http.Response, error) {
			test.Fatal("cross-region query")
			return nil, nil
		}}
		value, err := NewCNProviderBalanceService(repo, nil, upstream, nil).QueryAccountBalance(context.Background(), a.ID)
		require.NoError(test, err)
		require.Equal(test, "no_verified_query", requireCompatibleBalanceResult(test, value).Reason)
	}
}

func TestCompatibleManagementResponses(test *testing.T) {
	for _, tc := range []struct {
		provider, body, metadata string
		valid                    bool
	}{
		{"aihubmix", `{"success":true,"data":{"quota":0,"used_quota":0}}`, "", true},
		{"dmxapi", `{"success":true,"data":{"quota":0}}`, "", true},
		{"therouter", `{"data":{"total_credits":0,"total_usage":0}}`, "", true},
		{"therouter", `{"data":{"total_credits":1}}`, "", false},
		{"therouter", `{"success":false,"data":{"total_credits":1,"total_usage":0}}`, "", false},
		{"aihubmix", `{"data":{"quota":1,"used_quota":0}}`, "", false},
		{"aihubmix", `{"success":true,"data":{"quota":1}}`, "", false},
		{"dmxapi", `{"success":true,"data":{"quota":"1"}}`, "", false},
		{"dmxapi", `{"success":false,"data":{"quota":1}}`, "", false},
		{"crazyrouter", `{"success":true,"data":{"quota":10,"used_quota":2}}`, `{"success":true,"data":{"quota_per_unit":2}}`, true},
		{"crazyrouter", `{"success":true,"data":{"quota":10,"used_quota":2}}`, `{"success":true,"data":{}}`, false},
		{"crazyrouter", `{"success":true,"data":{"quota":10,"used_quota":2}}`, `{"success":true,"data":{"quota_per_unit":0}}`, false},
		{"crazyrouter", `{"success":true,"data":{"quota":10,"used_quota":2}}`, `{"success":true,"data":{"quota_per_unit":2,"quota_display_type":"CNY"}}`, false},
		{"crazyrouter", `{"success":true,"data":{"quota":1e308,"used_quota":2}}`, `{"success":true,"data":{"quota_per_unit":1e-308}}`, false},
	} {
		result := &CompatibleBalanceResult{Provider: tc.provider}
		err := parseManagementBalance(result, []byte(tc.body), []byte(tc.metadata))
		if tc.valid {
			require.NoError(test, err, tc.body)
		} else {
			require.Error(test, err, tc.body)
			require.Nil(test, result.Balance)
			require.Nil(test, result.Used)
			require.Nil(test, result.Credits)
		}
	}
}

func TestCompatibleManagementFailures(test *testing.T) {
	for _, provider := range []string{"aihubmix", "dmxapi", "crazyrouter", "crazyrouter_intl", "therouter"} {
		for _, tc := range []struct {
			status       int
			reason, body string
			err          error
		}{
			{401, "invalid_credentials", "fixture-management-only", nil}, {403, "permission_denied", "fixture-management-only", nil}, {429, "rate_limited", "fixture-management-only", nil}, {500, "upstream_error", "fixture-management-only", nil}, {302, "upstream_error", "", nil},
			{200, "invalid_response", "{}", nil}, {200, "invalid_response", strings.Repeat("x", cnBalanceMaxBodyBytes+1), nil}, {0, "upstream_error", "", errors.New("fixture-management-only")},
		} {
			repo := &compatibleBalanceRepo{account: managementAccount(provider)}
			upstream := compatibleBalanceHTTP{send: func(*http.Request, string, int64, int) (*http.Response, error) {
				return managementResponse(tc.body, tc.status), tc.err
			}}
			value, err := NewCNProviderBalanceService(repo, nil, upstream, nil).QueryAccountBalance(context.Background(), repo.account.ID)
			require.NoError(test, err)
			result := requireCompatibleBalanceResult(test, value)
			require.False(test, result.Success)
			require.Equal(test, tc.reason, result.Reason)
			require.Empty(test, repo.writes)
			encoded, err := json.Marshal(result)
			require.NoError(test, err)
			require.NotContains(test, string(encoded), "fixture-management-only")
			require.NotContains(test, string(encoded), `"balance":`)
			require.NotContains(test, string(encoded), `"credits":`)
		}
	}
}

func TestCompatibleManagementPolicyAndCAS(test *testing.T) {
	a := managementAccount("aihubmix")
	delete(a.Credentials, "api_key")
	repo := &compatibleBalanceRepo{account: a, persistErr: errors.New("identity changed")}
	calls := 0
	upstream := compatibleBalanceHTTP{send: func(*http.Request, string, int64, int) (*http.Response, error) {
		calls++
		return managementResponse(`{"success":true,"data":{"quota":0,"used_quota":0}}`, 200), nil
	}}
	value, err := NewCNProviderBalanceService(repo, nil, upstream, nil).QueryAccountBalance(context.Background(), a.ID)
	require.NoError(test, err)
	result := requireCompatibleBalanceResult(test, value)
	require.True(test, result.Success)
	require.False(test, result.Persisted)
	require.Equal(test, 1, calls)
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.Enabled = true
	cfg.Security.URLAllowlist.UpstreamHosts = []string{"unrelated.example"}
	value, err = NewCNProviderBalanceService(repo, nil, upstream, cfg).QueryAccountBalance(context.Background(), a.ID)
	require.NoError(test, err)
	require.False(test, requireCompatibleBalanceResult(test, value).Success)
	require.Equal(test, 1, calls)
	extra := map[string]any{"unrelated": true}
	for _, provider := range []string{"aihubmix", "dmxapi", "crazyrouter", "crazyrouter_intl", "therouter"} {
		for _, suffix := range []string{"balance", "balance_currency", "balances", "balance_updated_at"} {
			extra[provider+"_"+suffix] = 1
		}
	}
	clearCompatibleBalanceSnapshots(extra)
	require.Equal(test, map[string]any{"unrelated": true}, extra)
}

func TestCompatibleManagementWireAndMissingUser(test *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		calls++
		require.Equal(test, "/api/user/self", req.URL.Path)
		require.Equal(test, "Bearer fixture-management-only", req.Header.Get("Authorization"))
		require.Equal(test, "42", req.Header.Get("Dmx-Api-User"))
		_, _ = w.Write([]byte(`{"success":true,"data":{"quota":0}}`))
	}))
	defer server.Close()
	target, _ := url.Parse(server.URL)
	upstream := compatibleBalanceHTTP{send: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		require.Equal(test, "www.dmxapi.cn", req.URL.Host)
		copied := req.Clone(req.Context())
		copyURL := *req.URL
		copied.URL = &copyURL
		copied.URL.Scheme = target.Scheme
		copied.URL.Host = target.Host
		return server.Client().Do(copied)
	}}
	a := managementAccount("dmxapi")
	repo := &compatibleBalanceRepo{account: a}
	service := NewCNProviderBalanceService(repo, nil, upstream, nil)
	for _, user := range []any{nil, "", "  "} {
		a.Credentials["usage_query_user_id"] = user
		result, err := service.QueryAccountBalance(context.Background(), a.ID)
		require.NoError(test, err)
		require.Equal(test, "missing_query_credentials", requireCompatibleBalanceResult(test, result).Reason)
	}
	require.Zero(test, calls)
	a.Credentials["usage_query_user_id"] = "42"
	result, err := service.QueryAccountBalance(context.Background(), a.ID)
	require.NoError(test, err)
	require.True(test, requireCompatibleBalanceResult(test, result).Success)
	require.Equal(test, 1, calls)
}

type managementIdentityRepo struct {
	AccountRepository
	queried, current *Account
	writes           int
}

func (r *managementIdentityRepo) GetByID(context.Context, int64) (*Account, error) {
	return r.queried, nil
}
func (r *managementIdentityRepo) UpdateAccountRuntimeSnapshot(_ context.Context, expected *Account, updates map[string]any, _ *time.Time) error {
	if AccountRuntimeIdentityKey(expected) != AccountRuntimeIdentityKey(r.current) {
		return ErrAccountRuntimeIdentityChanged
	}
	r.writes++
	for key, value := range updates {
		r.current.Extra[key] = value
	}
	return nil
}
func TestCompatibleManagementIdentityChanges(test *testing.T) {
	for _, field := range []string{"usage_query_user_id", CredentialSecretVersionKey} {
		old := managementAccount("crazyrouter")
		old.Credentials[CredentialSecretReferenceKey] = "fixture-reference"
		old.Credentials[CredentialSecretVersionKey] = int64(1)
		current := *old
		current.Credentials = map[string]any{}
		for key, value := range old.Credentials {
			current.Credentials[key] = value
		}
		current.Extra = map[string]any{"crazyrouter_balance": 777, "unrelated": true}
		repo := &managementIdentityRepo{queried: old, current: &current}
		upstream := compatibleBalanceHTTP{send: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
			if req.URL.Path == "/api/status" {
				if field == CredentialSecretVersionKey {
					current.Credentials[field] = int64(2)
				} else {
					current.Credentials[field] = "43"
				}
				return managementResponse(`{"success":true,"data":{"quota_per_unit":100}}`, 200), nil
			}
			return managementResponse(`{"success":true,"data":{"quota":100,"used_quota":20}}`, 200), nil
		}}
		value, err := NewCNProviderBalanceService(repo, nil, upstream, nil).QueryAccountBalance(context.Background(), old.ID)
		require.NoError(test, err)
		result := requireCompatibleBalanceResult(test, value)
		require.False(test, result.Success)
		require.False(test, result.Persisted)
		require.Equal(test, "identity_changed", result.Reason)
		require.Nil(test, result.Balance)
		require.Nil(test, result.Used)
		require.Empty(test, result.Balances)
		require.Zero(test, repo.writes)
		require.Equal(test, map[string]any{"crazyrouter_balance": 777, "unrelated": true}, current.Extra)
	}
}

func TestCompatibleManagementCrossRegionAndStatusFailure(test *testing.T) {
	for _, tc := range []struct{ provider, base string }{
		{"dmxapi", "https://www.dmxapi.com/v1"}, {"dmxapi", "https://ssvip.dmxapi.com/v1"},
		{"crazyrouter", "https://api.crazyrouter.com/v1"}, {"crazyrouter_intl", "https://cn.crazyrouter.com/v1"},
	} {
		a := managementAccount(tc.provider)
		a.Credentials["base_url"] = tc.base
		_, err := compatibleQueryTarget(a)
		require.Error(test, err)
		require.Equal(test, "custom_origin", compatibleQueryReason(err))
	}
	for _, status := range []int{401, 403, 429, 500} {
		repo := &compatibleBalanceRepo{account: managementAccount("crazyrouter")}
		upstream := compatibleBalanceHTTP{send: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
			if req.URL.Path == "/api/status" {
				return managementResponse("fixture-management-only", status), nil
			}
			return managementResponse(`{"success":true,"data":{"quota":100,"used_quota":20}}`, 200), nil
		}}
		value, err := NewCNProviderBalanceService(repo, nil, upstream, nil).QueryAccountBalance(context.Background(), repo.account.ID)
		require.NoError(test, err)
		result := requireCompatibleBalanceResult(test, value)
		require.False(test, result.Success)
		require.Nil(test, result.Balance)
		require.Nil(test, result.Used)
		require.Empty(test, repo.writes)
		require.NotContains(test, result.Error, "fixture-management-only")
	}
}
