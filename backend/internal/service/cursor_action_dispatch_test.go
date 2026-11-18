package service

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// Every Cursor cell must resolve through the real dispatcher: implemented cells
// reach their own adapter, and every remaining cell fails closed instead of
// falling back to another provider's adapter. This is the Cursor counterpart of
// the shared dispatch boundary test and pins "never default normalise Cursor
// into the OpenAI trunk".
func TestCursorActionDispatchReachesOnlyCursorAdapters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handlers := GatewayActionHandlers{}
	called := ""
	record := func(adapter string) gin.HandlerFunc {
		return func(requestContext *gin.Context) {
			called = adapter
			requestContext.Status(http.StatusNoContent)
		}
	}
	for _, adapter := range []string{
		"gateway.chat", "gateway.messages", "gateway.responses", "cursor.count_tokens",
		"gateway.generate_content_compat",
		"openai.chat", "openai.messages", "openai.responses", "openai.count_tokens",
		"openai.embeddings", "openai.images", "openai.video.generate", "openai.realtime",
		"grok.images", "grok.video.generate", "grok.realtime", "grok.search",
		"qianwen.native", "qianwen.images",
	} {
		handlers[adapter] = record(adapter)
	}
	router := gin.New()
	router.Use(func(requestContext *gin.Context) {
		requestContext.Set("api_key", &APIKey{Group: &Group{Platform: PlatformCursor}})
	})
	dispatch := NewGatewayActionDispatch(handlers)
	identity := domain.ActionIdentity{Platform: PlatformCursor}
	for _, action := range domain.GatewayActions() {
		router.POST("/probe/"+action, dispatch.Handler(action))
	}
	for _, action := range domain.GatewayActions() {
		called = ""
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/probe/"+action, strings.NewReader(`{"model":"cursor-fixture"}`)))
		cell := domain.CursorAction(identity, action)
		if cell.Status == domain.ActionImplemented {
			require.Equal(t, cell.Adapter, called, "cursor %s must reach its own adapter", action)
			require.Equal(t, http.StatusNoContent, recorder.Code, "cursor %s", action)
			continue
		}
		require.Empty(t, called, "cursor %s must not borrow another platform adapter", action)
		require.Equal(t, http.StatusBadRequest, recorder.Code, "cursor %s must fail closed", action)
		require.Contains(t, recorder.Body.String(), "action_not_implemented", "cursor %s", action)
	}
}
