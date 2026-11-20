package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/stretchr/testify/require"
)

// These are synthetic REST contract fixtures derived from first-party API
// reference examples, not captured authenticated production traffic.
// Source: https://ai.google.dev/api/embeddings, accessed 2026-09-21.
func TestGeminiNativeEmbeddingWire(t *testing.T) {
	for _, action := range []string{"embedContent", "batchEmbedContents"} {
		t.Run(action, func(t *testing.T) {
			body := `{"model":"models/public-embedding","content":{"parts":[{"text":"hello"}]},"outputDimensionality":2}`
			response := `{"embedding":{"values":[0.1,0.2]}}`
			if action == "batchEmbedContents" {
				body = `{"requests":[` + body + `]}`
				response = `{"embeddings":[{"values":[0.1,0.2]}]}`
			}
			upstream := &geminiCompatHTTPUpstreamStub{response: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(response))}}
			gateway := &GeminiMessagesCompatService{httpUpstream: upstream, cfg: &config.Config{}}
			account := &Account{ID: 1, Platform: PlatformGemini, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture-key", "model_mapping": map[string]any{"public-embedding": "gemini-embedding-001"}}}
			recorder := httptest.NewRecorder()
			requestContext, _ := gin.CreateTestContext(recorder)
			requestContext.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/public-embedding:"+action, strings.NewReader(body))
			result, err := gateway.ForwardNative(context.Background(), requestContext, account, "public-embedding", action, false, []byte(body))
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, "https://generativelanguage.googleapis.com/v1beta/models/gemini-embedding-001:"+action, upstream.lastReq.URL.String())
			require.Equal(t, "fixture-key", upstream.lastReq.Header.Get("x-goog-api-key"))
			wireBody, err := io.ReadAll(upstream.lastReq.Body)
			require.NoError(t, err)
			require.Contains(t, string(wireBody), `"model":"models/gemini-embedding-001"`)
			require.Contains(t, string(wireBody), `"outputDimensionality":2`)
			require.JSONEq(t, response, recorder.Body.String())
		})
	}
}

func TestGeminiEmbeddingRejectsMixedModels(t *testing.T) {
	for _, body := range []string{`{"requests":[{"model":"models/other"}]}`, `{"requests":[null]}`, `{"requests":[]}`, `null`, `bad json`} {
		_, err := prepareGeminiEmbeddingBody([]byte(body), "batchEmbedContents", "public", "upstream")
		require.Error(t, err)
	}
}

func TestGeminiBatchRESTOperationLifecycle(t *testing.T) {
	for _, fixture := range []struct{ body, state, output string }{
		{`{"name":"batches/test","metadata":{"state":"BATCH_STATE_RUNNING"},"done":false}`, "JOB_STATE_RUNNING", ""},
		{`{"name":"batches/test","metadata":{"state":"BATCH_STATE_SUCCEEDED"},"done":true,"response":{"responsesFile":"files/result"}}`, "JOB_STATE_SUCCEEDED", "files/result"},
		{`{"name":"batches/test","done":true,"error":{"code":1,"message":"cancelled"}}`, "JOB_STATE_CANCELLED", ""},
		{`{"name":"batches/test","done":true,"error":{"code":13,"message":"internal"}}`, "JOB_STATE_FAILED", ""},
	} {
		t.Run(fixture.state, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				require.Equal(t, "/v1beta/batches/test", request.URL.Path)
				require.Equal(t, "fixture-key", request.Header.Get("x-goog-api-key"))
				_, _ = io.WriteString(writer, fixture.body)
			}))
			defer server.Close()
			client := NewGeminiBatchHTTPClient(server.URL, server.Client())
			job, err := client.GetBatch(context.Background(), "fixture-key", "batches/test")
			require.NoError(t, err)
			require.Equal(t, fixture.state, job.State)
			require.Equal(t, fixture.output, geminiBatchOutputRef(job))
		})
	}
}

