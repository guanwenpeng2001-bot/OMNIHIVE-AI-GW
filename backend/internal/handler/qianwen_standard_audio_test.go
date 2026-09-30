//go:build unit

package handler

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"mime/multipart"
	"net/http/httptest"
	"net/textproto"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func qianwenAudioUploadContext(t *testing.T, filename, mediaType string, audio []byte, fields map[string]string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, value := range fields {
		require.NoError(t, writer.WriteField(key, value))
	}
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename=%q`, filename))
	header.Set("Content-Type", mediaType)
	part, err := writer.CreatePart(header)
	require.NoError(t, err)
	_, err = part.Write(audio)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	c.Request = httptest.NewRequest("POST", "/v1/audio/transcriptions", &body)
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	return c, response
}

func TestQianwenAudioMultipartBody(t *testing.T) {
	audio := []byte("RIFFfixture")
	c, _ := qianwenAudioUploadContext(t, "generated-speech.wav", "application/octet-stream", audio, map[string]string{"model": "qwen3-asr-flash"})
	body, err := readQianwenAudioMultipart(c)
	require.NoError(t, err)
	require.Equal(t, "data:audio/wav;base64,"+base64.StdEncoding.EncodeToString(audio), gjson.GetBytes(body, "input.messages.0.content.0.audio").String())
	require.Empty(t, c.Request.MultipartForm.File, "streaming multipart never spills files to disk")
}

func TestQianwenAudioMultipartRejections(t *testing.T) {
	for _, filename := range []string{""} {
		c, w := qianwenAudioUploadContext(t, filename, "audio/wav", []byte("audio"), map[string]string{"model": "qwen3-asr-flash"})
		(&OpenAIGatewayHandler{}).QianwenAudioTranscriptions(c)
		require.Equal(t, 400, w.Code, filename)
	}
	for _, fields := range []map[string]string{{}, {"model": "   "}, {"model": "qwen3-asr-flash", "file_url": "/etc/passwd"}, {"model": "qwen3-asr-flash", "response_format": "text"}} {
		c, w := qianwenAudioUploadContext(t, "audio.wav", "audio/wav", []byte("audio"), fields)
		(&OpenAIGatewayHandler{}).QianwenAudioTranscriptions(c)
		require.Equal(t, 400, w.Code)
	}
	c, w := qianwenAudioUploadContext(t, "audio.wav", "text/plain", []byte("audio"), map[string]string{"model": "qwen3-asr-flash"})
	(&OpenAIGatewayHandler{}).QianwenAudioTranscriptions(c)
	require.Equal(t, 400, w.Code)
	c, w = qianwenAudioUploadContext(t, "audio.wav", "audio/wav", make([]byte, service.QianwenAudioMaxUploadBytes+1), map[string]string{"model": "qwen3-asr-flash"})
	c.Request.ContentLength = -1 // limit also applies to chunked/unknown-length uploads
	(&OpenAIGatewayHandler{}).QianwenAudioTranscriptions(c)
	require.Equal(t, 413, w.Code, w.Body.String())
	c, _ = qianwenAudioUploadContext(t, "audio.wav", "audio/wav", make([]byte, service.QianwenAudioMaxUploadBytes), map[string]string{"model": "qwen3-asr-flash"})
	_, err := readQianwenAudioMultipart(c)
	require.NoError(t, err)
	response := httptest.NewRecorder()
	c, _ = gin.CreateTestContext(response)
	c.Request = httptest.NewRequest("POST", "/v1/audio/transcriptions", bytes.NewBufferString(`{"model":"qwen3-asr-flash","file":"/etc/passwd"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	(&OpenAIGatewayHandler{}).QianwenAudioTranscriptions(c)
	require.Equal(t, 400, response.Code)
}

func TestQianwenAudioTranscriptionsHandlerNativeWire(t *testing.T) {
	for _, requestedModel := range []string{"qwen3-asr-flash", "selected-model", "private-audio-alias"} {
		t.Run(requestedModel, func(t *testing.T) {
			payload := `{"request_id":"asr-compat","output":{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":[{"text":"你好，测试成功。"}]}}]},"usage":{"seconds":2}}`
			upstream := &httpUpstreamRecorderStub{body: payload}
			account := qianwenCatalogAccount(1, requestedModel, "chat_completions")
			account.Credentials["model_mapping"] = map[string]any{requestedModel: "qwen3-asr-flash"}
			handler := newQianwenStandardTestHandler(t, upstream, []service.Account{account})
			rate := 2.0
			group := &service.Group{ID: 7, Platform: service.PlatformQianwen, RateMultiplier: 1, AudioSTTPricePerHour: &rate}
			c, response := qianwenStandardContext(t, "POST", "/v1/audio/transcriptions", nil, group)
			upload, _ := qianwenAudioUploadContext(t, "generated-speech.wav", "audio/wav", []byte("RIFFfixture"), map[string]string{"model": requestedModel})
			c.Request = upload.Request
			handler.QianwenAudioTranscriptions(c)
			require.Equal(t, 200, response.Code, response.Body.String())
			require.JSONEq(t, `{"text":"你好，测试成功。"}`, response.Body.String())
			require.Equal(t, "https://dashscope.aliyuncs.com/api/v1/services/aigc/multimodal-generation/generation", upstream.lastURL)
			require.Contains(t, gjson.GetBytes(upstream.lastBody, "input.messages.0.content.0.audio").String(), "data:audio/wav;base64,")
			require.Equal(t, "qwen3-asr-flash", gjson.GetBytes(upstream.lastBody, "model").String())
		})
	}
}

func TestQianwenAudioSpeechHandlerRejectsUnsupportedFormat(t *testing.T) {
	upstream := &httpUpstreamRecorderStub{}
	handler := newQianwenStandardTestHandler(t, upstream, []service.Account{qianwenCatalogAccount(1, "qwen3-tts-flash", "chat_completions")})
	c, response := qianwenStandardContext(t, "POST", "/v1/audio/speech", []byte(`{"model":"qwen3-tts-flash","input":"你好。","voice":"Cherry","response_format":"mp3"}`), nil)
	handler.QianwenAudioSpeech(c)
	require.Equal(t, 400, response.Code, response.Body.String())
	require.Empty(t, upstream.lastURL)
}

func TestQianwenAudioMultipartFilenameMetadata(t *testing.T) {
	for _, filename := range []string{"take..wav", `C:\fakepath\recording.wav`, "../recording.wav", "/missing/server/file.wav", "file:///missing.wav"} {
		t.Run(filename, func(t *testing.T) {
			audio := []byte("uploaded bytes only")
			c, _ := qianwenAudioUploadContext(t, filename, "application/octet-stream", audio, map[string]string{"model": "selected-model"})
			body, err := readQianwenAudioMultipart(c)
			require.NoError(t, err)
			require.Equal(t, "selected-model", gjson.GetBytes(body, "model").String())
			require.Equal(t, "data:audio/wav;base64,"+base64.StdEncoding.EncodeToString(audio), gjson.GetBytes(body, "input.messages.0.content.0.audio").String())
		})
	}
}

func TestQianwenAudioMultipartRejectsPlainPathField(t *testing.T) {
	for _, path := range []string{"/etc/passwd", `C:\secret.wav`, "file:///secret.wav"} {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		require.NoError(t, writer.WriteField("model", "selected-model"))
		require.NoError(t, writer.WriteField("file", path))
		require.NoError(t, writer.Close())
		response := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(response)
		c.Request = httptest.NewRequest("POST", "/v1/audio/transcriptions", &body)
		c.Request.Header.Set("Content-Type", writer.FormDataContentType())
		(&OpenAIGatewayHandler{}).QianwenAudioTranscriptions(c)
		require.Equal(t, 400, response.Code)
		require.Equal(t, "invalid_request_error", gjson.GetBytes(response.Body.Bytes(), "error.type").String())
	}
}

func TestQianwenAudioSpeechHandlerRejectsMalformedRequests(t *testing.T) {
	for _, body := range []string{`{`, `{}`, `{"input":"hello","voice":"Cherry"}`, `{"model":"   ","input":"hello","voice":"Cherry"}`, `{"model":123,"input":"hello","voice":"Cherry"}`, `{"model":"selected-model","voice":"Cherry"}`, `{"model":"selected-model","input":{},"voice":"Cherry"}`} {
		upstream := &httpUpstreamRecorderStub{}
		handler := newQianwenStandardTestHandler(t, upstream, nil)
		c, response := qianwenStandardContext(t, "POST", "/v1/audio/speech", []byte(body), nil)
		handler.QianwenAudioSpeech(c)
		require.Equal(t, 400, response.Code, body)
		require.Empty(t, upstream.lastURL)
	}
}
