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
	"testing"

	"github.com/gin-gonic/gin"
	middleware2 "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type standardVideoSubmitRepository struct {
	service.UpstreamJobRepository
	job *service.UpstreamJob
}

func (repo *standardVideoSubmitRepository) Create(_ context.Context, job *service.UpstreamJob) (*service.UpstreamJob, bool, error) {
	copy := *job
	repo.job = &copy
	return repo.job, true, nil
}

func (repo *standardVideoSubmitRepository) WithLocked(_ context.Context, _ string, update func(*service.UpstreamJob, service.UpstreamJobTx) error) (*service.UpstreamJob, error) {
	return repo.job, update(repo.job, nil)
}

func TestQianwenStandardVideoCreateUsesDurableNativeSubmission(t *testing.T) {
	upstream := &httpUpstreamRecorderStub{body: `{"output":{"task_id":"private-task","task_status":"PENDING"}}`}
	account := qianwenCatalogAccount(56, "wan2.6-t2v", "chat_completions")
	account.Credentials["model_mapping"] = map[string]any{"wan2.6-t2v": "wan2.6-t2v"}
	handler := newQianwenStandardTestHandler(t, upstream, []service.Account{account})
	repo := &standardVideoSubmitRepository{}
	handler.upstreamJobs = service.NewUpstreamJobService(repo, nil, nil)
	group := &service.Group{ID: 7, Platform: service.PlatformQianwen, RateMultiplier: 1, VideoModelPrices: map[string]map[string]float64{"wan2.6-t2v": {"720p": 0.1}}}
	ctx, response := qianwenStandardContext(t, http.MethodPost, "/v1/videos", []byte(`{"model":"wan2.6-t2v","prompt":"A cat","seconds":"5","size":"1280x720"}`), group)
	handler.QianwenVideoGeneration(ctx)
	require.Equal(t, 202, response.Code, response.Body.String())
	require.NotNil(t, repo.job)
	require.Equal(t, repo.job.PublicRequestID, gjson.Get(response.Body.String(), "id").String())
	require.Equal(t, "queued", gjson.Get(response.Body.String(), "status").String())
	require.Equal(t, "private-task", repo.job.UpstreamTaskID)
	require.Equal(t, "video", repo.job.Family)
	require.Equal(t, int64(56), repo.job.AccountID)
	require.Equal(t, int64(5), repo.job.UserID)
	require.Equal(t, int64(3), repo.job.APIKeyID)
	require.Equal(t, 0.1, repo.job.Billing.UnitPrice)
	require.JSONEq(t, `{"model":"wan2.6-t2v","input":{"prompt":"A cat"},"parameters":{"duration":5,"size":"1280*720"}}`, string(upstream.lastBody))
	require.Equal(t, "https://dashscope.aliyuncs.com/api/v1/services/aigc/video-generation/video-synthesis", upstream.lastURL)
	require.NotContains(t, response.Body.String(), "private-task")
}

