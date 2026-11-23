package routes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/handler"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

type clientContractKeyRepo struct{ keyBillingRouteAPIKeyRepo }

func (*clientContractKeyRepo) UpdateLastUsed(context.Context, int64, time.Time) error { return nil }

// Use real authentication and the registered routes. A disallowed original
// model stops before scheduling; this is admission evidence, not a generation.
func TestGatewayClientAuthAndURLContracts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	group := &service.Group{ID: 1, Platform: service.PlatformOpenAI, Status: service.StatusActive, Hydrated: true,
		ModelAllowlist: service.GroupModelAllowlist{Enabled: true, Models: []string{"allowed"}}}
	key := &service.APIKey{WorkspaceID: 1, ID: 1, Key: "client-contract-key", UserID: 1, Status: service.StatusActive, Group: group, GroupID: &group.ID,
		User: &service.User{ID: 1, Status: service.StatusActive, Balance: 100}}
	cfg := &config.Config{RunMode: config.RunModeSimple, Gateway: config.GatewayConfig{MaxBodySize: 1 << 20, TextMaxBodySize: 1 << 20}}
	keys := service.NewAPIKeyService(&clientContractKeyRepo{keyBillingRouteAPIKeyRepo{apiKey: key}}, nil, nil, nil, &keyBillingRouteRateRepo{}, nil, cfg)
	router := gin.New()
	RegisterGatewayRoutes(router, &handler.Handlers{Gateway: &handler.GatewayHandler{}, OpenAIGateway: &handler.OpenAIGatewayHandler{}, AsyncImage: handler.NewAsyncImageHandler(nil, nil)},
		middleware.NewAPIKeyAuthMiddleware(keys, nil, cfg), keys, nil, nil, nil, nil, cfg)
	request := func(path, header, value string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"blocked-original","messages":[{"role":"user","content":"hello"}],"contents":[{"parts":[{"text":"hello"}]}]}`))
		req.Header.Set("Content-Type", "application/json")
		if header != "" {
			req.Header.Set(header, value)
		}
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		return recorder
	}
	paths := []string{"/v1/messages", "/messages", "/v1/chat/completions", "/chat/completions", "/v1/responses", "/responses", "/backend-api/codex/responses", "/v1beta/models/blocked-original:generateContent"}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			missing := request(path, "", "")
			require.Equal(t, http.StatusUnauthorized, missing.Code, missing.Body.String())
			for _, header := range []string{"Authorization", "x-api-key", "x-goog-api-key"} {
				value := key.Key
				if header == "Authorization" {
					value = "Bearer " + value
				}
				admitted := request(path, header, value)
				require.Equal(t, http.StatusNotFound, admitted.Code, admitted.Body.String())
				require.Contains(t, admitted.Body.String(), "blocked-original")
			}
			query := request(path+"?key="+key.Key, "", "")
			if strings.HasPrefix(path, "/v1beta/") {
				require.Equal(t, http.StatusNotFound, query.Code, query.Body.String())
				require.Contains(t, query.Body.String(), "blocked-original")
			} else {
				require.Equal(t, http.StatusBadRequest, query.Code, query.Body.String())
				require.Contains(t, query.Body.String(), "api_key_in_query_deprecated")
			}
		})
	}
	for _, path := range []string{"/v1", "/v1/v1/messages", "/v1/v1/chat/completions", "/v1/v1/responses", "/v1beta/v1beta/models/blocked-original:generateContent"} {
		got := request(path, "Authorization", "Bearer "+key.Key)
		require.Equal(t, http.StatusNotFound, got.Code, got.Body.String())
		require.NotContains(t, got.Body.String(), "blocked-original", "misconfigured base URLs must not be rewritten into a generation route")
	}
}
