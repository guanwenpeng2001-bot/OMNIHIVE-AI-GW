//go:build unit

package handler

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	middleware2 "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

// Mistral native action handler tests. Endpoint shapes follow the official
// specification (mistralai/platform-docs-public openapi.yaml, retrieved
// 2026-09-21) and are fixed in service/mistral_actions.go; positive
// upstream-shape coverage for those exact endpoints lives in
// backend/internal/service/mistral_actions_test.go and
// backend/internal/service/mistral_action_dispatch_test.go.

type mistralActionAccountRepo struct {
	service.AccountRepository
	accounts []service.Account
}

func (r mistralActionAccountRepo) GetByID(_ context.Context, id int64) (*service.Account, error) {
	for i := range r.accounts {
		if r.accounts[i].ID == id {
			account := r.accounts[i]
			return &account, nil
		}
	}
	return nil, service.ErrNoAvailableAccounts
}

func (r mistralActionAccountRepo) ListSchedulableByGroupIDAndPlatform(_ context.Context, _ int64, platform string) ([]service.Account, error) {
	return r.accountsForPlatform(platform), nil
}

func (r mistralActionAccountRepo) ListSchedulableByPlatform(_ context.Context, platform string) ([]service.Account, error) {
	return r.accountsForPlatform(platform), nil
}

func (r mistralActionAccountRepo) ListSchedulableUngroupedByPlatform(_ context.Context, platform string) ([]service.Account, error) {
	return r.accountsForPlatform(platform), nil
}

func (r mistralActionAccountRepo) ListModelAvailabilityCandidates(_ context.Context, _ *int64, platforms []string, _ bool) ([]service.Account, error) {
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

func (r mistralActionAccountRepo) accountsForPlatform(platform string) []service.Account {
	out := make([]service.Account, 0, len(r.accounts))
	for _, account := range r.accounts {
		if account.Platform == platform {
			out = append(out, account)
		}
	}
	return out
}

// mistralActionCatalogEvidence declares the model families the account observed
// upstream, the same shape a probed account carries in production.
func mistralActionCatalogEvidence(account service.Account) service.Account {
	models := []string{"mistral-embed", "mistral-large-latest", "voxtral-mini-latest", "mistral-tts-latest"}
	snapshot := service.AccountModelCatalogSnapshot{Complete: true, Revision: 1, DiscoveredModels: models}
	for _, model := range models {
		endpoints := []string{"responses", "chat_completions", "embeddings"}
		switch model {
		case "voxtral-mini-latest":
			endpoints = []string{"transcriptions"}
		case "mistral-tts-latest":
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
	account.Extra[service.UpstreamModelCatalogExtraKey] = snapshot
	return account
}

type mistralActionUpstream struct {
	service.HTTPUpstream
	mu        sync.Mutex
	responder func(*http.Request) *http.Response
	requests  []*http.Request
}

func (u *mistralActionUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.mu.Lock()
	u.requests = append(u.requests, req)
	responder := u.responder
	u.mu.Unlock()
	return responder(req), nil
}

func (u *mistralActionUpstream) snapshot() []*http.Request {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]*http.Request(nil), u.requests...)
}

func mistralActionJSONResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

type mistralActionHarness struct {
	router   *gin.Engine
	upstream *mistralActionUpstream
	cleanup  func()
}

func newMistralActionHarness(t *testing.T, responder func(*http.Request) *http.Response) *mistralActionHarness {
	t.Helper()
	gin.SetMode(gin.TestMode)
	groupID := int64(9201)
	repo := &mistralActionAccountRepo{accounts: []service.Account{mistralActionCatalogEvidence(service.Account{
		ID:          9202,
		Platform:    service.PlatformMistral,
		Type:        service.AccountTypeAPIKey,
		Status:      service.StatusActive,
		Schedulable: true,
		Concurrency: 50,
		GroupIDs:    []int64{groupID},
		Credentials: map[string]any{"api_key": "mistral-flow-key", "base_url": "https://api.mistral.ai/v1"},
	})}}
	upstream := &mistralActionUpstream{responder: responder}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Gateway.MaxAccountSwitches = 1
	cfg.Gateway.Scheduling.StickySessionWaitTimeout = 20 * time.Millisecond
	cfg.Gateway.Scheduling.StickySessionMaxWaiting = 3
	billingCache := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	concurrency := service.NewConcurrencyService(&grokMediaSlotsCache{accounts: map[string]int64{}, users: map[string]int64{}})
	// The gateway cache stub the sibling action tests already provide; only the
	// sticky-session binding it implements is exercised here.
	gatewayCache := newHunyuanActionCache()
	gateway := service.ProvideOpenAIGatewayService(
		repo, nil, nil, nil, nil, nil, gatewayCache, cfg, nil, concurrency,
		service.NewBillingService(cfg, nil), nil, billingCache, upstream,
		&service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil, service.NewUsageSettlementService(&unlimitedTPMReceiptStub{}, nil, nil))
	handler := NewOpenAIGatewayHandler(gateway, concurrency, billingCache, service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, cfg)

	apiKey := &service.APIKey{
		ID:      9203,
		GroupID: &groupID,
		User:    &service.User{ID: 9204, Status: service.StatusActive},
		Group:   &service.Group{ID: groupID, Platform: service.PlatformMistral, Status: service.StatusActive},
	}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyAPIKey), apiKey)
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: apiKey.User.ID, Concurrency: 1})
		c.Next()
	})
	// The registered gateway paths for these cells; the voice family keeps the
	// official /v1/audio/voices surface the forwarder implements.
	router.POST("/v1/embeddings", handler.MistralEmbeddings)
	router.POST("/v1/audio/speech", handler.MistralAudioSpeech)
	router.POST("/v1/audio/transcriptions", handler.MistralAudioTranscriptions)
	router.POST("/v1/audio/voices", handler.MistralVoices)
	router.PATCH("/v1/audio/voices/:voice_id", handler.MistralVoices)
	return &mistralActionHarness{
		router:   router,
		upstream: upstream,
		cleanup:  func() { billingCache.Stop() },
	}
}

