package service

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func grokCompositeTestRouter(groupPlatform string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(requestContext *gin.Context) {
		requestContext.Set("api_key", &APIKey{Group: &Group{Platform: groupPlatform}})
	})
	return router
}

func grokCompositeJSONRequest(method, target, body string) *http.Request {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	return request
}

// Model-less Grok-only actions (voice endpoints) must resolve Composite groups
// to Grok when no explicit composite route decision exists, while groups of
// other platforms keep their documented rejection.
func TestGrokCompositeVoiceHandlerPinsUnresolvedTarget(t *testing.T) {
	for _, fixture := range []struct {
		name          string
		groupPlatform string
		wantStatus    int
	}{
		{"composite without model pins grok", PlatformComposite, http.StatusNoContent},
		{"anthropic group rejected", PlatformAnthropic, http.StatusBadRequest},
		{"openai group rejected", PlatformOpenAI, http.StatusBadRequest},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			router := grokCompositeTestRouter(fixture.groupPlatform)
			called := false
			router.POST("/v1/tts", GatewayVoiceHandler(func(requestContext *gin.Context, endpoint string) {
				called = true
				require.Equal(t, "tts", endpoint)
				require.Equal(t, PlatformGrok, GatewayTargetPlatform(requestContext))
				platform, resolved := ResolvedTargetPlatformFromContext(requestContext.Request.Context())
				require.True(t, resolved, "pin must be visible to quota and account selection")
				require.Equal(t, PlatformGrok, platform)
				requestContext.Status(http.StatusNoContent)
			}, func(*gin.Context) string { return "tts" }))
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, grokCompositeJSONRequest(http.MethodPost, "/v1/tts", `{"text":"hello","voice_id":"eve"}`))
			require.Equal(t, fixture.wantStatus, recorder.Code)
			require.Equal(t, fixture.wantStatus == http.StatusNoContent, called)
		})
	}
}

// An explicit composite resolution always wins over the Grok-only pin: a voice
// request whose body model resolves to a platform without the action adapter
// is still rejected.
func TestGrokCompositeVoiceExplicitResolutionWinsOverPin(t *testing.T) {
	for _, fixture := range []struct {
		name       string
		body       string
		wantStatus int
		wantCalled bool
	}{
		{"grok model resolves to grok", `{"model":"grok-voice-latest","text":"hello","voice_id":"eve"}`, http.StatusNoContent, true},
		{"openai model has no tts adapter", `{"model":"gpt-4o","text":"hello"}`, http.StatusBadRequest, false},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			router := grokCompositeTestRouter(PlatformComposite)
			router.Use(CompositeTargetPlatformMiddleware(nil))
			called := false
			router.POST("/v1/tts", GatewayVoiceHandler(func(requestContext *gin.Context, endpoint string) {
				called = true
				requestContext.Status(http.StatusNoContent)
			}, func(*gin.Context) string { return "tts" }))
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, grokCompositeJSONRequest(http.MethodPost, "/v1/tts", fixture.body))
			require.Equal(t, fixture.wantStatus, recorder.Code)
			require.Equal(t, fixture.wantCalled, called)
		})
	}
}

// Standalone search is a Grok-only, model-less action: Composite groups pin to
// Grok so the owner group search handler runs; other platforms reject.
func TestGrokCompositeSearchHandlerPinsUnresolvedTarget(t *testing.T) {
	for _, fixture := range []struct {
		name          string
		groupPlatform string
		wantStatus    int
	}{
		{"composite pins grok", PlatformComposite, http.StatusNoContent},
		{"gemini group rejected", PlatformGemini, http.StatusBadRequest},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			router := grokCompositeTestRouter(fixture.groupPlatform)
			called := false
			router.POST("/v1/web_search", GatewaySearchHandler(func(requestContext *gin.Context) {
				called = true
				require.Equal(t, PlatformGrok, GatewayTargetPlatform(requestContext))
				requestContext.Status(http.StatusNoContent)
			}))
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, grokCompositeJSONRequest(http.MethodPost, "/v1/web_search", `{"query":"xAI voice"}`))
			require.Equal(t, fixture.wantStatus, recorder.Code)
			require.Equal(t, fixture.wantStatus == http.StatusNoContent, called)
		})
	}
}

