//go:build unit

package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// gatewayResourceHandlerRepo is a resource store fake: reads, lists and locked
// transitions are served from memory so ownership and conflict rules are
// exercised without the shared database.
type gatewayResourceHandlerRepo struct {
	service.UpstreamJobRepository
	job      *service.UpstreamJob
	jobs     []*service.UpstreamJob
	fileUsed bool
}

func (r *gatewayResourceHandlerRepo) Get(_ context.Context, id string, user, key int64) (*service.UpstreamJob, error) {
	if r.job == nil || id != r.job.PublicRequestID || user != r.job.UserID || key != r.job.APIKeyID {
		return nil, service.ErrUpstreamJobNotFound
	}
	return r.job, nil
}

func (r *gatewayResourceHandlerRepo) ListGatewayResources(_ context.Context, platform string, user, key, group int64, family, purpose, after string, limit int) ([]*service.UpstreamJob, error) {
	out := make([]*service.UpstreamJob, 0, len(r.jobs))
	for _, job := range r.jobs {
		if job.Platform != platform || job.UserID != user || job.APIKeyID != key || job.ResourceState.Deleted {
			continue
		}
		out = append(out, job)
	}
	return out, nil
}

func (r *gatewayResourceHandlerRepo) GatewayResourceFileInUse(context.Context, string) (bool, error) {
	return r.fileUsed, nil
}

func (r *gatewayResourceHandlerRepo) WithLocked(_ context.Context, id string, fn func(*service.UpstreamJob, service.UpstreamJobTx) error) (*service.UpstreamJob, error) {
	candidates := append([]*service.UpstreamJob{r.job}, r.jobs...)
	for _, job := range candidates {
		if job == nil || job.PublicRequestID != id {
			continue
		}
		if err := fn(job, nil); err != nil {
			return nil, err
		}
		return job, nil
	}
	return nil, service.ErrUpstreamJobNotFound
}

func (r *gatewayResourceHandlerRepo) Due(context.Context, time.Time, int) ([]string, error) {
	return nil, nil
}

func (r *gatewayResourceHandlerRepo) Expire(context.Context, time.Time) (int64, error) { return 0, nil }

func (r *gatewayResourceHandlerRepo) Create(context.Context, *service.UpstreamJob) (*service.UpstreamJob, bool, error) {
	return nil, false, errors.New("not used by resource reads")
}

func gatewayResourceHandlerFixture(repo *gatewayResourceHandlerRepo) *GatewayResourceHandler {
	jobs := service.NewUpstreamJobService(repo, nil, nil)
	return &GatewayResourceHandler{openAI: &OpenAIGatewayHandler{upstreamJobs: jobs, gatewayService: &service.OpenAIGatewayService{}}, upstreamJobs: jobs}
}

func gatewayResourceKey(platform string) *service.APIKey {
	groupID := int64(13)
	return &service.APIKey{
		ID: 2, UserID: 1, GroupID: &groupID,
		User:  &service.User{ID: 1, Status: service.StatusActive},
		Group: &service.Group{ID: groupID, Platform: platform, Status: service.StatusActive, RateMultiplier: 1},
	}
}

func gatewayResourceServe(t *testing.T, handler gin.HandlerFunc, method, pattern, target string, body io.Reader, contentType string, key *service.APIKey) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		if key != nil {
			c.Set(string(middleware2.ContextKeyAPIKey), key)
			c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: key.UserID, Concurrency: 1})
		}
		c.Next()
	})
	router.Handle(method, pattern, handler)
	request := httptest.NewRequest(method, target, body)
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestGatewayResourcePlatformGateHidesOtherPlatforms(t *testing.T) {
	repo := &gatewayResourceHandlerRepo{}
	h := gatewayResourceHandlerFixture(repo)

	cases := []struct {
		name, platform, method, target string
		handler                        gin.HandlerFunc
	}{
		{"anthropic group cannot list files", service.PlatformAnthropic, http.MethodGet, "/v1/files", h.ListFiles},
		{"anthropic group cannot create batches", service.PlatformAnthropic, http.MethodPost, "/v1/batches", h.CreateBatch},
		{"anthropic group cannot moderate", service.PlatformAnthropic, http.MethodPost, "/v1/moderations", h.Moderations},
		{"openai group cannot create message batches", service.PlatformOpenAI, http.MethodPost, "/v1/messages/batches", h.CreateMessageBatch},
		{"openai group cannot read message batches", service.PlatformOpenAI, http.MethodGet, "/v1/messages/batches", h.ListMessageBatches},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := gatewayResourceServe(t, testCase.handler, testCase.method, testCase.target, testCase.target, strings.NewReader("{}"), "application/json", gatewayResourceKey(testCase.platform))
			require.Equal(t, http.StatusNotFound, recorder.Code)
			require.Contains(t, recorder.Body.String(), "not_found_error")
		})
	}
}

