package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/securityaudit"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestKimiSearchAuditsQueryBeforeUpstream(t *testing.T) {
	engine := blockingHandlerPromptEngine()
	gateway := NewOpenAIGatewayHandler(&service.OpenAIGatewayService{}, &service.ConcurrencyService{}, &service.BillingCacheService{}, &service.APIKeyService{}, nil, nil, nil, nil, nil)
	gateway.securityAuditCoordinator = securityaudit.NewCoordinator(nil, engine)
	router := gin.New()
	router.Use(securityAuditMediaTestMiddleware)
	router.POST("/v1/tools/search", gateway.KimiSearch)
	request := httptest.NewRequest(http.MethodPost, "/v1/tools/search", strings.NewReader(`{"text_query":"query to audit"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusForbidden, recorder.Code)
	evaluated, _, requests := engine.snapshot()
	require.Equal(t, 1, evaluated)
	require.Len(t, requests, 1)
	require.JSONEq(t, `{"messages":[{"role":"user","content":"query to audit"}]}`, string(requests[0].Body))
	require.Equal(t, service.ContentModerationProtocolOpenAIChat, requests[0].Protocol)
}
