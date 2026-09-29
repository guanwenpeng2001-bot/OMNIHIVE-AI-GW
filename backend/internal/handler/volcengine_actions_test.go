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

// Volcengine Ark media/embedding handler tests. Fixtures mirror the grok/minimax
// media slot tests; upstream payloads follow the frozen Ark contract plus the
// official API references recorded in the captured model-list page (2026-09-20).

type volcengineMediaActionCache struct {
	testutil.StubGatewayCache
	mu       sync.Mutex
	sessions map[string]int64
	pending  map[string][]byte
	billed   map[string]bool
}

func newVolcengineMediaActionCache() *volcengineMediaActionCache {
	return &volcengineMediaActionCache{
		sessions: map[string]int64{},
		pending:  map[string][]byte{},
		billed:   map[string]bool{},
	}
}

func (s *volcengineMediaActionCache) GetSessionAccountID(_ context.Context, groupID int64, key string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	owner, ok := s.sessions[volcengineMediaSessionKey(groupID, key)]
	if !ok {
		return 0, service.ErrStickySessionNotFound
	}
	return owner, nil
}

func (s *volcengineMediaActionCache) SetSessionAccountID(_ context.Context, groupID int64, key string, owner int64, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[volcengineMediaSessionKey(groupID, key)] = owner
	return nil
}

func (s *volcengineMediaActionCache) SetGrokVideoPendingBilling(_ context.Context, key string, payload []byte, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending[key] = append([]byte(nil), payload...)
	return nil
}

func (s *volcengineMediaActionCache) GetGrokVideoPendingBilling(_ context.Context, key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	payload, ok := s.pending[key]
	if !ok {
		return nil, nil
	}
	return payload, nil
}

func (s *volcengineMediaActionCache) ClaimGrokVideoBilled(_ context.Context, key string, _ time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.billed[key] {
		return false, nil
	}
	s.billed[key] = true
	return true, nil
}

func (s *volcengineMediaActionCache) ReleaseGrokVideoBilled(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.billed, key)
	return nil
}

func volcengineMediaSessionKey(groupID int64, key string) string {
	return fmt.Sprintf("%d|%s", groupID, key)
}

// volcengineMediaActionAccountRepo is the account repository fixture. It adds
// ListModelAvailabilityCandidates, which the no-account classifier queries for
// non-generation endpoints (embeddings); without it the embedded nil interface
// panics and the failure path degrades into a broken empty 200 response.
type volcengineMediaActionAccountRepo struct {
	service.AccountRepository
	accounts []service.Account
}

func (r volcengineMediaActionAccountRepo) GetByID(_ context.Context, id int64) (*service.Account, error) {
	for index := range r.accounts {
		if r.accounts[index].ID == id {
			account := r.accounts[index]
			return &account, nil
		}
	}
	return nil, service.ErrNoAvailableAccounts
}

func (r volcengineMediaActionAccountRepo) ListSchedulableByGroupIDAndPlatform(_ context.Context, _ int64, platform string) ([]service.Account, error) {
	return r.accountsForPlatform(platform), nil
}

func (r volcengineMediaActionAccountRepo) ListSchedulableByPlatform(_ context.Context, platform string) ([]service.Account, error) {
	return r.accountsForPlatform(platform), nil
}

func (r volcengineMediaActionAccountRepo) ListSchedulableUngroupedByPlatform(_ context.Context, platform string) ([]service.Account, error) {
	return r.accountsForPlatform(platform), nil
}

func (r volcengineMediaActionAccountRepo) ListModelAvailabilityCandidates(_ context.Context, _ *int64, platforms []string, _ bool) ([]service.Account, error) {
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

func (r volcengineMediaActionAccountRepo) accountsForPlatform(platform string) []service.Account {
	out := make([]service.Account, 0, len(r.accounts))
	for _, account := range r.accounts {
		if account.Platform == platform {
			out = append(out, account)
		}
	}
	return out
}

type volcengineMediaActionUpstream struct {
	service.HTTPUpstream
	mu    sync.Mutex
	call  func(*http.Request, int64) (*http.Response, error)
	calls int
}

func (u *volcengineMediaActionUpstream) Do(req *http.Request, _ string, id int64, _ int) (*http.Response, error) {
	u.mu.Lock()
	u.calls++
	call := u.call
	u.mu.Unlock()
	return call(req, id)
}

func (u *volcengineMediaActionUpstream) callCount() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.calls
}

func volcengineMediaJSONResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

// volcengineMediaFixtureAccount attaches catalog evidence for the Volcengine
// action models (mirrors preparedGatewayFixtureAccount but with Ark IDs).
func volcengineMediaFixtureAccount(account service.Account) service.Account {
	extra := make(map[string]any, len(account.Extra)+1)
	for key, value := range account.Extra {
		extra[key] = value
	}
	models := []string{"doubao-seed-2-0-260128", "doubao-seedream-4-0-250828", "doubao-seedance-2-0-260128", "doubao-embedding-vision-250615"}
	catalog := service.AccountModelCatalogSnapshot{Complete: true, Revision: 1, DiscoveredModels: models, EligibleModels: []service.AccountCatalogModel{}}
	for _, model := range models {
		catalog.EligibleModels = append(catalog.EligibleModels, service.AccountCatalogModel{ID: model, Endpoints: []string{"chat_completions", "responses", "images", "embeddings"}, EvidenceKind: "fixture", EvidenceObservedAt: "2026-09-20T14:30:00Z"})
	}
	extra[service.UpstreamModelCatalogExtraKey] = catalog
	account.Extra = extra
	return account
}

func newVolcengineMediaActionHandler(t *testing.T) (*OpenAIGatewayHandler, *volcengineMediaActionCache, *volcengineMediaActionUpstream, []service.Account) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	accounts := make([]service.Account, 2)
	for i := range accounts {
		accounts[i] = service.Account{
			ID: int64(i + 1), Platform: service.PlatformVolcengine, Type: service.AccountTypeAPIKey,
			Status: service.StatusActive, Schedulable: true, Concurrency: 50, Priority: i,
			GroupIDs:    []int64{24},
			Credentials: map[string]any{"api_key": "ark-test-key", "base_url": "https://ark.cn-beijing.volces.com/api/v3"},
		}
	}
	for index := range accounts {
		accounts[index] = volcengineMediaFixtureAccount(accounts[index])
	}
	concurrency := service.NewConcurrencyService(&grokMediaSlotsCache{accounts: map[string]int64{}, users: map[string]int64{}})
	bindings := newVolcengineMediaActionCache()
	upstream := &volcengineMediaActionUpstream{call: func(*http.Request, int64) (*http.Response, error) {
		return volcengineMediaJSONResponse(`{"id":"cgt-1"}`), nil
	}}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Gateway.Scheduling.StickySessionWaitTimeout = 20 * time.Millisecond
	cfg.Gateway.Scheduling.StickySessionMaxWaiting = 3
	repo := volcengineMediaActionAccountRepo{accounts: accounts}
	gateway := service.NewOpenAIGatewayService(repo, nil, nil, nil, nil, nil, bindings, cfg, nil, concurrency, nil, nil, nil, upstream, nil, nil, nil, nil, nil, nil, nil, nil)
	billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billing.Stop)
	handler := NewOpenAIGatewayHandler(gateway, concurrency, billing, service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, cfg)
	return handler, bindings, upstream, accounts
}

