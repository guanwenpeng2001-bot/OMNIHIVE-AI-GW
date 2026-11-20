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
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/tlsfingerprint"
	middleware2 "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

// gatewayResourceWiringRepo is the full durable job store of the wiring test:
// claims, owner-scoped reads, locked transitions and settlement all happen in
// memory, so the standard resource actions run through the real handler, job
// service and provider adapter without a shared database.
type gatewayResourceWiringRepo struct {
	mu      sync.Mutex
	jobs    map[string]*service.UpstreamJob
	claims  map[string]string
	charges int
}

func newGatewayResourceWiringRepo() *gatewayResourceWiringRepo {
	return &gatewayResourceWiringRepo{jobs: map[string]*service.UpstreamJob{}, claims: map[string]string{}}
}

func cloneGatewayResourceJob(job *service.UpstreamJob) *service.UpstreamJob {
	raw, _ := json.Marshal(job)
	var copy service.UpstreamJob
	_ = json.Unmarshal(raw, &copy)
	return &copy
}

func (r *gatewayResourceWiringRepo) Create(_ context.Context, job *service.UpstreamJob) (*service.UpstreamJob, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if id := r.claims[job.SubmissionKey]; id != "" {
		return cloneGatewayResourceJob(r.jobs[id]), false, nil
	}
	job.ID = int64(len(r.jobs) + 1)
	r.claims[job.SubmissionKey] = job.PublicRequestID
	r.jobs[job.PublicRequestID] = cloneGatewayResourceJob(job)
	return cloneGatewayResourceJob(job), true, nil
}

func (r *gatewayResourceWiringRepo) Get(_ context.Context, id string, user, key int64) (*service.UpstreamJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	job := r.jobs[id]
	if job == nil || job.UserID != user || job.APIKeyID != key {
		return nil, service.ErrUpstreamJobNotFound
	}
	return cloneGatewayResourceJob(job), nil
}

func (r *gatewayResourceWiringRepo) Due(context.Context, time.Time, int) ([]string, error) {
	return nil, nil
}

func (r *gatewayResourceWiringRepo) Expire(context.Context, time.Time) (int64, error) { return 0, nil }

// WithLocked clones the row, runs the transition and stores the result. The real
// store holds a row lock for the duration; the fake keeps no lock while the
// callback runs, because the callback submits a child job through the same store
// (a different row in PostgreSQL).
func (r *gatewayResourceWiringRepo) WithLocked(_ context.Context, id string, fn func(*service.UpstreamJob, service.UpstreamJobTx) error) (*service.UpstreamJob, error) {
	r.mu.Lock()
	job := cloneGatewayResourceJob(r.jobs[id])
	r.mu.Unlock()
	if job == nil || job.PublicRequestID == "" {
		return nil, service.ErrUpstreamJobNotFound
	}
	if err := fn(job, r); err != nil {
		return nil, err
	}
	r.mu.Lock()
	r.jobs[id] = job
	stored := cloneGatewayResourceJob(job)
	r.mu.Unlock()
	return stored, nil
}

// Charge runs inside WithLocked, so it never re-acquires the store lock.
func (r *gatewayResourceWiringRepo) Charge(_ context.Context, log *service.UsageLog, _ *service.UsageBillingCommand) error {
	r.charges++
	log.ID = int64(r.charges)
	return nil
}

