//go:build unit

package handler

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/internal/testutil"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

// Tencent Hunyuan / TokenHub action handler tests. Upstream payloads follow the
// saved first-party TokenHub documentation captured on 2026-09-21
// (image 1823/135745, video 1823/137202, speech 1823/135791-135796,
// voice clone 1823/135798, embeddings 1823/133515). Positive upstream-shape and
// billing coverage for those paths lives in
// backend/internal/service/hunyuan_actions_test.go; this file covers handler
// admission, group exposure, owner-scoped task reads and claim idempotency.

type hunyuanActionCache struct {
	testutil.StubGatewayCache
	mu       sync.Mutex
	sessions map[string]int64
	pending  map[string][]byte
	billed   map[string]bool
}

func newHunyuanActionCache() *hunyuanActionCache {
	return &hunyuanActionCache{
		sessions: map[string]int64{},
		pending:  map[string][]byte{},
		billed:   map[string]bool{},
	}
}

func (s *hunyuanActionCache) GetSessionAccountID(_ context.Context, groupID int64, key string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	owner, ok := s.sessions[hunyuanActionSessionKey(groupID, key)]
	if !ok {
		return 0, service.ErrStickySessionNotFound
	}
	return owner, nil
}

func (s *hunyuanActionCache) SetSessionAccountID(_ context.Context, groupID int64, key string, owner int64, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[hunyuanActionSessionKey(groupID, key)] = owner
	return nil
}

func (s *hunyuanActionCache) SetGrokVideoPendingBilling(_ context.Context, key string, payload []byte, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending[key] = append([]byte(nil), payload...)
	return nil
}

func (s *hunyuanActionCache) GetGrokVideoPendingBilling(_ context.Context, key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	payload, ok := s.pending[key]
	if !ok {
		return nil, nil
	}
	return payload, nil
}

func (s *hunyuanActionCache) ClaimGrokVideoBilled(_ context.Context, key string, _ time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.billed[key] {
		return false, nil
	}
	s.billed[key] = true
	return true, nil
}

func (s *hunyuanActionCache) ReleaseGrokVideoBilled(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.billed, key)
	return nil
}

func hunyuanActionSessionKey(groupID int64, key string) string {
	return fmt.Sprintf("%d|%s", groupID, key)
}

type hunyuanActionUpstream struct {
	service.HTTPUpstream
	mu    sync.Mutex
	call  func(*http.Request, int64) (*http.Response, error)
	calls int
}

func (u *hunyuanActionUpstream) Do(req *http.Request, _ string, id int64, _ int) (*http.Response, error) {
	u.mu.Lock()
	u.calls++
	call := u.call
	u.mu.Unlock()
	return call(req, id)
}

func (u *hunyuanActionUpstream) callCount() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.calls
}

func hunyuanActionJSONResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

// hunyuanActionFixtureAccount attaches catalog evidence for the TokenHub action
// models so account eligibility has the same shape as production accounts.
func hunyuanActionFixtureAccount(account service.Account) service.Account {
	extra := make(map[string]any, len(account.Extra)+1)
	for key, value := range account.Extra {
		extra[key] = value
	}
	models := []string{"hy3", "hy-image-v3", "hy-world2-panorama", "hy-video-v1.5", "hy-asr-3.0-preview", "minimax-speech-2.8-hd", "minimax-voice-clone", "kinfra-text-embedding-0.6b"}
	catalog := service.AccountModelCatalogSnapshot{Complete: true, Revision: 1, DiscoveredModels: models, EligibleModels: []service.AccountCatalogModel{}}
	for _, model := range models {
		endpoints := []string{"responses", "chat_completions", "messages", "images"}
		switch model {
		case "hy-asr-3.0-preview":
			endpoints = []string{"transcriptions"}
		case "minimax-speech-2.8-hd":
			endpoints = []string{"speech"}
		case "minimax-voice-clone":
			endpoints = []string{"voice"}
		}
		catalog.EligibleModels = append(catalog.EligibleModels, service.AccountCatalogModel{ID: model, Endpoints: endpoints, EvidenceKind: "fixture", EvidenceObservedAt: "2026-09-21T00:00:00Z"})
	}
	extra[service.UpstreamModelCatalogExtraKey] = catalog
	account.Extra = extra
	return account
}