func TestGatewayResourceListFilesRendersOwnedPublicObjects(t *testing.T) {
	groupID := int64(13)
	batchModel := "gpt-4o"
	repo := &gatewayResourceHandlerRepo{jobs: []*service.UpstreamJob{
		{
			PublicRequestID: "public-file", Platform: service.PlatformOpenAI, Family: "files", UserID: 1, APIKeyID: 2, GroupID: &groupID,
			AccountID: 42, Status: "succeeded", UpstreamTaskID: "secret-upstream-file",
			Billing: service.UpstreamJobBilling{Model: batchModel, RequestedModel: batchModel, ResourceFile: &service.GatewayResourceFileSnapshot{Purpose: "batch", Filename: "input.jsonl", Bytes: 128}},
		},
		{
			PublicRequestID: "public-batch", Platform: service.PlatformOpenAI, Family: "batch", UserID: 1, APIKeyID: 2, GroupID: &groupID,
			AccountID: 42, Status: "succeeded", UpstreamTaskID: "secret-upstream-batch", ResultRef: "secret-output-file",
			Billing: service.UpstreamJobBilling{Model: batchModel, RequestedModel: batchModel, ResourceBatch: &service.GatewayResourceBatchSnapshot{Model: batchModel, Endpoint: "/v1/chat/completions", InputFileID: "public-file", RequestCount: 1, CustomIDHashes: []string{"h"}}},
		},
	}}
	h := gatewayResourceHandlerFixture(repo)

	recorder := gatewayResourceServe(t, h.ListFiles, http.MethodGet, "/v1/files", "/v1/files", nil, "", gatewayResourceKey(service.PlatformOpenAI))
	require.Equal(t, http.StatusOK, recorder.Code)
	body := recorder.Body.String()
	require.Contains(t, body, `"object":"list"`)
	require.Contains(t, body, `"first_id":"public-file"`)
	require.NotContains(t, body, "secret-", "upstream bindings never reach the caller")

	recorder = gatewayResourceServe(t, h.ListBatches, http.MethodGet, "/v1/batches", "/v1/batches", nil, "", gatewayResourceKey(service.PlatformOpenAI))
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"object":"batch"`)
	require.Contains(t, recorder.Body.String(), `"output_file_id":"output-public-batch"`)
	require.NotContains(t, recorder.Body.String(), "secret-")

	recorder = gatewayResourceServe(t, h.ListFiles, http.MethodGet, "/v1/files", "/v1/files?limit=0", nil, "", gatewayResourceKey(service.PlatformOpenAI))
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	recorder = gatewayResourceServe(t, h.ListFiles, http.MethodGet, "/v1/files", "/v1/files?purpose=fine-tune", nil, "", gatewayResourceKey(service.PlatformOpenAI))
	require.Equal(t, http.StatusBadRequest, recorder.Code)
}

func TestGatewayResourceCrossTenantReadsAreNotFound(t *testing.T) {
	groupID := int64(13)
	repo := &gatewayResourceHandlerRepo{job: &service.UpstreamJob{
		PublicRequestID: "public-batch", Platform: service.PlatformOpenAI, Family: "batch", UserID: 1, APIKeyID: 2, GroupID: &groupID,
		AccountID: 42, Status: "running", UpstreamTaskID: "secret-upstream",
		Billing: service.UpstreamJobBilling{Model: "gpt-4o", RequestedModel: "gpt-4o", ResourceBatch: &service.GatewayResourceBatchSnapshot{Model: "gpt-4o", RequestCount: 1, CustomIDHashes: []string{"h"}}},
	}}
	h := gatewayResourceHandlerFixture(repo)

	recorder := gatewayResourceServe(t, h.GetBatch, http.MethodGet, "/v1/batches/:batch_id", "/v1/batches/public-batch", nil, "", gatewayResourceKey(service.PlatformOpenAI))
	require.Equal(t, http.StatusOK, recorder.Code)

	otherKey := gatewayResourceKey(service.PlatformOpenAI)
	otherKey.ID = 99
	recorder = gatewayResourceServe(t, h.GetBatch, http.MethodGet, "/v1/batches/:batch_id", "/v1/batches/public-batch", nil, "", otherKey)
	require.Equal(t, http.StatusNotFound, recorder.Code, "another key sees the same response as a missing batch")

	recorder = gatewayResourceServe(t, h.GetBatch, http.MethodGet, "/v1/batches/:batch_id", "/v1/batches/unknown-batch", nil, "", gatewayResourceKey(service.PlatformOpenAI))
	require.Equal(t, http.StatusNotFound, recorder.Code)

	recorder = gatewayResourceServe(t, h.DeleteFile, http.MethodDelete, "/v1/files/:file_id", "/v1/files/public-batch", nil, "", otherKey)
	require.Equal(t, http.StatusNotFound, recorder.Code)
	require.NotContains(t, recorder.Body.String(), "secret-")
}

func TestGatewayResourceDeleteRespectsBatchInputUse(t *testing.T) {
	groupID := int64(13)
	repo := &gatewayResourceHandlerRepo{
		fileUsed: true,
		job: &service.UpstreamJob{
			PublicRequestID: "public-file", Platform: service.PlatformOpenAI, Family: "files", UserID: 1, APIKeyID: 2, GroupID: &groupID,
			AccountID: 42, Status: "succeeded", UpstreamTaskID: "secret-upstream",
			Billing: service.UpstreamJobBilling{ResourceFile: &service.GatewayResourceFileSnapshot{Purpose: "batch"}},
		},
	}
	h := gatewayResourceHandlerFixture(repo)
	recorder := gatewayResourceServe(t, h.DeleteFile, http.MethodDelete, "/v1/files/:file_id", "/v1/files/public-file", nil, "", gatewayResourceKey(service.PlatformOpenAI))
	require.Equal(t, http.StatusConflict, recorder.Code, "an input file referenced by a live batch cannot be deleted")
	require.Contains(t, recorder.Body.String(), "resource_conflict")
}

func TestGatewayResourceFileUploadRejectsMalformedBodies(t *testing.T) {
	repo := &gatewayResourceHandlerRepo{}
	h := gatewayResourceHandlerFixture(repo)
	key := gatewayResourceKey(service.PlatformOpenAI)

	recorder := gatewayResourceServe(t, h.UploadFile, http.MethodPost, "/v1/files", "/v1/files", strings.NewReader(`{"purpose":"batch"}`), "application/json", key)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), "multipart/form-data")

	var body bytes.Buffer
	multipartWriter := multipart.NewWriter(&body)
	require.NoError(t, multipartWriter.WriteField("purpose", "batch"))
	part, err := multipartWriter.CreateFormFile("file", "input.jsonl")
	require.NoError(t, err)
	_, err = part.Write(make([]byte, service.GatewayResourceFileLimit+1))
	require.NoError(t, err)
	require.NoError(t, multipartWriter.Close())
	recorder = gatewayResourceServe(t, h.UploadFile, http.MethodPost, "/v1/files", "/v1/files", bytes.NewReader(body.Bytes()), multipartWriter.FormDataContentType(), key)
	require.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code, "an oversized upload is refused explicitly")
	require.Contains(t, recorder.Body.String(), "request_too_large")
}

func TestGatewayResourceBatchCreateRejectsUnsupportedTargets(t *testing.T) {
	groupID := int64(13)
	repo := &gatewayResourceHandlerRepo{job: &service.UpstreamJob{
		PublicRequestID: "public-file", Platform: service.PlatformOpenAI, Family: "files", UserID: 1, APIKeyID: 2, GroupID: &groupID,
		AccountID: 42, Status: "succeeded", UpstreamTaskID: "secret-upstream",
		Billing: service.UpstreamJobBilling{ResourceFile: &service.GatewayResourceFileSnapshot{Purpose: "batch", BatchModel: "gpt-4o", BatchEndpoint: "/v1/chat/completions", Lines: 1, CustomIDHashes: []string{"h"}}},
	}}
	h := gatewayResourceHandlerFixture(repo)
	key := gatewayResourceKey(service.PlatformOpenAI)

	cases := []struct {
		name   string
		body   string
		status int
	}{
		{"dedicated pipeline endpoint", `{"input_file_id":"public-file","endpoint":"/v1/images/generations","completion_window":"24h"}`, http.StatusUnprocessableEntity},
		{"unknown endpoint", `{"input_file_id":"public-file","endpoint":"/v1/unknown","completion_window":"24h"}`, http.StatusUnprocessableEntity},
		{"unknown fields", `{"input_file_id":"public-file","endpoint":"/v1/chat/completions","completion_window":"24h","model":"gpt-4o"}`, http.StatusUnprocessableEntity},
		{"wrong window", `{"input_file_id":"public-file","endpoint":"/v1/chat/completions","completion_window":"1h"}`, http.StatusUnprocessableEntity},
		{"foreign input file", `{"input_file_id":"00000000-0000-0000-0000-000000000000","endpoint":"/v1/chat/completions","completion_window":"24h"}`, http.StatusNotFound},
		{"endpoint mismatch", `{"input_file_id":"public-file","endpoint":"/v1/responses","completion_window":"24h"}`, http.StatusUnprocessableEntity},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := gatewayResourceServe(t, h.CreateBatch, http.MethodPost, "/v1/batches", "/v1/batches", strings.NewReader(testCase.body), "application/json", key)
			require.Equal(t, testCase.status, recorder.Code, recorder.Body.String())
		})
	}
}

func TestGatewayResourceMessageBatchCreateRejectsInvalidBodies(t *testing.T) {
	repo := &gatewayResourceHandlerRepo{}
	h := gatewayResourceHandlerFixture(repo)
	key := gatewayResourceKey(service.PlatformAnthropic)

	cases := map[string]string{
		"missing requests":  `{"requests":[]}`,
		"streaming request": `{"requests":[{"custom_id":"a","params":{"model":"claude-sonnet-4-5","max_tokens":8,"messages":[{"role":"user","content":"hi"}],"stream":true}}]}`,
		"mixed models":      `{"requests":[{"custom_id":"a","params":{"model":"claude-sonnet-4-5","max_tokens":8,"messages":[{"role":"user","content":"hi"}]}},{"custom_id":"b","params":{"model":"claude-haiku-4-5","max_tokens":8,"messages":[{"role":"user","content":"hi"}]}}]}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			recorder := gatewayResourceServe(t, h.CreateMessageBatch, http.MethodPost, "/v1/messages/batches", "/v1/messages/batches", strings.NewReader(body), "application/json", key)
			require.Equal(t, http.StatusUnprocessableEntity, recorder.Code, recorder.Body.String())
			require.Contains(t, recorder.Body.String(), `"type":"error"`, "message batch errors use the Anthropic envelope")
		})
	}
}