func (r *gatewayResourceWiringRepo) ListGatewayResources(_ context.Context, platform string, user, key, group int64, family, purpose, after string, limit int) ([]*service.UpstreamJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []*service.UpstreamJob{}
	for _, job := range r.jobs {
		if job.Platform != platform || job.UserID != user || job.APIKeyID != key || job.GroupID == nil || *job.GroupID != group || job.ResourceState.Deleted {
			continue
		}
		switch family {
		case "files":
			if job.Family != "files" {
				continue
			}
			if purpose != "" && (job.Billing.ResourceFile == nil || job.Billing.ResourceFile.Purpose != purpose) {
				continue
			}
		case "batch":
			if job.Family != "batch" {
				continue
			}
		case "batch_output":
			if job.Family != "batch" || job.Status != "succeeded" || job.ResultRef == "" || job.ResourceState.OutputDeleted {
				continue
			}
		default:
			continue
		}
		if after != "" && job.PublicRequestID >= after {
			continue
		}
		out = append(out, cloneGatewayResourceJob(job))
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (r *gatewayResourceWiringRepo) GatewayResourceFileInUse(context.Context, string) (bool, error) {
	return false, nil
}

// dueNow makes the stored row due for polling without waiting for the retry
// backoff scheduled at submission.
func (r *gatewayResourceWiringRepo) dueNow(publicID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if job := r.jobs[publicID]; job != nil {
		job.NextPollAt = time.Now().Add(-time.Minute)
	}
}

func (r *gatewayResourceWiringRepo) snapshot(publicID string) *service.UpstreamJob {
	r.mu.Lock()
	defer r.mu.Unlock()
	return cloneGatewayResourceJob(r.jobs[publicID])
}

// gatewayResourceWiringUpstream performs real HTTP against the loopback fixture
// so the resource actions exercise the provider adapter end to end.
type gatewayResourceWiringUpstream struct {
	client     *http.Client
	beforeSend func(*http.Request)
}

func (u gatewayResourceWiringUpstream) Do(request *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	if u.beforeSend != nil {
		u.beforeSend(request)
	}
	return u.client.Do(request)
}

func (u gatewayResourceWiringUpstream) DoWithTLS(request *http.Request, proxyURL string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(request, proxyURL, accountID, concurrency)
}

func requireGatewaySubmissionPlan(t *testing.T, request *http.Request, model string) {
	t.Helper()
	plan := service.GatewayRequestPlanFromContext(request.Context())
	require.NotNil(t, plan, "the real ingress must supply the submission plan")
	spec := plan.Snapshot()
	require.Equal(t, "job", spec.ExecutionKind)
	require.NotEmpty(t, spec.ExecutionID)
	require.EqualValues(t, 20, spec.APIKeyID)
	require.EqualValues(t, 31, spec.BillingGroupID)
	require.EqualValues(t, 7, spec.UpstreamAccountID)
	require.Equal(t, model, spec.Model)
	require.False(t, plan.State().Finalized)
}

// gatewayResourceWiringFixture serves the documented OpenAI resource wire shapes
// and records every upstream request for assertions.
type gatewayResourceWiringFixture struct {
	mu           sync.Mutex
	requests     []string
	bodies       map[string][]byte
	upload       string
	batch        string
	batchPoll    string
	uploadStatus int
	batchStatus  int
}

func (f *gatewayResourceWiringFixture) record(request *http.Request) {
	body, _ := io.ReadAll(request.Body)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, request.Method+" "+request.URL.Path)
	f.bodies[request.Method+" "+request.URL.Path] = body
}

func (f *gatewayResourceWiringFixture) calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.requests...)
}

func (f *gatewayResourceWiringFixture) bodyOf(key string) []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]byte(nil), f.bodies[key]...)
}

func (f *gatewayResourceWiringFixture) serve(t *testing.T) *httptest.Server {
	t.Helper()
	f.bodies = map[string][]byte{}
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		f.record(request)
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodPost && request.URL.Path == "/v1/files":
			if f.uploadStatus != 0 {
				writer.WriteHeader(f.uploadStatus)
			}
			_, _ = io.WriteString(writer, f.upload)
		case request.Method == http.MethodGet && strings.HasSuffix(request.URL.Path, "/content"):
			writer.Header().Set("Content-Type", "application/jsonl")
			_, _ = io.WriteString(writer, "{\"custom_id\":\"row-1\",\"response\":{\"status_code\":200,\"body\":{\"model\":\"gpt-5.4\",\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15}}},\"error\":null}\n")
		case request.Method == http.MethodPost && request.URL.Path == "/v1/batches":
			if f.batchStatus != 0 {
				writer.WriteHeader(f.batchStatus)
			}
			_, _ = io.WriteString(writer, f.batch)
		case request.Method == http.MethodGet && strings.HasPrefix(request.URL.Path, "/v1/batches/"):
			if f.batchPoll != "" {
				_, _ = io.WriteString(writer, f.batchPoll)
				return
			}
			_, _ = io.WriteString(writer, f.batch)
		default:
			http.NotFound(writer, request)
		}
	}))
}