// hunyuanActionAccountRepo answers the account queries the handler needs,
// including the model-availability diagnosis used by no-account classification.
type hunyuanActionAccountRepo struct {
	service.AccountRepository
	accounts []service.Account
}

func (r hunyuanActionAccountRepo) GetByID(_ context.Context, id int64) (*service.Account, error) {
	for i := range r.accounts {
		if r.accounts[i].ID == id {
			account := r.accounts[i]
			return &account, nil
		}
	}
	return nil, service.ErrNoAvailableAccounts
}

func (r hunyuanActionAccountRepo) ListSchedulableByGroupIDAndPlatform(_ context.Context, _ int64, platform string) ([]service.Account, error) {
	return r.accountsForPlatform(platform), nil
}

func (r hunyuanActionAccountRepo) ListSchedulableByPlatform(_ context.Context, platform string) ([]service.Account, error) {
	return r.accountsForPlatform(platform), nil
}

func (r hunyuanActionAccountRepo) ListSchedulableUngroupedByPlatform(_ context.Context, platform string) ([]service.Account, error) {
	return r.accountsForPlatform(platform), nil
}

func (r hunyuanActionAccountRepo) ListModelAvailabilityCandidates(_ context.Context, groupID *int64, platforms []string, _ bool) ([]service.Account, error) {
	out := make([]service.Account, 0, len(r.accounts))
	for _, account := range r.accounts {
		for _, platform := range platforms {
			if account.Platform == platform {
				out = append(out, account)
				break
			}
		}
	}
	return out, nil
}

func (r hunyuanActionAccountRepo) accountsForPlatform(platform string) []service.Account {
	out := make([]service.Account, 0, len(r.accounts))
	for _, account := range r.accounts {
		if account.Platform == platform {
			out = append(out, account)
		}
	}
	return out
}

func newHunyuanActionHandler(t *testing.T) (*OpenAIGatewayHandler, *hunyuanActionCache, *hunyuanActionUpstream, []service.Account) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	accounts := make([]service.Account, 2)
	for i := range accounts {
		accounts[i] = service.Account{
			ID: int64(i + 1), Platform: service.PlatformHunyuan, Type: service.AccountTypeAPIKey,
			Status: service.StatusActive, Schedulable: true, Concurrency: 50, Priority: i,
			GroupIDs:    []int64{24},
			Credentials: map[string]any{"api_key": "hunyuan-test-key", "base_url": "https://tokenhub.tencentmaas.com/v1"},
		}
	}
	for index := range accounts {
		accounts[index] = hunyuanActionFixtureAccount(accounts[index])
	}
	concurrency := service.NewConcurrencyService(&grokMediaSlotsCache{accounts: map[string]int64{}, users: map[string]int64{}})
	bindings := newHunyuanActionCache()
	upstream := &hunyuanActionUpstream{call: func(*http.Request, int64) (*http.Response, error) {
		return hunyuanActionJSONResponse(`{"task_id":"task-1"}`), nil
	}}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Gateway.Scheduling.StickySessionWaitTimeout = 20 * time.Millisecond
	cfg.Gateway.Scheduling.StickySessionMaxWaiting = 3
	repo := hunyuanActionAccountRepo{accounts: accounts}
	gateway := service.ProvideOpenAIGatewayService(repo, nil, nil, nil, nil, nil, bindings, cfg, nil, concurrency, nil, nil, nil, upstream, nil, nil, nil, nil, nil, nil, nil, nil, service.NewUsageSettlementService(&unlimitedTPMReceiptStub{}, nil, nil))
	billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billing.Stop)
	handler := NewOpenAIGatewayHandler(gateway, concurrency, billing, service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, cfg)
	return handler, bindings, upstream, accounts
}