// Image generation/editing bridge evidence:
// https://ai.google.dev/gemini-api/docs/image-generation (responseModalities
// TEXT+IMAGE, inlineData image parts), accessed 2026-09-21. The upstream body
// here is a synthetic fixture shaped after the documented REST response.
func TestGeminiForwardActionImageGenerateWire(t *testing.T) {
	pixel := base64.StdEncoding.EncodeToString([]byte{0x89, 0x50, 0x4E, 0x47})
	upstreamBody := `{"candidates":[{"content":{"role":"model","parts":[{"inlineData":{"mimeType":"image/png","data":"` + pixel + `"}}]}}],"usageMetadata":{"promptTokenCount":7,"totalTokenCount":7}}`
	upstream := &geminiCompatHTTPUpstreamStub{response: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(upstreamBody))}}
	gateway := &GeminiMessagesCompatService{httpUpstream: upstream, cfg: &config.Config{}}
	account := &Account{ID: 2, Platform: PlatformGemini, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture-key", "model_mapping": map[string]any{"public-image": "gemini-image-fixture"}}}
	recorder := httptest.NewRecorder()
	requestContext, _ := gin.CreateTestContext(recorder)
	requestContext.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(""))
	body := `{"model":"public-image","prompt":"draw a cat","n":1}`
	result, err := gateway.ForwardAction(context.Background(), requestContext, account, "image.generate", []byte(body))
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 1, upstream.calls)
	require.Equal(t, "https://generativelanguage.googleapis.com/v1beta/models/gemini-image-fixture:generateContent", upstream.lastReq.URL.String())
	require.Equal(t, "fixture-key", upstream.lastReq.Header.Get("x-goog-api-key"))
	wireBody, err := io.ReadAll(upstream.lastReq.Body)
	require.NoError(t, err)
	require.Contains(t, string(wireBody), `"text":"draw a cat"`)
	require.Contains(t, string(wireBody), `"responseModalities":["TEXT","IMAGE"]`)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Header().Get("Content-Type"), "application/json")
	var payload struct {
		Created int64 `json:"created"`
		Data    []struct {
			B64JSON string `json:"b64_json"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
	require.Len(t, payload.Data, 1)
	require.Equal(t, pixel, payload.Data[0].B64JSON)
}

func TestGeminiForwardActionImageEditWire(t *testing.T) {
	source := base64.StdEncoding.EncodeToString([]byte{0x01, 0x02, 0x03})
	pixel := base64.StdEncoding.EncodeToString([]byte{0x89, 0x50, 0x4E, 0x47})
	upstreamBody := `{"candidates":[{"content":{"role":"model","parts":[{"inlineData":{"mimeType":"image/png","data":"` + pixel + `"}}]}}]}`
	upstream := &geminiCompatHTTPUpstreamStub{response: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(upstreamBody))}}
	gateway := &GeminiMessagesCompatService{httpUpstream: upstream, cfg: &config.Config{}}
	account := &Account{ID: 2, Platform: PlatformGemini, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture-key", "model_mapping": map[string]any{"public-image": "gemini-image-fixture"}}}
	recorder := httptest.NewRecorder()
	requestContext, _ := gin.CreateTestContext(recorder)
	requestContext.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", strings.NewReader(""))
	body := `{"model":"public-image","prompt":"make it blue","image":"data:image/png;base64,` + source + `"}`
	result, err := gateway.ForwardAction(context.Background(), requestContext, account, "image.edit", []byte(body))
	require.NoError(t, err)
	require.NotNil(t, result)
	wireBody, err := io.ReadAll(upstream.lastReq.Body)
	require.NoError(t, err)
	require.Contains(t, string(wireBody), `"text":"make it blue"`)
	require.Contains(t, string(wireBody), `"mimeType":"image/png"`)
	require.Contains(t, string(wireBody), `"data":"`+source+`"`)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), pixel)
}

// Speech bridge evidence: https://ai.google.dev/gemini-api/docs/speech-generation,
// accessed 2026-09-21; the returned inlineData MIME type is observed on the wire
// as "audio/L16;codec=pcm;rate=24000" (also "audio/l16; rate=24000; channels=1"),
// i.e. raw PCM that must be wrapped into WAV for clients expecting audio/wav.
func TestGeminiForwardActionSpeechWire(t *testing.T) {
	pcm := []byte{0x01, 0x02, 0x03, 0x04}
	for _, fixture := range []struct {
		name            string
		mimeType        string
		requestFormat   string
		expectedType    string
		expectedPayload func() []byte
	}{
		{name: "documented mime wraps wav", mimeType: "audio/L16;codec=pcm;rate=24000", expectedType: "audio/wav"},
		{name: "spaced lowercase mime wraps wav", mimeType: "audio/l16; rate=24000; channels=1", expectedType: "audio/wav"},
		{name: "pcm passthrough", mimeType: "audio/L16;codec=pcm;rate=24000", requestFormat: "pcm", expectedType: "audio/pcm"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			upstreamBody := `{"candidates":[{"content":{"role":"model","parts":[{"inlineData":{"mimeType":"` + fixture.mimeType + `","data":"` + base64.StdEncoding.EncodeToString(pcm) + `"}}]}}]}`
			upstream := &geminiCompatHTTPUpstreamStub{response: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(upstreamBody))}}
			gateway := &GeminiMessagesCompatService{httpUpstream: upstream, cfg: &config.Config{}}
			account := &Account{ID: 3, Platform: PlatformGemini, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture-key", "model_mapping": map[string]any{"public-tts": "gemini-tts-fixture"}}}
			recorder := httptest.NewRecorder()
			requestContext, _ := gin.CreateTestContext(recorder)
			requestContext.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/speech", strings.NewReader(""))
			body := `{"model":"public-tts","input":"hello world","voice":"Kore"}`
			if fixture.requestFormat != "" {
				body = `{"model":"public-tts","input":"hello world","voice":"Kore","response_format":"` + fixture.requestFormat + `"}`
			}
			result, err := gateway.ForwardAction(context.Background(), requestContext, account, "audio.tts", []byte(body))
			require.NoError(t, err)
			require.NotNil(t, result)
			wireBody, err := io.ReadAll(upstream.lastReq.Body)
			require.NoError(t, err)
			require.Contains(t, string(wireBody), `"text":"hello world"`)
			require.Contains(t, string(wireBody), `"responseModalities":["AUDIO"]`)
			require.Contains(t, string(wireBody), `"voiceName":"Kore"`)
			require.Equal(t, http.StatusOK, recorder.Code)
			require.Contains(t, recorder.Header().Get("Content-Type"), fixture.expectedType)
			payload := recorder.Body.Bytes()
			if fixture.expectedType == "audio/pcm" {
				require.Equal(t, pcm, payload)
				return
			}
			require.True(t, bytes.HasPrefix(payload, []byte("RIFF")), "wav must start with RIFF")
			require.Equal(t, "WAVE", string(payload[8:12]))
			require.Equal(t, uint32(36+len(pcm)), binary.LittleEndian.Uint32(payload[4:8]))
			require.Equal(t, pcm, payload[44:])
		})
	}
}

func TestGeminiActionRequestValidation(t *testing.T) {
	for _, fixture := range []struct{ name, action, body string }{
		{"unknown action has no bridge", "video.generate", `{"model":"m","prompt":"x"}`},
		{"missing model", "image.generate", `{"prompt":"x"}`},
		{"malformed json", "image.generate", `not json`},
		{"missing prompt", "image.generate", `{"model":"m"}`},
		{"more than one image", "image.generate", `{"model":"m","prompt":"x","n":2}`},
		{"pixel size override", "image.generate", `{"model":"m","prompt":"x","size":"1024x1024"}`},
		{"url response format", "image.generate", `{"model":"m","prompt":"x","response_format":"url"}`},
		{"edit requires data url", "image.edit", `{"model":"m","prompt":"x","image":"https://example.com/a.png"}`},
		{"edit rejects invalid base64", "image.edit", `{"model":"m","prompt":"x","image":"data:image/png;base64,!!!"}`},
		{"speech missing input", "audio.tts", `{"model":"m","voice":"Kore"}`},
		{"speech missing voice", "audio.tts", `{"model":"m","input":"hello"}`},
		{"speech rejects speed shift", "audio.tts", `{"model":"m","input":"hello","voice":"Kore","speed":2}`},
		{"speech rejects compressed format", "audio.tts", `{"model":"m","input":"hello","voice":"Kore","response_format":"mp3"}`},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			_, _, _, err := buildGeminiActionRequest(fixture.action, []byte(fixture.body))
			require.Error(t, err)
		})
	}
}

func TestGeminiForwardActionUpstreamErrorPassthrough(t *testing.T) {
	upstreamBody := `{"error":{"code":400,"message":"Invalid prompt","status":"INVALID_ARGUMENT"}}`
	upstream := &geminiCompatHTTPUpstreamStub{response: &http.Response{StatusCode: 400, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(upstreamBody))}}
	gateway := &GeminiMessagesCompatService{httpUpstream: upstream, cfg: &config.Config{}}
	account := &Account{ID: 4, Platform: PlatformGemini, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture-key"}}
	recorder := httptest.NewRecorder()
	requestContext, _ := gin.CreateTestContext(recorder)
	requestContext.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(""))
	result, err := gateway.ForwardAction(context.Background(), requestContext, account, "image.generate", []byte(`{"model":"m","prompt":"x"}`))
	require.Error(t, err)
	require.Nil(t, result)
	require.Equal(t, 400, recorder.Code)
	require.Contains(t, recorder.Body.String(), "Invalid prompt")
}

func TestGeminiForwardActionRequiresMedia(t *testing.T) {
	upstreamBody := `{"candidates":[{"content":{"role":"model","parts":[{"text":"no media here"}]}}]}`
	upstream := &geminiCompatHTTPUpstreamStub{response: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(upstreamBody))}}
	gateway := &GeminiMessagesCompatService{httpUpstream: upstream, cfg: &config.Config{}}
	account := &Account{ID: 5, Platform: PlatformGemini, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture-key"}}
	recorder := httptest.NewRecorder()
	requestContext, _ := gin.CreateTestContext(recorder)
	requestContext.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(""))
	result, err := gateway.ForwardAction(context.Background(), requestContext, account, "image.generate", []byte(`{"model":"m","prompt":"x"}`))
	require.Error(t, err)
	require.NotNil(t, result, "the completed upstream call keeps its usage record")
	require.Equal(t, http.StatusBadGateway, recorder.Code)
	require.Contains(t, recorder.Body.String(), "upstream_error")
}

// Vertex (service account) embeddings are served by :predict with an
// instances/predictions shape, not by :embedContent, so the native embedding
// path deliberately accepts only non-streaming API key requests instead of
// silently dropping the SA project/region or proxying an unfaithful shape.
// Evidence: https://cloud.google.com/vertex-ai/generative-ai/docs/model-reference/text-embeddings-api
// (POST .../publishers/google/models/gemini-embedding-001:predict), accessed 2026-09-21.
func TestGeminiEmbedContentRejectsStreamAndNonAPIKey(t *testing.T) {
	for _, fixture := range []struct {
		name        string
		accountType string
		stream      bool
	}{
		{"stream rejected", AccountTypeAPIKey, true},
		{"service account rejected", AccountTypeServiceAccount, false},
		{"oauth rejected", AccountTypeOAuth, false},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			upstream := &geminiCompatHTTPUpstreamStub{}
			gateway := &GeminiMessagesCompatService{httpUpstream: upstream, cfg: &config.Config{}}
			account := &Account{ID: 6, Platform: PlatformGemini, Type: fixture.accountType, Credentials: map[string]any{"api_key": "fixture-key"}}
			recorder := httptest.NewRecorder()
			requestContext, _ := gin.CreateTestContext(recorder)
			requestContext.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/public-embedding:embedContent", strings.NewReader(`{"content":{"parts":[{"text":"hello"}]}}`))
			result, err := gateway.ForwardNative(context.Background(), requestContext, account, "public-embedding", "embedContent", fixture.stream, []byte(`{"content":{"parts":[{"text":"hello"}]}}`))
			require.Error(t, err)
			require.Nil(t, result)
			require.Equal(t, http.StatusBadRequest, recorder.Code)
			require.Contains(t, recorder.Body.String(), "Native embeddings require")
			require.Zero(t, upstream.calls, "rejection must precede the transport call")
		})
	}
}

func TestGeminiEmbeddingRejectsSingleModelMismatch(t *testing.T) {
	for _, body := range []string{`{"model":"models/other","content":{"parts":[{"text":"x"}]}}`, `null`, `bad json`} {
		_, err := prepareGeminiEmbeddingBody([]byte(body), "embedContent", "public", "upstream")
		require.Error(t, err)
	}
	kept, err := prepareGeminiEmbeddingBody([]byte(`{"model":"models/public","content":{"parts":[{"text":"x"}]},"taskType":"RETRIEVAL_QUERY"}`), "embedContent", "public", "upstream")
	require.NoError(t, err)
	require.Contains(t, string(kept), `"model":"models/upstream"`)
	require.Contains(t, string(kept), `"taskType":"RETRIEVAL_QUERY"`)
}

// Model listing pagination evidence: https://ai.google.dev/api/models#method:-models.list
// (pageSize/pageToken query, alt=json), accessed 2026-09-21.
func TestGeminiAIStudioGETPaginationAndAlt(t *testing.T) {
	upstream := &geminiCompatHTTPUpstreamStub{response: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"models":[],"nextPageToken":"tok-2"}`))}}
	gateway := &GeminiMessagesCompatService{httpUpstream: upstream, cfg: &config.Config{}}
	account := &Account{ID: 7, Platform: PlatformGemini, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture-key"}}
	result, err := gateway.ForwardAIStudioGET(context.Background(), account, "/v1beta/models?pageSize=2&pageToken=tok-1&alt=json")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, http.MethodGet, upstream.lastReq.Method)
	require.Equal(t, "https://generativelanguage.googleapis.com/v1beta/models?alt=json&pageSize=2&pageToken=tok-1", upstream.lastReq.URL.String())
	require.Equal(t, "fixture-key", upstream.lastReq.Header.Get("x-goog-api-key"))
	require.JSONEq(t, `{"models":[],"nextPageToken":"tok-2"}`, string(result.Body))
}

