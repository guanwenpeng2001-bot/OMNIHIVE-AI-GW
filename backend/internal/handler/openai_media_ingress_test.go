//go:build unit

package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	middleware2 "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// The fixtures below serve the documented OpenAI video and audio wire shapes
// (VideoResource, CreateVideo*Body and the /audio/translations response) and
// record every upstream call, so the ingress is exercised through the real
// handler, the shared job trunk and the provider adapter without a database or
// a credentialed upstream.

type openAIMediaIngressFixture struct {
	mu       sync.Mutex
	requests []string
	bodies   map[string][]byte
}

type openAIMediaFailedResponseBody struct{ delivered bool }

func (b *openAIMediaFailedResponseBody) Read(p []byte) (int, error) {
	if b.delivered {
		return 0, io.ErrUnexpectedEOF
	}
	b.delivered = true
	return copy(p, `{"text":"partial"}`), nil
}

func (*openAIMediaFailedResponseBody) Close() error { return nil }

type openAIAudioReceiptCapture struct {
	unlimitedTPMRepository
	mu   sync.Mutex
	rows []*service.UsageSettlementReceipt
}

func (repo *openAIAudioReceiptCapture) Persist(_ context.Context, receipt *service.UsageSettlementReceipt) (*service.UsageSettlementReceipt, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	repo.rows = append(repo.rows, receipt)
	return receipt, nil
}

func (repo *openAIAudioReceiptCapture) Get(_ context.Context, key service.UsageSettlementKey) (*service.UsageSettlementReceipt, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	for _, receipt := range repo.rows {
		if receipt.Key == key {
			return receipt, nil
		}
	}
	return nil, service.ErrUsageSettlementNotFound
}

func (repo *openAIAudioReceiptCapture) Settle(_ context.Context, key service.UsageSettlementKey, _ string) (*service.UsageSettlementResult, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	for _, receipt := range repo.rows {
		if receipt.Key == key {
			receipt.State = service.UsageSettlementSettled
			return &service.UsageSettlementResult{Receipt: receipt, Billing: &service.UsageBillingApplyResult{}}, nil
		}
	}
	return nil, service.ErrUsageSettlementNotFound
}

func (repo *openAIAudioReceiptCapture) ListPending(context.Context, int64, int) ([]*service.UsageSettlementReceipt, error) {
	return nil, nil
}

func (repo *openAIAudioReceiptCapture) stored() []*service.UsageSettlementReceipt {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	return append([]*service.UsageSettlementReceipt(nil), repo.rows...)
}

func (f *openAIMediaIngressFixture) record(request *http.Request) {
	body, _ := io.ReadAll(request.Body)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, request.Method+" "+request.URL.Path)
	f.bodies[request.Method+" "+request.URL.Path] = body
}

func (f *openAIMediaIngressFixture) calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.requests...)
}

func (f *openAIMediaIngressFixture) bodyOf(key string) []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]byte(nil), f.bodies[key]...)
}

func (f *openAIMediaIngressFixture) serve(t *testing.T) *httptest.Server {
	t.Helper()
	f.bodies = map[string][]byte{}
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		f.record(request)
		switch {
		case request.Method == http.MethodPost && request.URL.Path == "/v1/videos":
			writer.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(writer, `{"id":"video_upstream","object":"video","status":"queued","model":"sora-2","progress":0,"seconds":"8","size":"1280x720"}`)
		case request.Method == http.MethodGet && request.URL.Path == "/v1/videos/video_upstream":
			writer.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(writer, `{"id":"video_upstream","object":"video","status":"completed","model":"sora-2","progress":100,"seconds":"8","size":"1280x720"}`)
		case request.Method == http.MethodGet && request.URL.Path == "/v1/videos/video_upstream/content":
			writer.Header().Set("Content-Type", "video/mp4")
			_, _ = io.WriteString(writer, "mp4-bytes")
		case request.Method == http.MethodPost && request.URL.Path == "/v1/audio/translations":
			writer.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(writer, `{"text":"hello"}`)
		default:
			http.NotFound(writer, request)
		}
	}))
}

