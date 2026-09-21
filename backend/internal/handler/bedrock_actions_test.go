//go:build unit

package handler

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// The Bedrock media/vector handlers pass their contract through the shared
// Bedrock adapter; these tests cover the handler glue (platform pin, body shape,
// admission order) and one full positive flow that reaches a stub upstream.

type bedrockHandlerUpstream struct {
	mu        sync.Mutex
	requests  []*http.Request
	bodies    [][]byte
	responder func(req *http.Request) *http.Response
}

func (s *bedrockHandlerUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		body, _ = io.ReadAll(req.Body)
	}
	s.mu.Lock()
	s.requests = append(s.requests, req)
	s.bodies = append(s.bodies, body)
	s.mu.Unlock()
	return s.responder(req), nil
}

func (s *bedrockHandlerUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return s.Do(req, proxyURL, accountID, accountConcurrency)
}

func (s *bedrockHandlerUpstream) snapshot() ([]*http.Request, [][]byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*http.Request(nil), s.requests...), append([][]byte(nil), s.bodies...)
}

func bedrockHandlerJSON(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": {"application/json"}, "X-Amzn-Requestid": {"bedrock-handler-1"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func bedrockActionTestContext() (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	ginContext, _ := gin.CreateTestContext(recorder)
	return ginContext, recorder
}

func bedrockHandlerContext(platform string) (*gin.Context, *httptest.ResponseRecorder, *service.APIKey) {
	groupID := int64(7301)
	ginContext, recorder := bedrockActionTestContext()
	group := &service.Group{ID: groupID, Platform: platform, Status: service.StatusActive, Hydrated: true, AllowImageGeneration: true}
	apiKey := &service.APIKey{ID: 7302, UserID: 7303, Status: service.StatusActive, Group: group, GroupID: &groupID, User: &service.User{ID: 7303, Status: service.StatusActive}}
	ginContext.Set(string(middleware.ContextKeyAPIKey), apiKey)
	ginContext.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7303, Concurrency: 1})
	return ginContext, recorder, apiKey
}

func TestBedrockActionPlatformGateRejectsNonBedrockGroups(test *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, platform := range []string{service.PlatformOpenAI, service.PlatformAnthropic} {
		ginContext, recorder, _ := bedrockHandlerContext(platform)
		ginContext.Request = httptest.NewRequest(http.MethodPost, "/v1/embeddings", strings.NewReader(`{"model":"amazon.titan-embed-text-v2:0","input":"alpha"}`))
		// A zero-value handler is enough: the rejection happens before any
		// dependency is touched, so a nil service would panic if it did not.
		(&GatewayHandler{}).BedrockEmbeddings(ginContext)
		require.Equal(test, http.StatusBadRequest, recorder.Code, platform)
		require.Contains(test, recorder.Body.String(), "requires a Bedrock group", platform)
	}
}

func TestBedrockActionAdmissionRejectsMissingBodyAndModel(test *testing.T) {
	gin.SetMode(gin.TestMode)
	emptyContext, emptyRecorder, _ := bedrockHandlerContext(service.PlatformBedrock)
	emptyContext.Request = httptest.NewRequest(http.MethodPost, "/v1/embeddings", nil)
	(&GatewayHandler{}).BedrockEmbeddings(emptyContext)
	require.Equal(test, http.StatusBadRequest, emptyRecorder.Code)
	require.Contains(test, emptyRecorder.Body.String(), "Request body is empty")

	modelContext, modelRecorder, _ := bedrockHandlerContext(service.PlatformBedrock)
	modelContext.Request = httptest.NewRequest(http.MethodPost, "/v1/embeddings", strings.NewReader(`{"input":"alpha"}`))
	(&GatewayHandler{}).BedrockEmbeddings(modelContext)
	require.Equal(test, http.StatusBadRequest, modelRecorder.Code)
	require.Contains(test, modelRecorder.Body.String(), "model is required")
}

func TestBedrockImagesAdmissionRejectsEditWithoutImageBytes(test *testing.T) {
	gin.SetMode(gin.TestMode)
	ginContext, recorder, _ := bedrockHandlerContext(service.PlatformBedrock)
	ginContext.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", strings.NewReader(`{"model":"amazon.nova-canvas-v1:0","prompt":"dusk"}`))
	ginContext.Request.Header.Set("Content-Type", "application/json")
	(&GatewayHandler{}).BedrockImages(ginContext)
	require.Equal(test, http.StatusBadRequest, recorder.Code)
	require.Contains(test, recorder.Body.String(), "input image bytes")
}

