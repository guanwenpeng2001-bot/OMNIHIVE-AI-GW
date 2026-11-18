package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/tlsfingerprint"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestAccountModelCatalogPlatformDefaultsAndConfiguredAliases(t *testing.T) {
	for _, descriptor := range domain.PlatformDescriptors() {
		t.Run(descriptor.ID, func(t *testing.T) {
			account := service.Account{ID: 1, Platform: descriptor.ID, Type: service.AccountTypeAPIKey}
			svc := &availableModelsAdminService{stubAdminService: newStubAdminService(), account: account}
			router := setupAvailableModelsRouter(svc)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/1/models", nil))
			require.Equal(t, http.StatusOK, rec.Code)
			var payload struct {
				Data []struct {
					ID     string `json:"id"`
					Source string `json:"catalog_source"`
				} `json:"data"`
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
			ids := make([]string, 0, len(payload.Data))
			for _, model := range payload.Data {
				ids = append(ids, model.ID)
				require.Equal(t, "preset", model.Source)
			}
			require.ElementsMatch(t, service.NewModelRegistry().Snapshot().CandidateIDs(descriptor.ID, ""), ids)
		})
	}
	account := service.Account{ID: 2, Platform: service.PlatformKimi, Type: service.AccountTypeAPIKey, Credentials: map[string]any{"model_mapping": map[string]any{"MyAlias": "kimi-k3", "myalias": "kimi-k2.5"}}}
	svc := &availableModelsAdminService{stubAdminService: newStubAdminService(), account: account}
	rec := httptest.NewRecorder()
	setupAvailableModelsRouter(svc).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/2/models", nil))
	require.Contains(t, rec.Body.String(), `"id":"MyAlias"`)
	require.Contains(t, rec.Body.String(), `"id":"myalias"`)
	require.Contains(t, rec.Body.String(), `"catalog_source":"configured"`)
}

type availableModelsAdminService struct {
	*stubAdminService
	account     service.Account
	mergeCalls  int
	commitCalls int
}

func (s *availableModelsAdminService) GetAccount(_ context.Context, id int64) (*service.Account, error) {
	if s.account.ID == id {
		acc := s.account
		return &acc, nil
	}
	return s.stubAdminService.GetAccount(context.Background(), id)
}

func setupAvailableModelsRouter(adminSvc service.AdminService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewAccountHandler(adminSvc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	router.GET("/api/v1/admin/accounts/:id/models", handler.GetAvailableModels)
	return router
}

type syncUpstreamHTTPUpstream struct {
	requests  []*http.Request
	resp      *http.Response
	responses []*http.Response
	err       error
	errAfter  int
}

func (u *syncUpstreamHTTPUpstream) Do(req *http.Request, proxyURL string, accountID int64, accountConcurrency int) (*http.Response, error) {
	u.requests = append(u.requests, req)
	if u.err != nil && len(u.requests) > u.errAfter {
		return nil, u.err
	}
	if len(u.responses) > 0 {
		resp := u.responses[0]
		u.responses = u.responses[1:]
		return resp, nil
	}
	return u.resp, nil
}

func (u *syncUpstreamHTTPUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, accountConcurrency)
}

type discoveryMappingRepo struct {
	service.AccountRepository
	admin *availableModelsAdminService
}

func (r *discoveryMappingRepo) CommitAccountModelCatalog(_ context.Context, _ int64, _ service.AccountModelCatalogBaseline, snapshot *service.AccountModelCatalogSnapshot, state service.AccountModelCatalogState, _ *service.UpstreamModelMetadataSnapshot, _ bool) error {
	r.admin.commitCalls++
	if r.admin.updateAccountErr != nil {
		return r.admin.updateAccountErr
	}
	if r.admin.account.Extra == nil {
		r.admin.account.Extra = map[string]any{}
	}
	r.admin.account.Extra[service.UpstreamModelCatalogExtraKey] = snapshot
	r.admin.account.Extra[service.UpstreamModelCatalogStateExtraKey] = state
	return nil
}

func (r *discoveryMappingRepo) UpdateExtra(context.Context, int64, map[string]any) error { return nil }

func setupSyncUpstreamModelsRouter(adminSvc service.AdminService, upstream service.HTTPUpstream) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	var repo service.AccountRepository
	if admin, ok := adminSvc.(*availableModelsAdminService); ok {
		repo = &discoveryMappingRepo{admin: admin}
	}
	accountTestSvc := service.NewAccountTestService(
		repo,
		nil,
		nil,
		nil,
		nil,
		upstream,
		&config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}},
		nil,
	)
	handler := NewAccountHandler(adminSvc, nil, nil, nil, nil, nil, nil, nil, accountTestSvc, nil, nil, nil, nil, nil)
	router.POST("/api/v1/admin/accounts/:id/models/sync-upstream", handler.SyncUpstreamModels)
	router.POST("/api/v1/admin/accounts/models/sync-upstream-preview", handler.SyncUpstreamModelsPreview)
	return router
}

