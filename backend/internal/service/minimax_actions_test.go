//go:build unit

package service

import (
	"context"
	"encoding/hex"
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

// MiniMax action adapter tests. Upstream request/response fixtures mirror the
// official MiniMax OpenAPI references captured 2026-09-26 (image-01,
// MiniMax-H3 video V2, t2a_v2, speech_to_text asr-1.0, voice_clone).

type minimaxActionUpstreamStub struct {
	mu        sync.Mutex
	requests  []*http.Request
	bodies    [][]byte
	responses []*http.Response
	err       error
	onRequest func(*http.Request)
}

func (s *minimaxActionUpstreamStub) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
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

func (s *minimaxActionUpstreamStub) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return s.Do(req, proxyURL, accountID, accountConcurrency)
}

func (s *minimaxActionUpstreamStub) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.requests)
}

func (s *minimaxActionUpstreamStub) requestAt(index int) (*http.Request, []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests[index], s.bodies[index]
}

func minimaxActionTestAccount() *Account {
	return &Account{
		ID:       7,
		Platform: PlatformMiniMax,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "minimax-test-key",
			"base_url": "https://api.minimax.io/v1",
		},
	}
}

func minimaxActionTestContext(method, target string, headers map[string]string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(method, target, nil)
	for name, value := range headers {
		c.Request.Header.Set(name, value)
	}
	return c, recorder
}

func minimaxJSONResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

// minimaxActionFakeCache implements the cache surface the video ownership and
// deferred-billing paths use (session binding + pending billing + claim).
type minimaxActionFakeCache struct {
	GatewayCache
	mu       sync.Mutex
	sessions map[string]int64
	pending  map[string][]byte
	claims   map[string]bool
}

func sessionMapKey(groupID int64, key string) string {
	return fmt.Sprintf("%d|%s", groupID, key)
}

func newMinimaxActionFakeCache() *minimaxActionFakeCache {
	return &minimaxActionFakeCache{
		sessions: map[string]int64{},
		pending:  map[string][]byte{},
		claims:   map[string]bool{},
	}
}

func (c *minimaxActionFakeCache) SetSessionAccountID(_ context.Context, groupID int64, key string, accountID int64, _ time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sessions[sessionMapKey(groupID, key)] = accountID
	return nil
}

func (c *minimaxActionFakeCache) GetSessionAccountID(_ context.Context, groupID int64, key string) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	accountID, ok := c.sessions[sessionMapKey(groupID, key)]
	if !ok {
		return 0, ErrStickySessionNotFound
	}
	return accountID, nil
}

func (c *minimaxActionFakeCache) SetGrokVideoPendingBilling(_ context.Context, key string, payload []byte, _ time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pending[key] = append([]byte(nil), payload...)
	return nil
}

func (c *minimaxActionFakeCache) GetGrokVideoPendingBilling(_ context.Context, key string) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	payload, ok := c.pending[key]
	if !ok {
		return nil, nil
	}
	return payload, nil
}

func (c *minimaxActionFakeCache) ClaimGrokVideoBilled(_ context.Context, key string, _ time.Duration) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.claims[key] {
		return false, nil
	}
	c.claims[key] = true
	return true, nil
}

func (c *minimaxActionFakeCache) ReleaseGrokVideoBilled(_ context.Context, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.claims, key)
	return nil
}

// ---------------------------------------------------------------------------
// URL derivation (intl / CN / relay prefix)
// ---------------------------------------------------------------------------

func TestMinimaxAPIRootDerivesMediaOrigin(t *testing.T) {
	t.Run("international", func(t *testing.T) {
		account := minimaxActionTestAccount()
		require.Equal(t, "https://api.minimax.io", minimaxAPIRoot(account))
		require.Equal(t, "https://api.minimax.io/v1/image_generation", buildMiniMaxActionURL(account, "/v1/image_generation"))
		require.Equal(t, "https://api.minimax.io/v2/video_generation", buildMiniMaxActionURL(account, "/v2/video_generation"))
	})
	t.Run("china default", func(t *testing.T) {
		account := minimaxActionTestAccount()
		delete(account.Credentials, "base_url")
		require.Equal(t, "https://api.minimaxi.com", minimaxAPIRoot(account))
		require.Equal(t, "https://api.minimaxi.com/v1/t2a_v2", buildMiniMaxActionURL(account, "/v1/t2a_v2"))
	})
	t.Run("relay path prefix preserved", func(t *testing.T) {
		account := minimaxActionTestAccount()
		account.Credentials["base_url"] = "https://relay.example/mm/v1"
		require.Equal(t, "https://relay.example/mm", minimaxAPIRoot(account))
		require.Equal(t, "https://relay.example/mm/v2/query/video_generation/task-1", buildMiniMaxActionURL(account, minimaxVideoStatusPath("task-1")))
	})
}

// ---------------------------------------------------------------------------
// Image generation bridging
// ---------------------------------------------------------------------------

func TestBuildMiniMaxImageBodyBridgesOpenAIShape(t *testing.T) {
	seed := int64(42)
	info := MiniMaxMediaRequestInfo{
		Prompt:         "a cat on the moon",
		Model:          "image-01",
		N:              3,
		AspectRatio:    "16:9",
		ResponseFormat: "b64_json",
		Seed:           &seed,
	}
	body, err := buildMiniMaxImageBody(MiniMaxMediaEndpointImagesGenerations, info, "image-01")
	require.NoError(t, err)
	require.Equal(t, "image-01", gjson.GetBytes(body, "model").String())
	require.Equal(t, "a cat on the moon", gjson.GetBytes(body, "prompt").String())
	require.Equal(t, int64(3), gjson.GetBytes(body, "n").Int())
	require.Equal(t, "16:9", gjson.GetBytes(body, "aspect_ratio").String())
	require.Equal(t, "base64", gjson.GetBytes(body, "response_format").String(), "OpenAI b64_json must map to MiniMax base64")
	require.Equal(t, int64(42), gjson.GetBytes(body, "seed").Int())
}

func TestBuildMiniMaxImageBodySizeToWidthHeight(t *testing.T) {
	info := MiniMaxMediaRequestInfo{Prompt: "p", Size: "1024x1024"}
	body, err := buildMiniMaxImageBody(MiniMaxMediaEndpointImagesGenerations, info, "image-01")
	require.NoError(t, err)
	require.Equal(t, int64(1024), gjson.GetBytes(body, "width").Int())
	require.Equal(t, int64(1024), gjson.GetBytes(body, "height").Int())
	require.False(t, gjson.GetBytes(body, "aspect_ratio").Exists())

	_, err = buildMiniMaxImageBody(MiniMaxMediaEndpointImagesGenerations, MiniMaxMediaRequestInfo{Prompt: "p", Size: "300x300"}, "image-01")
	require.Error(t, err, "out-of-range size must be rejected")
}

func TestBuildMiniMaxImageBodyRejectsBadInput(t *testing.T) {
	_, err := buildMiniMaxImageBody(MiniMaxMediaEndpointImagesGenerations, MiniMaxMediaRequestInfo{Model: "image-01"}, "image-01")
	require.ErrorContains(t, err, "prompt is required")

	_, err = buildMiniMaxImageBody(MiniMaxMediaEndpointImagesGenerations, MiniMaxMediaRequestInfo{Prompt: "p", ResponseFormat: "png"}, "image-01")
	require.ErrorContains(t, err, "response_format")
}

