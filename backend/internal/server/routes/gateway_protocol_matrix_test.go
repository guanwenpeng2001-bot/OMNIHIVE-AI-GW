package routes

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func protocolMatrixRouter(platform string) *gin.Engine {
	group := &service.Group{ID: 1, Platform: platform, Status: service.StatusActive, Hydrated: true}
	key := &service.APIKey{Key: "matrix-key", UserID: 1, Status: service.StatusActive, Group: group, GroupID: &group.ID,
		User: &service.User{ID: 1, Status: service.StatusActive}}
	cfg := &config.Config{RunMode: config.RunModeSimple, Gateway: config.GatewayConfig{MaxBodySize: 1 << 20, TextMaxBodySize: 1 << 20}}
	keyService := service.NewAPIKeyService(&keyBillingRouteAPIKeyRepo{apiKey: key}, nil, nil, nil, &keyBillingRouteRateRepo{}, nil, cfg)
	router := gin.New()
	RegisterGatewayRoutes(router, &handler.Handlers{Gateway: &handler.GatewayHandler{}, OpenAIGateway: &handler.OpenAIGatewayHandler{}, AsyncImage: handler.NewAsyncImageHandler(nil, nil)},
		middleware.APIKeyAuthMiddleware(func(requestContext *gin.Context) {
			requestContext.Set(string(middleware.ContextKeyAPIKey), key)
			requestContext.Next()
		}),
		keyService, nil, nil, nil, nil, cfg)
	return router
}

// The registered-route inventory and the account evidence gate are separate
// contracts: a registered route alone is never a promise of upstream support.
func TestGatewayPlatformProtocolMatrix(test *testing.T) {
	for _, descriptor := range domain.PlatformDescriptors() {
		accountTypes := []string{service.AccountTypeAPIKey}
		if descriptor.OAuth {
			accountTypes = append(accountTypes, service.AccountTypeOAuth)
		}
		if descriptor.ID == service.PlatformAnthropic {
			accountTypes = append(accountTypes, service.AccountTypeSetupToken, service.AccountTypeBedrock)
		}
		for _, accountType := range accountTypes {
			for _, endpoint := range []string{"messages", "responses", "chat_completions", "generate_content", "embeddings", "images", "live"} {
				test.Run(descriptor.ID+"/"+accountType+"/"+endpoint, func(test *testing.T) {
					account := &service.Account{ID: 1, Platform: descriptor.ID, Type: accountType, Status: service.StatusActive, Schedulable: true}
					catalog := service.AccountModelCatalogSnapshot{Complete: true, DiscoveredModels: []string{"fixture-model"}, EligibleModels: []service.AccountCatalogModel{{ID: "fixture-model", Endpoints: []string{endpoint}, EvidenceKind: "synthetic-contract"}}}
					account.Extra = map[string]any{service.UpstreamModelCatalogExtraKey: catalog}
					options := service.AccountModelBindingOptions{Endpoint: endpoint, FinalModel: "fixture-model"}
					action := map[string]string{"messages": "messages", "responses": "responses", "chat_completions": "chat", "generate_content": "generate_content", "embeddings": "embeddings", "images": "image.generate", "live": "live"}[endpoint]
					ctx := service.WithGatewayRequestPlan(context.Background(), service.NewGatewayRequestPlan(service.GatewayRequestSpec{Action: action}))
					cell := domain.PlatformAction(domain.ActionIdentity{Platform: descriptor.ID, HostedAccountType: accountType}, action)
					require.Equal(test, cell.Status == domain.ActionImplemented, service.ResolveAccountModelBinding(ctx, account, "public-alias", options).Supported, "action evidence cannot enable a missing adapter")
					catalog.EligibleModels[0].Endpoints = []string{"unrelated-endpoint"}
					account.Extra[service.UpstreamModelCatalogExtraKey] = catalog
					require.False(test, service.ResolveAccountModelBinding(context.Background(), account, "public-alias", options).Supported)
					account.Extra = nil
					require.False(test, service.ResolveAccountModelBinding(context.Background(), account, "public-alias", options).Supported)
				})
			}
		}
	}
	// Existing tests exercise the actual dispatch branches and preserve their
	// protocol-specific errors, rather than rewriting them to match descriptors.
	test.Run("pinned-and-client-version", TestGatewayRoutesPinnedModelsDispatchesOrdinaryAndCodexRequests)
	test.Run("composite-messages", TestGatewayRoutesCompositeMessagesWithGrokModelUsesOpenAIGateway)
	test.Run("composite-chat", TestGatewayRoutesCompositeChatCompletionsWithGrokModelUsesOpenAIGateway)
	test.Run("composite-video-lookups", TestGatewayRoutesCompositeVideoLookupsUseGrokHandler)
	test.Run("composite-openai-only", TestGatewayRoutesCompositeOpenAIOnlyEndpointsRequireOpenAITarget)
	test.Run("grok-cli-compatibility", TestGatewayRoutesGrokAllowsCLICompatibilityEntrypoints)
}