func TestAccountHandlerGetAvailableModels_GrokUsesXAIModels(t *testing.T) {
	svc := &availableModelsAdminService{
		stubAdminService: newStubAdminService(),
		account: service.Account{
			ID:       44,
			Name:     "grok-oauth",
			Platform: service.PlatformGrok,
			Type:     service.AccountTypeOAuth,
			Status:   service.StatusActive,
			Credentials: map[string]any{
				"model_mapping": map[string]any{
					"grok-4.3": "grok-4.3",
				},
			},
		},
	}
	router := setupAvailableModelsRouter(svc)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/44/models", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Data, 1)
	require.Equal(t, "grok-4.3", resp.Data[0].ID)
}

func TestAccountHandlerGetAvailableModels_GrokDefaultsToXAIModelsWithoutMapping(t *testing.T) {
	svc := &availableModelsAdminService{
		stubAdminService: newStubAdminService(),
		account: service.Account{
			ID:       45,
			Name:     "grok-oauth-defaults",
			Platform: service.PlatformGrok,
			Type:     service.AccountTypeOAuth,
			Status:   service.StatusActive,
		},
	}
	router := setupAvailableModelsRouter(svc)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/45/models", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.Data)

	var ids []string
	for _, model := range resp.Data {
		id := model.ID
		ids = append(ids, id)
		require.NotContains(t, strings.ToLower(id), "claude")
	}
	require.Contains(t, ids, "grok-4.3")
	require.Contains(t, ids, "grok-build-0.1")
}

func TestAccountHandlerGetAvailableModels_OpenAIOAuthUsesExplicitModelMapping(t *testing.T) {
	svc := &availableModelsAdminService{
		stubAdminService: newStubAdminService(),
		account: service.Account{
			ID:       42,
			Name:     "openai-oauth",
			Platform: service.PlatformOpenAI,
			Type:     service.AccountTypeOAuth,
			Status:   service.StatusActive,
			Credentials: map[string]any{
				"model_mapping": map[string]any{
					"gpt-5": "gpt-5.1",
				},
			},
		},
	}
	router := setupAvailableModelsRouter(svc)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/42/models", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Data, 1)
	require.Equal(t, "gpt-5", resp.Data[0].ID)
}

func TestAccountHandlerGetAvailableModels_OpenAIOAuthPassthroughFallsBackToDefaults(t *testing.T) {
	svc := &availableModelsAdminService{
		stubAdminService: newStubAdminService(),
		account: service.Account{
			ID:       43,
			Name:     "openai-oauth-passthrough",
			Platform: service.PlatformOpenAI,
			Type:     service.AccountTypeOAuth,
			Status:   service.StatusActive,
			Credentials: map[string]any{
				"model_mapping": map[string]any{
					"gpt-5": "gpt-5.1",
				},
			},
			Extra: map[string]any{
				"openai_passthrough": true,
			},
		},
	}
	router := setupAvailableModelsRouter(svc)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/43/models", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.Data)
	require.NotEqual(t, "gpt-5", resp.Data[0].ID)
}

func TestAccountHandlerGetAvailableModels_OpenAIAPIKeyUsesApprovedPresetOrder(t *testing.T) {
	svc := &availableModelsAdminService{
		stubAdminService: newStubAdminService(),
		account: service.Account{
			ID:       46,
			Name:     "openai-apikey",
			Platform: service.PlatformOpenAI,
			Type:     service.AccountTypeAPIKey,
			Status:   service.StatusActive,
			Credentials: map[string]any{
				"api_key": "test-key",
			},
		},
	}
	router := setupAvailableModelsRouter(svc)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/46/models", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.Data)
	require.Equal(t, service.OpenAIPresetModels()[0].ID, resp.Data[0].ID)
}

func TestAccountHandlerGetAvailableModels_OpenAISparkShadowReturnsMappingModels(t *testing.T) {
	parentID := int64(100)
	svc := &availableModelsAdminService{
		stubAdminService: newStubAdminService(),
		account: service.Account{
			ID:              44,
			Name:            "openai-spark-shadow",
			Platform:        service.PlatformOpenAI,
			Type:            service.AccountTypeOAuth,
			Status:          service.StatusActive,
			ParentAccountID: &parentID,
			QuotaDimension:  service.QuotaDimensionSpark,
			Credentials: map[string]any{
				"model_mapping": map[string]any{
					"gpt-5.3-codex-spark": "gpt-5.3-codex-spark",
				},
			},
		},
	}
	router := setupAvailableModelsRouter(svc)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/44/models", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	ids := make([]string, 0, len(resp.Data))
	for _, m := range resp.Data {
		ids = append(ids, m.ID)
	}
	require.ElementsMatch(t, []string{
		"gpt-5.3-codex-spark",
	}, ids, "影子可用模型由 model_mapping 派生（非写死）")
}

