//go:build unit

package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Volcengine Ark action adapter tests. Endpoint paths, request bodies and
// response shapes are constructed from the frozen Ark runtime contract plus the
// official API references recorded in the captured API-reference navigation and
// per-API pages (video generation docs/82379/1520757 and /1521309, image
// generation docs/82379/1541523, embeddings docs/82379/1521765 text and
// docs/82379/1409290 multimodal, all captured 2026-09-21). They are contract
// fixtures, not captured vendor responses.

type volcengineActionUpstreamStub struct {
	mu        sync.Mutex
	requests  []*http.Request
	bodies    [][]byte
	responses []*http.Response
	err       error
	onRequest func(*http.Request)
}

func (s *volcengineActionUpstreamStub) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	s.mu.Lock()
	s.requests = append(s.requests, req)
	if req.Body != nil {
		body, _ := io.ReadAll(req.Body)
		s.bodies = append(s.bodies, body)
	} else {
		s.bodies = append(s.bodies, nil)
	}
	onRequest := s.onRequest
	err := s.err
	var resp *http.Response
	if len(s.responses) > 0 {
		resp = s.responses[0]
		s.responses = s.responses[1:]
	}
	s.mu.Unlock()
	if onRequest != nil {
		onRequest(req)
	}
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (s *volcengineActionUpstreamStub) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return s.Do(req, proxyURL, accountID, accountConcurrency)
}

func (s *volcengineActionUpstreamStub) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.requests)
}

func (s *volcengineActionUpstreamStub) requestAt(index int) (*http.Request, []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests[index], s.bodies[index]
}

func volcengineActionTestAccount() *Account {
	return &Account{
		ID:       7,
		Platform: PlatformVolcengine,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "ark-test-key",
			"base_url": volcengineRuntimeBaseURL,
		},
	}
}

func volcengineActionTestContext(method, target string, headers map[string]string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(method, target, nil)
	for name, value := range headers {
		c.Request.Header.Set(name, value)
	}
	return c, recorder
}

func volcengineJSONResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

// volcengineActionFakeCache implements the cache surface the video ownership and
// deferred-billing paths use (session binding + pending billing + claim).
type volcengineActionFakeCache struct {
	GatewayCache
	mu       sync.Mutex
	sessions map[string]int64
	pending  map[string][]byte
	claims   map[string]bool
}

func volcengineSessionMapKey(groupID int64, key string) string {
	return fmt.Sprintf("%d|%s", groupID, key)
}

func newVolcengineActionFakeCache() *volcengineActionFakeCache {
	return &volcengineActionFakeCache{
		sessions: map[string]int64{},
		pending:  map[string][]byte{},
		claims:   map[string]bool{},
	}
}

func (c *volcengineActionFakeCache) SetSessionAccountID(_ context.Context, groupID int64, key string, accountID int64, _ time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sessions[volcengineSessionMapKey(groupID, key)] = accountID
	return nil
}

func (c *volcengineActionFakeCache) GetSessionAccountID(_ context.Context, groupID int64, key string) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	accountID, ok := c.sessions[volcengineSessionMapKey(groupID, key)]
	if !ok {
		return 0, ErrStickySessionNotFound
	}
	return accountID, nil
}

func (c *volcengineActionFakeCache) SetGrokVideoPendingBilling(_ context.Context, key string, payload []byte, _ time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pending[key] = append([]byte(nil), payload...)
	return nil
}

func (c *volcengineActionFakeCache) GetGrokVideoPendingBilling(_ context.Context, key string) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	payload, ok := c.pending[key]
	if !ok {
		return nil, nil
	}
	return payload, nil
}

func (c *volcengineActionFakeCache) ClaimGrokVideoBilled(_ context.Context, key string, _ time.Duration) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.claims[key] {
		return false, nil
	}
	c.claims[key] = true
	return true, nil
}

func (c *volcengineActionFakeCache) ReleaseGrokVideoBilled(_ context.Context, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.claims, key)
	return nil
}

// ---------------------------------------------------------------------------
// Ark origin derivation (region / coding plan / relay prefix)
// ---------------------------------------------------------------------------

func TestVolcengineAPIRootDerivesArkOrigin(t *testing.T) {
	t.Run("default base keeps the /api/v3 version segment", func(t *testing.T) {
		require.Equal(t, "https://ark.cn-beijing.volces.com/api/v3", volcengineAPIRoot(volcengineActionTestAccount()))
	})
	t.Run("a full chat endpoint is trimmed to the origin", func(t *testing.T) {
		account := volcengineActionTestAccount()
		account.Credentials["base_url"] = "https://ark.cn-beijing.volces.com/api/v3/chat/completions"
		require.Equal(t, "https://ark.cn-beijing.volces.com/api/v3", volcengineAPIRoot(account))
	})
	t.Run("region host flows through unchanged", func(t *testing.T) {
		account := volcengineActionTestAccount()
		account.Credentials["base_url"] = "https://ark.ap-southeast.bytepluses.com/api/v3"
		require.Equal(t, "https://ark.ap-southeast.bytepluses.com/api/v3", volcengineAPIRoot(account))
	})
	t.Run("coding-plan product base flows through unchanged", func(t *testing.T) {
		account := volcengineActionTestAccount()
		account.Credentials["base_url"] = "https://ark.cn-beijing.volces.com/api/coding/v3"
		require.Equal(t, "https://ark.cn-beijing.volces.com/api/coding/v3", volcengineAPIRoot(account))
	})
	t.Run("relay path prefix is preserved", func(t *testing.T) {
		account := volcengineActionTestAccount()
		account.Credentials["base_url"] = "https://relay.example/ark/api/v3"
		require.Equal(t, "https://relay.example/ark/api/v3", volcengineAPIRoot(account))
	})
	require.Equal(t, "https://ark.cn-beijing.volces.com/api/v3/images/generations",
		buildVolcengineActionURL(volcengineActionTestAccount(), "/images/generations"))
}

func TestVolcengineImageResponseFormat(t *testing.T) {
	require.Equal(t, "url", mustVolcengineImageFormat(t, ""))
	require.Equal(t, "url", mustVolcengineImageFormat(t, "URL"))
	require.Equal(t, "b64_json", mustVolcengineImageFormat(t, "b64_json"))
	require.Equal(t, "b64_json", mustVolcengineImageFormat(t, "base64"))
	_, err := volcengineImageResponseFormat("webp")
	require.Error(t, err)
}

func mustVolcengineImageFormat(t *testing.T, requested string) string {
	t.Helper()
	format, err := volcengineImageResponseFormat(requested)
	require.NoError(t, err)
	return format
}

func TestVolcengineImageSize(t *testing.T) {
	for input, expected := range map[string]string{"": "", "2k": "2K", "4K": "4K", "1024x1024": "1024x1024", "2048X1024": "2048x1024"} {
		size, err := volcengineImageSize(input)
		require.NoError(t, err, input)
		require.Equal(t, expected, size, input)
	}
	for _, invalid := range []string{"huge", "0x10", "10x", "axb"} {
		_, err := volcengineImageSize(invalid)
		require.Error(t, err, invalid)
	}
}