// openAIMediaIngressAccount is an OpenAI API-key account whose catalogue carries
// the priced video family and the translation model, so account selection and
// model authorization see the same evidence a production account publishes.
func openAIMediaIngressAccount(baseURL string) service.Account {
	catalog := service.AccountModelCatalogSnapshot{
		Complete: true, Revision: 1, DiscoveredModels: []string{"sora-2", "whisper-1"},
		EligibleModels: []service.AccountCatalogModel{
			{ID: "sora-2", Endpoints: []string{"chat_completions", "responses"}, EvidenceKind: "fixture", EvidenceObservedAt: "2026-09-21T00:00:00Z"},
			{ID: "whisper-1", Endpoints: []string{"audio_translations"}, EvidenceKind: "fixture", EvidenceObservedAt: "2026-09-21T00:00:00Z"},
		},
	}
	return service.Account{
		ID: 7, Name: "openai-media-ingress", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true, Concurrency: 20, GroupIDs: []int64{31},
		Credentials: map[string]any{"api_key": "sk-media", "base_url": baseURL},
		Extra:       map[string]any{service.UpstreamModelCatalogExtraKey: catalog},
	}
}

func newOpenAIMediaIngressHandler(t *testing.T, server *httptest.Server, financialRepo ...service.UsageSettlementReceiptRepository) (*OpenAIGatewayHandler, *gatewayResourceWiringRepo) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	repo := newGatewayResourceWiringRepo()
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	cfg.Security.URLAllowlist.AllowPrivateHosts = true
	cfg.Security.URLAllowlist.UpstreamHosts = []string{"127.0.0.1"}
	cfg.Gateway.Scheduling.StickySessionWaitTimeout = 20 * time.Millisecond
	cfg.Gateway.Scheduling.StickySessionMaxWaiting = 3
	concurrency := service.NewConcurrencyService(&grokMediaSlotsCache{accounts: map[string]int64{}, users: map[string]int64{}})
	billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billing.Stop)
	var receipts service.UsageSettlementReceiptRepository = &unlimitedTPMReceiptStub{}
	gatewayCfg := *cfg
	if len(financialRepo) > 0 {
		receipts = financialRepo[0]
		gatewayCfg.RunMode = config.RunModeStandard
		gatewayCfg.Default.RateMultiplier = 1
	}
	gateway := service.ProvideOpenAIGatewayService(
		openAIImagesFailoverAccountRepo{accounts: []service.Account{openAIMediaIngressAccount(server.URL)}},
		nil, nil, nil, nil, nil, nil, &gatewayCfg, nil, concurrency,
		service.NewBillingService(cfg, nil), nil, nil,
		gatewayResourceWiringUpstream{client: server.Client()},
		nil, nil, nil, nil, nil, nil, nil, nil, service.NewUsageSettlementService(receipts, nil, nil))
	openAI := NewOpenAIGatewayHandler(gateway, concurrency, billing, service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, cfg)
	jobs := service.NewUpstreamJobService(repo, service.GatewayResourcePollJob(gateway), service.GatewayResourceJobCharge)
	openAI.upstreamJobs = jobs
	return openAI, repo
}

func openAIMediaIngressKey() *service.APIKey {
	groupID := int64(31)
	return &service.APIKey{
		ID: 20, UserID: 10, GroupID: &groupID,
		User: &service.User{ID: 10, Status: service.StatusActive},
		Group: &service.Group{
			ID: groupID, Platform: service.PlatformOpenAI, Status: service.StatusActive, RateMultiplier: 1,
			VideoModelPrices: map[string]map[string]float64{"sora-2": {"720p": 0.1}},
		},
	}
}

func openAIMediaIngressRequest(t *testing.T, handler gin.HandlerFunc, method, pattern, target string, body []byte, contentType string) *httptest.ResponseRecorder {
	t.Helper()
	return openAIMediaIngressRequestForUser(t, handler, method, pattern, target, body, contentType, 10)
}

