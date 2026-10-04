package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type compatibleBalanceRepo struct {
	AccountRepository
	account    *Account
	writes     []map[string]any
	persistErr error
}

func (repo *compatibleBalanceRepo) GetByID(context.Context, int64) (*Account, error) {
	return repo.account, nil
}
func (repo *compatibleBalanceRepo) UpdateAccountRuntimeSnapshot(_ context.Context, _ *Account, updates map[string]any, _ *time.Time) error {
	repo.writes = append(repo.writes, updates)
	return repo.persistErr
}

type compatibleBalanceHTTP struct {
	HTTPUpstream
	send func(*http.Request, string, int64, int) (*http.Response, error)
}

func (upstream compatibleBalanceHTTP) Do(request *http.Request, proxy string, accountID int64, concurrency int) (*http.Response, error) {
	return upstream.send(request, proxy, accountID, concurrency)
}

func compatibleBalanceAccount(provider string) *Account {
	return &Account{ID: 123, Platform: provider, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture-only-key"}}
}

func requireCompatibleBalanceResult(test *testing.T, value any) *CompatibleBalanceResult {
	test.Helper()
	result, valid := value.(*CompatibleBalanceResult)
	require.True(test, valid)
	require.NotNil(test, result)
	return result
}

func TestCompatibleBalanceRequestAndPersistence(test *testing.T) {
	for _, provider := range []string{"novita", "ppio", "siliconflow_cn", "siliconflow_global", "shengsuanyun"} {
		test.Run(provider, func(test *testing.T) {
			account := compatibleBalanceAccount(provider)
			proxyID := int64(9)
			account.ProxyID = &proxyID
			account.Proxy = &Proxy{Protocol: "http", Host: "proxy.example", Port: 8080}
			repo := &compatibleBalanceRepo{account: account}
			upstream := compatibleBalanceHTTP{send: func(request *http.Request, proxy string, identity int64, concurrency int) (*http.Response, error) {
				require.Equal(test, compatibleBalanceEndpoint(provider), request.URL.String())
				require.Equal(test, "Bearer fixture-only-key", request.Header.Get("Authorization"))
				require.Equal(test, "http://proxy.example:8080", proxy)
				require.Equal(test, account.ID, identity)
				require.Equal(test, 1, concurrency)
				require.True(test, HTTPUpstreamRedirectsDisabled(request.Context()))
				deadline, exists := request.Context().Deadline()
				require.True(test, exists)
				require.LessOrEqual(test, time.Until(deadline), cnBalanceUpstreamTimeout)
				body := `{"availableBalance":"10000"}`
				if strings.HasPrefix(provider, "siliconflow") {
					body = `{"code":20000,"status":true,"data":{"totalBalance":"1"}}`
				}
				if provider == "shengsuanyun" {
					body = `{"success":true,"data":{"gateway_type":"common","account_balance_cny":1}}`
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
			}}
			result, err := NewCNProviderBalanceService(repo, nil, upstream, nil).QueryAccountBalance(context.Background(), account.ID)
			require.NoError(test, err)
			balance := requireCompatibleBalanceResult(test, result)
			require.True(test, balance.Success)
			require.True(test, balance.Persisted)
			require.Equal(test, float64(1), *balance.Balance)
			require.Len(test, repo.writes, 1)
			require.Equal(test, float64(1), repo.writes[0][provider+"_balance"])
		})
	}
}

func TestCompatibleBalanceRejectsBeforeSending(test *testing.T) {
	for _, scenario := range []struct {
		name   string
		change func(*Account)
		policy bool
	}{
		{"malformed mode type", func(account *Account) { account.Credentials["account_mode"] = false }, false},
		{"malformed base type", func(account *Account) { account.Credentials["base_url"] = []string{"https://relay.example"} }, false},
		{"cross region", func(account *Account) {
			account.Platform = "siliconflow_cn"
			account.Credentials["base_url"] = "https://api.siliconflow.com/v1"
		}, false},
		{"coding", func(account *Account) { account.Credentials["account_mode"] = "coding" }, false},
		{"invalid mode", func(account *Account) { account.Credentials["account_mode"] = "invalid" }, false},
		{"invalid protocol", func(account *Account) { account.Credentials["api_protocol"] = "responses" }, false},
		{"arbitrary base", func(account *Account) { account.Credentials["base_url"] = "https://relay.example/openai/v1" }, false},
		{"other vendor", func(account *Account) { account.Credentials["base_url"] = "https://api.ppio.com/openai/v1" }, false},
		{"userinfo", func(account *Account) { account.Credentials["base_url"] = "https://key@api.novita.ai/openai/v1" }, false},
		{"query", func(account *Account) { account.Credentials["base_url"] = "https://api.novita.ai/openai/v1?key=secret" }, false},
		{"custom path", func(account *Account) { account.Credentials["base_url"] = "https://api.novita.ai/coding/v1" }, false},
		{"protocol override", func(account *Account) {
			account.Credentials["api_base_urls"] = map[string]any{"anthropic": "https://relay.example"}
		}, false},
		{"unknown provider endpoint", func(account *Account) { account.Platform = "together" }, false},
		{"Atlas coding", func(account *Account) {
			account.Platform = "atlascloud"
			account.Credentials["account_mode"] = "coding"
		}, false},
		{"missing key", func(account *Account) { delete(account.Credentials, "api_key") }, false},
		{"non API key", func(account *Account) { account.Type = "oauth" }, false},
		{"missing proxy", func(account *Account) { identity := int64(3); account.ProxyID = &identity }, false},
		{"unresolved group", func(account *Account) { identity := int64(3); account.ProxyGroupID = &identity }, false},
		{"operator allowlist", func(account *Account) {}, true},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			account := compatibleBalanceAccount("novita")
			scenario.change(account)
			repo := &compatibleBalanceRepo{account: account}
			upstream := compatibleBalanceHTTP{send: func(*http.Request, string, int64, int) (*http.Response, error) {
				test.Fatal("credential-bearing request escaped validation")
				return nil, nil
			}}
			var cfg *config.Config
			if scenario.policy {
				cfg = &config.Config{}
				cfg.Security.URLAllowlist.Enabled = true
				cfg.Security.URLAllowlist.UpstreamHosts = []string{"api.ppio.com"}
			}
			result, err := NewCNProviderBalanceService(repo, nil, upstream, cfg).QueryAccountBalance(context.Background(), account.ID)
			if err == nil {
				require.False(test, requireCompatibleBalanceResult(test, result).Success)
				require.Nil(test, requireCompatibleBalanceResult(test, result).Balance)
			}
			require.Empty(test, repo.writes)
		})
	}
}

