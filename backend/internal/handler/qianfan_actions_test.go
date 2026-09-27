//go:build unit

package handler

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

func TestShouldRecordQianfanActionUsage(t *testing.T) {
	tests := []struct {
		name     string
		endpoint service.QianfanActionEndpoint
		result   *service.OpenAIForwardResult
		want     bool
	}{
		{name: "image with output", endpoint: service.QianfanEndpointImagesGenerations, result: &service.OpenAIForwardResult{ImageCount: 1}, want: true},
		{name: "image without output", endpoint: service.QianfanEndpointImagesEdits, result: &service.OpenAIForwardResult{}, want: false},
		{name: "embeddings with tokens", endpoint: service.QianfanEndpointEmbeddings, result: &service.OpenAIForwardResult{Usage: service.OpenAIUsage{InputTokens: 3}}, want: true},
		{name: "rerank zero tokens", endpoint: service.QianfanEndpointRerank, result: &service.OpenAIForwardResult{}, want: false},
		{name: "tts with audio units", endpoint: service.QianfanEndpointAudioSpeech, result: &service.OpenAIForwardResult{AudioUsage: &service.AudioUsage{Mode: "tts"}}, want: true},
		{name: "search with call", endpoint: service.QianfanEndpointSearchWebSearch, result: &service.OpenAIForwardResult{WebSearchCalls: 1}, want: true},
		{name: "video create defers billing", endpoint: service.QianfanEndpointVideoGenerate, result: &service.OpenAIForwardResult{ResponseID: "task-1"}, want: false},
		{name: "video status claims separately", endpoint: service.QianfanEndpointVideoStatus, result: &service.OpenAIForwardResult{VideoCount: 1}, want: false},
		{name: "video cancel has no billable units", endpoint: service.QianfanEndpointVideoCancel, result: &service.OpenAIForwardResult{}, want: false},
		{name: "voice clone bills once per enrollment", endpoint: service.QianfanEndpointVoiceCloneCreate, result: &service.OpenAIForwardResult{ResponseID: "voice-1"}, want: true},
		{name: "voice list has no billable units", endpoint: service.QianfanEndpointVoiceCloneList, result: &service.OpenAIForwardResult{}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.False(t, shouldRecordQianfanActionUsage(tt.endpoint, "model", nil), "nil result must never bill")
			require.Equal(t, tt.want, shouldRecordQianfanActionUsage(tt.endpoint, "model", tt.result))
		})
	}
}

func TestQianfanActionRequiredCapability(t *testing.T) {
	require.Equal(t, service.OpenAIEndpointCapabilityEmbeddings, qianfanActionRequiredCapability(service.QianfanEndpointEmbeddings))
	for _, endpoint := range []service.QianfanActionEndpoint{
		service.QianfanEndpointImagesGenerations,
		service.QianfanEndpointImagesEdits,
		service.QianfanEndpointVideoGenerate,
		service.QianfanEndpointVideoStatus,
		service.QianfanEndpointRerank,
		service.QianfanEndpointSearchWebSearch,
		service.QianfanEndpointAudioSpeech,
		service.QianfanEndpointVoiceCloneCreate,
	} {
		require.Empty(t, qianfanActionRequiredCapability(endpoint), "endpoint %s stays model-evidence gated", endpoint)
	}
}

type qianfanHandlerCacheStub struct {
	mu              sync.Mutex
	sessionBindings map[string]int64
	pendingBilling  map[string][]byte
	claims          map[string]bool
}

func newQianfanHandlerCacheStub() *qianfanHandlerCacheStub {
	return &qianfanHandlerCacheStub{sessionBindings: map[string]int64{}, pendingBilling: map[string][]byte{}, claims: map[string]bool{}}
}