func volcengineMediaActionContext(method, path, body, platform string) (*gin.Context, *httptest.ResponseRecorder) {
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

func TestVolcengineVideoLookupOwnerIsolation(t *testing.T) {
	for _, other := range []string{"user", "api key", "group", "task"} {
		t.Run(other, func(t *testing.T) {
			h, _, upstream, _ := newVolcengineMediaActionHandler(t)
			groupID := int64(24)
			require.NoError(t, h.gatewayService.BindVolcengineVideoRequestAccount(context.Background(), &groupID, "task-1", 10, 20, 1))
			c, w := volcengineMediaActionContext(http.MethodGet, "/v1/videos/task-1", "", service.PlatformVolcengine)
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
			h.VolcengineVideoStatus(c)
			require.Equal(t, http.StatusNotFound, w.Code, "task_id must never cross tenants (%s mismatch)", other)
			require.Zero(t, upstream.callCount())
		})
	}
}

func TestVolcengineVideoCompletionStillClaimsBillingOnce(t *testing.T) {
	h, bindings, _, _ := newVolcengineMediaActionHandler(t)
	c, _ := volcengineMediaActionContext(http.MethodGet, "/v1/videos/task-1", "", service.PlatformVolcengine)
	key, ok := middleware2.GetAPIKeyFromContext(c)
	require.True(t, ok)
	subject := middleware2.AuthSubject{UserID: 10, Concurrency: 5}
	// The create-time snapshot supplies the model/resolution/duration that a
	// sparse task body may omit, and the create-accept timestamp for E2E duration.
	require.NoError(t, h.gatewayService.StoreVolcengineVideoPendingBilling(c.Request.Context(), "task-1", 10, 20, service.VolcengineVideoPendingBilling{
		Model: "doubao-seedance-2-0-260128", BillingModel: "doubao-seedance-2-0-260128",
		VideoResolution: service.VideoBillingResolution720P, VideoDurationSeconds: 5,
		CreatedAt: time.Now().Add(-30 * time.Second).UTC().Format(time.RFC3339Nano),
	}))
	result := &service.OpenAIForwardResult{ResponseID: "task-1", Model: "doubao-seedance-2-0-260128",
		VideoCount: 1, VideoDurationSeconds: 5, VideoResolution: service.VideoBillingResolution720P}
	for i := range 20 {
		bill := h.prepareVolcengineVideoCompletionBilling(c.Request.Context(), zap.NewNop(), key, subject, "task-1", result)
		if i == 0 {
			require.NotNil(t, bill)
			require.Equal(t, service.StableVolcengineVideoBillingRequestID("task-1"), bill.RequestID)
			require.Equal(t, 1, bill.VideoCount)
			require.Equal(t, service.VideoBillingResolution720P, bill.VideoResolution)
			require.Equal(t, 5, bill.VideoDurationSeconds)
			require.Greater(t, bill.Duration, 25*time.Second,
				"the create-accept timestamp must produce the E2E duration recorded for the job")
		} else {
			require.Nil(t, bill, "repeated status/content polls must never double-bill")
		}
	}
	bindings.mu.Lock()
	defer bindings.mu.Unlock()
	require.Len(t, bindings.billed, 1)
}

// TestVolcengineMediaGenerationPathsFailClosedOnPlatformCapabilityLock records
// the one remaining integration seam with the exact gate, instead of leaving the
// adapter unreachable for an unexplained reason.
//
// The Ark adapters exist and are wire-tested, and
// internal/domain/volcengine_actions.go records the eight media/embedding cells
// in volcenginePendingAdapters (adapter bound, cell still unverified), because a
// Volcengine account cannot execute them while the platform descriptor keeps
// ChatOnly=true. That single boolean locks three independent layers:
//   - service/account.go SupportsOpenAIEndpointCapability: a chat-only platform
//     supports only chat_completions for any account;
//   - service/account_model_catalog.go accountCatalogModelEndpoints: a chat-only
//     platform publishes only chat_completions endpoint evidence, so the catalog
//     binding for the media/embedding endpoint never resolves;
//   - service/gateway_forward_model_binding.go requestCatalogEndpoint: the media
//     request plan requires the grok_media_generation catalog endpoint.
//
// Flipping the descriptor is not a safe local edit. An experimental flip of this
// row (ChatOnly=false + ResponsesPOST=EndpointNative, reverted) made the domain
// matrix disagree with the routes invariant (messages became implemented), left
// these media paths at 503 because the media catalog endpoint mapping still does
// not resolve, and turned the currently working status/cancel lookups from 200
// into 404. The descriptor row, the catalog endpoint mapping and the ingress
// registration must therefore change together in the integration branch. The
// adapter's real upstream shape (URL, method, Authorization header, body) is
// proven in backend/internal/service/volcengine_actions_test.go.
func TestVolcengineMediaGenerationPathsFailClosedOnPlatformCapabilityLock(t *testing.T) {
	scenarios := []struct {
		name     string
		method   string
		path     string
		body     string
		platform string
		invoke   func(*OpenAIGatewayHandler, *gin.Context)
	}{
		{
			name: "image generate", method: http.MethodPost, path: "/v1/images/generations",
			body:     `{"model":"doubao-seedream-4-0-250828","prompt":"a red panda","size":"2K"}`,
			platform: service.PlatformVolcengine,
			invoke:   func(h *OpenAIGatewayHandler, c *gin.Context) { h.VolcengineImages(c) },
		},
		{
			name: "image edit", method: http.MethodPost, path: "/v1/images/edits",
			body:     `{"model":"doubao-seedream-4-0-250828","prompt":"turn the sky green","image":"https://cdn.example/in.png"}`,
			platform: service.PlatformVolcengine,
			invoke:   func(h *OpenAIGatewayHandler, c *gin.Context) { h.VolcengineImages(c) },
		},
		{
			name: "video create", method: http.MethodPost, path: "/v1/videos/generations",
			body:     `{"model":"doubao-seedance-2-0-260128","prompt":"sea","resolution":"720p","duration":5,"aspect_ratio":"16:9"}`,
			platform: service.PlatformVolcengine,
			invoke:   func(h *OpenAIGatewayHandler, c *gin.Context) { h.VolcengineVideoGeneration(c) },
		},
		{
			name: "video edit", method: http.MethodPost, path: "/v1/videos/edits",
			body:     `{"model":"doubao-seedance-2-0-260128","prompt":"change the weather","video":"https://cdn.example/in.mp4"}`,
			platform: service.PlatformVolcengine,
			invoke:   func(h *OpenAIGatewayHandler, c *gin.Context) { h.VolcengineVideoEdit(c) },
		},
		{
			name: "video extend", method: http.MethodPost, path: "/v1/videos/extensions",
			body:     `{"model":"doubao-seedance-2-0-260128","prompt":"keep going","video":"https://cdn.example/in.mp4"}`,
			platform: service.PlatformVolcengine,
			invoke:   func(h *OpenAIGatewayHandler, c *gin.Context) { h.VolcengineVideoExtend(c) },
		},
		{
			name: "embeddings", method: http.MethodPost, path: "/v1/embeddings",
			body:     `{"model":"doubao-embedding-vision-250615","input":["hello"]}`,
			platform: service.PlatformVolcengine,
			invoke:   func(h *OpenAIGatewayHandler, c *gin.Context) { h.VolcengineEmbeddings(c) },
		},
		{
			name: "composite target video create", method: http.MethodPost, path: "/v1/videos/generations",
			body:     `{"model":"doubao-seedance-2-0-260128","prompt":"composite video","resolution":"1080p","duration":5,"aspect_ratio":"16:9"}`,
			platform: service.PlatformComposite,
			invoke:   func(h *OpenAIGatewayHandler, c *gin.Context) { h.VolcengineVideoGeneration(c) },
		},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			h, _, upstream, _ := newVolcengineMediaActionHandler(t)
			upstream.call = func(*http.Request, int64) (*http.Response, error) {
				return volcengineMediaJSONResponse(`{"id":"cgt-xyz"}`), nil
			}
			c, w := volcengineMediaActionContext(scenario.method, scenario.path, scenario.body, scenario.platform)
			if scenario.platform == service.PlatformComposite {
				c.Request = c.Request.WithContext(service.WithResolvedTargetPlatform(c.Request.Context(), service.PlatformVolcengine))
			}
			scenario.invoke(h, c)
			require.Equal(t, http.StatusServiceUnavailable, w.Code, "body=%s", w.Body.String())
			if scenario.platform == service.PlatformVolcengine {
				require.Contains(t, w.Body.String(), "error",
					"the failure must be reported as an error body, never as an empty 200")
			}
			require.Zero(t, upstream.callCount(), "the lock must fail closed before any upstream call")
		})
	}
}