func TestGatewayResourceMessageBatchObjectsFollowAnthropicShape(t *testing.T) {
	groupID := int64(13)
	job := &service.UpstreamJob{
		PublicRequestID: "public-batch", Platform: service.PlatformAnthropic, Family: "batch", UserID: 1, APIKeyID: 2, GroupID: &groupID,
		AccountID: 42, Status: "running", UpstreamTaskID: "msgbatch_secret", SubmittedAt: time.Unix(1700000000, 0).UTC(), ExpiresAt: time.Unix(1700086400, 0).UTC(),
		Billing:   service.UpstreamJobBilling{Model: "claude-sonnet-4-5", RequestedModel: "claude-sonnet-4-5", ResourceBatch: &service.GatewayResourceBatchSnapshot{Model: "claude-sonnet-4-5", RequestCount: 1, CustomIDHashes: []string{"h"}}},
		UsageJSON: []byte(`{"request_counts":{"processing":1,"succeeded":0,"errored":0,"canceled":0,"expired":0},"provider_status":"in_progress","result":{"lines":0,"matched":0}}`),
	}
	encoded, err := json.Marshal(gatewayResourceMessageBatchObject(job))
	require.NoError(t, err)
	body := string(encoded)
	require.Contains(t, body, `"type":"message_batch"`)
	require.Contains(t, body, `"processing_status":"in_progress"`)
	require.Contains(t, body, `"results_url":"/v1/messages/batches/public-batch/results"`)
	require.NotContains(t, body, "msgbatch_secret")

	job.ResourceState.CancelRequested = true
	encoded, err = json.Marshal(gatewayResourceMessageBatchObject(job))
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"processing_status":"canceling"`)

	job.Status = "succeeded"
	job.FinishedAt = gatewayResourcePtrTime(time.Unix(1700003600, 0).UTC())
	encoded, err = json.Marshal(gatewayResourceMessageBatchObject(job))
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"processing_status":"ended"`)
	require.Contains(t, string(encoded), `"ended_at":"2023-11-14T23:13:20Z`)
}