func TestBedrockImageActionForPath(test *testing.T) {
	for _, scenario := range []struct{ path, action string }{
		{"/v1/images/generations", "image.generate"},
		{"/v1/images/generations/", "image.generate"},
		{"/v1/images/edits", "image.edit"},
		{"/v1/images/variations", "image.variation"},
		{"/images/variations", "image.variation"},
	} {
		require.Equal(test, scenario.action, BedrockImageActionForPath(scenario.path), scenario.path)
	}
}

// bedrockHandlerFixture builds one Bedrock group with one Bedrock credential
// account behind the trunk scheduler plus a stub upstream on the OpenAI service.
func bedrockHandlerFixture(test *testing.T, groupID, accountID int64, credentials map[string]any, allowImageGeneration bool, responder func(*http.Request) *http.Response) (*GatewayHandler, *gin.Engine, *bedrockHandlerUpstream) {
	test.Helper()
	group := &service.Group{ID: groupID, Hydrated: true, Platform: service.PlatformBedrock, Status: service.StatusActive, AllowImageGeneration: allowImageGeneration}
	return bedrockHandlerFixtureForGroup(test, group, accountID, credentials, allowImageGeneration, responder, nil)
}

// bedrockHandlerFixtureForGroup wires the same fixture for an arbitrary group
// platform and an optional resolved Composite route decision. The decision is
// attached the way the Composite middleware attaches it - before the handler
// runs - so the target platform is already pinned when admission reads it.
func bedrockHandlerFixtureForGroup(test *testing.T, group *service.Group, accountID int64, credentials map[string]any, allowImageGeneration bool, responder func(*http.Request) *http.Response, decision *service.CompositeRouteDecision) (*GatewayHandler, *gin.Engine, *bedrockHandlerUpstream) {
	test.Helper()
	if group != nil {
		group.AllowImageGeneration = allowImageGeneration
	}
	account := &service.Account{
		ID: accountID, Name: "bedrock-fixture", Platform: service.PlatformBedrock, Type: service.AccountTypeBedrock,
		Status: service.StatusActive, Schedulable: true, Concurrency: 1, Credentials: credentials,
	}
	gatewayHandler, cleanup := newTestGatewayHandler(test, group, []*service.Account{account})
	test.Cleanup(cleanup)

	upstream := &bedrockHandlerUpstream{responder: responder}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Gateway.MaxAccountSwitches = 1
	billingCache := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	test.Cleanup(billingCache.Stop)
	gatewayHandler.openAIGatewayService = service.NewOpenAIGatewayService(
		nil, nil, nil, nil, nil, nil, newZhipuHandlerCacheStub(), cfg, nil, nil,
		service.NewBillingService(cfg, nil), nil, billingCache, upstream,
		&service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil,
	)
	gatewayHandler.apiKeyService = &service.APIKeyService{}
	gatewayHandler.billingCacheService = billingCache

	apiKey := &service.APIKey{
		ID: accountID + 1, UserID: accountID + 2, Status: service.StatusActive,
		Group: group, GroupID: &group.ID,
		User: &service.User{ID: accountID + 2, Status: service.StatusActive, Concurrency: 1, Balance: 100},
	}
	router := gin.New()
	router.Use(func(requestContext *gin.Context) {
		requestContext.Set(string(middleware.ContextKeyAPIKey), apiKey)
		requestContext.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: apiKey.UserID, Concurrency: 1})
		if decision != nil {
			requestContext.Request = requestContext.Request.WithContext(service.WithCompositeRouteDecision(requestContext.Request.Context(), *decision))
		}
		requestContext.Next()
	})
	return gatewayHandler, router, upstream
}