// gatewayResourceWiringAccount is an OpenAI API-key account with catalogue
// evidence for the batch model, so model authorization is exercised for real.
func gatewayResourceWiringAccount(baseURL string) service.Account {
	catalog := service.AccountModelCatalogSnapshot{
		Complete: true, Revision: 1, DiscoveredModels: []string{gatewayResourceWiringModel},
		EligibleModels: []service.AccountCatalogModel{{
			ID: gatewayResourceWiringModel, Endpoints: []string{"chat_completions", "responses"},
			EvidenceKind: "fixture", EvidenceObservedAt: "2026-09-21T00:00:00Z",
		}},
	}
	return service.Account{
		ID: 7, Name: "resource-wiring", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true, Concurrency: 20, GroupIDs: []int64{31},
		Credentials: map[string]any{"api_key": "sk-wiring", "base_url": baseURL},
		Extra:       map[string]any{service.UpstreamModelCatalogExtraKey: catalog},
	}
}

func newGatewayResourceWiringHandler(t *testing.T, server *httptest.Server) (*GatewayResourceHandler, *gatewayResourceWiringRepo) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	repo := newGatewayResourceWiringRepo()
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	cfg.Gateway.Scheduling.StickySessionWaitTimeout = 20 * time.Millisecond
	cfg.Gateway.Scheduling.StickySessionMaxWaiting = 3
	concurrency := service.NewConcurrencyService(&grokMediaSlotsCache{accounts: map[string]int64{}, users: map[string]int64{}})
	billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billing.Stop)
	gateway := service.ProvideOpenAIGatewayService(
		openAIImagesFailoverAccountRepo{accounts: []service.Account{gatewayResourceWiringAccount(server.URL)}},
		nil, nil, nil, nil, nil, nil, cfg, nil, concurrency,
		service.NewBillingService(cfg, nil), nil, nil,
		gatewayResourceWiringUpstream{client: server.Client(), beforeSend: func(request *http.Request) {
			if request.Method == http.MethodPost && request.URL.Path == "/v1/batches" {
				requireGatewaySubmissionPlan(t, request, gatewayResourceWiringModel)
			}
		}},
		nil, nil, nil, nil, nil, nil, nil, nil, service.NewUsageSettlementService(&unlimitedTPMReceiptStub{}, nil, nil))
	openAI := NewOpenAIGatewayHandler(gateway, concurrency, billing, service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, cfg)
	jobs := service.NewUpstreamJobService(repo, service.GatewayResourcePollJob(gateway), service.GatewayResourceJobCharge)
	openAI.upstreamJobs = jobs
	return NewGatewayResourceHandler(openAI, jobs), repo
}

func gatewayResourceWiringKey(platform string) *service.APIKey {
	groupID := int64(31)
	return &service.APIKey{
		ID: 20, UserID: 10, GroupID: &groupID,
		User:  &service.User{ID: 10, Status: service.StatusActive},
		Group: &service.Group{ID: groupID, Platform: platform, Status: service.StatusActive, RateMultiplier: 1},
	}
}

func gatewayResourceWiringRequest(t *testing.T, handler gin.HandlerFunc, method, pattern, target string, body []byte, contentType, platform string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	key := gatewayResourceWiringKey(platform)
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyAPIKey), key)
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: key.UserID, Concurrency: 5})
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

func gatewayResourceWiringMultipart(t *testing.T, purpose, filename, content string) ([]byte, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("purpose", purpose))
	part, err := writer.CreateFormFile("file", filename)
	require.NoError(t, err)
	_, err = part.Write([]byte(content))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	return body.Bytes(), writer.FormDataContentType()
}