func hunyuanActionContext(method, path, body, platform string) (*gin.Context, *httptest.ResponseRecorder) {
	groupID := int64(24)
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{ID: 20, UserID: 10, GroupID: &groupID,
		Group: &service.Group{ID: groupID, Platform: platform, AllowImageGeneration: true}, User: &service.User{ID: 10}})
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 10, Concurrency: 5})
	return c, w
}

// TestHunyuanNativeActionPathsReachTheDocumentedTokenHubEndpoints proves the
// ingress seam end to end for the promoted cells: once the platform publishes its
// protocol facts and the catalog evidence admits the account, every native
// handler forwards to its documented TokenHub path instead of failing closed
// before the upstream. Audio models declare their actual action family;
// text evidence cannot authorize an audio request. Positive upstream-shape
// coverage for these exact endpoints lives in
// backend/internal/service/hunyuan_actions_test.go
// (TestHunyuanForwardImageGeneration, TestHunyuanForwardVideoGeneration,
// TestHunyuanForwardSpeechSynthesis, TestHunyuanForwardVoiceClone,
// TestHunyuanForwardEmbeddings).
func TestHunyuanNativeActionPathsReachTheDocumentedTokenHubEndpoints(t *testing.T) {
	scenarios := []struct {
		name     string
		method   string
		path     string
		body     string
		platform string
		response string
		wantURL  string
		invoke   func(*OpenAIGatewayHandler, *gin.Context)
	}{
		{
			name: "image generate", method: http.MethodPost, path: "/v1/images/generations",
			body:     `{"model":"hy-image-v3","prompt":"一只橙色小猫在窗台上","size":"1024x1024"}`,
			platform: service.PlatformHunyuan,
			response: `{"data":[{"url":"https://aigc-image.cos.myqcloud.com/xxx/result.png"}]}`,
			wantURL:  "https://tokenhub.tencentmaas.com/v1/wand/hunyuan-image/v3-generation",
			invoke:   func(h *OpenAIGatewayHandler, c *gin.Context) { h.HunyuanImageGeneration(c) },
		},
		{
			name: "video create", method: http.MethodPost, path: "/v1/videos",
			body:     `{"model":"hy-video-v1.5","prompt":"一只小狗在草地上奔跑","aspect_ratio":"16:9"}`,
			platform: service.PlatformHunyuan,
			response: `{"task_id":"task-xyz"}`,
			wantURL:  "https://tokenhub.tencentmaas.com/v1/wand/hunyuan-video/generation",
			invoke:   func(h *OpenAIGatewayHandler, c *gin.Context) { h.HunyuanVideoGeneration(c) },
		},
		{
			name: "tts", method: http.MethodPost, path: "/v1/audio/speech",
			body:     `{"model":"minimax-speech-2.8-hd","text":"刚吹过晚风的巷口"}`,
			platform: service.PlatformHunyuan,
			response: `{"data":{"audio":"deadbeef","status":2},"extra_info":{"audio_length":9900,"usage_characters":26},"base_resp":{"status_code":0,"status_msg":"success"}}`,
			wantURL:  "https://tokenhub.tencentmaas.com/v1/wand/minimax-tts/sync_tts",
			invoke:   func(h *OpenAIGatewayHandler, c *gin.Context) { h.HunyuanAudioSpeech(c) },
		},
		{
			name: "voice clone", method: http.MethodPost, path: "/v1/custom-voices",
			body:     `{"model":"minimax-voice-clone","audio_url":"https://cos.example.com/source.wav"}`,
			platform: service.PlatformHunyuan,
			response: `{"voice_id":"minimax_55e81114-ce7e-4ca1","base_resp":{"status_code":0,"status_msg":"success"}}`,
			wantURL:  "https://tokenhub.tencentmaas.com/v1/wand/minimax-tts/sync_clone",
			invoke:   func(h *OpenAIGatewayHandler, c *gin.Context) { h.HunyuanVoiceClone(c) },
		},
		{
			name: "composite video create", method: http.MethodPost, path: "/v1/videos",
			body:     `{"model":"hy-video-v1.5","prompt":"composite video"}`,
			platform: service.PlatformComposite,
			response: `{"task_id":"task-xyz"}`,
			wantURL:  "https://tokenhub.tencentmaas.com/v1/wand/hunyuan-video/generation",
			invoke:   func(h *OpenAIGatewayHandler, c *gin.Context) { h.HunyuanVideoGeneration(c) },
		},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			h, _, upstream, _ := newHunyuanActionHandler(t)
			var sent *http.Request
			upstream.call = func(request *http.Request, _ int64) (*http.Response, error) {
				sent = request
				return hunyuanActionJSONResponse(scenario.response), nil
			}
			c, w := hunyuanActionContext(scenario.method, scenario.path, scenario.body, scenario.platform)
			if scenario.platform == service.PlatformComposite {
				c.Request = c.Request.WithContext(service.WithResolvedTargetPlatform(c.Request.Context(), service.PlatformHunyuan))
			}
			scenario.invoke(h, c)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			require.Equal(t, 1, upstream.callCount(), "the documented endpoint is called exactly once")
			require.NotNil(t, sent)
			require.Equal(t, scenario.wantURL, sent.URL.String())
		})
	}
}