func bedrockHandlerServe(test *testing.T, router *gin.Engine, method, target, body, contentType string) *httptest.ResponseRecorder {
	test.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	request := httptest.NewRequest(method, target, reader)
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

// TestBedrockEmbeddingsHandlerFlow walks the whole handler path: admission,
// trunk account selection, the Bedrock adapter's InvokeModel call on the Titan
// text embedding family, the OpenAI client envelope and usage recording.
func TestBedrockEmbeddingsHandlerFlow(test *testing.T) {
	gin.SetMode(gin.TestMode)
	gatewayHandler, router, upstream := bedrockHandlerFixture(test, 7401, 7402,
		map[string]any{
			"aws_access_key_id": "AKIAFIXTUREEXAMPLE", "aws_secret_access_key": "fixture-secret",
			"aws_region": "us-east-1", "model_mapping": map[string]any{"titan-embed": "amazon.titan-embed-text-v2:0"},
		}, false, func(*http.Request) *http.Response {
			return bedrockHandlerJSON(http.StatusOK, `{"embedding":[0.5,0.25],"inputTextTokenCount":3}`)
		})
	router.POST("/v1/embeddings", gatewayHandler.BedrockEmbeddings)

	recorder := bedrockHandlerServe(test, router, http.MethodPost, "/v1/embeddings", `{"model":"titan-embed","input":["alpha","beta"],"dimensions":512}`, "application/json")
	require.Equal(test, http.StatusOK, recorder.Code, recorder.Body.String())
	requests, bodies := upstream.snapshot()
	require.Len(test, requests, 2, "Titan takes one inputText per InvokeModel call")
	for _, upstreamRequest := range requests {
		require.Equal(test, "https://bedrock-runtime.us-east-1.amazonaws.com/model/amazon.titan-embed-text-v2%3A0/invoke", upstreamRequest.URL.String())
		require.True(test, strings.HasPrefix(upstreamRequest.Header.Get("Authorization"), "AWS4-HMAC-SHA256"), "bedrock accounts sign with SigV4")
	}
	require.Contains(test, string(bodies[0]), `"inputText":"alpha"`)
	require.Contains(test, string(bodies[1]), `"inputText":"beta"`)
	require.Contains(test, recorder.Body.String(), `"object":"list"`)
	require.Contains(test, recorder.Body.String(), `"total_tokens":6`)
}

// TestBedrockImagesHandlerFlow walks the image path: the group image gate, the
// serving-family task body, the OpenAI b64_json envelope and the billable image
// count reported to the usage path.
func TestBedrockImagesHandlerFlow(test *testing.T) {
	gin.SetMode(gin.TestMode)
	pngBase64 := bedrockHandlerPNG(test, 1, 1)
	gatewayHandler, router, upstream := bedrockHandlerFixture(test, 7501, 7502,
		map[string]any{"aws_access_key_id": "AKIAFIXTUREEXAMPLE", "aws_secret_access_key": "fixture-secret", "aws_region": "us-east-1"},
		true, func(*http.Request) *http.Response {
			return bedrockHandlerJSON(http.StatusOK, `{"images":["`+pngBase64+`"]}`)
		})
	router.POST("/v1/images/generations", gatewayHandler.BedrockImages)

	recorder := bedrockHandlerServe(test, router, http.MethodPost, "/v1/images/generations",
		`{"model":"amazon.nova-canvas-v1:0","prompt":"a lighthouse","n":1,"size":"1024x1024"}`, "application/json")
	require.Equal(test, http.StatusOK, recorder.Code, recorder.Body.String())
	requests, bodies := upstream.snapshot()
	require.Len(test, requests, 1)
	require.Equal(test, "https://bedrock-runtime.us-east-1.amazonaws.com/model/amazon.nova-canvas-v1%3A0/invoke", requests[0].URL.String())
	require.Contains(test, string(bodies[0]), `"taskType":"TEXT_IMAGE"`)
	require.Contains(test, string(bodies[0]), `"numberOfImages":1`)
	require.Contains(test, recorder.Body.String(), `"b64_json":"`+pngBase64+`"`)
}

// TestBedrockImagesHandlerFlowRequiresTheGroupGate proves the image permission
// gate still applies on the Bedrock entry.
func TestBedrockImagesHandlerFlowRequiresTheGroupGate(test *testing.T) {
	gin.SetMode(gin.TestMode)
	gatewayHandler, router, upstream := bedrockHandlerFixture(test, 7601, 7602,
		map[string]any{"aws_access_key_id": "AKIAFIXTUREEXAMPLE", "aws_secret_access_key": "fixture-secret", "aws_region": "us-east-1"},
		false, func(*http.Request) *http.Response {
			return bedrockHandlerJSON(http.StatusOK, `{"images":[]}`)
		})
	router.POST("/v1/images/generations", gatewayHandler.BedrockImages)

	recorder := bedrockHandlerServe(test, router, http.MethodPost, "/v1/images/generations",
		`{"model":"amazon.nova-canvas-v1:0","prompt":"a lighthouse"}`, "application/json")
	require.Equal(test, http.StatusForbidden, recorder.Code, recorder.Body.String())
	requests, _ := upstream.snapshot()
	require.Empty(test, requests, "a denied group must never reach upstream")
}

// TestBedrockRerankHandlerFlow walks the rerank entry: admission, trunk account
// selection, the Agents for Amazon Bedrock Runtime Rerank call and the flat
// client envelope. The upstream response carries no token counters, so no usage
// is fabricated on the way out.
func TestBedrockRerankHandlerFlow(test *testing.T) {
	gin.SetMode(gin.TestMode)
	gatewayHandler, router, upstream := bedrockHandlerFixture(test, 7801, 7802,
		map[string]any{
			"aws_access_key_id": "AKIAFIXTUREEXAMPLE", "aws_secret_access_key": "fixture-secret",
			"aws_region": "us-east-1", "model_mapping": map[string]any{"rerank-alias": "cohere.rerank-v3-5:0"},
		}, false, func(*http.Request) *http.Response {
			return bedrockHandlerJSON(http.StatusOK, `{"results":[{"index":1,"relevanceScore":0.9,"document":{"textDocument":{"text":"beta"}}}]}`)
		})
	router.POST("/v1/rerank", gatewayHandler.BedrockRerank)

	recorder := bedrockHandlerServe(test, router, http.MethodPost, "/v1/rerank",
		`{"model":"cohere.rerank-v3-5:0","query":"alpha","documents":["beta","gamma"],"top_n":1}`, "application/json")
	require.Equal(test, http.StatusOK, recorder.Code, recorder.Body.String())
	requests, bodies := upstream.snapshot()
	require.Len(test, requests, 1)
	require.Equal(test, "https://bedrock-agent-runtime.us-east-1.amazonaws.com/rerank", requests[0].URL.String())
	require.True(test, strings.HasPrefix(requests[0].Header.Get("Authorization"), "AWS4-HMAC-SHA256"), "the agents runtime signs with the shared Bedrock SigV4 name")
	require.Contains(test, string(bodies[0]), `"type":"BEDROCK_RERANKING_MODEL"`)
	require.Contains(test, string(bodies[0]), `"modelArn":"cohere.rerank-v3-5:0"`)
	require.Contains(test, string(bodies[0]), `"text":"alpha"`)
	require.Contains(test, recorder.Body.String(), `"relevance_score":0.9`)
}

// TestBedrockActionsCompositeExplicitTargets proves that a Composite group
// whose explicit ordered target set pins Bedrock reaches the Bedrock media and
// vector adapters (embeddings and the image variation contract), and that a
// Composite route pinned to another platform never reaches them. The Composite
// decision is attached by the middleware before the handler runs, exactly as in
// production.
func TestBedrockActionsCompositeExplicitTargets(test *testing.T) {
	gin.SetMode(gin.TestMode)
	credentials := map[string]any{
		"aws_access_key_id": "AKIAFIXTUREEXAMPLE", "aws_secret_access_key": "fixture-secret", "aws_region": "us-east-1",
		"model_mapping": map[string]any{
			"bedrock-embed-alias": "amazon.titan-embed-text-v2:0",
			"bedrock-image-alias": "amazon.nova-canvas-v1:0",
		},
	}
	compositeGroup := func() *service.Group {
		return &service.Group{ID: 7701, Hydrated: true, Platform: service.PlatformComposite, Status: service.StatusActive}
	}

	test.Run("embeddings reach the Bedrock adapter through an explicit Bedrock target", func(test *testing.T) {
		decision := &service.CompositeRouteDecision{
			Matched: true, Source: service.CompositeRouteSourceExplicit,
			PublicModel: "bedrock-embed-alias", TargetPlatform: service.PlatformBedrock,
			UpstreamModel: "amazon.titan-embed-text-v2:0",
		}
		gatewayHandler, router, upstream := bedrockHandlerFixtureForGroup(test, compositeGroup(), 7702, credentials, true,
			func(*http.Request) *http.Response {
				return bedrockHandlerJSON(http.StatusOK, `{"embedding":[0.5,0.25],"inputTextTokenCount":3}`)
			}, decision)
		router.POST("/v1/embeddings", gatewayHandler.BedrockEmbeddings)

		recorder := bedrockHandlerServe(test, router, http.MethodPost, "/v1/embeddings",
			`{"model":"amazon.titan-embed-text-v2:0","input":"alpha"}`, "application/json")
		require.Equal(test, http.StatusOK, recorder.Code, recorder.Body.String())
		requests, bodies := upstream.snapshot()
		require.Len(test, requests, 1)
		require.Equal(test, "https://bedrock-runtime.us-east-1.amazonaws.com/model/amazon.titan-embed-text-v2%3A0/invoke", requests[0].URL.String())
		require.True(test, strings.HasPrefix(requests[0].Header.Get("Authorization"), "AWS4-HMAC-SHA256"), "a Composite Bedrock target keeps SigV4")
		require.Contains(test, string(bodies[0]), `"inputText":"alpha"`)
		require.Contains(test, recorder.Body.String(), `"total_tokens":3`)
	})

	test.Run("the image variation contract reaches the Bedrock adapter through an explicit Bedrock target", func(test *testing.T) {
		decision := &service.CompositeRouteDecision{
			Matched: true, Source: service.CompositeRouteSourceExplicit,
			PublicModel: "bedrock-image-alias", TargetPlatform: service.PlatformBedrock,
			UpstreamModel: "amazon.nova-canvas-v1:0",
		}
		pngBase64 := bedrockHandlerPNG(test, 1, 1)
		gatewayHandler, router, upstream := bedrockHandlerFixtureForGroup(test, compositeGroup(), 7712, credentials, true,
			func(*http.Request) *http.Response {
				return bedrockHandlerJSON(http.StatusOK, `{"images":["`+pngBase64+`"]}`)
			}, decision)
		router.POST("/v1/images/variations", gatewayHandler.BedrockImages)

		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		require.NoError(test, writer.WriteField("model", "amazon.nova-canvas-v1:0"))
		require.NoError(test, writer.WriteField("prompt", "dusk"))
		imagePart, err := writer.CreateFormFile("image", "input.png")
		require.NoError(test, err)
		_, err = imagePart.Write([]byte("fixture-image-bytes"))
		require.NoError(test, err)
		require.NoError(test, writer.Close())

		recorder := bedrockHandlerServe(test, router, http.MethodPost, "/v1/images/variations", body.String(), writer.FormDataContentType())
		require.Equal(test, http.StatusOK, recorder.Code, recorder.Body.String())
		requests, upstreamBodies := upstream.snapshot()
		require.Len(test, requests, 1)
		require.Equal(test, "https://bedrock-runtime.us-east-1.amazonaws.com/model/amazon.nova-canvas-v1%3A0/invoke", requests[0].URL.String())
		require.Contains(test, string(upstreamBodies[0]), `"taskType":"IMAGE_VARIATION"`)
		require.Contains(test, string(upstreamBodies[0]), base64.StdEncoding.EncodeToString([]byte("fixture-image-bytes")))
		require.Contains(test, recorder.Body.String(), `"b64_json":"`+pngBase64+`"`)
	})

	test.Run("a Composite route pinned to another platform is refused", func(test *testing.T) {
		decision := &service.CompositeRouteDecision{
			Matched: true, Source: service.CompositeRouteSourceExplicit,
			PublicModel: "grok-image-alias", TargetPlatform: service.PlatformGrok,
			UpstreamModel: "grok-imagine-video",
		}
		gatewayHandler, router, upstream := bedrockHandlerFixtureForGroup(test, compositeGroup(), 7722, credentials, true,
			func(*http.Request) *http.Response {
				return bedrockHandlerJSON(http.StatusOK, `{"embedding":[0.5],"inputTextTokenCount":1}`)
			}, decision)
		router.POST("/v1/embeddings", gatewayHandler.BedrockEmbeddings)

		recorder := bedrockHandlerServe(test, router, http.MethodPost, "/v1/embeddings",
			`{"model":"amazon.titan-embed-text-v2:0","input":"alpha"}`, "application/json")
		require.Equal(test, http.StatusBadRequest, recorder.Code, recorder.Body.String())
		require.Contains(test, recorder.Body.String(), "requires a Bedrock group")
		requests, _ := upstream.snapshot()
		require.Empty(test, requests, "a Composite target of another platform must never reach the Bedrock adapter")
	})
}

func bedrockHandlerPNG(test *testing.T, width, height int) string {
	test.Helper()
	buffer := &bytes.Buffer{}
	source := image.NewRGBA(image.Rect(0, 0, width, height))
	for x := 0; x < width; x++ {
		for y := 0; y < height; y++ {
			source.Set(x, y, color.RGBA{R: uint8(x % 251), G: uint8(y % 251), B: 64, A: 255})
		}
	}
	require.NoError(test, png.Encode(buffer, source))
	return base64.StdEncoding.EncodeToString(buffer.Bytes())
}