const (
	gatewayResourceWiringModel = "gpt-5.4"
	gatewayResourceWiringLine  = `{"custom_id":"row-1","method":"POST","url":"/v1/chat/completions","body":{"model":"gpt-5.4","messages":[{"role":"user","content":"hi"}]}}`
)

func TestGatewayResourceWiringUploadsFileAndCreatesBatchWithProviderFileID(t *testing.T) {
	fixture := &gatewayResourceWiringFixture{
		upload:    `{"id":"file_upstream","object":"file","purpose":"batch","status":"processed","bytes":128,"filename":"input.jsonl"}`,
		batch:     `{"id":"batch_upstream","object":"batch","status":"validating","endpoint":"/v1/chat/completions","input_file_id":"file_upstream","completion_window":"24h","request_counts":{"total":1,"completed":0,"failed":0}}`,
		batchPoll: `{"id":"batch_upstream","object":"batch","status":"completed","endpoint":"/v1/chat/completions","input_file_id":"file_upstream","output_file_id":"file_output","completion_window":"24h","request_counts":{"total":1,"completed":1,"failed":0}}`,
	}
	server := fixture.serve(t)
	defer server.Close()
	h, repo := newGatewayResourceWiringHandler(t, server)

	uploadBody, contentType := gatewayResourceWiringMultipart(t, "batch", "input.jsonl", gatewayResourceWiringLine+"\n")
	recorder := gatewayResourceWiringRequest(t, h.UploadFile, http.MethodPost, "/v1/files", "/v1/files", uploadBody, contentType, service.PlatformOpenAI)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var uploaded map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &uploaded))
	require.Equal(t, "file", uploaded["object"])
	require.Equal(t, "processed", uploaded["status"])
	require.Equal(t, "batch", uploaded["purpose"])
	publicFileID, _ := uploaded["id"].(string)
	require.NotEmpty(t, publicFileID)
	require.NotEqual(t, "file_upstream", publicFileID, "the upstream file id never reaches the caller")
	require.Contains(t, fixture.calls(), "POST /v1/files")
	require.Contains(t, string(fixture.bodyOf("POST /v1/files")), "row-1", "the multipart upload is forwarded verbatim")

	stored := repo.snapshot(publicFileID)
	require.NotNil(t, stored)
	require.Equal(t, "file_upstream", stored.UpstreamTaskID)
	require.NotNil(t, stored.ChargedUsageLogID, "a stored file settles as an explicitly free job")

	recorder = gatewayResourceWiringRequest(t, h.ListFiles, http.MethodGet, "/v1/files", "/v1/files", nil, "", service.PlatformOpenAI)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"id":"`+publicFileID+`"`)
	require.NotContains(t, recorder.Body.String(), "file_upstream")

	createBody := []byte(`{"input_file_id":"` + publicFileID + `","endpoint":"/v1/chat/completions","completion_window":"24h"}`)
	recorder = gatewayResourceWiringRequest(t, h.CreateBatch, http.MethodPost, "/v1/batches", "/v1/batches", createBody, "application/json", service.PlatformOpenAI)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Contains(t, recorder.Body.String(), `"object":"batch"`)
	require.Contains(t, recorder.Body.String(), `"input_file_id":"`+publicFileID+`"`)
	require.NotContains(t, recorder.Body.String(), "batch_upstream")
	require.NotContains(t, recorder.Body.String(), "file_upstream")

	var upstreamCreate map[string]any
	require.NoError(t, json.Unmarshal(fixture.bodyOf("POST /v1/batches"), &upstreamCreate))
	require.Equal(t, "file_upstream", upstreamCreate["input_file_id"], "the provider receives its own file id, never the public one")
	require.Equal(t, "/v1/chat/completions", upstreamCreate["endpoint"])
	require.Equal(t, "24h", upstreamCreate["completion_window"])

	var batchObject map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &batchObject))
	publicBatchID, _ := batchObject["id"].(string)
	require.NotEmpty(t, publicBatchID)

	recorder = gatewayResourceWiringRequest(t, h.GetBatch, http.MethodGet, "/v1/batches/:batch_id", "/v1/batches/"+publicBatchID, nil, "", service.PlatformOpenAI)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"status":"validating"`)

	// The batch is polled through the shared job service and settles from the
	// validated output file, so the client-facing output id is the virtual one.
	repo.dueNow(publicBatchID)
	job, err := h.upstreamJobs.Poll(context.Background(), publicBatchID)
	require.NoError(t, err)
	require.Equal(t, "succeeded", job.Status)
	require.NotNil(t, job.ChargedUsageLogID)
	recorder = gatewayResourceWiringRequest(t, h.GetBatch, http.MethodGet, "/v1/batches/:batch_id", "/v1/batches/"+publicBatchID, nil, "", service.PlatformOpenAI)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"status":"completed"`)
	require.Contains(t, recorder.Body.String(), `"output_file_id":"output-`+publicBatchID+`"`)
	require.Contains(t, recorder.Body.String(), `"request_counts":{"completed":1,"failed":0,"total":1}`)

	recorder = gatewayResourceWiringRequest(t, h.GetFile, http.MethodGet, "/v1/files/:file_id", "/v1/files/output-"+publicBatchID, nil, "", service.PlatformOpenAI)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"purpose":"batch_output"`)

	recorder = gatewayResourceWiringRequest(t, h.DownloadFileContent, http.MethodGet, "/v1/files/:file_id/content", "/v1/files/output-"+publicBatchID+"/content", nil, "", service.PlatformOpenAI)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"custom_id":"row-1"`)
	require.Contains(t, fixture.calls(), "GET /v1/files/file_output/content", "the client download streams the provider output file")
}

func TestGatewayResourceWiringProviderRejectionUsesTheErrorEnvelope(t *testing.T) {
	fixture := &gatewayResourceWiringFixture{uploadStatus: http.StatusBadRequest, upload: `{"error":{"message":"invalid purpose","type":"invalid_request_error"}}`}
	server := fixture.serve(t)
	defer server.Close()
	h, repo := newGatewayResourceWiringHandler(t, server)

	uploadBody, contentType := gatewayResourceWiringMultipart(t, "batch", "input.jsonl", gatewayResourceWiringLine+"\n")
	recorder := gatewayResourceWiringRequest(t, h.UploadFile, http.MethodPost, "/v1/files", "/v1/files", uploadBody, contentType, service.PlatformOpenAI)
	require.Equal(t, http.StatusUnprocessableEntity, recorder.Code, recorder.Body.String())
	require.Contains(t, recorder.Body.String(), `"code":"upstream_rejected"`)
	require.Contains(t, recorder.Body.String(), `"error"`)
	require.NotContains(t, recorder.Body.String(), `"object":"file"`, "a rejected upload never answers with a file object")

	// The durable receipt is retained even though the client saw an error.
	repo.mu.Lock()
	require.Len(t, repo.jobs, 1)
	for _, job := range repo.jobs {
		require.Equal(t, "failed", job.Status)
		require.Equal(t, "upstream_rejected", job.ErrorCode)
	}
	repo.mu.Unlock()
}

func TestGatewayResourceWiringDeletedResourcesAreNotFound(t *testing.T) {
	fixture := &gatewayResourceWiringFixture{
		upload: `{"id":"file_upstream","object":"file","purpose":"batch","status":"processed"}`,
		batch:  `{"id":"batch_upstream","object":"batch","status":"validating","endpoint":"/v1/chat/completions","input_file_id":"file_upstream","completion_window":"24h","request_counts":{"total":1,"completed":0,"failed":0}}`,
	}
	server := fixture.serve(t)
	defer server.Close()
	h, repo := newGatewayResourceWiringHandler(t, server)

	uploadBody, contentType := gatewayResourceWiringMultipart(t, "batch", "input.jsonl", gatewayResourceWiringLine+"\n")
	recorder := gatewayResourceWiringRequest(t, h.UploadFile, http.MethodPost, "/v1/files", "/v1/files", uploadBody, contentType, service.PlatformOpenAI)
	require.Equal(t, http.StatusOK, recorder.Code)
	var uploaded map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &uploaded))
	publicFileID, _ := uploaded["id"].(string)

	recorder = gatewayResourceWiringRequest(t, h.DeleteFile, http.MethodDelete, "/v1/files/:file_id", "/v1/files/"+publicFileID, nil, "", service.PlatformOpenAI)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"deleted":true`)
	require.Contains(t, fixture.calls(), "DELETE /v1/files/file_upstream")

	recorder = gatewayResourceWiringRequest(t, h.GetFile, http.MethodGet, "/v1/files/:file_id", "/v1/files/"+publicFileID, nil, "", service.PlatformOpenAI)
	require.Equal(t, http.StatusNotFound, recorder.Code, "a deleted file is reported exactly like a missing one")
	require.NotContains(t, recorder.Body.String(), "deleted")

	recorder = gatewayResourceWiringRequest(t, h.DownloadFileContent, http.MethodGet, "/v1/files/:file_id/content", "/v1/files/"+publicFileID+"/content", nil, "", service.PlatformOpenAI)
	require.Equal(t, http.StatusNotFound, recorder.Code)

	// A second delete stays idempotent instead of inventing a second upstream call.
	repo.mu.Lock()
	chargesBefore := repo.charges
	repo.mu.Unlock()
	recorder = gatewayResourceWiringRequest(t, h.DeleteFile, http.MethodDelete, "/v1/files/:file_id", "/v1/files/"+publicFileID, nil, "", service.PlatformOpenAI)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Len(t, fixture.calls(), 2, "the retry does not call the provider again")
	repo.mu.Lock()
	require.Equal(t, chargesBefore, repo.charges, "a repeated delete never charges the resource again")
	repo.mu.Unlock()
}

