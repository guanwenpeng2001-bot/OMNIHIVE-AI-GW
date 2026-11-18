package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/stretchr/testify/require"
)

// Synthetic public-document examples, never real account responses.
const managementAzureFixture = `{"value":[{"name":{"value":"OpenAI.Standard.model","localizedValue":"Tokens Per Minute (thousands)"},"currentValue":3,"limit":200,"unit":"Count"}]}`
const managementGitHubFixture = `{"timePeriod":{"year":2026,"month":10},"user":"octocat","usageItems":[{"product":"Copilot","sku":"Copilot Premium Request","model":"GPT-5","unitType":"requests","pricePerUnit":0.04,"grossQuantity":100,"grossAmount":4,"discountQuantity":25,"discountAmount":1,"netQuantity":75,"netAmount":3}]}`

func managementFixtureAccount(provider string) *Account {
	account := compatibleBalanceAccount(provider)
	account.Credentials["usage_query_token"] = "management-fixture-only"
	account.Credentials["usage_query_subscription_id"] = "00000000-0000-0000-0000-000000000000"
	account.Credentials["usage_query_region"] = "eastus"
	account.Credentials["usage_query_username"] = "octocat"
	return account
}
func managementFixtureResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}
}
func managementCurrentGitHubFixture() string {
	now := time.Now().UTC()
	return strings.Replace(managementGitHubFixture, `"year":2026,"month":10`, fmt.Sprintf(`"year":%d,"month":%d`, now.Year(), now.Month()), 1)
}

func TestManagementUsageRequests(test *testing.T) {
	for _, provider := range []string{"azure_openai", "github_copilot"} {
		for _, grouped := range []bool{false, true} {
			test.Run(fmt.Sprintf("%s/group=%v", provider, grouped), func(test *testing.T) {
				account := managementFixtureAccount(provider)
				identity := int64(9)
				if grouped {
					account.ProxyGroupID = &identity
					account.egress = &accountEgress{target: EgressTarget{Resolved: true, URL: "http://proxy.example:8080", Source: EgressSourceGroupBinding}}
				} else {
					account.ProxyID = &identity
					account.Proxy = &Proxy{Protocol: "http", Host: "proxy.example", Port: 8080}
				}
				account.Extra = map[string]any{"header_overrides": map[string]any{"Authorization": "inference-secret", "X-Secret": "secret"}}
				repo := &compatibleBalanceRepo{account: account}
				calls := 0
				upstream := compatibleBalanceHTTP{send: func(request *http.Request, proxy string, accountID int64, concurrency int) (*http.Response, error) {
					calls++
					require.Equal(test, http.MethodGet, request.Method)
					require.Equal(test, "Bearer management-fixture-only", request.Header.Get("Authorization"))
					require.Empty(test, request.Header.Get("api-key"))
					require.Empty(test, request.Header.Get("X-Secret"))
					require.NotContains(test, request.URL.String(), "fixture")
					require.Equal(test, "http://proxy.example:8080", proxy)
					require.Equal(test, account.ID, accountID)
					require.Equal(test, 1, concurrency)
					require.True(test, HTTPUpstreamRedirectsDisabled(request.Context()))
					deadline, exists := request.Context().Deadline()
					require.True(test, exists)
					require.LessOrEqual(test, time.Until(deadline), cnBalanceUpstreamTimeout)
					if provider == "azure_openai" {
						require.Equal(test, "management.azure.com", request.URL.Host)
						require.Equal(test, "/subscriptions/00000000-0000-0000-0000-000000000000/providers/Microsoft.CognitiveServices/locations/eastus/usages", request.URL.Path)
						require.Equal(test, url.Values{"api-version": {"2024-10-01"}}, request.URL.Query())
						return managementFixtureResponse(200, managementAzureFixture), nil
					}
					require.Equal(test, "api.github.com", request.URL.Host)
					kind := "premium_request"
					if calls == 2 {
						kind = "ai_credit"
					}
					require.Equal(test, "/users/octocat/settings/billing/"+kind+"/usage", request.URL.Path)
					require.Equal(test, "2026-03-10", request.Header.Get("X-GitHub-Api-Version"))
					require.Equal(test, "application/vnd.github+json", request.Header.Get("Accept"))
					require.Equal(test, "sub2api-management-usage", request.Header.Get("User-Agent"))
					require.Len(test, request.URL.Query(), 2)
					return managementFixtureResponse(200, managementCurrentGitHubFixture()), nil
				}}
				result, err := NewCNProviderBalanceService(repo, nil, upstream, nil).QueryManagementUsage(context.Background(), account.ID, nil)
				require.NoError(test, err)
				require.True(test, result.Success, "%+v", result)
				require.False(test, result.Partial)
				require.False(test, result.Persisted)
				require.Empty(test, repo.writes)
				if provider == "azure_openai" {
					require.Equal(test, float64(3), *result.Quota.Value[0].CurrentValue)
					require.Equal(test, "Count", *result.Quota.Value[0].Unit)
					require.Equal(test, 1, calls)
				} else {
					require.Equal(test, 2, calls)
					require.Equal(test, float64(3), *result.Billing.Reports[0].UsageItems[0].NetAmount)
				}
				encoded, _ := json.Marshal(result)
				require.NotContains(test, string(encoded), "USD")
				require.NotContains(test, string(encoded), "balance")
				require.NotContains(test, string(encoded), "fixture-only")
			})
		}
	}
}