func TestAccountHandlerGetAvailableModels_GeminiGoogleOneUsesConservativeCatalog(t *testing.T) {
	svc := &availableModelsAdminService{
		stubAdminService: newStubAdminService(),
		account: service.Account{
			ID:       45,
			Name:     "google-one",
			Platform: service.PlatformGemini,
			Type:     service.AccountTypeOAuth,
			Status:   service.StatusActive,
			Credentials: map[string]any{
				"oauth_type": "google_one",
			},
		},
	}
	router := setupAvailableModelsRouter(svc)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/45/models", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	ids := make([]string, 0, len(resp.Data))
	for _, model := range resp.Data {
		ids = append(ids, model.ID)
	}
	require.ElementsMatch(t, []string{"gemini-2.5-flash", "gemini-2.5-pro"}, ids)
	require.NotContains(t, ids, "gemini-3.5-flash")
	require.NotContains(t, ids, "gemini-2.5-flash-image")
}

func TestAccountHandlerSyncUpstreamModels_ConfigErrorReturnsBadRequest(t *testing.T) {
	svc := &availableModelsAdminService{
		stubAdminService: newStubAdminService(),
		account: service.Account{
			ID:       44,
			Name:     "openai-apikey-missing-key",
			Platform: service.PlatformOpenAI,
			Type:     service.AccountTypeAPIKey,
			Status:   service.StatusActive,
			Credentials: map[string]any{
				"base_url": "https://openai.example.com/v1",
			},
		},
	}
	router := setupSyncUpstreamModelsRouter(svc, &syncUpstreamHTTPUpstream{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/44/models/sync-upstream", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "No OpenAI API key is available")
}

func TestAccountHandlerSyncUpstreamModelsReturnsCapabilityMetadata(t *testing.T) {
	svc := &availableModelsAdminService{
		stubAdminService: newStubAdminService(),
		account: service.Account{
			ID: 48, Name: "custom-openai", Platform: service.PlatformOpenAI,
			Type: service.AccountTypeAPIKey, Status: service.StatusActive,
			Credentials: map[string]any{"api_key": "key", "base_url": "https://provider.example/v1"},
		},
	}
	upstream := &syncUpstreamHTTPUpstream{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(`{"models":[{
			"id":"custom-thinking-model",
			"reasoning":true,
			"default_reasoning_level":"high",
			"supported_reasoning_levels":["low","high"],
			"input_modalities":["text","image"],
			"context_window":256000
		}]}`)),
	}}
	router := setupSyncUpstreamModelsRouter(svc, upstream)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/48/models/sync-upstream", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Data service.UpstreamModelCatalog `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, []string{"custom-thinking-model"}, resp.Data.DiscoveredModels)
	metadata := resp.Data.Metadata["custom-thinking-model"]
	require.NotNil(t, metadata.Reasoning)
	require.True(t, *metadata.Reasoning)
	require.Equal(t, []string{"low", "high"}, metadata.SupportedReasoningLevels)
	require.Equal(t, []string{"text", "image"}, metadata.InputModalities)
}

// Scenario: 创建账号 preview 将具体 mapping 传给 404/405 配置回退。
func TestAccountHandlerSyncUpstreamModelsPreviewUsesProvidedModelMapping(t *testing.T) {
	upstream := &syncUpstreamHTTPUpstream{responses: []*http.Response{
		{
			StatusCode: http.StatusNotFound,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"error":"not found"}`)),
		},
		{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{
				"configured-provider": {
					"api": "https://provider.example/v1",
					"models": {
						"glm-5.3": {
							"id": "glm-5.3",
							"reasoning": true,
							"reasoning_options": [{"type":"effort","values":["low","high"]}],
							"modalities": {"input":["text"],"output":["text"]},
							"limit": {"context":1000000,"output":131072}
						}
					}
				}
			}`)),
		},
	}}
	router := setupSyncUpstreamModelsRouter(newStubAdminService(), upstream)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/admin/accounts/models/sync-upstream-preview",
		strings.NewReader(`{
			"platform":"openai",
			"type":"apikey",
			"base_url":"https://provider.example/v1",
			"api_key":"key",
			"model_mapping":{"public-glm":"glm-5.3"}
		}`),
	)
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "upstream_model_sync_endpoint_not_found")
	require.Len(t, upstream.requests, 1)
	require.NotContains(t, rec.Body.String(), "glm-5.3")
}