func TestBuildVolcengineImageBodyOpenAIShape(t *testing.T) {
	seed := int64(42)
	watermark := false
	info := VolcengineMediaRequestInfo{
		Model:          "doubao-seedream-4-0-250828",
		Prompt:         "a red panda on a skateboard",
		N:              2,
		Size:           "2K",
		ResponseFormat: "url",
		Seed:           &seed,
		Watermark:      &watermark,
	}
	body, err := buildVolcengineImageBody(VolcengineMediaEndpointImagesGenerations, info, "doubao-seedream-4-0-250828")
	require.NoError(t, err)
	require.Equal(t, "doubao-seedream-4-0-250828", gjson.GetBytes(body, "model").String())
	require.Equal(t, "a red panda on a skateboard", gjson.GetBytes(body, "prompt").String())
	require.Equal(t, "url", gjson.GetBytes(body, "response_format").String())
	require.Equal(t, "2K", gjson.GetBytes(body, "size").String())
	require.Equal(t, int64(42), gjson.GetBytes(body, "seed").Int())
	require.False(t, gjson.GetBytes(body, "watermark").Bool())
	require.Equal(t, "auto", gjson.GetBytes(body, "sequential_image_generation").String(), "n>1 maps to Seedream 组图生成")
	require.Equal(t, int64(2), gjson.GetBytes(body, "sequential_image_generation_options.max_images").Int())
}

func TestBuildVolcengineImageBodyRejectsBadInput(t *testing.T) {
	cases := []struct {
		name     string
		endpoint VolcengineMediaEndpoint
		info     VolcengineMediaRequestInfo
	}{
		{"missing prompt", VolcengineMediaEndpointImagesGenerations, VolcengineMediaRequestInfo{Model: "m"}},
		{"bad response format", VolcengineMediaEndpointImagesGenerations, VolcengineMediaRequestInfo{Prompt: "p", ResponseFormat: "webp"}},
		{"bad size", VolcengineMediaEndpointImagesGenerations, VolcengineMediaRequestInfo{Prompt: "p", Size: "huge"}},
		{"edit without image", VolcengineMediaEndpointImagesEdits, VolcengineMediaRequestInfo{Prompt: "p"}},
		{"edit with mask", VolcengineMediaEndpointImagesEdits, VolcengineMediaRequestInfo{Prompt: "p", InputImageURLs: []string{"https://x/y.png"}, MaskUpload: &OpenAIImagesUpload{Data: []byte("x"), ContentType: "image/png"}}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := buildVolcengineImageBody(testCase.endpoint, testCase.info, "m")
			require.Error(t, err)
		})
	}
}

func TestBuildVolcengineImageBodyEditsReferences(t *testing.T) {
	t.Run("single reference passes as a string", func(t *testing.T) {
		body, err := buildVolcengineImageBody(VolcengineMediaEndpointImagesEdits,
			VolcengineMediaRequestInfo{Model: "m", Prompt: "make it snow", InputImageURLs: []string{"https://cdn.example/in.png"}}, "m")
		require.NoError(t, err)
		require.Equal(t, "https://cdn.example/in.png", gjson.GetBytes(body, "image").String())
		require.False(t, gjson.GetBytes(body, "image").IsArray())
	})
	t.Run("multiple references pass as an array", func(t *testing.T) {
		body, err := buildVolcengineImageBody(VolcengineMediaEndpointImagesEdits,
			VolcengineMediaRequestInfo{Model: "m", Prompt: "merge", InputImageURLs: []string{"https://cdn.example/a.png", "https://cdn.example/b.png"}}, "m")
		require.NoError(t, err)
		require.True(t, gjson.GetBytes(body, "image").IsArray())
		require.Equal(t, 2, len(gjson.GetBytes(body, "image").Array()))
	})
	t.Run("an uploaded file becomes a data URL reference", func(t *testing.T) {
		upload := OpenAIImagesUpload{FieldName: "image", FileName: "in.png", ContentType: "image/png", Data: []byte("png-bytes")}
		body, err := buildVolcengineImageBody(VolcengineMediaEndpointImagesEdits,
			VolcengineMediaRequestInfo{Model: "m", Prompt: "edit", Uploads: []OpenAIImagesUpload{upload}}, "m")
		require.NoError(t, err)
		require.True(t, strings.HasPrefix(gjson.GetBytes(body, "image").String(), "data:image/png;base64,"))
	})
}

func TestBridgeVolcengineImageResponse(t *testing.T) {
	body, count, err := bridgeVolcengineImageResponse([]byte(`{"model":"doubao-seedream-4-0-250828","created":1785125529,"data":[{"url":"https://cdn.example/out.png"}],"usage":{"generated_images":1}}`))
	require.NoError(t, err)
	require.Equal(t, 1, count)
	require.Equal(t, "https://cdn.example/out.png", gjson.GetBytes(body, "data.0.url").String())

	b64Body, b64Count, b64Err := bridgeVolcengineImageResponse([]byte(`{"data":[{"b64_json":"AAA"}]}`))
	require.NoError(t, b64Err)
	require.Equal(t, 1, b64Count)
	require.Equal(t, "AAA", gjson.GetBytes(b64Body, "data.0.b64_json").String())

	_, _, missingErr := bridgeVolcengineImageResponse([]byte(`{"model":"m","usage":{"generated_images":0}}`))
	require.Error(t, missingErr)
	_, _, emptyErr := bridgeVolcengineImageResponse([]byte(`{"data":[]}`))
	require.Error(t, emptyErr)

	// HTTP 200 with an Ark error envelope must surface as the business error.
	_, _, businessErr := bridgeVolcengineImageResponse([]byte(`{"error":{"code":"InvalidParameter","message":"size is out of range","type":"invalid_request_error"}}`))
	var volErr *VolcengineBusinessError
	require.True(t, errors.As(businessErr, &volErr))
	require.Equal(t, "InvalidParameter", volErr.Code)
}

// ---------------------------------------------------------------------------
// Image forwarding
// ---------------------------------------------------------------------------

func TestForwardVolcengineMediaImageReachesUpstream(t *testing.T) {
	upstream := &volcengineActionUpstreamStub{
		responses: []*http.Response{volcengineJSONResponse(http.StatusOK,
			`{"model":"doubao-seedream-4-0-250828","created":1785125529,"data":[{"url":"https://cdn.example/out.png"}],"usage":{"generated_images":1,"total_tokens":4000}}`)},
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	c, recorder := volcengineActionTestContext(http.MethodPost, "/v1/images/generations", nil)
	body := []byte(`{"model":"doubao-seedream-4-0-250828","prompt":"a red panda","size":"2K"}`)
	result, err := svc.ForwardVolcengineMedia(context.Background(), c, volcengineActionTestAccount(), VolcengineMediaEndpointImagesGenerations, "", body, "application/json")
	require.NoError(t, err)
	require.NotNil(t, result)

	require.Equal(t, 1, upstream.callCount())
	req, reqBody := upstream.requestAt(0)
	require.Equal(t, "https://ark.cn-beijing.volces.com/api/v3/images/generations", req.URL.String())
	require.Equal(t, http.MethodPost, req.Method)
	require.Equal(t, "Bearer ark-test-key", req.Header.Get("Authorization"))
	require.Equal(t, "application/json", req.Header.Get("Content-Type"))
	require.Equal(t, "doubao-seedream-4-0-250828", gjson.GetBytes(reqBody, "model").String())
	require.Equal(t, "a red panda", gjson.GetBytes(reqBody, "prompt").String())
	require.Equal(t, "2K", gjson.GetBytes(reqBody, "size").String())

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "https://cdn.example/out.png", gjson.GetBytes(recorder.Body.Bytes(), "data.0.url").String())
	require.Equal(t, 1, result.ImageCount)
	require.Equal(t, "doubao-seedream-4-0-250828", result.UpstreamModel)
}

