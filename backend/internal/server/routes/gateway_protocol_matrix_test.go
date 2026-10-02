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
					// Exercise the wire family used by the account's actual adapter.
					// Cursor Responses and TokenHub's non-native Responses branch
					// bridge through Chat Completions; they do not send /responses.
					wireEndpoint := endpoint
					if endpoint == "messages" {
						switch descriptor.ID {
						case service.PlatformMistral:
							wireEndpoint = "chat_completions"
						case service.PlatformVolcengine, service.PlatformQianfan:
							wireEndpoint = "responses"
						}
					}
					if endpoint == "responses" && (descriptor.ID == service.PlatformCursor || descriptor.ID == service.PlatformHunyuan || descriptor.ID == service.PlatformMistral) {
						wireEndpoint = "chat_completions"
					}
					if endpoint == "responses" && descriptor.ID == service.PlatformGemini {
						wireEndpoint = "generate_content"
					}
					if endpoint == "generate_content" && domain.PlatformAction(domain.ActionIdentity{Platform: descriptor.ID}, endpoint).Adapter == "gateway.generate_content_compat" {
						switch descriptor.ID {
						case service.PlatformAnthropic, service.PlatformBedrock:
							wireEndpoint = "messages"
						case service.PlatformCursor, service.PlatformMistral, service.PlatformOpenRouter, service.PlatformHunyuan:
							wireEndpoint = "chat_completions"
						default:
							wireEndpoint = "responses"
						}
					}
					if account.IsMultiProtocolAPIKey() {
						if wireEndpoint == "messages" {
							account.Credentials = map[string]any{"api_protocol": service.APIProtocolAnthropic}
						}
						if wireEndpoint == "responses" && descriptor.NativeMultiProtocolResponses && descriptor.ID != service.PlatformCommandCode {
							account.Credentials = map[string]any{"api_protocol": service.APIProtocolResponses}
						}
					}
					catalog := service.AccountModelCatalogSnapshot{Complete: true, DiscoveredModels: []string{"fixture-model"}, EligibleModels: []service.AccountCatalogModel{{ID: "fixture-model", Endpoints: []string{wireEndpoint}, EvidenceKind: "synthetic-contract"}}}
					account.Extra = map[string]any{service.UpstreamModelCatalogExtraKey: catalog}
					options := service.AccountModelBindingOptions{Endpoint: wireEndpoint, FinalModel: "fixture-model"}
					action := map[string]string{"messages": "messages", "responses": "responses", "chat_completions": "chat", "generate_content": "generate_content", "embeddings": "embeddings", "images": "image.generate", "live": "live"}[endpoint]
					ctx := service.WithGatewayRequestPlan(context.Background(), service.NewGatewayRequestPlan(service.GatewayRequestSpec{Action: action}))
					cell := domain.PlatformAction(domain.ActionIdentity{Platform: descriptor.ID, HostedAccountType: accountType}, action)
					require.Equal(test, cell.Status == domain.ActionImplemented, service.ResolveAccountModelBinding(ctx, account, "public-alias", options).Supported, "representative account must bind every implemented adapter")
					if account.IsMultiProtocolAPIKey() && (wireEndpoint == "messages" || wireEndpoint == "responses") {
						credentials := account.Credentials
						account.Credentials = map[string]any{"api_protocol": service.APIProtocolChatCompletions}
						require.False(test, service.ResolveAccountModelBinding(context.Background(), account, "public-alias", options).Supported, "fixed chat protocol cannot send the other wire family")
						account.Credentials = credentials
					}
					if wireEndpoint != endpoint {
						incompatible := options
						incompatible.Endpoint = endpoint
						require.False(test, service.ResolveAccountModelBinding(context.Background(), account, "public-alias", incompatible).Supported, "bridge support must not pretend native wire support")
					}
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
	for _, platform := range []string{service.PlatformVolcengine, service.PlatformQianfan, service.PlatformHunyuan, service.PlatformMistral, service.PlatformOpenRouter, service.PlatformBedrock} {
		router := protocolMatrixRouter(platform)
		for _, route := range router.Routes() {
			path := concreteMatrixPath(route.Path)
			if strings.HasPrefix(path, "/antigravity/") || path == "/v1/sub2api/billing" {
				continue
			}
			if route.Method == http.MethodGet {
				switch route.Path {
				case "/models", "/models/:model", "/v1/models", "/v1/models/:model", "/v1/models/catalog", "/v1/models/catalog/:model",
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
						if strings.HasPrefix(request.URL.Path, "/v1beta/") {
							// The action remains a 400 rejection; GatewayProtocol renders
							// the native Google equivalent of invalid_request_error.
							var envelope struct {
								Error struct {
									Code   int    `json:"code"`
									Status string `json:"status"`
								} `json:"error"`
							}
							require.NoError(test, json.Unmarshal(recorder.Body.Bytes(), &envelope))
							require.Equal(test, http.StatusBadRequest, envelope.Error.Code)
							require.Equal(test, "INVALID_ARGUMENT", envelope.Error.Status)
						} else {
							require.Contains(test, recorder.Body.String(), "invalid_request_error")
						}
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
	test.Run("other-platform-videos", TestGatewayRoutesOpenAIVideosReachTheOpenAIVideoIngress)
	for _, platform := range service.ConcretePlatforms() {
		router := protocolMatrixRouter(platform)
		for _, feature := range []struct {
			method, path string
			action       string
		}{
			{http.MethodPost, "/embeddings", "embeddings"},
			{http.MethodPost, "/images/generations", "image.generate"},
			{http.MethodPost, "/images/edits", "image.edit"},
			{http.MethodPost, "/images/variations", "image.variation"},
			{http.MethodPost, "/videos/generations", "video.generate"},
			{http.MethodPost, "/videos/edits", "video.edit"},
			{http.MethodPost, "/videos/extensions", "video.extend"},
			{http.MethodPost, "/tts", "audio.tts"},
			{http.MethodPost, "/stt", "audio.stt"},
			{http.MethodPost, "/audio/speech", "audio.tts"},
			{http.MethodPost, "/audio/transcriptions", "audio.stt"},
			{http.MethodPost, "/audio/translations", "audio.translate"},
			{http.MethodPost, "/web_search", "search"},
			{http.MethodPost, "/x_search", "search"},
			{http.MethodGet, "/realtime", "realtime"},
		} {
			// The allow list is the platform action matrix itself, not a second
			// hand-maintained table: a platform whose cell binds an adapter must
			// serve the route, and every other platform must still fail closed
			// with the local 400 before scheduling, transport or billing.
			if domain.PlatformAction(domain.ActionIdentity{Platform: platform}, feature.action).Status == domain.ActionImplemented {
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
	}, service.GatewayActionRequirement)
	router.POST("/v1/responses/compact", func(requestContext *gin.Context) {
		upstreamCalls++
		billingCalls++
		requestContext.Status(http.StatusOK)
	})
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/v1/responses/compact", strings.NewReader(`{"model":"fixture"}`)))
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