func TestAccountHandlerSyncUpstreamModels_UpstreamErrorDoesNotExposeBody(t *testing.T) {
	svc := &availableModelsAdminService{
		stubAdminService: newStubAdminService(),
		account: service.Account{
			ID:       45,
			Name:     "openai-apikey-upstream-error",
			Platform: service.PlatformOpenAI,
			Type:     service.AccountTypeAPIKey,
			Status:   service.StatusActive,
			Credentials: map[string]any{
				"api_key":  "openai-key",
				"base_url": "https://openai.example.com/v1",
			},
		},
	}
	upstream := &syncUpstreamHTTPUpstream{resp: &http.Response{
		StatusCode: http.StatusBadGateway,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":"SECRET_TOKEN should not be exposed"}`)),
	}}
	router := setupSyncUpstreamModelsRouter(svc, upstream)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/45/models/sync-upstream", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadGateway, rec.Code)
	require.Contains(t, rec.Body.String(), "Upstream model list request failed with HTTP 502")
	require.NotContains(t, rec.Body.String(), "SECRET_TOKEN")
}

// Scenario: 能力补全失败显示部分成功。
func TestAccountHandlerSyncUpstreamModels_MetadataEnrichmentFailureReturnsWarning(t *testing.T) {
	svc := &availableModelsAdminService{
		stubAdminService: newStubAdminService(),
		account: service.Account{
			ID:       46,
			Name:     "opencode-id-only-model-list",
			Platform: service.PlatformOpenAI,
			Type:     service.AccountTypeAPIKey,
			Status:   service.StatusActive,
			Credentials: map[string]any{
				"api_key":  "opencode-key",
				"base_url": "https://opencode.ai/zen/v1",
			},
		},
	}
	upstream := &syncUpstreamHTTPUpstream{responses: []*http.Response{
		{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"data":[{"id":"x-preview-f-free"}]}`)),
		},
		{
			StatusCode: http.StatusBadGateway,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"error":"registry unavailable"}`)),
		},
	}}
	router := setupSyncUpstreamModelsRouter(svc, upstream)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/46/models/sync-upstream", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Data struct {
			DiscoveredModels []string `json:"discovered_models"`
			Warnings         []struct {
				Code string `json:"code"`
			} `json:"warnings"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, []string{"x-preview-f-free"}, resp.Data.DiscoveredModels)
	// Discovery still succeeds, with both the incomplete metadata summary and
	// the registry failure diagnostic exposed to the caller.
	require.Len(t, resp.Data.Warnings, 2)
	require.Equal(t, "upstream_model_metadata_incomplete", resp.Data.Warnings[0].Code)
	require.Equal(t, "model_metadata_registry_unavailable", resp.Data.Warnings[1].Code)
}

func TestSyncPreviewQianwenProtocolAndPagination(t *testing.T) {
	upstream := &syncUpstreamHTTPUpstream{responses: []*http.Response{
		{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"data":[{"id":"first"}],"has_more":true,"last_id":"page-1"}`))},
		{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"data":[{"id":"second"}],"has_more":false}`))},
		{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{}`))},
	}}
	router := setupSyncUpstreamModelsRouter(newStubAdminService(), upstream)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/models/sync-upstream-preview", strings.NewReader(`{"platform":"qianwen","type":"apikey","credentials":{"api_key":"preview-secret","base_url":"https://relay.example/anthropic","api_protocol":"anthropic","account_mode":"payg","api_base_urls":{"chat_completions":"https://relay.example/v1"}}}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.JSONEq(t, `["first","second"]`, string(gjson.Get(w.Body.String(), "data.discovered_models").Raw))
	require.Equal(t, "https://relay.example/v1/models", upstream.requests[0].URL.String())
	require.Equal(t, "page-1", upstream.requests[1].URL.Query().Get("after"))
	require.Equal(t, "Bearer preview-secret", upstream.requests[1].Header.Get("Authorization"))
	require.NotContains(t, w.Body.String(), "preview-secret")
}

func TestSyncPreviewErrorReasons(t *testing.T) {
	for _, tc := range []struct {
		name, platform, key, reason string
		upstreamStatus, status      int
	}{
		{"missing credentials", "qianwen", "", "configuration", 200, 400},
		{"unsupported", "unknown", "key", "not_adapted", 200, 400},
		{"authentication", "qianwen", "key", "credentials", 401, 400},
		{"authorization", "qianwen", "key", "permission", 403, 502},
		{"path", "qianwen", "key", "endpoint_not_found", 404, 400},
		{"method", "qianwen", "key", "method_not_allowed", 405, 400},
		{"rate", "qianwen", "key", "rate_limited", 429, 502},
		{"unavailable", "qianwen", "key", "upstream", 503, 502},
		{"malformed", "qianwen", "key", "upstream", 200, 502},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := &syncUpstreamHTTPUpstream{resp: &http.Response{StatusCode: tc.upstreamStatus, Body: io.NopCloser(strings.NewReader(`private upstream body`))}}
			router := setupSyncUpstreamModelsRouter(newStubAdminService(), upstream)
			body, _ := json.Marshal(map[string]any{"platform": tc.platform, "type": "apikey", "api_key": tc.key})
			req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/models/sync-upstream-preview", strings.NewReader(string(body)))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Contains(t, w.Body.String(), `"reason":"upstream_model_sync_`+tc.reason+`"`)
			require.NotContains(t, w.Body.String(), "private upstream body")
		})
	}
}

