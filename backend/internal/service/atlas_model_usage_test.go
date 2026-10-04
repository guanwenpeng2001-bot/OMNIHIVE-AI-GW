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

// Synthetic fixture from the public 200 example (not captured account data):
// https://www.atlascloud.ai/docs/public-api/model-usage, read 2026-10-04.
const atlasUsageFixture = `{"object":"list","scope":"self","data":[{"object":"usage.bucket","date":"2026-05-01","start_at":"2026-05-01T00:00:00Z","end_at":"2026-05-02T00:00:00Z","covered_until":"2026-05-02T00:00:00Z","partial":false,"results":[{"object":"usage.result","usage":{"requests":12,"tokens":{"input":12000,"output":3000,"total":15000,"cache_creation":0,"cache_creation_1h":0,"cache_read":0,"input_image":0,"output_image":0},"images":null,"video":null}}]}],"has_more":false,"next_page":null,"request_id":"req_123"}`

func atlasQueryWindow() url.Values {
	return url.Values{"start_date": {"2026-05-01"}, "end_date": {"2026-05-08"}}
}
func atlasFixtureAccount() *Account {
	account := compatibleBalanceAccount("atlascloud")
	account.Credentials["api_key"] = "apikey-fixture-only"
	account.Credentials["account_mode"] = "payg"
	return account
}

func TestAtlasUsageRequestAndCounters(test *testing.T) {
	for _, grouped := range []bool{false, true} {
		account := atlasFixtureAccount()
		proxyID := int64(9)
		if grouped {
			account.ProxyGroupID = &proxyID
			account.egress = &accountEgress{target: EgressTarget{Resolved: true, URL: "http://proxy.example:8080", Source: EgressSourceGroupBinding}}
		} else {
			account.ProxyID = &proxyID
			account.Proxy = &Proxy{Protocol: "http", Host: "proxy.example", Port: 8080}
		}
		account.Extra = map[string]any{"header_overrides": map[string]any{"X-Secret": "do-not-send"}}
		repo := &compatibleBalanceRepo{account: account}
		upstream := compatibleBalanceHTTP{send: func(request *http.Request, proxy string, identity int64, concurrency int) (*http.Response, error) {
			require.Equal(test, "GET", request.Method)
			require.Equal(test, "api.atlascloud.ai", request.URL.Host)
			require.Equal(test, "/public/v1/model-usage", request.URL.Path)
			require.Equal(test, atlasQueryWindow(), request.URL.Query())
			require.Equal(test, "Bearer apikey-fixture-only", request.Header.Get("Authorization"))
			require.Empty(test, request.Header.Get("X-Secret"))
			require.NotContains(test, request.URL.String(), "apikey-")
			require.Equal(test, "http://proxy.example:8080", proxy)
			require.Equal(test, account.ID, identity)
			require.Equal(test, 1, concurrency)
			require.True(test, HTTPUpstreamRedirectsDisabled(request.Context()))
			deadline, exists := request.Context().Deadline()
			require.True(test, exists)
			require.LessOrEqual(test, time.Until(deadline), cnBalanceUpstreamTimeout)
			// Deliberately independent total/cache counters: no addition is permitted.
			body := strings.Replace(atlasUsageFixture, `"total":15000`, `"total":999`, 1)
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
		}}
		value, err := NewCNProviderBalanceService(repo, nil, upstream, nil).QueryProviderUsage(context.Background(), account.ID, atlasQueryWindow())
		require.NoError(test, err)
		result := testassert.Type[*AtlasUsageResult](test, value)
		require.True(test, result.Success)
		require.Equal(test, "self", result.Usage.Scope)
		require.Equal(test, float64(999), result.Usage.Data[0].Results[0].Usage.Tokens["total"])
		require.Nil(test, result.Usage.Data[0].Results[0].Usage.Images)
		require.Nil(test, result.Usage.Data[0].Results[0].Usage.Video)
		require.False(test, result.Persisted)
		require.Empty(test, repo.writes)
	}
}