func concreteMatrixPath(path string) string {
	for _, parameter := range []string{":model", ":call_id", ":task_id", ":request_id", ":voice_id", ":custom_id", ":id"} {
		path = strings.ReplaceAll(path, parameter, "fixture")
	}
	path = strings.ReplaceAll(path, "*subpath", "unsupported-action")
	path = strings.ReplaceAll(path, "*modelAction", "fixture:generateContent")
	return strings.ReplaceAll(path, "*nativePath", "unsupported-action")
}

func TestGatewayUnsupportedProtocolContracts(test *testing.T) {
	for _, platform := range []string{service.PlatformVolcengine, service.PlatformQianfan, service.PlatformHunyuan, service.PlatformMistral, service.PlatformBedrock} {
		router := protocolMatrixRouter(platform)
		for _, route := range router.Routes() {
			path := concreteMatrixPath(route.Path)
			if strings.HasPrefix(path, "/antigravity/") || path == "/v1/sub2api/billing" {
				continue
			}
			if route.Method == http.MethodGet {
				switch route.Path {
				case "/models", "/models/:model", "/v1/models", "/v1/models/:model",
					"/v1beta/models", "/v1beta/models/:model", "/backend-api/codex/models", "/v1/usage":
					continue // Supported catalog and usage contracts are tested by the guard suite.
				}
			}
			action := service.GatewayActionForRoute(route.Method, path)
			if action == "" || domain.PlatformAction(domain.ActionIdentity{Platform: platform}, action).Status == domain.ActionImplemented {
				continue
			}
			for _, stream := range []bool{false, true} {
				for _, withTools := range []bool{false, true} {
					test.Run(fmt.Sprintf("%s/%s%s/stream=%t/tools=%t", platform, route.Method, path, stream, withTools), func(test *testing.T) {
						payload := gin.H{"model": "fixture-model", "stream": stream}
						if withTools {
							payload["tools"] = []gin.H{{"type": "function", "function": gin.H{"name": "fixture"}}}
						}
						body, err := json.Marshal(payload)
						require.NoError(test, err)
						recorder := httptest.NewRecorder()
						request := httptest.NewRequest(route.Method, path, strings.NewReader(string(body)))
						request.Header.Set("Content-Type", "application/json")
						request.Header.Set("Authorization", "Bearer matrix-key")
						router.ServeHTTP(recorder, request)
						require.Equal(test, http.StatusBadRequest, recorder.Code, recorder.Body.String())
						require.Contains(test, recorder.Body.String(), "invalid_request_error")
						// The real router has no transport or billing services. Reaching
						// either would panic instead of returning this local rejection.
					})
				}
			}
		}
	}
	test.Run("dashscope-resources", TestQianwenNativeUnsupportedFamilies)
	test.Run("dashscope-owned-resource-gates", TestQianwenNativeResourceLifecycleRoutes)
	test.Run("responses-actions", TestGatewayRoutesResponsesSubpathRejectsNonConformingSubpaths)
	test.Run("other-platform-videos", TestGatewayRoutesNonGrokVideosAreRejectedAtPlatformGate)
	for _, platform := range service.ConcretePlatforms() {
		router := protocolMatrixRouter(platform)
		for _, feature := range []struct {
			method, path string
			platforms    []string
		}{
			{http.MethodPost, "/embeddings", []string{service.PlatformOpenAI}},
			{http.MethodPost, "/images/generations", []string{service.PlatformOpenAI, service.PlatformGrok}},
			{http.MethodPost, "/images/edits", []string{service.PlatformOpenAI, service.PlatformGrok}},
			{http.MethodPost, "/videos/generations", []string{service.PlatformGrok}},
			{http.MethodPost, "/videos/edits", []string{service.PlatformGrok}},
			{http.MethodPost, "/videos/extensions", []string{service.PlatformGrok}},
			{http.MethodPost, "/tts", []string{service.PlatformGrok}},
			{http.MethodPost, "/stt", []string{service.PlatformGrok}},
			{http.MethodPost, "/web_search", []string{service.PlatformGrok}},
			{http.MethodPost, "/x_search", []string{service.PlatformGrok}},
			{http.MethodGet, "/realtime", []string{service.PlatformGrok}},
		} {
			if slices.Contains(feature.platforms, platform) {
				continue
			}
			for _, prefix := range []string{"", "/v1"} {
				for _, body := range []string{`{"model":"fixture-model","stream":false}`, `{"model":"fixture-model","stream":true,"tools":[{"type":"function","function":{"name":"fixture"}}]}`} {
					test.Run("feature-gate/"+platform+"/"+feature.method+prefix+feature.path, func(test *testing.T) {
						recorder := httptest.NewRecorder()
						request := httptest.NewRequest(feature.method, prefix+feature.path, strings.NewReader(body))
						request.Header.Set("Content-Type", "application/json")
						router.ServeHTTP(recorder, request)
						require.Equal(test, http.StatusBadRequest, recorder.Code, recorder.Body.String())
						require.Contains(test, recorder.Body.String(), "invalid_request_error")
					})
				}
			}
		}
	}
	// A counter proves the reject occurs before any downstream work.
	router := gin.New()
	upstreamCalls, billingCalls := 0, 0
	router.Use(func(requestContext *gin.Context) {
		requestContext.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{Group: &service.Group{Platform: service.PlatformMistral}})
	}, chatOnlyPlatformGuard)
	router.POST("/v1/responses", func(requestContext *gin.Context) {
		upstreamCalls++
		billingCalls++
		requestContext.Status(http.StatusOK)
	})
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"fixture"}`)))
	require.Zero(test, upstreamCalls)
	require.Zero(test, billingCalls)
}

func TestGatewayProtocolMatrixDocumentation(test *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "docs", "gateway-platform-protocol-matrix.md"))
	require.NoError(test, err)
	document := string(body)
	var registered []string
	for _, route := range newGatewayRoutesTestRouter().Routes() {
		registered = append(registered, route.Method+" "+route.Path)
	}
	slices.Sort(registered)
	for _, route := range registered {
		require.Contains(test, document, "| `"+route+"` |", "registered method/path absent from maintained matrix")
	}
	var documented []string
	for _, line := range strings.Split(document, "\n") {
		if strings.HasPrefix(line, "| `") {
			candidate := strings.SplitN(strings.TrimPrefix(line, "| `"), "`", 2)[0]
			if strings.HasPrefix(candidate, "GET ") || strings.HasPrefix(candidate, "POST ") || strings.HasPrefix(candidate, "PUT ") || strings.HasPrefix(candidate, "PATCH ") || strings.HasPrefix(candidate, "DELETE ") {
				documented = append(documented, candidate)
			}
		}
	}
	slices.Sort(documented)
	require.Equal(test, registered, documented, "documentation must not advertise unregistered routes")
	for _, platform := range domain.ConcretePlatforms() {
		require.Contains(test, document, "| "+platform+" |")
	}
	require.False(test, domain.IsConcretePlatform("workbuddy"))
}