// Realtime over Composite resolves the query model through the composite
// middleware, and still reaches the Grok realtime adapter when the model
// selector is absent (the handler applies its documented default).
func TestGrokCompositeRealtimeResolution(t *testing.T) {
	for _, fixture := range []struct {
		name   string
		target string
	}{
		{"query model resolves via middleware", "/v1/realtime?model=grok-voice-latest"},
		{"missing model pinned by dispatch", "/v1/realtime"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			router := grokCompositeTestRouter(PlatformComposite)
			router.Use(CompositeTargetPlatformMiddleware(nil))
			called := false
			dispatch := NewGatewayActionDispatch(GatewayActionHandlers{
				"grok.realtime": func(requestContext *gin.Context) {
					called = true
					require.Equal(t, PlatformGrok, GatewayTargetPlatform(requestContext))
					requestContext.Status(http.StatusNoContent)
				},
			})
			router.GET("/v1/realtime", dispatch.Handler("realtime"))
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, fixture.target, nil))
			require.Equal(t, http.StatusNoContent, recorder.Code)
			require.True(t, called)
		})
	}
}

// Media actions are shared with OpenAI, so Composite groups must resolve the
// body model explicitly; unresolved requests keep the fail-closed rejection.
func TestGrokCompositeMediaDispatchRequiresResolvedTarget(t *testing.T) {
	for _, fixture := range []struct {
		name       string
		body       string
		wantStatus int
		wantCalled bool
	}{
		{"grok image model resolves to grok adapter", `{"model":"grok-imagine-image","prompt":"a cloud"}`, http.StatusNoContent, true},
		{"grok video model resolves to grok adapter", `{"model":"grok-imagine-video","prompt":"a moving cloud"}`, http.StatusNoContent, true},
		{"unknown model fails closed", `{"model":"unknown-private-model","prompt":"a cloud"}`, http.StatusBadRequest, false},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			router := grokCompositeTestRouter(PlatformComposite)
			router.Use(CompositeTargetPlatformMiddleware(nil))
			called := false
			dispatch := NewGatewayActionDispatch(GatewayActionHandlers{
				"grok.images": func(requestContext *gin.Context) {
					called = true
					require.Equal(t, PlatformGrok, GatewayTargetPlatform(requestContext))
					requestContext.Status(http.StatusNoContent)
				},
				"grok.video.generate": func(requestContext *gin.Context) {
					called = true
					require.Equal(t, PlatformGrok, GatewayTargetPlatform(requestContext))
					requestContext.Status(http.StatusNoContent)
				},
			})
			router.POST("/v1/images/generations", dispatch.ImageHandler())
			router.POST("/v1/videos/generations", dispatch.Handler("video.generate"))
			path := "/v1/images/generations"
			if strings.Contains(fixture.body, "moving") {
				path = "/v1/videos/generations"
			}
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, grokCompositeJSONRequest(http.MethodPost, path, fixture.body))
			require.Equal(t, fixture.wantStatus, recorder.Code)
			require.Equal(t, fixture.wantCalled, called)
		})
	}
}

// xAI exposes a native /v1/responses/compact endpoint (docs.x.ai Context
// Compaction, retrieved 2026-09-21) and the gateway already bridges Grok
// compact turns; the action matrix must admit Grok groups instead of
// short-circuiting them, while platforms without the adapter still reject.
func TestGrokResponsesCompactAdapterEnabled(t *testing.T) {
	descriptor := domain.PlatformAction(domain.ActionIdentity{Platform: PlatformGrok}, "responses.compact")
	require.Equal(t, domain.ActionImplemented, descriptor.Status)
	require.Equal(t, "openai.responses", descriptor.Adapter)
	require.Equal(t, domain.ActionUnverified, domain.PlatformAction(domain.ActionIdentity{Platform: PlatformAnthropic}, "responses.compact").Status)

	router := grokCompositeTestRouter(PlatformGrok)
	router.Use(GatewayActionRequirement)
	called := false
	dispatch := NewGatewayActionDispatch(GatewayActionHandlers{
		"openai.responses": func(requestContext *gin.Context) {
			called = true
			requestContext.Status(http.StatusNoContent)
		},
	})
	router.POST("/v1/responses/compact", dispatch.Handler("responses"))
	recorder := httptest.NewRecorder()
	body := `{"model":"grok-4.5","input":[{"role":"user","content":"summarize the prior turns"}]}`
	router.ServeHTTP(recorder, grokCompositeJSONRequest(http.MethodPost, "/v1/responses/compact", body))
	require.Equal(t, http.StatusNoContent, recorder.Code)
	require.True(t, called, "grok /responses/compact must reach the responses adapter")

	anthropicRouter := grokCompositeTestRouter(PlatformAnthropic)
	anthropicRouter.Use(GatewayActionRequirement)
	anthropicRouter.POST("/v1/responses/compact", dispatch.Handler("responses"))
	recorder = httptest.NewRecorder()
	anthropicRouter.ServeHTTP(recorder, grokCompositeJSONRequest(http.MethodPost, "/v1/responses/compact", body))
	require.Equal(t, http.StatusBadRequest, recorder.Code)
}

