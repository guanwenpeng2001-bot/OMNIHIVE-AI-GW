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

// MiniMax media/voice handler tests. Fixtures mirror the grok media slot tests;
// upstream payloads follow the official MiniMax OpenAPI references (2026-09-26).

type minimaxMediaActionCache struct {
	testutil.StubGatewayCache
	mu       sync.Mutex
	sessions map[string]int64
	pending  map[string][]byte
	billed   map[string]bool
	writes   int
}

func newMinimaxMediaActionCache() *minimaxMediaActionCache {
	return &minimaxMediaActionCache{
		sessions: map[string]int64{},
		pending:  map[string][]byte{},
		billed:   map[string]bool{},
	}
}

func (s *minimaxMediaActionCache) GetSessionAccountID(_ context.Context, groupID int64, key string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	owner, ok := s.sessions[minimaxMediaSessionKey(groupID, key)]
	if !ok {
		return 0, service.ErrStickySessionNotFound
	}
	return owner, nil
}

func (s *minimaxMediaActionCache) SetSessionAccountID(_ context.Context, groupID int64, key string, owner int64, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[minimaxMediaSessionKey(groupID, key)] = owner
	s.writes++
	return nil
}

func (s *minimaxMediaActionCache) SetGrokVideoPendingBilling(_ context.Context, key string, payload []byte, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending[key] = append([]byte(nil), payload...)
	return nil
}

func (s *minimaxMediaActionCache) GetGrokVideoPendingBilling(_ context.Context, key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	payload, ok := s.pending[key]
	if !ok {
		return nil, nil
	}
	return payload, nil
}

func (s *minimaxMediaActionCache) ClaimGrokVideoBilled(_ context.Context, key string, _ time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.billed[key] {
		return false, nil
	}
	s.billed[key] = true
	return true, nil
}

func (s *minimaxMediaActionCache) ReleaseGrokVideoBilled(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.billed, key)
	return nil
}

func minimaxMediaSessionKey(groupID int64, key string) string {
	return fmt.Sprintf("%d|%s", groupID, key)
}

type minimaxMediaActionUpstream struct {
	service.HTTPUpstream
	mu    sync.Mutex
	call  func(*http.Request, int64) (*http.Response, error)
	calls int
}

func (u *minimaxMediaActionUpstream) Do(req *http.Request, _ string, id int64, _ int) (*http.Response, error) {
	u.mu.Lock()
	u.calls++
	call := u.call
	u.mu.Unlock()
	return call(req, id)
}

func (u *minimaxMediaActionUpstream) callCount() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.calls
}

func minimaxMediaJSONResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

// minimaxMediaFixtureAccount attaches catalog evidence for the MiniMax action
// models (mirrors preparedGatewayFixtureAccount but with MiniMax IDs).
func minimaxMediaFixtureAccount(account service.Account) service.Account {
	extra := make(map[string]any, len(account.Extra)+1)
	for key, value := range account.Extra {
		extra[key] = value
	}
	models := []string{"MiniMax-M2", "MiniMax-H3", "MiniMax-H3-Max", "image-01", "speech-2.8-hd", "speech-02-hd", "asr-1.0"}
	catalog := service.AccountModelCatalogSnapshot{Complete: true, Revision: 1, DiscoveredModels: models, EligibleModels: []service.AccountCatalogModel{}}
	for _, model := range models {
		catalog.EligibleModels = append(catalog.EligibleModels, service.AccountCatalogModel{ID: model, Endpoints: []string{"responses", "chat_completions", "messages", "images"}, EvidenceKind: "fixture", EvidenceObservedAt: "2026-09-26T00:00:00Z"})
	}
	extra[service.UpstreamModelCatalogExtraKey] = catalog
	account.Extra = extra
	return account
}

func newMiniMaxMediaActionHandler(t *testing.T) (*OpenAIGatewayHandler, *minimaxMediaActionCache, *minimaxMediaActionUpstream, []service.Account) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	accounts := make([]service.Account, 2)
	for i := range accounts {
		accounts[i] = service.Account{
			ID: int64(i + 1), Platform: service.PlatformMiniMax, Type: service.AccountTypeAPIKey,
			Status: service.StatusActive, Schedulable: true, Concurrency: 50, Priority: i,
			GroupIDs:    []int64{24},
			Credentials: map[string]any{"api_key": "minimax-test-key", "base_url": "https://api.minimax.io/v1"},
		}
	}
	for index := range accounts {
		accounts[index] = minimaxMediaFixtureAccount(accounts[index])
	}
	concurrency := service.NewConcurrencyService(&grokMediaSlotsCache{accounts: map[string]int64{}, users: map[string]int64{}})
	bindings := newMinimaxMediaActionCache()
	upstream := &minimaxMediaActionUpstream{call: func(*http.Request, int64) (*http.Response, error) {
		return minimaxMediaJSONResponse(`{"task_id":"task-1"}`), nil
	}}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Gateway.Scheduling.StickySessionWaitTimeout = 20 * time.Millisecond
	cfg.Gateway.Scheduling.StickySessionMaxWaiting = 3
	repo := openAIImagesFailoverAccountRepo{accounts: accounts}
	gateway := service.NewOpenAIGatewayService(repo, nil, nil, nil, nil, nil, bindings, cfg, nil, concurrency, nil, nil, nil, upstream, nil, nil, nil, nil, nil, nil, nil, nil)
	billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billing.Stop)
	handler := NewOpenAIGatewayHandler(gateway, concurrency, billing, service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, cfg)
	return handler, bindings, upstream, accounts
}