func TestForwardVolcengineMediaImageEditMultipartBridge(t *testing.T) {
	upstream := &volcengineActionUpstreamStub{
		responses: []*http.Response{volcengineJSONResponse(http.StatusOK,
			`{"data":[{"url":"https://cdn.example/edited.png"}],"usage":{"generated_images":1}}`)},
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	c, recorder := volcengineActionTestContext(http.MethodPost, "/v1/images/edits", nil)

	var multipartBody strings.Builder
	multipartBody.WriteString("--eboundary\r\nContent-Disposition: form-data; name=\"model\"\r\n\r\ndoubao-seedream-4-0-250828\r\n")
	multipartBody.WriteString("--eboundary\r\nContent-Disposition: form-data; name=\"prompt\"\r\n\r\nturn the sky green\r\n")
	multipartBody.WriteString("--eboundary\r\nContent-Disposition: form-data; name=\"image\"; filename=\"in.png\"\r\nContent-Type: image/png\r\n\r\npng-bytes\r\n")
	multipartBody.WriteString("--eboundary--\r\n")

	result, err := svc.ForwardVolcengineMedia(context.Background(), c, volcengineActionTestAccount(),
		VolcengineMediaEndpointImagesEdits, "", []byte(multipartBody.String()), "multipart/form-data; boundary=eboundary")
	require.NoError(t, err)
	require.NotNil(t, result)

	req, reqBody := upstream.requestAt(0)
	require.Equal(t, "https://ark.cn-beijing.volces.com/api/v3/images/generations", req.URL.String(),
		"Ark generation and editing share one endpoint")
	require.Equal(t, "application/json", req.Header.Get("Content-Type"), "the multipart edit bridges to the Ark JSON body")
	require.True(t, strings.HasPrefix(gjson.GetBytes(reqBody, "image").String(), "data:image/png;base64,"))
	require.Equal(t, "turn the sky green", gjson.GetBytes(reqBody, "prompt").String())
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, 1, result.ImageCount)
}

func TestForwardVolcengineMediaImageClientErrorNotFailover(t *testing.T) {
	upstream := &volcengineActionUpstreamStub{
		responses: []*http.Response{volcengineJSONResponse(http.StatusBadRequest,
			`{"error":{"code":"InvalidParameter","message":"prompt is too long","type":"invalid_request_error"}}`)},
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	c, recorder := volcengineActionTestContext(http.MethodPost, "/v1/images/generations", nil)
	_, err := svc.ForwardVolcengineMedia(context.Background(), c, volcengineActionTestAccount(), VolcengineMediaEndpointImagesGenerations, "", []byte(`{"model":"m","prompt":"p"}`), "application/json")
	require.Error(t, err)
	var failoverErr *UpstreamFailoverError
	require.False(t, errors.As(err, &failoverErr), "a request-scoped 400 must not move to another account")
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), "prompt is too long")
}

func TestForwardVolcengineMediaImageAccountErrorFailsOver(t *testing.T) {
	upstream := &volcengineActionUpstreamStub{
		responses: []*http.Response{volcengineJSONResponse(http.StatusTooManyRequests,
			`{"error":{"code":"RateLimitExceeded","message":"rate limit","type":"rate_limit_error"}}`)},
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	c, _ := volcengineActionTestContext(http.MethodPost, "/v1/images/generations", nil)
	_, err := svc.ForwardVolcengineMedia(context.Background(), c, volcengineActionTestAccount(), VolcengineMediaEndpointImagesGenerations, "", []byte(`{"model":"m","prompt":"p"}`), "application/json")
	require.Error(t, err)
	var failoverErr *UpstreamFailoverError
	require.True(t, errors.As(err, &failoverErr))
	require.Equal(t, http.StatusTooManyRequests, failoverErr.StatusCode)
}

func TestVolcengineHTTPStatusFailoverable(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusPaymentRequired, http.StatusTooManyRequests, http.StatusRequestTimeout, http.StatusInternalServerError, http.StatusBadGateway} {
		require.True(t, volcengineHTTPStatusFailoverable(status), status)
	}
	for _, status := range []int{http.StatusBadRequest, http.StatusForbidden, http.StatusNotFound, http.StatusUnprocessableEntity} {
		require.False(t, volcengineHTTPStatusFailoverable(status), status)
	}
}

// ---------------------------------------------------------------------------
// Video generation / edit / extend
// ---------------------------------------------------------------------------

func TestBuildVolcengineVideoCreateBody(t *testing.T) {
	watermark := false
	info := VolcengineMediaRequestInfo{
		Model:           "doubao-seedance-2-0-260128",
		Prompt:          "a wave crashing on rocks",
		Resolution:      "1080p",
		DurationSeconds: 5,
		AspectRatio:     "16:9",
		Watermark:       &watermark,
		InputImageURLs:  []string{"https://cdn.example/first.png", "https://cdn.example/last.png"},
	}
	body, err := buildVolcengineVideoCreateBody(VolcengineMediaEndpointVideosGenerations, info, "doubao-seedance-2-0-260128")
	require.NoError(t, err)
	require.Equal(t, "doubao-seedance-2-0-260128", gjson.GetBytes(body, "model").String())
	require.Equal(t, "text", gjson.GetBytes(body, "content.0.type").String())
	require.Equal(t, "a wave crashing on rocks", gjson.GetBytes(body, "content.0.text").String())
	require.Equal(t, "image_url", gjson.GetBytes(body, "content.1.type").String())
	require.Equal(t, "first_frame", gjson.GetBytes(body, "content.1.role").String())
	require.Equal(t, "https://cdn.example/first.png", gjson.GetBytes(body, "content.1.image_url.url").String())
	require.Equal(t, "last_frame", gjson.GetBytes(body, "content.2.role").String())
	require.Equal(t, "1080p", gjson.GetBytes(body, "resolution").String())
	require.Equal(t, int64(5), gjson.GetBytes(body, "duration").Int())
	require.Equal(t, "16:9", gjson.GetBytes(body, "ratio").String())
	require.False(t, gjson.GetBytes(body, "watermark").Bool())
}

func TestBuildVolcengineVideoCreateBodyRejectsBadInput(t *testing.T) {
	_, err := buildVolcengineVideoCreateBody(VolcengineMediaEndpointVideosGenerations, VolcengineMediaRequestInfo{Model: "m"}, "m")
	require.Error(t, err, "prompt is required")

	_, err = buildVolcengineVideoCreateBody(VolcengineMediaEndpointVideosGenerations,
		VolcengineMediaRequestInfo{Model: "m", Prompt: "p", Resolution: "8k"}, "m")
	require.Error(t, err, "unknown resolution rejected")

	_, err = buildVolcengineVideoCreateBody(VolcengineMediaEndpointVideosGenerations,
		VolcengineMediaRequestInfo{Model: "m", Prompt: "p", InputImageURLs: []string{"a", "b", "c"}}, "m")
	require.Error(t, err, "at most a first and a last frame")
}