func TestVolcengineMediaMissingModelRejected(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		path   string
		body   string
		invoke func(*OpenAIGatewayHandler, *gin.Context)
	}{
		{"image generate", "/v1/images/generations", `{"prompt":"p"}`, func(h *OpenAIGatewayHandler, c *gin.Context) { h.VolcengineImages(c) }},
		{"video create", "/v1/videos/generations", `{"prompt":"p"}`, func(h *OpenAIGatewayHandler, c *gin.Context) { h.VolcengineVideoGeneration(c) }},
		{"embeddings", "/v1/embeddings", `{"input":["x"]}`, func(h *OpenAIGatewayHandler, c *gin.Context) { h.VolcengineEmbeddings(c) }},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			h, _, upstream, _ := newVolcengineMediaActionHandler(t)
			c, w := volcengineMediaActionContext(http.MethodPost, scenario.path, scenario.body, service.PlatformVolcengine)
			scenario.invoke(h, c)
			require.Equal(t, http.StatusBadRequest, w.Code)
			require.Contains(t, w.Body.String(), "model is required")
			require.Zero(t, upstream.callCount())
		})
	}
}

func TestVolcengineVideoContentAndCancelOwnerIsolation(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		path   string
		invoke func(*OpenAIGatewayHandler, *gin.Context)
	}{
		{"content", "/v1/videos/task-1/content", func(h *OpenAIGatewayHandler, c *gin.Context) { h.VolcengineVideoContent(c) }},
		{"cancel", "/v1/videos/task-1", func(h *OpenAIGatewayHandler, c *gin.Context) { h.VolcengineVideoCancel(c) }},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			h, _, upstream, _ := newVolcengineMediaActionHandler(t)
			groupID := int64(24)
			require.NoError(t, h.gatewayService.BindVolcengineVideoRequestAccount(context.Background(), &groupID, "task-1", 10, 20, 1))
			c, w := volcengineMediaActionContext(http.MethodGet, scenario.path, "", service.PlatformVolcengine)
			c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 11, Concurrency: 5})
			c.Params = gin.Params{{Key: "request_id", Value: "task-1"}}
			scenario.invoke(h, c)
			require.Equal(t, http.StatusNotFound, w.Code, "another tenant must not reach the task through %s", scenario.name)
			require.Zero(t, upstream.callCount())
		})
	}
}