func TestQianwenStandardVideoOwnerIsolation(t *testing.T) {
	job := &service.UpstreamJob{PublicRequestID: "public", UpstreamTaskID: "private-task", UserID: 1, APIKeyID: 2, AccountID: 56, Platform: service.PlatformQianwen, Family: "video", Status: "succeeded", ResultRef: "https://dashscope-result-bj.oss-cn-beijing.aliyuncs.com/video.mp4"}
	repo := &videoJobReadRepository{job: job}
	handler := newQianwenStandardTestHandler(t, nil, []service.Account{qianwenCatalogAccount(56, "wan2.6-t2v")})
	handler.upstreamJobs = service.NewUpstreamJobService(repo, nil, nil)
	opener := qianwenVideoContentOpener
	downloads := 0
	qianwenVideoContentOpener = func(_ context.Context, account *service.Account, _ string) (*http.Response, error) {
		require.Equal(t, int64(56), account.ID)
		downloads++
		return &http.Response{Header: http.Header{"Content-Type": []string{"video/mp4"}}, ContentLength: 9, Body: io.NopCloser(strings.NewReader("mp4-bytes"))}, nil
	}
	t.Cleanup(func() { qianwenVideoContentOpener = opener })
	for _, operation := range []string{"status", "content", "cancel"} {
		for _, owner := range []struct{ user, key int64 }{{1, 2}, {1, 3}, {4, 2}} {
			writer := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(writer)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/public", nil)
			ctx.Params = gin.Params{{Key: "request_id", Value: "public"}}
			ctx.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{ID: owner.key, UserID: owner.user})
			switch operation {
			case "status":
				handler.QianwenVideoStatus(ctx)
			case "content":
				handler.QianwenVideoContent(ctx)
			case "cancel":
				handler.QianwenVideoCancel(ctx)
			}
			if owner.user != 1 || owner.key != 2 {
				require.Equal(t, 404, writer.Code)
			} else if operation == "cancel" {
				require.Equal(t, 409, writer.Code)
			} else {
				require.Equal(t, 200, writer.Code)
			}
			require.NotContains(t, writer.Body.String(), "private-task")
			require.NotContains(t, writer.Body.String(), "aliyuncs.com")
		}
	}
	require.Equal(t, 1, downloads)
	job.Family = "image"
	writer := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(writer)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/public", nil)
	ctx.Params = gin.Params{{Key: "request_id", Value: "public"}}
	ctx.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{ID: 2, UserID: 1})
	handler.QianwenVideoStatus(ctx)
	require.Equal(t, 404, writer.Code)
}

func TestQianwenStandardVideoStatesAndCreationResponse(t *testing.T) {
	for native, standard := range map[string]string{"submitted": "queued", "running": "in_progress", "succeeded": "completed", "failed": "failed"} {
		writer := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(writer)
		writeQianwenStandardVideo(ctx, &service.UpstreamJob{PublicRequestID: "public", UpstreamTaskID: "private", Status: native}, 202)
		require.Equal(t, 202, writer.Code)
		require.Contains(t, writer.Body.String(), `"id":"public"`)
		require.Contains(t, writer.Body.String(), `"status":"`+standard+`"`)
		require.NotContains(t, writer.Body.String(), "private")
	}
}

func TestQianwenStandardVideoJSONAndMultipartAdmission(t *testing.T) {
	var form bytes.Buffer
	writer := multipart.NewWriter(&form)
	require.NoError(t, writer.WriteField("model", "wan2.6-t2v"))
	require.NoError(t, writer.WriteField("prompt", "A cat"))
	require.NoError(t, writer.WriteField("seconds", "5"))
	require.NoError(t, writer.WriteField("size", "1280x720"))
	require.NoError(t, writer.Close())
	for _, test := range []struct{ body, contentType string }{
		{`{"model":"wan2.6-t2v","prompt":"A cat","seconds":"5","size":"1280x720"}`, "application/json"},
		{form.String(), writer.FormDataContentType()},
	} {
		handler := &OpenAIGatewayHandler{upstreamJobs: service.NewUpstreamJobService(nil, nil, nil)}
		response := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(response)
		ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(test.body))
		ctx.Request.Header.Set("Content-Type", test.contentType)
		ctx.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{ID: 2, UserID: 1, Group: &service.Group{Platform: service.PlatformQianwen, RateMultiplier: 1}})
		handler.QianwenVideoGeneration(ctx)
		require.Equal(t, 422, response.Code)
		require.Contains(t, response.Body.String(), "tariff")
	}
}

func TestQianwenStandardVideoMultipartRejectsReferencesAndDuplicates(t *testing.T) {
	for _, names := range [][]string{{"model", "model"}, {"input_reference"}} {
		var form bytes.Buffer
		writer := multipart.NewWriter(&form)
		for _, name := range names {
			require.NoError(t, writer.WriteField(name, "value"))
		}
		require.NoError(t, writer.Close())
		_, err := qianwenVideoFormBody(form.Bytes(), writer.Boundary())
		require.Error(t, err)
	}
}