func (s *qianfanHandlerCacheStub) GetSessionAccountID(_ context.Context, _ int64, sessionHash string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id, ok := s.sessionBindings[sessionHash]; ok {
		return id, nil
	}
	return 0, service.ErrStickySessionNotFound
}
func (s *qianfanHandlerCacheStub) SetSessionAccountID(_ context.Context, _ int64, sessionHash string, accountID int64, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessionBindings[sessionHash] = accountID
	return nil
}
func (s *qianfanHandlerCacheStub) RefreshSessionTTL(_ context.Context, _ int64, _ string, _ time.Duration) error {
	return nil
}
func (s *qianfanHandlerCacheStub) DeleteSessionAccountID(_ context.Context, _ int64, _ string) error {
	return nil
}
func (s *qianfanHandlerCacheStub) SetGrokVideoPendingBilling(_ context.Context, key string, payload []byte, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pendingBilling[key] = payload
	return nil
}
func (s *qianfanHandlerCacheStub) GetGrokVideoPendingBilling(_ context.Context, key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pendingBilling[key], nil
}
func (s *qianfanHandlerCacheStub) ClaimGrokVideoBilled(_ context.Context, key string, _ time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.claims[key] {
		return false, nil
	}
	s.claims[key] = true
	return true, nil
}
func (s *qianfanHandlerCacheStub) ReleaseGrokVideoBilled(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.claims, key)
	return nil
}
func (s *qianfanHandlerCacheStub) SetReasoningContent(_ context.Context, _ string, _ string, _ time.Duration) error {
	return nil
}
func (s *qianfanHandlerCacheStub) GetReasoningContent(_ context.Context, _ string) (string, error) {
	return "", service.ErrReasoningContentNotFound
}

func newQianfanVideoBillingTestService(t *testing.T) *service.OpenAIGatewayService {
	t.Helper()
	return service.NewOpenAIGatewayService(
		nil, nil, nil, nil, nil, nil,
		newQianfanHandlerCacheStub(),
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
	)
}

func TestPrepareQianfanVideoCompletionBilling(t *testing.T) {
	gatewayService := newQianfanVideoBillingTestService(t)
	h := &OpenAIGatewayHandler{gatewayService: gatewayService}
	apiKey := &service.APIKey{ID: 300, UserID: 100}
	subject := middleware.AuthSubject{UserID: 100}
	ctx := context.Background()

	pending := service.BuildQianfanVideoPendingBilling(
		service.QianfanActionRequestInfo{Model: "musesteamer-air-i2v", DurationSeconds: 5},
		"musesteamer-air-i2v", "musesteamer-air-i2v", service.QianfanVideoPendingCreatedAtNow(),
	)
	require.NoError(t, gatewayService.StoreQianfanVideoPendingBilling(ctx, "task-1", 100, 300, pending))

	t.Run("claims once for completed task", func(t *testing.T) {
		statusResult := &service.OpenAIForwardResult{ResponseID: "task-1", VideoCount: 1, Model: "musesteamer-air-i2v", VideoResolution: service.VideoBillingResolution720P}
		billResult := h.prepareQianfanVideoCompletionBilling(ctx, zap.NewNop(), apiKey, subject, "task-1", statusResult)
		require.NotNil(t, billResult)
		require.Equal(t, 1, billResult.VideoCount)
		require.Equal(t, 5, billResult.VideoDurationSeconds)
		require.Equal(t, "qianfan-video:task-1", billResult.RequestID)

		require.Nil(t, h.prepareQianfanVideoCompletionBilling(ctx, zap.NewNop(), apiKey, subject, "task-1",
			&service.OpenAIForwardResult{ResponseID: "task-1", VideoCount: 1}), "second poll must not double bill")
	})

	t.Run("unfinished task never bills", func(t *testing.T) {
		require.Nil(t, h.prepareQianfanVideoCompletionBilling(ctx, zap.NewNop(), apiKey, subject, "task-1",
			&service.OpenAIForwardResult{ResponseID: "task-1"}))
	})

	t.Run("missing create snapshot fails closed", func(t *testing.T) {
		require.Nil(t, h.prepareQianfanVideoCompletionBilling(ctx, zap.NewNop(), apiKey, subject, "task-404",
			&service.OpenAIForwardResult{ResponseID: "task-404", VideoCount: 1}))
	})

	t.Run("nil inputs never bill", func(t *testing.T) {
		require.Nil(t, h.prepareQianfanVideoCompletionBilling(ctx, zap.NewNop(), apiKey, subject, "task-1", nil))
		empty := &OpenAIGatewayHandler{}
		require.Nil(t, empty.prepareQianfanVideoCompletionBilling(ctx, zap.NewNop(), apiKey, subject, "task-1",
			&service.OpenAIForwardResult{VideoCount: 1}))
	})
}