// TestHunyuan3DQueryOwnerIsolationAndLookup covers the HY-3D task pair through
// the handler. The query identifies its task in the body rather than the path,
// so the owner binding written when the task was accepted is the only thing that
// can resolve it, and the model named by the query never re-filters the bound
// account.
func TestHunyuan3DQueryOwnerIsolationAndLookup(t *testing.T) {
	t.Run("owner lookup reaches the bound account", func(t *testing.T) {
		h, _, upstream, _ := newHunyuanActionHandler(t)
		groupID := int64(24)
		require.NoError(t, h.gatewayService.BindHunyuanTaskAccount(context.Background(), &groupID, "hy3d-1", 10, 20, 1))
		upstream.call = func(req *http.Request, _ int64) (*http.Response, error) {
			require.Equal(t, "https://tokenhub.tencentmaas.com/v1/api/3d/query", req.URL.String())
			require.Equal(t, http.MethodPost, req.Method)
			require.Equal(t, "Bearer hunyuan-test-key", req.Header.Get("Authorization"))
			return hunyuanActionJSONResponse(`{"id":"hy3d-1","status":"completed","model":"hy-3d-3.1"}`), nil
		}
		c, w := hunyuanActionContext(http.MethodPost, "/v1/hunyuan/3d/query", `{"model":"hy-3d-3.1","id":"hy3d-1"}`, service.PlatformHunyuan)

		h.Hunyuan3DQuery(c)

		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.Equal(t, 1, upstream.callCount())
		require.Equal(t, "hy3d-1", gjson.GetBytes(w.Body.Bytes(), "id").String())
	})

	t.Run("another tenant cannot read the task", func(t *testing.T) {
		h, _, upstream, _ := newHunyuanActionHandler(t)
		groupID := int64(24)
		require.NoError(t, h.gatewayService.BindHunyuanTaskAccount(context.Background(), &groupID, "hy3d-1", 10, 20, 1))
		c, w := hunyuanActionContext(http.MethodPost, "/v1/hunyuan/3d/query", `{"model":"hy-3d-3.1","id":"hy3d-1"}`, service.PlatformHunyuan)
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 11, Concurrency: 5})

		h.Hunyuan3DQuery(c)

		require.Equal(t, http.StatusNotFound, w.Code)
		require.Zero(t, upstream.callCount(), "an unowned task id must never reach the upstream")
	})

	t.Run("a query without a task id is rejected", func(t *testing.T) {
		h, _, upstream, _ := newHunyuanActionHandler(t)
		c, w := hunyuanActionContext(http.MethodPost, "/v1/hunyuan/3d/query", `{"model":"hy-3d-3.1"}`, service.PlatformHunyuan)

		h.Hunyuan3DQuery(c)

		require.Equal(t, http.StatusBadRequest, w.Code)
		require.Zero(t, upstream.callCount())
	})
}