func TestVolcengineVideoStatusHandlerRewritesProxyAndClaims(t *testing.T) {
	h, bindings, upstream, _ := newVolcengineMediaActionHandler(t)
	groupID := int64(24)
	require.NoError(t, h.gatewayService.BindVolcengineVideoRequestAccount(context.Background(), &groupID, "task-1", 10, 20, 1))
	upstream.call = func(req *http.Request, id int64) (*http.Response, error) {
		require.Equal(t, "https://ark.cn-beijing.volces.com/api/v3/contents/generations/tasks/task-1", req.URL.String())
		require.Equal(t, "Bearer ark-test-key", req.Header.Get("Authorization"))
		return volcengineMediaJSONResponse(`{"id":"task-1","model":"doubao-seedance-2-0-260128","status":"succeeded","content":{"video_url":"https://ark-content.volces.com/out.mp4"},"usage":{"total_tokens":123},"created_at":1785125529,"updated_at":1785125946,"resolution":"720p","duration":5,"ratio":"16:9"}`), nil
	}
	c, w := volcengineMediaActionContext(http.MethodGet, "/v1/videos/task-1", "", service.PlatformVolcengine)
	c.Params = gin.Params{{Key: "request_id", Value: "task-1"}}
	h.VolcengineVideoStatus(c)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "/v1/videos/task-1/content", gjson.GetBytes(w.Body.Bytes(), "content.video_url").String())
	require.Equal(t, "succeeded", gjson.GetBytes(w.Body.Bytes(), "status").String())
	require.Equal(t, 1, upstream.callCount())
	bindings.mu.Lock()
	claimCount := len(bindings.billed)
	bindings.mu.Unlock()
	require.GreaterOrEqual(t, claimCount, 1, "a succeeded observation must attempt the one-shot billing claim")
}

