//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestQianwenAudioDataURIValidation(t *testing.T) {
	for _, source := range []string{"https://example.com/a.wav", "http://example.com/a.mp3", "data:audio/wav;base64,UklGRg==", "data:audio/mpeg;base64,YQ=="} {
		require.NoError(t, validateQianwenAudioSource(source))
		body := strings.Replace(audioASRRequest, "https://example.com/a.wav", source, 1)
		_, err := ParseQianwenNativeBody([]byte(body))
		require.NoError(t, err)
	}
	for _, source := range []string{"file:///etc/passwd", "/etc/passwd", `C:\secret.wav`, "data:text/plain;base64,YQ==", "data:audio/unknown;base64,YQ==", "data:audio/wav,YQ==", "data:audio/wav;base64,", "data:audio/wav;base64,%%%", "data:audio/wav;base64,YR==", "data:audio/wav;base64,YQ==\n", "data:audio/wav;foo=bar;base64,YQ=="} {
		require.Error(t, validateQianwenAudioSource(source), source)
	}
	atLimit := bytes.Repeat([]byte{1}, QianwenAudioMaxUploadBytes)
	body, err := QianwenAudioTranscriptionBody("qwen3-asr-flash", "audio/wav", atLimit)
	require.NoError(t, err)
	source := gjson.GetBytes(body, "input.messages.0.content.0.audio").String()
	require.LessOrEqual(t, len(source), QianwenAudioMaxEncodedBytes)
	require.NoError(t, validateQianwenAudioSource(source))
	_, err = QianwenAudioTranscriptionBody("qwen3-asr-flash", "audio/wav", append(atLimit, 1))
	require.ErrorIs(t, err, ErrQianwenAudioTooLarge)
	require.ErrorIs(t, validateQianwenAudioSource("data:audio/wav;base64,"+base64.StdEncoding.EncodeToString(append(atLimit, 1))), ErrQianwenAudioTooLarge)
}

func TestQianwenAudioSpeechRequest(t *testing.T) {
	native, err := QianwenAudioSpeechBody([]byte(`{"model":"qwen3-tts-flash","input":"你好。","voice":"Cherry","response_format":"wav"}`))
	require.NoError(t, err)
	require.JSONEq(t, `{"model":"qwen3-tts-flash","input":{"text":"你好。","voice":"Cherry"}}`, string(native))
	for _, body := range []string{
		`{"model":"qwen3-tts-flash","input":"hi","voice":"Cherry","response_format":"mp3"}`,
		`{"model":"qwen3-tts-flash","input":"hi","voice":"Cherry","stream":true}`,
		`{"model":"qwen3-asr-flash","input":"hi","voice":"Cherry"}`,
		`{"model":"qwen3-tts-flash","input":"","voice":"Cherry"}`,
		`{"model":"qwen3-tts-flash","input":"hi","voice":""}`,
		`{"model":"qwen3-tts-flash","input":{},"voice":"Cherry"}`,
		`{} {}`,
	} {
		_, err := QianwenAudioSpeechBody([]byte(body))
		require.Error(t, err)
	}
}

type qianwenSpeechRoundTripper func(*http.Request) (*http.Response, error)

