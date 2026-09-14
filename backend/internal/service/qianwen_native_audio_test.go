//go:build unit

package service

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const audioTTSRequest = `{"model":"qwen3-tts-flash","input":{"text":"你好。","voice":"Cherry","language_type":"Chinese"}}`
const audioASRRequest = `{"model":"qwen3-asr-flash","input":{"messages":[{"role":"user","content":[{"audio":"https://example.com/a.wav"}]}]},"parameters":{"asr_options":{"enable_itn":false}}}`
const audioTTSResponse = `{"request_id":"tts-native","output":{"finish_reason":"stop","audio":{"url":"https://example.com/a.wav","data":""}},"usage":{"characters":3,"input_tokens":0,"output_tokens":0}}`
const audioASRResponse = `{"request_id":"asr-native","output":{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":[{"text":"你好。"}]}}]},"usage":{"seconds":9,"output_tokens_details":{"text_tokens":3}}}`

func audioTestKey() *APIKey {
	tts, stt := 20.0, 2.0 // Fixture tariffs, not Alibaba prices.
	return &APIKey{ID: 10123, GroupID: i64p(123), Group: &Group{ID: 123, Platform: PlatformQianwen, RateMultiplier: 1, AudioTTSPricePerMillionChars: &tts, AudioSTTPricePerHour: &stt}}
}

func TestQianwenNativeAudioForwardAndCharge(t *testing.T) {
	for _, tc := range []struct {
		body, response, mode, tier string
		units, cost                float64
	}{
		{audioTTSRequest, audioTTSResponse, "tts", "qianwen_audio_v1:characters=3", 3.0 / 1e6, 0.00006},
		{audioASRRequest, audioASRResponse, "stt", "qianwen_audio_v1:seconds=9", 9.0 / 3600, 0.005},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			upstream := &httpUpstreamRecorder{resp: qianwenModelPage(tc.response, 200)}
			upstream.resp.Header.Set("Content-Type", "application/json")
			s := &OpenAIGatewayService{httpUpstream: upstream}
			account := qianwenTestAccount()
			key := audioTestKey()
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("POST", QianwenNativeMultimodalPath, nil)
			c.Set("api_key", key)
			result, err := s.ForwardQianwenNative(context.Background(), c, account, QianwenNativeMultimodal, []byte(tc.body), false)
			require.NoError(t, err)
			require.Equal(t, 200, w.Code)
			require.Equal(t, tc.response, w.Body.String())
			require.Equal(t, QianwenNativeMultimodalPath, upstream.requests[0].URL.Path)
			require.JSONEq(t, tc.body, string(upstream.bodies[0]))
			require.Nil(t, result.ThinkingEnabled)
			require.False(t, gjson.GetBytes(upstream.bodies[0], "parameters.enable_thinking").Exists())
			require.Equal(t, OpenAIUsage{}, result.Usage)
			require.Equal(t, tc.mode, result.AudioUsage.Mode)
			require.InDelta(t, tc.units, result.AudioUsage.DurationOrUnits, 1e-12)
			deadline, ok := upstream.requests[0].Context().Deadline()
			require.True(t, ok)
			require.LessOrEqual(t, time.Until(deadline), qianwenNativeAudioTimeout)
			repo := &openAIRecordUsageLogRepoStub{inserted: true}
			billing := newOpenAIRecordUsageServiceForTest(repo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
			err = billing.RecordUsage(context.Background(), &OpenAIRecordUsageInput{Result: result, Account: account, User: &User{ID: 20123}, APIKey: key})
			require.NoError(t, err)
			require.NotNil(t, repo.lastLog)
			require.InDelta(t, tc.cost, repo.lastLog.TotalCost, 1e-12)
			require.InDelta(t, tc.cost, repo.lastLog.ActualCost, 1e-12)
			require.Equal(t, tc.tier, *repo.lastLog.BillingTier)
			require.Equal(t, "qianwen_audio_group", *repo.lastLog.PriceSource)
			require.Zero(t, repo.lastLog.InputTokens)
			require.Zero(t, repo.lastLog.OutputTokens)
		})
	}
}

