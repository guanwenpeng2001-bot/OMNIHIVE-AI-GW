package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// antigravityImageUpstream stops an image generateContent with one inline image
// part, the shape a gemini-*-image model returns on the CloudCode surface.
func antigravityImageUpstream(onCall func(*http.Request, *queuedHTTPUpstreamStub)) *queuedHTTPUpstreamStub {
	return &queuedHTTPUpstreamStub{
		responses: []*http.Response{{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body: io.NopCloser(strings.NewReader(
				"data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"inlineData\":{\"mimeType\":\"image/png\",\"data\":\"aGVsbG8=\"}}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":1,\"candidatesTokenCount\":1}}}\n\n",
			)),
		}},
		onCall: onCall,
	}
}

func newAntigravityImageService(upstream HTTPUpstream) *AntigravityGatewayService {
	return &AntigravityGatewayService{
		settingService: NewSettingService(&antigravitySettingRepoStub{}, &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}),
		tokenProvider:  &AntigravityTokenProvider{},
		httpUpstream:   upstream,
	}
}

func antigravityImageAccount(modelMapping map[string]any) *Account {
	return &Account{
		ID:          211,
		Name:        "antigravity-image-actions",
		Platform:    PlatformAntigravity,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token":  "test-access-token",
			"project_id":    "test-project",
			"model_mapping": modelMapping,
		},
	}
}

func newAntigravityImageContext(t *testing.T, path string, body []byte) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	return c, recorder
}

func parseAntigravityImagesForTest(t *testing.T, path, contentType string, body []byte) *AntigravityImagesRequest {
	t.Helper()
	request, err := ParseAntigravityImagesRequest(path, contentType, body)
	require.NoError(t, err)
	return request
}

// Positive: an OpenAI-shaped image request reaches the real CloudCode v1internal
// generateContent shape on the account's own OAuth/project identity, and the
// inline image part is converted back into the standard OpenAI images payload.
// The forward result also carries the generated-image count and size tier, which
// is what the shared trunk bills.
func TestAntigravityForwardImagesServesGenerateThroughV1Internal(t *testing.T) {
	body := []byte(`{"model":"gemini-2.5-flash-image","prompt":"a red fox","n":1,"size":"1024x1024"}`)
	upstream := antigravityImageUpstream(func(req *http.Request, _ *queuedHTTPUpstreamStub) {
		require.Equal(t, "Bearer test-access-token", req.Header.Get("Authorization"))
		require.Equal(t, "application/json", req.Header.Get("Content-Type"))
		require.Contains(t, req.URL.String(), "/v1internal:streamGenerateContent?alt=sse")
	})
	svc := newAntigravityImageService(upstream)
	account := antigravityImageAccount(map[string]any{"gemini-2.5-flash-image": "gemini-2.5-flash-image"})
	c, recorder := newAntigravityImageContext(t, "/v1/images/generations", body)
	request := parseAntigravityImagesForTest(t, "/v1/images/generations", "application/json", body)

	result, err := svc.ForwardImages(context.Background(), c, account, request)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 1, result.ImageCount, "image billing counts the generated image")
	require.Equal(t, ImageBillingSize1K, result.ImageSize, "1024x1024 bills as the 1K tier")
	require.Len(t, upstream.requestBodies, 1)

	var wrapped map[string]any
	require.NoError(t, json.Unmarshal(upstream.requestBodies[0], &wrapped))
	require.Equal(t, "test-project", wrapped["project"])
	require.Equal(t, "agent", wrapped["requestType"])
	require.Equal(t, "gemini-2.5-flash-image", wrapped["model"])
	requestBody, ok := wrapped["request"].(map[string]any)
	require.True(t, ok)
	contents, ok := requestBody["contents"].([]any)
	require.True(t, ok)
	require.Len(t, contents, 1)
	firstContent, ok := contents[0].(map[string]any)
	require.True(t, ok)
	parts, ok := firstContent["parts"].([]any)
	require.True(t, ok)
	firstPart, ok := parts[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "a red fox", firstPart["text"])
	generationConfig, ok := requestBody["generationConfig"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, []any{"TEXT", "IMAGE"}, generationConfig["responseModalities"])
	imageConfig, ok := generationConfig["imageConfig"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "1K", imageConfig["imageSize"])

	var payload map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
	data, ok := payload["data"].([]any)
	require.True(t, ok)
	require.Len(t, data, 1)
	firstImage, ok := data[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "aGVsbG8=", firstImage["b64_json"])
	require.Contains(t, payload, "created")
}