func TestManagementUsageZeroOutbound(test *testing.T) {
	for _, scenario := range []struct {
		name, provider, field string
		value                 any
		reason                string
	}{
		{"no management token", "azure_openai", "usage_query_token", nil, "missing_query_credentials"},
		{"empty management token", "github_copilot", "usage_query_token", "", "missing_query_credentials"},
		{"no subscription", "azure_openai", "usage_query_subscription_id", nil, "missing_query_credentials"},
		{"no region", "azure_openai", "usage_query_region", nil, "missing_query_credentials"},
		{"no username", "github_copilot", "usage_query_username", nil, "missing_query_credentials"},
		{"subscription injection", "azure_openai", "usage_query_subscription_id", "../other?secret", "invalid_credentials"},
		{"region injection", "azure_openai", "usage_query_region", "eastus%2F..", "invalid_credentials"},
		{"username injection", "github_copilot", "usage_query_username", "../orgs/other", "invalid_credentials"},
		{"token header injection", "github_copilot", "usage_query_token", "token\r\nheader", "invalid_credentials"},
		{"wrong token type", "azure_openai", "usage_query_token", 42, "invalid_credentials"},
		{"wrong mode", "azure_openai", "account_mode", "coding", "mode_unsupported"},
		{"wrong auth", "github_copilot", "auth_mode", "oauth", "mode_unsupported"},
		{"org paid", "github_copilot", "billing_plan", "business", "mode_unsupported"},
		{"sovereign cloud", "azure_openai", "base_url", "https://resource.openai.azure.us/openai/v1", "custom_origin"},
		{"foreign host", "azure_openai", "base_url", "https://resource.openai.azure.com.evil.test", "custom_origin"},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			account := managementFixtureAccount(scenario.provider)
			account.Credentials[scenario.field] = scenario.value
			upstream := compatibleBalanceHTTP{send: func(*http.Request, string, int64, int) (*http.Response, error) {
				test.Fatal("unexpected outbound request")
				return nil, nil
			}}
			result, err := NewCNProviderBalanceService(&compatibleBalanceRepo{account: account}, nil, upstream, nil).QueryManagementUsage(context.Background(), account.ID, nil)
			require.NoError(test, err)
			require.False(test, result.Success)
			require.Equal(test, scenario.reason, result.Reason)
		})
	}
	for _, guard := range []string{"type", "proxy", "proxy group", "policy"} {
		test.Run(guard, func(test *testing.T) {
			account := managementFixtureAccount("azure_openai")
			identity := int64(1)
			var cfg *config.Config
			switch guard {
			case "type":
				account.Type = "oauth"
			case "proxy":
				account.ProxyID = &identity
			case "proxy group":
				account.ProxyGroupID = &identity
			case "policy":
				cfg = &config.Config{}
				cfg.Security.URLAllowlist.Enabled = true
				cfg.Security.URLAllowlist.UpstreamHosts = []string{"other.example"}
			}
			upstream := compatibleBalanceHTTP{send: func(*http.Request, string, int64, int) (*http.Response, error) {
				test.Fatal("unexpected outbound request")
				return nil, nil
			}}
			result, err := NewCNProviderBalanceService(&compatibleBalanceRepo{account: account}, nil, upstream, cfg).QueryManagementUsage(context.Background(), account.ID, nil)
			require.NoError(test, err)
			require.False(test, result.Success)
			require.NotEmpty(test, result.Reason)
		})
	}
}

