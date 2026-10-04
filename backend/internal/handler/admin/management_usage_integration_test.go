package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
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