// --- End-to-end action flow harness ---

type qianfanFlowAccountRepo struct {
	service.AccountRepository
	mu       sync.Mutex
	accounts []service.Account
}

func (r *qianfanFlowAccountRepo) schedulable(platform string) []service.Account {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]service.Account, 0, len(r.accounts))
	for _, account := range r.accounts {
		if account.Platform == platform && account.IsSchedulable() {
			out = append(out, account)
		}
	}
	return out
}

func (r *qianfanFlowAccountRepo) ListSchedulableByPlatform(_ context.Context, platform string) ([]service.Account, error) {
	return r.schedulable(platform), nil
}

func (r *qianfanFlowAccountRepo) ListSchedulableByGroupIDAndPlatform(ctx context.Context, _ int64, platform string) ([]service.Account, error) {
	return r.ListSchedulableByPlatform(ctx, platform)
}

func (r *qianfanFlowAccountRepo) ListSchedulableUngroupedByPlatform(ctx context.Context, platform string) ([]service.Account, error) {
	return r.ListSchedulableByPlatform(ctx, platform)
}

func (r *qianfanFlowAccountRepo) GetByID(_ context.Context, id int64) (*service.Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, account := range r.accounts {
		if account.ID == id {
			copy := account
			return &copy, nil
		}
	}
	return nil, nil
}

// ListModelAvailabilityCandidates feeds the no-account diagnosis path; without
// it an empty candidate pool panics instead of reporting the real reason.
func (r *qianfanFlowAccountRepo) ListModelAvailabilityCandidates(_ context.Context, _ *int64, platforms []string, _ bool) ([]service.Account, error) {
	if len(platforms) == 0 {
		return nil, nil
	}
	return r.schedulable(platforms[0]), nil
}

type qianfanFlowUpstream struct {
	mu        sync.Mutex
	requests  []*http.Request
	bodies    [][]byte
	responder func(req *http.Request) *http.Response
}

func (s *qianfanFlowUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		body, _ = io.ReadAll(req.Body)
	}
	s.mu.Lock()
	s.requests = append(s.requests, req)
	s.bodies = append(s.bodies, body)
	s.mu.Unlock()
	return s.responder(req), nil
}

func (s *qianfanFlowUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return s.Do(req, proxyURL, accountID, accountConcurrency)
}

func (s *qianfanFlowUpstream) snapshot() ([]*http.Request, [][]byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*http.Request(nil), s.requests...), append([][]byte(nil), s.bodies...)
}