func TestAtlasUsageWindowAndGuards(test *testing.T) {
	for _, scenario := range []struct {
		name   string
		change func(*Account, url.Values)
		policy bool
	}{
		{"181 days", func(_ *Account, query url.Values) {
			query.Set("start_date", "2026-01-01")
			query.Set("end_date", "2026-07-01")
		}, false},
		{"equal", func(_ *Account, query url.Values) { query.Set("end_date", "2026-05-01") }, false},
		{"invalid date", func(_ *Account, query url.Values) { query.Set("start_date", "2026-02-30") }, false},
		{"timestamp", func(_ *Account, query url.Values) { query.Set("start_date", "2026-05-01T00:00:00Z") }, false},
		{"scope override", func(_ *Account, query url.Values) { query.Set("scope", "account") }, false},
		{"duplicate", func(_ *Account, query url.Values) { query.Add("start_date", "2026-05-01") }, false},
		{"coding", func(account *Account, _ url.Values) { account.Credentials["account_mode"] = "coding" }, false},
		{"key", func(account *Account, _ url.Values) { account.Credentials["api_key"] = "sk-secret" }, false},
		{"relay", func(account *Account, _ url.Values) { account.Credentials["base_url"] = "https://relay.example/v1" }, false},
		{"custom flag", func(account *Account, _ url.Values) { account.Extra = map[string]any{"custom_base_url_enabled": true} }, false},
		{"proxy", func(account *Account, _ url.Values) { identity := int64(9); account.ProxyID = &identity }, false},
		{"policy", func(_ *Account, _ url.Values) {}, true},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			account, query := atlasFixtureAccount(), atlasQueryWindow()
			scenario.change(account, query)
			upstream := compatibleBalanceHTTP{send: func(*http.Request, string, int64, int) (*http.Response, error) {
				test.Fatal("unexpected outbound request")
				return nil, nil
			}}
			var cfg *config.Config
			if scenario.policy {
				cfg = &config.Config{}
				cfg.Security.URLAllowlist.Enabled = true
				cfg.Security.URLAllowlist.UpstreamHosts = []string{"other.example"}
			}
			value, err := NewCNProviderBalanceService(&compatibleBalanceRepo{account: account}, nil, upstream, cfg).QueryProviderUsage(context.Background(), account.ID, query)
			if err == nil {
				result := testassert.Type[*AtlasUsageResult](test, value)
				require.False(test, result.Success)
				require.Nil(test, result.Usage)
			}
		})
	}
	require.NoError(test, ValidateAtlasUsageWindow(AtlasUsageWindow{StartDate: "2026-01-01", EndDate: "2026-06-30"}))
}

func TestAtlasUsageParser(test *testing.T) {
	window := AtlasUsageWindow{StartDate: "2026-05-01", EndDate: "2026-05-08"}
	for _, body := range []string{`{}`, `null`, strings.Replace(atlasUsageFixture, `"scope":"self"`, `"scope":"account"`, 1),
		strings.Replace(atlasUsageFixture, `"requests":12`, `"requests":null`, 1),
		strings.Replace(atlasUsageFixture, `"requests":12`, `"requests":-1`, 1),
		strings.Replace(atlasUsageFixture, `"requests":12`, `"requests":1.5`, 1),
		strings.Replace(atlasUsageFixture, `"total":15000`, `"total":"15000"`, 1),
		strings.Replace(atlasUsageFixture, `"cache_read":0,`, ``, 1),
		strings.Replace(atlasUsageFixture, `"images":null`, `"images":{}`, 1),
		strings.Replace(atlasUsageFixture, `"video":null`, `"video":{"seconds":null}`, 1),
		strings.Replace(atlasUsageFixture, `"partial":false,`, ``, 1),
		strings.Replace(atlasUsageFixture, `"has_more":false`, `"has_more":true`, 1),
		strings.Replace(atlasUsageFixture, `"covered_until":"2026-05-02T00:00:00Z"`, `"covered_until":"2026-05-01T12:00:00Z"`, 1),
	} {
		_, err := parseAtlasUsagePage([]byte(body), window)
		require.Error(test, err, body)
	}
	body := strings.Replace(atlasUsageFixture, `"images":null,"video":null`, `"images":{"count":2},"video":{"seconds":5}`, 1)
	page, err := parseAtlasUsagePage([]byte(body), window)
	require.NoError(test, err)
	require.Equal(test, float64(2), page.Data[0].Results[0].Usage.Images.Count)
	require.Equal(test, float64(5), page.Data[0].Results[0].Usage.Video.Seconds)
	var raw map[string]any
	require.NoError(test, json.Unmarshal([]byte(body), &raw))
	bucket := testassert.Type[map[string]any](test, testassert.Type[[]any](test, raw["data"])[0])
	bucket["partial"], bucket["covered_until"] = true, "2026-05-01T12:00:00Z"
	testassert.Type[map[string]any](test, testassert.Type[map[string]any](test, testassert.Type[[]any](test, bucket["results"])[0])["usage"])["tokens"] = nil
	encoded, _ := json.Marshal(raw)
	page, err = parseAtlasUsagePage(encoded, window)
	require.NoError(test, err)
	require.Nil(test, page.Data[0].Results[0].Usage.Tokens)
	require.True(test, page.Data[0].Partial)
	_, err = parseAtlasUsagePage([]byte(`{"object":"list","scope":"self","data":[],"has_more":false,"next_page":null}`), window)
	require.NoError(test, err)
}

