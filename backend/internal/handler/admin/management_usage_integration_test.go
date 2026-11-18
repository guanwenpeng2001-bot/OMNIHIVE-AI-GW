package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

func TestManagementUsageStoredPlatformDispatch(test *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Now().UTC()
	for _, platform := range []string{"azure_openai", "github_copilot"} {
		for _, scenario := range []string{"configured", "missing", "foreign window", "timezone", "caller platform"} {
			test.Run(platform+"/"+scenario, func(test *testing.T) {
				credentials := map[string]any{"account_mode": "payg", "api_key": "inference-secret", "usage_query_token": "management-secret", "usage_query_subscription_id": "fixture-subscription", "usage_query_region": "eastus", "usage_query_username": "octocat"}
				query := ""
				body := `{"value":[{"name":{"value":"Quota","localizedValue":"Quota"},"currentValue":3,"limit":200,"unit":"Count"}]}`
				calls := 1
				if platform == "github_copilot" {
					query = fmt.Sprintf("year=%d&month=%d", now.Year(), now.Month())
					body = fmt.Sprintf(`{"timePeriod":{"year":%d,"month":%d},"user":"octocat","usageItems":[]}`, now.Year(), now.Month())
					calls = 2
				}
				status := 200
				switch scenario {
				case "missing":
					delete(credentials, "usage_query_token")
					calls = 0
				case "foreign window":
					query = "start_date=2026-05-01&end_date=2026-05-02"
					status = 400
					calls = 0
				case "timezone":
					query += "&timezone=UTC"
					status = 400
					calls = 0
				case "caller platform":
					query += "&platform=atlascloud"
					status = 400
					calls = 0
				}
				repo := compatibleBalanceHandlerRepo{account: &service.Account{ID: 12, Platform: platform, Type: service.AccountTypeAPIKey, Credentials: credentials}}
				upstream := &compatibleUsageHandlerHTTP{body: body}
				handler := NewCNProviderHandler(nil, service.NewCNProviderBalanceService(repo, nil, upstream, nil))
				router := gin.New()
				router.GET("/accounts/:id/usage", handler.QueryUsage)
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/accounts/12/usage?"+query, nil))
				require.Equal(test, status, recorder.Code, recorder.Body.String())
				require.Equal(test, calls, upstream.calls)
				require.NotContains(test, recorder.Body.String(), "inference-secret")
				require.NotContains(test, recorder.Body.String(), "management-secret")
				if status == 200 {
					var envelope struct {
						Data service.ManagementUsageResult `json:"data"`
					}
					require.NoError(test, json.Unmarshal(recorder.Body.Bytes(), &envelope))
					require.Equal(test, platform, envelope.Data.Provider)
					require.Equal(test, scenario == "configured", envelope.Data.Success)
					if scenario == "missing" {
						require.Equal(test, "missing_query_credentials", envelope.Data.Reason)
					}
					if calls > 0 {
						require.Equal(test, "Bearer management-secret", upstream.request.Header.Get("Authorization"))
					}
				}
			})
		}
	}
}

func TestByteplusSeatQuotaHandler(test *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, scenario := range []string{"configured", "missing seat", "missing key", "personal"} {
		test.Run(scenario, func(test *testing.T) {
			credentials := map[string]any{"account_mode": "coding", "billing_plan": "team", "api_key": "inference-fixture", "byteplus_access_key_id": "cloud-id", "byteplus_secret_access_key": "cloud-secret", "byteplus_seat_id": "seat-fixture"}
			expectedStatus, calls := "ok", 1
			switch scenario {
			case "missing seat":
				delete(credentials, "byteplus_seat_id")
				expectedStatus = "no_seat"
				calls = 0
			case "missing key":
				delete(credentials, "byteplus_secret_access_key")
				expectedStatus = "missing_credentials"
				calls = 0
			case "personal":
				credentials["billing_plan"] = "personal"
				expectedStatus = "no_verified_query"
				calls = 0
			}
			repo := compatibleBalanceHandlerRepo{account: &service.Account{ID: 12, Platform: "byteplus", Type: service.AccountTypeAPIKey, Credentials: credentials}}
			upstream := &compatibleUsageHandlerHTTP{body: `{"Result":{"SeatInfoUsage":{"SeatID":"seat-fixture","ShortTermUsage":12,"WeeklyUsage":34}}}`}
			handler := NewCNProviderHandler(service.NewCNProviderQuotaService(repo, nil, upstream, nil), nil)
			router := gin.New()
			router.GET("/accounts/:id/quota", handler.QueryQuota)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/accounts/12/quota", nil))
			require.Equal(test, 200, recorder.Code, recorder.Body.String())
			require.Equal(test, calls, upstream.calls)
			var envelope struct {
				Data service.CNProviderQuotaProbeResult `json:"data"`
			}
			require.NoError(test, json.Unmarshal(recorder.Body.Bytes(), &envelope))
			require.Equal(test, expectedStatus, envelope.Data.Status)
			require.Equal(test, scenario == "configured", envelope.Data.Success)
			require.NotContains(test, recorder.Body.String(), "cloud-secret")
			require.NotContains(test, recorder.Body.String(), "inference-fixture")
			if calls > 0 {
				require.Equal(test, "ark.ap-southeast-1.byteplusapi.com", upstream.request.URL.Host)
				require.Len(test, envelope.Data.Tiers, 2)
			}
		})
	}
}

func TestVolcengineAgentMissingSeatQuotaHandler(test *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := compatibleBalanceHandlerRepo{account: &service.Account{ID: 12, Platform: "volcengine", Type: service.AccountTypeAPIKey, Credentials: map[string]any{"account_mode": "coding", "api_protocol": "chat_completions", "base_url": "https://ark.cn-beijing.volces.com/api/plan/v3", "volcengine_plan_edition": "team", "api_key": "inference-fixture", "volcengine_access_key_id": "cloud-id", "volcengine_secret_access_key": "cloud-secret"}}}
	upstream := &compatibleUsageHandlerHTTP{}
	handler := NewCNProviderHandler(service.NewCNProviderQuotaService(repo, nil, upstream, nil), nil)
	router := gin.New()
	router.GET("/accounts/:id/quota", handler.QueryQuota)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/accounts/12/quota", nil))
	require.Equal(test, 200, recorder.Code)
	require.Zero(test, upstream.calls)
	var envelope struct {
		Data service.CNProviderQuotaProbeResult `json:"data"`
	}
	require.NoError(test, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.False(test, envelope.Data.Success)
	require.Equal(test, "missing_configuration", envelope.Data.Status)
	require.Empty(test, envelope.Data.Tiers)
}