func minimaxMediaActionContext(method, path, body, platform string) (*gin.Context, *httptest.ResponseRecorder) {
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

func TestMiniMaxVideoLookupOwnerIsolation(t *testing.T) {
	for _, other := range []string{"user", "api key", "group", "task"} {
		t.Run(other, func(t *testing.T) {
			h, bindings, upstream, _ := newMiniMaxMediaActionHandler(t)
			groupID := int64(24)
			require.NoError(t, h.gatewayService.BindMiniMaxVideoRequestAccount(context.Background(), &groupID, "task-1", 10, 20, 1))
			c, w := minimaxMediaActionContext(http.MethodGet, "/v1/videos/task-1", "", service.PlatformMiniMax)
			key, ok := middleware2.GetAPIKeyFromContext(c)
			require.True(t, ok)
			c.Params = gin.Params{{Key: "request_id", Value: "task-1"}}
			switch other {
			case "user":
				c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 11, Concurrency: 5})
			case "api key":
				key.ID = 21
			case "group":
				otherGroup := int64(25)
				key.GroupID = &otherGroup
			case "task":
				c.Params = gin.Params{{Key: "request_id", Value: "task-2"}}
			}
			h.MiniMaxVideoStatus(c)
			require.Equal(t, http.StatusNotFound, w.Code, "task_id must never cross tenants (%s mismatch)", other)
			require.Zero(t, upstream.callCount())
			_ = bindings
		})
	}
}

func TestMiniMaxVideoCompletionStillClaimsBillingOnce(t *testing.T) {
	h, bindings, _, _ := newMiniMaxMediaActionHandler(t)
	c, _ := minimaxMediaActionContext(http.MethodGet, "/v1/videos/task-1", "", service.PlatformMiniMax)
	key, ok := middleware2.GetAPIKeyFromContext(c)
	require.True(t, ok)
	subject := middleware2.AuthSubject{UserID: 10, Concurrency: 5}
	result := &service.OpenAIForwardResult{ResponseID: "task-1", Model: "MiniMax-H3",
		VideoCount: 1, VideoDurationSeconds: 5, VideoResolution: service.VideoBillingResolution720P}
	for i := range 20 {
		bill := h.prepareMiniMaxVideoCompletionBilling(c.Request.Context(), zap.NewNop(), key, subject, "task-1", result)
		if i == 0 {
			require.NotNil(t, bill)
			require.Equal(t, service.StableMiniMaxVideoBillingRequestID("task-1"), bill.RequestID)
			require.Equal(t, 1, bill.VideoCount)
		} else {
			require.Nil(t, bill, "repeated status/content polls must never double-bill")
		}
	}
	bindings.mu.Lock()
	defer bindings.mu.Unlock()
	require.Len(t, bindings.billed, 1)
}

// TestMiniMaxGenerationPathsGatedUntilActionMatrixWiring documents the
// integration seam: catalog-based account eligibility consults
// domain.PlatformAction, whose MiniMax media cells are registered by
// multimodal-ingress (this branch intentionally does not touch the domain
// action matrix). Until then, generation paths fail closed with 503
// no_eligible_account. Positive upstream-shape coverage for these exact paths
// lives in backend/internal/service/minimax_actions_test.go
// (TestForwardMiniMaxMediaVideoCreateReturnsTaskID,
// TestForwardMiniMaxMediaImageGenerationReachesUpstream,
// TestForwardMiniMaxVoiceTTSReachesUpstream); the binding/pending/claim
// lifecycle is covered by TestMinimaxVideoOwnershipAndPendingBillingLifecycle.
// multimodal-ingress: flip these to the full positive assertions when wiring.
func TestMiniMaxGenerationPathsGatedUntilActionMatrixWiring(t *testing.T) {
	upstreamOK := func(req *http.Request, id int64) (*http.Response, error) {
		return minimaxMediaJSONResponse(`{"task_id":"task-xyz"}`), nil
	}
	scenarios := []struct {
		name     string
		method   string
		path     string
		body     string
		platform string
		invoke   func(*OpenAIGatewayHandler, *gin.Context)
	}{
		{
			name: "video create", method: http.MethodPost, path: "/v1/videos/generations",
			body:     `{"model":"MiniMax-H3","prompt":"sea","resolution":"720p","duration":5,"aspect_ratio":"16:9"}`,
			platform: service.PlatformMiniMax,
			invoke:   func(h *OpenAIGatewayHandler, c *gin.Context) { h.MiniMaxVideoGeneration(c) },
		},
		{
			name: "image generate", method: http.MethodPost, path: "/v1/images/generations",
			body:     `{"model":"image-01","prompt":"a red panda"}`,
			platform: service.PlatformMiniMax,
			invoke:   func(h *OpenAIGatewayHandler, c *gin.Context) { h.MiniMaxImages(c) },
		},
		{
			name: "tts", method: http.MethodPost, path: "/v1/audio/speech",
			body:     `{"model":"speech-2.8-hd","input":"hello","voice":"English_Graceful_Lady"}`,
			platform: service.PlatformMiniMax,
			invoke:   func(h *OpenAIGatewayHandler, c *gin.Context) { h.MiniMaxVoice(c, "tts") },
		},
		{
			name: "composite video create", method: http.MethodPost, path: "/v1/videos/generations",
			body:     `{"model":"MiniMax-H3","prompt":"composite video","resolution":"768P","duration":5,"aspect_ratio":"16:9"}`,
			platform: service.PlatformComposite,
			invoke:   func(h *OpenAIGatewayHandler, c *gin.Context) { h.MiniMaxVideoGeneration(c) },
		},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			h, _, upstream, _ := newMiniMaxMediaActionHandler(t)
			upstream.call = upstreamOK
			c, w := minimaxMediaActionContext(scenario.method, scenario.path, scenario.body, scenario.platform)
			if scenario.platform == service.PlatformComposite {
				c.Request = c.Request.WithContext(service.WithResolvedTargetPlatform(c.Request.Context(), service.PlatformMiniMax))
			}
			scenario.invoke(h, c)
			require.Equal(t, http.StatusServiceUnavailable, w.Code)
			require.Zero(t, upstream.callCount(), "the admission gate must fail closed before any upstream call")
		})
	}
}