// openAIMediaIngressRequestForUser runs one request as another key owner, which
// is how the owner-scoped reads and the source binding are probed.
func openAIMediaIngressRequestForUser(t *testing.T, handler gin.HandlerFunc, method, pattern, target string, body []byte, contentType string, userID int64) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	key := openAIMediaIngressKey()
	key.UserID = userID
	key.User = &service.User{ID: userID, Status: service.StatusActive}
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyAPIKey), key)
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: userID, Concurrency: 5})
		c.Next()
	})
	router.Handle(method, pattern, handler)
	request := httptest.NewRequest(method, target, bytes.NewReader(body))
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func openAIMediaIngressMultipart(t *testing.T, fields map[string]string, fileField, filename, content string) ([]byte, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for name, value := range fields {
		require.NoError(t, writer.WriteField(name, value))
	}
	if fileField != "" {
		part, err := writer.CreateFormFile(fileField, filename)
		require.NoError(t, err)
		_, err = part.Write([]byte(content))
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())
	return body.Bytes(), writer.FormDataContentType()
}

// openAIMediaIngressVideoJob seeds one durable video receipt, which is what the
// shared poller stores before a client reads it.
func openAIMediaIngressVideoJob(t *testing.T, repo *gatewayResourceWiringRepo, userID, keyID int64, publicID, upstreamID, status string) *service.UpstreamJob {
	t.Helper()
	groupID := int64(31)
	job := &service.UpstreamJob{
		Platform: service.PlatformOpenAI, Family: "video", AccountID: 7,
		UserID: userID, APIKeyID: keyID, GroupID: &groupID,
		PublicRequestID: publicID, SubmissionKey: "submission-" + publicID,
		Status: status, UpstreamTaskID: upstreamID,
		SubmittedAt: time.Now().UTC(), NextPollAt: time.Now().UTC(),
		Billing: service.UpstreamJobBilling{Model: "sora-2", RequestedModel: "sora-2", Resolution: "720p", UnitPrice: 0.1, RateMultiplier: 1, AccountRateMultiplier: 1},
	}
	if status == "succeeded" {
		job.ResultRef = upstreamID
		job.UsageJSON = []byte(`{"seconds":8,"size":"1280x720","model":"sora-2"}`)
	}
	stored, created, err := repo.Create(context.Background(), job)
	require.NoError(t, err)
	require.True(t, created)
	return stored
}

// jobsForUser reads the durable receipts of one key owner. The wiring fixture
// keeps its rows private; this accessor keeps the ingress assertions on the
// store instead of on a second in-memory model.
func (r *gatewayResourceWiringRepo) jobsForUser(user, key int64) []*service.UpstreamJob {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []*service.UpstreamJob{}
	for _, job := range r.jobs {
		if job.UserID == user && job.APIKeyID == key {
			out = append(out, cloneGatewayResourceJob(job))
		}
	}
	return out
}

// TestOpenAIVideoIngressAdmitsFrozenTariffThenFailsClosedAtTheMediaSendBoundary
// drives one video create through the ingress: the action gate, the adapter
// dispatch, account selection and tariff admission all run, and the caller gets
// the shared trunk's refusal.
//
// The refusal is the pre-existing send boundary, not the ingress: job-backed
// submissions always carry a job plan, and service/openai_actions.go:212 then
// revalidates the model against catalogue evidence for the endpoint token
// "video.generate", which the OpenAI catalogue projection
// (service/account_model_catalog.go:380-406) can never publish. The service
// suite pins the same state in
// TestOpenAIVideoSendBoundaryFailsClosedWithoutEndpointEvidence; projecting
// media endpoints is the model-registry/catalogue decision tracked by the
// coordinator, not a change this ingress may make.
func TestOpenAIVideoIngressAdmitsFrozenTariffThenFailsClosedAtTheMediaSendBoundary(t *testing.T) {
	fixture := &openAIMediaIngressFixture{}
	server := fixture.serve(t)
	defer server.Close()
	openAI, repo := newOpenAIMediaIngressHandler(t, server)

	body := []byte(`{"model":"sora-2","prompt":"waves","size":"1280x720","seconds":"8"}`)
	recorder := openAIMediaIngressRequest(t, openAI.OpenAIVideoGeneration, http.MethodPost, "/v1/videos", "/v1/videos", body, "application/json")
	require.Equal(t, http.StatusBadGateway, recorder.Code, recorder.Body.String())
	require.Contains(t, recorder.Body.String(), "submission_unknown")

	admitted := repo.jobsForUser(10, 20)
	require.Len(t, admitted, 1, "the job receipt is durable even when the send boundary refuses the submission")
	job := admitted[0]
	require.Equal(t, "video", job.Family)
	require.Equal(t, "failed", job.Status)
	require.Empty(t, job.UpstreamTaskID)
	// The admission tariff was frozen before the upstream call and is what the
	// poller would settle, so a later boundary fix needs no ingress change.
	require.Equal(t, "sora-2", job.Billing.Model)
	require.Equal(t, "720p", job.Billing.Resolution)
	require.Equal(t, 0.1, job.Billing.UnitPrice)
	require.Equal(t, 1.0, job.Billing.RateMultiplier)
	require.Empty(t, fixture.calls(), "a locally refused submission never reaches the upstream")

	// An equivalent retry resolves the same durable job: no second admission and
	// no automatic resubmission of an unknown outcome.
	recorder = openAIMediaIngressRequest(t, openAI.OpenAIVideoGeneration, http.MethodPost, "/v1/videos", "/v1/videos", body, "application/json")
	require.Equal(t, http.StatusBadGateway, recorder.Code, recorder.Body.String())
	require.Len(t, repo.jobsForUser(10, 20), 1)
	require.Empty(t, fixture.calls())
}

