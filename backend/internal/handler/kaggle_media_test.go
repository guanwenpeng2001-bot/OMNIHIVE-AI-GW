//go:build unit

package handler

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type kaggleMediaUpstream struct {
	response     string
	responseType string
	status       int
	request      *http.Request
	body         []byte
	calls        int
}

func (upstream *kaggleMediaUpstream) Do(request *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	upstream.request = request
	upstream.body, _ = io.ReadAll(request.Body)
	upstream.calls++
	status := upstream.status
	if status == 0 {
		status = 200
	}
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {upstream.responseType}, "X-Request-Id": {"fixture-kaggle"}}, Body: io.NopCloser(strings.NewReader(upstream.response)), ContentLength: int64(len(upstream.response))}, nil
}
func (upstream *kaggleMediaUpstream) DoWithTLS(request *http.Request, proxy string, id int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return upstream.Do(request, proxy, id, concurrency)
}

func kaggleMediaHandler(t *testing.T, upstream *kaggleMediaUpstream, mapping map[string]any) (*OpenAIGatewayHandler, *service.Group) {
	t.Helper()
	cfg := &config.Config{RunMode: config.RunModeSimple}
	if mapping == nil {
		mapping = map[string]any{"qwen-image-2.1": "qwen-image-2.1", "wan2.2-ti2v-5b": "wan2.2-ti2v-5b"}
	}
	account := preparedGatewayFixtureAccount(service.Account{ID: 7, Platform: service.PlatformKaggle, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Credentials: map[string]any{"api_key": "fixture", "base_url": "https://kaggle.example.trycloudflare.com", "model_mapping": mapping}, AccountGroups: []service.AccountGroup{{AccountID: 7, GroupID: 3}}})
	repo := &geminiReviewAccountRepo{openAIWSUsageHandlerAccountRepoStub: openAIWSUsageHandlerAccountRepoStub{account: account}}
	usage := &cursorRouteUsageRepo{}
	receipts := newHunyuanSTTReceiptRepository()
	gateway := service.ProvideOpenAIGatewayService(repo, usage, nil, nil, nil, nil, nil, cfg, nil, nil, service.NewBillingService(cfg, nil), nil, nil, upstream, nil, nil, nil, nil, nil, nil, nil, nil, service.NewUsageSettlementService(receipts, nil, nil))
	billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billing.Stop)
	handler := NewOpenAIGatewayHandler(gateway, service.NewConcurrencyService(&fakeConcurrencyCache{}), billing, service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, cfg)
	group := &service.Group{ID: 3, Platform: service.PlatformKaggle, Status: service.StatusActive, Hydrated: true, AllowImageGeneration: true}
	return handler, group
}