// Positive (multimodal input, canonical shape): an edit request carries the
// source image as an inline image part next to the edit instruction on the same
// verified path.
func TestAntigravityForwardImagesEditCarriesInputImageInlineData(t *testing.T) {
	body := []byte(`{"model":"gemini-2.5-flash-image","prompt":"make it blue","image":"data:image/png;base64,aGVsbG8="}`)
	upstream := antigravityImageUpstream(nil)
	svc := newAntigravityImageService(upstream)
	account := antigravityImageAccount(map[string]any{"gemini-2.5-flash-image": "gemini-2.5-flash-image"})
	c, recorder := newAntigravityImageContext(t, "/v1/images/edits", body)
	request := parseAntigravityImagesForTest(t, "/v1/images/edits", "application/json", body)

	_, err := svc.ForwardImages(context.Background(), c, account, request)
	require.NoError(t, err)
	require.Len(t, upstream.requestBodies, 1)

	parts := gjsonParts(t, upstream.requestBodies[0])
	require.Len(t, parts, 2)
	require.Equal(t, "make it blue", parts[0]["text"])
	inline, ok := parts[1]["inlineData"].(map[string]any)
	require.True(t, ok, "edit must send the source image inline: %v", parts[1])
	require.Equal(t, "image/png", inline["mimeType"])
	require.Equal(t, "aGVsbG8=", inline["data"])
	require.Contains(t, recorder.Body.String(), "b64_json")
}

// Positive (multimodal input, OpenAI JSON shape): the standard
// images[].image_url entry normalizes to the same inline image part, so the
// documented OpenAI edits payload works without a client-side rewrite.
func TestAntigravityParseImagesRequestAcceptsStandardJSONEditShape(t *testing.T) {
	body := []byte(`{"model":"gemini-2.5-flash-image","prompt":"make it blue","images":[{"image_url":"data:image/jpeg;base64,aGVsbG8="}]}`)
	upstream := antigravityImageUpstream(nil)
	svc := newAntigravityImageService(upstream)
	account := antigravityImageAccount(map[string]any{"gemini-2.5-flash-image": "gemini-2.5-flash-image"})
	c, _ := newAntigravityImageContext(t, "/v1/images/edits", body)
	request := parseAntigravityImagesForTest(t, "/v1/images/edits", "application/json", body)
	require.Equal(t, AntigravityImageActionEdit, request.Action)
	require.True(t, request.HasInputImage)

	_, err := svc.ForwardImages(context.Background(), c, account, request)
	require.NoError(t, err)
	require.Len(t, upstream.requestBodies, 1)

	parts := gjsonParts(t, upstream.requestBodies[0])
	require.Len(t, parts, 2)
	inline, ok := parts[1]["inlineData"].(map[string]any)
	require.True(t, ok, "images[].image_url must reach upstream inline: %v", parts[1])
	require.Equal(t, "image/jpeg", inline["mimeType"])
	require.Equal(t, "aGVsbG8=", inline["data"])
}

// Positive (multimodal input, multipart upload): the multipart image file an
// OpenAI SDK sends is base64-encoded into the same inline image part.
func TestAntigravityParseImagesRequestAcceptsMultipartEditUpload(t *testing.T) {
	body, contentType := antigravityMultipartEditBody(t, map[string]string{
		"model":  "gemini-2.5-flash-image",
		"prompt": "make it blue",
		"size":   "2048x2048",
	}, "source.png", "image/png", []byte("hello"))

	request := parseAntigravityImagesForTest(t, "/v1/images/edits", contentType, body)
	require.Equal(t, AntigravityImageActionEdit, request.Action)
	require.True(t, request.Multipart)
	require.Equal(t, ImageBillingSize2K, request.SizeTier)
	require.JSONEq(t,
		`{"model":"gemini-2.5-flash-image","prompt":"make it blue","n":1,"size":"2048x2048","image":"data:image/png;base64,aGVsbG8="}`,
		string(request.CanonicalBody),
		"the canonical body is the single shape both bridge entries accept",
	)
	// The security audit reads prompt and image independent of the transport, so a
	// multipart upload is audited exactly like the JSON shapes.
	require.JSONEq(t,
		`{"prompt":"make it blue","images":[{"image_url":"data:image/png;base64,aGVsbG8="}]}`,
		string(request.ModerationBody),
	)

	upstream := antigravityImageUpstream(nil)
	svc := newAntigravityImageService(upstream)
	account := antigravityImageAccount(map[string]any{"gemini-2.5-flash-image": "gemini-2.5-flash-image"})
	c, _ := newAntigravityImageContext(t, "/v1/images/edits", body)
	c.Request.Header.Set("Content-Type", contentType)

	_, err := svc.ForwardImages(context.Background(), c, account, request)
	require.NoError(t, err)
	require.Len(t, upstream.requestBodies, 1)

	parts := gjsonParts(t, upstream.requestBodies[0])
	require.Len(t, parts, 2)
	inline, ok := parts[1]["inlineData"].(map[string]any)
	require.True(t, ok, "multipart upload must reach upstream inline: %v", parts[1])
	require.Equal(t, "image/png", inline["mimeType"])
	require.Equal(t, "aGVsbG8=", inline["data"])
}

