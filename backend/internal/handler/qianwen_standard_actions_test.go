//go:build unit

package handler

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type qianwenStandardAccountRepo struct {
	service.AccountRepository
	accounts []service.Account
}

func (r *qianwenStandardAccountRepo) GetByID(_ context.Context, id int64) (*service.Account, error) {
	for i := range r.accounts {
		if r.accounts[i].ID == id {
			account := r.accounts[i]
			return &account, nil
		}
	}
	return nil, service.ErrNoAvailableAccounts
}

func (r *qianwenStandardAccountRepo) ListSchedulableByGroupIDAndPlatform(_ context.Context, _ int64, platform string) ([]service.Account, error) {
	return r.byPlatform(platform), nil
}

func (r *qianwenStandardAccountRepo) ListSchedulableByPlatform(_ context.Context, platform string) ([]service.Account, error) {
	return r.byPlatform(platform), nil
}

func (r *qianwenStandardAccountRepo) ListSchedulableUngroupedByPlatform(_ context.Context, platform string) ([]service.Account, error) {
	return r.byPlatform(platform), nil
}

func (r *qianwenStandardAccountRepo) byPlatform(platform string) []service.Account {
	var out []service.Account
	for _, account := range r.accounts {
		if account.Platform == platform {
			out = append(out, account)
		}
	}
	return out
}

func qianwenCatalogAccount(id int64, model string, endpoints ...string) service.Account {
	return service.Account{
		ID: id, Platform: service.PlatformQianwen, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true,
		Credentials: map[string]any{"api_key": "sk-sp-test"},
		Extra: map[string]any{service.UpstreamModelCatalogExtraKey: service.AccountModelCatalogSnapshot{
			Complete:         true,
			Revision:         1,
			DiscoveredModels: []string{model},
			EligibleModels:   []service.AccountCatalogModel{{ID: model, Endpoints: endpoints, EvidenceKind: "fixture", EvidenceObservedAt: "2026-09-22T00:00:00Z"}},
		}},
	}
}

func newQianwenStandardTestHandler(t *testing.T, upstream service.HTTPUpstream, accounts []service.Account) *OpenAIGatewayHandler {
	t.Helper()
	cfg := &config.Config{RunMode: config.RunModeSimple, Gateway: config.GatewayConfig{MaxBodySize: 1 << 20, TextMaxBodySize: 1 << 20}}
	gatewayService := service.NewOpenAIGatewayService(
		&qianwenStandardAccountRepo{accounts: accounts},
		nil, nil, nil, nil, nil, nil,
		cfg,
		nil, nil, nil, nil, nil,
		upstream,
		nil, nil, nil, nil, nil, nil, nil, nil,
	)
	billingCache := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billingCache.Stop)
	h := NewOpenAIGatewayHandler(
		gatewayService,
		service.NewConcurrencyService(nil),
		billingCache,
		service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg),
		nil, nil, nil, nil, cfg,
	)
	h.maxAccountSwitches = 1
	return h
}

func qianwenStandardContext(t *testing.T, method, path string, body []byte, group *service.Group) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	if group == nil {
		group = &service.Group{ID: 7, Platform: service.PlatformQianwen, RateMultiplier: 1}
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	c.Request = request
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{ID: 3, UserID: 5, GroupID: &group.ID, Group: group, User: &service.User{ID: 5, Status: service.StatusActive}})
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 5, Concurrency: 0})
	return c, recorder
}