func (h *mistralActionHarness) serve(t *testing.T, method, path string, body []byte, contentType string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	request.Header.Set("Content-Type", contentType)
	recorder := httptest.NewRecorder()
	h.router.ServeHTTP(recorder, request)
	return recorder
}

func mistralActionMultipartBody(t *testing.T) ([]byte, string) {
	t.Helper()
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	require.NoError(t, writer.WriteField("model", "voxtral-mini-latest"))
	part, err := writer.CreateFormFile("file", "fixture.wav")
	require.NoError(t, err)
	_, err = part.Write([]byte("RIFF....WAVEfmt "))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	return buffer.Bytes(), writer.FormDataContentType()
}

// TestMistralActionPathsReachTheDocumentedEndpoints proves the ingress seam end
// to end for the cells that pass the whole chain today: the handler selects a
// Mistral account, calls the forwarder the mistral.* adapter key wraps and
// reaches the endpoint the official specification publishes with the account API
// key as a Bearer token.
//
// The two audio generation cells are exercised separately below: account
// selection admits them on the chat trunk Mistral publishes, so their cases carry
// the endpoint they reach and the usage guard each one owns.
func TestMistralActionPathsReachTheDocumentedEndpoints(t *testing.T) {
	scenarios := []struct {
		name        string
		method      string
		path        string
		body        []byte
		contentType string
		response    string
		wantURL     string
		wantMethod  string
	}{
		{
			name: "embeddings", method: http.MethodPost, path: "/v1/embeddings",
			body:        []byte(`{"model":"mistral-embed","input":"hello"}`),
			contentType: "application/json",
			response:    `{"object":"list","data":[],"model":"mistral-embed","usage":{"prompt_tokens":5,"completion_tokens":0,"total_tokens":5}}`,
			wantURL:     "https://api.mistral.ai/v1/embeddings",
			wantMethod:  http.MethodPost,
		},
		{
			name: "voices create", method: http.MethodPost, path: "/v1/audio/voices",
			body:        []byte(`{"name":"fixture","gender":"female"}`),
			contentType: "application/json",
			response:    `{"id":"voice-1","name":"fixture"}`,
			wantURL:     "https://api.mistral.ai/v1/audio/voices",
			wantMethod:  http.MethodPost,
		},
		{
			name: "voices subpath", method: http.MethodPatch, path: "/v1/audio/voices/voice-1",
			body:        []byte(`{"name":"renamed"}`),
			contentType: "application/json",
			response:    `{"id":"voice-1","name":"renamed"}`,
			wantURL:     "https://api.mistral.ai/v1/audio/voices/voice-1",
			wantMethod:  http.MethodPatch,
		},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			h := newMistralActionHarness(t, func(*http.Request) *http.Response {
				return mistralActionJSONResponse(http.StatusOK, scenario.response)
			})
			defer h.cleanup()

			recorder := h.serve(t, scenario.method, scenario.path, scenario.body, scenario.contentType)

			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
			requests := h.upstream.snapshot()
			require.Len(t, requests, 1, "the documented endpoint is called exactly once")
			require.Equal(t, scenario.wantMethod, requests[0].Method)
			require.Equal(t, scenario.wantURL, requests[0].URL.String())
			require.Equal(t, "Bearer mistral-flow-key", requests[0].Header.Get("Authorization"))
		})
	}
}

