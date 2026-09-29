//go:build unit

package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Fixtures are synthetic examples of the official audio/embeddings schema
// (mistralai/platform-docs-public openapi.yaml, retrieved 2026-09-21), not live
// account receipts. These tests perform no network, database, Redis or billable
// upstream actions.
func TestMistralActionsContract(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newActionContext := func(method, target string, body []byte) (*gin.Context, *httptest.ResponseRecorder) {
		recorder := httptest.NewRecorder()
		ginContext, _ := gin.CreateTestContext(recorder)
		ginContext.Request = httptest.NewRequest(method, target, bytes.NewReader(body))
		return ginContext, recorder
	}

	t.Run("embeddings reaches native endpoint with usage and passthrough", func(t *testing.T) {
		fixture, err := os.ReadFile("testdata/mistral_embed_response.json")
		require.NoError(t, err)
		body := []byte(`{"model":"mistral-embed","input":["Embed this sentence.","As well as this one."]}`)
		upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}, "X-Request-Id": {"req-embed"}}, Body: io.NopCloser(bytes.NewReader(fixture))}}
		service := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
		ginContext, recorder := newActionContext(http.MethodPost, "/v1/embeddings", body)
		result, err := service.ForwardMistralEmbeddings(context.Background(), ginContext, mistralContractAccount(), body, "")
		require.NoError(t, err)
		require.Equal(t, "https://api.mistral.ai/v1/embeddings", upstream.lastReq.URL.String())
		require.Equal(t, "Bearer sk-test", upstream.lastReq.Header.Get("Authorization"))
		require.JSONEq(t, string(body), string(upstream.lastBody))
		require.Equal(t, 15, result.Usage.InputTokens)
		require.Zero(t, result.Usage.OutputTokens)
		require.Equal(t, "mistral-embed", result.UpstreamModel)
		require.Equal(t, "mistral-embed", result.BillingModel)
		require.Equal(t, "req-embed", result.RequestID)
		require.JSONEq(t, string(fixture), recorder.Body.String())
		require.Len(t, upstream.requests, 1, "one forward is exactly one upstream call")
	})

	t.Run("embeddings bridges dimensions and honors mapping and relay base", func(t *testing.T) {
		fixture, err := os.ReadFile("testdata/mistral_embed_response.json")
		require.NoError(t, err)
		account := mistralContractAccount()
		account.Credentials["base_url"] = "https://relay.example/mistral/v1"
		account.Credentials["model_mapping"] = map[string]any{"embed-public": "mistral-embed"}
		for _, withNativeDimension := range []bool{false, true} {
			body := []byte(`{"model":"embed-public","input":"hello","dimensions":3072}`)
			if withNativeDimension {
				body = []byte(`{"model":"embed-public","input":"hello","dimensions":1024,"output_dimension":3072}`)
			}
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(bytes.NewReader(fixture))}}
			service := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
			ginContext, _ := newActionContext(http.MethodPost, "/v1/embeddings", body)
			result, err := service.ForwardMistralEmbeddings(context.Background(), ginContext, account, body, "")
			require.NoError(t, err)
			require.Equal(t, "https://relay.example/mistral/v1/embeddings", upstream.lastReq.URL.String())
			require.Equal(t, "mistral-embed", gjson.GetBytes(upstream.lastBody, "model").String())
			require.EqualValues(t, 3072, gjson.GetBytes(upstream.lastBody, "output_dimension").Int())
			require.False(t, gjson.GetBytes(upstream.lastBody, "dimensions").Exists(), "OpenAI-only field must not reach the native schema")
			require.Equal(t, "mistral-embed", result.BillingModel)
		}
	})

	t.Run("embeddings rejects invalid requests before upstream", func(t *testing.T) {
		upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{}`))}}
		service := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
		for _, body := range []string{
			`{"input":"hello"}`,
			`{"model":"mistral-embed"}`,
			`{"model":"mistral-embed","input":null}`,
			`{"model":"mistral-embed","input":{"text":"object"}}`,
			`{"model":"mistral-embed","input":"hello","dimensions":-3}`,
			`not-json`,
		} {
			ginContext, _ := newActionContext(http.MethodPost, "/v1/embeddings", []byte(body))
			_, err := service.ForwardMistralEmbeddings(context.Background(), ginContext, mistralContractAccount(), []byte(body), "")
			require.Error(t, err, body)
		}
		require.Nil(t, upstream.lastReq, "invalid requests must never reach upstream")
		otherPlatform := mistralContractAccount()
		otherPlatform.Platform = PlatformOpenAI
		ginContext, _ := newActionContext(http.MethodPost, "/v1/embeddings", []byte(`{"model":"mistral-embed","input":"x"}`))
		_, err := service.ForwardMistralEmbeddings(context.Background(), ginContext, otherPlatform, []byte(`{"model":"mistral-embed","input":"x"}`), "")
		require.Error(t, err)
	})

	t.Run("embeddings never settles unknown usage as zero", func(t *testing.T) {
		for _, scenario := range []string{
			`{"data":[],"model":"mistral-embed"}`,
			`{"data":[],"model":"mistral-embed","usage":null}`,
			`{"data":[],"model":"mistral-embed","usage":{"prompt_tokens":3}}`,
			`{"data":[],"model":"mistral-embed","usage":{"prompt_tokens":-1,"completion_tokens":0}}`,
			`{"data":[],"model":"mistral-embed","usage":{"prompt_tokens":1.5,"completion_tokens":0}}`,
		} {
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(scenario))}}
			service := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
			body := []byte(`{"model":"mistral-embed","input":"hello"}`)
			ginContext, _ := newActionContext(http.MethodPost, "/v1/embeddings", body)
			result, err := service.ForwardMistralEmbeddings(context.Background(), ginContext, mistralContractAccount(), body, "")
			require.ErrorContains(t, err, "unknown", scenario)
			require.Nil(t, result, scenario)
		}
	})

	t.Run("embeddings error statuses fail without retry and preserve classification", func(t *testing.T) {
		for _, status := range []int{401, 403, 404, 429, 500} {
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"message":"synthetic failure"}`))}}
			service := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
			body := []byte(`{"model":"mistral-embed","input":"hello"}`)
			ginContext, recorder := newActionContext(http.MethodPost, "/v1/embeddings", body)
			_, err := service.ForwardMistralEmbeddings(context.Background(), ginContext, mistralContractAccount(), body, "")
			require.Error(t, err, status)
			require.Len(t, upstream.requests, 1, "no blind retry inside the adapter")
			var failoverErr *UpstreamFailoverError
			if errors.As(err, &failoverErr) {
				require.Equal(t, status, failoverErr.StatusCode, "failover keeps the upstream status for the account policy loop")
			} else {
				require.Contains(t, recorder.Body.String(), `"error"`, "non-failover errors reach the client in the OpenAI error shape")
			}
		}
	})

	t.Run("embeddings pre-canceled request is never sent", func(t *testing.T) {
		upstream := &httpUpstreamRecorder{err: context.Canceled}
		service := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		body := []byte(`{"model":"mistral-embed","input":"hello"}`)
		ginContext, _ := newActionContext(http.MethodPost, "/v1/embeddings", body)
		ginContext.Request = ginContext.Request.WithContext(ctx)
		_, err := service.ForwardMistralEmbeddings(ctx, ginContext, mistralContractAccount(), body, "")
		require.Error(t, err)
		require.Nil(t, upstream.lastReq, "an already canceled request must not be sent")
	})

	transcriptionMultipart := func(t *testing.T, model string, stream bool) ([]byte, string) {
		var buffer bytes.Buffer
		writer := multipart.NewWriter(&buffer)
		require.NoError(t, writer.WriteField("model", model))
		if stream {
			require.NoError(t, writer.WriteField("stream", "true"))
		}
		file, err := writer.CreateFormFile("file", "speech.mp3")
		require.NoError(t, err)
		_, err = file.Write([]byte("synthetic-audio-bytes"))
		require.NoError(t, err)
		require.NoError(t, writer.Close())
		return buffer.Bytes(), writer.FormDataContentType()
	}

	t.Run("transcription forwards multipart verbatim and bills audio seconds", func(t *testing.T) {
		fixture, err := os.ReadFile("testdata/mistral_transcription_response.json")
		require.NoError(t, err)
		body, contentType := transcriptionMultipart(t, "voxtral-mini-2507", false)
		upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}, "X-Request-Id": {"req-stt"}}, Body: io.NopCloser(bytes.NewReader(fixture))}}
		service := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
		ginContext, recorder := newActionContext(http.MethodPost, "/v1/audio/transcriptions", body)
		result, err := service.ForwardMistralAudioTranscription(context.Background(), ginContext, mistralContractAccount(), body, contentType, "")
		require.NoError(t, err)
		require.Equal(t, "https://api.mistral.ai/v1/audio/transcriptions", upstream.lastReq.URL.String())
		require.Equal(t, contentType, upstream.lastReq.Header.Get("Content-Type"), "multipart envelope must pass through untouched")
		require.Equal(t, body, upstream.lastBody, "no mapping means byte-identical forward")
		require.NotNil(t, result.AudioUsage)
		require.Equal(t, "stt", result.AudioUsage.Mode)
		require.InDelta(t, 203.0/3600.0, result.AudioUsage.DurationOrUnits, 1e-12)
		require.Equal(t, 4, result.Usage.InputTokens)
		require.Equal(t, 635, result.Usage.OutputTokens)
		require.Equal(t, "req-stt", result.RequestID)
		require.JSONEq(t, string(fixture), recorder.Body.String())
	})

	t.Run("transcription rewrites only the model field when mapped", func(t *testing.T) {
		fixture, err := os.ReadFile("testdata/mistral_transcription_response.json")
		require.NoError(t, err)
		account := mistralContractAccount()
		account.Credentials["model_mapping"] = map[string]any{"stt-public": "voxtral-mini-2507"}
		body, contentType := transcriptionMultipart(t, "stt-public", false)
		upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(bytes.NewReader(fixture))}}
		service := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
		ginContext, _ := newActionContext(http.MethodPost, "/v1/audio/transcriptions", body)
		result, err := service.ForwardMistralAudioTranscription(context.Background(), ginContext, account, body, contentType, "")
		require.NoError(t, err)
		require.Equal(t, "voxtral-mini-2507", result.UpstreamModel)
		require.Equal(t, "stt-public", result.Model)
		require.Contains(t, upstream.lastReq.Header.Get("Content-Type"), "multipart/form-data; boundary=")
		rewrittenModel, err := mistralMultipartFieldValue(upstream.lastBody, upstream.lastReq.Header.Get("Content-Type"), "model")
		require.NoError(t, err)
		require.Equal(t, "voxtral-mini-2507", rewrittenModel)
		require.Contains(t, string(upstream.lastBody), "synthetic-audio-bytes", "file bytes must survive re-encoding")
	})

	t.Run("transcription streams events and settles on done usage", func(t *testing.T) {
		fixture, err := os.ReadFile("testdata/mistral_transcription.sse")
		require.NoError(t, err)
		body, contentType := transcriptionMultipart(t, "voxtral-mini-2507", true)
		upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(bytes.NewReader(fixture))}}
		service := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
		ginContext, recorder := newActionContext(http.MethodPost, "/v1/audio/transcriptions", body)
		result, err := service.ForwardMistralAudioTranscription(context.Background(), ginContext, mistralContractAccount(), body, contentType, "")
		require.NoError(t, err)
		require.True(t, result.Stream)
		require.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
		require.Contains(t, recorder.Body.String(), "event: transcription.text.delta")
		require.Contains(t, recorder.Body.String(), "event: transcription.done")
		require.Contains(t, recorder.Body.String(), "I traveled to Chicago.")
		require.NotNil(t, result.AudioUsage)
		require.InDelta(t, 203.0/3600.0, result.AudioUsage.DurationOrUnits, 1e-12)
		require.Equal(t, 635, result.Usage.OutputTokens)
	})

	t.Run("transcription rejects invalid requests and unknown usage", func(t *testing.T) {
		fixture, err := os.ReadFile("testdata/mistral_transcription_response.json")
		require.NoError(t, err)
		unknownUsage := strings.Replace(string(fixture), `"prompt_audio_seconds": 203`, `"prompt_audio_seconds": null`, 1)
		fileOnlyMultipart := func() ([]byte, string) {
			var buffer bytes.Buffer
			writer := multipart.NewWriter(&buffer)
			file, err := writer.CreateFormFile("file", "speech.mp3")
			require.NoError(t, err)
			_, err = file.Write([]byte("synthetic-audio-bytes"))
			require.NoError(t, err)
			require.NoError(t, writer.Close())
			return buffer.Bytes(), writer.FormDataContentType()
		}
		fileOnly, fileOnlyContentType := fileOnlyMultipart()
		modelOnly, modelOnlyContentType := transcriptionMultipart(t, "voxtral-mini-2507", false)
		for _, scenario := range []struct {
			name        string
			body        []byte
			contentType string
			response    string
			wantSent    bool
		}{
			{"not multipart", []byte(`{"model":"voxtral-mini-2507"}`), "application/json", string(fixture), false},
			{"missing model", fileOnly, fileOnlyContentType, string(fixture), false},
			{"unknown usage", modelOnly, modelOnlyContentType, unknownUsage, true},
		} {
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(scenario.response))}}
			service := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
			ginContext, _ := newActionContext(http.MethodPost, "/v1/audio/transcriptions", scenario.body)
			result, err := service.ForwardMistralAudioTranscription(context.Background(), ginContext, mistralContractAccount(), scenario.body, scenario.contentType, "")
			require.Error(t, err, scenario.name)
			require.Nil(t, result, scenario.name)
			require.Equal(t, scenario.wantSent, upstream.lastReq != nil, scenario.name)
			if scenario.wantSent {
				require.ErrorContains(t, err, "unknown", scenario.name)
			}
		}
	})

	t.Run("transcription truncated stream is an error not a partial success", func(t *testing.T) {
		truncated := "event: transcription.text.delta\ndata: {\"type\":\"transcription.text.delta\",\"text\":\"half\"}\n\n"
		body, contentType := transcriptionMultipart(t, "voxtral-mini-2507", true)
		upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(truncated))}}
		service := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
		ginContext, recorder := newActionContext(http.MethodPost, "/v1/audio/transcriptions", body)
		result, err := service.ForwardMistralAudioTranscription(context.Background(), ginContext, mistralContractAccount(), body, contentType, "")
		require.Error(t, err)
		require.Nil(t, result, "unknown usage must never produce a trusted-zero settlement result")
		require.Contains(t, recorder.Body.String(), "half", "already streamed bytes stay streamed")
	})

	t.Run("speech forwards JSON and bills input characters", func(t *testing.T) {
		for _, stream := range []bool{false, true} {
			var responseBody string
			contentType := "application/json"
			if stream {
				fixture, err := os.ReadFile("testdata/mistral_speech.sse")
				require.NoError(t, err)
				responseBody, contentType = string(fixture), "text/event-stream"
			} else {
				responseBody = `{"audio_data":"UklGRmQAAABXQVZFZm10"}`
			}
			body := []byte(fmt.Sprintf(`{"model":"voxtral-mini-tts-2603","input":"Bonjour le monde","voice_id":"5a271406-039d-46fe-835b-fbbb00eaf08d","response_format":"mp3","stream":%t}`, stream))
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {contentType}, "X-Request-Id": {"req-tts"}}, Body: io.NopCloser(strings.NewReader(responseBody))}}
			service := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
			ginContext, recorder := newActionContext(http.MethodPost, "/v1/audio/speech", body)
			result, err := service.ForwardMistralAudioSpeech(context.Background(), ginContext, mistralContractAccount(), body, "")
			require.NoError(t, err)
			require.Equal(t, "https://api.mistral.ai/v1/audio/speech", upstream.lastReq.URL.String())
			require.Equal(t, "Bearer sk-test", upstream.lastReq.Header.Get("Authorization"))
			require.JSONEq(t, string(body), string(upstream.lastBody))
			require.NotNil(t, result.AudioUsage)
			require.Equal(t, "tts", result.AudioUsage.Mode)
			require.InDelta(t, 16.0/1_000_000.0, result.AudioUsage.DurationOrUnits, 1e-12, "official pricing unit is input characters")
			if stream {
				require.True(t, result.Stream)
				require.Contains(t, recorder.Body.String(), "event: speech.audio.delta")
				require.Contains(t, recorder.Body.String(), "event: speech.audio.done")
				require.Equal(t, 9, result.Usage.InputTokens, "terminal done usage is preserved for observability")
			} else {
				require.JSONEq(t, responseBody, recorder.Body.String())
			}
		}
	})

	t.Run("speech counts runes not bytes for multibyte input", func(t *testing.T) {
		body := []byte(`{"model":"voxtral-mini-tts-2603","input":"你好世界","voice_id":"v1"}`)
		require.Equal(t, 4, mistralSpeechBillableCharacters(body))
		upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"audio_data":"UklGRg=="}`))}}
		service := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
		ginContext, _ := newActionContext(http.MethodPost, "/v1/audio/speech", body)
		result, err := service.ForwardMistralAudioSpeech(context.Background(), ginContext, mistralContractAccount(), body, "")
		require.NoError(t, err)
		require.InDelta(t, 4.0/1_000_000.0, result.AudioUsage.DurationOrUnits, 1e-12)
	})

	t.Run("speech rejects invalid requests before upstream", func(t *testing.T) {
		upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{}`))}}
		service := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
		for _, body := range []string{
			`{"input":"hello","voice_id":"v1"}`,
			`{"model":"voxtral-mini-tts-2603","voice_id":"v1"}`,
			`{"model":"voxtral-mini-tts-2603","input":"  ","voice_id":"v1"}`,
			`not-json`,
		} {
			ginContext, _ := newActionContext(http.MethodPost, "/v1/audio/speech", []byte(body))
			_, err := service.ForwardMistralAudioSpeech(context.Background(), ginContext, mistralContractAccount(), []byte(body), "")
			require.Error(t, err, body)
		}
		require.Nil(t, upstream.lastReq)
	})

	t.Run("speech error status follows shared failover classification", func(t *testing.T) {
		upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 429, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"message":"synthetic failure"}`))}}
		service := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
		body := []byte(`{"model":"voxtral-mini-tts-2603","input":"hello","voice_id":"v1"}`)
		ginContext, recorder := newActionContext(http.MethodPost, "/v1/audio/speech", body)
		_, err := service.ForwardMistralAudioSpeech(context.Background(), ginContext, mistralContractAccount(), body, "")
		require.Error(t, err)
		require.Len(t, upstream.requests, 1)
		var failoverErr *UpstreamFailoverError
		if errors.As(err, &failoverErr) {
			require.Equal(t, http.StatusTooManyRequests, failoverErr.StatusCode)
		} else {
			require.Contains(t, recorder.Body.String(), `"error"`)
		}
	})

	t.Run("voices CRUD passes through with method and path guards", func(t *testing.T) {
		for _, scenario := range []struct {
			method, subpath, wantURL string
			body                     []byte
		}{
			{http.MethodGet, "", "https://api.mistral.ai/v1/audio/voices", nil},
			{http.MethodPost, "", "https://api.mistral.ai/v1/audio/voices", []byte(`{"name":"brand-voice"}`)},
			{http.MethodGet, "5a271406-039d-46fe-835b-fbbb00eaf08d", "https://api.mistral.ai/v1/audio/voices/5a271406-039d-46fe-835b-fbbb00eaf08d", nil},
			{http.MethodPatch, "5a271406-039d-46fe-835b-fbbb00eaf08d", "https://api.mistral.ai/v1/audio/voices/5a271406-039d-46fe-835b-fbbb00eaf08d", []byte(`{"name":"renamed"}`)},
			{http.MethodDelete, "5a271406-039d-46fe-835b-fbbb00eaf08d", "https://api.mistral.ai/v1/audio/voices/5a271406-039d-46fe-835b-fbbb00eaf08d", nil},
			{http.MethodGet, "5a271406-039d-46fe-835b-fbbb00eaf08d/sample", "https://api.mistral.ai/v1/audio/voices/5a271406-039d-46fe-835b-fbbb00eaf08d/sample", nil},
		} {
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"voice-fixture"}`))}}
			service := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
			ginContext, recorder := newActionContext(scenario.method, "/v1/audio/voices", scenario.body)
			contentType := ""
			if scenario.body != nil {
				contentType = "application/json"
			}
			result, err := service.ForwardMistralVoices(context.Background(), ginContext, mistralContractAccount(), scenario.subpath, scenario.body, contentType, scenario.method)
			require.NoError(t, err, scenario.wantURL)
			require.Equal(t, scenario.wantURL, upstream.lastReq.URL.String())
			require.Equal(t, scenario.method, upstream.lastReq.Method)
			if scenario.body != nil {
				require.Equal(t, scenario.body, upstream.lastBody)
			}
			require.Nil(t, result.AudioUsage, "voice management is not a billable generation action")
			require.JSONEq(t, `{"id":"voice-fixture"}`, recorder.Body.String())
		}
	})

	t.Run("voices rejects traversal and unsupported endpoints", func(t *testing.T) {
		upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{}`))}}
		service := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
		for _, subpath := range []string{"../models", "voice/../sample", "voice-id/sample/extra", "voice%2e%2e/..", "a//b"} {
			ginContext, _ := newActionContext(http.MethodGet, "/v1/audio/voices", nil)
			_, err := service.ForwardMistralVoices(context.Background(), ginContext, mistralContractAccount(), subpath, nil, "", http.MethodGet)
			require.Error(t, err, subpath)
		}
		ginContext, _ := newActionContext(http.MethodPut, "/v1/audio/voices", nil)
		_, err := service.ForwardMistralVoices(context.Background(), ginContext, mistralContractAccount(), "", nil, "", http.MethodPut)
		require.Error(t, err)
		require.Nil(t, upstream.lastReq)
	})

	t.Run("audio usage resolves prices and rejects unknown modes", func(t *testing.T) {
		billing := &BillingService{}
		sttHours := 203.0 / 3600.0
		require.Positive(t, billing.CalculateAudioCost("stt", sttHours, nil, 1).TotalCost, "missing group price falls back to defaults, never free-by-absence")
		groupSTT := 0.18
		cost := billing.CalculateAudioCost("stt", sttHours, &audioPriceConfig{STTPerHour: &groupSTT}, 1)
		require.InDelta(t, sttHours*groupSTT, cost.TotalCost, 1e-9, "group price wins over defaults")
		groupTTS := 16.0
		cost = billing.CalculateAudioCost("tts", 17.0/1_000_000.0, &audioPriceConfig{TTSPerMChars: &groupTTS}, 1)
		require.InDelta(t, 17.0/1_000_000.0*groupTTS, cost.TotalCost, 1e-9)
		require.Equal(t, PricingStateUnpriced, billing.CalculateAudioCost("translate", 1, nil, 1).PricingState, "unknown audio modes must not price")
	})
}

func TestMistralActionsCancelInFlight(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	stream := newContextBoundBlockingReadCloser([]byte("event: speech.audio.delta\ndata: {\"type\":\"speech.audio.delta\",\"audio_data\":\"UklGRg==\"}\n\n"))
	t.Cleanup(stream.forceUnblock)
	service := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: &contextBoundHTTPUpstream{body: stream}}
	body := []byte(`{"model":"voxtral-mini-tts-2603","input":"hello","voice_id":"v1","stream":true}`)
	recorder := httptest.NewRecorder()
	ginContext, _ := gin.CreateTestContext(recorder)
	ginContext.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/speech", bytes.NewReader(body)).WithContext(ctx)
	finished := make(chan error, 1)
	go func() {
		_, err := service.ForwardMistralAudioSpeech(ctx, ginContext, mistralContractAccount(), body, "")
		finished <- err
	}()
	select {
	case err := <-finished:
		require.Error(t, err)
		require.Error(t, stream.ctx.Err())
	case <-time.After(8 * time.Second):
		stream.forceUnblock()
		<-finished
		t.Fatal("mistral event stream failed to propagate cancellation")
	}
}