func qianfanFlowJSON(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func qianfanFlowBinary(contentType, body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{contentType}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

type qianfanFlowHarness struct {
	router    *gin.Engine
	upstream  *qianfanFlowUpstream
	gateway   *service.OpenAIGatewayService
	cache     *qianfanHandlerCacheStub
	cleanup   func()
	groupID   int64
	accountID int64
	apiKeyID  int64
	userID    int64
}

func qianfanFlowCatalogEvidence(account service.Account) service.Account {
	models := []string{"ernie-image", "ernie-irag-edit", "embedding-v1", "bce-reranker-base", "musesteamer-air-i2v", "qianfan-tts"}
	snapshot := service.AccountModelCatalogSnapshot{Complete: true, Revision: 1, DiscoveredModels: models}
	for _, model := range models {
		endpoints := []string{"responses", "chat_completions", "images", "embeddings"}
		switch model {
		case "bce-reranker-base":
			endpoints = []string{"rerank"}
		case "qianfan-tts":
			endpoints = []string{"speech"}
		}
		snapshot.EligibleModels = append(snapshot.EligibleModels, service.AccountCatalogModel{
			ID:                 model,
			Endpoints:          endpoints,
			EvidenceKind:       "fixture",
			EvidenceObservedAt: "2026-09-21T00:00:00Z",
		})
	}
	if account.Extra == nil {
		account.Extra = map[string]any{}
	}
	// A probed Qianfan account is served through the chat-completions direct
	// path, exactly like the production chat-only classification
	// (service.PlatformQianfan has no standalone Responses trunk binding);
	// without this probed marker the selection gate compares against the
	// Responses endpoint and refuses every model before the adapter runs.
	account.Extra[openAICompatResponsesSupportedKey] = false
	account.Extra[service.UpstreamModelCatalogExtraKey] = snapshot
	return account
}

// openAICompatResponsesSupportedKey mirrors the account-level probe marker that
// production accounts carry in Extra (openai_compat.ExtraKeyResponsesSupported).
const openAICompatResponsesSupportedKey = "openai_responses_supported"

func newQianfanFlowHarness(t *testing.T, responder func(req *http.Request) *http.Response, allowImageGeneration bool) *qianfanFlowHarness {
	t.Helper()
	gin.SetMode(gin.TestMode)
	groupID := int64(8101)
	accountID := int64(8102)
	apiKeyID := int64(8103)
	userID := int64(8104)
	repo := &qianfanFlowAccountRepo{accounts: []service.Account{qianfanFlowCatalogEvidence(service.Account{
		ID:          accountID,
		Platform:    service.PlatformQianfan,
		Type:        service.AccountTypeAPIKey,
		Status:      service.StatusActive,
		Schedulable: true,
		Credentials: map[string]any{"api_key": "flow-upstream-key"},
	})}}
	upstream := &qianfanFlowUpstream{responder: responder}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Gateway.MaxAccountSwitches = 1
	billingCache := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	cache := newQianfanHandlerCacheStub()
	gateway := service.NewOpenAIGatewayService(
		repo, nil, nil, nil, nil, nil, cache, cfg, nil, nil,
		service.NewBillingService(cfg, nil), nil, billingCache, upstream,
		&service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil,
	)
	concurrencyCache := &concurrencyCacheMock{
		acquireUserSlotFn:    func(context.Context, int64, int, string) (bool, error) { return true, nil },
		acquireAccountSlotFn: func(context.Context, int64, int, string) (bool, error) { return true, nil },
	}
	handler := NewOpenAIGatewayHandler(gateway, service.NewConcurrencyService(concurrencyCache), billingCache, &service.APIKeyService{}, nil, nil, nil, nil, cfg)

	apiKey := &service.APIKey{
		ID:      apiKeyID,
		GroupID: &groupID,
		User:    &service.User{ID: userID, Status: service.StatusActive},
		Group: &service.Group{
			ID:                   groupID,
			Platform:             service.PlatformQianfan,
			Status:               service.StatusActive,
			AllowImageGeneration: allowImageGeneration,
		},
	}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyAPIKey), apiKey)
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: apiKey.User.ID, Concurrency: 1})
		c.Next()
	})
	// Test paths deliberately avoid the GatewayActionForRoute markers: the
	// domain action matrix for qianfan lands with multimodal-ingress wiring.
	// Speech and rerank use their real ingress paths to exercise action-family resolution.
	router.POST("/qianfan-adapter/img-gen", handler.QianfanImages)
	router.POST("/qianfan-adapter/img-edit", handler.QianfanImageEdit)
	router.POST("/qianfan-adapter/emb", handler.QianfanEmbeddings)
	router.POST("/v1/rerank", handler.QianfanRerank)
	router.POST("/qianfan-adapter/vid-gen", handler.QianfanVideoGeneration)
	router.GET("/qianfan-adapter/vid/:request_id", handler.QianfanVideoStatus)
	router.DELETE("/qianfan-adapter/vid/:request_id", handler.QianfanVideoCancel)
	router.POST("/v1/audio/speech", handler.QianfanAudioSpeech)
	router.POST("/qianfan-adapter/voice/clone", handler.QianfanVoiceCloneCreate)

	return &qianfanFlowHarness{
		router:    router,
		upstream:  upstream,
		gateway:   gateway,
		cache:     cache,
		cleanup:   func() { billingCache.Stop() },
		groupID:   groupID,
		accountID: accountID,
		apiKeyID:  apiKeyID,
		userID:    userID,
	}
}

