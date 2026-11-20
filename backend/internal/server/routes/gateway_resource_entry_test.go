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

type resourceEntryKeyRepository struct{ keyBillingRouteAPIKeyRepo }

func (r *resourceEntryKeyRepository) UpdateLastUsed(context.Context, int64, time.Time) error {
	return nil
}

func TestResourceEntrypointsRequireAuthentication(t *testing.T) {
	router, _, _ := newKeyBillingRouteTestRouter(t, config.RunModeStandard)
	for _, route := range []string{
		"POST /v1/files", "GET /v1/files", "GET /v1/files/file-fixture",
		"GET /v1/files/file-fixture/content", "DELETE /v1/files/file-fixture",
		"POST /v1/batches", "GET /v1/batches", "GET /v1/batches/batch-fixture", "POST /v1/batches/batch-fixture/cancel",
		"POST /v1/moderations", "POST /v1/messages/batches", "GET /v1/messages/batches",
		"GET /v1/messages/batches/batch-fixture", "POST /v1/messages/batches/batch-fixture/cancel",
		"DELETE /v1/messages/batches/batch-fixture", "GET /v1/messages/batches/batch-fixture/results",
		"POST /v1/tools/search", "POST /v1/tools/search_pro", "POST /v1/videos/video-fixture/cancel",
	} {
		t.Run(route, func(t *testing.T) {
			method, path, _ := strings.Cut(route, " ")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(method, path, nil))
			t.Logf("%s -> %d", route, recorder.Code)
			require.Equal(t, http.StatusUnauthorized, recorder.Code, recorder.Body.String())
		})
		method, path, _ := strings.Cut(route, " ")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(method, strings.TrimPrefix(path, "/v1"), nil))
		require.Equal(t, http.StatusUnauthorized, recorder.Code, "root alias of %s", route)
	}
}

func TestResourceCredentialReachesHandler(t *testing.T) {
	for _, platform := range []string{service.PlatformOpenAI, service.PlatformAnthropic} {
		group := &service.Group{ID: 42, Platform: platform, Status: service.StatusActive, Hydrated: true, SubscriptionType: service.SubscriptionTypeStandard}
		key := &service.APIKey{ID: 100, UserID: 7, Key: "resource-entry-key", Status: service.StatusActive, GroupID: &group.ID, Group: group,
			User: &service.User{ID: 7, Status: service.StatusActive, Role: service.RoleUser, Balance: 10}}
		cfg := &config.Config{RunMode: config.RunModeStandard, Gateway: config.GatewayConfig{MaxBodySize: 1024, TextMaxBodySize: 1024}}
		keyService := service.NewAPIKeyService(&resourceEntryKeyRepository{keyBillingRouteAPIKeyRepo{apiKey: key}}, nil, nil, nil, &keyBillingRouteRateRepo{}, nil, cfg)
		openAI := handler.NewOpenAIGatewayHandler(&service.OpenAIGatewayService{}, nil, nil, keyService, nil, nil, nil, nil, cfg)
		router := gin.New()
		RegisterGatewayRoutes(router, &handler.Handlers{Gateway: &handler.GatewayHandler{}, OpenAIGateway: openAI,
			Resource: handler.NewGatewayResourceHandler(openAI, &service.UpstreamJobService{})},
			middleware.NewAPIKeyAuthMiddleware(keyService, nil, cfg), keyService, nil, nil, nil, nil, cfg)
		paths := []string{"/v1/files", "/files", "/v1/batches", "/batches"}
		if platform == service.PlatformAnthropic {
			paths = []string{"/v1/messages/batches", "/messages/batches"}
		}
		for _, path := range paths {
			request := httptest.NewRequest(http.MethodGet, path+"?limit=101", nil)
			request.Header.Set("Authorization", "Bearer "+key.Key)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			require.Equal(t, http.StatusBadRequest, recorder.Code, "%s: %s", path, recorder.Body.String())
			require.Contains(t, recorder.Body.String(), "limit must be 1-100", "credential must reach resource handler validation")
		}
	}
}

func TestNativeSearchAndCancelReachProductionBindings(t *testing.T) {
	for _, scenario := range []struct{ platform, path, adapter string }{
		{service.PlatformKimi, "/v1/tools/search", "kimi.search"},
		{service.PlatformKimi, "/v1/tools/search_pro", "kimi.search"},
		{service.PlatformQianfan, "/v1/videos/video-fixture/cancel", "qianfan.video.cancel"},
	} {
		router := newGatewayRoutesTestRouter(scenario.platform)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, scenario.path, strings.NewReader(`{"text_query":"fixture"}`)))
		// The test router authenticates a key but deliberately has no subject.
		// This exact response is emitted by each real handler, after dispatch.
		require.Equal(t, http.StatusInternalServerError, recorder.Code, scenario.adapter)
		require.Contains(t, recorder.Body.String(), "User context not found", scenario.adapter)
	}
}