// Exactly-once submission: the same client body converts to a byte-identical
// upstream request every time and each call issues exactly one upstream call, so
// a retried or duplicated request cannot fan out into extra upstream work.
func TestAntigravityForwardImagesSubmitsIdenticalUpstreamBodyPerRequest(t *testing.T) {
	body := []byte(`{"model":"gemini-2.5-flash-image","prompt":"a red fox","size":"1024x1024"}`)
	upstream := &queuedHTTPUpstreamStub{}
	for i := 0; i < 2; i++ {
		upstream.responses = append(upstream.responses, antigravityImageUpstream(nil).responses[0])
	}
	svc := newAntigravityImageService(upstream)
	account := antigravityImageAccount(map[string]any{"gemini-2.5-flash-image": "gemini-2.5-flash-image"})

	for i := 0; i < 2; i++ {
		c, _ := newAntigravityImageContext(t, "/v1/images/generations", body)
		request := parseAntigravityImagesForTest(t, "/v1/images/generations", "application/json", body)
		_, err := svc.ForwardImages(context.Background(), c, account, request)
		require.NoError(t, err)
	}

	require.Len(t, upstream.requestBodies, 2, "one upstream call per forwarded request")
	require.Equal(t,
		gjson.GetBytes(upstream.requestBodies[0], "request").Raw,
		gjson.GetBytes(upstream.requestBodies[1], "request").Raw,
		"conversion must be deterministic (only the per-call request id may differ)",
	)
}

// Cancellation: the upstream call is bound to the client context, so a client
// disconnect cancels the upstream request and never commits an image payload.
func TestAntigravityForwardImagesCancelsUpstreamOnClientDisconnect(t *testing.T) {
	body := []byte(`{"model":"gemini-2.5-flash-image","prompt":"a red fox"}`)
	upstream := &antigravityBlockingUpstreamStub{started: make(chan struct{}, 1)}
	svc := newAntigravityImageService(upstream)
	account := antigravityImageAccount(map[string]any{"gemini-2.5-flash-image": "gemini-2.5-flash-image"})
	c, recorder := newAntigravityImageContext(t, "/v1/images/generations", body)
	request := parseAntigravityImagesForTest(t, "/v1/images/generations", "application/json", body)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, forwardErr := svc.ForwardImages(ctx, c, account, request)
		done <- forwardErr
	}()

	select {
	case <-upstream.started:
	case <-time.After(5 * time.Second):
		t.Fatal("upstream request was never issued")
	}
	cancel()

	select {
	case forwardErr := <-done:
		require.Error(t, forwardErr)
		require.Equal(t, "Upstream request failed after retries", forwardErr.Error(),
			"a cancelled client must fail the forward instead of reporting a generated image")
	case <-time.After(5 * time.Second):
		t.Fatal("forward did not return after the client disconnected")
	}
	require.ErrorIs(t, upstream.requestContext.Err(), context.Canceled,
		"client cancellation must propagate into the upstream request context")
	require.NotContains(t, recorder.Body.String(), "b64_json")
	require.Equal(t, 1, upstream.callCount, "a cancelled request must not be retried upstream")
}

// Negative: a whitelisted text model must be refused before any upstream call.
func TestAntigravityForwardImagesRejectsNonImageModel(t *testing.T) {
	body := []byte(`{"model":"gemini-2.5-flash","prompt":"a red fox"}`)
	upstream := antigravityImageUpstream(nil)
	svc := newAntigravityImageService(upstream)
	account := antigravityImageAccount(map[string]any{"gemini-2.5-flash": "gemini-2.5-flash"})
	c, recorder := newAntigravityImageContext(t, "/v1/images/generations", body)
	request := parseAntigravityImagesForTest(t, "/v1/images/generations", "application/json", body)

	_, err := svc.ForwardImages(context.Background(), c, account, request)
	require.Error(t, err)
	require.Empty(t, upstream.requestBodies)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), "does not produce images")
}