func TestKaggleImageHandlerPassthroughAndKeepalivePadding(t *testing.T) {
	var picture bytes.Buffer
	require.NoError(t, png.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	encoded := base64.StdEncoding.EncodeToString(picture.Bytes())
	jsonBody := fmt.Sprintf(`{"created":1,"data":[{"b64_json":%q,"seed":7}],"usage":{"width":2,"height":2,"seconds":1.2}}`, encoded)
	upstream := &kaggleMediaUpstream{response: "    " + jsonBody, responseType: "application/json"}
	handler, group := kaggleMediaHandler(t, upstream, map[string]any{"qwen-image-2.1": "qwen-image-2.1"})
	request, recorder := requestPlanHTTPContext("/v1/images/generations", `{"model":"qwen-image-2.1","prompt":"a red circle","size":"1024x1024","n":1,"response_format":"b64_json"}`, group)
	handler.KaggleImages(request)
	require.Equal(t, 200, recorder.Code, recorder.Body.String())
	require.Equal(t, 1, upstream.calls)
	require.Equal(t, "/v1/images/generations", upstream.request.URL.Path)
	require.Equal(t, "Bearer fixture", upstream.request.Header.Get("Authorization"))
	require.Equal(t, "qwen-image-2.1", gjson.GetBytes(upstream.body, "model").String())
	require.Equal(t, "a red circle", gjson.GetBytes(upstream.body, "prompt").String())
	require.JSONEq(t, jsonBody, recorder.Body.String())
}

func TestKaggleImageHandlerRejectsUnsupportedFormat(t *testing.T) {
	upstream := &kaggleMediaUpstream{}
	handler, group := kaggleMediaHandler(t, upstream, map[string]any{"qwen-image-2.1": "qwen-image-2.1"})
	request, recorder := requestPlanHTTPContext("/v1/images/generations", `{"model":"qwen-image-2.1","prompt":"dot","response_format":"url"}`, group)
	handler.KaggleImages(request)
	require.Equal(t, 400, recorder.Code, recorder.Body.String())
	require.Zero(t, upstream.calls)
}

func TestKaggleVideoHandlerLifecycle(t *testing.T) {
	for _, scenario := range []string{"priced", "unpriced", "failed"} {
		t.Run(scenario, func(t *testing.T) {
			upstream := &kaggleMediaUpstream{response: `{"id":"vid_aaaaaaaaaaaaaaaaaaaa","object":"video","model":"wan2.2-ti2v-5b","status":"queued","progress":0}`, responseType: "application/json", status: 202}
			handler, group := kaggleMediaHandler(t, upstream, map[string]any{"wan2.2-ti2v-5b": "wan2.2-ti2v-5b"})
			group.RateMultiplier = 1
			if scenario == "priced" {
				price := 0.5
				group.ModelPricing = []service.ChannelModelPricing{{Models: []string{"wan2.2-ti2v-5b"}, BillingMode: service.BillingModePerRequest, PerRequestPrice: &price}}
			}
			repository := newGatewayResourceWiringRepo()
			jobs := service.NewUpstreamJobService(repository, service.GatewayResourcePollJob(handler.gatewayService), service.GatewayResourceJobCharge)
			handler.upstreamJobs = jobs
			payload := `{"model":"wan2.2-ti2v-5b","prompt":"a cat walking on a beach","width":832,"height":480,"num_frames":49}`
			request, recorder := requestPlanHTTPContext("/v1/videos", payload, group)
			handler.KaggleVideoGeneration(request)
			require.Equal(t, 200, recorder.Code, recorder.Body.String())
			identifier := gjson.Get(recorder.Body.String(), "id").String()
			require.NotEmpty(t, identifier)
			require.Equal(t, "queued", gjson.Get(recorder.Body.String(), "status").String())
			require.Equal(t, 1, upstream.calls)
			require.Equal(t, "/v1/videos", upstream.request.URL.Path)
			require.Equal(t, "wan2.2-ti2v-5b", gjson.GetBytes(upstream.body, "model").String())
			require.Equal(t, "Bearer fixture", upstream.request.Header.Get("Authorization"))
			require.Zero(t, repository.charges)
			duplicate, duplicateResult := requestPlanHTTPContext("/v1/videos", payload, group)
			handler.KaggleVideoGeneration(duplicate)
			require.Equal(t, identifier, gjson.Get(duplicateResult.Body.String(), "id").String())
			require.Equal(t, 1, upstream.calls)
			repository.dueNow(identifier)
			upstream.status = 200
			upstream.response = `{"id":"vid_aaaaaaaaaaaaaaaaaaaa","status":"in_progress","progress":40}`
			job, err := jobs.Poll(context.Background(), identifier)
			require.NoError(t, err)
			require.Equal(t, "running", job.Status)
			repository.dueNow(identifier)
			upstream.response = `{"id":"vid_aaaaaaaaaaaaaaaaaaaa","status":"completed","progress":100,"seconds":3.5}`
			if scenario == "failed" {
				upstream.response = `{"id":"vid_aaaaaaaaaaaaaaaaaaaa","status":"failed","error":"stub failure"}`
			}
			job, err = jobs.Poll(context.Background(), identifier)
			require.NoError(t, err)
			if scenario == "failed" {
				require.Equal(t, "failed", job.Status)
				require.Zero(t, repository.charges)
				return
			}
			require.Equal(t, "succeeded", job.Status)
			require.Equal(t, "vid_aaaaaaaaaaaaaaaaaaaa", job.ResultRef)
			if scenario == "priced" {
				require.Equal(t, 1, repository.charges)
				require.NotNil(t, job.ChargedUsageLogID)
			} else {
				require.Zero(t, repository.charges)
				require.Nil(t, job.ChargedUsageLogID)
				require.Equal(t, "settlement_needs_review", job.ErrorCode)
				require.Equal(t, service.PricingStateUnpriced, job.Billing.PricingState)
			}
			status, statusResult := requestPlanHTTPContext("/v1/videos/"+identifier, `{}`, group)
			status.Request.Method = "GET"
			status.Params = gin.Params{{Key: "request_id", Value: identifier}}
			handler.KaggleVideoStatus(status)
			require.Equal(t, 200, statusResult.Code)
			require.Equal(t, "completed", gjson.Get(statusResult.Body.String(), "status").String())
			upstream.response = "mp4-fixture-bytes"
			upstream.responseType = "video/mp4"
			content, contentResult := requestPlanHTTPContext("/v1/videos/"+identifier+"/content", `{}`, group)
			content.Request.Method = "GET"
			content.Params = gin.Params{{Key: "request_id", Value: identifier}}
			handler.KaggleVideoContent(content)
			require.Equal(t, 200, contentResult.Code, contentResult.Body.String())
			require.Equal(t, "mp4-fixture-bytes", contentResult.Body.String())
			require.Equal(t, "/v1/videos/vid_aaaaaaaaaaaaaaaaaaaa/content", upstream.request.URL.Path)
		})
	}
}

func TestKaggleVideoRequiresModelAndPrompt(t *testing.T) {
	upstream := &kaggleMediaUpstream{}
	handler, group := kaggleMediaHandler(t, upstream, map[string]any{"wan2.2-ti2v-5b": "wan2.2-ti2v-5b"})
	handler.upstreamJobs = service.NewUpstreamJobService(newGatewayResourceWiringRepo(), service.GatewayResourcePollJob(handler.gatewayService), service.GatewayResourceJobCharge)
	request, response := requestPlanHTTPContext("/v1/videos", `{"prompt":"waves"}`, group)
	handler.KaggleVideoGeneration(request)
	require.Equal(t, 400, response.Code)
	require.Zero(t, upstream.calls)
}