func TestBuildVolcengineVideoEditAndExtendUseSourceVideo(t *testing.T) {
	for _, endpoint := range []VolcengineMediaEndpoint{VolcengineMediaEndpointVideoEdit, VolcengineMediaEndpointVideoExtend} {
		t.Run(string(endpoint), func(t *testing.T) {
			_, err := buildVolcengineVideoCreateBody(endpoint,
				VolcengineMediaRequestInfo{Model: "m", Prompt: "change the weather"}, "m")
			require.Error(t, err, "edit/extend require a source video")

			body, err := buildVolcengineVideoCreateBody(endpoint,
				VolcengineMediaRequestInfo{Model: "m", Prompt: "change the weather", InputVideoURLs: []string{"https://cdn.example/in.mp4"}}, "m")
			require.NoError(t, err)
			require.Equal(t, "video_url", gjson.GetBytes(body, "content.1.type").String())
			require.Equal(t, "https://cdn.example/in.mp4", gjson.GetBytes(body, "content.1.video_url.url").String())
			require.Equal(t, "change the weather", gjson.GetBytes(body, "content.0.text").String())
		})
	}
}

func TestForwardVolcengineMediaVideoCreateReturnsTaskID(t *testing.T) {
	upstream := &volcengineActionUpstreamStub{
		responses: []*http.Response{volcengineJSONResponse(http.StatusOK, `{"id":"cgt-2026-09-21-abc"}`)},
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	c, recorder := volcengineActionTestContext(http.MethodPost, "/v1/videos/generations", nil)
	body := []byte(`{"model":"doubao-seedance-2-0-260128","prompt":"a boy playing basketball by the sea","resolution":"1080p","duration":5,"aspect_ratio":"16:9"}`)
	result, err := svc.ForwardVolcengineMedia(context.Background(), c, volcengineActionTestAccount(), VolcengineMediaEndpointVideosGenerations, "", body, "application/json")
	require.NoError(t, err)
	require.NotNil(t, result)

	req, reqBody := upstream.requestAt(0)
	require.Equal(t, "https://ark.cn-beijing.volces.com/api/v3/contents/generations/tasks", req.URL.String())
	require.Equal(t, http.MethodPost, req.Method)
	require.Equal(t, "Bearer ark-test-key", req.Header.Get("Authorization"))
	require.Equal(t, "doubao-seedance-2-0-260128", gjson.GetBytes(reqBody, "model").String())
	require.Equal(t, "a boy playing basketball by the sea", gjson.GetBytes(reqBody, "content.0.text").String())
	require.Equal(t, "1080p", gjson.GetBytes(reqBody, "resolution").String())

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "cgt-2026-09-21-abc", gjson.GetBytes(recorder.Body.Bytes(), "id").String())
	require.Equal(t, "cgt-2026-09-21-abc", result.ResponseID)
	require.Equal(t, VideoBillingResolution1080P, result.VideoResolution)
	require.Equal(t, 5, result.VideoDurationSeconds)
}