func TestQianwenEmbeddingsHandlerWireAndPlatformGate(t *testing.T) {
	payload := `{"object":"list","data":[{"embedding":[0.1,0.2],"index":0,"object":"embedding"}],"model":"text-embedding-v4","usage":{"prompt_tokens":11,"total_tokens":11}}`
	recorder := &httpUpstreamRecorderStub{body: payload}
	h := newQianwenStandardTestHandler(t, recorder, []service.Account{qianwenCatalogAccount(1, "text-embedding-v4", "embeddings")})
	c, w := qianwenStandardContext(t, http.MethodPost, "/v1/embeddings", []byte(`{"model":"text-embedding-v4","input":["hello"]}`), nil)
	h.QianwenEmbeddings(c)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, "https://dashscope.aliyuncs.com/compatible-mode/v1/embeddings", recorder.lastURL)
	require.Equal(t, "Bearer sk-sp-test", recorder.lastHeader.Get("Authorization"))
	require.Equal(t, "text-embedding-v4", gjson.GetBytes(recorder.lastBody, "model").String())
	require.Equal(t, payload, w.Body.String())

	// Non-qianwen groups never reach an upstream call.
	h = newQianwenStandardTestHandler(t, recorder, []service.Account{qianwenCatalogAccount(1, "text-embedding-v4", "embeddings")})
	c, w = qianwenStandardContext(t, http.MethodPost, "/v1/embeddings", []byte(`{"model":"text-embedding-v4","input":["hello"]}`), &service.Group{ID: 9, Platform: service.PlatformMistral})
	h.QianwenEmbeddings(c)
	require.Equal(t, 400, w.Code)
	// Models outside the verified compatible family are rejected before upstream.
	c, w = qianwenStandardContext(t, http.MethodPost, "/v1/embeddings", []byte(`{"model":"qwen-plus","input":["hello"]}`), nil)
	h.QianwenEmbeddings(c)
	require.Equal(t, 400, w.Code)
}

func TestQianwenRerankHandlerBridgeWireAndConversion(t *testing.T) {
	native := `{"output":{"results":[{"index":0,"relevance_score":0.93,"document":{"text":"alpha"}},{"index":2,"relevance_score":0.41,"document":{"text":"gamma"}}]},"usage":{"total_tokens":79},"request_id":"rr-9"}`
	recorder := &httpUpstreamRecorderStub{body: native}
	h := newQianwenStandardTestHandler(t, recorder, []service.Account{qianwenCatalogAccount(1, "gte-rerank-v2", "chat_completions")})
	c, w := qianwenStandardContext(t, http.MethodPost, "/v1/rerank", []byte(`{"model":"gte-rerank-v2","query":"what is rerank","documents":["alpha","beta","gamma"],"top_n":2}`), nil)
	h.QianwenRerank(c)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, "https://dashscope.aliyuncs.com/api/v1/services/rerank/text-rerank/text-rerank", recorder.lastURL)
	require.Equal(t, "Bearer sk-sp-test", recorder.lastHeader.Get("Authorization"))
	require.Equal(t, "what is rerank", gjson.GetBytes(recorder.lastBody, "input.query").String())
	require.Equal(t, 3, len(gjson.GetBytes(recorder.lastBody, "input.documents").Array()))
	require.True(t, gjson.GetBytes(recorder.lastBody, "parameters.return_documents").Bool())
	require.EqualValues(t, 2, gjson.GetBytes(recorder.lastBody, "parameters.top_n").Int())
	require.Equal(t, "gte-rerank-v2", gjson.GetBytes(w.Body.Bytes(), "model").String())
	require.Equal(t, 2, len(gjson.GetBytes(w.Body.Bytes(), "results").Array()))
	require.Equal(t, "alpha", gjson.GetBytes(w.Body.Bytes(), "results.0.document.text").String())
	require.EqualValues(t, 79, gjson.GetBytes(w.Body.Bytes(), "usage.total_tokens").Int())
	require.Equal(t, "rr-9", gjson.GetBytes(w.Body.Bytes(), "id").String())

	// Invalid flat shapes and unsupported models fail before upstream.
	for _, body := range []string{
		`{"model":"gte-rerank-v2","query":"","documents":["a"]}`,
		`{"model":"gte-rerank-v2","query":"q","documents":[]}`,
		`{"model":"qwen-plus","query":"q","documents":["a"]}`,
	} {
		c, w = qianwenStandardContext(t, http.MethodPost, "/v1/rerank", []byte(body), nil)
		h.QianwenRerank(c)
		require.Equal(t, 400, w.Code, body)
	}
}

