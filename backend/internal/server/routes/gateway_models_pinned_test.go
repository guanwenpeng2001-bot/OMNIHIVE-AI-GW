package routes

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/handler"
	servermiddleware "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

type pinnedModelsRoutesRepository struct {
	service.AccountRepository
	account service.Account
}

func (r *pinnedModelsRoutesRepository) ListByGroup(context.Context, int64) ([]service.Account, error) {
	return []service.Account{r.account}, nil
}

type pinnedModelsRoutesUpstream struct {
	service.HTTPUpstream
	ordinaryCalls atomic.Int32
	codexCalls    atomic.Int32
}

func (u *pinnedModelsRoutesUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	body := `{"data":[{"id":"ordinary-upstream-model"}]}`
	if req.URL.Query().Has("client_version") {
		u.codexCalls.Add(1)
		body = `{"models":[{"slug":"gpt-5.5"}]}`
	} else {
		u.ordinaryCalls.Add(1)
	}
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
}

func TestGatewayRoutesPinnedModelsDispatchesOrdinaryAndCodexRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &pinnedModelsRoutesRepository{account: service.Account{
		ID: 7, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true,
		Credentials: map[string]any{"api_key": "test-models-key", "base_url": "https://models.example/v1"},
	}}
	upstream := &pinnedModelsRoutesUpstream{}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	s := service.NewOpenAIGatewayService(repo, nil, nil, nil, nil, nil, nil, cfg,
		nil, nil, nil, nil, nil, upstream, nil, nil, nil, nil, nil, nil, nil, nil)
	h := &handler.Handlers{
		Gateway:       handler.NewGatewayHandler(nil, s, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, cfg, nil),
		OpenAIGateway: handler.NewOpenAIGatewayHandler(s, nil, nil, nil, nil, nil, nil, nil, cfg),
		AsyncImage:    handler.NewAsyncImageHandler(nil, nil),
	}
	group := &service.Group{ID: 1, Platform: service.PlatformOpenAI,
		CodexModelsManifestConfig: service.GroupCodexModelsManifestConfig{Enabled: true, AccountIDs: []int64{7}}}
	router := gin.New()
	RegisterGatewayRoutes(router, h, servermiddleware.APIKeyAuthMiddleware(func(c *gin.Context) {
		c.Set(string(servermiddleware.ContextKeyAPIKey), &service.APIKey{GroupID: &group.ID, Group: group})
		c.Next()
	}), nil, nil, nil, nil, nil, cfg)
	for _, path := range []string{"/v1/models", "/models", "/v1/models?client_version=", "/models?client_version="} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusOK, w.Code, "%s: %s", path, w.Body.String())
		var response struct {
			Object string `json:"object"`
			Data   []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		require.Equal(t, "list", response.Object)
		require.Empty(t, response.Data, "pinned Codex discovery cannot manufacture ordinary visibility")
	}
	for _, path := range []string{"/v1/models?client_version=" + service.CodexCanonicalClientVersion(), "/models?client_version=" + service.CodexCanonicalClientVersion(), "/backend-api/codex/models"} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusOK, w.Code, "%s: %s", path, w.Body.String())
		require.Contains(t, w.Body.String(), `"slug":"gpt-5.5"`)
		require.NotContains(t, w.Body.String(), `"data"`)
	}
	require.Zero(t, upstream.ordinaryCalls.Load())
	require.EqualValues(t, 1, upstream.codexCalls.Load())
}

func TestGatewayRoutesRetrievePinnedModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &pinnedModelsRoutesRepository{account: service.Account{
		ID: 7, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true,
		Credentials: map[string]any{"api_key": "test-models-key", "base_url": "https://models.example/v1"},
	}}
	upstream := &pinnedModelsRoutesUpstream{}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	s := service.NewOpenAIGatewayService(repo, nil, nil, nil, nil, nil, nil, cfg,
		nil, nil, nil, nil, nil, upstream, nil, nil, nil, nil, nil, nil, nil, nil)
	h := &handler.Handlers{
		Gateway:       handler.NewGatewayHandler(nil, s, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, cfg, nil),
		OpenAIGateway: handler.NewOpenAIGatewayHandler(s, nil, nil, nil, nil, nil, nil, nil, cfg),
		AsyncImage:    handler.NewAsyncImageHandler(nil, nil),
	}
	group := &service.Group{ID: 1, Platform: service.PlatformOpenAI,
		CodexModelsManifestConfig: service.GroupCodexModelsManifestConfig{Enabled: true, AccountIDs: []int64{7}}}
	router := gin.New()
	RegisterGatewayRoutes(router, h, servermiddleware.APIKeyAuthMiddleware(func(c *gin.Context) {
		if c.GetHeader("Authorization") != "Bearer client-key" {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		c.Set(string(servermiddleware.ContextKeyAPIKey), &service.APIKey{GroupID: &group.ID, Group: group})
		c.Next()
	}), nil, nil, nil, nil, nil, cfg)
	request := func(path, key, etag string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", key)
		req.Header.Set("If-None-Match", etag)
		router.ServeHTTP(w, req)
		return w
	}
	list := request("/v1/models", "Bearer client-key", "")
	require.Equal(t, http.StatusOK, list.Code)
	var catalog struct {
		Data []json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(list.Body.Bytes(), &catalog))
	require.Empty(t, catalog.Data)
	for _, base := range []string{"/v1/models/", "/models/"} {
		path := base + "ordinary-upstream-model?client_version=ignored"
		require.Equal(t, http.StatusUnauthorized, request(path, "", "").Code)
		got := request(path, "Bearer client-key", list.Header().Get("ETag"))
		require.Equal(t, http.StatusNotFound, got.Code, got.Body.String())
		require.Empty(t, got.Header().Get("ETag"), "a collection validator cannot validate one model")
		missing := request(base+"unknown-model", "Bearer client-key", "")
		require.Equal(t, http.StatusNotFound, missing.Code)
		require.Contains(t, missing.Body.String(), `"error"`)
	}
	require.Zero(t, upstream.codexCalls.Load(), "retrieve never dispatches a Codex manifest")
}

func TestPublicCatalogStaticRoutesPrecedeModelLookup(t *testing.T) {
	for _, platform := range []string{service.PlatformOpenAI, service.PlatformGemini, service.PlatformMistral} {
		router := newGatewayRoutesTestRouterWithGroup(&service.Group{ID: 1, Platform: platform})
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest("GET", "/v1/models/catalog", nil))
		require.Equal(t, 200, recorder.Code, platform+": "+recorder.Body.String())
		require.Contains(t, recorder.Body.String(), `"fully_enumerable":true`)
		require.Equal(t, "private, no-store", recorder.Header().Get("Cache-Control"))
	}
}

type qualifiedModelsRoutesRepository struct {
	service.AccountRepository
	account service.Account
}

func (repo *qualifiedModelsRoutesRepository) GetModelCatalogAccount(context.Context, int64) (*service.Account, error) {
	return &repo.account, nil
}
func (repo *qualifiedModelsRoutesRepository) ListModelCatalogAccounts(context.Context, *int64, []string, bool) ([]service.Account, error) {
	return []service.Account{repo.account}, nil
}

