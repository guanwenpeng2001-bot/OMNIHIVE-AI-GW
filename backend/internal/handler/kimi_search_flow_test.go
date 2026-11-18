//go:build unit

package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	middleware2 "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

func TestKimiSearchDispatchSchedulesAndForwards(t *testing.T) {
	for _, path := range []string{"/v1/tools/search", "/v1/tools/search_pro"} {
		t.Run(path, func(t *testing.T) {
			cfg := &config.Config{RunMode: config.RunModeSimple}
			billingCache := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
			t.Cleanup(billingCache.Stop)
			repository := &qianfanFlowAccountRepo{accounts: []service.Account{{
				ID: 12, Platform: service.PlatformKimi, Type: service.AccountTypeAPIKey,
				Status: service.StatusActive, Schedulable: true, Credentials: map[string]any{"api_key": "kimi-fixture", "account_mode": "payg"},
			}}}
			upstream := &qianfanFlowUpstream{responder: func(request *http.Request) *http.Response {
				require.Equal(t, path, request.URL.Path)
				require.Equal(t, "Bearer kimi-fixture", request.Header.Get("Authorization"))
				return qianfanFlowJSON(http.StatusOK, `{"search_results":[]}`)
			}}
			gateway := service.ProvideOpenAIGatewayService(repository, nil, nil, nil, nil, nil, newQianfanHandlerCacheStub(), cfg, nil, nil,
				service.NewBillingService(cfg, nil), nil, billingCache, upstream, &service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil, service.NewUsageSettlementService(&unlimitedTPMReceiptStub{}, nil, nil))
			concurrencyCache := &concurrencyCacheMock{
				acquireUserSlotFn:    func(context.Context, int64, int, string) (bool, error) { return true, nil },
				acquireAccountSlotFn: func(context.Context, int64, int, string) (bool, error) { return true, nil },
			}
			gatewayHandler := NewOpenAIGatewayHandler(gateway, service.NewConcurrencyService(concurrencyCache), billingCache, &service.APIKeyService{}, nil, nil, nil, nil, cfg)
			router := gin.New()
			router.Use(securityAuditMediaTestMiddleware)
			router.Use(func(requestContext *gin.Context) {
				key, _ := middleware2.GetAPIKeyFromContext(requestContext)
				key.Group.Platform = service.PlatformKimi
			})
			dispatch := service.NewGatewayActionDispatch(service.GatewayActionHandlers{"kimi.search": gatewayHandler.KimiSearch})
			router.POST(path, service.GatewayActionRequirement, dispatch.Handler("search"))
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"text_query":"fixture"}`))
			request.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(recorder, request)
			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
			require.JSONEq(t, `{"search_results":[]}`, recorder.Body.String())
			requests, _ := upstream.snapshot()
			require.Len(t, requests, 1)
		})
	}
}