// TestMistralAudioPathsReachTheirEndpointsAndFailClosedOnUnknownUsage pins both
// halves of the audio generation seam with real audio-family evidence.
// The plan gate requires transcriptions or speech, as declared by the fixture,
// so the request reaches the documented endpoint without borrowing text evidence.
// What each path does with an upstream envelope that
// carries no parseable usage then differs, and both behaviours are real:
// transcriptions settle on the Voxtral usage contract (usage.prompt_audio_seconds),
// so an unparseable envelope fails closed with 502 instead of booking a trusted
// zero, while speech bills the request's input characters and relays the upstream
// envelope with its own status.
func TestMistralAudioPathsReachTheirEndpointsAndFailClosedOnUnknownUsage(t *testing.T) {
	transcriptionBody, transcriptionContentType := mistralActionMultipartBody(t)
	speechBody := []byte(`{"model":"mistral-tts-latest","input":"hello"}`)
	scenarios := []struct {
		name        string
		path        string
		body        []byte
		contentType string
		wantURL     string
		wantStatus  int
		wantBody    string
	}{
		{
			name: "audio transcriptions", path: "/v1/audio/transcriptions", body: transcriptionBody, contentType: transcriptionContentType,
			wantURL: "https://api.mistral.ai/v1/audio/transcriptions",
			// usage.prompt_audio_seconds is missing from the stub envelope, so the
			// transcription has no billable duration and the adapter refuses it.
			wantStatus: http.StatusBadGateway,
			wantBody:   `{"error":{"message":"Mistral usage is unknown","type":"upstream_error"}}`,
		},
		{
			name: "audio speech", path: "/v1/audio/speech", body: speechBody, contentType: "application/json",
			wantURL: "https://api.mistral.ai/v1/audio/speech",
			// Speech bills the input characters of the request, so the same empty
			// upstream envelope is relayed unchanged.
			wantStatus: http.StatusOK,
			wantBody:   `{}`,
		},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			h := newMistralActionHarness(t, func(*http.Request) *http.Response {
				return mistralActionJSONResponse(http.StatusOK, `{}`)
			})
			defer h.cleanup()

			recorder := h.serve(t, http.MethodPost, scenario.path, scenario.body, scenario.contentType)

			require.Equal(t, scenario.wantStatus, recorder.Code, recorder.Body.String())
			require.JSONEq(t, scenario.wantBody, recorder.Body.String())
			requests := h.upstream.snapshot()
			require.Len(t, requests, 1, "the selected account reaches the documented endpoint exactly once")
			require.Equal(t, scenario.wantURL, requests[0].URL.String())
		})
	}

	// Counter-case: the very same path answers 200 and relays the transcription as
	// soon as the upstream envelope carries the Voxtral usage fields, so the 502
	// above is produced by the usage contract and by nothing else on this path.
	usageEnvelope := `{"model":"voxtral-mini-latest","text":"hello","usage":{"prompt_audio_seconds":12.5,"prompt_tokens":3,"completion_tokens":2}}`
	h := newMistralActionHarness(t, func(*http.Request) *http.Response {
		return mistralActionJSONResponse(http.StatusOK, usageEnvelope)
	})
	defer h.cleanup()
	recorder := h.serve(t, http.MethodPost, "/v1/audio/transcriptions", transcriptionBody, transcriptionContentType)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.JSONEq(t, usageEnvelope, recorder.Body.String())
	requests := h.upstream.snapshot()
	require.Len(t, requests, 1)
	require.Equal(t, "https://api.mistral.ai/v1/audio/transcriptions", requests[0].URL.String())
}

// TestMistralActionPathsRejectMissingModel covers the admission the handler owns
// before any scheduling: embeddings, transcriptions and speech all need a model
// selector, and a request without one is a client fault that never reaches the
// upstream.
func TestMistralActionPathsRejectMissingModel(t *testing.T) {
	scenarios := []struct {
		name        string
		path        string
		body        string
		contentType string
	}{
		{name: "embeddings", path: "/v1/embeddings", body: `{"input":"hello"}`, contentType: "application/json"},
		{name: "audio speech", path: "/v1/audio/speech", body: `{"input":"hello"}`, contentType: "application/json"},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			h := newMistralActionHarness(t, func(*http.Request) *http.Response {
				return mistralActionJSONResponse(http.StatusOK, `{}`)
			})
			defer h.cleanup()

			recorder := h.serve(t, http.MethodPost, scenario.path, []byte(scenario.body), scenario.contentType)

			require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
			require.Contains(t, recorder.Body.String(), "model is required")
			require.Empty(t, h.upstream.snapshot(), "a client fault must never reach the upstream")
		})
	}
}