func TestQianwenNativeAudioFailClosed(t *testing.T) {
	for _, model := range []string{"qwen-tts", "qwen3-tts-flash-future", "qwen3-asr-flash-filetrans", "qwen3-asr-flash-realtime", "paraformer-v2", "sensevoice-v1", "fun-asr", "cosyvoice-v3", "sambert-zhichu-v1", "voice-enrollment"} {
		body := []byte(strings.Replace(audioTTSRequest, "qwen3-tts-flash", model, 1))
		_, err := ParseQianwenNativeBody(body)
		require.ErrorContains(t, err, "unsupported", model)
		// Account aliases cannot evade the post-mapping operation gate.
		a := qianwenTestAccount()
		a.Credentials["model_mapping"] = map[string]any{"alias": model}
		_, err = (&OpenAIGatewayService{}).BuildQianwenNativeRequest(context.Background(), a, QianwenNativeMultimodal, []byte(`{"model":"alias","input":{"prompt":"hi"}}`), false)
		require.Error(t, err, model)
	}
	for _, body := range []string{
		strings.Replace(audioTTSRequest, `"text":"你好。"`, `"text":12`, 1),
		strings.Replace(audioTTSRequest, `"voice":"Cherry"`, `"voice":""`, 1),
		strings.Replace(audioTTSRequest, `"你好。"`, `"`+strings.Repeat("好", 601)+`"`, 1),
		strings.Replace(audioASRRequest, "https://example.com/a.wav", "file:///etc/passwd", 1),
		strings.Replace(audioASRRequest, `"audio"`, `"image"`, 1),
		strings.Replace(audioASRRequest, `"user"`, `"assistant"`, 1),
		`{"model":"qwen3-tts-flash","input":{"text":"ok","voice":"Cherry"},"input":{"text":"","voice":"Cherry"}}`,
		`{"model":"qwen3-tts-flash","unknown":1e400,"input":{"text":"ok","voice":"Cherry"},"input":{"text":"","voice":"Cherry"}}`,
	} {
		_, err := ParseQianwenNativeBody([]byte(body))
		require.Error(t, err, body)
	}
	for _, body := range []string{audioTTSRequest, audioASRRequest} {
		model, err := ParseQianwenNativeBody([]byte(body))
		require.NoError(t, err)
		require.Error(t, ValidateQianwenNativeOperation(QianwenNativeText, model, []byte(body), false))
		require.Error(t, ValidateQianwenNativeOperation(QianwenNativeMultimodal, model, []byte(body), true))
		for _, field := range []string{`"stream":true`, `"parameters":{"enable_thinking":false}`, `"parameters":{"incremental_output":true}`} {
			modified := strings.TrimSuffix(body, "}") + "," + field + "}"
			require.Error(t, ValidateQianwenNativeOperation(QianwenNativeMultimodal, model, []byte(modified), false))
		}
	}
}

func TestQianwenNativeAudioMissingPriceNeverCallsUpstream(t *testing.T) {
	for _, rate := range []*float64{nil, func() *float64 { v := -1.0; return &v }(), func() *float64 { v := math.NaN(); return &v }()} {
		u := &httpUpstreamRecorder{}
		s := &OpenAIGatewayService{httpUpstream: u}
		key := audioTestKey()
		key.Group.AudioTTSPricePerMillionChars = rate
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", QianwenNativeMultimodalPath, nil)
		c.Set("api_key", key)
		result, err := s.ForwardQianwenNative(context.Background(), c, qianwenTestAccount(), QianwenNativeMultimodal, []byte(audioTTSRequest), false)
		require.ErrorContains(t, err, "explicit nonnegative group audio rate")
		require.Nil(t, result)
		require.Empty(t, u.requests)
		require.Equal(t, 400, c.Writer.Status())
	}
}

