package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestTogetherBillingUsageHandler(test *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, scenario := range []struct {
		query  string
		status int
	}{
		{"month=2026-06&granularity=day", 200},
		{"month=2026-06&granularity=day&organization_id=other", 400},
		{"month=2026-06&granularity=day&month=2026-06", 400},
		{"month=2026-06&granularity=hour", 400},
		{"start_date=2026-06-01&end_date=2026-06-02", 400},
		{"month=2026-00&granularity=day", 400},
		{"month=2026-06&granularity=day&limit=1000", 400},
		{"month=2026-06&granularity=day&after=cursor", 400},
	} {
		repo := compatibleBalanceHandlerRepo{account: &service.Account{ID: 12, Platform: "together", Type: service.AccountTypeAPIKey, Credentials: map[string]any{"account_mode": "payg", "api_key": "fixture-only"}}}
		upstream := &compatibleUsageHandlerHTTP{body: `{"object":"list","organization_id":"org-fixture","billing_period":"2026-06","currency":"USD","earliest_window_start":null,"latest_window_end":null,"data":[],"next_cursor":null}`}
		handler := NewCNProviderHandler(nil, service.NewCNProviderBalanceService(repo, nil, upstream, nil))
		router := gin.New()
		router.GET("/accounts/:id/usage", handler.QueryUsage)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/accounts/12/usage?"+scenario.query, nil))
		require.Equal(test, scenario.status, recorder.Code)
		if scenario.status == 200 {
			require.Equal(test, 1, upstream.calls)
			require.Equal(test, "/v1/billing/usage", upstream.request.URL.Path)
			var envelope struct {
				Data service.TogetherBillingResult `json:"data"`
			}
			require.NoError(test, json.Unmarshal(recorder.Body.Bytes(), &envelope))
			require.True(test, envelope.Data.Success)
			require.Equal(test, "organization", envelope.Data.Usage.Scope)
			require.False(test, envelope.Data.Persisted)
		} else {
			require.Zero(test, upstream.calls)
		}
	}
}
