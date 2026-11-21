package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/securityaudit"
	middleware2 "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

type resourceItemAuditEngine struct{ requests []securityaudit.Request }

func (r *resourceItemAuditEngine) Check(_ context.Context, request securityaudit.Request) (*securityaudit.LegacyDecision, error) {
	r.requests = append(r.requests, request.Clone())
	blocked := len(r.requests) == 2
	return &securityaudit.LegacyDecision{Allowed: !blocked, Blocked: blocked, StatusCode: http.StatusForbidden, ErrorCode: "content_policy_violation", Message: "second item blocked"}, nil
}

func TestResourceFileAuditsEveryBatchLine(t *testing.T) {
	engine := &resourceItemAuditEngine{}
	resource := NewGatewayResourceHandler(&OpenAIGatewayHandler{securityAuditCoordinator: securityaudit.NewCoordinator(engine, nil)}, nil)
	recorder := httptest.NewRecorder()
	requestContext, _ := gin.CreateTestContext(recorder)
	requestContext.Request = httptest.NewRequest(http.MethodPost, "/v1/files", nil)
	body := `{"body":{"messages":[{"role":"user","content":"first item"}]}}` + "\n" + `{"body":{"messages":[{"role":"user","content":"second item"}]}}`
	allowed := resource.auditResourceBatchLines(requestContext, nil, middleware2.AuthSubject{UserID: 7}, "model", []byte(body))
	require.False(t, allowed)
	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.Len(t, engine.requests, 2, "the first item must not cache approval for all remaining items")
	require.Contains(t, string(engine.requests[1].Body), "second item")
}

func TestMessageBatchAuditsEveryItemBeforeAccountSelection(t *testing.T) {
	engine := &resourceItemAuditEngine{}
	resource := NewGatewayResourceHandler(&OpenAIGatewayHandler{
		gatewayService: &service.OpenAIGatewayService{}, securityAuditCoordinator: securityaudit.NewCoordinator(engine, nil),
	}, &service.UpstreamJobService{})
	router := gin.New()
	router.Use(securityAuditMediaTestMiddleware)
	router.Use(func(requestContext *gin.Context) {
		key, _ := middleware2.GetAPIKeyFromContext(requestContext)
		key.Group.Platform = service.PlatformAnthropic
	})
	router.POST("/v1/messages/batches", resource.CreateMessageBatch)
	body := `{"requests":[{"custom_id":"first","params":{"model":"claude-fixture","max_tokens":1,"messages":[{"role":"user","content":"first item"}]}},{"custom_id":"second","params":{"model":"claude-fixture","max_tokens":1,"messages":[{"role":"user","content":"second item"}]}}]}`
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/messages/batches", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusForbidden, recorder.Code, recorder.Body.String())
	require.Len(t, engine.requests, 2)
	require.Contains(t, string(engine.requests[1].Body), "second item")
	require.Equal(t, service.ContentModerationProtocolAnthropicMessages, engine.requests[1].Protocol)
}