// TestOpenAIVideoEditAndExtensionResolveTheOwnerCheckedSource covers the
// pre-adapter contract of edit and extension: the source must be the caller's
// own completed video, and the extension segment must be one of the documented
// values. Both refusals happen before any upstream work.
func TestOpenAIVideoEditAndExtensionResolveTheOwnerCheckedSource(t *testing.T) {
	fixture := &openAIMediaIngressFixture{}
	server := fixture.serve(t)
	defer server.Close()
	openAI, repo := newOpenAIMediaIngressHandler(t, server)
	source := openAIMediaIngressVideoJob(t, repo, 10, 20, "job-media-settled", "video_upstream", "succeeded")

	// The body the adapter would forward is not observable while the media send
	// boundary refuses the submission, so the id translation is asserted on the
	// transformation itself and the handler assertions below stay on what the
	// ingress answers.
	rewritten, rewriteErr := openAIVideoSourceBody([]byte(`{"video":{"id":"`+source.PublicRequestID+`"},"prompt":"continue"}`), source)
	require.NoError(t, rewriteErr)
	require.JSONEq(t, `{"video":{"id":"video_upstream"},"prompt":"continue"}`, string(rewritten))

	recorder := openAIMediaIngressRequest(t, openAI.OpenAIVideoEdit, http.MethodPost, "/v1/videos/edits", "/v1/videos/edits",
		[]byte(`{"video":{"id":"`+source.PublicRequestID+`"},"prompt":"continue"}`), "application/json")
	require.Equal(t, http.StatusBadGateway, recorder.Code, recorder.Body.String())
	require.Contains(t, recorder.Body.String(), "submission_unknown")

	recorder = openAIMediaIngressRequest(t, openAI.OpenAIVideoExtension, http.MethodPost, "/v1/videos/extensions", "/v1/videos/extensions",
		[]byte(`{"video":{"id":"`+source.PublicRequestID+`"},"prompt":"continue","seconds":"7"}`), "application/json")
	require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
	require.NotContains(t, fixture.calls(), "POST /v1/videos/extensions")

	recorder = openAIMediaIngressRequest(t, openAI.OpenAIVideoExtension, http.MethodPost, "/v1/videos/extensions", "/v1/videos/extensions",
		[]byte(`{"video":{"id":"`+source.PublicRequestID+`"},"prompt":"continue","seconds":"8"}`), "application/json")
	require.Equal(t, http.StatusBadGateway, recorder.Code, recorder.Body.String())

	// A foreign, unknown or unfinished source stays indistinguishable from a
	// missing one and never reaches the upstream.
	foreign := openAIMediaIngressRequestForUser(t, openAI.OpenAIVideoEdit, http.MethodPost, "/v1/videos/edits", "/v1/videos/edits",
		[]byte(`{"video":{"id":"`+source.PublicRequestID+`"},"prompt":"continue"}`), "application/json", 99)
	require.Equal(t, http.StatusNotFound, foreign.Code, foreign.Body.String())
	unknown := openAIMediaIngressRequest(t, openAI.OpenAIVideoEdit, http.MethodPost, "/v1/videos/edits", "/v1/videos/edits",
		[]byte(`{"video":{"id":"public-missing"},"prompt":"continue"}`), "application/json")
	require.Equal(t, http.StatusNotFound, unknown.Code, unknown.Body.String())

	running := openAIMediaIngressVideoJob(t, repo, 10, 20, "job-media-running", "video_running", "running")
	unfinished := openAIMediaIngressRequest(t, openAI.OpenAIVideoEdit, http.MethodPost, "/v1/videos/edits", "/v1/videos/edits",
		[]byte(`{"video":{"id":"`+running.PublicRequestID+`"},"prompt":"continue"}`), "application/json")
	require.Equal(t, http.StatusConflict, unfinished.Code, unfinished.Body.String())
}