func TestPublicModelRetrieveAcceptsEscapedQualifiedID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &qualifiedModelsRoutesRepository{account: service.Account{ID: 7, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Credentials: map[string]any{"api_key": "fixture", "base_url": "https://api.openai.com", "model_mapping": map[string]any{"provider/public-model": "gpt-native"}}, Extra: map[string]any{service.UpstreamModelCatalogExtraKey: service.AccountModelCatalogSnapshot{Complete: true, Revision: 1, DiscoveredModels: []string{"gpt-native"}, EligibleModels: []service.AccountCatalogModel{{ID: "gpt-native", Endpoints: []string{"responses"}, EvidenceKind: "synthetic-contract"}}}}}}
	gateway := service.NewGatewayService(repo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	cfg := &config.Config{RunMode: config.RunModeSimple}
	handlers := &handler.Handlers{Gateway: handler.NewGatewayHandler(gateway, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, cfg, nil), OpenAIGateway: &handler.OpenAIGatewayHandler{}, AsyncImage: handler.NewAsyncImageHandler(nil, nil)}
	group := &service.Group{ID: 1, Platform: service.PlatformOpenAI}
	router := gin.New()
	RegisterGatewayRoutes(router, handlers, servermiddleware.APIKeyAuthMiddleware(func(ctx *gin.Context) {
		ctx.Set(string(servermiddleware.ContextKeyAPIKey), &service.APIKey{ID: 1, GroupID: &group.ID, Group: group})
		ctx.Next()
	}), nil, nil, nil, nil, nil, cfg)
	for _, path := range []string{"/v1/models/provider%2Fpublic-model", "/v1/models/catalog/provider%2Fpublic-model"} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest("GET", path, nil))
		require.Equal(t, 200, recorder.Code, recorder.Body.String())
		require.Contains(t, recorder.Body.String(), "provider/public-model")
		require.NotContains(t, recorder.Body.String(), "gpt-native")
	}
	for _, path := range []string{"/v1/models/provider/public-model", "/v1/models/catalog/provider/public-model"} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest("GET", path, nil))
		require.Equal(t, 404, recorder.Code)
		require.Contains(t, recorder.Body.String(), `"code":"not_found_error"`)
	}

}

func TestGatewayRawPathPreservesCanonicalRouteFamilies(t *testing.T) {
	router := newGatewayRoutesTestRouterWithGroup(&service.Group{ID: 1, Platform: service.PlatformOpenAI})
	for _, scenario := range []struct{ method, path, body string }{
		{"GET", "/v1/models", ""},
		{"POST", "/v1/chat/completions", `{"model":"fixture","messages":[]}`},
		{"POST", "/v1/messages", `{"model":"fixture","max_tokens":8,"messages":[]}`},
		{"POST", "/v1/images/generations", `{"model":"gpt-image-1","prompt":"fixture"}`},
	} {
		request := func(rawPath bool) *httptest.ResponseRecorder {
			router.UseRawPath = rawPath
			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(scenario.method, scenario.path, strings.NewReader(scenario.body))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(recorder, req)
			return recorder
		}
		before, after := request(false), request(true)
		require.Equal(t, before.Code, after.Code, scenario.path)
		require.Equal(t, before.Body.String(), after.Body.String(), scenario.path)
		require.NotEqual(t, 404, after.Code, scenario.path)
	}
}
func TestGatewayRawPathDocumentsEngineWideEncodedMatching(t *testing.T) {
	router := newGatewayRoutesTestRouterWithGroup(&service.Group{ID: 1, Platform: service.PlatformOpenAI})
	router.GET("/api/v1/admin/probe/:id", func(ctx *gin.Context) { ctx.String(200, ctx.Param("id")) })
	request := func(path string, rawPath bool) *httptest.ResponseRecorder {
		router.UseRawPath = rawPath
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest("GET", path, nil))
		return recorder
	}
	require.Equal(t, 200, request("/api/v1/admin/probe/123", false).Code)
	require.Equal(t, 200, request("/api/v1/admin/probe/123", true).Code)
	require.Equal(t, 404, request("/api/v1/admin/probe/a%2Fb", false).Code)
	encoded := request("/api/v1/admin/probe/a%2Fb", true)
	require.Equal(t, 200, encoded.Code)
	require.Equal(t, "a/b", encoded.Body.String())
	require.Equal(t, 404, request("/api/v1/admin/probe/a/b", true).Code)
	require.Equal(t, 200, request("/api/v1/admin/pr%6Fbe/123", false).Code)
	require.Equal(t, 404, request("/api/v1/admin/pr%6Fbe/123", true).Code, "static route segments must use canonical spelling")
}