func (h *qianfanFlowHarness) serve(t *testing.T, method, target, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, reader)
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	if body != "" && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	h.router.ServeHTTP(recorder, req)
	return recorder
}

func TestQianfanImagesFlowPositive(t *testing.T) {
	h := newQianfanFlowHarness(t, func(req *http.Request) *http.Response {
		require.Equal(t, "https://qianfan.baidubce.com/v2/images/generations", req.URL.String())
		require.Equal(t, "Bearer flow-upstream-key", req.Header.Get("Authorization"))
		require.Empty(t, req.Header.Get("X-Appbuilder-Authorization"))
		return qianfanFlowJSON(http.StatusOK, `{"id":"as-1","created":1735264326,"data":[{"url":"https://qianfan-img-gen.bj.bcebos.com/img.png"}]}`)
	}, true)
	defer h.cleanup()

	recorder := h.serve(t, http.MethodPost, "/qianfan-adapter/img-gen",
		`{"model":"ernie-image","prompt":"海边日落","size":"1024x1024"}`, nil)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Contains(t, recorder.Body.String(), "qianfan-img-gen")
	requests, bodies := h.upstream.snapshot()
	require.Len(t, requests, 1)
	require.JSONEq(t, `{"model":"ernie-image","prompt":"海边日落","size":"1024x1024"}`, string(bodies[0]))
}

func TestQianfanImagesFlowPermissionDenied(t *testing.T) {
	upstreamCalled := false
	h := newQianfanFlowHarness(t, func(req *http.Request) *http.Response {
		upstreamCalled = true
		return qianfanFlowJSON(http.StatusOK, `{}`)
	}, false)
	defer h.cleanup()

	recorder := h.serve(t, http.MethodPost, "/qianfan-adapter/img-gen", `{"model":"ernie-image","prompt":"x"}`, nil)

	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.Contains(t, recorder.Body.String(), service.ImageGenerationPermissionMessage())
	require.False(t, upstreamCalled, "permission gate must reject before any upstream call")
}

func TestQianfanImagesFlowModelRequired(t *testing.T) {
	h := newQianfanFlowHarness(t, func(req *http.Request) *http.Response {
		return qianfanFlowJSON(http.StatusOK, `{}`)
	}, true)
	defer h.cleanup()

	recorder := h.serve(t, http.MethodPost, "/qianfan-adapter/img-gen", `{"prompt":"x"}`, nil)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), "model is required")
}

// Embeddings is no longer a chat-only capability mismatch: multimodal-ingress
// bound the qianfan.embeddings adapter in the action matrix, so
// SupportsOpenAIEndpointCapability admits OpenAIEndpointCapabilityEmbeddings for
// this API-key entry and the request reaches the documented /v2/embeddings
// contract instead of being vetoed before any upstream call.
func TestQianfanEmbeddingsFlowResolvesAfterTheChatOnlyRelease(t *testing.T) {
	upstreamCalled := false
	h := newQianfanFlowHarness(t, func(req *http.Request) *http.Response {
		upstreamCalled = true
		require.Equal(t, "https://qianfan.baidubce.com/v2/embeddings", req.URL.String())
		require.Equal(t, "Bearer flow-upstream-key", req.Header.Get("Authorization"))
		return qianfanFlowJSON(http.StatusOK, `{"id":"as-1","object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1,0.2]}],"usage":{"prompt_tokens":3,"total_tokens":3}}`)
	}, true)
	defer h.cleanup()

	recorder := h.serve(t, http.MethodPost, "/qianfan-adapter/emb",
		`{"model":"embedding-v1","input":["White T-shirt"]}`, nil)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.True(t, upstreamCalled, "the released action must reach its documented upstream contract")
}