func TestBuildMiniMaxImageBodyEditsSubjectReference(t *testing.T) {
	info := MiniMaxMediaRequestInfo{
		Prompt:         "same person, new background",
		InputImageURLs: []string{"https://cdn.example/portrait.jpg"},
	}
	body, err := buildMiniMaxImageBody(MiniMaxMediaEndpointImagesEdits, info, "image-01")
	require.NoError(t, err)
	require.Equal(t, "character", gjson.GetBytes(body, "subject_reference.0.type").String())
	require.Equal(t, "https://cdn.example/portrait.jpg", gjson.GetBytes(body, "subject_reference.0.image_file").String())

	_, err = buildMiniMaxImageBody(MiniMaxMediaEndpointImagesEdits, MiniMaxMediaRequestInfo{Prompt: "p"}, "image-01")
	require.ErrorContains(t, err, "image is required")

	_, err = buildMiniMaxImageBody(MiniMaxMediaEndpointImagesEdits, MiniMaxMediaRequestInfo{
		Prompt:         "p",
		MaskUpload:     &OpenAIImagesUpload{FieldName: "mask", FileName: "mask.png", Data: []byte("mask")},
		InputImageURLs: []string{"https://cdn.example/a.jpg"},
	}, "image-01")
	require.ErrorContains(t, err, "mask is not supported")

	_, err = buildMiniMaxImageBody(MiniMaxMediaEndpointImagesEdits, MiniMaxMediaRequestInfo{
		Prompt:         "p",
		InputImageURLs: []string{"https://cdn.example/a.jpg", "https://cdn.example/b.jpg"},
	}, "image-01")
	require.ErrorContains(t, err, "exactly one")
}

func TestBridgeMiniMaxImageResponsePositive(t *testing.T) {
	upstream := `{"id":"trace-1","data":{"image_urls":["https://cdn.example/1.png","https://cdn.example/2.png"]},"metadata":{"success_count":2,"failed_count":0},"base_resp":{"status_code":0,"status_msg":"success"}}`
	bridged, count, err := bridgeMiniMaxImageResponse([]byte(upstream))
	require.NoError(t, err)
	require.Equal(t, 2, count)
	require.Equal(t, "https://cdn.example/1.png", gjson.GetBytes(bridged, "data.0.url").String())
	require.Equal(t, "https://cdn.example/2.png", gjson.GetBytes(bridged, "data.1.url").String())
	require.True(t, gjson.GetBytes(bridged, "created").Exists())
}

func TestBridgeMiniMaxImageResponseBase64(t *testing.T) {
	upstream := `{"id":"trace-2","data":{"image_base64":["QUJD"]},"metadata":{"success_count":1,"failed_count":0},"base_resp":{"status_code":0}}`
	bridged, count, err := bridgeMiniMaxImageResponse([]byte(upstream))
	require.NoError(t, err)
	require.Equal(t, 1, count)
	require.Equal(t, "QUJD", gjson.GetBytes(bridged, "data.0.b64_json").String())
}

func TestBridgeMiniMaxImageResponseBusinessError(t *testing.T) {
	upstream := `{"id":"trace-3","data":{},"metadata":{"success_count":0,"failed_count":1},"base_resp":{"status_code":1026,"status_msg":"sensitive content detected"}}`
	_, _, err := bridgeMiniMaxImageResponse([]byte(upstream))
	var businessErr *MiniMaxBusinessError
	require.True(t, errors.As(err, &businessErr))
	require.Equal(t, 1026, businessErr.Code)
	require.Equal(t, http.StatusUnprocessableEntity, businessErr.httpStatus())
	require.False(t, businessErr.failoverable())
}

func TestForwardMiniMaxMediaImageGenerationReachesUpstream(t *testing.T) {
	upstream := &minimaxActionUpstreamStub{
		responses: []*http.Response{minimaxJSONResponse(http.StatusOK,
			`{"id":"trace-9","data":{"image_urls":["https://cdn.example/out.png"]},"metadata":{"success_count":1,"failed_count":0},"base_resp":{"status_code":0}}`)},
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	c, recorder := minimaxActionTestContext(http.MethodPost, "/v1/images/generations", nil)
	body := []byte(`{"model":"image-01","prompt":"a red panda","n":1,"aspect_ratio":"1:1"}`)
	result, err := svc.ForwardMiniMaxMedia(context.Background(), c, minimaxActionTestAccount(), MiniMaxMediaEndpointImagesGenerations, "", body, "application/json")
	require.NoError(t, err)
	require.NotNil(t, result)

	require.Equal(t, 1, upstream.callCount())
	req, reqBody := upstream.requestAt(0)
	require.Equal(t, "https://api.minimax.io/v1/image_generation", req.URL.String())
	require.Equal(t, http.MethodPost, req.Method)
	require.Equal(t, "Bearer minimax-test-key", req.Header.Get("Authorization"))
	require.Equal(t, "application/json", req.Header.Get("Content-Type"))
	require.Equal(t, "image-01", gjson.GetBytes(reqBody, "model").String())
	require.Equal(t, "a red panda", gjson.GetBytes(reqBody, "prompt").String())
	require.Equal(t, "1:1", gjson.GetBytes(reqBody, "aspect_ratio").String())

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "https://cdn.example/out.png", gjson.GetBytes(recorder.Body.Bytes(), "data.0.url").String())
	require.Equal(t, 1, result.ImageCount)
	require.Equal(t, "image-01", result.UpstreamModel)
}

func TestForwardMiniMaxMediaImageEditMultipartBridge(t *testing.T) {
	upstream := &minimaxActionUpstreamStub{
		responses: []*http.Response{minimaxJSONResponse(http.StatusOK,
			`{"id":"trace-edit","data":{"image_urls":["https://cdn.example/edited.png"]},"metadata":{"success_count":1,"failed_count":0},"base_resp":{"status_code":0}}`)},
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	c, recorder := minimaxActionTestContext(http.MethodPost, "/v1/images/edits", nil)

	var multipartBody strings.Builder
	multipartBody.WriteString("--eboundary\r\nContent-Disposition: form-data; name=\"model\"\r\n\r\nimage-01\r\n")
	multipartBody.WriteString("--eboundary\r\nContent-Disposition: form-data; name=\"prompt\"\r\n\r\nsame person on a beach\r\n")
	multipartBody.WriteString("--eboundary\r\nContent-Disposition: form-data; name=\"image\"; filename=\"portrait.png\"\r\nContent-Type: image/png\r\n\r\nPNGDATA\r\n")
	multipartBody.WriteString("--eboundary--\r\n")
	contentType := "multipart/form-data; boundary=eboundary"

	result, err := svc.ForwardMiniMaxMedia(context.Background(), c, minimaxActionTestAccount(), MiniMaxMediaEndpointImagesEdits, "", []byte(multipartBody.String()), contentType)
	require.NoError(t, err)
	require.NotNil(t, result)

	req, reqBody := upstream.requestAt(0)
	require.Equal(t, "https://api.minimax.io/v1/image_generation", req.URL.String())
	require.Equal(t, "application/json", req.Header.Get("Content-Type"), "multipart edits bridge to the official JSON body")
	require.Equal(t, "image-01", gjson.GetBytes(reqBody, "model").String())
	require.Equal(t, "same person on a beach", gjson.GetBytes(reqBody, "prompt").String())
	require.Equal(t, "character", gjson.GetBytes(reqBody, "subject_reference.0.type").String())
	require.True(t, strings.HasPrefix(gjson.GetBytes(reqBody, "subject_reference.0.image_file").String(), "data:image/png;base64,"),
		"the uploaded reference becomes an official data-URL subject_reference")

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "https://cdn.example/edited.png", gjson.GetBytes(recorder.Body.Bytes(), "data.0.url").String())
	require.Equal(t, 1, result.ImageCount)
}