func TestShouldRecordMistralActionUsage(t *testing.T) {
	embeddings := &service.OpenAIForwardResult{Usage: service.OpenAIUsage{InputTokens: 7}}
	require.True(t, shouldRecordMistralActionUsage(mistralActionEmbeddings, embeddings))
	require.False(t, shouldRecordMistralActionUsage(mistralActionEmbeddings, &service.OpenAIForwardResult{}))
	require.False(t, shouldRecordMistralActionUsage(mistralActionEmbeddings, nil))

	for _, kind := range []mistralActionKind{mistralActionTranscription, mistralActionSpeech} {
		require.True(t, shouldRecordMistralActionUsage(kind, &service.OpenAIForwardResult{AudioUsage: &service.AudioUsage{Mode: "tts", DurationOrUnits: 1}}))
		require.False(t, shouldRecordMistralActionUsage(kind, &service.OpenAIForwardResult{}))
	}

	// Voice management is not a billable generation action: even a result that
	// carries usage must not settle gateway units for it.
	require.False(t, shouldRecordMistralActionUsage(mistralActionVoices, &service.OpenAIForwardResult{
		Usage:      service.OpenAIUsage{InputTokens: 9},
		AudioUsage: &service.AudioUsage{Mode: "tts", DurationOrUnits: 9},
	}))
}

func TestMistralActionPlanProtocolAndCapability(t *testing.T) {
	require.Equal(t, "embeddings", mistralActionPlanProtocol(mistralActionEmbeddings))
	for _, kind := range []mistralActionKind{mistralActionTranscription, mistralActionSpeech, mistralActionVoices} {
		require.Equal(t, "audio", mistralActionPlanProtocol(kind))
	}
	require.Equal(t, service.OpenAIEndpointCapabilityEmbeddings, mistralActionRequiredCapability(mistralActionEmbeddings))
	for _, kind := range []mistralActionKind{mistralActionTranscription, mistralActionSpeech, mistralActionVoices} {
		require.Empty(t, mistralActionRequiredCapability(kind))
	}
	require.True(t, mistralActionRequiresModel(mistralActionEmbeddings))
	require.True(t, mistralActionRequiresModel(mistralActionTranscription))
	require.True(t, mistralActionRequiresModel(mistralActionSpeech))
	require.False(t, mistralActionRequiresModel(mistralActionVoices), "voice management is not model-scoped")
}

func TestMistralActionRequestModelReadsJSONAndMultipart(t *testing.T) {
	require.Equal(t, "mistral-embed", mistralActionRequestModel(mistralActionEmbeddings, "application/json", []byte(`{"model":"mistral-embed","input":"x"}`)))
	require.Empty(t, mistralActionRequestModel(mistralActionEmbeddings, "application/json", []byte(`not json`)))

	body, contentType := mistralActionMultipartBody(t)
	require.Equal(t, "voxtral-mini-latest", mistralActionRequestModel(mistralActionTranscription, contentType, body))
	require.Empty(t, mistralActionRequestModel(mistralActionTranscription, "application/json", []byte(`{"model":"mistral-embed"}`)), "a non-multipart transcriptions request has no model field")
	require.Empty(t, mistralActionRequestModel(mistralActionTranscription, "multipart/form-data", body), "a missing boundary is not a parseable upload")

	// A file part named "model" is not a model selector.
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	part, err := writer.CreateFormFile("model", "model.bin")
	require.NoError(t, err)
	_, err = part.Write([]byte("binary"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	require.Empty(t, mistralActionRequestModel(mistralActionTranscription, writer.FormDataContentType(), buffer.Bytes()))
}

func TestMistralVoicesSubpathFollowsTheDocumentedMarker(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{path: "/v1/audio/voices", want: ""},
		{path: "/v1/audio/voices/", want: ""},
		{path: "/v1/audio/voices/voice-1", want: "voice-1"},
		{path: "/v1/audio/voices/voice-1/sample", want: "voice-1/sample"},
		{path: "/audio/voices/voice-1", want: "voice-1"},
		// A path outside the voice family contributes no subpath instead of
		// inventing one.
		{path: "/v1/embeddings", want: ""},
		{path: "/v1/custom-voices", want: ""},
	}
	for _, scenario := range cases {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodGet, scenario.path, nil)
		require.Equal(t, scenario.want, mistralVoicesSubpath(c), scenario.path)
	}
	require.Empty(t, mistralVoicesSubpath(nil))
}