func TestQianfanRerankFlow(t *testing.T) {
	h := newQianfanFlowHarness(t, func(req *http.Request) *http.Response {
		require.Equal(t, "https://qianfan.baidubce.com/v2/rerank", req.URL.String())
		return qianfanFlowJSON(http.StatusOK, `{"id":"as-3","object":"reranker_list","model":"bce-reranker-base","results":[{"document":"上海气候","relevance_score":0.68,"index":0}],"usage":{"prompt_tokens":11,"total_tokens":11}}`)
	}, true)
	defer h.cleanup()

	recorder := h.serve(t, http.MethodPost, "/v1/rerank",
		`{"model":"bce-reranker-base","query":"上海天气","documents":["上海气候","北京美食"]}`, nil)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Contains(t, recorder.Body.String(), "上海气候")
}

func TestQianfanImageEditFlowMultipartBridge(t *testing.T) {
	h := newQianfanFlowHarness(t, func(req *http.Request) *http.Response {
		require.Equal(t, "https://qianfan.baidubce.com/v2/images/edits", req.URL.String())
		require.Equal(t, "application/json", req.Header.Get("Content-Type"), "multipart edits bridge to the documented JSON body")
		require.Equal(t, "Bearer flow-upstream-key", req.Header.Get("Authorization"))
		require.Empty(t, req.Header.Get("X-Appbuilder-Authorization"))
		return qianfanFlowJSON(http.StatusOK, `{"id":"as-9","created":1,"data":[{"url":"https://x/edited.png"}]}`)
	}, true)
	defer h.cleanup()

	var body strings.Builder
	body.WriteString("--eboundary\r\nContent-Disposition: form-data; name=\"model\"\r\n\r\nernie-irag-edit\r\n")
	body.WriteString("--eboundary\r\nContent-Disposition: form-data; name=\"prompt\"\r\n\r\n替换背景\r\n")
	body.WriteString("--eboundary\r\nContent-Disposition: form-data; name=\"image\"; filename=\"source.png\"\r\nContent-Type: image/png\r\n\r\nPNGDATA\r\n")
	body.WriteString("--eboundary--\r\n")

	recorder := h.serve(t, http.MethodPost, "/qianfan-adapter/img-edit", body.String(),
		map[string]string{"Content-Type": "multipart/form-data; boundary=eboundary"})

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	requests, bodies := h.upstream.snapshot()
	require.Len(t, requests, 1)
	require.Equal(t, "ernie-irag-edit", gjson.GetBytes(bodies[0], "model").String())
	require.Equal(t, "替换背景", gjson.GetBytes(bodies[0], "prompt").String())
	require.True(t, strings.HasPrefix(gjson.GetBytes(bodies[0], "image").String(), "data:image/png;base64,"))
}

func TestQianfanImageEditFlowMissingImageIsClientFault(t *testing.T) {
	upstreamCalled := false
	h := newQianfanFlowHarness(t, func(req *http.Request) *http.Response {
		upstreamCalled = true
		return qianfanFlowJSON(http.StatusOK, `{"data":[{"url":"https://x/y.png"}]}`)
	}, true)
	defer h.cleanup()

	body := "--eboundary\r\nContent-Disposition: form-data; name=\"model\"\r\n\r\nernie-irag-edit\r\n--eboundary--\r\n"
	recorder := h.serve(t, http.MethodPost, "/qianfan-adapter/img-edit", body,
		map[string]string{"Content-Type": "multipart/form-data; boundary=eboundary"})

	require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
	require.Contains(t, recorder.Body.String(), "image part")
	require.False(t, upstreamCalled, "a malformed edit body must be rejected before any upstream call")
}