// TestOpenAIVideoStatusAndContentAreOwnerBound covers the read half of the video
// surface against a settled durable receipt: the caller sees the public id and
// the documented status, the upstream id never leaks, and another key owner sees
// a 404.
func TestOpenAIVideoStatusAndContentAreOwnerBound(t *testing.T) {
	fixture := &openAIMediaIngressFixture{}
	server := fixture.serve(t)
	defer server.Close()
	openAI, repo := newOpenAIMediaIngressHandler(t, server)
	job := openAIMediaIngressVideoJob(t, repo, 10, 20, "job-media-settled", "video_upstream", "succeeded")

	recorder := openAIMediaIngressRequest(t, openAI.OpenAIVideoStatus, http.MethodGet, "/v1/videos/:request_id", "/v1/videos/"+job.PublicRequestID, nil, "")
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var read map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &read))
	require.Equal(t, job.PublicRequestID, read["id"])
	require.Equal(t, "video", read["object"])
	require.Equal(t, "completed", read["status"])
	require.Equal(t, "sora-2", read["model"])
	require.Equal(t, "8", read["seconds"])
	require.Equal(t, "1280x720", read["size"])
	require.NotContains(t, recorder.Body.String(), "video_upstream")

	recorder = openAIMediaIngressRequest(t, openAI.OpenAIVideoContent, http.MethodGet, "/v1/videos/:request_id/content", "/v1/videos/"+job.PublicRequestID+"/content", nil, "")
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, "mp4-bytes", recorder.Body.String())
	require.Equal(t, "video/mp4", recorder.Header().Get("Content-Type"))
	require.Contains(t, fixture.calls(), "GET /v1/videos/video_upstream/content")

	foreign := openAIMediaIngressRequestForUser(t, openAI.OpenAIVideoStatus, http.MethodGet, "/v1/videos/:request_id", "/v1/videos/"+job.PublicRequestID, nil, "", 99)
	require.Equal(t, http.StatusNotFound, foreign.Code, foreign.Body.String())
	foreignContent := openAIMediaIngressRequestForUser(t, openAI.OpenAIVideoContent, http.MethodGet, "/v1/videos/:request_id/content", "/v1/videos/"+job.PublicRequestID+"/content", nil, "", 99)
	require.Equal(t, http.StatusNotFound, foreignContent.Code, foreignContent.Body.String())
}

// TestOpenAIVideoContentRefusesUnsettledJob keeps the content route from
// streaming anything before the shared poller observed a result.
func TestOpenAIVideoContentRefusesUnsettledJob(t *testing.T) {
	fixture := &openAIMediaIngressFixture{}
	server := fixture.serve(t)
	defer server.Close()
	openAI, repo := newOpenAIMediaIngressHandler(t, server)
	job := openAIMediaIngressVideoJob(t, repo, 10, 20, "job-media-running", "video_upstream", "running")

	recorder := openAIMediaIngressRequest(t, openAI.OpenAIVideoContent, http.MethodGet, "/v1/videos/:request_id/content", "/v1/videos/"+job.PublicRequestID+"/content", nil, "")
	require.Equal(t, http.StatusConflict, recorder.Code, recorder.Body.String())
	require.Empty(t, fixture.calls())
}