// TestHunyuanVideoLookupOwnerIsolation proves a task id can never be read across
// tenants: another user, key, group or task id always answers not-found before
// any upstream traffic.
func TestHunyuanVideoLookupOwnerIsolation(t *testing.T) {
	for _, other := range []string{"user", "api key", "group", "task"} {
		t.Run(other, func(t *testing.T) {
			h, _, upstream, _ := newHunyuanActionHandler(t)
			groupID := int64(24)
			require.NoError(t, h.gatewayService.BindHunyuanTaskAccount(context.Background(), &groupID, "task-1", 10, 20, 1))
			c, w := hunyuanActionContext(http.MethodGet, "/v1/videos/task-1", "", service.PlatformHunyuan)
			key, ok := middleware2.GetAPIKeyFromContext(c)
			require.True(t, ok)
			c.Params = gin.Params{{Key: "task_id", Value: "task-1"}}
			switch other {
			case "user":
				c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 11, Concurrency: 5})
			case "api key":
				key.ID = 21
			case "group":
				otherGroup := int64(25)
				key.GroupID = &otherGroup
			case "task":
				c.Params = gin.Params{{Key: "task_id", Value: "task-2"}}
			}
			h.HunyuanVideoStatus(c)
			require.Equal(t, http.StatusNotFound, w.Code, "task_id must never cross tenants (%s mismatch)", other)
			require.Zero(t, upstream.callCount())
		})
	}
}

// TestHunyuanVideoStatusHandlerRewritesProxyAndClaims covers the owner-bound
// status read: the settled vendor URL is rewritten onto the gateway content
// proxy and the one-shot completion claim is attempted.
func TestHunyuanVideoStatusHandlerRewritesProxyAndClaims(t *testing.T) {
	for _, platform := range []string{service.PlatformHunyuan, service.PlatformComposite} {
		t.Run(platform, func(t *testing.T) {
			h, bindings, upstream, _ := newHunyuanActionHandler(t)
			groupID := int64(24)
			require.NoError(t, h.gatewayService.BindHunyuanTaskAccount(context.Background(), &groupID, "task-1", 10, 20, 1))
			require.NoError(t, h.gatewayService.StoreHunyuanTaskPendingBilling(context.Background(), "task-1", 10, 20, service.BuildHunyuanTaskPendingBilling(
				service.HunyuanActionRequestInfo{Endpoint: service.HunyuanEndpointVideoGeneration, Model: "hy-video-v1.5", Resolution: "720p", DurationSeconds: 5},
				"hy-video-v1.5", "hy-video-v1.5", "hy-video-v1.5", "",
			)))
			upstream.call = func(req *http.Request, _ int64) (*http.Response, error) {
				require.Equal(t, "https://tokenhub.tencentmaas.com/v1/wand/hunyuan-video/tasks/task-1", req.URL.String())
				require.Equal(t, http.MethodGet, req.Method)
				require.Equal(t, "Bearer hunyuan-test-key", req.Header.Get("Authorization"))
				return hunyuanActionJSONResponse(`{"task_id":"task-1","status":"succeeded","model":"hy-video-v1.5","videos":[{"url":"https://aigc-output-video.cos.ap-guangzhou.myqcloud.com/out.mp4"}],"tokenhub_usage":{"total_tokens":150000}}`), nil
			}
			c, w := hunyuanActionContext(http.MethodGet, "/v1/videos/task-1", "", platform)
			if platform == service.PlatformComposite {
				c.Request = c.Request.WithContext(service.WithResolvedTargetPlatform(c.Request.Context(), service.PlatformHunyuan))
			}
			c.Params = gin.Params{{Key: "task_id", Value: "task-1"}}
			h.HunyuanVideoStatus(c)
			require.Equal(t, http.StatusOK, w.Code)
			require.Equal(t, "/v1/videos/task-1/content", gjson.GetBytes(w.Body.Bytes(), "videos.0.url").String())
			require.NotContains(t, w.Body.String(), "myqcloud.com")
			require.Equal(t, 1, upstream.callCount())
			bindings.mu.Lock()
			claimCount := len(bindings.billed)
			bindings.mu.Unlock()
			require.Equal(t, 1, claimCount, "a succeeded observation must attempt the one-shot billing claim")
		})
	}
}