func TestQianfanVideoFlowOwnerBinding(t *testing.T) {
	h := newQianfanFlowHarness(t, func(req *http.Request) *http.Response {
		return qianfanFlowJSON(http.StatusOK, `{"id":"as-4","task_id":"task-9","model":"musesteamer-air-i2v","status":"succeeded","content":{"video_url":"https://cdn.example.com/v.mp4"},"duration":5,"width":1280,"height":704}`)
	}, true)
	defer h.cleanup()

	t.Run("unbound task is not found for any tenant", func(t *testing.T) {
		recorder := h.serve(t, http.MethodGet, "/qianfan-adapter/vid/task-9", "", nil)
		require.Equal(t, http.StatusNotFound, recorder.Code)
		require.Contains(t, recorder.Body.String(), "Video request not found")
	})

	t.Run("owner bound task is served", func(t *testing.T) {
		require.NoError(t, h.gateway.BindQianfanVideoTaskAccount(context.Background(), &h.groupID, "task-9", h.userID, h.apiKeyID, h.accountID))
		recorder := h.serve(t, http.MethodGet, "/qianfan-adapter/vid/task-9", "", nil)
		require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
		require.Contains(t, recorder.Body.String(), "succeeded")
	})

	t.Run("a different key cannot read the bound task", func(t *testing.T) {
		// The binding is scoped to owner+key, so resolving with another key id
		// yields no account and the handler returns 404 before any upstream call.
		_, err := h.gateway.ResolveQianfanVideoTaskAccount(context.Background(), &h.groupID, "task-9", h.userID, h.apiKeyID+1)
		require.Error(t, err)
	})
}

func TestQianfanVideoCancelFlow(t *testing.T) {
	h := newQianfanFlowHarness(t, func(req *http.Request) *http.Response {
		require.Equal(t, http.MethodDelete, req.Method)
		require.Contains(t, req.URL.String(), "task_id=task-9")
		return qianfanFlowJSON(http.StatusOK, `{"id":"as-5","task_id":"task-9","status":"Terminated"}`)
	}, true)
	defer h.cleanup()

	require.NoError(t, h.gateway.BindQianfanVideoTaskAccount(context.Background(), &h.groupID, "task-9", h.userID, h.apiKeyID, h.accountID))
	recorder := h.serve(t, http.MethodDelete, "/qianfan-adapter/vid/task-9", "", nil)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Contains(t, recorder.Body.String(), "Terminated")
}

func TestQianfanAudioSpeechFlow(t *testing.T) {
	h := newQianfanFlowHarness(t, func(req *http.Request) *http.Response {
		require.Equal(t, "https://tsn.baidu.com/text2audio", req.URL.String())
		require.Equal(t, "Bearer flow-upstream-key", req.Header.Get("Authorization"))
		return qianfanFlowBinary("audio/mp3", "MP3DATA")
	}, true)
	defer h.cleanup()

	recorder := h.serve(t, http.MethodPost, "/v1/audio/speech",
		`{"model":"qianfan-tts","input":"今天天气不错","voice":"5003"}`, nil)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, "MP3DATA", recorder.Body.String())
	requests, bodies := h.upstream.snapshot()
	require.Len(t, requests, 1)
	require.Contains(t, string(bodies[0]), "tex=")
}

func TestQianfanVoiceCloneFlow(t *testing.T) {
	h := newQianfanFlowHarness(t, func(req *http.Request) *http.Response {
		require.Equal(t, "https://aip.baidubce.com/rest/2.0/speech/publiccloudspeech/v1/voice/clone/create", req.URL.String())
		return qianfanFlowJSON(http.StatusOK, `{"voice_id":"voice-1"}`)
	}, true)
	defer h.cleanup()

	recorder := h.serve(t, http.MethodPost, "/qianfan-adapter/voice/clone", `{"voice_name":"demo","lang":"zh"}`, nil)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Contains(t, recorder.Body.String(), "voice-1")
}