func TestGeminiAIStudioGETPreservesOpaquePagination(testingT *testing.T) {
	body := `{"models":[{"name":"models/gemini-fixture","description":"full model metadata","future_capability":{"supported":true}}],"nextPageToken":"next+/= token","future_envelope":true}`
	upstream := &geminiCompatHTTPUpstreamStub{response: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}}
	gateway := &GeminiMessagesCompatService{httpUpstream: upstream, cfg: &config.Config{}}
	account := &Account{ID: 7, Platform: PlatformGemini, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture-key"}}
	result, err := gateway.ForwardAIStudioGET(context.Background(), account, "/v1beta/models?pageSize=17&pageToken=opaque%2B%2F%3D%20token")
	require.NoError(testingT, err)
	require.Equal(testingT, "17", upstream.lastReq.URL.Query().Get("pageSize"))
	require.Equal(testingT, "opaque+/= token", upstream.lastReq.URL.Query().Get("pageToken"))
	require.Equal(testingT, body, string(result.Body))
}

func TestGeminiAIStudioGETRejectsUnsafeTargets(t *testing.T) {
	gateway := &GeminiMessagesCompatService{httpUpstream: &geminiCompatHTTPUpstreamStub{}, cfg: &config.Config{}}
	account := &Account{ID: 7, Platform: PlatformGemini, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture-key"}}
	for _, path := range []string{
		"https://generativelanguage.googleapis.com/v1beta/models",
		"//evil.example/v1beta/models",
		"/v1beta/models#fragment",
		"/v1beta/models?alt=proto",
		"/v1beta/models?key=fixture-key",
		"/v1beta/models?pageSize=1&pageSize=2",
		"/v1beta/models?alt=json&alt=json",
	} {
		t.Run(path, func(t *testing.T) {
			_, err := gateway.ForwardAIStudioGET(context.Background(), account, path)
			require.Error(t, err)
		})
	}
}

func TestGeminiForwardNativeHonorsCancelledContext(t *testing.T) {
	upstream := &geminiCompatHTTPUpstreamStub{}
	gateway := &GeminiMessagesCompatService{httpUpstream: upstream, cfg: &config.Config{}}
	account := &Account{ID: 8, Platform: PlatformGemini, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture-key"}}
	recorder := httptest.NewRecorder()
	requestContext, _ := gin.CreateTestContext(recorder)
	requestContext.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/public:generateContent", strings.NewReader(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := gateway.ForwardNative(ctx, requestContext, account, "public", "generateContent", false, []byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`))
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, result)
	require.Zero(t, upstream.calls, "a cancelled request must not reach the transport")
	require.Equal(t, http.StatusOK, recorder.Code, "cancellation must not commit a synthetic client response")
}