// TestOpenAIAudioTranslationsForwardsMultipartAndProviderBody covers the standard
// OpenAI translation ingress end to end: multipart only, a required model, the
// provider response passed through, and a local rejection that never reaches the
// upstream. This adapter is synchronous and carries no job plan, so it is the
// half of the surface that previously bypassed the send boundary.
func TestOpenAIAudioTranslationsForwardsMultipartAndProviderBody(t *testing.T) {
	fixture := &openAIMediaIngressFixture{}
	server := fixture.serve(t)
	defer server.Close()
	openAI, _ := newOpenAIMediaIngressHandler(t, server)

	body, contentType := openAIMediaIngressMultipart(t, map[string]string{"model": "whisper-1"}, "file", "speech.mp3", "audio-bytes")
	recorder := openAIMediaIngressRequest(t, openAI.OpenAIAudioTranslations, http.MethodPost, "/v1/audio/translations", "/v1/audio/translations", body, contentType)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.JSONEq(t, `{"text":"hello"}`, recorder.Body.String())
	require.Contains(t, fixture.calls(), "POST /v1/audio/translations")
	require.Contains(t, string(fixture.bodyOf("POST /v1/audio/translations")), "audio-bytes")

	// A missing model field is a client error, not an upstream failure.
	body, contentType = openAIMediaIngressMultipart(t, nil, "file", "speech.mp3", "audio-bytes")
	recorder = openAIMediaIngressRequest(t, openAI.OpenAIAudioTranslations, http.MethodPost, "/v1/audio/translations", "/v1/audio/translations", body, contentType)
	require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
	require.Equal(t, 1, countOpenAIMediaCalls(fixture.calls(), "POST /v1/audio/translations"), "a locally rejected body never reaches the upstream")

	// The documented wire shape is multipart.
	recorder = openAIMediaIngressRequest(t, openAI.OpenAIAudioTranslations, http.MethodPost, "/v1/audio/translations", "/v1/audio/translations",
		[]byte(`{"model":"whisper-1"}`), "application/json")
	require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
	require.Equal(t, 1, countOpenAIMediaCalls(fixture.calls(), "POST /v1/audio/translations"))
}

func countOpenAIMediaCalls(calls []string, key string) int {
	count := 0
	for _, call := range calls {
		if call == key {
			count++
		}
	}
	return count
}

func TestTranslationRecordsObservedSecondsOrReview(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       float64
		review     bool
	}{
		{"duration", `{"text":"hello","duration":36}`, 0.001, false},
		{"zero", `{"text":"hello","duration":0}`, 0, false},
		{"missing", `{"text":"hello"}`, 0, true},
		{"malformed", `{"text":"hello","duration":"36"}`, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, tc.body) }))
			defer server.Close()
			receipts := &openAIAudioReceiptCapture{}
			gateway, _ := newOpenAIMediaIngressHandler(t, server, receipts)
			body, ct := openAIMediaIngressMultipart(t, map[string]string{"model": "whisper-1"}, "file", "speech.mp3", "audio")
			w := openAIMediaIngressRequest(t, gateway.OpenAIAudioTranslations, http.MethodPost, "/v1/audio/translations", "/v1/audio/translations", body, ct)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			require.JSONEq(t, tc.body, w.Body.String())
			require.Len(t, receipts.stored(), 1)
			r := receipts.stored()[0]
			require.InDelta(t, tc.want, r.Command.BalanceCost, 1e-12)
			if tc.review {
				require.Equal(t, service.UsageSettlementNeedsReview, r.State)
			} else {
				require.Equal(t, service.PricingStatePriced, r.Usage.PricingState)
			}
		})
	}
}

func TestTranslationRejectsUnboundedHardBudgetBeforeSend(t *testing.T) {
	fixture := &openAIMediaIngressFixture{}
	server := fixture.serve(t)
	defer server.Close()
	gateway, _ := newOpenAIMediaIngressHandler(t, server)
	body, ct := openAIMediaIngressMultipart(t, map[string]string{"model": "whisper-1"}, "file", "speech.mp3", "opaque")
	handler := func(c *gin.Context) {
		key, _ := middleware2.GetAPIKeyFromContext(c)
		key.BudgetPolicy = service.APIKeyBudgetPolicy{Mode: service.APIKeyBudgetModeDuration, DurationSeconds: 3600, LimitUSD: 1}
		key.Group.AudioSTTPricePerHour = new(float64)
		gateway.OpenAIAudioTranslations(c)
	}
	w := openAIMediaIngressRequest(t, handler, http.MethodPost, "/v1/audio/translations", "/v1/audio/translations", body, ct)
	require.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "API_KEY_BUDGET_UNAVAILABLE")
	require.Empty(t, fixture.calls())
}

