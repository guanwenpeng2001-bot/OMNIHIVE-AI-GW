package routes

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type productionVideoJobs struct {
	service.UpstreamJobRepository
	job *service.UpstreamJob
}

func (repository *productionVideoJobs) Create(_ context.Context, job *service.UpstreamJob) (*service.UpstreamJob, bool, error) {
	repository.job = job
	return job, true, nil
}
func (repository *productionVideoJobs) Get(_ context.Context, id string, user, key int64) (*service.UpstreamJob, error) {
	if repository.job == nil || repository.job.PublicRequestID != id || repository.job.UserID != user || repository.job.APIKeyID != key {
		return nil, service.ErrUpstreamJobNotFound
	}
	return repository.job, nil
}
func (repository *productionVideoJobs) WithLocked(_ context.Context, id string, update func(*service.UpstreamJob, service.UpstreamJobTx) error) (*service.UpstreamJob, error) {
	if repository.job == nil || repository.job.PublicRequestID != id {
		return nil, service.ErrUpstreamJobNotFound
	}
	return repository.job, update(repository.job, nil)
}

type productionVideoTransport struct {
	testing    *testing.T
	paths      []string
	accountIDs []int64
	model      string
}

func (transport *productionVideoTransport) Do(request *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	transport.paths = append(transport.paths, request.URL.Path)
	transport.accountIDs = append(transport.accountIDs, accountID)
	require.Equal(transport.testing, "dashscope.aliyuncs.com", request.URL.Host)
	require.Equal(transport.testing, http.MethodPost, request.Method)
	payload := `{"output":{"task_id":"provider-private-id","task_status":"PENDING"}}`
	switch request.URL.Path {
	case service.QianwenNativeVideoPath:
		body, err := io.ReadAll(request.Body)
		require.NoError(transport.testing, err)
		expectedModel := transport.model
		if expectedModel == "" {
			expectedModel = "wan2.6-t2v"
		}
		require.Equal(transport.testing, expectedModel, gjson.GetBytes(body, "model").String())
	case service.QianwenNativeVoiceEnrollmentPath:
		payload = `{"output":{"voice_list":[]},"request_id":"fixture-voice-list"}`
	case "/api/v1/tasks/provider-private-id/cancel":
		payload = `"provider-cancellation-ack"`
	default:
		transport.testing.Fatalf("unexpected upstream path: %s", request.URL.Path)
	}
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(payload))}, nil
}
func (transport *productionVideoTransport) DoWithTLS(request *http.Request, proxy string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return transport.Do(request, proxy, accountID, concurrency)
}

func TestProductionCompositeVideoLifecycleUsesOwnedJob(test *testing.T) {
	for _, platform := range []string{service.PlatformQianwen, service.PlatformComposite} {
		test.Run(platform, func(test *testing.T) {
			account := service.Account{ID: 7, Platform: service.PlatformQianwen, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Concurrency: 2, Credentials: map[string]any{"api_key": "fixture", "model_mapping": map[string]any{"wan2.6-t2v": "wan2.6-t2v"}}, AccountGroups: []service.AccountGroup{{AccountID: 7, GroupID: 3}}}
			jobs := &productionVideoJobs{}
			transport := &productionVideoTransport{testing: test}
			router := realtimeProductionRouter(test, account, platform, "wan2.6-t2v", "wan2.6-t2v", "", jobs, transport)
			send := func(method, path, body string) *httptest.ResponseRecorder {
				request := httptest.NewRequest(method, path, strings.NewReader(body))
				request.Header.Set("Content-Type", "application/json")
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, request)
				return recorder
			}
			created := send("POST", "/v1/videos", `{"model":"wan2.6-t2v","prompt":"fixture","size":"1280x720","seconds":"5"}`)
			require.Equal(test, 202, created.Code, created.Body.String())
			require.NotNil(test, jobs.job)
			require.NotEmpty(test, jobs.job.UpstreamTaskID, created.Body.String())
			publicID := gjson.Get(created.Body.String(), "id").String()
			require.Equal(test, jobs.job.PublicRequestID, publicID)
			require.NotContains(test, created.Body.String(), "provider-private-id")
			status := send("GET", "/v1/videos/"+publicID, "")
			require.Equal(test, 200, status.Code, status.Body.String())
			require.Equal(test, "queued", gjson.Get(status.Body.String(), "status").String())
			content := send("GET", "/v1/videos/"+publicID+"/content", "")
			require.Equal(test, 409, content.Code, content.Body.String())
			require.Contains(test, content.Body.String(), "resource_not_ready")
			canceled := send("POST", "/v1/videos/"+publicID+"/cancel", `{"model":"gpt-realtime"}`)
			require.Equal(test, 200, canceled.Code, canceled.Body.String())
			require.True(test, gjson.Get(canceled.Body.String(), "cancellation_acknowledged").Bool())
			require.Equal(test, "upstream", gjson.Get(canceled.Body.String(), "cancel_scope").String())
			require.Equal(test, "submitted", jobs.job.Status)
			require.Equal(test, []int64{7, 7}, transport.accountIDs)
			for _, mismatch := range []string{"user", "key"} {
				if mismatch == "user" {
					jobs.job.UserID = 99
				} else {
					jobs.job.APIKeyID = 99
				}
				for _, route := range []string{"GET /v1/videos/" + publicID, "GET /v1/videos/" + publicID + "/content", "POST /v1/videos/" + publicID + "/cancel"} {
					method, path, _ := strings.Cut(route, " ")
					denied := send(method, path, "")
					require.Equal(test, 404, denied.Code, denied.Body.String())
					require.NotContains(test, denied.Body.String(), "qianwen")
					require.NotContains(test, denied.Body.String(), "provider-private-id")
				}
				jobs.job.UserID = 1
				jobs.job.APIKeyID = 2
			}
			require.Len(test, transport.paths, 2, "foreign reads/cancel must never contact upstream")
		})
	}
}

