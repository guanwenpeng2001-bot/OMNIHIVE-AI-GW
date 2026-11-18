package handler

import (
	"crypto/tls"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestLiveOriginContract(t *testing.T) {
	for _, scenario := range []struct {
		origin       string
		allowed      []string
		secure, want bool
	}{
		{"", nil, false, true}, {"http://gateway.example", nil, false, true}, {"https://gateway.example", nil, true, true}, {"https://gateway.example", nil, false, false}, {"https://evil.example", nil, false, false}, {"null", []string{"*"}, false, true}, {"null", nil, false, false}, {"https://client.example", []string{"https://client.example"}, false, true}, {"https://client.example", []string{"*"}, false, true},
	} {
		request := httptest.NewRequest("GET", "http://gateway.example/v1/live/call", nil)
		request.Header.Set("Origin", scenario.origin)
		if scenario.secure {
			request.TLS = &tls.ConnectionState{}
		}
		require.Equal(t, scenario.want, liveOriginAllowed(request, &config.Config{CORS: config.CORSConfig{AllowedOrigins: scenario.allowed}}), scenario.origin)
	}
}
func TestMessagesMaxTokensContract(t *testing.T) {
	for _, body := range []string{`{}`, `{"max_tokens":0}`, `{"max_tokens":-1}`, `{"max_tokens":1.5}`, `{"max_tokens":"5"}`, `{"max_tokens":2147483648}`, `{"max_tokens":1.0}`, `{"max_tokens":1e3}`, `{"max_tokens":1,"max_tokens":-1}`, `{"max_tokens":1,"MAX_TOKENS":-1}`} {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest("POST", "/v1/messages", nil)
		require.False(t, validateMessagesMaxTokens(ctx, []byte(body)))
		require.Equal(t, 400, recorder.Code)
		require.Equal(t, "error", gjson.GetBytes(recorder.Body.Bytes(), "type").String())
	}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("POST", "/messages", nil)
	require.True(t, validateMessagesMaxTokens(ctx, []byte(`{"max_tokens":1}`)))
}
func TestAnthropicModelPaginationAndNegotiation(t *testing.T) {
	account := publicMetadataAccount()
	account.Credentials["model_mapping"] = map[string]any{"alpha": "gpt-native", "beta": "gpt-native", "gamma": "gpt-native"}
	handler := newGatewayModelsHandlerForTest(&publicMetadataRepository{accounts: []service.Account{account}})
	request := func(query, version string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest("GET", "/v1/models"+query, nil)
		ctx.Request.Header.Set("anthropic-version", version)
		ctx.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 1, Group: &service.Group{ID: 31, Platform: service.PlatformOpenAI}})
		handler.Models(ctx)
		return recorder
	}
	first := request("?limit=1", "2023-06-01")
	require.Equal(t, 200, first.Code)
	require.True(t, gjson.GetBytes(first.Body.Bytes(), "has_more").Bool())
	require.Equal(t, "alpha", gjson.GetBytes(first.Body.Bytes(), "first_id").String())
	require.Equal(t, "model", gjson.GetBytes(first.Body.Bytes(), "data.0.type").String())
	next := request("?after_id=alpha&limit=1", "2023-06-01")
	require.Equal(t, "beta", gjson.GetBytes(next.Body.Bytes(), "last_id").String())
	previous := request("?before_id=gamma&limit=1", "2023-06-01")
	require.Equal(t, "beta", gjson.GetBytes(previous.Body.Bytes(), "first_id").String())
	require.True(t, gjson.GetBytes(previous.Body.Bytes(), "has_more").Bool())
	// has_more is directional, not a claim that the entire catalog is exhausted.
	last := request("?after_id=beta&limit=1", "2023-06-01")
	require.False(t, gjson.GetBytes(last.Body.Bytes(), "has_more").Bool())
	require.Equal(t, "gamma", gjson.GetBytes(last.Body.Bytes(), "last_id").String())
	beginning := request("?before_id=beta&limit=1", "2023-06-01")
	require.False(t, gjson.GetBytes(beginning.Body.Bytes(), "has_more").Bool())
	require.Equal(t, "alpha", gjson.GetBytes(beginning.Body.Bytes(), "first_id").String())
	for _, query := range []string{"?before_id=alpha", "?after_id=gamma"} {
		empty := request(query, "2023-06-01")
		require.Equal(t, 200, empty.Code)
		require.JSONEq(t, `{"data":[],"first_id":null,"last_id":null,"has_more":false}`, empty.Body.String())
	}
	require.Equal(t, 400, request("?after_id=alpha&before_id=gamma", "2023-06-01").Code)
	require.Equal(t, 400, request("?limit=0", "2023-06-01").Code)
	require.Equal(t, 400, request("?after_id=missing", "2023-06-01").Code)
	ordinary := request("", "")
	require.Equal(t, "list", gjson.GetBytes(ordinary.Body.Bytes(), "object").String())
	require.False(t, gjson.GetBytes(ordinary.Body.Bytes(), "data.0.type").Exists())
}
func TestLegacyCodexMigrationExpires(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("GET", "/v1/models?client_version=explicit", nil)
	require.True(t, legacyCodexModelsRequestedAt(ctx, time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)))
	require.NotEmpty(t, recorder.Header().Get("Sunset"))
	require.Contains(t, recorder.Header().Get("Link"), "/backend-api/codex/models")
	require.False(t, legacyCodexModelsRequestedAt(ctx, time.Date(2027, 3, 21, 0, 0, 0, 0, time.UTC)))
}
func TestGoogleModelPaginationUsesAuthorizedRevision(t *testing.T) {
	account := publicMetadataAccount()
	handler := newGatewayModelsHandlerForTest(&publicMetadataRepository{accounts: []service.Account{account}})
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("GET", "/v1beta/models?pageSize=1", nil)
	ctx.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 1, Group: &service.Group{ID: 31, Platform: service.PlatformOpenAI}})
	handler.GeminiV1BetaListModels(ctx)
	require.Equal(t, 200, recorder.Code)
	var response map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Len(t, gjson.GetBytes(recorder.Body.Bytes(), "models").Array(), 1)
	require.False(t, gjson.GetBytes(recorder.Body.Bytes(), "models.0.inputTokenLimit").Exists(), "total context must not be relabeled input")
	require.False(t, gjson.GetBytes(recorder.Body.Bytes(), "models.0.version").Exists(), "unknown version cannot be fabricated")
}