func TestAccountModelCatalogQianwenNativeCatalogAppliesIndependentSnapshot(t *testing.T) {
	t.Setenv("MODEL_CAPABILITY_OFFLINE", "true")
	svc := &availableModelsAdminService{stubAdminService: newStubAdminService(), account: service.Account{
		ID: 19, Platform: service.PlatformQianwen, Type: service.AccountTypeAPIKey, Status: service.StatusActive,
		Credentials: map[string]any{"api_key": "secret", "model_mapping": map[string]any{"qwen-plus": "custom-target", "alias": "qwen-plus"}},
	}}
	upstream := &syncUpstreamHTTPUpstream{responses: []*http.Response{
		{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"success":true,"output":{"total":3,"page_no":1,"models":[{"model":"qwen-plus","modalities":{"output":["text"]}},{"model":"ZHIPU/GLM-5.3-Flash"},{"model":"wanx-3d"}]}}`))},
		{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{}`))},
	}}
	firstBody, err := io.ReadAll(upstream.responses[0].Body)
	require.NoError(t, err)
	upstream.responses[0].Body = io.NopCloser(strings.NewReader(string(firstBody)))
	upstream.responses = append(upstream.responses[:1], &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(firstBody)))})
	router := setupSyncUpstreamModelsRouter(svc, upstream)
	preview := httptest.NewRecorder()
	router.ServeHTTP(preview, httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/19/models/sync-upstream", nil))
	require.Equal(t, 200, preview.Code, preview.Body.String())
	require.Zero(t, svc.commitCalls)
	reviewBody := `{"review":` + gjson.Get(preview.Body.String(), "data.review").Raw + `}`
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/19/models/sync-upstream?apply=true", strings.NewReader(reviewBody)))
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Zero(t, svc.updateAccountCalls, "discovery must never send an entire credentials snapshot")
	require.Equal(t, 1, svc.commitCalls)
	require.Zero(t, svc.mergeCalls)
	require.Equal(t, map[string]any{"alias": "qwen-plus", "qwen-plus": "custom-target"}, svc.account.Credentials["model_mapping"])
	// Discovery retains native entries with unknown metadata and the explicitly
	// configured target; missing capability observations are not admission gates.
	eligible := svc.account.EffectiveUpstreamModelCatalog().EligibleModels
	ids := make([]string, 0, len(eligible))
	for _, model := range eligible {
		ids = append(ids, model.ID)
	}
	require.ElementsMatch(t, []string{"qwen-plus", "ZHIPU/GLM-5.3-Flash", "wanx-3d", "custom-target"}, ids)
	require.Len(t, svc.account.GetUpstreamModelCatalog().DiscoveredModels, 3)
	require.Equal(t, `"1"`, gjson.Get(w.Body.String(), "data.snapshot_revision").Raw)
	require.Equal(t, `"0"`, gjson.Get(w.Body.String(), "data.identity_revision").Raw)
	require.Equal(t, `"0"`, gjson.Get(w.Body.String(), "data.rules_revision").Raw)
	require.Equal(t, `[]`, gjson.Get(w.Body.String(), "data.invalid_aliases").Raw)
	require.JSONEq(t, `["ZHIPU/GLM-5.3-Flash","qwen-plus","wanx-3d"]`, gjson.Get(w.Body.String(), "data.models").Raw)
	require.Empty(t, gjson.Get(w.Body.String(), "data.excluded_models").Array(), "unknown metadata is not an exclusion reason")
}