func TestForwardMiniMaxMediaImageBusinessErrorClientFault(t *testing.T) {
	upstream := &minimaxActionUpstreamStub{
		responses: []*http.Response{minimaxJSONResponse(http.StatusOK,
			`{"id":"t","data":{},"metadata":{"success_count":0,"failed_count":0},"base_resp":{"status_code":2013,"status_msg":"invalid input parameters"}}`)},
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	c, recorder := minimaxActionTestContext(http.MethodPost, "/v1/images/generations", nil)
	_, err := svc.ForwardMiniMaxMedia(context.Background(), c, minimaxActionTestAccount(), MiniMaxMediaEndpointImagesGenerations, "", []byte(`{"model":"image-01","prompt":"p"}`), "application/json")
	require.Error(t, err)
	var failoverErr *UpstreamFailoverError
	require.False(t, errors.As(err, &failoverErr), "request-scoped validation failures must not fail over")
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Equal(t, "invalid_request_error", gjson.GetBytes(recorder.Body.Bytes(), "error.type").String())
}

func TestForwardMiniMaxMediaImageBusinessErrorFailoverable(t *testing.T) {
	upstream := &minimaxActionUpstreamStub{
		responses: []*http.Response{minimaxJSONResponse(http.StatusOK,
			`{"id":"t","data":{},"metadata":{"success_count":0,"failed_count":0},"base_resp":{"status_code":1008,"status_msg":"insufficient balance"}}`)},
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	c, _ := minimaxActionTestContext(http.MethodPost, "/v1/images/generations", nil)
	_, err := svc.ForwardMiniMaxMedia(context.Background(), c, minimaxActionTestAccount(), MiniMaxMediaEndpointImagesGenerations, "", []byte(`{"model":"image-01","prompt":"p"}`), "application/json")
	require.Error(t, err)
	var failoverErr *UpstreamFailoverError
	require.True(t, errors.As(err, &failoverErr), "account-scoped balance failure must fail over to the next account")
	require.Equal(t, http.StatusPaymentRequired, failoverErr.StatusCode)
}

func TestMinimaxBusinessErrorMapping(t *testing.T) {
	cases := []struct {
		code         int
		status       int
		failoverable bool
	}{
		{1004, http.StatusUnauthorized, true},
		{2049, http.StatusUnauthorized, true},
		{1008, http.StatusPaymentRequired, true},
		{1002, http.StatusTooManyRequests, true},
		{1039, http.StatusTooManyRequests, true},
		{1026, http.StatusUnprocessableEntity, false},
		{2013, http.StatusBadRequest, false},
		{2038, http.StatusForbidden, false},
		{1000, http.StatusBadGateway, true},
	}
	for _, tc := range cases {
		businessErr := &MiniMaxBusinessError{Code: tc.code, Message: "m"}
		require.Equal(t, tc.status, businessErr.httpStatus(), "code %d", tc.code)
		require.Equal(t, tc.failoverable, businessErr.failoverable(), "code %d", tc.code)
	}
}

// ---------------------------------------------------------------------------
// Video generation (V2) bridging, ownership, deferred billing
// ---------------------------------------------------------------------------

func TestBuildMiniMaxVideoCreateBody(t *testing.T) {
	t.Run("text to video", func(t *testing.T) {
		info := MiniMaxMediaRequestInfo{Prompt: "ocean waves", Resolution: "720p", DurationSeconds: 6, AspectRatio: "16:9"}
		body, err := buildMiniMaxVideoCreateBody(info, "MiniMax-H3")
		require.NoError(t, err)
		require.Equal(t, "MiniMax-H3", gjson.GetBytes(body, "model").String())
		require.Equal(t, "text", gjson.GetBytes(body, "content.0.type").String())
		require.Equal(t, "ocean waves", gjson.GetBytes(body, "content.0.text").String())
		require.Equal(t, "768P", gjson.GetBytes(body, "resolution").String(), "720p must map to the official 768P tier")
		require.Equal(t, int64(6), gjson.GetBytes(body, "duration").Int())
		require.Equal(t, "16:9", gjson.GetBytes(body, "ratio").String())
	})
	t.Run("first and last frame", func(t *testing.T) {
		info := MiniMaxMediaRequestInfo{
			Prompt:         "zoom in",
			InputImageURLs: []string{"https://cdn.example/first.png", "https://cdn.example/last.png"},
		}
		body, err := buildMiniMaxVideoCreateBody(info, "MiniMax-H3-Max")
		require.NoError(t, err)
		require.Equal(t, "first_frame", gjson.GetBytes(body, "content.1.role").String())
		require.Equal(t, "last_frame", gjson.GetBytes(body, "content.2.role").String())
		require.Equal(t, "https://cdn.example/first.png", gjson.GetBytes(body, "content.1.image_url.url").String())
	})
	t.Run("resolution folding and validation", func(t *testing.T) {
		for input, expected := range map[string]string{"480p": "480P", "720p": "768P", "768P": "768P", "1080p": "2K", "2K": "2K"} {
			mapped, err := minimaxVideoUpstreamResolution(input)
			require.NoError(t, err)
			require.Equal(t, expected, mapped)
		}
		_, err := minimaxVideoUpstreamResolution("4k")
		require.Error(t, err)
	})
	t.Run("rejects invalid input", func(t *testing.T) {
		_, err := buildMiniMaxVideoCreateBody(MiniMaxMediaRequestInfo{Resolution: "720p"}, "MiniMax-H3")
		require.ErrorContains(t, err, "prompt is required")
		_, err = buildMiniMaxVideoCreateBody(MiniMaxMediaRequestInfo{
			Prompt:         "p",
			InputImageURLs: []string{"a", "b", "c"},
		}, "MiniMax-H3")
		require.ErrorContains(t, err, "at most")
	})
}

func TestForwardMiniMaxMediaVideoCreateReturnsTaskID(t *testing.T) {
	upstream := &minimaxActionUpstreamStub{
		responses: []*http.Response{minimaxJSONResponse(http.StatusOK, `{"task_id":"424010985738629"}`)},
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	c, recorder := minimaxActionTestContext(http.MethodPost, "/v1/videos/generations", nil)
	body := []byte(`{"model":"MiniMax-H3","prompt":"a boy playing basketball by the sea","resolution":"1080p","duration":5,"aspect_ratio":"16:9"}`)
	result, err := svc.ForwardMiniMaxMedia(context.Background(), c, minimaxActionTestAccount(), MiniMaxMediaEndpointVideosGenerations, "", body, "application/json")
	require.NoError(t, err)
	require.NotNil(t, result)

	req, reqBody := upstream.requestAt(0)
	require.Equal(t, "https://api.minimax.io/v2/video_generation", req.URL.String())
	require.Equal(t, http.MethodPost, req.Method)
	require.Equal(t, "Bearer minimax-test-key", req.Header.Get("Authorization"))
	require.Equal(t, "MiniMax-H3", gjson.GetBytes(reqBody, "model").String())
	require.Equal(t, "2K", gjson.GetBytes(reqBody, "resolution").String())
	require.Equal(t, int64(5), gjson.GetBytes(reqBody, "duration").Int())

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "424010985738629", gjson.GetBytes(recorder.Body.Bytes(), "task_id").String())
	require.Equal(t, "424010985738629", result.ResponseID)
	require.Equal(t, VideoBillingResolution1080P, result.VideoResolution, "2K folds onto the highest existing price tier")
	require.Equal(t, 5, result.VideoDurationSeconds)
}

const minimaxVideoSucceededBody = `{"task":{"id":"424010985738629","model":"MiniMax-H3","status":"succeeded","created_at":1785125529,"updated_at":1785125946,"content":{"url":"https://video-product.cdn.minimax.io/inference_output/rollout/2026-07-27/abc/output.mp4"},"resolution":"2K","duration":5,"usage":{"total_seconds":5,"input_seconds":0,"output_seconds":5,"input_image_count":0},"ratio":"16:9","task_type":"generation","modality":"video"}}`

func TestIsMiniMaxVideoStatusBillable(t *testing.T) {
	require.True(t, IsMiniMaxVideoStatusBillable([]byte(minimaxVideoSucceededBody)))
	notBillable := []string{
		`{"task":{"id":"t","status":"queued"}}`,
		`{"task":{"id":"t","status":"running"}}`,
		`{"task":{"id":"t","status":"failed","error":{"code":"1026","message":"sensitive"}}}`,
		`{"task":{"id":"t","status":"cancelled"}}`,
		`{"task":{"id":"t","status":"succeeded","content":{}}}`,
		`{"task":{"id":"t","status":"succeeded","content":{"prompt":"enhanced prompt only"}}}`,
		`not json`,
		``,
	}
	for _, body := range notBillable {
		require.False(t, IsMiniMaxVideoStatusBillable([]byte(body)), body)
	}
}

func TestExtractMiniMaxVideoBillingFromStatusBody(t *testing.T) {
	t.Run("official succeeded shape", func(t *testing.T) {
		billed := ExtractMiniMaxVideoBillingFromStatusBody([]byte(minimaxVideoSucceededBody), nil, "")
		require.NotNil(t, billed)
		require.Equal(t, "424010985738629", billed.ResponseID)
		require.Equal(t, "MiniMax-H3", billed.Model)
		require.Equal(t, 1, billed.VideoCount)
		require.Equal(t, 5, billed.VideoDurationSeconds, "usage.output_seconds is the official metered duration")
		require.Equal(t, VideoBillingResolution1080P, billed.VideoResolution, "2K folds onto the 1080p price tier")
	})
	t.Run("pending snapshot fills gaps", func(t *testing.T) {
		statusBody := `{"task":{"id":"task-1","status":"succeeded","content":{"url":"https://cdn.example/v.mp4"}}}`
		pending := &MiniMaxVideoPendingBilling{Model: "MiniMax-H3", BillingModel: "MiniMax-H3", UpstreamModel: "MiniMax-H3", VideoResolution: VideoBillingResolution720P, VideoDurationSeconds: 8}
		billed := ExtractMiniMaxVideoBillingFromStatusBody([]byte(statusBody), pending, "")
		require.NotNil(t, billed)
		require.Equal(t, "MiniMax-H3", billed.Model)
		require.Equal(t, VideoBillingResolution720P, billed.VideoResolution)
		require.Equal(t, 8, billed.VideoDurationSeconds)
	})
	t.Run("not billable", func(t *testing.T) {
		require.Nil(t, ExtractMiniMaxVideoBillingFromStatusBody([]byte(`{"task":{"id":"t","status":"running"}}`), nil, ""))
	})
}

func TestForwardMiniMaxMediaVideoStatusRewritesProxyURL(t *testing.T) {
	upstream := &minimaxActionUpstreamStub{
		responses: []*http.Response{minimaxJSONResponse(http.StatusOK, minimaxVideoSucceededBody)},
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	c, recorder := minimaxActionTestContext(http.MethodGet, "/v1/videos/424010985738629", nil)
	result, err := svc.ForwardMiniMaxMedia(context.Background(), c, minimaxActionTestAccount(), MiniMaxMediaEndpointVideoStatus, "424010985738629", nil, "")
	require.NoError(t, err)
	require.NotNil(t, result)

	req, _ := upstream.requestAt(0)
	require.Equal(t, "https://api.minimax.io/v2/query/video_generation/424010985738629", req.URL.String())
	require.Equal(t, http.MethodGet, req.Method)
	require.Equal(t, "Bearer minimax-test-key", req.Header.Get("Authorization"))

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "/v1/videos/424010985738629/content", gjson.GetBytes(recorder.Body.Bytes(), "task.content.url").String(),
		"time-limited CDN URLs must be rewritten to the owner-checked gateway proxy")
	require.Equal(t, "succeeded", gjson.GetBytes(recorder.Body.Bytes(), "task.status").String())
	require.Equal(t, 1, result.VideoCount)
	require.Equal(t, 5, result.VideoDurationSeconds)
}

func TestForwardMiniMaxMediaVideoContentStreamsRange(t *testing.T) {
	upstream := &minimaxActionUpstreamStub{
		responses: []*http.Response{
			minimaxJSONResponse(http.StatusOK, minimaxVideoSucceededBody),
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
	c, recorder := minimaxActionTestContext(http.MethodGet, "/v1/videos/424010985738629/content", map[string]string{"Range": "bytes=0-12"})
	result, err := svc.ForwardMiniMaxMedia(context.Background(), c, minimaxActionTestAccount(), MiniMaxMediaEndpointVideoContent, "424010985738629", nil, "")
	require.NoError(t, err)
	require.NotNil(t, result)

	require.Equal(t, 2, upstream.callCount())
	statusReq, _ := upstream.requestAt(0)
	require.Equal(t, "https://api.minimax.io/v2/query/video_generation/424010985738629", statusReq.URL.String())
	contentReq, _ := upstream.requestAt(1)
	require.Equal(t, "https://video-product.cdn.minimax.io/inference_output/rollout/2026-07-27/abc/output.mp4", contentReq.URL.String())
	require.Empty(t, contentReq.Header.Get("Authorization"), "signed CDN downloads must not leak the account credential")
	require.Equal(t, "bytes=0-12", contentReq.Header.Get("Range"))

	require.Equal(t, http.StatusPartialContent, recorder.Code)
	require.Equal(t, "video-payload", recorder.Body.String())
	require.Equal(t, "bytes 0-12/100", recorder.Header().Get("Content-Range"))
	require.Equal(t, 1, result.VideoCount)
}

func TestForwardMiniMaxMediaVideoContentRejectsNonHTTPSURL(t *testing.T) {
	insecureBody := strings.Replace(minimaxVideoSucceededBody, "https://video-product.cdn.minimax.io", "http://video-product.cdn.minimax.io", 1)
	upstream := &minimaxActionUpstreamStub{
		responses: []*http.Response{minimaxJSONResponse(http.StatusOK, insecureBody)},
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	c, _ := minimaxActionTestContext(http.MethodGet, "/v1/videos/424010985738629/content", nil)
	_, err := svc.ForwardMiniMaxMedia(context.Background(), c, minimaxActionTestAccount(), MiniMaxMediaEndpointVideoContent, "424010985738629", nil, "")
	require.Error(t, err)
	require.Equal(t, 1, upstream.callCount(), "an insecure content URL must be rejected before any download")
}

func TestForwardMiniMaxMediaVideoContentNotReady(t *testing.T) {
	upstream := &minimaxActionUpstreamStub{
		responses: []*http.Response{minimaxJSONResponse(http.StatusOK, `{"task":{"id":"task-9","status":"running"}}`)},
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	c, recorder := minimaxActionTestContext(http.MethodGet, "/v1/videos/task-9/content", nil)
	_, err := svc.ForwardMiniMaxMedia(context.Background(), c, minimaxActionTestAccount(), MiniMaxMediaEndpointVideoContent, "task-9", nil, "")
	require.Error(t, err)
	require.Equal(t, http.StatusConflict, recorder.Code)
	require.Equal(t, 1, upstream.callCount())
}

func TestForwardMiniMaxMediaVideoCancelQueuedTask(t *testing.T) {
	upstream := &minimaxActionUpstreamStub{
		responses: []*http.Response{minimaxJSONResponse(http.StatusOK, `{"task_id":"task-3","action":"cancelled","status":"cancelled"}`)},
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	c, recorder := minimaxActionTestContext(http.MethodDelete, "/v1/videos/task-3", nil)
	result, err := svc.ForwardMiniMaxMedia(context.Background(), c, minimaxActionTestAccount(), MiniMaxMediaEndpointVideoCancel, "task-3", nil, "")
	require.NoError(t, err)
	require.NotNil(t, result)

	req, body := upstream.requestAt(0)
	require.Equal(t, http.MethodDelete, req.Method)
	require.Equal(t, "https://api.minimax.io/v2/video_generation/task-3", req.URL.String())
	require.Equal(t, "Bearer minimax-test-key", req.Header.Get("Authorization"))
	require.Nil(t, body)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "cancelled", gjson.GetBytes(recorder.Body.Bytes(), "action").String())
	require.Equal(t, "task-3", result.ResponseID)
}

func TestForwardMiniMaxMediaVideoCancelRunningTaskFails(t *testing.T) {
	upstream := &minimaxActionUpstreamStub{
		responses: []*http.Response{minimaxJSONResponse(http.StatusBadRequest,
			`{"type":"error","error":{"type":"bad_request_error","message":"cannot cancel a running task (2013)","http_code":"400"},"request_id":"req-1"}`)},
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	c, recorder := minimaxActionTestContext(http.MethodDelete, "/v1/videos/task-4", nil)
	_, err := svc.ForwardMiniMaxMedia(context.Background(), c, minimaxActionTestAccount(), MiniMaxMediaEndpointVideoCancel, "task-4", nil, "")
	require.Error(t, err)
	var failoverErr *UpstreamFailoverError
	require.False(t, errors.As(err, &failoverErr), "a running-task cancel rejection must not fail over")
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), "cannot cancel a running task")
}

func TestMinimaxVideoOwnershipAndPendingBillingLifecycle(t *testing.T) {
	cache := newMinimaxActionFakeCache()
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: &minimaxActionUpstreamStub{}, cache: cache}
	ctx := context.Background()
	groupID := int64(24)

	require.NoError(t, svc.BindMiniMaxVideoRequestAccount(ctx, &groupID, "task-1", 10, 20, 7))
	owner, err := svc.ResolveMiniMaxVideoRequestAccount(ctx, &groupID, "task-1", 10, 20)
	require.NoError(t, err)
	require.Equal(t, int64(7), owner)

	// Cross-tenant probes: other user, other key, other task must not resolve.
	for _, probe := range []struct {
		userID, keyID int64
		taskID        string
	}{{11, 20, "task-1"}, {10, 21, "task-1"}, {10, 20, "task-2"}} {
		_, err := svc.ResolveMiniMaxVideoRequestAccount(ctx, &groupID, probe.taskID, probe.userID, probe.keyID)
		require.Error(t, err, "task %s user %d key %d must not resolve another tenant's binding", probe.taskID, probe.userID, probe.keyID)
	}

	pending := MiniMaxVideoPendingBilling{Model: "MiniMax-H3", BillingModel: "MiniMax-H3", VideoResolution: "768P", VideoDurationSeconds: 5}
	require.NoError(t, svc.StoreMiniMaxVideoPendingBilling(ctx, "task-1", 10, 20, pending))
	loaded, err := svc.LoadMiniMaxVideoPendingBilling(ctx, "task-1", 10, 20)
	require.NoError(t, err)
	require.NotNil(t, loaded)
	require.Equal(t, "MiniMax-H3", loaded.Model)
	require.Equal(t, VideoBillingResolution720P, loaded.VideoResolution, "768P folds onto the 720p price tier")
	require.NotEmpty(t, loaded.CreatedAt, "create-accept time is always stamped for E2E duration")

	claimed, err := svc.ClaimMiniMaxVideoBilling(ctx, "task-1", 10, 20)
	require.NoError(t, err)
	require.True(t, claimed, "first completion observation claims billing")
	for range 3 {
		again, err := svc.ClaimMiniMaxVideoBilling(ctx, "task-1", 10, 20)
		require.NoError(t, err)
		require.False(t, again, "repeated status/content polls must never double-bill")
	}
	require.NoError(t, svc.ReleaseMiniMaxVideoBilling(ctx, "task-1", 10, 20))
	reclaimed, err := svc.ClaimMiniMaxVideoBilling(ctx, "task-1", 10, 20)
	require.NoError(t, err)
	require.True(t, reclaimed, "a released claim (failed RecordUsage) is billable again")
}

func TestStableMiniMaxBillingRequestIDs(t *testing.T) {
	require.Equal(t, "minimax_video:task-1", StableMiniMaxVideoBillingRequestID("task-1"))
	require.Equal(t, "minimax_video:task-1", StableMiniMaxVideoBillingRequestID("minimax_video:task-1"))
	require.Empty(t, StableMiniMaxVideoBillingRequestID(""))
	require.True(t, isForcedUsageBillingRequestID("minimax_video:task-1"), "forced IDs survive client-supplied request IDs across polls")
	require.True(t, isForcedUsageBillingRequestID(StableMiniMaxAudioBillingRequestID("trace-1")))
}

// ---------------------------------------------------------------------------
// Cancellation propagation
// ---------------------------------------------------------------------------

func TestForwardMiniMaxMediaCancellationPropagates(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	upstream := &minimaxActionUpstreamStub{
		onRequest: func(req *http.Request) { cancel() },
		err:       context.Canceled,
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	c, recorder := minimaxActionTestContext(http.MethodPost, "/v1/images/generations", nil)
	_, err := svc.ForwardMiniMaxMedia(ctx, c, minimaxActionTestAccount(), MiniMaxMediaEndpointImagesGenerations, "", []byte(`{"model":"image-01","prompt":"p"}`), "application/json")
	require.Error(t, err)
	require.True(t, errors.Is(err, context.Canceled), "client cancellation must abort the upstream attempt without failover")
	var failoverErr *UpstreamFailoverError
	require.False(t, errors.As(err, &failoverErr))
	require.Equal(t, http.StatusOK, recorder.Code, "nothing is written to a disconnected client")
	require.Empty(t, recorder.Body.String())
}

// ---------------------------------------------------------------------------
// TTS (t2a_v2)
// ---------------------------------------------------------------------------

func TestBuildMiniMaxTTSBody(t *testing.T) {
	body, err := buildMiniMaxTTSBody([]byte(`{"model":"speech-02-hd","input":"hello world","voice":"English_expressive_narrator","speed":1.2,"response_format":"wav"}`), "speech-02-hd")
	require.NoError(t, err)
	require.Equal(t, "speech-02-hd", gjson.GetBytes(body, "model").String())
	require.Equal(t, "hello world", gjson.GetBytes(body, "text").String())
	require.False(t, gjson.GetBytes(body, "stream").Bool())
	require.Equal(t, "hex", gjson.GetBytes(body, "output_format").String())
	require.Equal(t, "English_expressive_narrator", gjson.GetBytes(body, "voice_setting.voice_id").String())
	require.Equal(t, 1.2, gjson.GetBytes(body, "voice_setting.speed").Float())
	require.Equal(t, "wav", gjson.GetBytes(body, "audio_setting.format").String())
}

func TestBuildMiniMaxTTSBodyRejects(t *testing.T) {
	_, err := buildMiniMaxTTSBody([]byte(`{"voice":"v"}`), "speech-2.8-hd")
	require.ErrorContains(t, err, "input is required")
	_, err = buildMiniMaxTTSBody([]byte(`{"input":"hello"}`), "speech-2.8-hd")
	require.ErrorContains(t, err, "voice is required")
	_, err = buildMiniMaxTTSBody([]byte(`{"input":"hello","voice":"v","response_format":"aac"}`), "speech-2.8-hd")
	require.ErrorContains(t, err, "response_format")
}

func TestDecodeMiniMaxTTSResponse(t *testing.T) {
	audio := []byte("ID3-fake-mp3-bytes")
	upstream := `{"data":{"audio":"` + hex.EncodeToString(audio) + `","status":2},"extra_info":{"audio_length":11124,"audio_sample_rate":32000,"audio_size":179926,"bitrate":128000,"word_count":163,"usage_characters":163,"audio_format":"mp3","audio_channel":1},"trace_id":"01b8bf9bb7433cc75c18eee6cfa8fe21","base_resp":{"status_code":0,"status_msg":"success"}}`
	decoded, err := decodeMiniMaxTTSResponse([]byte(upstream))
	require.NoError(t, err)
	require.Equal(t, audio, decoded.Audio)
	require.Equal(t, int64(163), decoded.UsageCharacters)
	require.Equal(t, "audio/mpeg", decoded.ContentType)
	require.Equal(t, "01b8bf9bb7433cc75c18eee6cfa8fe21", decoded.TraceID)

	_, err = decodeMiniMaxTTSResponse([]byte(`{"data":{"audio":"zz"},"base_resp":{"status_code":0}}`))
	require.Error(t, err, "invalid hex audio must fail closed")
	_, err = decodeMiniMaxTTSResponse([]byte(`{"base_resp":{"status_code":1004,"status_msg":"invalid api key"}}`))
	var businessErr *MiniMaxBusinessError
	require.True(t, errors.As(err, &businessErr))
	require.Equal(t, 1004, businessErr.Code)
}

func TestForwardMiniMaxVoiceTTSReachesUpstream(t *testing.T) {
	audio := []byte("mp3-audio")
	upstream := &minimaxActionUpstreamStub{
		responses: []*http.Response{minimaxJSONResponse(http.StatusOK,
			`{"data":{"audio":"`+hex.EncodeToString(audio)+`","status":2},"extra_info":{"audio_length":1200,"usage_characters":42,"audio_format":"mp3"},"trace_id":"trace-tts-1","base_resp":{"status_code":0}}`)},
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	c, recorder := minimaxActionTestContext(http.MethodPost, "/v1/audio/speech", nil)
	body := []byte(`{"model":"speech-2.8-hd","input":"hello minimax","voice":"English_Graceful_Lady"}`)
	result, err := svc.ForwardMiniMaxVoice(context.Background(), c, minimaxActionTestAccount(), MiniMaxVoiceEndpointTTS, body, "application/json")
	require.NoError(t, err)
	require.NotNil(t, result)

	req, reqBody := upstream.requestAt(0)
	require.Equal(t, "https://api.minimax.io/v1/t2a_v2", req.URL.String())
	require.Equal(t, "Bearer minimax-test-key", req.Header.Get("Authorization"))
	require.Equal(t, "speech-2.8-hd", gjson.GetBytes(reqBody, "model").String())
	require.Equal(t, "hello minimax", gjson.GetBytes(reqBody, "text").String())
	require.Equal(t, "English_Graceful_Lady", gjson.GetBytes(reqBody, "voice_setting.voice_id").String())
	require.Equal(t, "hex", gjson.GetBytes(reqBody, "output_format").String())

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "audio/mpeg", recorder.Header().Get("Content-Type"))
	require.Equal(t, audio, recorder.Body.Bytes())
	require.NotNil(t, result.AudioUsage)
	require.Equal(t, "tts", result.AudioUsage.Mode)
	require.InDelta(t, 42.0/1_000_000.0, result.AudioUsage.DurationOrUnits, 1e-12, "billing uses official usage_characters")
	require.True(t, strings.HasPrefix(result.RequestID, "minimax_audio:"), "audio billing IDs are forced durable")
}

func TestForwardMiniMaxVoiceRejectsWrongPlatform(t *testing.T) {
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: &minimaxActionUpstreamStub{}}
	c, _ := minimaxActionTestContext(http.MethodPost, "/v1/audio/speech", nil)
	account := minimaxActionTestAccount()
	account.Platform = PlatformGrok
	_, err := svc.ForwardMiniMaxVoice(context.Background(), c, account, MiniMaxVoiceEndpointTTS, []byte(`{"input":"x","voice":"v"}`), "application/json")
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// STT (speech_to_text)
// ---------------------------------------------------------------------------

func TestMinimaxSTTBillingSeconds(t *testing.T) {
	require.InDelta(t, 26.325, minimaxSTTBillingSeconds("application/json", []byte(`{"text":"hello","duration":26.325,"trace_id":"t"}`), 100, time.Second), 1e-9)
	sse := "data: {\"index\":0,\"delta\":\"Hello\",\"finish\":false}\n\ndata: {\"index\":1,\"delta\":\"\",\"finish\":true,\"duration\":12.744}\n\n"
	require.InDelta(t, 12.744, minimaxSTTBillingSeconds("text/event-stream", []byte(sse), 100, time.Second), 1e-9)
	floor := minimaxSTTBillingSeconds("application/json", []byte(`{"text":"x"}`), 16000*30, time.Second)
	require.InDelta(t, 30.0, floor, 1e-9, "missing duration falls back to the compressed-speech size heuristic")
}

func TestForwardMiniMaxVoiceSTTReachesUpstream(t *testing.T) {
	upstream := &minimaxActionUpstreamStub{
		responses: []*http.Response{minimaxJSONResponse(http.StatusOK,
			`{"text":"The merchant actually made money.","duration":26.325,"trace_id":"021785229015510a2c883cf675b9804d"}`)},
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	c, recorder := minimaxActionTestContext(http.MethodPost, "/v1/audio/transcriptions", nil)

	var multipartBody strings.Builder
	multipartBody.WriteString("--boundary123\r\nContent-Disposition: form-data; name=\"model\"\r\n\r\nasr-1.0\r\n")
	multipartBody.WriteString("--boundary123\r\nContent-Disposition: form-data; name=\"response_format\"\r\n\r\njson\r\n")
	multipartBody.WriteString("--boundary123\r\nContent-Disposition: form-data; name=\"language\"\r\n\r\nen\r\n")
	multipartBody.WriteString("--boundary123\r\nContent-Disposition: form-data; name=\"file\"; filename=\"speech.mp3\"\r\nContent-Type: audio/mpeg\r\n\r\nFAKEMP3DATA\r\n")
	multipartBody.WriteString("--boundary123--\r\n")
	contentType := "multipart/form-data; boundary=boundary123"

	result, err := svc.ForwardMiniMaxVoice(context.Background(), c, minimaxActionTestAccount(), MiniMaxVoiceEndpointSTT, []byte(multipartBody.String()), contentType)
	require.NoError(t, err)
	require.NotNil(t, result)

	req, reqBody := upstream.requestAt(0)
	require.Equal(t, "https://api.minimax.io/v1/speech_to_text", req.URL.String())
	require.Equal(t, "Bearer minimax-test-key", req.Header.Get("Authorization"))
	require.Equal(t, "en", req.Header.Get("language"), "OpenAI language form field moves to the official language header")
	require.Contains(t, req.Header.Get("Content-Type"), "multipart/form-data")
	require.Contains(t, string(reqBody), `name="model"`, "model passes through as a form field")
	require.Contains(t, string(reqBody), "asr-1.0")
	require.Contains(t, string(reqBody), `name="file"; filename="speech.mp3"`)
	require.Contains(t, string(reqBody), "FAKEMP3DATA")

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "The merchant actually made money.", gjson.GetBytes(recorder.Body.Bytes(), "text").String())
	require.NotNil(t, result.AudioUsage)
	require.Equal(t, "stt", result.AudioUsage.Mode)
	require.InDelta(t, 26.325/3600.0, result.AudioUsage.DurationOrUnits, 1e-12, "billing uses the official billed duration")
}

// ---------------------------------------------------------------------------
// Custom voices (voice_clone chain)
// ---------------------------------------------------------------------------

func minimaxVoiceCloneMultipart(voiceIDField string) ([]byte, string) {
	var b strings.Builder
	if voiceIDField != "" {
		b.WriteString("--vboundary\r\nContent-Disposition: form-data; name=\"voice_id\"\r\n\r\n" + voiceIDField + "\r\n")
	}
	b.WriteString("--vboundary\r\nContent-Disposition: form-data; name=\"file\"; filename=\"sample.mp3\"\r\nContent-Type: audio/mpeg\r\n\r\nVOICESAMPLE\r\n")
	b.WriteString("--vboundary--\r\n")
	return []byte(b.String()), "multipart/form-data; boundary=vboundary"
}

func TestForwardMiniMaxVoiceCloneCreateTwoStep(t *testing.T) {
	upstream := &minimaxActionUpstreamStub{
		responses: []*http.Response{
			minimaxJSONResponse(http.StatusOK, `{"file":{"file_id":123456789,"bytes":5896337,"created_at":1700469398,"filename":"sample.mp3","purpose":"voice_clone"},"base_resp":{"status_code":0,"status_msg":"success"}}`),
			minimaxJSONResponse(http.StatusOK, `{"input_sensitive":false,"demo_audio":"","base_resp":{"status_code":0,"status_msg":"success"}}`),
		},
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	c, recorder := minimaxActionTestContext(http.MethodPost, "/v1/custom-voices", nil)
	body, contentType := minimaxVoiceCloneMultipart("MyClonedVoice01")
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/custom-voices", strings.NewReader(string(body)))
	c.Request.Header.Set("Content-Type", contentType)

	result, err := svc.ForwardMiniMaxVoice(context.Background(), c, minimaxActionTestAccount(), MiniMaxVoiceEndpointCustomVoices, body, contentType)
	require.NoError(t, err)
	require.NotNil(t, result)

	require.Equal(t, 2, upstream.callCount())
	uploadReq, uploadBody := upstream.requestAt(0)
	require.Equal(t, "https://api.minimax.io/v1/files/upload", uploadReq.URL.String())
	require.Contains(t, string(uploadBody), `name="purpose"`, "official upload purpose field")
	require.Contains(t, string(uploadBody), "voice_clone")
	require.Contains(t, string(uploadBody), "VOICESAMPLE")

	cloneReq, cloneBody := upstream.requestAt(1)
	require.Equal(t, "https://api.minimax.io/v1/voice_clone", cloneReq.URL.String())
	require.Equal(t, int64(123456789), gjson.GetBytes(cloneBody, "file_id").Int())
	require.Equal(t, "MyClonedVoice01", gjson.GetBytes(cloneBody, "voice_id").String())

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "MyClonedVoice01", gjson.GetBytes(recorder.Body.Bytes(), "voice_id").String())
	require.Equal(t, int64(123456789), gjson.GetBytes(recorder.Body.Bytes(), "file_id").Int())
}

func TestForwardMiniMaxVoiceCloneCreateGeneratesVoiceID(t *testing.T) {
	upstream := &minimaxActionUpstreamStub{
		responses: []*http.Response{
			minimaxJSONResponse(http.StatusOK, `{"file":{"file_id":42},"base_resp":{"status_code":0}}`),
			minimaxJSONResponse(http.StatusOK, `{"base_resp":{"status_code":0}}`),
		},
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	c, _ := minimaxActionTestContext(http.MethodPost, "/v1/custom-voices", nil)
	body, contentType := minimaxVoiceCloneMultipart("")
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/custom-voices", strings.NewReader(string(body)))
	_, err := svc.ForwardMiniMaxVoice(context.Background(), c, minimaxActionTestAccount(), MiniMaxVoiceEndpointCustomVoices, body, contentType)
	require.NoError(t, err)
	_, cloneBody := upstream.requestAt(1)
	voiceID := gjson.GetBytes(cloneBody, "voice_id").String()
	require.True(t, strings.HasPrefix(voiceID, "mmcv"), "generated voice IDs follow the official [8,256] letter-first rule")
	require.GreaterOrEqual(t, len(voiceID), 8)
}

func TestForwardMiniMaxVoiceCloneUploadBusinessError(t *testing.T) {
	upstream := &minimaxActionUpstreamStub{
		responses: []*http.Response{
			minimaxJSONResponse(http.StatusOK, `{"base_resp":{"status_code":2038,"status_msg":"No cloning permission"}}`),
		},
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	c, recorder := minimaxActionTestContext(http.MethodPost, "/v1/custom-voices", nil)
	body, contentType := minimaxVoiceCloneMultipart("v1")
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/custom-voices", strings.NewReader(string(body)))
	_, err := svc.ForwardMiniMaxVoice(context.Background(), c, minimaxActionTestAccount(), MiniMaxVoiceEndpointCustomVoices, body, contentType)
	require.Error(t, err)
	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.Equal(t, 1, upstream.callCount(), "clone is never attempted when the upload fails")
}

func TestForwardMiniMaxVoiceGetAndDelete(t *testing.T) {
	t.Run("single voice found", func(t *testing.T) {
		upstream := &minimaxActionUpstreamStub{
			responses: []*http.Response{minimaxJSONResponse(http.StatusOK,
				`{"voice_cloning":[{"voice_id":"test12345","description":[],"created_time":"2025-08-20"},{"voice_id":"other","created_time":"2025-08-21"}],"base_resp":{"status_code":0}}`)},
		}
		svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
		c, recorder := minimaxActionTestContext(http.MethodGet, "/v1/custom-voices/test12345", nil)
		c.Request = httptest.NewRequest(http.MethodGet, "/v1/custom-voices/test12345", nil)
		_, err := svc.ForwardMiniMaxVoice(context.Background(), c, minimaxActionTestAccount(), "custom-voices/test12345", nil, "")
		require.NoError(t, err)
		req, reqBody := upstream.requestAt(0)
		require.Equal(t, "https://api.minimax.io/v1/get_voice", req.URL.String())
		require.Equal(t, "voice_cloning", gjson.GetBytes(reqBody, "voice_type").String())
		require.Equal(t, "test12345", gjson.GetBytes(recorder.Body.Bytes(), "voice_id").String())
		require.Equal(t, "2025-08-20", gjson.GetBytes(recorder.Body.Bytes(), "created_time").String())
	})
	t.Run("single voice missing is 404", func(t *testing.T) {
		upstream := &minimaxActionUpstreamStub{
			responses: []*http.Response{minimaxJSONResponse(http.StatusOK, `{"voice_cloning":[],"base_resp":{"status_code":0}}`)},
		}
		svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
		c, recorder := minimaxActionTestContext(http.MethodGet, "/v1/custom-voices/ghost", nil)
		c.Request = httptest.NewRequest(http.MethodGet, "/v1/custom-voices/ghost", nil)
		_, err := svc.ForwardMiniMaxVoice(context.Background(), c, minimaxActionTestAccount(), "custom-voices/ghost", nil, "")
		require.Error(t, err)
		require.Equal(t, http.StatusNotFound, recorder.Code)
	})
	t.Run("list voices", func(t *testing.T) {
		upstream := &minimaxActionUpstreamStub{
			responses: []*http.Response{minimaxJSONResponse(http.StatusOK,
				`{"voice_cloning":[{"voice_id":"a","created_time":"2025-08-20"},{"voice_id":"b","created_time":"2025-08-21"}],"base_resp":{"status_code":0}}`)},
		}
		svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
		c, recorder := minimaxActionTestContext(http.MethodGet, "/v1/custom-voices", nil)
		c.Request = httptest.NewRequest(http.MethodGet, "/v1/custom-voices", nil)
		_, err := svc.ForwardMiniMaxVoice(context.Background(), c, minimaxActionTestAccount(), MiniMaxVoiceEndpointCustomVoices, nil, "")
		require.NoError(t, err)
		require.Equal(t, 2, len(gjson.GetBytes(recorder.Body.Bytes(), "data").Array()))
	})
	t.Run("delete voice", func(t *testing.T) {
		upstream := &minimaxActionUpstreamStub{
			responses: []*http.Response{minimaxJSONResponse(http.StatusOK, `{"base_resp":{"status_code":0,"status_msg":"success"}}`)},
		}
		svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
		c, recorder := minimaxActionTestContext(http.MethodDelete, "/v1/custom-voices/test12345", nil)
		c.Request = httptest.NewRequest(http.MethodDelete, "/v1/custom-voices/test12345", nil)
		_, err := svc.ForwardMiniMaxVoice(context.Background(), c, minimaxActionTestAccount(), "custom-voices/test12345", nil, "")
		require.NoError(t, err)
		req, reqBody := upstream.requestAt(0)
		require.Equal(t, "https://api.minimax.io/v1/delete_voice", req.URL.String())
		require.Equal(t, http.MethodPost, req.Method, "delete_voice is officially a POST")
		require.Equal(t, "test12345", gjson.GetBytes(reqBody, "voice_id").String())
		require.Equal(t, "voice_cloning", gjson.GetBytes(reqBody, "voice_type").String())
		require.True(t, gjson.GetBytes(recorder.Body.Bytes(), "deleted").Bool())
	})
	t.Run("audio subresource is honestly rejected", func(t *testing.T) {
		svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: &minimaxActionUpstreamStub{}}
		c, recorder := minimaxActionTestContext(http.MethodGet, "/v1/custom-voices/v1/audio", nil)
		c.Request = httptest.NewRequest(http.MethodGet, "/v1/custom-voices/v1/audio", nil)
		_, err := svc.ForwardMiniMaxVoice(context.Background(), c, minimaxActionTestAccount(), "custom-voices/v1/audio", nil, "")
		require.Error(t, err)
		require.Equal(t, "action_not_implemented", gjson.GetBytes(recorder.Body.Bytes(), "error.type").String())
	})
}

// ---------------------------------------------------------------------------
// HTTP-level error handling (video V2 / STT OpenAI-style envelopes)
// ---------------------------------------------------------------------------

func TestForwardMiniMaxMediaVideoHTTPError(t *testing.T) {
	t.Run("rate limit fails over", func(t *testing.T) {
		upstream := &minimaxActionUpstreamStub{
			responses: []*http.Response{minimaxJSONResponse(http.StatusTooManyRequests,
				`{"type":"error","error":{"type":"rate_limit_error","message":"rate limit, please retry later (1002)","http_code":"429"},"request_id":"req-9"}`)},
		}
		svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
		c, _ := minimaxActionTestContext(http.MethodPost, "/v1/videos/generations", nil)
		account := minimaxActionTestAccount()
		_, err := svc.ForwardMiniMaxMedia(context.Background(), c, account, MiniMaxMediaEndpointVideosGenerations, "", []byte(`{"model":"MiniMax-H3","prompt":"p","resolution":"768P","duration":5,"aspect_ratio":"16:9"}`), "application/json")
		require.Error(t, err)
		var failoverErr *UpstreamFailoverError
		require.True(t, errors.As(err, &failoverErr))
		require.Equal(t, http.StatusTooManyRequests, failoverErr.StatusCode)
	})
	t.Run("sensitive content is a client error", func(t *testing.T) {
		upstream := &minimaxActionUpstreamStub{
			responses: []*http.Response{minimaxJSONResponse(http.StatusUnprocessableEntity,
				`{"type":"error","error":{"type":"unprocessable_entity_error","message":"video description contains sensitive content (1026)","http_code":"422"},"request_id":"req-10"}`)},
		}
		svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
		c, recorder := minimaxActionTestContext(http.MethodPost, "/v1/videos/generations", nil)
		account := minimaxActionTestAccount()
		_, err := svc.ForwardMiniMaxMedia(context.Background(), c, account, MiniMaxMediaEndpointVideosGenerations, "", []byte(`{"model":"MiniMax-H3","prompt":"p","resolution":"768P","duration":5,"aspect_ratio":"16:9"}`), "application/json")
		require.Error(t, err)
		var failoverErr *UpstreamFailoverError
		require.False(t, errors.As(err, &failoverErr))
		require.Equal(t, http.StatusUnprocessableEntity, recorder.Code)
		require.Contains(t, recorder.Body.String(), "sensitive content")
	})
}