func TestAtlasUsagePaginationAndFailures(test *testing.T) {
	for _, scenario := range []string{"two pages", "bounded", "repeated cursor", "duplicate bucket", "later failure", "403", "429", "401", "transport", "oversized", "empty"} {
		test.Run(scenario, func(test *testing.T) {
			account := atlasFixtureAccount()
			calls := 0
			upstream := compatibleBalanceHTTP{send: func(request *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
				calls++
				body, status := atlasUsageFixture, 200
				if scenario == "transport" {
					return nil, errors.New("apikey-fixture-only")
				}
				if scenario == "oversized" {
					body = strings.Repeat("x", cnBalanceMaxBodyBytes+1)
				}
				if scenario == "empty" {
					body = `{"object":"list","scope":"self","data":[],"has_more":false,"next_page":null}`
				}
				if scenario == "403" {
					status = 403
					body = "apikey-fixture-only"
				}
				if scenario == "429" {
					status = 429
				}
				if scenario == "401" {
					status = 401
				}
				if scenario == "two pages" || scenario == "later failure" || scenario == "duplicate bucket" || scenario == "repeated cursor" || scenario == "bounded" {
					if calls > 1 {
						require.Equal(test, fmt.Sprintf("cursor +/%d", calls-1), request.URL.Query().Get("page"))
						body = strings.ReplaceAll(body, "2026-05-02", "2026-05-03")
						body = strings.ReplaceAll(body, "2026-05-01", "2026-05-02")
					}
					if scenario == "bounded" {
						body = `{"object":"list","scope":"self","data":[],"has_more":false,"next_page":null}`
					}
					if calls == 1 || scenario == "bounded" || scenario == "repeated cursor" {
						cursor := fmt.Sprintf("cursor +/%d", calls)
						if scenario == "repeated cursor" {
							cursor = "cursor +/1"
						}
						body = strings.Replace(body, `"has_more":false,"next_page":null`, fmt.Sprintf(`"has_more":true,"next_page":%q`, cursor), 1)
					}
					if calls == 2 && scenario == "later failure" {
						status = 403
						body = "apikey-fixture-only"
					}
					if calls == 2 && scenario == "duplicate bucket" {
						body = atlasUsageFixture
					}
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}, nil
			}}
			value, err := NewCNProviderBalanceService(&compatibleBalanceRepo{account: account}, nil, upstream, nil).QueryProviderUsage(context.Background(), account.ID, atlasQueryWindow())
			require.NoError(test, err)
			result := testassert.Type[*AtlasUsageResult](test, value)
			switch scenario {
			case "two pages":
				require.True(test, result.Success)
				require.Len(test, result.Usage.Data, 2)
				require.Equal(test, 2, calls)
				require.False(test, result.Usage.HasMore)
			case "bounded":
				require.True(test, result.Success)
				require.Equal(test, atlasUsageMaxPages, calls)
				require.True(test, result.Usage.Partial)
				require.True(test, result.Usage.HasMore)
				require.NotNil(test, result.Usage.NextPage)
			case "empty":
				require.True(test, result.Success)
				require.Empty(test, result.Usage.Data)
			default:
				require.False(test, result.Success)
				require.Nil(test, result.Usage)
				require.NotEmpty(test, result.Reason)
			}
			encoded, _ := json.Marshal(result)
			require.NotContains(test, string(encoded), "apikey-fixture-only")
		})
	}
}

func TestAtlasUsageCancellationAndEmptyBucket(test *testing.T) {
	account := atlasFixtureAccount()
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	upstream := compatibleBalanceHTTP{send: func(*http.Request, string, int64, int) (*http.Response, error) {
		test.Fatal("canceled request must not be sent")
		return nil, nil
	}}
	_, err := NewCNProviderBalanceService(&compatibleBalanceRepo{account: account}, nil, upstream, nil).QueryProviderUsage(canceled, account.ID, atlasQueryWindow())
	require.ErrorIs(test, err, context.Canceled)
	var raw map[string]any
	require.NoError(test, json.Unmarshal([]byte(atlasUsageFixture), &raw))
	testassert.Type[map[string]any](test, testassert.Type[[]any](test, raw["data"])[0])["results"] = []any{}
	encoded, err := json.Marshal(raw)
	require.NoError(test, err)
	page, err := parseAtlasUsagePage(encoded, AtlasUsageWindow{StartDate: "2026-05-01", EndDate: "2026-05-08"})
	require.NoError(test, err)
	require.Empty(test, page.Data[0].Results)
}