func TestProductionCompositeOpenAIVideoStatusUsesOwnedJob(test *testing.T) {
	account := service.Account{ID: 7, Platform: service.PlatformOpenAI}
	jobs := &productionVideoJobs{job: &service.UpstreamJob{PublicRequestID: "owned-openai-video", UserID: 1, APIKeyID: 2, AccountID: 7, Platform: service.PlatformOpenAI, Family: "video", Status: "submitted", UpstreamTaskID: "private-provider-id", Billing: service.UpstreamJobBilling{RequestedModel: "public-video", Model: "sora-fixture"}}}
	router := realtimeProductionRouter(test, account, service.PlatformComposite, "public-video", "sora-fixture", "", jobs)
	for _, prefix := range []string{"/v1", ""} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, prefix+"/videos/owned-openai-video", nil))
		require.Equal(test, 200, recorder.Code, recorder.Body.String())
		require.Contains(test, recorder.Body.String(), "owned-openai-video")
		require.NotContains(test, recorder.Body.String(), "private-provider-id")
	}
	jobs.job.APIKeyID = 99
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/videos/owned-openai-video", nil))
	require.Equal(test, 404, recorder.Code)
}

func TestProductionQianwenNativeOnlyOperationsRemainReachable(test *testing.T) {
	for _, scenario := range []struct {
		name, model, path, body string
		status                  int
	}{
		{"VACE edit", "wanx2.1-vace-plus", service.QianwenNativeVideoPath, `{"model":"wanx2.1-vace-plus","input":{"function":"video_edit","mask_image_url":"https://example.com/mask.png"},"parameters":{"resolution":"720P"}}`, 202},
		{"VACE extension", "wanx2.1-vace-plus", service.QianwenNativeVideoPath, `{"model":"wanx2.1-vace-plus","input":{"function":"video_extension","first_clip_url":"https://example.com/clip.mp4"},"parameters":{"resolution":"720P"}}`, 202},
		{"voice enrollment list", "qwen-voice-enrollment", service.QianwenNativeVoiceEnrollmentPath, `{"model":"qwen-voice-enrollment","input":{"action":"list"}}`, 200},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			account := service.Account{ID: 7, Platform: service.PlatformQianwen, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Concurrency: 2, Credentials: map[string]any{"api_key": "fixture", "model_mapping": map[string]any{scenario.model: scenario.model}}, AccountGroups: []service.AccountGroup{{AccountID: 7, GroupID: 3}}}
			transport := &productionVideoTransport{testing: test, model: scenario.model}
			router := realtimeProductionRouter(test, account, service.PlatformComposite, scenario.model, scenario.model, "", &productionVideoJobs{}, transport)
			request := httptest.NewRequest(http.MethodPost, scenario.path, strings.NewReader(scenario.body))
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			require.Equal(test, scenario.status, recorder.Code, recorder.Body.String())
			require.Equal(test, []string{scenario.path}, transport.paths)
		})
	}
}
