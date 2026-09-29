package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestGatewayProtocolEnvelopesAndQuota(t *testing.T) {
	for _, scenario := range []struct {
		path, header, reason, kind string
		status                     int
	}{
		{"/v1/chat/completions", "", "INSUFFICIENT_BALANCE", "insufficient_quota", 429},
		{"/v1/messages", "", "INSUFFICIENT_BALANCE", "invalid_request_error", 429},
		{"/v1/models", "2023-06-01", "ACCESS_DENIED", "permission_error", 403},
		{"/v1beta/models/model:generateContent", "", "INSUFFICIENT_BALANCE", "RESOURCE_EXHAUSTED", 429},
		{"/v1/chat/completions", "", "ACCESS_DENIED", "permission_error", 403},
	} {
		t.Run(scenario.path+scenario.reason, func(t *testing.T) {
			router := gin.New()
			router.Use(GatewayProtocol())
			router.POST(scenario.path, func(ctx *gin.Context) {
				ctx.Header("Retry-After", "60")
				AbortWithError(ctx, 403, scenario.reason, "Request rejected")
			})
			request := httptest.NewRequest("POST", scenario.path, nil)
			request.Header.Set("anthropic-version", scenario.header)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			require.Equal(t, scenario.status, recorder.Code)
			require.Contains(t, recorder.Body.String(), scenario.kind)
			require.False(t, gjson.GetBytes(recorder.Body.Bytes(), "reason").Exists())
			require.False(t, gjson.GetBytes(recorder.Body.Bytes(), "metadata").Exists())
			if scenario.reason == "INSUFFICIENT_BALANCE" {
				require.Empty(t, recorder.Header().Get("Retry-After"))
			}
			require.Contains(t, strings.Join(recorder.Header().Values("Vary"), ","), "anthropic-version")
		})
	}
}
func TestGatewayProtocolWrapsLegacyAndRawErrors(t *testing.T) {
	for _, payload := range []string{`{"code":"INVALID_API_KEY","message":"Key rejected"}`, `{"error":"Key rejected"}`, `{"error":{"type":"authentication_error","message":"Key rejected"}}`, `<html>private upstream response</html>`} {
		router := gin.New()
		router.Use(GatewayProtocol())
		router.GET("/v1/models", func(ctx *gin.Context) {
			ctx.Data(401, "text/plain", []byte(payload))
			require.True(t, ctx.Writer.Written())
		})
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest("GET", "/v1/models", nil))
		require.Equal(t, 401, recorder.Code)
		require.Equal(t, "authentication_error", gjson.GetBytes(recorder.Body.Bytes(), "error.type").String())
		require.NotContains(t, recorder.Body.String(), "private")
	}
}
func TestGatewayCORSProtocolHeaders(t *testing.T) {
	for _, allowed := range [][]string{{"https://client.example"}, {"*"}, nil} {
		router := gin.New()
		router.Use(CORS(config.CORSConfig{AllowedOrigins: allowed}))
		router.POST("/v1/messages", func(ctx *gin.Context) { ctx.Status(200) })
		request := httptest.NewRequest(http.MethodOptions, "/v1/messages", nil)
		request.Header.Set("Origin", "https://client.example")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		if len(allowed) == 0 {
			require.Equal(t, 403, recorder.Code)
			continue
		}
		require.Equal(t, 204, recorder.Code)
		headers := strings.ToLower(recorder.Header().Get("Access-Control-Allow-Headers"))
		for _, header := range []string{"anthropic-version", "anthropic-beta", "x-goog-api-key", "openai-project", "x-stainless-lang"} {
			require.Contains(t, headers, header)
		}
	}
}

func TestGoogleQueryKeyNativeBoundary(t *testing.T) {
	for _, scenario := range []struct{ path, want string }{
		{"/v1beta/models?key=native-key", "native-key"},
		{"/antigravity/v1beta/models?key=native-key", "native-key"},
		{"/v1/models?key=unsupported", ""},
		{"/v1beta-fake/models?key=unsupported", ""},
	} {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest("GET", scenario.path, nil)
		require.Equal(t, scenario.want, extractAPIKeyForGoogle(ctx))
	}
}
func TestAllowlistBodyErrorsKeepInboundProtocol(t *testing.T) {
	for _, scenario := range []struct{ path, marker string }{
		{"/v1/messages", `"type":"error"`},
		{"/v1beta/models/fixture:generateContent", `"status":"RESOURCE_EXHAUSTED"`},
		{"/v1/chat/completions", `"type":"invalid_request_error"`},
	} {
		router := gin.New()
		router.Use(GatewayProtocol(), RequestBodyLimit(2))
		router.POST(scenario.path, func(ctx *gin.Context) { _, valid := groupModelAllowlistModelsFromBody(ctx); require.False(t, valid) })
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest("POST", scenario.path, strings.NewReader(`{"model":"fixture"}`)))
		require.Equal(t, 413, recorder.Code)
		require.Contains(t, recorder.Body.String(), scenario.marker)
	}
}

func TestUnknownGatewayRouteNeverUsesFrontendFallback(t *testing.T) {
	router := gin.New()
	router.Use(GatewayUnknownRouteProtocol())
	router.NoRoute(func(ctx *gin.Context) { ctx.String(200, "<html>frontend</html>") })
	for _, scenario := range []struct{ path, version, marker string }{
		{"/v1/unknown", "", `"error"`},
		{"/v1/unknown", "2023-06-01", `"type":"error"`},
		{"/v1beta/unknown", "", `"status":"NOT_FOUND"`},
		{"/messages/unknown", "", `"type":"error"`},
	} {
		request := httptest.NewRequest("GET", scenario.path, nil)
		request.Header.Set("anthropic-version", scenario.version)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		require.Equal(t, 404, recorder.Code)
		require.Contains(t, recorder.Body.String(), scenario.marker)
		require.NotContains(t, recorder.Body.String(), "frontend")
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/unrelated-page", nil))
	require.Equal(t, 200, recorder.Code)
	require.Contains(t, recorder.Body.String(), "frontend")
}