// Negative: an unmapped model must fail the whitelist gate with no upstream call.
func TestAntigravityForwardImagesRejectsUnmappedModel(t *testing.T) {
	body := []byte(`{"model":"gpt-4o","prompt":"a red fox"}`)
	upstream := antigravityImageUpstream(nil)
	svc := newAntigravityImageService(upstream)
	account := antigravityImageAccount(map[string]any{"gemini-2.5-flash-image": "gemini-2.5-flash-image"})
	c, recorder := newAntigravityImageContext(t, "/v1/images/generations", body)
	request := parseAntigravityImagesForTest(t, "/v1/images/generations", "application/json", body)

	_, err := svc.ForwardImages(context.Background(), c, account, request)
	require.Error(t, err)
	require.Empty(t, upstream.requestBodies)
	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.Contains(t, recorder.Body.String(), "not in whitelist")
}

// Negative: an upstream 200 that carries no image part is a protocol failure, not
// an empty success.
func TestAntigravityForwardImagesRejectsUpstreamWithoutImagePart(t *testing.T) {
	body := []byte(`{"model":"gemini-2.5-flash-image","prompt":"a red fox"}`)
	upstream := &queuedHTTPUpstreamStub{
		responses: []*http.Response{{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body: io.NopCloser(strings.NewReader(
				"data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"no image here\"}]},\"finishReason\":\"STOP\"}]}}\n\n",
			)),
		}},
	}
	svc := newAntigravityImageService(upstream)
	account := antigravityImageAccount(map[string]any{"gemini-2.5-flash-image": "gemini-2.5-flash-image"})
	c, recorder := newAntigravityImageContext(t, "/v1/images/generations", body)
	request := parseAntigravityImagesForTest(t, "/v1/images/generations", "application/json", body)

	_, err := svc.ForwardImages(context.Background(), c, account, request)
	require.Error(t, err)
	require.Equal(t, http.StatusBadGateway, recorder.Code)
	require.Contains(t, recorder.Body.String(), "no usable image")
}

// Negative: actions the CloudCode protocol cannot serve are refused with the
// recorded hard-conflict evidence, never forwarded upstream.
func TestAntigravityBuildImageRequestRejectsHardConflictAction(t *testing.T) {
	body := []byte(`{"model":"gemini-2.5-flash-image","prompt":"a red fox"}`)
	_, err := buildAntigravityImageActionRequest("audio.tts", body)
	require.Error(t, err)
	require.Contains(t, err.Error(), "does not implement")
	require.Contains(t, err.Error(), "2026-09-21")
}

func TestAntigravityUnsupportedActionErrorCarriesEvidence(t *testing.T) {
	conflict := AntigravityUnsupportedActionError("video.generate")
	require.Contains(t, conflict.Error(), "video.generate")
	require.Contains(t, conflict.Error(), "2026-09-21")

	unknown := AntigravityUnsupportedActionError("not_a_real_action")
	require.Contains(t, unknown.Error(), "not_a_real_action")
}

