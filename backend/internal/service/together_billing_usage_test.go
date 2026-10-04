package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/testutil/testassert"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// Synthetic, schema-based example; no customer data or live credentials.
const togetherBillingFixture = `{"object":"list","organization_id":"org-fixture","billing_period":"2026-06","currency":"USD","earliest_window_start":"2026-06-01T00:00:00Z","latest_window_end":"2026-06-02T00:00:00Z","data":[{"date":"2026-06-01","start_time":"2026-06-01T00:00:00Z","end_time":"2026-06-02T00:00:00Z","line_items":[{"product_name":"Inference","quantity":"9007199254740993.123","unit_price":"0.000000001","cost":"0.1234567890123456789","pricing_dimensions":{"unit":"tokens"},"attributes":{}}]}],"next_cursor":null}`

func togetherFixtureAccount() *Account {
	account := compatibleBalanceAccount("together")
	account.Credentials["account_mode"] = "payg"
	return account
}
func togetherFixtureQuery() url.Values {
	return url.Values{"month": {"2026-06"}, "granularity": {"day"}}
}
func togetherFixtureBody(change func(map[string]any)) string {
	var root map[string]any
	_ = json.Unmarshal([]byte(togetherBillingFixture), &root)
	change(root)
	body, _ := json.Marshal(root)
	return string(body)
}
func TestTogetherBillingRequest(test *testing.T) {
	for _, grouped := range []bool{false, true} {
		account := togetherFixtureAccount()
		identity := int64(9)
		if grouped {
			account.ProxyGroupID = &identity
			account.egress = &accountEgress{target: EgressTarget{Resolved: true, URL: "http://proxy.example:8080", Source: EgressSourceGroupBinding}}
		} else {
			account.ProxyID = &identity
			account.Proxy = &Proxy{Protocol: "http", Host: "proxy.example", Port: 8080}
		}
		account.Extra = map[string]any{"header_overrides": map[string]any{"X-Secret": "secret"}}
		repo := &compatibleBalanceRepo{account: account}
		upstream := compatibleBalanceHTTP{send: func(request *http.Request, proxy string, accountID int64, concurrency int) (*http.Response, error) {
			require.Equal(test, "GET", request.Method)
			require.Equal(test, "api.together.ai", request.URL.Host)
			require.Equal(test, "/v1/billing/usage", request.URL.Path)
			require.Equal(test, url.Values{"month": {"2026-06"}, "granularity": {"day"}, "limit": {"100"}}, request.URL.Query())
			require.Equal(test, "Bearer "+account.GetCredential("api_key"), request.Header.Get("Authorization"))
			require.Empty(test, request.Header.Get("X-Secret"))
			require.Equal(test, "http://proxy.example:8080", proxy)
			require.Equal(test, account.ID, accountID)
			require.True(test, HTTPUpstreamRedirectsDisabled(request.Context()))
			deadline, exists := request.Context().Deadline()
			require.True(test, exists)
			require.LessOrEqual(test, time.Until(deadline), cnBalanceUpstreamTimeout)
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(togetherBillingFixture))}, nil
		}}
		value, err := NewCNProviderBalanceService(repo, nil, upstream, nil).QueryBillingUsage(context.Background(), account.ID, togetherFixtureQuery())
		require.NoError(test, err)
		result := testassert.Type[*TogetherBillingResult](test, value)
		require.True(test, result.Success)
		require.Equal(test, "organization", result.Usage.Scope)
		require.Equal(test, "0.1234567890123456789", result.Usage.Data[0].LineItems[0].Cost)
		require.Equal(test, "9007199254740993.123", result.Usage.Data[0].LineItems[0].Quantity)
		require.False(test, result.Persisted)
		require.Empty(test, repo.writes)
	}
}
func TestTogetherBillingParser(test *testing.T) {
	for _, field := range []string{"object", "organization_id", "billing_period", "currency", "data", "next_cursor", "earliest_window_start", "latest_window_end"} {
		for _, replacement := range []any{nil, 12} {
			body := togetherFixtureBody(func(root map[string]any) {
				if replacement == nil {
					delete(root, field)
				} else {
					root[field] = replacement
				}
			})
			_, err := parseTogetherBillingPage([]byte(body), "2026-06")
			require.Error(test, err, field)
		}
	}
	for _, field := range []string{"product_name", "quantity", "unit_price", "cost", "pricing_dimensions", "attributes"} {
		for _, replacement := range []any{nil, 12} {
			body := togetherFixtureBody(func(root map[string]any) {
				line := testassert.Type[map[string]any](test, testassert.Type[[]any](test, testassert.Type[map[string]any](test, testassert.Type[[]any](test, root["data"])[0])["line_items"])[0])
				if replacement == nil {
					delete(line, field)
				} else {
					line[field] = replacement
				}
			})
			_, err := parseTogetherBillingPage([]byte(body), "2026-06")
			require.Error(test, err, field)
		}
	}
	for _, body := range []string{`null`, `{}`, strings.Replace(togetherBillingFixture, `"USD"`, `"CNY"`, 1), strings.Replace(togetherBillingFixture, `"2026-06-01"`, `"2026-07-01"`, 1), strings.Replace(togetherBillingFixture, `"quantity":"9007199254740993.123"`, `"quantity":"NaN"`, 1)} {
		_, err := parseTogetherBillingPage([]byte(body), "2026-06")
		require.Error(test, err)
	}
}
func TestTogetherBillingPaginationFailures(test *testing.T) {
	for _, scenario := range []string{"two pages", "bounded", "repeated cursor", "duplicate window", "organization changed", "later transport", "401", "403", "404", "429", "oversized", "empty", "malformed"} {
		test.Run(scenario, func(test *testing.T) {
			account := togetherFixtureAccount()
			calls := 0
			upstream := compatibleBalanceHTTP{send: func(request *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
				calls++
				status := 200
				body := togetherBillingFixture
				if scenario == "two pages" || scenario == "bounded" || scenario == "repeated cursor" || scenario == "duplicate window" || scenario == "later transport" || scenario == "organization changed" {
					if calls > 1 {
						require.Equal(test, fmt.Sprintf("cursor +/%d", calls-1), request.URL.Query().Get("after"))
					}
					body = togetherFixtureBody(func(root map[string]any) {
						if calls > 1 || scenario == "bounded" {
							root["data"] = []any{}
						}
						if calls == 1 || scenario == "bounded" || scenario == "repeated cursor" {
							root["next_cursor"] = fmt.Sprintf("cursor +/%d", calls)
						}
						if calls == 2 && scenario == "repeated cursor" {
							root["next_cursor"] = "cursor +/1"
						}
						if calls == 2 && scenario == "organization changed" {
							root["organization_id"] = "other"
						}
					})
					if calls == 2 && scenario == "duplicate window" {
						body = togetherBillingFixture
					}
					if calls == 2 && scenario == "later transport" {
						return nil, errors.New("secret key")
					}
				}
				switch scenario {
				case "401":
					status = 401
				case "403":
					status = 403
				case "404":
					status = 404
				case "429":
					status = 429
				case "oversized":
					body = strings.Repeat("x", cnBalanceMaxBodyBytes+1)
				case "malformed":
					body = `{}`
				case "empty":
					body = togetherFixtureBody(func(root map[string]any) {
						root["data"] = []any{}
						root["earliest_window_start"] = nil
						root["latest_window_end"] = nil
					})
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}, nil
			}}
			repo := &compatibleBalanceRepo{account: account}
			value, err := NewCNProviderBalanceService(repo, nil, upstream, nil).QueryBillingUsage(context.Background(), account.ID, togetherFixtureQuery())
			require.NoError(test, err)
			result := testassert.Type[*TogetherBillingResult](test, value)
			switch scenario {
			case "two pages":
				require.True(test, result.Success)
				require.Equal(test, 2, result.Usage.PagesFetched)
				require.Len(test, result.Usage.Data, 1)
			case "bounded":
				require.True(test, result.Success)
				require.True(test, result.Usage.Partial)
				require.Equal(test, togetherBillingMaxPages, calls)
			case "empty":
				require.True(test, result.Success)
				require.Empty(test, result.Usage.Data)
			default:
				require.False(test, result.Success)
				require.Nil(test, result.Usage)
				require.NotEmpty(test, result.Reason)
			}
			if scenario == "404" {
				require.Equal(test, "not_enabled", result.Reason)
			}
			encoded, _ := json.Marshal(result)
			require.NotContains(test, string(encoded), "secret key")
			require.Empty(test, repo.writes)
		})
	}
}
func TestTogetherBillingGuards(test *testing.T) {
	for _, scenario := range []string{"month", "hour", "organization", "duplicate", "cursor", "mode", "type", "key", "origin", "protocol bases", "relay", "proxy", "policy", "cancel"} {
		test.Run(scenario, func(test *testing.T) {
			account, query := togetherFixtureAccount(), togetherFixtureQuery()
			var cfg *config.Config
			ctx := context.Background()
			switch scenario {
			case "month":
				query.Set("month", "2026-13")
			case "hour":
				query.Set("granularity", "hour")
			case "organization":
				query.Set("organization_id", "other")
			case "duplicate":
				query.Add("month", "2026-06")
			case "cursor":
				query.Set("after", "other")
			case "mode":
				account.Credentials["account_mode"] = "coding"
			case "type":
				account.Type = "oauth"
			case "key":
				account.Credentials["api_key"] = 12
			case "origin":
				account.Credentials["base_url"] = "https://relay.example/v1"
			case "protocol bases":
				account.Credentials["api_base_urls"] = map[string]any{"chat_completions": "https://relay.example/v1"}
			case "relay":
				account.Extra = map[string]any{"custom_base_url_enabled": true}
			case "proxy":
				identity := int64(9)
				account.ProxyID = &identity
			case "policy":
				cfg = &config.Config{}
				cfg.Security.URLAllowlist.Enabled = true
				cfg.Security.URLAllowlist.UpstreamHosts = []string{"other.example"}
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			upstream := compatibleBalanceHTTP{send: func(*http.Request, string, int64, int) (*http.Response, error) {
				test.Fatal("unexpected request")
				return nil, nil
			}}
			value, err := NewCNProviderBalanceService(&compatibleBalanceRepo{account: account}, nil, upstream, cfg).QueryBillingUsage(ctx, account.ID, query)
			if err == nil {
				result := testassert.Type[*TogetherBillingResult](test, value)
				require.False(test, result.Success)
				require.Nil(test, result.Usage)
			}
		})
	}
}

func TestTogetherBillingIgnoresSupplierInternalMetadata(test *testing.T) {
	body := togetherFixtureBody(func(root map[string]any) {
		root["pages_fetched"] = 9000
		root["partial"] = true
		root["scope"] = "key"
		root["granularity"] = "hour"
	})
	page, err := parseTogetherBillingPage([]byte(body), "2026-06")
	require.NoError(test, err)
	require.Zero(test, page.PagesFetched)
	require.False(test, page.Partial)
	require.Equal(test, "organization", page.Scope)
	require.Equal(test, "day", page.Granularity)
	for _, field := range []string{"date", "start_time", "end_time", "line_items"} {
		for _, replacement := range []any{nil, 12} {
			body = togetherFixtureBody(func(root map[string]any) {
				window := testassert.Type[map[string]any](test, testassert.Type[[]any](test, root["data"])[0])
				if replacement == nil {
					delete(window, field)
				} else {
					window[field] = replacement
				}
			})
			_, err = parseTogetherBillingPage([]byte(body), "2026-06")
			require.Error(test, err, field)
		}
	}
	body = togetherFixtureBody(func(root map[string]any) { root["earliest_window_start"] = nil; root["latest_window_end"] = nil })
	_, err = parseTogetherBillingPage([]byte(body), "2026-06")
	require.Error(test, err)
}