// TestHunyuanImageGenerationGroupGate keeps the allow_image_generation switch a
// pure exposure control: a disabled group is rejected before any upstream call.
func TestHunyuanImageGenerationGroupGate(t *testing.T) {
	h, _, upstream, _ := newHunyuanActionHandler(t)
	c, w := hunyuanActionContext(http.MethodPost, "/v1/images/generations", `{"model":"hy-image-v3","prompt":"一只橙色小猫"}`, service.PlatformHunyuan)
	key, ok := middleware2.GetAPIKeyFromContext(c)
	require.True(t, ok)
	key.Group.AllowImageGeneration = false
	h.HunyuanImageGeneration(c)
	require.Equal(t, http.StatusForbidden, w.Code)
	require.Zero(t, upstream.callCount())
}

// TestHunyuanVoiceCloneRequiresModel keeps the model selector mandatory for the
// 音色复刻 action before any routing or upstream traffic.
func TestHunyuanVoiceCloneRequiresModel(t *testing.T) {
	h, _, upstream, _ := newHunyuanActionHandler(t)
	c, w := hunyuanActionContext(http.MethodPost, "/v1/custom-voices", `{"audio_url":"https://cos.example.com/source.wav"}`, service.PlatformHunyuan)
	h.HunyuanVoiceClone(c)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Zero(t, upstream.callCount())
}

// TestHunyuanVideoCompletionStillClaimsBillingOnce proves concurrent status and
// content reads cannot double-bill one completed task.
func TestHunyuanVideoCompletionStillClaimsBillingOnce(t *testing.T) {
	h, bindings, _, _ := newHunyuanActionHandler(t)
	c, _ := hunyuanActionContext(http.MethodGet, "/v1/videos/task-1", "", service.PlatformHunyuan)
	key, ok := middleware2.GetAPIKeyFromContext(c)
	require.True(t, ok)
	subject := middleware2.AuthSubject{UserID: 10, Concurrency: 5}
	result := &service.OpenAIForwardResult{ResponseID: "task-1", Model: "hy-video-v1.5", VideoCount: 1}
	require.NoError(t, h.gatewayService.StoreHunyuanTaskPendingBilling(c.Request.Context(), "task-1", 10, 20, service.BuildHunyuanTaskPendingBilling(
		service.HunyuanActionRequestInfo{Endpoint: service.HunyuanEndpointVideoGeneration, Model: "hy-video-v1.5"},
		"hy-video-v1.5", "hy-video-v1.5", "hy-video-v1.5", "",
	)))
	for i := range 20 {
		bill := h.prepareHunyuanVideoCompletionBilling(c.Request.Context(), zap.NewNop(), key, subject, "task-1", result)
		if i == 0 {
			require.NotNil(t, bill)
			require.Equal(t, service.StableHunyuanVideoBillingRequestID("task-1"), bill.RequestID)
			require.Equal(t, 1, bill.VideoCount)
		} else {
			require.Nil(t, bill, "repeated status/content polls must never double-bill")
		}
	}
	bindings.mu.Lock()
	defer bindings.mu.Unlock()
	require.Len(t, bindings.billed, 1)
}

// TestHunyuanVideoCompletionBillingFailsClosedWithoutSnapshot keeps a completed
// task unbillable when the submit-time pricing evidence is missing instead of
// inventing duration or resolution.
func TestHunyuanVideoCompletionBillingFailsClosedWithoutSnapshot(t *testing.T) {
	h, _, _, _ := newHunyuanActionHandler(t)
	c, _ := hunyuanActionContext(http.MethodGet, "/v1/videos/unknown-task", "", service.PlatformHunyuan)
	key, ok := middleware2.GetAPIKeyFromContext(c)
	require.True(t, ok)
	subject := middleware2.AuthSubject{UserID: 10, Concurrency: 5}
	result := &service.OpenAIForwardResult{ResponseID: "unknown-task", Model: "hy-video-v1.5", VideoCount: 1}

	bill := h.prepareHunyuanVideoCompletionBilling(c.Request.Context(), zap.NewNop(), key, subject, "unknown-task", result)

	require.Nil(t, bill, "no submit-time snapshot means no honest price")
}
