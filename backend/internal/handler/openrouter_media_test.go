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
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/tlsfingerprint"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

type openRouterMediaUpstream struct {
	response     string
	responseType string
	status       int
	request      *http.Request
	body         []byte
	calls        int
}

func (upstream *openRouterMediaUpstream) Do(request *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	upstream.request = request
	upstream.body, _ = io.ReadAll(request.Body)
	upstream.calls++
	status := upstream.status
	if status == 0 {
		status = 200
	}
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {upstream.responseType}, "X-Request-Id": {"fixture-media"}}, Body: io.NopCloser(strings.NewReader(upstream.response))}, nil
}
func (upstream *openRouterMediaUpstream) DoWithTLS(request *http.Request, proxy string, id int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return upstream.Do(request, proxy, id, concurrency)
}

func openRouterMediaHandler(t *testing.T, upstream *openRouterMediaUpstream) (*OpenAIGatewayHandler, *service.Group, *cursorRouteUsageRepo, *hunyuanSTTReceiptRepository) {
	t.Helper()
	cfg := &config.Config{RunMode: config.RunModeSimple}
	account := preparedGatewayFixtureAccount(service.Account{ID: 7, Platform: service.PlatformOpenRouter, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Credentials: map[string]any{"api_key": "fixture", "model_mapping": map[string]any{"vendor/future:variant": "vendor/future:variant"}}, AccountGroups: []service.AccountGroup{{AccountID: 7, GroupID: 3}}})
	account.SetUpstreamModelMetadataSnapshot(service.UpstreamModelMetadataSnapshot{Models: map[string]service.UpstreamModelMetadata{
		"vendor/future:variant": {OutputModalities: []string{"embedding"}},
	}})
	repo := &geminiReviewAccountRepo{openAIWSUsageHandlerAccountRepoStub: openAIWSUsageHandlerAccountRepoStub{account: account}}
	usage := &cursorRouteUsageRepo{}
	receipts := newHunyuanSTTReceiptRepository()
	gateway := service.ProvideOpenAIGatewayService(repo, usage, nil, nil, nil, nil, nil, cfg, nil, nil, service.NewBillingService(cfg, nil), nil, nil, upstream, nil, nil, nil, nil, nil, nil, nil, nil, service.NewUsageSettlementService(receipts, nil, nil))
	billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billing.Stop)
	handler := NewOpenAIGatewayHandler(gateway, service.NewConcurrencyService(&fakeConcurrencyCache{}), billing, service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, cfg)
	group := &service.Group{ID: 3, Platform: service.PlatformOpenRouter, Status: service.StatusActive, Hydrated: true}
	return handler, group, usage, receipts
}

func TestOpenRouterSynchronousMediaActualHandlers(t *testing.T) {
	var picture bytes.Buffer
	require.NoError(t, png.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 1, 1))))
	for _, fixture := range []struct{ action, path, body, response, responseType string }{
		{"images", "/v1/images/generations", `{"model":"vendor/future:variant","prompt":"a dot","size":"1024x1024","response_format":"b64_json"}`, fmt.Sprintf(`{"created":1,"data":[{"b64_json":%q}],"usage":{"prompt_tokens":0,"completion_tokens":1,"cost":99}}`, base64.StdEncoding.EncodeToString(picture.Bytes())), "application/json"},
		{"embeddings", "/v1/embeddings", `{"model":"vendor/future:variant","input":["hello"],"dimensions":2}`, `{"object":"list","model":"vendor/future:variant","data":[{"index":0,"object":"embedding","embedding":[0.1,0.2]}],"usage":{"prompt_tokens":2,"total_tokens":2}}`, "application/json"},
		{"rerank", "/v1/rerank", `{"model":"vendor/future:variant","query":"hello","documents":["hello","world"],"top_n":1}`, `{"model":"vendor/future:variant","results":[{"index":0,"relevance_score":0.99}],"usage":{"total_tokens":3,"search_units":1}}`, "application/json"},
		{"speech", "/v1/audio/speech", `{"model":"vendor/future:variant","input":"hello","voice":"sample","response_format":"pcm"}`, "\x01\x00\x02\x00", "audio/pcm"},
		{"transcriptions", "/v1/audio/transcriptions", "", `{"text":"hello world","usage":{"seconds":1.5,"input_tokens":2,"output_tokens":1,"cost":99}}`, "application/json"},
	} {
		t.Run(fixture.action, func(t *testing.T) {
			upstream := &openRouterMediaUpstream{response: fixture.response, responseType: fixture.responseType}
			handler, group, _, _ := openRouterMediaHandler(t, upstream)
			body := fixture.body
			contentType := "application/json"
			if fixture.action == "transcriptions" {
				var payload bytes.Buffer
				writer := multipart.NewWriter(&payload)
				require.NoError(t, writer.WriteField("model", "vendor/future:variant"))
				file, err := writer.CreateFormFile("file", "sample.wav")
				require.NoError(t, err)
				_, err = file.Write([]byte("RIFFfixtureWAVE"))
				require.NoError(t, err)
				require.NoError(t, writer.Close())
				body = payload.String()
				contentType = writer.FormDataContentType()
			}
			request, recorder := requestPlanHTTPContext(fixture.path, body, group)
			request.Request.Header.Set("Content-Type", contentType)
			methods := map[string]gin.HandlerFunc{"images": handler.OpenRouterImages, "embeddings": handler.OpenRouterEmbeddings, "rerank": handler.OpenRouterRerank, "speech": handler.OpenRouterSpeech, "transcriptions": handler.OpenRouterTranscriptions}
			methods[fixture.action](request)
			require.Equal(t, 200, recorder.Code, recorder.Body.String())
			require.Equal(t, 1, upstream.calls)
			path := strings.Replace(fixture.path, "/images/generations", "/images", 1)
			require.Equal(t, "/api"+path, upstream.request.URL.Path)
			require.Equal(t, "Bearer fixture", upstream.request.Header.Get("Authorization"))
			if fixture.action == "transcriptions" {
				require.Contains(t, string(upstream.body), "vendor/future:variant")
				require.Contains(t, string(upstream.body), "RIFFfixtureWAVE")
			} else {
				require.Equal(t, "vendor/future:variant", gjson.GetBytes(upstream.body, "model").String())
				expected := body
				if fixture.action == "images" {
					expected, _ = sjson.Delete(expected, "response_format")
				}
				require.JSONEq(t, expected, string(upstream.body))
			}
			if fixture.action == "images" {
				require.False(t, gjson.GetBytes(upstream.body, "response_format").Exists())
			}
			require.Equal(t, fixture.response, recorder.Body.String())
			require.Equal(t, fixture.responseType, strings.Split(recorder.Header().Get("Content-Type"), ";")[0])
		})
	}
}

