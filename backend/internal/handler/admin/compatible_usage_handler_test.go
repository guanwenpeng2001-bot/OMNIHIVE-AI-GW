package admin

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

type compatibleUsageHandlerHTTP struct {
	service.HTTPUpstream
	calls   int
	body    string
	request *http.Request
}

func (upstream *compatibleUsageHandlerHTTP) Do(request *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	upstream.calls++
	upstream.request = request
	if upstream.body != "" {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(upstream.body))}, nil
	}
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"status":true,"data":[{"id":"model","name":"Model","items":[{"name":"Input","unit":"kToken","total":0}]}]}`))}, nil
}

func TestAtlasUsageAdminProviderDispatch(test *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, scenario := range []struct {
		name, platform, query string
		status, calls         int
	}{
		{"atlas", "atlascloud", "start_date=2026-05-01&end_date=2026-05-02", 200, 1},
		{"atlas missing dates", "atlascloud", "", 400, 0},
		{"atlas rejects qiniu dates", "atlascloud", "granularity=day&start=2026-05-01T00:00:00Z&end=2026-05-02T00:00:00Z", 400, 0},
		{"qiniu rejects atlas dates", "qiniu", "start_date=2026-05-01&end_date=2026-05-02", 400, 0},
		{"atlas rejects scope", "atlascloud", "start_date=2026-05-01&end_date=2026-05-02&scope=account", 400, 0},
		{"atlas rejects duplicate", "atlascloud", "start_date=2026-05-01&start_date=2026-05-01&end_date=2026-05-02", 400, 0},
		{"atlas rejects excessive span", "atlascloud", "start_date=2026-01-01&end_date=2026-07-01", 400, 0},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			repo := compatibleBalanceHandlerRepo{account: &service.Account{ID: 12, Platform: scenario.platform, Type: service.AccountTypeAPIKey, Credentials: map[string]any{"account_mode": "payg", "api_key": "apikey-fixture-only"}}}
			upstream := &compatibleUsageHandlerHTTP{body: `{"object":"list","scope":"self","data":[],"has_more":false,"next_page":null}`}
			handler := NewCNProviderHandler(nil, service.NewCNProviderBalanceService(repo, nil, upstream, nil))
			router := gin.New()
			router.GET("/admin/cn-providers/accounts/:id/usage", handler.QueryUsage)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/admin/cn-providers/accounts/12/usage?"+scenario.query, nil))
			require.Equal(test, scenario.status, recorder.Code)
			require.Equal(test, scenario.calls, upstream.calls)
			if scenario.calls > 0 {
				require.Equal(test, "/public/v1/model-usage", upstream.request.URL.Path)
				require.NotContains(test, recorder.Body.String(), "apikey-fixture-only")
				var envelope struct {
					Data service.AtlasUsageResult `json:"data"`
				}
				require.NoError(test, json.Unmarshal(recorder.Body.Bytes(), &envelope))
				require.True(test, envelope.Data.Success)
				require.Equal(test, "self", envelope.Data.Usage.Scope)
			}
		})
	}
}

func TestCompatibleUsageAdminHandler(test *testing.T) {
	gin.SetMode(gin.TestMode)
	query := url.Values{"granularity": {"day"}, "start": {"2026-09-01T00:00:00+08:00"}, "end": {"2026-09-02T00:00:00+08:00"}}.Encode()
	for _, scenario := range []struct {
		name, identity, params, mode string
		status, calls                int
		success                      bool
	}{
		{"consumed zero", "12", query, "payg", 200, 1, true},
		{"subscription unknown", "12", query, "subscription", 200, 0, false},
		{"missing dates", "12", "granularity=day", "payg", 400, 0, false},
		{"missing granularity", "12", strings.Replace(query, "granularity=day&", "", 1), "payg", 400, 0, false},
		{"unsupported granularity", "12", strings.Replace(query, "day", "hour", 1), "payg", 400, 0, false},
		{"extra credential query", "12", query + "&api_key=sk-secret", "payg", 400, 0, false},
		{"duplicate value", "12", query + "&granularity=day", "payg", 400, 0, false},
		{"malformed escape", "12", query + "&timezone=%zz", "payg", 400, 0, false},
		{"invalid timezone", "12", query + "&timezone=Local", "payg", 400, 0, false},
		{"bad ID", "not-id", query, "payg", 400, 0, false},
		{"zero ID", "0", query, "payg", 400, 0, false},
		{"negative ID", "-1", query, "payg", 400, 0, false},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			repo := compatibleBalanceHandlerRepo{account: &service.Account{ID: 12, Platform: "qiniu", Type: service.AccountTypeAPIKey, Credentials: map[string]any{"account_mode": scenario.mode, "api_key": "sk-fixture-only"}}}
			upstream := &compatibleUsageHandlerHTTP{}
			handler := NewCNProviderHandler(nil, service.NewCNProviderBalanceService(repo, nil, upstream, nil))
			router := gin.New()
			router.GET("/admin/cn-providers/accounts/:id/usage", handler.QueryUsage)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/admin/cn-providers/accounts/"+scenario.identity+"/usage?"+scenario.params, nil))
			require.Equal(test, scenario.status, recorder.Code)
			require.Equal(test, scenario.calls, upstream.calls)
			require.NotContains(test, recorder.Body.String(), "sk-fixture-only")
			if scenario.status == 200 {
				var envelope struct {
					Data service.CompatibleUsageResult `json:"data"`
				}
				require.NoError(test, json.Unmarshal(recorder.Body.Bytes(), &envelope))
				require.Equal(test, scenario.success, envelope.Data.Success)
				require.False(test, envelope.Data.Persisted)
				if scenario.success {
					require.Equal(test, "consumed_usage", envelope.Data.Kind)
					require.Zero(test, envelope.Data.Usage.Models[0].Items[0].Total)
					require.Equal(test, "kToken", envelope.Data.Usage.Models[0].Items[0].Unit)
				} else {
					require.True(test, envelope.Data.Unsupported)
					require.Equal(test, "mode_unsupported", envelope.Data.Reason)
					require.Nil(test, envelope.Data.Usage)
				}
			}
		})
	}
}