func TestVolcengineVideoContentHandlerStreamsOwnerVideo(t *testing.T) {
	h, bindings, upstream, _ := newVolcengineMediaActionHandler(t)
	groupID := int64(24)
	require.NoError(t, h.gatewayService.BindVolcengineVideoRequestAccount(context.Background(), &groupID, "task-1", 10, 20, 1))
	var captured []*http.Request
	upstream.call = func(req *http.Request, id int64) (*http.Response, error) {
		captured = append(captured, req)
		if len(captured) == 1 {
			return volcengineMediaJSONResponse(`{"id":"task-1","model":"doubao-seedance-2-0-260128","status":"succeeded","content":{"video_url":"https://ark-content.volces.com/out.mp4"},"usage":{"total_tokens":123},"created_at":1785125529,"updated_at":1785125946,"resolution":"720p","duration":5,"ratio":"16:9"}`), nil
		}
		return &http.Response{
			StatusCode: http.StatusPartialContent,
			Header: http.Header{
				"Content-Type":   []string{"video/mp4"},
				"Content-Length": []string{"13"},
				"Content-Range":  []string{"bytes 0-12/100"},
				"Accept-Ranges":  []string{"bytes"},
			},
			Body: io.NopCloser(strings.NewReader("video-payload")),
		}, nil
	}
	c, w := volcengineMediaActionContext(http.MethodGet, "/v1/videos/task-1/content", "", service.PlatformVolcengine)
	c.Request.Header.Set("Range", "bytes=0-12")
	c.Params = gin.Params{{Key: "request_id", Value: "task-1"}}
	h.VolcengineVideoContent(c)

	require.Equal(t, http.StatusPartialContent, w.Code)
	require.Equal(t, "video-payload", w.Body.String())
	require.Equal(t, "bytes 0-12/100", w.Header().Get("Content-Range"))
	require.Len(t, captured, 2)
	require.Equal(t, "https://ark.cn-beijing.volces.com/api/v3/contents/generations/tasks/task-1", captured[0].URL.String())
	require.Equal(t, "https://ark-content.volces.com/out.mp4", captured[1].URL.String())
	require.Empty(t, captured[1].Header.Get("Authorization"), "signed CDN downloads must not leak the account credential")
	require.Equal(t, "bytes=0-12", captured[1].Header.Get("Range"))
	bindings.mu.Lock()
	claimCount := len(bindings.billed)
	bindings.mu.Unlock()
	require.GreaterOrEqual(t, claimCount, 1, "a succeeded observation through the content path must attempt the one-shot billing claim")
}

func TestVolcengineVideoCancelHandlerPath(t *testing.T) {
	h, _, upstream, _ := newVolcengineMediaActionHandler(t)
	groupID := int64(24)
	require.NoError(t, h.gatewayService.BindVolcengineVideoRequestAccount(context.Background(), &groupID, "task-1", 10, 20, 1))
	upstream.call = func(req *http.Request, id int64) (*http.Response, error) {
		require.Equal(t, http.MethodDelete, req.Method)
		require.Equal(t, "https://ark.cn-beijing.volces.com/api/v3/contents/generations/tasks/task-1", req.URL.String())
		return volcengineMediaJSONResponse(`{"id":"task-1","status":"cancelled"}`), nil
	}
	c, w := volcengineMediaActionContext(http.MethodDelete, "/v1/videos/task-1", "", service.PlatformVolcengine)
	c.Params = gin.Params{{Key: "request_id", Value: "task-1"}}
	h.VolcengineVideoCancel(c)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "cancelled", gjson.GetBytes(w.Body.Bytes(), "status").String())
}

func TestVolcengineImageGenerationGroupGate(t *testing.T) {
	h, _, upstream, _ := newVolcengineMediaActionHandler(t)
	body := `{"model":"doubao-seedream-4-0-250828","prompt":"a red panda"}`
	c, w := volcengineMediaActionContext(http.MethodPost, "/v1/images/generations", body, service.PlatformVolcengine)
	key, ok := middleware2.GetAPIKeyFromContext(c)
	require.True(t, ok)
	key.Group.AllowImageGeneration = false
	h.VolcengineImages(c)
	require.Equal(t, http.StatusForbidden, w.Code, "the allow_image_generation exposure switch still gates generation")
	require.Zero(t, upstream.callCount())
}