func TestOpenRouterMediaRejectsInvalidResultsAndUnsupportedInputs(t *testing.T) {
	for _, fixture := range []struct {
		body, result string
		status       int
	}{
		{`{"model":"vendor/future:variant","input":"hello","dimensions":2}`, `{"data":[{"index":0,"embedding":[0.1]}],"usage":{"prompt_tokens":1}}`, 502},
		{`{"model":"vendor/future:variant","input":"hello"}`, `{"data":[{"index":0,"embedding":[0.1]}]}`, 502},
		{`{"model":"vendor/future:variant","input":"hello","encoding_format":"other"}`, `{}`, 400},
	} {
		upstream := &openRouterMediaUpstream{response: fixture.result, responseType: "application/json"}
		handler, group, usage, receipts := openRouterMediaHandler(t, upstream)
		request, recorder := requestPlanHTTPContext("/v1/embeddings", fixture.body, group)
		handler.OpenRouterEmbeddings(request)
		require.Equal(t, fixture.status, recorder.Code, recorder.Body.String())
		require.Empty(t, usage.logs)
		require.Empty(t, receipts.receipts)
		if fixture.status == 400 {
			require.Zero(t, upstream.calls)
		}
	}
}

func TestOpenRouterVideoActualHandlerLifecycle(t *testing.T) {
	for _, scenario := range []string{"priced", "free", "duration_tariff", "unpriced", "failed", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			upstream := &openRouterMediaUpstream{response: `{"id":"gen-vid-fixture","status":"pending","polling_url":"/api/v1/videos/gen-vid-fixture"}`, responseType: "application/json", status: 202}
			handler, group, _, _ := openRouterMediaHandler(t, upstream)
			group.RateMultiplier = 1
			if scenario == "priced" || scenario == "free" {
				price := 0.25
				if scenario == "free" {
					price = 0
				}
				group.ModelPricing = []service.ChannelModelPricing{{Models: []string{"vendor/future:variant"}, BillingMode: service.BillingModePerRequest, PerRequestPrice: &price}}
			}
			if scenario == "duration_tariff" {
				group.VideoModelPrices = map[string]map[string]float64{"vendor/future:variant": {"720p": 0.10}}
			}
			repository := newGatewayResourceWiringRepo()
			jobs := service.NewUpstreamJobService(repository, service.GatewayResourcePollJob(handler.gatewayService), service.GatewayResourceJobCharge)
			handler.upstreamJobs = jobs
			payload := `{"model":"vendor/future:variant","prompt":"waves","seconds":"8","resolution":"720p"}`
			request, recorder := requestPlanHTTPContext("/v1/videos", payload, group)
			handler.OpenRouterVideoGeneration(request)
			require.Equal(t, 200, recorder.Code, recorder.Body.String())
			identifier := gjson.Get(recorder.Body.String(), "id").String()
			require.NotEmpty(t, identifier)
			require.Equal(t, "queued", gjson.Get(recorder.Body.String(), "status").String())
			require.Equal(t, 1, upstream.calls)
			require.Equal(t, "/api/v1/videos", upstream.request.URL.Path)
			require.EqualValues(t, 8, gjson.GetBytes(upstream.body, "duration").Int())
			require.False(t, gjson.GetBytes(upstream.body, "seconds").Exists())
			require.Equal(t, "vendor/future:variant", gjson.GetBytes(upstream.body, "model").String())
			require.Equal(t, "Bearer fixture", upstream.request.Header.Get("Authorization"))
			require.Zero(t, repository.charges)
			duplicate, duplicateResult := requestPlanHTTPContext("/v1/videos", payload, group)
			handler.OpenRouterVideoGeneration(duplicate)
			require.Equal(t, identifier, gjson.Get(duplicateResult.Body.String(), "id").String())
			require.Equal(t, 1, upstream.calls)
			repository.dueNow(identifier)
			upstream.status = 200
			upstream.response = `{"id":"gen-vid-fixture","status":"in_progress"}`
			job, err := jobs.Poll(context.Background(), identifier)
			require.NoError(t, err)
			require.Equal(t, "running", job.Status)
			require.Zero(t, repository.charges)
			repository.dueNow(identifier)
			upstream.response = `{"id":"gen-vid-fixture","status":"completed","unsigned_urls":["https://foreign.example/result.mp4"],"usage":{"cost":99,"is_byok":false}}`
			if scenario == "failed" || scenario == "cancelled" {
				upstream.response = fmt.Sprintf(`{"id":"gen-vid-fixture","status":%q,"error":"fixture"}`, scenario)
			}
			job, err = jobs.Poll(context.Background(), identifier)
			require.NoError(t, err)
			if scenario == "failed" || scenario == "cancelled" {
				require.Equal(t, "failed", job.Status)
				require.Zero(t, repository.charges)
				return
			}
			require.Equal(t, "succeeded", job.Status)
			require.Equal(t, "gen-vid-fixture", job.ResultRef)
			require.NotContains(t, string(job.UsageJSON), "foreign.example")
			if scenario == "priced" || scenario == "free" {
				require.Equal(t, 1, repository.charges)
				require.NotNil(t, job.ChargedUsageLogID)
				if scenario == "free" {
					require.Equal(t, service.PricingStateFree, job.Billing.PricingState)
				} else {
					require.Equal(t, service.PricingStatePriced, job.Billing.PricingState)
				}
				log, command, chargeErr := service.OpenRouterVideoJobCharge(job)
				require.NoError(t, chargeErr)
				amount := 0.25
				if scenario == "free" {
					amount = 0
				}
				require.InDelta(t, amount, log.ActualCost, 0.0000001)
				require.InDelta(t, amount, command.BalanceCost, 0.0000001)
				require.NotEqual(t, float64(99), command.BalanceCost)
			} else {
				require.Zero(t, repository.charges)
				require.Nil(t, job.ChargedUsageLogID)
				require.Equal(t, "settlement_needs_review", job.ErrorCode)
				require.Equal(t, service.PricingStateUnpriced, job.Billing.PricingState)
			}
			calls := upstream.calls
			_, err = jobs.Poll(context.Background(), identifier)
			require.NoError(t, err)
			require.Equal(t, calls, upstream.calls, "terminal polling is idempotent")
			status, statusResult := requestPlanHTTPContext("/v1/videos/"+identifier, `{}`, group)
			status.Request.Method = "GET"
			status.Params = gin.Params{{Key: "request_id", Value: identifier}}
			handler.OpenRouterVideoStatus(status)
			require.Equal(t, 200, statusResult.Code)
			require.Equal(t, "completed", gjson.Get(statusResult.Body.String(), "status").String())
			upstream.response = "mp4-fixture-bytes"
			upstream.responseType = "video/mp4"
			content, contentResult := requestPlanHTTPContext("/v1/videos/"+identifier+"/content", `{}`, group)
			content.Request.Method = "GET"
			content.Params = gin.Params{{Key: "request_id", Value: identifier}}
			handler.OpenRouterVideoContent(content)
			require.Equal(t, 200, contentResult.Code, contentResult.Body.String())
			require.Equal(t, "mp4-fixture-bytes", contentResult.Body.String())
			require.Equal(t, "/api/v1/videos/gen-vid-fixture/content", upstream.request.URL.Path)
			require.Equal(t, "Bearer fixture", upstream.request.Header.Get("Authorization"))
			foreign, foreignResult := requestPlanHTTPContext("/v1/videos/"+identifier+"/content", `{}`, group)
			foreign.Params = gin.Params{{Key: "request_id", Value: identifier}}
			foreign.Set("api_key", &service.APIKey{ID: 999, UserID: 1, Group: group, GroupID: &group.ID})
			handler.OpenRouterVideoContent(foreign)
			require.Equal(t, 404, foreignResult.Code)
		})
	}
}