func TestAccountModelCatalogQianwenCatalogDoesNotPersistIncompletePages(t *testing.T) {
	svc := &availableModelsAdminService{stubAdminService: newStubAdminService(), account: service.Account{ID: 19, Platform: service.PlatformQianwen, Type: service.AccountTypeAPIKey, Credentials: map[string]any{"api_key": "secret"}}}
	upstream := &syncUpstreamHTTPUpstream{responses: []*http.Response{
		{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"success":true,"output":{"total":2,"page_no":1,"models":[{"model":"first"}]}}`))},
		{StatusCode: 503, Body: io.NopCloser(strings.NewReader(`private upstream body`))},
	}}
	w := httptest.NewRecorder()
	setupSyncUpstreamModelsRouter(svc, upstream).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/19/models/sync-upstream", nil))
	require.Equal(t, http.StatusBadGateway, w.Code, w.Body.String())
	require.Equal(t, "upstream_model_sync_upstream", gjson.Get(w.Body.String(), "reason").String())
	require.NotContains(t, w.Body.String(), "first")
	require.NotContains(t, w.Body.String(), "discovered_models")
	require.NotContains(t, w.Body.String(), "candidate_models")
	require.False(t, gjson.Get(w.Body.String(), "data.review").Exists())
	require.Zero(t, svc.commitCalls)
	require.Zero(t, svc.updateAccountCalls)
	require.NotContains(t, w.Body.String(), "private upstream body")
}

func TestAccountModelCatalogQianwenCatalogPersistenceFailure(t *testing.T) {
	t.Setenv("MODEL_CAPABILITY_OFFLINE", "true")
	svc := &availableModelsAdminService{stubAdminService: newStubAdminService(), account: service.Account{ID: 19, Platform: service.PlatformQianwen, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Credentials: map[string]any{"api_key": "secret"}}}
	svc.updateAccountErr = io.ErrClosedPipe
	upstream := &syncUpstreamHTTPUpstream{responses: []*http.Response{
		{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"success":true,"output":{"total":1,"page_no":1,"models":[{"model":"qwen-plus"}]}}`))},
		{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{}`))},
	}}
	firstBody, err := io.ReadAll(upstream.responses[0].Body)
	require.NoError(t, err)
	upstream.responses[0].Body = io.NopCloser(strings.NewReader(string(firstBody)))
	upstream.responses = append(upstream.responses[:1], &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(firstBody)))})
	router := setupSyncUpstreamModelsRouter(svc, upstream)
	preview := httptest.NewRecorder()
	router.ServeHTTP(preview, httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/19/models/sync-upstream", nil))
	require.Equal(t, 200, preview.Code, preview.Body.String())
	require.Zero(t, svc.commitCalls)
	reviewBody := `{"review":` + gjson.Get(preview.Body.String(), "data.review").Raw + `}`
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/19/models/sync-upstream?apply=true", strings.NewReader(reviewBody)))
	require.Equal(t, 500, w.Code, w.Body.String())
	require.Equal(t, 1, svc.commitCalls)
	require.Zero(t, svc.updateAccountCalls)
}

func TestAccountModelCatalogQianwenPreviewDoesNotApplyMapping(t *testing.T) {
	svc := &availableModelsAdminService{stubAdminService: newStubAdminService(), account: service.Account{ID: 19, Platform: service.PlatformQianwen, Type: service.AccountTypeAPIKey, Credentials: map[string]any{"api_key": "synthetic"}}}
	upstream := &syncUpstreamHTTPUpstream{responses: []*http.Response{
		{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"success":true,"output":{"total":1,"page_no":1,"models":[{"model":"qwen-plus"}]}}`))},
		{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{}`))},
	}}
	w := httptest.NewRecorder()
	setupSyncUpstreamModelsRouter(svc, upstream).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/19/models/sync-upstream?apply=false", nil))
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Zero(t, svc.mergeCalls)
	require.Zero(t, svc.updateAccountCalls)
	require.Contains(t, w.Body.String(), "qwen-plus")
}

func TestModelCatalogDefaultPreviewAndApplyRequiresReview(t *testing.T) {
	for _, query := range []string{"", "?apply=false", "?apply=true"} {
		t.Run(query, func(t *testing.T) {
			adminService := &availableModelsAdminService{stubAdminService: newStubAdminService(), account: service.Account{ID: 19, Platform: service.PlatformQianwen, Type: service.AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture"}}}
			adminService.updateAccountErr = service.ErrAccountModelCatalogConflict
			upstream := &syncUpstreamHTTPUpstream{resp: &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"data":[]}`))}}
			recorder := httptest.NewRecorder()
			setupSyncUpstreamModelsRouter(adminService, upstream).ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/19/models/sync-upstream"+query, nil))
			if query == "?apply=true" {
				require.Equal(t, http.StatusBadRequest, recorder.Code)
				require.Contains(t, recorder.Body.String(), "review")
				require.Zero(t, adminService.commitCalls)
				require.Empty(t, upstream.requests)
			} else {
				require.Equal(t, http.StatusOK, recorder.Code)
				require.Zero(t, adminService.commitCalls)
				require.False(t, gjson.Get(recorder.Body.String(), "data.applied").Bool())
				require.Equal(t, "[]", gjson.Get(recorder.Body.String(), "data.models").Raw)
				require.Equal(t, `"0"`, gjson.Get(recorder.Body.String(), "data.snapshot_revision").Raw)
				require.Equal(t, `"0"`, gjson.Get(recorder.Body.String(), "data.identity_revision").Raw)
				require.Equal(t, `"0"`, gjson.Get(recorder.Body.String(), "data.rules_revision").Raw)
				require.Equal(t, "[]", gjson.Get(recorder.Body.String(), "data.invalid_aliases").Raw)
				require.Equal(t, "[]", gjson.Get(recorder.Body.String(), "data.excluded_models").Raw)
			}
		})
	}
}