func TestQianwenNativeAudioInvalidResponses(t *testing.T) {
	for _, tc := range []struct{ model, body string }{
		{"qwen3-tts-flash", `not-json`},
		{"qwen3-tts-flash", audioTTSResponse + `garbage`},
		{"qwen3-tts-flash", `{"code":"InvalidParameter"}`},
		{"qwen3-tts-flash", `{"output":{"task_id":"unowned","task_status":"PENDING"}}`},
		{"qwen3-tts-flash", strings.Replace(audioTTSResponse, `"characters":3`, `"characters":0`, 1)},
		{"qwen3-tts-flash", strings.Replace(audioTTSResponse, `"characters":3`, `"characters":1.5`, 1)},
		{"qwen3-tts-flash", strings.Replace(audioTTSResponse, `"characters":3`, `"characters":"3"`, 1)},
		{"qwen3-tts-flash", strings.Replace(audioTTSResponse, `"characters":3`, `"unknown":3`, 1)},
		{"qwen3-tts-flash", strings.Replace(audioTTSResponse, `"stop"`, `null`, 1)},
		{"qwen3-asr-flash", strings.Replace(audioASRResponse, `"seconds":9`, `"duration":9`, 1)},
		{"qwen3-asr-flash", strings.Replace(audioASRResponse, `"text":"你好。"`, `"text":null`, 1)},
	} {
		result := &OpenAIForwardResult{UpstreamModel: tc.model}
		require.Error(t, observeQianwenNativeAudio([]byte(tc.body), result), tc.body)
		require.Nil(t, result.AudioUsage)
	}
	for _, status := range []int{http.StatusAccepted, http.StatusNoContent} {
		u := &httpUpstreamRecorder{resp: qianwenModelPage(audioTTSResponse, status)}
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", QianwenNativeMultimodalPath, nil)
		c.Set("api_key", audioTestKey())
		result, err := (&OpenAIGatewayService{httpUpstream: u}).ForwardQianwenNative(context.Background(), c, qianwenTestAccount(), QianwenNativeMultimodal, []byte(audioTTSRequest), false)
		require.Error(t, err)
		require.Nil(t, result)
		require.Equal(t, 502, w.Code)
		require.Equal(t, "upstream_error", gjson.Get(w.Body.String(), "error.type").String())
	}
}

func TestQianwenNativeAudioHeaderOverridesCannotStartJobs(t *testing.T) {
	req := httptest.NewRequest("POST", QianwenNativeMultimodalPath, nil)
	req.Header.Set("X-DashScope-Async", "enable")
	req.Header.Set("X-DashScope-SSE", "enable")
	applyQianwenNativeAudioHeaders(req, "qwen3-tts-flash")
	require.Empty(t, req.Header.Get("X-DashScope-Async"))
	require.Empty(t, req.Header.Get("X-DashScope-SSE"))
	require.Equal(t, "application/json", req.Header.Get("Accept"))
}

func TestQianwenNativeAudioModelMapping(t *testing.T) {
	for _, body := range []string{audioTTSRequest, audioASRRequest} {
		model := gjson.Get(body, "model").String()
		a := qianwenTestAccount()
		a.Credentials["model_mapping"] = map[string]any{model: "qwen-plus"}
		_, err := (&OpenAIGatewayService{}).BuildQianwenNativeRequest(context.Background(), a, QianwenNativeMultimodal, []byte(body), false)
		require.ErrorContains(t, err, "non-audio mapped model")
	}
	a := qianwenTestAccount()
	a.Credentials["model_mapping"] = map[string]any{"qwen3-tts-flash": "qwen3-tts-flash-2025-11-27"}
	req, err := (&OpenAIGatewayService{}).BuildQianwenNativeRequest(context.Background(), a, QianwenNativeMultimodal, []byte(audioTTSRequest), false)
	require.NoError(t, err)
	require.Equal(t, QianwenNativeMultimodalPath, req.URL.Path)
}

func TestQianwenNativeAudioRecordUsageMissingPriceIsError(t *testing.T) {
	repo := &openAIRecordUsageLogRepoStub{inserted: true}
	billing := newOpenAIRecordUsageServiceForTest(repo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
	key := audioTestKey()
	key.Group.AudioTTSPricePerMillionChars = nil
	result := &OpenAIForwardResult{Model: "qwen3-tts-flash", UpstreamModel: "qwen3-tts-flash", UpstreamEndpoint: QianwenNativeMultimodalPath, AudioUsage: &AudioUsage{Mode: "tts", DurationOrUnits: 3.0 / 1e6}}
	err := billing.RecordUsage(context.Background(), &OpenAIRecordUsageInput{Result: result, Account: qianwenTestAccount(), User: &User{ID: 20123}, APIKey: key})
	require.ErrorContains(t, err, "explicit nonnegative group audio rate")
	require.Nil(t, repo.lastLog)
}