func TestGatewayResourceWiringCompositeGroupUsesTheOpenAISurface(t *testing.T) {
	fixture := &gatewayResourceWiringFixture{}
	server := fixture.serve(t)
	defer server.Close()
	h, _ := newGatewayResourceWiringHandler(t, server)

	recorder := gatewayResourceWiringRequest(t, h.ListFiles, http.MethodGet, "/v1/files", "/v1/files", nil, "", service.PlatformComposite)
	require.Equal(t, http.StatusOK, recorder.Code, "a composite group is served by the OpenAI resource surface")
	require.Contains(t, recorder.Body.String(), `"object":"list"`)

	recorder = gatewayResourceWiringRequest(t, h.ListBatches, http.MethodGet, "/v1/batches", "/v1/batches?order=asc", nil, "", service.PlatformOpenAI)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), "order must be desc")

	recorder = gatewayResourceWiringRequest(t, h.ListBatches, http.MethodGet, "/v1/batches", "/v1/batches?order=desc", nil, "", service.PlatformOpenAI)
	require.Equal(t, http.StatusOK, recorder.Code)

	recorder = gatewayResourceWiringRequest(t, h.ListFiles, http.MethodGet, "/v1/files", "/v1/files?after=00000000-0000-0000-0000-000000000000", nil, "", service.PlatformOpenAI)
	require.Equal(t, http.StatusBadRequest, recorder.Code, "an unknown cursor is reported instead of an empty page")
	require.Contains(t, recorder.Body.String(), "invalid_pagination")

	recorder = gatewayResourceWiringRequest(t, h.ListMessageBatches, http.MethodGet, "/v1/messages/batches", "/v1/messages/batches?before_id=msgbatch_x", nil, "", service.PlatformAnthropic)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), "before_id is not supported")

	recorder = gatewayResourceWiringRequest(t, h.ListMessageBatches, http.MethodGet, "/v1/messages/batches", "/v1/messages/batches?after_id=msgbatch_x", nil, "", service.PlatformAnthropic)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), "invalid_pagination")

	recorder = gatewayResourceWiringRequest(t, h.ListMessageBatches, http.MethodGet, "/v1/messages/batches", "/v1/messages/batches", nil, "", service.PlatformAnthropic)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"has_more":false`)
}