func TestManagementUsageFailuresAndPartial(test *testing.T) {
	for _, status := range []int{301, 401, 403, 404, 429, 500} {
		for _, provider := range []string{"azure_openai", "github_copilot"} {
			account := managementFixtureAccount(provider)
			calls := 0
			upstream := compatibleBalanceHTTP{send: func(*http.Request, string, int64, int) (*http.Response, error) {
				calls++
				if provider == "github_copilot" && calls == 1 {
					return managementFixtureResponse(200, managementCurrentGitHubFixture()), nil
				}
				return managementFixtureResponse(status, `{"error":"secret-token-and-url"}`), nil
			}}
			result, err := NewCNProviderBalanceService(&compatibleBalanceRepo{account: account}, nil, upstream, nil).QueryManagementUsage(context.Background(), account.ID, nil)
			require.NoError(test, err)
			require.False(test, result.Success)
			if provider == "github_copilot" {
				require.True(test, result.Partial)
				require.True(test, result.Billing.Reports[0].Success)
				require.False(test, result.Billing.Reports[1].Success)
				require.Equal(test, status, result.Billing.Reports[1].StatusCode)
			} else {
				require.Equal(test, status, result.StatusCode)
			}
			encoded, _ := json.Marshal(result)
			require.NotContains(test, string(encoded), "secret-token-and-url")
		}
	}
	for _, body := range []string{`null`, `{"value":{}}`, `{"value":[{"name":{"value":"model"},"currentValue":"3"}]}`, strings.Repeat("x", cnBalanceMaxBodyBytes+1)} {
		account := managementFixtureAccount("azure_openai")
		upstream := compatibleBalanceHTTP{send: func(*http.Request, string, int64, int) (*http.Response, error) {
			return managementFixtureResponse(200, body), nil
		}}
		result, err := NewCNProviderBalanceService(&compatibleBalanceRepo{account: account}, nil, upstream, nil).QueryManagementUsage(context.Background(), account.ID, nil)
		require.NoError(test, err)
		require.Equal(test, "invalid_response", result.Reason)
		require.Nil(test, result.Quota)
	}
	account := managementFixtureAccount("azure_openai")
	upstream := compatibleBalanceHTTP{send: func(*http.Request, string, int64, int) (*http.Response, error) {
		return nil, errors.New("secret transport URL")
	}}
	result, err := NewCNProviderBalanceService(&compatibleBalanceRepo{account: account}, nil, upstream, nil).QueryManagementUsage(context.Background(), account.ID, nil)
	// No raw transport error can escape via a result.
	require.NoError(test, err)
	require.Equal(test, "upstream_error", result.Reason)
}