// Model-less video lookups keep the existing force-map: Composite groups reach
// the owner-checked Grok video handlers, which still enforce the per-owner
// session binding before any upstream call.
func TestGrokCompositeVideoLookupForceMapsToGrok(t *testing.T) {
	router := grokCompositeTestRouter(PlatformComposite)
	called := []string{}
	dispatch := NewGatewayActionDispatch(GatewayActionHandlers{
		"grok.video.status": func(requestContext *gin.Context) {
			called = append(called, "status")
			requestContext.Status(http.StatusNoContent)
		},
		"grok.video.content": func(requestContext *gin.Context) {
			called = append(called, "content")
			requestContext.Status(http.StatusNoContent)
		},
	})
	router.GET("/v1/videos/:request_id", dispatch.Handler("video.status"))
	router.GET("/v1/videos/:request_id/content", dispatch.Handler("video.content"))
	for _, target := range []string{"/v1/videos/req-1", "/v1/videos/req-1/content"} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
		require.Equal(t, http.StatusNoContent, recorder.Code, target)
	}
	require.Equal(t, []string{"status", "content"}, called)
}

// Composite explicit ordered target sets (configured routes) must drive Grok
// media actions the same way detector resolution does: a public alias routed
// to Grok reaches the Grok video adapter with the body model rewritten to the
// route's upstream model.
func TestGrokCompositeExplicitRouteDrivesVideoGeneration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	resolver := NewCompositeRouteResolver(compositeRouteRepoStub{routes: []CompositeModelRoute{{
		GroupID:        7,
		PublicModel:    "my-video-alias",
		MatchType:      CompositeRouteMatchExact,
		TargetPlatform: PlatformGrok,
		UpstreamModel:  "grok-imagine-video",
		Endpoint:       CompositeRouteEndpointAny,
		Enabled:        true,
	}}})
	router := gin.New()
	router.Use(func(requestContext *gin.Context) {
		requestContext.Set("api_key", &APIKey{Group: &Group{ID: 7, Platform: PlatformComposite}})
	}, CompositeTargetPlatformMiddleware(resolver))
	called := false
	dispatch := NewGatewayActionDispatch(GatewayActionHandlers{
		"grok.video.generate": func(requestContext *gin.Context) {
			called = true
			require.Equal(t, PlatformGrok, GatewayTargetPlatform(requestContext))
			upstreamModel, ok := ResolvedUpstreamModelFromContext(requestContext.Request.Context())
			require.True(t, ok)
			require.Equal(t, "grok-imagine-video", upstreamModel)
			body, err := io.ReadAll(requestContext.Request.Body)
			require.NoError(t, err)
			require.Equal(t, "grok-imagine-video", gjson.GetBytes(body, "model").String(), "public alias must be rewritten to the route upstream model")
			requestContext.Status(http.StatusNoContent)
		},
	})
	router.POST("/v1/videos/generations", dispatch.Handler("video.generate"))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, grokCompositeJSONRequest(http.MethodPost, "/v1/videos/generations", `{"model":"my-video-alias","prompt":"a moving cloud"}`))
	require.Equal(t, http.StatusNoContent, recorder.Code)
	require.True(t, called)
}