// Negative: shapes the CloudCode surface cannot honour are rejected before any
// upstream call instead of being silently reinterpreted.
func TestAntigravityParseImagesRequestRejectsUnsupportedShapes(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		body        string
		wantMessage string
	}{
		{
			name:        "edit without a source image",
			path:        "/v1/images/edits",
			body:        `{"model":"gemini-2.5-flash-image","prompt":"x"}`,
			wantMessage: "requires a source image",
		},
		{
			name:        "edit with a non data URL image",
			path:        "/v1/images/edits",
			body:        `{"model":"gemini-2.5-flash-image","prompt":"x","image":"https://example.com/a.png"}`,
			wantMessage: "base64 image data URL",
		},
		{
			name:        "edit with more than one source image",
			path:        "/v1/images/edits",
			body:        `{"model":"gemini-2.5-flash-image","prompt":"x","images":[{"image_url":"data:image/png;base64,aGVsbG8="},{"image_url":"data:image/png;base64,aGVsbG8="}]}`,
			wantMessage: "single source image",
		},
		{
			name:        "edit with an upstream file id",
			path:        "/v1/images/edits",
			body:        `{"model":"gemini-2.5-flash-image","prompt":"x","images":[{"file_id":"file-1"}]}`,
			wantMessage: "file_id is not supported",
		},
		{
			name:        "edit with a mask",
			path:        "/v1/images/edits",
			body:        `{"model":"gemini-2.5-flash-image","prompt":"x","image":"data:image/png;base64,aGVsbG8=","mask":{"image_url":"data:image/png;base64,aGVsbG8="}}`,
			wantMessage: "does not support a mask",
		},
		{
			name:        "streaming generation",
			path:        "/v1/images/generations",
			body:        `{"model":"gemini-2.5-flash-image","prompt":"x","stream":true}`,
			wantMessage: "do not support streaming",
		},
		{
			name:        "more than one generated image",
			path:        "/v1/images/generations",
			body:        `{"model":"gemini-2.5-flash-image","prompt":"x","n":2}`,
			wantMessage: "supports n<=1",
		},
		{
			name:        "url response format",
			path:        "/v1/images/generations",
			body:        `{"model":"gemini-2.5-flash-image","prompt":"x","response_format":"url"}`,
			wantMessage: "response_format=b64_json",
		},
		{
			name:        "missing model",
			path:        "/v1/images/generations",
			body:        `{"prompt":"x"}`,
			wantMessage: "requires model",
		},
		{
			name:        "missing prompt",
			path:        "/v1/images/generations",
			body:        `{"model":"gemini-2.5-flash-image"}`,
			wantMessage: "requires prompt",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseAntigravityImagesRequest(tt.path, "application/json", []byte(tt.body))
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.wantMessage)
		})
	}

	_, err := ParseAntigravityImagesRequest("/v1/audio/speech", "application/json", []byte(`{"model":"x"}`))
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported images endpoint")
}

// The size override maps to the verified imageSize tier and defaults to 2K.
func TestAntigravityBuildImageRequestMapsSizeTier(t *testing.T) {
	request, err := buildAntigravityImageActionRequest(AntigravityImageActionGenerate, []byte(`{"model":"gemini-2.5-flash-image","prompt":"x","size":"2048x2048"}`))
	require.NoError(t, err)
	require.Equal(t, ImageBillingSize2K, request.SizeTier)
	require.Contains(t, string(request.NativeBody), `"imageSize":"2K"`)

	request, err = buildAntigravityImageActionRequest(AntigravityImageActionGenerate, []byte(`{"model":"gemini-2.5-flash-image","prompt":"x"}`))
	require.NoError(t, err)
	require.Equal(t, ImageBillingSize2K, request.SizeTier)
	require.NotContains(t, string(request.NativeBody), "imageConfig")
}

func gjsonParts(t *testing.T, wrapped []byte) []map[string]any {
	t.Helper()
	var parsed map[string]any
	require.NoError(t, json.Unmarshal(wrapped, &parsed))
	request, ok := parsed["request"].(map[string]any)
	require.True(t, ok)
	contents, ok := request["contents"].([]any)
	require.True(t, ok)
	require.Len(t, contents, 1)
	content, ok := contents[0].(map[string]any)
	require.True(t, ok)
	rawParts, ok := content["parts"].([]any)
	require.True(t, ok)
	parts := make([]map[string]any, 0, len(rawParts))
	for _, rawPart := range rawParts {
		part, ok := rawPart.(map[string]any)
		require.True(t, ok)
		parts = append(parts, part)
	}
	return parts
}

func antigravityMultipartEditBody(t *testing.T, fields map[string]string, fileName, contentType string, data []byte) ([]byte, string) {
	t.Helper()
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	for name, value := range fields {
		require.NoError(t, writer.WriteField(name, value))
	}
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="image"; filename="%s"`, fileName))
	header.Set("Content-Type", contentType)
	part, err := writer.CreatePart(header)
	require.NoError(t, err)
	_, err = part.Write(data)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	return buffer.Bytes(), writer.FormDataContentType()
}

// antigravityBlockingUpstreamStub holds the upstream call open until the request
// context is cancelled, which is what a client disconnect looks like upstream.
type antigravityBlockingUpstreamStub struct {
	queuedHTTPUpstreamStub
	started        chan struct{}
	requestContext context.Context
}

func (s *antigravityBlockingUpstreamStub) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	s.callCount++
	s.requestContext = req.Context()
	s.started <- struct{}{}
	<-req.Context().Done()
	return nil, req.Context().Err()
}
