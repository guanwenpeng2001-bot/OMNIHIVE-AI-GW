//go:build unit

package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/securityaudit"
	middleware "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

func TestQianwenRealtimeAuditsEveryTurnDespiteHTTPCache(t *testing.T) {
	engine := &turnCountingEngine{mode: securityaudit.ModeBlocking, decisions: []*securityaudit.PromptDecision{
		{Kind: securityaudit.DecisionAllow, AllowNextStage: true},
		{Kind: securityaudit.DecisionBlock, AllowNextStage: false},
	}}
	h := &OpenAIGatewayHandler{securityAuditCoordinator: securityaudit.NewCoordinator(nil, engine)}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/api-ws/v1/realtime", nil)
	c.Set(securityAuditCompletedContextKey, true)
	require.NoError(t, h.auditQianwenRealtimeTurn(context.Background(), c, nil, nil, middleware.AuthSubject{UserID: 7}, "qwen3-tts-flash-realtime", "same text", 1))
	require.Error(t, h.auditQianwenRealtimeTurn(context.Background(), c, nil, nil, middleware.AuthSubject{UserID: 7}, "qwen3-tts-flash-realtime", "same text", 2))
	require.Equal(t, int64(2), engine.evaluates.Load())
}

func TestQianwenRealtimeAuthenticationAndHandshakeAdmission(t *testing.T) {
	for _, tt := range []struct {
		name, platform, origin string
		auth, upgrade          bool
		status                 int
	}{
		{"missing_key", "qianwen", "", false, true, 401},
		{"wrong_platform", "openai", "", true, true, 404},
		{"no_upgrade", "qianwen", "", true, false, 426},
		{"cross_origin", "qianwen", "https://evil.test", true, true, 403},
		{"missing_billing_store", "qianwen", "", true, true, 503},
	} {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("GET", "/api-ws/v1/realtime?model=qwen3-tts-flash-realtime", nil)
			if tt.auth {
				c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 2, UserID: 1, User: &service.User{ID: 1}, Group: &service.Group{Platform: tt.platform}})
				c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1, Concurrency: 3})
			}
			if tt.upgrade {
				c.Request.Header.Set("Sec-WebSocket-Key", base64.StdEncoding.EncodeToString([]byte("0123456789abcdef")))
				c.Request.Header.Set("Sec-WebSocket-Version", "13")
				c.Request.Header.Set("Connection", "Upgrade")
				c.Request.Header.Set("Upgrade", "websocket")
			}
			c.Request.Header.Set("Origin", tt.origin)
			(&OpenAIGatewayHandler{}).QianwenNative(c)
			require.Equal(t, tt.status, w.Code)
			require.Empty(t, w.Header().Get("Upgrade"))
		})
	}
}

func TestQianwenNativeRealtimeRejectsBeforeHandshakeAndAdmission(t *testing.T) {
	for _, path := range []string{"/api-ws/v1/realtime/", "/api/v1/realtime", "/api/v1/realtime/tokens", "/api/v1/realtime/calls", "/api/v1/services/aigc/realtime/generation"} {
		for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
			t.Run(method+path, func(t *testing.T) {
				w := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(w)
				c.Request = httptest.NewRequest(method, path, strings.NewReader("not JSON"))
				c.Request.Header.Set("Connection", "Upgrade")
				c.Request.Header.Set("Upgrade", "websocket")
				// No service dependencies: rejection cannot dial, acquire slots or bill.
				h := &OpenAIGatewayHandler{}
				h.QianwenNative(c)
				require.Equal(t, http.StatusNotFound, w.Code)
				require.Empty(t, w.Header().Get("Upgrade"))
				require.Contains(t, w.Body.String(), "qianwen_realtime_unsupported")
				var response struct {
					Error struct {
						Type    string `json:"type"`
						Message string `json:"message"`
					} `json:"error"`
				}
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
				require.Equal(t, "unsupported_operation_error", response.Error.Type)
				require.Contains(t, response.Error.Message, "per-response text/audio usage billing")
				require.Contains(t, response.Error.Message, "audio_realtime_price_per_min cannot enable DashScope")
				_, selected := c.Get(opsAccountIDKey)
				require.False(t, selected)
			})
		}
	}
}

func TestQianwenNativeRealtimeGateDoesNotClaimOtherFamilies(t *testing.T) {
	for _, path := range []string{"/api-ws/v1/inference", "/api-ws/v1/realtime-other", "/api/v1/realtime-other", "/api/v1/services/aigc/multimodal-generation/generation", "/v1/realtime"} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodGet, path, nil)
		require.False(t, (&OpenAIGatewayHandler{}).rejectQianwenNativeRealtime(c), path)
	}
}

func TestQianwenRealtimeCompatHandshakeContract(t *testing.T) {
	for _, test := range []struct {
		name, query string
		upgrade     bool
		status      int
	}{
		{"non_websocket", "model=qwen3-asr-flash-realtime", false, 426},
		{"duplicate_model", "model=qwen3-asr-flash-realtime&model=qwen3-omni-flash-realtime", true, 400},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/realtime?"+test.query, nil)
			ctx.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 2, UserID: 1, User: &service.User{ID: 1}, Group: &service.Group{Platform: service.PlatformQianwen}})
			ctx.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1, Concurrency: 3})
			if test.upgrade {
				ctx.Request.Header.Set("Sec-WebSocket-Key", base64.StdEncoding.EncodeToString([]byte("0123456789abcdef")))
				ctx.Request.Header.Set("Sec-WebSocket-Version", "13")
				ctx.Request.Header.Set("Connection", "Upgrade")
				ctx.Request.Header.Set("Upgrade", "websocket")
			}
			(&OpenAIGatewayHandler{}).QianwenRealtimeCompat(ctx)
			require.Equal(t, test.status, recorder.Code)
		})
	}
}

func TestQianwenRealtimeInvalidHandshakePrecedesAdmission(t *testing.T) {
	for _, tc := range []struct {
		header string
		values []string
		status int
	}{
		{"Connection", []string{"not-upgrade"}, 426},
		{"Sec-WebSocket-Key", []string{"bad"}, 400},
		{"Sec-WebSocket-Key", []string{"one", "two"}, 400},
		{"Sec-WebSocket-Version", []string{"12"}, 400},
	} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("GET", "/v1/realtime?model=qwen", nil)
		c.Request.Header.Set("Connection", "Upgrade")
		c.Request.Header.Set("Upgrade", "websocket")
		c.Request.Header.Set("Sec-WebSocket-Key", base64.StdEncoding.EncodeToString([]byte("0123456789abcdef")))
		c.Request.Header.Set("Sec-WebSocket-Version", "13")
		c.Request.Header[http.CanonicalHeaderKey(tc.header)] = tc.values
		c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{User: &service.User{ID: 1}, Group: &service.Group{Platform: service.PlatformQianwen}})
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1})
		(&OpenAIGatewayHandler{}).QianwenRealtimeCompat(c)
		require.Equal(t, tc.status, c.Writer.Status())
	}
}