func TestOpenRouterVideoContinuationRejectedBeforeSubmit(t *testing.T) {
	upstream := &openRouterMediaUpstream{}
	handler, group, _, _ := openRouterMediaHandler(t, upstream)
	handler.upstreamJobs = service.NewUpstreamJobService(newGatewayResourceWiringRepo(), service.GatewayResourcePollJob(handler.gatewayService), service.GatewayResourceJobCharge)
	request, response := requestPlanHTTPContext("/v1/videos", `{"model":"vendor/future:variant","previous_job_id":"foreign-job"}`, group)
	handler.OpenRouterVideoGeneration(request)
	require.Equal(t, 400, response.Code)
	require.Zero(t, upstream.calls)
}

func TestOpenRouterMediaOperationFailureBoundaries(t *testing.T) {
	for _, fixture := range []struct {
		action, path, body, result, contentType string
		status                                  int
	}{
		{"images", "/v1/images/generations", `{"model":"vendor/future:variant","prompt":"dot"}`, `{"data":[{"b64_json":"not-base64"}],"usage":{"prompt_tokens":0,"completion_tokens":1}}`, "application/json", 502},
		{"images", "/v1/images/generations", `{"model":"vendor/future:variant","prompt":"dot","response_format":"url"}`, `{}`, "application/json", 400},
		{"speech", "/v1/audio/speech", `{"model":"vendor/future:variant","input":"hello","response_format":"wav"}`, `{}`, "application/json", 400},
		{"speech", "/v1/audio/speech", `{"model":"vendor/future:variant","input":"hello"}`, `{"message":"not audio"}`, "application/json", 502},
		{"rerank", "/v1/rerank", `{"model":"vendor/future:variant","query":"hello","documents":["one"]}`, `{"results":[{"index":4,"relevance_score":0.4}],"usage":{"total_tokens":1}}`, "application/json", 502},
		{"rerank", "/v1/rerank", `{"model":"vendor/future:variant","query":"hello","documents":["one"]}`, `{"results":[{"index":0,"relevance_score":1e999}],"usage":{"total_tokens":1}}`, "application/json", 502},
		{"rerank", "/v1/rerank", `{"model":"vendor/future:variant","query":"hello","documents":["one"]}`, `{"results":[{"index":0,"relevance_score":0.4}],"usage":{"search_units":1}}`, "application/json", 502},
	} {
		t.Run(fixture.action+fixture.result, func(t *testing.T) {
			upstream := &openRouterMediaUpstream{response: fixture.result, responseType: fixture.contentType}
			handler, group, usage, receipts := openRouterMediaHandler(t, upstream)
			request, response := requestPlanHTTPContext(fixture.path, fixture.body, group)
			methods := map[string]gin.HandlerFunc{"images": handler.OpenRouterImages, "speech": handler.OpenRouterSpeech, "rerank": handler.OpenRouterRerank}
			methods[fixture.action](request)
			require.Equal(t, fixture.status, response.Code, response.Body.String())
			require.Empty(t, usage.logs)
			require.Empty(t, receipts.receipts)
			if fixture.status == 400 {
				require.Zero(t, upstream.calls)
			}
		})
	}
	for _, format := range []string{"json", "text"} {
		upstream := &openRouterMediaUpstream{response: `{"text":"hello"}`, responseType: "application/json"}
		handler, group, usage, receipts := openRouterMediaHandler(t, upstream)
		var payload bytes.Buffer
		writer := multipart.NewWriter(&payload)
		require.NoError(t, writer.WriteField("model", "vendor/future:variant"))
		require.NoError(t, writer.WriteField("response_format", format))
		part, err := writer.CreateFormFile("file", "voice.wav")
		require.NoError(t, err)
		_, err = part.Write([]byte("audio"))
		require.NoError(t, err)
		require.NoError(t, writer.Close())
		request, response := requestPlanHTTPContext("/v1/audio/transcriptions", payload.String(), group)
		request.Request.Header.Set("Content-Type", writer.FormDataContentType())
		handler.OpenRouterTranscriptions(request)
		expected := 502
		if format == "text" {
			expected = 400
			require.Zero(t, upstream.calls)
		}
		require.Equal(t, expected, response.Code, response.Body.String())
		require.Empty(t, usage.logs)
		require.Empty(t, receipts.receipts)
	}
}