func TestGatewayResourceBatchObjectsMapDurableStates(t *testing.T) {
	groupID := int64(13)
	job := &service.UpstreamJob{
		PublicRequestID: "public-batch", Platform: service.PlatformOpenAI, Family: "batch", UserID: 1, APIKeyID: 2, GroupID: &groupID,
		AccountID: 42, Status: "running", UpstreamTaskID: "batch_secret", ResultRef: "file_secret", SubmittedAt: time.Unix(1700000000, 0).UTC(), ExpiresAt: time.Unix(1700086400, 0).UTC(),
		Billing:   service.UpstreamJobBilling{Model: "gpt-4o", RequestedModel: "gpt-4o", ResourceBatch: &service.GatewayResourceBatchSnapshot{Model: "gpt-4o", Endpoint: "/v1/chat/completions", InputFileID: "public-file", RequestCount: 2, CustomIDHashes: []string{"a", "b"}}},
		UsageJSON: []byte(`{"request_counts":{"total":2,"completed":0,"failed":0},"result":{"lines":0,"matched":0}}`),
	}
	encoded, err := json.Marshal(gatewayResourceBatchObject(job))
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"status":"in_progress"`)
	require.Contains(t, string(encoded), `"input_file_id":"public-file"`)
	require.Contains(t, string(encoded), `"completion_window":"24h"`)
	require.Contains(t, string(encoded), `"request_counts":{"completed":0,"failed":0,"total":2}`)
	require.NotContains(t, string(encoded), "file_secret")

	job.Status = "succeeded"
	job.ChargedUsageLogID = gatewayResourcePtrInt64(7)
	encoded, err = json.Marshal(gatewayResourceBatchObject(job))
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"status":"completed"`)
	require.Contains(t, string(encoded), `"output_file_id":"output-public-batch"`)

	job.Status = "failed"
	job.ErrorCode = "upstream_cancelled"
	job.ErrorMessage = "cancelled upstream"
	encoded, err = json.Marshal(gatewayResourceBatchObject(job))
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"status":"cancelled"`)
	require.Contains(t, string(encoded), `"code":"upstream_cancelled"`)

	job.ResourceState.OutputDeleted = true
	encoded, err = json.Marshal(gatewayResourceBatchObject(job))
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"output_file_id":null`)
}