func TestCompatibleBalanceFailuresNeverPersistZero(test *testing.T) {
	for _, scenario := range []struct {
		name, body   string
		status       int
		transportErr error
	}{
		{"missing", `{}`, 200, nil},
		{"bad JSON", `<html>fixture-only-key</html>`, 200, nil},
		{"auth", `fixture-only-key`, 401, nil},
		{"forbidden", `fixture-only-key`, 403, nil},
		{"rate limit", `fixture-only-key`, 429, nil},
		{"server error", `fixture-only-key`, 500, nil},
		{"oversize", strings.Repeat(" ", cnBalanceMaxBodyBytes) + `{"availableBalance":1}`, 200, nil},
		{"transport", ``, 0, errors.New("fixture-only-key")},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			repo := &compatibleBalanceRepo{account: compatibleBalanceAccount("novita")}
			upstream := compatibleBalanceHTTP{send: func(*http.Request, string, int64, int) (*http.Response, error) {
				return &http.Response{StatusCode: scenario.status, Body: io.NopCloser(strings.NewReader(scenario.body))}, scenario.transportErr
			}}
			value, err := NewCNProviderBalanceService(repo, nil, upstream, nil).QueryAccountBalance(context.Background(), repo.account.ID)
			require.NoError(test, err)
			result := requireCompatibleBalanceResult(test, value)
			require.False(test, result.Success)
			require.NotEmpty(test, result.Error)
			require.NotContains(test, result.Error, "fixture-only-key")
			require.Empty(test, repo.writes)
			encoded, err := json.Marshal(result)
			require.NoError(test, err)
			require.NotContains(test, string(encoded), `"balance":`)
		})
	}
}

func TestCompatibleBalanceRedirectNeverReachesAnotherHost(test *testing.T) {
	escaped := 0
	destination := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) { escaped++; writer.WriteHeader(500) }))
	defer destination.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, destination.URL, http.StatusFound)
	}))
	defer origin.Close()
	repo := &compatibleBalanceRepo{account: compatibleBalanceAccount("novita")}
	upstream := compatibleBalanceHTTP{send: func(request *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		require.True(test, HTTPUpstreamRedirectsDisabled(request.Context()))
		fixture, err := http.NewRequestWithContext(request.Context(), request.Method, origin.URL, nil)
		require.NoError(test, err)
		fixture.Header = request.Header.Clone()
		client := origin.Client()
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		return client.Do(fixture)
	}}
	value, err := NewCNProviderBalanceService(repo, nil, upstream, nil).QueryAccountBalance(context.Background(), repo.account.ID)
	require.NoError(test, err)
	require.Equal(test, http.StatusFound, requireCompatibleBalanceResult(test, value).StatusCode)
	require.False(test, requireCompatibleBalanceResult(test, value).Success)
	require.Zero(test, escaped)
	require.Empty(test, repo.writes)
}

