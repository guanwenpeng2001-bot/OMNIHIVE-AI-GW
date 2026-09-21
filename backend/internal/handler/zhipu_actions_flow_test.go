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

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// zhipuFlowAccountRepo serves schedulable zhipu accounts to the scheduler.
type zhipuFlowAccountRepo struct {
	service.AccountRepository
	mu       sync.Mutex
	accounts []service.Account
}

func (r *zhipuFlowAccountRepo) schedulable(platform string) []service.Account {
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

func (r *zhipuFlowAccountRepo) ListSchedulableByPlatform(_ context.Context, platform string) ([]service.Account, error) {
	return r.schedulable(platform), nil
}

func (r *zhipuFlowAccountRepo) ListSchedulableByGroupIDAndPlatform(ctx context.Context, _ int64, platform string) ([]service.Account, error) {
	return r.ListSchedulableByPlatform(ctx, platform)
}

func (r *zhipuFlowAccountRepo) ListSchedulableUngroupedByPlatform(ctx context.Context, platform string) ([]service.Account, error) {
	return r.ListSchedulableByPlatform(ctx, platform)
}

func (r *zhipuFlowAccountRepo) GetByID(_ context.Context, id int64) (*service.Account, error) {
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

// zhipuFlowUpstream records requests and delegates responses.
type zhipuFlowUpstream struct {
	mu        sync.Mutex
	requests  []*http.Request
	bodies    [][]byte
	responder func(req *http.Request) *http.Response
}

func (s *zhipuFlowUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
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

func (s *zhipuFlowUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return s.Do(req, proxyURL, accountID, accountConcurrency)
}

func (s *zhipuFlowUpstream) snapshot() ([]*http.Request, [][]byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*http.Request(nil), s.requests...), append([][]byte(nil), s.bodies...)
}

func zhipuFlowJSON(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

type zhipuFlowHarness struct {
	router    *gin.Engine
	upstream  *zhipuFlowUpstream
	cache     *zhipuHandlerCacheStub
	cleanup   func()
	groupID   int64
	accountID int64
}

// zhipuFlowCatalogEvidence attaches catalog evidence for the models exercised
// by the flow tests. The shared eligibility probe (requestAccountCatalogSupports)
// requires per-model endpoint evidence whenever a request plan is in context;
// production accounts receive the same evidence from model discovery.
func zhipuFlowCatalogEvidence(account service.Account) service.Account {
	models := []string{"glm-image", "cogview-4", "cogvideox-3", "embedding-3", "glm-tts", "glm-asr-2512", "rerank"}
	snapshot := service.AccountModelCatalogSnapshot{Complete: true, Revision: 1, DiscoveredModels: models}
	for _, model := range models {
		snapshot.EligibleModels = append(snapshot.EligibleModels, service.AccountCatalogModel{
			ID:                 model,
			Endpoints:          []string{"responses", "chat_completions", "images", "embeddings"},
			EvidenceKind:       "fixture",
			EvidenceObservedAt: "2026-09-25T00:00:00Z",
		})
	}
	if account.Extra == nil {
		account.Extra = map[string]any{}
	}
	account.Extra[service.UpstreamModelCatalogExtraKey] = snapshot
	return account
}

func newZhipuFlowHarness(t *testing.T, responder func(req *http.Request) *http.Response, allowImageGeneration bool) *zhipuFlowHarness {
	t.Helper()
	gin.SetMode(gin.TestMode)
	groupID := int64(7101)
	accountID := int64(7102)
	repo := &zhipuFlowAccountRepo{accounts: []service.Account{zhipuFlowCatalogEvidence(service.Account{
		ID:          accountID,
		Platform:    service.PlatformZhipu,
		Type:        service.AccountTypeAPIKey,
		Status:      service.StatusActive,
		Schedulable: true,
		Credentials: map[string]any{"api_key": "flow-upstream-key"},
	})}}
	upstream := &zhipuFlowUpstream{responder: responder}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Gateway.MaxAccountSwitches = 1
	billingCache := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	cache := newZhipuHandlerCacheStub()
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
		ID:      7103,
		GroupID: &groupID,
		User:    &service.User{ID: 7104, Status: service.StatusActive},
		Group: &service.Group{
			ID:                   groupID,
			Platform:             service.PlatformZhipu,
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
	// domain action matrix for zhipu lands with multimodal-ingress wiring, and
	// an unresolved action would filter the fixture account out of selection.
	router.POST("/zhipu-adapter/img-gen", handler.ZhipuImages)
	router.POST("/zhipu-adapter/vid-gen", handler.ZhipuVideoGeneration)
	router.GET("/zhipu-adapter/vid/:request_id", handler.ZhipuVideoStatus)
	router.GET("/zhipu-adapter/vid/:request_id/content", handler.ZhipuVideoContent)
	router.POST("/zhipu-adapter/emb", handler.ZhipuEmbeddings)
	router.POST("/zhipu-adapter/speak", handler.ZhipuAudioSpeech)

	return &zhipuFlowHarness{
		router:    router,
		upstream:  upstream,
		cache:     cache,
		cleanup:   func() { billingCache.Stop() },
		groupID:   groupID,
		accountID: accountID,
	}
}

func (h *zhipuFlowHarness) serve(t *testing.T, method, target, body string, headers map[string]string) *httptest.ResponseRecorder {
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

func TestZhipuImagesFlowPositive(t *testing.T) {
	h := newZhipuFlowHarness(t, func(req *http.Request) *http.Response {
		require.Equal(t, "https://open.bigmodel.cn/api/paas/v4/images/generations", req.URL.String())
		require.Equal(t, "Bearer flow-upstream-key", req.Header.Get("Authorization"))
		return zhipuFlowJSON(http.StatusOK, `{"created":1732083164,"data":[{"url":"https://cdn.bigmodel.cn/img.png"}]}`)
	}, true)
	defer h.cleanup()

	recorder := h.serve(t, http.MethodPost, "/zhipu-adapter/img-gen",
		`{"model":"glm-image","prompt":"一只小猫","size":"1280x1280"}`, nil)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Contains(t, recorder.Body.String(), "https://cdn.bigmodel.cn/img.png")
	requests, bodies := h.upstream.snapshot()
	require.Len(t, requests, 1)
	require.JSONEq(t, `{"model":"glm-image","prompt":"一只小猫","size":"1280x1280"}`, string(bodies[0]))
}

func TestZhipuImagesFlowPermissionDenied(t *testing.T) {
	upstreamCalled := false
	h := newZhipuFlowHarness(t, func(req *http.Request) *http.Response {
		upstreamCalled = true
		return zhipuFlowJSON(http.StatusOK, `{}`)
	}, false)
	defer h.cleanup()

	recorder := h.serve(t, http.MethodPost, "/zhipu-adapter/img-gen",
		`{"model":"glm-image","prompt":"一只小猫"}`, nil)

	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.Contains(t, recorder.Body.String(), service.ImageGenerationPermissionMessage())
	require.False(t, upstreamCalled, "permission gate must reject before any upstream call")
}

func TestZhipuImagesFlowModelRequired(t *testing.T) {
	h := newZhipuFlowHarness(t, func(req *http.Request) *http.Response {
		return zhipuFlowJSON(http.StatusOK, `{}`)
	}, true)
	defer h.cleanup()

	recorder := h.serve(t, http.MethodPost, "/zhipu-adapter/img-gen", `{"prompt":"x"}`, nil)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), "model is required")
}

func TestZhipuImagesFlowUpstreamError(t *testing.T) {
	h := newZhipuFlowHarness(t, func(req *http.Request) *http.Response {
		return zhipuFlowJSON(http.StatusBadRequest, `{"error":{"code":"1214","message":"prompt 长度超限"}}`)
	}, true)
	defer h.cleanup()

	recorder := h.serve(t, http.MethodPost, "/zhipu-adapter/img-gen",
		`{"model":"cogview-4","prompt":"x"}`, nil)
	require.NotEqual(t, http.StatusOK, recorder.Code)
	require.NotEqual(t, http.StatusForbidden, recorder.Code)
}

func TestZhipuVideoLifecycleFlow(t *testing.T) {
	h := newZhipuFlowHarness(t, func(req *http.Request) *http.Response {
		switch {
		case req.URL.Path == "/api/paas/v4/videos/generations":
			return zhipuFlowJSON(http.StatusOK, `{"id":"task-flow-1","request_id":"req-1","model":"cogvideox-3","task_status":"PROCESSING"}`)
		case req.URL.Path == "/api/paas/v4/async-result/task-flow-1":
			return zhipuFlowJSON(http.StatusOK, `{"model":"cogvideox-3","task_status":"SUCCESS","video_result":[{"url":"https://cdn.bigmodel.cn/v.mp4","cover_image_url":"https://cdn.bigmodel.cn/c.png"}]}`)
		case req.URL.String() == "https://cdn.bigmodel.cn/v.mp4":
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"video/mp4"}},
				Body:       io.NopCloser(strings.NewReader("video-bytes")),
			}
		default:
			return zhipuFlowJSON(http.StatusNotFound, `{"error":{"code":"404","message":"unknown"}}`)
		}
	}, true)
	defer h.cleanup()

	// 1. Create: accepted, task bound to owner+key+account, no billing yet.
	createRecorder := h.serve(t, http.MethodPost, "/zhipu-adapter/vid-gen",
		`{"model":"cogvideox-3","prompt":"A cat is playing with a ball.","size":"1920x1080","duration":10,"request_id":"idem-submit-0001"}`, nil)
	require.Equal(t, http.StatusOK, createRecorder.Code, createRecorder.Body.String())
	require.Contains(t, createRecorder.Body.String(), "task-flow-1")
	boundAccountID, bindErr := h.cache.GetSessionAccountID(context.Background(), h.groupID,
		"openai:"+service.ZhipuVideoTaskSessionHash("task-flow-1", 7104, 7103))
	require.NoError(t, bindErr)
	require.Equal(t, h.accountID, boundAccountID, "create must bind the task to the accepting account")
	createRequests, createBodies := h.upstream.snapshot()
	require.Len(t, createRequests, 1)
	require.JSONEq(t, `{"model":"cogvideox-3","prompt":"A cat is playing with a ball.","size":"1920x1080","duration":10,"request_id":"idem-submit-0001"}`,
		string(createBodies[0]), "client request_id must pass through for idempotent submission")

	// 2. Status success: video_result.url rewritten to the gateway proxy; completion billed once.
	statusRecorder := h.serve(t, http.MethodGet, "/zhipu-adapter/vid/task-flow-1", "", nil)
	require.Equal(t, http.StatusOK, statusRecorder.Code)
	require.Contains(t, statusRecorder.Body.String(), "/videos/task-flow-1/content")
	require.NotContains(t, statusRecorder.Body.String(), "cdn.bigmodel.cn/v.mp4")
	claimKey := "7104:7103:task-flow-1"
	h.cache.mu.Lock()
	_, pendingStored := h.cache.pendingBilling[claimKey]
	claimed := h.cache.claims[claimKey]
	h.cache.mu.Unlock()
	require.True(t, pendingStored, "create must store the pending billing snapshot")
	require.True(t, claimed, "first SUCCESS observation must claim the completion billing")

	// 3. Content download: re-queries the owner-bound status, then proxies the
	// CDN download without leaking the upstream credential.
	contentRecorder := h.serve(t, http.MethodGet, "/zhipu-adapter/vid/task-flow-1/content", "", nil)
	require.Equal(t, http.StatusOK, contentRecorder.Code)
	require.Equal(t, "video-bytes", contentRecorder.Body.String())
	requests, _ := h.upstream.snapshot()
	require.Len(t, requests, 4)
	require.Equal(t, "https://open.bigmodel.cn/api/paas/v4/async-result/task-flow-1", requests[2].URL.String())
	require.Equal(t, "https://cdn.bigmodel.cn/v.mp4", requests[3].URL.String())
	require.Empty(t, requests[3].Header.Get("Authorization"), "CDN download must not carry the upstream key")
}