func TestGatewayResourceDownloadBoundAbortsTruncatedContent(t *testing.T) {
	h := gatewayResourceHandlerFixture(&gatewayResourceHandlerRepo{})
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	h.streamResourceContent(c, &http.Response{ContentLength: service.GatewayResourceDownloadLimit + 1, Body: io.NopCloser(strings.NewReader(""))}, "application/jsonl")
	require.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)

	recorder = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(recorder)
	h.streamResourceContent(c, &http.Response{ContentLength: 7, Body: io.NopCloser(strings.NewReader("payload"))}, "application/jsonl")
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "payload", recorder.Body.String())
	require.Equal(t, "application/jsonl", recorder.Header().Get("Content-Type"))

	recorder = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(recorder)
	require.PanicsWithValue(t, http.ErrAbortHandler, func() {
		h.streamResourceContent(c, &http.Response{ContentLength: -1, Body: io.NopCloser(erroringGatewayResourceReader{})}, "application/jsonl")
	}, "an interrupted upstream stream aborts instead of presenting a truncated body")
}

type erroringGatewayResourceReader struct{}

func (erroringGatewayResourceReader) Read([]byte) (int, error) {
	return 0, errors.New("upstream stream interrupted")
}

func gatewayResourcePtrTime(value time.Time) *time.Time { return &value }

func gatewayResourcePtrInt64(value int64) *int64 { return &value }