func TestCompatibleBalanceParsing(test *testing.T) {
	for _, scenario := range []struct {
		name, provider, body, currency string
		balance                        float64
		valid                          bool
	}{
		{"sole credits", "soleapi", `{"remaining":131.4,"used":69.6,"total":201,"unit":"Credits"}`, "Credits", 131.4, true},
		{"sole missing", "soleapi", `{"unit":"Credits"}`, "", 0, false},
		{"sole unknown unit", "soleapi", `{"remaining":1,"unit":"USD"}`, "", 0, false},
		{"novita USD scale", "novita", `{"availableBalance":"12345"}`, "USD", 1.2345, true},
		{"ppio CNY scale", "ppio", `{"availableBalance":"12345"}`, "CNY", 1.2345, true},
		{"zero is real", "novita", `{"availableBalance":0}`, "USD", 0, true},
		{"missing", "novita", `{}`, "", 0, false},
		{"null", "novita", `{"availableBalance":null}`, "", 0, false},
		{"empty", "novita", `{"availableBalance":""}`, "", 0, false},
		{"nonfinite", "novita", `{"availableBalance":"NaN"}`, "", 0, false},
		{"hex float", "novita", `{"availableBalance":"0x1p4"}`, "", 0, false},
		{"explicit failure", "novita", `{"availableBalance":"0","success":false}`, "", 0, false},
		{"boolean", "ppio", `{"availableBalance":false}`, "", 0, false},
		{"malformed", "ppio", `{"availableBalance":1`, "", 0, false},
		{"CN total", "siliconflow_cn", `{"code":20000,"status":true,"data":{"balance":"2","chargeBalance":"3","totalBalance":"5"}}`, "CNY", 5, true},
		{"global total", "siliconflow_global", `{"code":20000,"status":true,"data":{"totalBalance":"0"}}`, "USD", 0, true},
		{"missing total", "siliconflow_cn", `{"code":20000,"status":true,"data":{"balance":"2"}}`, "", 0, false},
		{"error envelope", "siliconflow_cn", `{"code":40000,"status":false,"data":{"totalBalance":"2"}}`, "", 0, false},
		{"ordinary", "shengsuanyun", `{"success":true,"data":{"gateway_type":"common","account_balance_cny":12.5}}`, "CNY", 12.5, true},
		{"enterprise budget", "shengsuanyun", `{"success":true,"data":{"gateway_type":"enterprise","period_limit_cny":100}}`, "", 0, false},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			entries, err := parseCompatibleBalance(scenario.provider, []byte(scenario.body))
			if !scenario.valid {
				require.Error(test, err)
				require.Empty(test, entries)
				return
			}
			require.NoError(test, err)
			require.Equal(test, []CNProviderBalanceEntry{{Currency: scenario.currency, Balance: scenario.balance}}, entries)
		})
	}
}

func TestCompatibleBalanceSnapshotInvalidation(test *testing.T) {
	for _, provider := range []string{"novita", "ppio", "siliconflow_cn", "siliconflow_global", "shengsuanyun", "soleapi"} {
		account := compatibleBalanceAccount(provider)
		account.Extra = map[string]any{provider + "_balance": 2, provider + "_balances": []any{}, provider + "_balance_currency": "USD", provider + "_balance_updated_at": "old", "unrelated": true}
		ClearAccountRuntimeObservations(account)
		require.Equal(test, map[string]any{"unrelated": true}, account.Extra)
	}
}