func TestWriteUpstreamModelSyncErrorContract(test *testing.T) {
	for _, scenario := range []struct {
		kind   service.UpstreamModelSyncErrorKind
		status int
	}{
		{service.UpstreamModelSyncErrorConfiguration, 400}, {service.UpstreamModelSyncErrorCredentials, 400},
		{service.UpstreamModelSyncErrorAccountType, 400}, {service.UpstreamModelSyncErrorNotAdapted, 400},
		{service.UpstreamModelSyncErrorDiscoveryUnconfirmed, 400}, {service.UpstreamModelSyncErrorEndpointNotFound, 400},
		{service.UpstreamModelSyncErrorMethodNotAllowed, 400}, {service.UpstreamModelSyncErrorPermission, 502},
		{service.UpstreamModelSyncErrorRateLimited, 502}, {service.UpstreamModelSyncErrorNetwork, 502},
		{service.UpstreamModelSyncErrorUpstream, 502}, {service.UpstreamModelSyncErrorInternal, 500},
		{service.UpstreamModelSyncErrorUnsupported, 400},
	} {
		test.Run(string(scenario.kind), func(test *testing.T) {
			recorder := httptest.NewRecorder()
			ginContext, _ := gin.CreateTestContext(recorder)
			writeUpstreamModelSyncError(ginContext, &service.UpstreamModelSyncError{Kind: scenario.kind, Message: "Safe discovery failure", Err: fmt.Errorf("private-secret"), PartialCatalog: &service.UpstreamModelCatalog{CandidateModels: []string{"private-candidate"}, DiscoveredModels: []string{"private-discovery"}}})
			require.Equal(test, scenario.status, recorder.Code)
			require.Equal(test, "upstream_model_sync_"+string(scenario.kind), gjson.Get(recorder.Body.String(), "reason").String())
			for _, secret := range []string{"private-secret", "private-candidate", "private-discovery", "candidate_models", "discovered_models"} {
				require.NotContains(test, recorder.Body.String(), secret)
			}
		})
	}
}

func TestSyncPreviewNetworkError(test *testing.T) {
	router := setupSyncUpstreamModelsRouter(newStubAdminService(), &syncUpstreamHTTPUpstream{err: context.DeadlineExceeded})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/models/sync-upstream-preview", strings.NewReader(`{"platform":"openai","type":"apikey","api_key":"private-secret"}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	require.Equal(test, 502, recorder.Code)
	require.Equal(test, "upstream_model_sync_network", gjson.Get(recorder.Body.String(), "reason").String())
	require.NotContains(test, recorder.Body.String(), "private-secret")
}