func TestMiniMaxVideoStatusHandlerRewritesProxyAndClaims(t *testing.T) {
	h, bindings, upstream, _ := newMiniMaxMediaActionHandler(t)
	groupID := int64(24)
	require.NoError(t, h.gatewayService.BindMiniMaxVideoRequestAccount(context.Background(), &groupID, "task-1", 10, 20, 1))
	upstream.call = func(req *http.Request, id int64) (*http.Response, error) {
		require.Equal(t, "https://api.minimax.io/v2/query/video_generation/task-1", req.URL.String())
		require.Equal(t, "Bearer minimax-test-key", req.Header.Get("Authorization"))
		return minimaxMediaJSONResponse(`{"task":{"id":"task-1","model":"MiniMax-H3","status":"succeeded","created_at":1785125529,"updated_at":1785125946,"content":{"url":"https://video-product.cdn.minimax.io/out.mp4"},"resolution":"768P","duration":5,"usage":{"total_seconds":5,"output_seconds":5},"ratio":"16:9","task_type":"generation","modality":"video"}}`), nil
	}
	c, w := minimaxMediaActionContext(http.MethodGet, "/v1/videos/task-1", "", service.PlatformMiniMax)
	c.Params = gin.Params{{Key: "request_id", Value: "task-1"}}
	h.MiniMaxVideoStatus(c)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "/v1/videos/task-1/content", gjson.GetBytes(w.Body.Bytes(), "task.content.url").String())
	require.Equal(t, "succeeded", gjson.GetBytes(w.Body.Bytes(), "task.status").String())
	require.Equal(t, 1, upstream.callCount())
	bindings.mu.Lock()
	claimCount := len(bindings.billed)
	bindings.mu.Unlock()
	require.GreaterOrEqual(t, claimCount, 1, "a succeeded observation must attempt the one-shot billing claim")
}

func TestMiniMaxVideoCancelHandlerPath(t *testing.T) {
	h, _, upstream, _ := newMiniMaxMediaActionHandler(t)
	groupID := int64(24)
	require.NoError(t, h.gatewayService.BindMiniMaxVideoRequestAccount(context.Background(), &groupID, "task-1", 10, 20, 1))
	upstream.call = func(req *http.Request, id int64) (*http.Response, error) {
		require.Equal(t, http.MethodDelete, req.Method)
		require.Equal(t, "https://api.minimax.io/v2/video_generation/task-1", req.URL.String())
		return minimaxMediaJSONResponse(`{"task_id":"task-1","action":"cancelled","status":"cancelled"}`), nil
	}
	c, w := minimaxMediaActionContext(http.MethodDelete, "/v1/videos/task-1", "", service.PlatformMiniMax)
	c.Params = gin.Params{{Key: "request_id", Value: "task-1"}}
	h.MiniMaxVideoCancel(c)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "cancelled", gjson.GetBytes(w.Body.Bytes(), "action").String())
}

func TestMiniMaxImageGenerationGroupGate(t *testing.T) {
	h, _, upstream, _ := newMiniMaxMediaActionHandler(t)
	body := `{"model":"image-01","prompt":"a red panda"}`
	c, w := minimaxMediaActionContext(http.MethodPost, "/v1/images/generations", body, service.PlatformMiniMax)
	key, ok := middleware2.GetAPIKeyFromContext(c)
	require.True(t, ok)
	key.Group.AllowImageGeneration = false
	h.MiniMaxImages(c)
	require.Equal(t, http.StatusForbidden, w.Code, "the allow_image_generation exposure switch still gates generation")
	require.Zero(t, upstream.callCount())
}