func TestManagementAzurePagination(test *testing.T) {
	initial := "https://management.azure.com/subscriptions/00000000-0000-0000-0000-000000000000/providers/Microsoft.CognitiveServices/locations/eastus/usages?api-version=2024-10-01"
	for _, scenario := range []struct {
		name, next    string
		expectedCalls int
		success       bool
	}{
		{"same scope", initial + "&$skiptoken=opaque", 2, true},
		{"host", strings.Replace(initial, "management.azure.com", "evil.test", 1), 1, false},
		{"subscription", strings.Replace(initial, "00000000-0000-0000-0000-000000000000", "another", 1), 1, false},
		{"region", strings.Replace(initial, "eastus", "westus", 1), 1, false},
		{"userinfo", strings.Replace(initial, "https://", "https://secret@", 1), 1, false},
		{"version", strings.Replace(initial, "2024-10-01", "2025-06-01", 1), 1, false},
		{"relative", "/subscriptions/other", 1, false},
		{"cycle", initial, 1, false},
		{"encoded traversal", strings.Replace(initial, "/eastus/", "/eastus/%2e%2e/", 1), 1, false},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			calls := 0
			account := managementFixtureAccount("azure_openai")
			upstream := compatibleBalanceHTTP{send: func(request *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
				calls++
				if calls == 1 {
					encoded, _ := json.Marshal(scenario.next)
					return managementFixtureResponse(200, strings.TrimSuffix(managementAzureFixture, "}")+`,"nextLink":`+string(encoded)+`}`), nil
				}
				require.Equal(test, scenario.next, request.URL.String())
				return managementFixtureResponse(200, `{"value":[]}`), nil
			}}
			result, err := NewCNProviderBalanceService(&compatibleBalanceRepo{account: account}, nil, upstream, nil).QueryManagementUsage(context.Background(), account.ID, nil)
			require.NoError(test, err)
			require.Equal(test, scenario.expectedCalls, calls)
			require.Equal(test, scenario.success, result.Success)
			require.Equal(test, !scenario.success, result.Partial)
		})
	}
	calls := 0
	account := managementFixtureAccount("azure_openai")
	upstream := compatibleBalanceHTTP{send: func(*http.Request, string, int64, int) (*http.Response, error) {
		calls++
		return managementFixtureResponse(200, fmt.Sprintf(`{"value":[],"nextLink":%q}`, initial+fmt.Sprintf("&page=%d", calls))), nil
	}}
	result, err := NewCNProviderBalanceService(&compatibleBalanceRepo{account: account}, nil, upstream, nil).QueryManagementUsage(context.Background(), account.ID, nil)
	require.NoError(test, err)
	require.Equal(test, managementUsageMaxPages, calls)
	require.Equal(test, "pagination_limit", result.Reason)
	require.True(test, result.Partial)
}

func TestManagementResponseTypesAndUnknowns(test *testing.T) {
	items, _, err := parseAzureRegionalQuota([]byte(`{"value":[{"name":{"value":"model"}}]}`))
	require.NoError(test, err)
	require.Nil(test, items[0].CurrentValue)
	require.Nil(test, items[0].Limit)
	require.Nil(test, items[0].Unit)
	report, err := parseGitHubBilling([]byte(managementGitHubFixture), "octocat", 2026, 10)
	require.NoError(test, err)
	require.Equal(test, float64(75), *report.UsageItems[0].NetQuantity)
	report, err = parseGitHubBilling([]byte(`{"timePeriod":{"year":2026},"user":"octocat","usageItems":[{}]}`), "octocat", 2026, 10)
	require.NoError(test, err)
	require.Nil(test, report.TimePeriod.Month)
	require.Nil(test, report.UsageItems[0].NetAmount)
	for _, body := range []string{
		`{"timePeriod":{"year":2026},"user":"other","usageItems":[]}`,
		strings.Replace(managementGitHubFixture, `"month":10`, `"month":9`, 1),
		strings.Replace(managementGitHubFixture, `"grossAmount":4`, `"grossAmount":"4"`, 1),
		strings.Replace(managementGitHubFixture, `"model":"GPT-5"`, `"model":12`, 1),
		strings.Replace(managementGitHubFixture, `"year":2026`, `"year":"2026"`, 1),
		`{"timePeriod":{"year":2026},"user":"octocat","usageItems":{}}`,
	} {
		_, err := parseGitHubBilling([]byte(body), "octocat", 2026, 10)
		require.Error(test, err)
	}
}

func TestManagementBillingWindowAndQuery(test *testing.T) {
	now := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	for _, query := range []url.Values{{"year": {"2024"}, "month": {"10"}}, {"year": {"2026"}, "month": {"11"}}, {"month": {"0"}}, {"month": {"13"}}, {"month": {"1", "2"}}, {"year": {"2026x"}}, {"month": {""}}} {
		_, _, err := managementBillingMonth(query, now)
		require.Error(test, err)
	}
	year, month, err := managementBillingMonth(url.Values{"year": {"2024"}, "month": {"11"}}, now)
	require.NoError(test, err)
	require.Equal(test, 2024, year)
	require.Equal(test, 11, month)
	for _, provider := range []string{"azure_openai", "github_copilot"} {
		account := managementFixtureAccount(provider)
		upstream := compatibleBalanceHTTP{send: func(*http.Request, string, int64, int) (*http.Response, error) {
			test.Fatal("unexpected request")
			return nil, nil
		}}
		_, err := NewCNProviderBalanceService(&compatibleBalanceRepo{account: account}, nil, upstream, nil).QueryManagementUsage(context.Background(), account.ID, url.Values{"username": {"injected"}})
		require.Error(test, err)
	}
}
