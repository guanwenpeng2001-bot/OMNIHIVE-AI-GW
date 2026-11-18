package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

func TestVolcengineExistingAdminQuotaRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, platform := range []string{"volcengine", "byteplus"} {
		t.Run(platform, func(t *testing.T) {
			repo := compatibleBalanceHandlerRepo{account: &service.Account{ID: 12, Platform: platform, Type: service.AccountTypeAPIKey, Credentials: map[string]any{"account_mode": "coding", "base_url": "https://ark.cn-beijing.volces.com/api/coding/v3", "api_key": "inference-only"}}}
			upstream := &compatibleBalanceHandlerHTTP{}
			handler := NewCNProviderHandler(service.NewCNProviderQuotaService(repo, nil, upstream, nil), nil)
			router := gin.New()
			router.GET("/admin/cn-providers/accounts/:id/quota", handler.QueryQuota)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/admin/cn-providers/accounts/12/quota", nil))
			require.Equal(t, http.StatusOK, response.Code)
			require.Zero(t, upstream.calls)
			require.NotContains(t, response.Body.String(), "inference-only")
			var envelope struct {
				Data service.CNProviderQuotaProbeResult `json:"data"`
			}
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
			require.False(t, envelope.Data.Success)
			require.Empty(t, envelope.Data.Tiers)
			if platform == "volcengine" {
				require.Equal(t, "missing_credentials", envelope.Data.Status)
			} else {
				require.Equal(t, "no_verified_query", envelope.Data.Status)
				require.True(t, envelope.Data.Unsupported)
			}
		})
	}
}