func (f qianwenSpeechRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestQianwenAudioCompatForward(t *testing.T) {
	wav := []byte("RIFF\x04\x00\x00\x00WAVEfixture")
	originalTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = originalTransport })
	downloads := 0
	http.DefaultTransport = qianwenSpeechRoundTripper(func(request *http.Request) (*http.Response, error) {
		downloads++
		require.Equal(t, "https", request.URL.Scheme)
		require.Equal(t, "dashscope-result-bj.oss-cn-beijing.aliyuncs.com", request.URL.Host)
		require.Empty(t, request.Header.Get("Authorization"))
		require.Empty(t, request.Header.Get("Cookie"))
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/octet-stream"}}, ContentLength: int64(len(wav)), Body: io.NopCloser(bytes.NewReader(wav))}, nil
	})
	for _, mode := range []string{"tts", "stt"} {
		body, payload := audioTTSRequest, strings.Replace(audioTTSResponse, "https://example.com/a.wav", "http://dashscope-result-bj.oss-cn-beijing.aliyuncs.com/a.wav", 1)
		if mode == "stt" {
			body, payload = audioASRRequest, audioASRResponse
		}
		upstream := &httpUpstreamRecorder{resp: qianwenModelPage(payload, 200)}
		upstream.resp.Header.Set("Content-Type", "application/json")
		gateway := &OpenAIGatewayService{httpUpstream: upstream}
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest("POST", "/v1/audio/speech", nil)
		c.Set("api_key", audioTestKey())
		writer := c.Writer
		result, err := gateway.ForwardQianwenAudioCompat(context.Background(), c, qianwenTestAccount(), []byte(body), mode)
		require.NoError(t, err)
		require.Same(t, writer, c.Writer)
		require.Equal(t, 200, recorder.Code)
		require.Equal(t, QianwenNativeMultimodalPath, upstream.requests[0].URL.Path)
		require.Equal(t, mode, result.AudioUsage.Mode)
		require.Equal(t, OpenAIUsage{}, result.Usage)
		if mode == "tts" {
			require.Equal(t, "audio/wav", recorder.Header().Get("Content-Type"))
			require.Equal(t, wav, recorder.Body.Bytes())
			require.False(t, gjson.ValidBytes(recorder.Body.Bytes()))
			require.InDelta(t, 3.0/1e6, result.AudioUsage.DurationOrUnits, 1e-12)
		} else {
			require.JSONEq(t, `{"text":"你好。"}`, recorder.Body.String())
			require.InDelta(t, 9.0/3600, result.AudioUsage.DurationOrUnits, 1e-12)
		}
	}
	require.Equal(t, 1, downloads)
}

func TestQianwenSpeechDownloadBoundaries(t *testing.T) {
	originalTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = originalTransport })
	calls := 0
	http.DefaultTransport = qianwenSpeechRoundTripper(func(request *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"http://127.0.0.1/private"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	for _, address := range []string{"file:///etc/passwd", "https://localhost/audio.wav", "http://127.0.0.1/audio.wav", "https://dashscope-result-bj.aliyuncs.com.evil.test/a", "https://user@dashscope-result-bj.aliyuncs.com/a", "https://dashscope-result-bj.aliyuncs.com:8443/a"} {
		_, err := downloadQianwenSpeech(context.Background(), address)
		require.Error(t, err)
	}
	require.Zero(t, calls)
	_, err := downloadQianwenSpeech(context.Background(), "https://dashscope-result-bj.aliyuncs.com/a")
	require.Error(t, err)
	require.Equal(t, 1, calls, "redirect must not be followed")
	for _, payload := range []string{`{"error":"not audio"}`, "RIFF"} {
		http.DefaultTransport = qianwenSpeechRoundTripper(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: make(http.Header), ContentLength: int64(len(payload)), Body: io.NopCloser(strings.NewReader(payload))}, nil
		})
		_, err := downloadQianwenSpeech(context.Background(), "https://dashscope-result-bj.aliyuncs.com/a")
		require.Error(t, err)
	}
}

func TestQianwenAudioCompatRetainsUsageOnDownloadFailure(t *testing.T) {
	upstream := &httpUpstreamRecorder{resp: qianwenModelPage(audioTTSResponse, 200)} // non-provider origin
	upstream.resp.Header.Set("Content-Type", "application/json")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/audio/speech", nil)
	c.Set("api_key", audioTestKey())
	result, err := (&OpenAIGatewayService{httpUpstream: upstream}).ForwardQianwenAudioCompat(context.Background(), c, qianwenTestAccount(), []byte(audioTTSRequest), "tts")
	require.NoError(t, err)
	require.NotNil(t, result.AudioUsage)
	require.Equal(t, 502, c.Writer.Status())
}