func TestForwardVolcengineMediaVideoCreateWithoutTaskIDFailsOver(t *testing.T) {
	upstream := &volcengineActionUpstreamStub{
		responses: []*http.Response{volcengineJSONResponse(http.StatusOK, `{"usage":{"total_tokens":1}}`)},
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	c, _ := volcengineActionTestContext(http.MethodPost, "/v1/videos/generations", nil)
	_, err := svc.ForwardVolcengineMedia(context.Background(), c, volcengineActionTestAccount(), VolcengineMediaEndpointVideosGenerations, "", []byte(`{"model":"m","prompt":"p"}`), "application/json")
	require.Error(t, err)
	var failoverErr *UpstreamFailoverError
	require.True(t, errors.As(err, &failoverErr))
}

const volcengineVideoSucceededBody = `{"id":"cgt-2026-09-21-abc","model":"doubao-seedance-2-0-260128","status":"succeeded","content":{"video_url":"https://ark-content.volces.com/inference/output/2026-09-21/abc/output.mp4"},"usage":{"completion_tokens":123,"total_tokens":123},"created_at":1785125529,"updated_at":1785125946,"seed":-1,"resolution":"1080p","duration":5,"ratio":"16:9","framespersecond":24}`

func TestIsVolcengineVideoStatusBillable(t *testing.T) {
	require.True(t, IsVolcengineVideoStatusBillable([]byte(volcengineVideoSucceededBody)))
	for _, notBillable := range []string{
		`{"id":"t","status":"queued"}`,
		`{"id":"t","status":"running"}`,
		`{"id":"t","status":"failed","content":{"video_url":""}}`,
		`{"id":"t","status":"succeeded","content":{}}`,
		``,
	} {
		require.False(t, IsVolcengineVideoStatusBillable([]byte(notBillable)), notBillable)
	}
}

func TestExtractVolcengineVideoBillingFromStatusBody(t *testing.T) {
	result := ExtractVolcengineVideoBillingFromStatusBody([]byte(volcengineVideoSucceededBody), nil, "cgt-2026-09-21-abc")
	require.NotNil(t, result)
	require.Equal(t, "cgt-2026-09-21-abc", result.ResponseID)
	require.Equal(t, "doubao-seedance-2-0-260128", result.Model)
	require.Equal(t, 1, result.VideoCount)
	require.Equal(t, VideoBillingResolution1080P, result.VideoResolution)
	require.Equal(t, 5, result.VideoDurationSeconds)

	// 4k folds onto the highest existing price tier.
	fourK := strings.Replace(volcengineVideoSucceededBody, `"resolution":"1080p"`, `"resolution":"4k"`, 1)
	fourKResult := ExtractVolcengineVideoBillingFromStatusBody([]byte(fourK), nil, "cgt")
	require.Equal(t, VideoBillingResolution1080P, fourKResult.VideoResolution)

	// The create-time snapshot supplies model/duration/resolution when the task
	// body omits them.
	pending := &VolcengineVideoPendingBilling{Model: "doubao-seedance-2-0-260128", BillingModel: "doubao-seedance-2-0-260128", VideoResolution: VideoBillingResolution720P, VideoDurationSeconds: 5}
	sparse := `{"id":"task-1","status":"succeeded","content":{"video_url":"https://cdn.example/v.mp4"}}`
	sparseResult := ExtractVolcengineVideoBillingFromStatusBody([]byte(sparse), pending, "task-1")
	require.NotNil(t, sparseResult)
	require.Equal(t, "doubao-seedance-2-0-260128", sparseResult.Model)
	require.Equal(t, VideoBillingResolution720P, sparseResult.VideoResolution)
	require.Equal(t, 5, sparseResult.VideoDurationSeconds)

	require.Nil(t, ExtractVolcengineVideoBillingFromStatusBody([]byte(`{"id":"t","status":"running"}`), nil, "t"))
}

func TestForwardVolcengineMediaVideoStatusRewritesProxyURL(t *testing.T) {
	upstream := &volcengineActionUpstreamStub{
		responses: []*http.Response{volcengineJSONResponse(http.StatusOK, volcengineVideoSucceededBody)},
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	c, recorder := volcengineActionTestContext(http.MethodGet, "/v1/videos/cgt-2026-09-21-abc", nil)
	result, err := svc.ForwardVolcengineMedia(context.Background(), c, volcengineActionTestAccount(), VolcengineMediaEndpointVideoStatus, "cgt-2026-09-21-abc", nil, "")
	require.NoError(t, err)
	require.NotNil(t, result)

	req, _ := upstream.requestAt(0)
	require.Equal(t, "https://ark.cn-beijing.volces.com/api/v3/contents/generations/tasks/cgt-2026-09-21-abc", req.URL.String())
	require.Equal(t, http.MethodGet, req.Method)
	require.Equal(t, "Bearer ark-test-key", req.Header.Get("Authorization"))

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "/v1/videos/cgt-2026-09-21-abc/content", gjson.GetBytes(recorder.Body.Bytes(), "content.video_url").String(),
		"time-limited CDN URLs must be rewritten to the owner-checked gateway proxy")
	require.Equal(t, "succeeded", gjson.GetBytes(recorder.Body.Bytes(), "status").String())
	require.Equal(t, 1, result.VideoCount)
	require.Equal(t, 5, result.VideoDurationSeconds)
}

func TestForwardVolcengineMediaVideoContentStreamsRange(t *testing.T) {
	upstream := &volcengineActionUpstreamStub{
		responses: []*http.Response{
			volcengineJSONResponse(http.StatusOK, volcengineVideoSucceededBody),
			{
				StatusCode: http.StatusPartialContent,
				Header: http.Header{
					"Content-Type":   []string{"video/mp4"},
					"Content-Length": []string{"13"},
					"Content-Range":  []string{"bytes 0-12/100"},
					"Accept-Ranges":  []string{"bytes"},
				},
				Body: io.NopCloser(strings.NewReader("video-payload")),
			},
		},
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	c, recorder := volcengineActionTestContext(http.MethodGet, "/v1/videos/cgt-2026-09-21-abc/content", map[string]string{"Range": "bytes=0-12"})
	result, err := svc.ForwardVolcengineMedia(context.Background(), c, volcengineActionTestAccount(), VolcengineMediaEndpointVideoContent, "cgt-2026-09-21-abc", nil, "")
	require.NoError(t, err)
	require.NotNil(t, result)

	require.Equal(t, 2, upstream.callCount())
	statusReq, _ := upstream.requestAt(0)
	require.Equal(t, "https://ark.cn-beijing.volces.com/api/v3/contents/generations/tasks/cgt-2026-09-21-abc", statusReq.URL.String())
	contentReq, _ := upstream.requestAt(1)
	require.Equal(t, "https://ark-content.volces.com/inference/output/2026-09-21/abc/output.mp4", contentReq.URL.String())
	require.Empty(t, contentReq.Header.Get("Authorization"), "signed CDN downloads must not leak the account credential")
	require.Equal(t, "bytes=0-12", contentReq.Header.Get("Range"))

	require.Equal(t, http.StatusPartialContent, recorder.Code)
	require.Equal(t, "video-payload", recorder.Body.String())
	require.Equal(t, "bytes 0-12/100", recorder.Header().Get("Content-Range"))
	require.Equal(t, 1, result.VideoCount)
}

func TestForwardVolcengineMediaVideoContentRejectsNonHTTPSURL(t *testing.T) {
	insecureBody := strings.Replace(volcengineVideoSucceededBody, "https://ark-content.volces.com", "http://ark-content.volces.com", 1)
	upstream := &volcengineActionUpstreamStub{
		responses: []*http.Response{volcengineJSONResponse(http.StatusOK, insecureBody)},
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	c, _ := volcengineActionTestContext(http.MethodGet, "/v1/videos/cgt-2026-09-21-abc/content", nil)
	_, err := svc.ForwardVolcengineMedia(context.Background(), c, volcengineActionTestAccount(), VolcengineMediaEndpointVideoContent, "cgt-2026-09-21-abc", nil, "")
	require.Error(t, err)
	require.Equal(t, 1, upstream.callCount(), "an insecure content URL must be rejected before any download")
}

func TestForwardVolcengineMediaVideoContentNotReady(t *testing.T) {
	upstream := &volcengineActionUpstreamStub{
		responses: []*http.Response{volcengineJSONResponse(http.StatusOK, `{"id":"task-9","status":"running"}`)},
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	c, recorder := volcengineActionTestContext(http.MethodGet, "/v1/videos/task-9/content", nil)
	_, err := svc.ForwardVolcengineMedia(context.Background(), c, volcengineActionTestAccount(), VolcengineMediaEndpointVideoContent, "task-9", nil, "")
	require.Error(t, err)
	require.Equal(t, http.StatusConflict, recorder.Code)
	require.Equal(t, 1, upstream.callCount())
}

func TestForwardVolcengineMediaVideoCancel(t *testing.T) {
	upstream := &volcengineActionUpstreamStub{
		responses: []*http.Response{volcengineJSONResponse(http.StatusOK, `{"id":"task-3","status":"cancelled"}`)},
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	c, recorder := volcengineActionTestContext(http.MethodDelete, "/v1/videos/task-3", nil)
	result, err := svc.ForwardVolcengineMedia(context.Background(), c, volcengineActionTestAccount(), VolcengineMediaEndpointVideoCancel, "task-3", nil, "")
	require.NoError(t, err)
	require.NotNil(t, result)

	req, body := upstream.requestAt(0)
	require.Equal(t, http.MethodDelete, req.Method)
	require.Equal(t, "https://ark.cn-beijing.volces.com/api/v3/contents/generations/tasks/task-3", req.URL.String())
	require.Equal(t, "Bearer ark-test-key", req.Header.Get("Authorization"))
	require.Nil(t, body)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "task-3", result.ResponseID)
}

func TestForwardVolcengineMediaVideoCancelRunningTaskFails(t *testing.T) {
	upstream := &volcengineActionUpstreamStub{
		responses: []*http.Response{volcengineJSONResponse(http.StatusBadRequest,
			`{"error":{"code":"InvalidParameter","message":"cannot cancel a running task","type":"invalid_request_error"}}`)},
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	c, recorder := volcengineActionTestContext(http.MethodDelete, "/v1/videos/task-4", nil)
	_, err := svc.ForwardVolcengineMedia(context.Background(), c, volcengineActionTestAccount(), VolcengineMediaEndpointVideoCancel, "task-4", nil, "")
	require.Error(t, err)
	var failoverErr *UpstreamFailoverError
	require.False(t, errors.As(err, &failoverErr), "a running-task cancel rejection must not fail over")
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), "cannot cancel a running task")
}

// ---------------------------------------------------------------------------
// Ownership / idempotency / pricing
// ---------------------------------------------------------------------------

func TestVolcengineVideoOwnershipAndPendingBillingLifecycle(t *testing.T) {
	cache := newVolcengineActionFakeCache()
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: &volcengineActionUpstreamStub{}, cache: cache}
	ctx := context.Background()
	groupID := int64(24)

	require.NoError(t, svc.BindVolcengineVideoRequestAccount(ctx, &groupID, "task-1", 10, 20, 7))
	owner, err := svc.ResolveVolcengineVideoRequestAccount(ctx, &groupID, "task-1", 10, 20)
	require.NoError(t, err)
	require.Equal(t, int64(7), owner)

	// Cross-tenant probes: other user, other key, other task must not resolve.
	for _, probe := range []struct {
		userID, keyID int64
		taskID        string
	}{{11, 20, "task-1"}, {10, 21, "task-1"}, {10, 20, "task-2"}} {
		_, err := svc.ResolveVolcengineVideoRequestAccount(ctx, &groupID, probe.taskID, probe.userID, probe.keyID)
		require.Error(t, err, "task %s user %d key %d must not resolve another tenant's binding", probe.taskID, probe.userID, probe.keyID)
	}

	pending := VolcengineVideoPendingBilling{Model: "doubao-seedance-2-0-260128", BillingModel: "doubao-seedance-2-0-260128", VideoResolution: "4k", VideoDurationSeconds: 5}
	require.NoError(t, svc.StoreVolcengineVideoPendingBilling(ctx, "task-1", 10, 20, pending))
	loaded, err := svc.LoadVolcengineVideoPendingBilling(ctx, "task-1", 10, 20)
	require.NoError(t, err)
	require.NotNil(t, loaded)
	require.Equal(t, "doubao-seedance-2-0-260128", loaded.Model)
	require.Equal(t, VideoBillingResolution1080P, loaded.VideoResolution, "4k folds onto the highest price tier")
	require.NotEmpty(t, loaded.CreatedAt, "create-accept time is always stamped for E2E duration")

	claimed, err := svc.ClaimVolcengineVideoBilling(ctx, "task-1", 10, 20)
	require.NoError(t, err)
	require.True(t, claimed, "first completion observation claims billing")
	for range 3 {
		again, err := svc.ClaimVolcengineVideoBilling(ctx, "task-1", 10, 20)
		require.NoError(t, err)
		require.False(t, again, "repeated status/content polls must never double-bill")
	}
	require.NoError(t, svc.ReleaseVolcengineVideoBilling(ctx, "task-1", 10, 20))
	reclaimed, err := svc.ClaimVolcengineVideoBilling(ctx, "task-1", 10, 20)
	require.NoError(t, err)
	require.True(t, reclaimed, "a released claim (failed RecordUsage) is billable again")
}

func TestStableVolcengineVideoBillingRequestID(t *testing.T) {
	require.Equal(t, "volcengine_video:task-1", StableVolcengineVideoBillingRequestID("task-1"))
	require.Equal(t, "volcengine_video:task-1", StableVolcengineVideoBillingRequestID("volcengine_video:task-1"))
	require.Empty(t, StableVolcengineVideoBillingRequestID(""))
	require.True(t, isForcedUsageBillingRequestID("volcengine_video:task-1"), "forced IDs survive client-supplied request IDs across polls")
}

// ---------------------------------------------------------------------------
// Embeddings
// ---------------------------------------------------------------------------

func TestForwardVolcengineEmbeddingsReachesUpstream(t *testing.T) {
	upstream := &volcengineActionUpstreamStub{
		responses: []*http.Response{volcengineJSONResponse(http.StatusOK,
			`{"object":"list","data":[{"object":"embedding","embedding":[0.1,0.2],"index":0}],"model":"doubao-embedding-vision-250615","usage":{"prompt_tokens":12,"total_tokens":12}}`)},
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	c, recorder := volcengineActionTestContext(http.MethodPost, "/v1/embeddings", nil)
	body := []byte(`{"model":"public-embedding-alias","input":["hello world"],"encoding_format":"float"}`)
	result, err := svc.ForwardVolcengineEmbeddings(context.Background(), c, volcengineActionTestAccount(), body, "application/json")
	require.NoError(t, err)
	require.NotNil(t, result)

	require.Equal(t, 1, upstream.callCount())
	req, reqBody := upstream.requestAt(0)
	require.Equal(t, "https://ark.cn-beijing.volces.com/api/v3/embeddings", req.URL.String())
	require.Equal(t, http.MethodPost, req.Method)
	require.Equal(t, "Bearer ark-test-key", req.Header.Get("Authorization"))
	require.Equal(t, "public-embedding-alias", gjson.GetBytes(reqBody, "model").String())
	require.Equal(t, "hello world", gjson.GetBytes(reqBody, "input.0").String())

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "list", gjson.GetBytes(recorder.Body.Bytes(), "object").String())
	require.Equal(t, 12, result.Usage.InputTokens)
	require.Equal(t, 12, result.Usage.OutputTokens)
}

func TestForwardVolcengineEmbeddingsRewritesMappedModel(t *testing.T) {
	upstream := &volcengineActionUpstreamStub{
		responses: []*http.Response{volcengineJSONResponse(http.StatusOK,
			`{"object":"list","data":[{"embedding":[0.1],"index":0}],"usage":{"prompt_tokens":1,"total_tokens":1}}`)},
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	account := volcengineActionTestAccount()
	account.Credentials["model_mapping"] = map[string]any{"embed-alias": "ep-embedding-endpoint"}
	c, _ := volcengineActionTestContext(http.MethodPost, "/v1/embeddings", nil)
	body := []byte(`{"model":"embed-alias","input":["x"]}`)
	_, err := svc.ForwardVolcengineEmbeddings(context.Background(), c, account, body, "application/json")
	require.NoError(t, err)
	_, reqBody := upstream.requestAt(0)
	require.Equal(t, "ep-embedding-endpoint", gjson.GetBytes(reqBody, "model").String(), "an Endpoint ID target must be sent like the chat path")
}

func TestForwardVolcengineEmbeddingsRejectsEmptyVectors(t *testing.T) {
	upstream := &volcengineActionUpstreamStub{
		responses: []*http.Response{volcengineJSONResponse(http.StatusOK, `{"object":"list","data":[],"usage":{"total_tokens":0}}`)},
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	c, _ := volcengineActionTestContext(http.MethodPost, "/v1/embeddings", nil)
	_, err := svc.ForwardVolcengineEmbeddings(context.Background(), c, volcengineActionTestAccount(), []byte(`{"model":"m","input":["x"]}`), "application/json")
	require.Error(t, err)
	var failoverErr *UpstreamFailoverError
	require.True(t, errors.As(err, &failoverErr))
}

func TestForwardVolcengineEmbeddingsClientErrorNotFailover(t *testing.T) {
	upstream := &volcengineActionUpstreamStub{
		responses: []*http.Response{volcengineJSONResponse(http.StatusBadRequest,
			`{"error":{"code":"InvalidParameter","message":"input is required","type":"invalid_request_error"}}`)},
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	c, recorder := volcengineActionTestContext(http.MethodPost, "/v1/embeddings", nil)
	_, err := svc.ForwardVolcengineEmbeddings(context.Background(), c, volcengineActionTestAccount(), []byte(`{"model":"m","input":["x"]}`), "application/json")
	require.Error(t, err)
	var failoverErr *UpstreamFailoverError
	require.False(t, errors.As(err, &failoverErr))
	require.Equal(t, http.StatusBadRequest, recorder.Code)
}

// ---------------------------------------------------------------------------
// Platform guard + cancellation
// ---------------------------------------------------------------------------

func TestForwardVolcengineMediaRejectsWrongPlatform(t *testing.T) {
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: &volcengineActionUpstreamStub{}}
	other := volcengineActionTestAccount()
	other.Platform = PlatformMiniMax
	c, _ := volcengineActionTestContext(http.MethodPost, "/v1/images/generations", nil)
	_, err := svc.ForwardVolcengineMedia(context.Background(), c, other, VolcengineMediaEndpointImagesGenerations, "", []byte(`{"model":"m","prompt":"p"}`), "application/json")
	require.Error(t, err)

	_, embErr := svc.ForwardVolcengineEmbeddings(context.Background(), c, other, []byte(`{"model":"m","input":["x"]}`), "application/json")
	require.Error(t, embErr)

	require.Equal(t, 0, svc.httpUpstream.(*volcengineActionUpstreamStub).callCount(), "a wrong-platform account must never reach upstream")
}

func TestForwardVolcengineMediaCancellationPropagates(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	upstream := &volcengineActionUpstreamStub{
		onRequest: func(req *http.Request) { cancel() },
		err:       context.Canceled,
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	c, recorder := volcengineActionTestContext(http.MethodPost, "/v1/images/generations", nil)
	_, err := svc.ForwardVolcengineMedia(ctx, c, volcengineActionTestAccount(), VolcengineMediaEndpointImagesGenerations, "", []byte(`{"model":"m","prompt":"p"}`), "application/json")
	require.Error(t, err)
	require.True(t, errors.Is(err, context.Canceled), "client cancellation must abort the upstream attempt without failover")
	var failoverErr *UpstreamFailoverError
	require.False(t, errors.As(err, &failoverErr))
	require.Equal(t, http.StatusOK, recorder.Code, "nothing is written to a disconnected client")
	require.Empty(t, recorder.Body.String())
}

// ---------------------------------------------------------------------------
// Embeddings endpoint selection (text vs multimodal vectorization)
// ---------------------------------------------------------------------------

func TestVolcengineEmbeddingsPathByInputShape(t *testing.T) {
	for body, expected := range map[string]string{
		`{"model":"doubao-embedding-text-240715","input":["天很蓝"]}`:                                                   "/embeddings",
		`{"model":"doubao-embedding-vision-250615","input":[]}`:                                                      "/embeddings",
		`{"model":"doubao-embedding-vision-250615","input":["a","b"],"encoding_format":"float"}`:                     "/embeddings",
		`{"model":"doubao-embedding-vision-250615","input":[{"type":"text","text":"天很蓝"}]}`:                          "/embeddings/multimodal",
		`{"model":"doubao-embedding-vision-250615","input":[{"type":"image_url","image_url":{"url":"u"}}]}`:          "/embeddings/multimodal",
		`{"model":"doubao-embedding-vision-250615","input":[{"type":"video_url","video_url":{"url":"u"}}]}`:          "/embeddings/multimodal",
		`{"model":"doubao-embedding-vision-250615","input":[{"type":"text","text":"x"},{"type":"text","text":"y"}]}`: "/embeddings/multimodal",
	} {
		require.Equal(t, expected, volcengineEmbeddingsPath([]byte(body)), body)
	}
}

func TestForwardVolcengineEmbeddingsMultimodalReachesMultimodalEndpoint(t *testing.T) {
	upstream := &volcengineActionUpstreamStub{
		responses: []*http.Response{volcengineJSONResponse(http.StatusOK,
			`{"id":"0217","model":"doubao-embedding-vision-250615","object":"list","data":[{"object":"embedding","embedding":[0.1,0.2],"index":0}],"usage":{"prompt_tokens":8,"total_tokens":8}}`)},
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	c, recorder := volcengineActionTestContext(http.MethodPost, "/v1/embeddings", nil)
	body := []byte(`{"model":"doubao-embedding-vision-250615","instructions":"Target_modality: text and image.\nQuery:","encoding_format":"float","input":[{"type":"image_url","image_url":{"url":"https://cdn.example/in.png"}},{"type":"text","text":"图片中是什么"}]}`)
	result, err := svc.ForwardVolcengineEmbeddings(context.Background(), c, volcengineActionTestAccount(), body, "application/json")
	require.NoError(t, err)
	require.NotNil(t, result)

	req, reqBody := upstream.requestAt(0)
	require.Equal(t, "https://ark.cn-beijing.volces.com/api/v3/embeddings/multimodal", req.URL.String(),
		"multimodal input objects use the documented multimodal vectorization path")
	require.Equal(t, http.MethodPost, req.Method)
	require.Equal(t, "Bearer ark-test-key", req.Header.Get("Authorization"))
	require.Equal(t, "image_url", gjson.GetBytes(reqBody, "input.0.type").String())
	require.Equal(t, "https://cdn.example/in.png", gjson.GetBytes(reqBody, "input.0.image_url.url").String())
	require.Equal(t, "图片中是什么", gjson.GetBytes(reqBody, "input.1.text").String())
	require.Contains(t, gjson.GetBytes(reqBody, "instructions").String(), "Target_modality")
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, 8, result.Usage.InputTokens)
}

func TestForwardVolcengineEmbeddingsRejectsNonObjectBody(t *testing.T) {
	upstream := &volcengineActionUpstreamStub{}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	c, recorder := volcengineActionTestContext(http.MethodPost, "/v1/embeddings", nil)
	_, err := svc.ForwardVolcengineEmbeddings(context.Background(), c, volcengineActionTestAccount(), []byte(`["not-an-object"]`), "application/json")
	require.Error(t, err)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Zero(t, upstream.callCount(), "an invalid body must never reach upstream")
}

// ---------------------------------------------------------------------------
// Price resolution: the Ark response must feed the configured group price tiers
// ---------------------------------------------------------------------------

func TestForwardVolcengineImageBillingTierMatchesGroupPrice(t *testing.T) {
	price1K, price2K, price4K := 0.01, 0.02, 0.04
	group := &Group{ID: 24, ImagePrice1K: &price1K, ImagePrice2K: &price2K, ImagePrice4K: &price4K}
	for _, testCase := range []struct {
		name         string
		size         string
		billingTier  string
		expectedCost float64
	}{
		{"explicit 1K", "1K", "1K", price1K},
		{"explicit 4K", "4K", "4K", price4K},
		{"pixel size 2048x2048", "2048x2048", "2K", price2K},
		{"pixel size 4096x4096", "4096x4096", "4K", price4K},
		{"omitted size uses the documented 2K default", "", "2K", price2K},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			upstream := &volcengineActionUpstreamStub{
				responses: []*http.Response{volcengineJSONResponse(http.StatusOK,
					`{"model":"doubao-seedream-4-0-250828","data":[{"url":"https://cdn.example/out.png"}],"usage":{"generated_images":1}}`)},
			}
			svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
			c, _ := volcengineActionTestContext(http.MethodPost, "/v1/images/generations", nil)
			body := []byte(`{"model":"doubao-seedream-4-0-250828","prompt":"p"}`)
			if testCase.size != "" {
				body = []byte(`{"model":"doubao-seedream-4-0-250828","prompt":"p","size":"` + testCase.size + `"}`)
			}
			result, err := svc.ForwardVolcengineMedia(context.Background(), c, volcengineActionTestAccount(), VolcengineMediaEndpointImagesGenerations, "", body, "application/json")
			require.NoError(t, err)
			require.Equal(t, testCase.billingTier, result.ImageSize, "the billed size tier must match the Ark size tier")
			require.Equal(t, 1, result.ImageCount)
			require.Equal(t, testCase.expectedCost, *group.GetImagePrice(result.ImageSize), "the group price column of the billed tier must be selected")
		})
	}
}

func TestVolcengineVideoBillingTierMatchesGroupPrice(t *testing.T) {
	price480p, price720p, price1080p := 0.05, 0.08, 0.12
	group := &Group{ID: 24, VideoPrice480P: &price480p, VideoPrice720P: &price720p, VideoPrice1080P: &price1080p}
	for _, testCase := range []struct {
		name         string
		statusBody   string
		expectedTier string
		expectedCost float64
		expectedSecs int
	}{
		{
			name:         "1080p task bills the 1080p tier",
			statusBody:   `{"id":"task-1080","model":"doubao-seedance-2-0-260128","status":"succeeded","content":{"video_url":"https://cdn.example/v.mp4"},"resolution":"1080p","duration":5}`,
			expectedTier: VideoBillingResolution1080P, expectedCost: price1080p, expectedSecs: 5,
		},
		{
			name:         "4k folds onto the highest existing tier",
			statusBody:   `{"id":"task-4k","model":"doubao-seedance-2-0-260128","status":"succeeded","content":{"video_url":"https://cdn.example/v.mp4"},"resolution":"4k","duration":10}`,
			expectedTier: VideoBillingResolution1080P, expectedCost: price1080p, expectedSecs: 10,
		},
		{
			name:         "720p task bills the 720p tier",
			statusBody:   `{"id":"task-720","model":"doubao-seedance-2-0-260128","status":"succeeded","content":{"video_url":"https://cdn.example/v.mp4"},"resolution":"720p","duration":5}`,
			expectedTier: VideoBillingResolution720P, expectedCost: price720p, expectedSecs: 5,
		},
		{
			name:         "480p task bills the 480p tier",
			statusBody:   `{"id":"task-480","model":"doubao-seedance-2-0-260128","status":"succeeded","content":{"video_url":"https://cdn.example/v.mp4"},"resolution":"480p","duration":5}`,
			expectedTier: VideoBillingResolution480P, expectedCost: price480p, expectedSecs: 5,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			result := ExtractVolcengineVideoBillingFromStatusBody([]byte(testCase.statusBody), nil, "task")
			require.NotNil(t, result)
			require.Equal(t, testCase.expectedTier, result.VideoResolution)
			require.Equal(t, testCase.expectedSecs, result.VideoDurationSeconds)
			require.Equal(t, 1, result.VideoCount)
			require.Equal(t, testCase.expectedCost, *group.GetVideoPriceForModel(result.BillingModel, result.VideoResolution),
				"the group price of the billed resolution tier must be selected")

			// Per-model overrides win over the flat columns, like every other platform.
			override := 0.20
			group.VideoModelPrices = map[string]map[string]float64{"doubao-seedance-2-0-260128": {testCase.expectedTier: override}}
			require.Equal(t, override, *group.GetVideoPriceForModel(result.BillingModel, result.VideoResolution))
			group.VideoModelPrices = nil
		})
	}
}

// ---------------------------------------------------------------------------
// Owner-scoped billing idempotency
// ---------------------------------------------------------------------------

func TestVolcengineVideoBillingClaimIsOwnerScoped(t *testing.T) {
	cache := newVolcengineActionFakeCache()
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: &volcengineActionUpstreamStub{}, cache: cache}
	ctx := context.Background()

	ownerClaim, err := svc.ClaimVolcengineVideoBilling(ctx, "task-shared", 10, 20)
	require.NoError(t, err)
	require.True(t, ownerClaim)

	// A different key (or user) observing the same task id must get its own
	// claim instead of consuming another tenant's one-shot claim.
	otherKeyClaim, err := svc.ClaimVolcengineVideoBilling(ctx, "task-shared", 10, 21)
	require.NoError(t, err)
	require.True(t, otherKeyClaim)
	otherUserClaim, err := svc.ClaimVolcengineVideoBilling(ctx, "task-shared", 11, 20)
	require.NoError(t, err)
	require.True(t, otherUserClaim)

	// Each owner still claims exactly once.
	for _, owner := range []struct{ userID, keyID int64 }{{10, 20}, {10, 21}, {11, 20}} {
		again, err := svc.ClaimVolcengineVideoBilling(ctx, "task-shared", owner.userID, owner.keyID)
		require.NoError(t, err)
		require.False(t, again, "owner %d/%d must not be billed twice for one task", owner.userID, owner.keyID)
	}
}

// TestVolcengineGroupImageGenerationDefaults pins the exposure default for new
// Volcengine groups: Ark serves native Seedream image generation, so the switch
// defaults enabled like Grok and MiniMax. It stays an exposure switch only - the
// adapter gate is the action matrix, and batch image generation remains a
// Gemini-only capability because the Ark data plane publishes no batch image API.
func TestVolcengineGroupImageGenerationDefaults(t *testing.T) {
	require.True(t, defaultAllowImageGenerationForPlatform(PlatformVolcengine))
	require.True(t, defaultAllowImageGenerationForPlatform(PlatformGrok))
	require.True(t, defaultAllowImageGenerationForPlatform(PlatformMiniMax))
	require.False(t, defaultAllowImageGenerationForPlatform(PlatformOpenAI))
	require.False(t, defaultAllowImageGenerationForPlatform(PlatformAnthropic))
}

func TestVolcengineVideoPendingBillingIsOwnerScoped(t *testing.T) {
	cache := newVolcengineActionFakeCache()
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: &volcengineActionUpstreamStub{}, cache: cache}
	ctx := context.Background()

	require.NoError(t, svc.StoreVolcengineVideoPendingBilling(ctx, "task-9", 10, 20, VolcengineVideoPendingBilling{Model: "doubao-seedance-2-0-260128"}))
	loaded, err := svc.LoadVolcengineVideoPendingBilling(ctx, "task-9", 10, 20)
	require.NoError(t, err)
	require.NotNil(t, loaded)
	require.Equal(t, "doubao-seedance-2-0-260128", loaded.Model)

	// Another user or key shares no snapshot: the owner-scoped cache key must
	// never resolve to the owner's billing data.
	otherUserPending, err := svc.LoadVolcengineVideoPendingBilling(ctx, "task-9", 11, 20)
	require.NoError(t, err)
	require.Nil(t, otherUserPending, "another user must not read the owner's pending billing snapshot")
	otherKeyPending, err := svc.LoadVolcengineVideoPendingBilling(ctx, "task-9", 10, 21)
	require.NoError(t, err)
	require.Nil(t, otherKeyPending, "another key must not read the owner's pending billing snapshot")
}