func TestTranslationWithoutJournalNeverSends(t *testing.T) {
	fixture := &openAIMediaIngressFixture{}
	server := fixture.serve(t)
	defer server.Close()
	gateway, _ := newOpenAIMediaIngressHandler(t, server)
	body, ct := openAIMediaIngressMultipart(t, map[string]string{"model": "whisper-1"}, "file", "speech.mp3", "audio")
	handler := func(c *gin.Context) {
		InstallExecutionJournal(nil, nil)(c)
		if !c.IsAborted() {
			gateway.OpenAIAudioTranslations(c)
		}
	}
	w := openAIMediaIngressRequest(t, handler, http.MethodPost, "/v1/audio/translations", "/v1/audio/translations", body, ct)
	require.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
	require.Empty(t, fixture.calls())
}

func TestTranslationUnknownTransportCreatesReview(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		require.NoError(t, err)
		require.NoError(t, conn.Close())
	}))
	defer server.Close()
	receipts := &openAIAudioReceiptCapture{}
	gateway, _ := newOpenAIMediaIngressHandler(t, server, receipts)
	body, ct := openAIMediaIngressMultipart(t, map[string]string{"model": "whisper-1"}, "file", "speech.mp3", "audio")
	w := openAIMediaIngressRequest(t, gateway.OpenAIAudioTranslations, http.MethodPost, "/v1/audio/translations", "/v1/audio/translations", body, ct)
	require.NotEqual(t, http.StatusOK, w.Code, w.Body.String())
	require.Len(t, receipts.stored(), 1)
	require.Equal(t, service.UsageSettlementNeedsReview, receipts.stored()[0].State)
	require.Zero(t, receipts.stored()[0].Command.BalanceCost)
}

func TestTranslationUnresolvedResponseRetainsReview(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{name: "upstream_error", status: http.StatusInternalServerError, body: `{"error":"failed"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			receipts := &openAIAudioReceiptCapture{}
			gateway, _ := newOpenAIMediaIngressHandler(t, server, receipts)
			body, ct := openAIMediaIngressMultipart(t, map[string]string{"model": "whisper-1"}, "file", "speech.mp3", "audio")
			w := openAIMediaIngressRequest(t, gateway.OpenAIAudioTranslations, http.MethodPost, "/v1/audio/translations", "/v1/audio/translations", body, ct)
			require.NotEqual(t, http.StatusOK, w.Code, w.Body.String())
			require.Len(t, receipts.stored(), 1)
			require.Equal(t, service.UsageSettlementNeedsReview, receipts.stored()[0].State)
			require.Zero(t, receipts.stored()[0].Command.BalanceCost)
			require.Equal(t, "interrupted", receipts.stored()[0].CompletionState)
		})
	}
	t.Run("interrupted", func(t *testing.T) {
		receipts := &openAIAudioReceiptCapture{}
		server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		defer server.Close()
		gateway, _ := newOpenAIMediaIngressHandler(t, server, receipts)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/translations", nil)
		request, finishBudget := service.WithAttemptBudget(c.Request.Context(), 1, time.Minute)
		defer finishBudget()
		key := openAIMediaIngressKey()
		account := openAIMediaIngressAccount(server.URL)
		request, _ = gateway.gatewayService.CaptureRequestPlan(request, service.GatewayRequestSpec{WorkspaceID: key.WorkspaceID, UserID: key.UserID, APIKeyID: key.ID, Model: "whisper-1", Route: "/v1/audio/translations", Protocol: "audio"})
		c.Request = c.Request.WithContext(request)
		response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: &openAIMediaFailedResponseBody{}}
		gateway.writeOpenAIAudioTranslationResponse(c, zap.NewNop(), response, key, &account, nil, "whisper-1", "whisper-1", time.Now())
		require.Equal(t, http.StatusBadGateway, w.Code)
		require.Len(t, receipts.stored(), 1)
		require.Equal(t, service.UsageSettlementNeedsReview, receipts.stored()[0].State)
		require.Zero(t, receipts.stored()[0].Command.BalanceCost)
		require.Equal(t, "interrupted", receipts.stored()[0].CompletionState)
	})
}