func TestQianwenImagesHandlerBridgeWire(t *testing.T) {
	payload := `{"created":1720000000,"data":[{"url":"https://example.com/1.png"}],"usage":{"output_image_count":1,"output_width":1024,"output_height":1024,"input_image_count":0}}`
	recorder := &httpUpstreamRecorderStub{body: payload}
	h := newQianwenStandardTestHandler(t, recorder, []service.Account{qianwenCatalogAccount(1, "qwen-image-3.0", "images")})
	c, w := qianwenStandardContext(t, http.MethodPost, "/v1/images/generations", []byte(`{"model":"qwen-image-3.0","prompt":"a cat","n":1,"size":"1024x1024"}`), nil)
	h.QianwenImages(c)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, "https://dashscope.aliyuncs.com/compatible-mode/v1/images/generations", recorder.lastURL)
	require.Equal(t, "Bearer sk-sp-test", recorder.lastHeader.Get("Authorization"))
	require.Equal(t, payload, w.Body.String())
	for _, body := range []string{
		`{"model":"qwen-image-2.0","prompt":"a cat"}`,
		`{"model":"qwen-image-3.0","prompt":"a cat","n":8}`,
		`{"model":"qwen-image-3.0","prompt":" "}`,
		`{"model":"qwen-image-3.0","prompt":"a cat","stream":true}`,
	} {
		c, w = qianwenStandardContext(t, http.MethodPost, "/v1/images/generations", []byte(body), nil)
		h.QianwenImages(c)
		require.Equal(t, 400, w.Code, body)
	}
}

func TestQianwenStandardActionsCompositeExplicitTargets(t *testing.T) {
	native := `{"output":{"results":[{"index":0,"relevance_score":0.9}]},"usage":{"total_tokens":5},"request_id":"rr-c"}`
	recorder := &httpUpstreamRecorderStub{body: native}
	h := newQianwenStandardTestHandler(t, recorder, []service.Account{qianwenCatalogAccount(1, "gte-rerank-v2", "chat_completions")})
	composite := &service.Group{ID: 21, Platform: service.PlatformComposite, RateMultiplier: 1}
	// The composite middleware has already rewritten the body model to the
	// resolved upstream model when the handler runs; the decision context is
	// what platform-pins the request.
	body := []byte(`{"model":"gte-rerank-v2","query":"q","documents":["a"]}`)
	c, w := qianwenStandardContext(t, http.MethodPost, "/v1/rerank", body, composite)
	c.Request = c.Request.WithContext(service.WithCompositeRouteDecision(c.Request.Context(), service.CompositeRouteDecision{
		Matched:        true,
		Source:         service.CompositeRouteSourceExplicit,
		PublicModel:    "rerank-alias",
		TargetPlatform: service.PlatformQianwen,
		UpstreamModel:  "gte-rerank-v2",
	}))
	h.QianwenRerank(c)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, "gte-rerank-v2", gjson.GetBytes(recorder.lastBody, "model").String(), "composite upstream model is forwarded")

	// A composite route to a different platform is rejected by this endpoint.
	c, w = qianwenStandardContext(t, http.MethodPost, "/v1/rerank", body, composite)
	c.Request = c.Request.WithContext(service.WithCompositeRouteDecision(c.Request.Context(), service.CompositeRouteDecision{
		Matched:        true,
		Source:         service.CompositeRouteSourceExplicit,
		PublicModel:    "rerank-alias",
		TargetPlatform: service.PlatformGrok,
		UpstreamModel:  "grok-rerank",
	}))
	h.QianwenRerank(c)
	require.Equal(t, 400, w.Code)
}

type httpUpstreamRecorderStub struct {
	service.HTTPUpstream
	body       string
	status     int
	lastURL    string
	lastHeader http.Header
	lastBody   []byte
	calls      int
}

func (s *httpUpstreamRecorderStub) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	s.calls++
	s.lastURL = req.URL.String()
	s.lastHeader = req.Header.Clone()
	if req.Body != nil {
		body, _ := io.ReadAll(req.Body)
		s.lastBody = body
		_ = req.Body.Close()
	}
	status := s.status
	if status == 0 {
		status = 200
	}
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewReader([]byte(s.body)))}, nil
}