func TestZhipuVideoStatusFlowCrossTenantDenied(t *testing.T) {
	h := newZhipuFlowHarness(t, func(req *http.Request) *http.Response {
		return zhipuFlowJSON(http.StatusOK, `{"task_status":"SUCCESS","video_result":[{"url":"https://cdn/x.mp4"}]}`)
	}, true)
	defer h.cleanup()

	// No binding exists for this task under the caller's user+key: a foreign
	// task id must not resolve (404, upstream never queried for it).
	recorder := h.serve(t, http.MethodGet, "/zhipu-adapter/vid/foreign-task-9", "", nil)
	require.Equal(t, http.StatusNotFound, recorder.Code)
	require.Empty(t, func() []*http.Request { r, _ := h.upstream.snapshot(); return r }())
}

func TestZhipuEmbeddingsFlowPositive(t *testing.T) {
	h := newZhipuFlowHarness(t, func(req *http.Request) *http.Response {
		require.Equal(t, "https://open.bigmodel.cn/api/paas/v4/embeddings", req.URL.String())
		require.Equal(t, "Bearer flow-upstream-key", req.Header.Get("Authorization"))
		return zhipuFlowJSON(http.StatusOK, `{"model":"embedding-3","object":"list","data":[{"index":0,"object":"embedding","embedding":[0.1]}],"usage":{"prompt_tokens":7,"total_tokens":7}}`)
	}, true)
	defer h.cleanup()

	recorder := h.serve(t, http.MethodPost, "/zhipu-adapter/emb",
		`{"model":"embedding-3","input":"你好，今天天气怎么样.","dimensions":2048}`, nil)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Contains(t, recorder.Body.String(), `"object":"list"`)
}

func TestZhipuAudioSpeechFlowPositive(t *testing.T) {
	h := newZhipuFlowHarness(t, func(req *http.Request) *http.Response {
		require.Equal(t, "https://open.bigmodel.cn/api/paas/v4/audio/speech", req.URL.String())
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"audio/wav"}},
			Body:       io.NopCloser(strings.NewReader("RIFF-wav")),
		}
	}, true)
	defer h.cleanup()

	recorder := h.serve(t, http.MethodPost, "/zhipu-adapter/speak",
		`{"model":"glm-tts","input":"你好，今天天气怎么样.","voice":"tongtong","response_format":"wav"}`, nil)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, "RIFF-wav", recorder.Body.String())
	require.Equal(t, "audio/wav", recorder.Header().Get("Content-Type"))
}