func TestCompatibleBalancePersistenceFailureRemainsExplicit(test *testing.T) {
	repo := &compatibleBalanceRepo{account: compatibleBalanceAccount("soleapi"), persistErr: errors.New("store unavailable")}
	upstream := compatibleBalanceHTTP{send: func(request *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		require.Equal(test, "https://soleapi.com/v1/usage", request.URL.String())
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"remaining":0,"unit":"Credits"}`))}, nil
	}}
	value, err := NewCNProviderBalanceService(repo, nil, upstream, nil).QueryAccountBalance(context.Background(), repo.account.ID)
	require.NoError(test, err)
	result := requireCompatibleBalanceResult(test, value)
	require.True(test, result.Success)
	require.False(test, result.Persisted)
	require.Equal(test, "Credits", result.Currency)
	require.Zero(test, *result.Balance)
}

func TestCompatibleBalancePersistenceOutcomes(test *testing.T) {
	for _, provider := range []string{"soleapi", "aihubmix", "dmxapi", "crazyrouter", "crazyrouter_intl"} {
		for _, amount := range []int{0, 19} {
			for _, outcome := range []struct {
				name            string
				err             error
				identityChanged bool
			}{
				{"saved", nil, false},
				{"identity", ErrAccountRuntimeIdentityChanged, true},
				{"wrapped identity", fmt.Errorf("private-token-and-url: %w", ErrAccountRuntimeIdentityChanged), true},
				{"store error", errors.New("private-store-error"), false},
			} {
				test.Run(fmt.Sprintf("%s/%d/%s", provider, amount, outcome.name), func(test *testing.T) {
					account := compatibleBalanceAccount(provider)
					if provider != "soleapi" {
						account = managementAccount(provider)
					}
					account.Extra = map[string]any{provider + "_balance": 777, "unrelated": true}
					repo := &compatibleBalanceRepo{account: account, persistErr: outcome.err}
					upstream := compatibleBalanceHTTP{send: func(request *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
						body := fmt.Sprintf(`{"success":true,"data":{"quota":%d,"used_quota":%d}}`, amount*500000, amount*500000)
						if provider == "soleapi" {
							body = fmt.Sprintf(`{"remaining":%d,"unit":"Credits"}`, amount)
						} else if request.URL.Path == "/api/status" {
							body = `{"success":true,"data":{"quota_per_unit":500000}}`
						}
						return managementResponse(body, 200), nil
					}}
					value, err := NewCNProviderBalanceService(repo, nil, upstream, nil).QueryAccountBalance(context.Background(), account.ID)
					require.NoError(test, err)
					result := requireCompatibleBalanceResult(test, value)
					require.Len(test, repo.writes, 1)
					require.Equal(test, map[string]any{provider + "_balance": 777, "unrelated": true}, account.Extra)
					require.Equal(test, outcome.err == nil, result.Persisted)
					if outcome.identityChanged {
						requireCompatibleIdentityChanged(test, result)
					} else {
						require.True(test, result.Success)
						require.Empty(test, result.Reason)
						require.Empty(test, result.Error)
						require.NotNil(test, result.Balance)
						require.Equal(test, float64(amount), *result.Balance)
						require.Len(test, result.Balances, 1)
						if provider != "soleapi" && provider != "dmxapi" {
							require.NotNil(test, result.Used)
							require.Equal(test, float64(amount), *result.Used)
						}
					}
				})
			}
		}
	}
}

func requireCompatibleIdentityChanged(test *testing.T, result *CompatibleBalanceResult) {
	test.Helper()
	require.False(test, result.Success)
	require.False(test, result.Persisted)
	require.False(test, result.Unsupported)
	require.Equal(test, "identity_changed", result.Reason)
	require.Equal(test, "Account configuration changed; query again.", result.Error)
	require.Nil(test, result.Balance)
	require.Nil(test, result.Balances)
	require.Nil(test, result.Used)
	require.Nil(test, result.Credits)
	require.Nil(test, result.ProjectBudget)
	require.Empty(test, result.Currency)
	encoded, err := json.Marshal(result)
	require.NoError(test, err)
	for _, field := range []string{"balance", "balances", "used", "credits", "project_budget", "currency"} {
		require.NotContains(test, string(encoded), `"`+field+`":`)
	}
	require.NotContains(test, string(encoded), "private-")
}

func TestCompatibleBalanceIdentityChangedClearsAllObservations(test *testing.T) {
	amount := 19.0
	result := &CompatibleBalanceResult{
		Provider: "aihubmix", Kind: "wallet_balance", Scope: "account", FetchedAt: 123, StatusCode: 200,
		Success: true, Persisted: true, Balance: &amount, Used: &amount, Currency: "USD",
		Balances: []CNProviderBalanceEntry{{Balance: amount, Currency: "USD"}},
		Credits:  &CompatibleAccountCredits{TotalCredits: amount}, ProjectBudget: &CompatibleProjectBudget{},
	}
	result.applyPersistenceOutcome(fmt.Errorf("private-secret: %w", ErrAccountRuntimeIdentityChanged))
	requireCompatibleIdentityChanged(test, result)
	require.Equal(test, "aihubmix", result.Provider)
	require.Equal(test, "wallet_balance", result.Kind)
	require.Equal(test, "account", result.Scope)
	require.Equal(test, int64(123), result.FetchedAt)
	require.Equal(test, 200, result.StatusCode)
}

func TestCompatibleBalanceTheRouterDoesNotPersist(test *testing.T) {
	repo := &compatibleBalanceRepo{account: managementAccount("therouter"), persistErr: ErrAccountRuntimeIdentityChanged}
	upstream := compatibleBalanceHTTP{send: func(*http.Request, string, int64, int) (*http.Response, error) {
		return managementResponse(`{"data":{"total_credits":0,"total_usage":0}}`, 200), nil
	}}
	value, err := NewCNProviderBalanceService(repo, nil, upstream, nil).QueryAccountBalance(context.Background(), repo.account.ID)
	require.NoError(test, err)
	result := requireCompatibleBalanceResult(test, value)
	require.True(test, result.Success)
	require.False(test, result.Persisted)
	require.Equal(test, &CompatibleAccountCredits{Unit: "credits"}, result.Credits)
	require.Nil(test, result.Balance)
	require.Empty(test, repo.writes)
}

func TestCompatibleBalanceUsesResolvedGroup(test *testing.T) {
	account := compatibleBalanceAccount("ppio")
	groupID := int64(19)
	account.ProxyGroupID = &groupID
	account.egress = &accountEgress{target: EgressTarget{Resolved: true, URL: "http://group-proxy.example:8123", Source: EgressSourceGroupBinding}}
	repo := &compatibleBalanceRepo{account: account}
	upstream := compatibleBalanceHTTP{send: func(_ *http.Request, proxy string, _ int64, _ int) (*http.Response, error) {
		require.Equal(test, "http://group-proxy.example:8123", proxy)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"availableBalance":0}`))}, nil
	}}
	value, err := NewCNProviderBalanceService(repo, nil, upstream, nil).QueryAccountBalance(context.Background(), account.ID)
	require.NoError(test, err)
	require.True(test, requireCompatibleBalanceResult(test, value).Persisted)
}