func TestSyncPreviewPartialFailureReachesAdmin(test *testing.T) {
	test.Setenv("MODEL_CAPABILITY_OFFLINE", "true")
	for _, discovery := range []struct{ name, platform, firstPage, cursor string }{
		{"qianwen native", service.PlatformQianwen, `{"success":true,"output":{"total":2,"page_no":1,"models":[{"model":"private-first-page"}]}}`, "page_no"},
		{"generic", service.PlatformOpenAI, `{"data":[{"id":"private-first-page"}],"has_more":true,"last_id":"private-first-page"}`, "after"},
	} {
		for _, failure := range []struct {
			name, reason, body string
			status             int
			network            bool
		}{
			{"rate limited", "rate_limited", `{"error":"private-upstream-body"}`, 429, false},
			{"permission", "permission", `{"error":"private-upstream-body"}`, 403, false},
			{"network", "network", "", 0, true},
			{"malformed page", "upstream", `private-upstream-body`, 200, false},
		} {
			for _, saved := range []bool{false, true} {
				test.Run(fmt.Sprintf("%s/%s/saved=%t", discovery.name, failure.name, saved), func(test *testing.T) {
					mapping := map[string]any{"kept": "kept", "alias": "private-selected-target"}
					old := service.AccountModelCatalogSnapshot{Complete: true, Revision: 7, DiscoveredModels: []string{"kept"}, EligibleModels: []service.AccountCatalogModel{{ID: "kept"}}}
					adminService := &availableModelsAdminService{stubAdminService: newStubAdminService(), account: service.Account{
						ID: 19, Platform: discovery.platform, Type: service.AccountTypeAPIKey, Status: service.StatusActive,
						Credentials: map[string]any{"api_key": "private-secret", "model_mapping": mapping},
						Extra:       map[string]any{service.UpstreamModelCatalogExtraKey: old},
					}}
					before, err := json.Marshal(adminService.account)
					require.NoError(test, err)
					upstream := &syncUpstreamHTTPUpstream{responses: []*http.Response{
						{StatusCode: 200, Body: io.NopCloser(strings.NewReader(discovery.firstPage))},
						{StatusCode: failure.status, Body: io.NopCloser(strings.NewReader(failure.body))},
					}}
					if failure.network {
						upstream.err, upstream.errAfter = fmt.Errorf("private-secret: %w", context.DeadlineExceeded), 1
					}
					path := "/api/v1/admin/accounts/19/models/sync-upstream"
					body := []byte{}
					if !saved {
						path = "/api/v1/admin/accounts/models/sync-upstream-preview"
						body, err = json.Marshal(map[string]any{"platform": discovery.platform, "type": "apikey", "credentials": adminService.account.Credentials})
						require.NoError(test, err)
					}
					request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(body)))
					request.Header.Set("Content-Type", "application/json")
					recorder := httptest.NewRecorder()
					setupSyncUpstreamModelsRouter(adminService, upstream).ServeHTTP(recorder, request)
					require.Equal(test, http.StatusBadGateway, recorder.Code, recorder.Body.String())
					require.Equal(test, "upstream_model_sync_"+failure.reason, gjson.Get(recorder.Body.String(), "reason").String())
					require.Len(test, upstream.requests, 2)
					require.NotEmpty(test, upstream.requests[1].URL.Query().Get(discovery.cursor))
					for _, private := range []string{"private-first-page", "private-selected-target", "private-upstream-body", "private-secret", "candidate_models", "discovered_models", "eligible_models"} {
						require.NotContains(test, recorder.Body.String(), private)
					}
					require.False(test, gjson.Get(recorder.Body.String(), "data.review").Exists())
					after, err := json.Marshal(adminService.account)
					require.NoError(test, err)
					require.JSONEq(test, string(before), string(after), "preview must preserve prior snapshot, selection and state")
					require.Zero(test, adminService.commitCalls)
					require.Zero(test, adminService.mergeCalls)
					require.Zero(test, adminService.updateAccountCalls)
				})
			}
		}
	}
}

func TestSyncPreviewCandidatesReturnUnconfirmed(test *testing.T) {
	for _, platform := range []string{service.PlatformVolcengine, service.PlatformHunyuan} {
		for _, saved := range []bool{false, true} {
			test.Run(fmt.Sprintf("%s/saved=%t", platform, saved), func(test *testing.T) {
				credentials := map[string]any{"api_key": "private-secret", "model_mapping": map[string]any{"kept": "kept"}}
				if platform == service.PlatformHunyuan {
					credentials["base_url"] = "https://api.hunyuan.cloud.tencent.com/v1"
				}
				adminService := &availableModelsAdminService{stubAdminService: newStubAdminService(), account: service.Account{ID: 19, Platform: platform, Type: service.AccountTypeAPIKey, Credentials: credentials,
					Extra: map[string]any{service.UpstreamModelCatalogExtraKey: service.AccountModelCatalogSnapshot{Complete: true, Revision: 7, DiscoveredModels: []string{"kept"}}},
				}}
				before, err := json.Marshal(adminService.account)
				require.NoError(test, err)
				path := "/api/v1/admin/accounts/19/models/sync-upstream"
				body := []byte{}
				if !saved {
					path = "/api/v1/admin/accounts/models/sync-upstream-preview"
					body, err = json.Marshal(map[string]any{"platform": platform, "type": "apikey", "credentials": credentials})
					require.NoError(test, err)
				}
				upstream := &syncUpstreamHTTPUpstream{}
				recorder := httptest.NewRecorder()
				request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(body)))
				request.Header.Set("Content-Type", "application/json")
				setupSyncUpstreamModelsRouter(adminService, upstream).ServeHTTP(recorder, request)
				require.Equal(test, http.StatusBadRequest, recorder.Code, recorder.Body.String())
				require.Equal(test, "upstream_model_sync_discovery_unconfirmed", gjson.Get(recorder.Body.String(), "reason").String())
				require.Empty(test, upstream.requests)
				for _, private := range []string{"private-secret", "kept", "candidate_models", "discovered_models", "eligible_models"} {
					require.NotContains(test, recorder.Body.String(), private)
				}
				require.False(test, gjson.Get(recorder.Body.String(), "data").Exists())
				after, err := json.Marshal(adminService.account)
				require.NoError(test, err)
				require.JSONEq(test, string(before), string(after))
				require.Zero(test, adminService.commitCalls)
				require.Zero(test, adminService.updateAccountCalls)
			})
		}
	}
}