type compatibleBalanceBrokenBody struct{}

func (compatibleBalanceBrokenBody) Read([]byte) (int, error) {
	return 0, errors.New("fixture read failure")
}
func (compatibleBalanceBrokenBody) Close() error { return nil }

func TestCompatibleBalanceBodyReadFailure(test *testing.T) {
	repo := &compatibleBalanceRepo{account: compatibleBalanceAccount("novita")}
	upstream := compatibleBalanceHTTP{send: func(*http.Request, string, int64, int) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: compatibleBalanceBrokenBody{}}, nil
	}}
	value, err := NewCNProviderBalanceService(repo, nil, upstream, nil).QueryAccountBalance(context.Background(), repo.account.ID)
	require.NoError(test, err)
	require.False(test, requireCompatibleBalanceResult(test, value).Success)
	require.Empty(test, repo.writes)
}

func TestCompatibleBalanceCallerCancellation(test *testing.T) {
	repo := &compatibleBalanceRepo{account: compatibleBalanceAccount("novita")}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	upstream := compatibleBalanceHTTP{send: func(request *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		require.ErrorIs(test, request.Context().Err(), context.Canceled)
		return nil, request.Context().Err()
	}}
	value, err := NewCNProviderBalanceService(repo, nil, upstream, nil).QueryAccountBalance(ctx, repo.account.ID)
	require.NoError(test, err)
	require.False(test, requireCompatibleBalanceResult(test, value).Success)
	require.Empty(test, repo.writes)
}

func TestCompatibleBalanceObservationKeys(t *testing.T) {
	providers := []string{"novita", "ppio", "siliconflow_cn", "siliconflow_global", "shengsuanyun", "soleapi", "atlascloud", "stepfun_api", "stepfun_api_intl", "teamorouter", "teamorouter_com", "sudocode_chat", "aihubmix", "dmxapi", "crazyrouter", "crazyrouter_intl", "therouter"}
	var expected []string
	extra := map[string]any{"quota_used": 17, "configuration": "keep"}
	for _, provider := range providers {
		for _, suffix := range []string{"balance", "balance_currency", "balances", "balance_updated_at"} {
			key := provider + "_" + suffix
			expected = append(expected, key)
			extra[key] = "old observation"
		}
	}
	require.ElementsMatch(t, expected, CompatibleBalanceObservationKeys())
	clearCompatibleBalanceSnapshots(extra)
	require.Equal(t, map[string]any{"quota_used": 17, "configuration": "keep"}, extra)
	keys := CompatibleBalanceObservationKeys()
	keys[0] = "changed by caller"
	require.ElementsMatch(t, expected, CompatibleBalanceObservationKeys())
}
