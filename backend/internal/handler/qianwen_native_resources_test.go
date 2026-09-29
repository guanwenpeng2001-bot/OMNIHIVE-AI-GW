//go:build unit

package handler

import (
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestQianwenNativeRerankResourceHandlerWire(t *testing.T) {
	native := `{"output":{"results":[{"index":1,"relevance_score":0.88}]},"usage":{"total_tokens":12},"request_id":"rr-native"}`
	recorder := &httpUpstreamRecorderStub{body: native}
	h := newQianwenStandardTestHandler(t, recorder, []service.Account{qianwenCatalogAccount(1, "gte-rerank-v2", "chat_completions")})
	body := []byte(`{"model":"gte-rerank-v2","input":{"query":"q","documents":["a","b"]},"parameters":{"top_n":1}}`)
	c, w := qianwenStandardContext(t, http.MethodPost, service.QianwenNativeRerankPath, body, nil)
	h.QianwenNative(c)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, "https://dashscope.aliyuncs.com/api/v1/services/rerank/text-rerank/text-rerank", recorder.lastURL)
	require.Equal(t, native, w.Body.String(), "native wire passes through verbatim")

	// Conversation models never enter the rerank endpoint; rerank models never
	// enter conversation endpoints; foreign groups are rejected at the gate.
	for _, tc := range []struct {
		path, body string
		status     int
	}{
		{service.QianwenNativeRerankPath, `{"model":"qwen-plus","input":{"query":"q","documents":["a"]}}`, 400},
		{service.QianwenNativeTextPath, `{"model":"gte-rerank-v2","input":{"query":"q","documents":["a"]}}`, 400},
		{service.QianwenNativeEmbeddingPath, `{"model":"qwen-plus","input":{"texts":["a"]}}`, 400},
		{service.QianwenNativeVoiceEnrollmentPath, `{"model":"qwen-plus","input":{"action":"list"}}`, 400},
	} {
		c, w = qianwenStandardContext(t, http.MethodPost, tc.path, []byte(tc.body), nil)
		h.QianwenNative(c)
		require.Equal(t, tc.status, w.Code, tc.body)
	}
	c, w = qianwenStandardContext(t, http.MethodPost, service.QianwenNativeRerankPath, body, &service.Group{ID: 11, Platform: service.PlatformKimi})
	h.QianwenNative(c)
	require.Equal(t, 404, w.Code)
	// Async/SSE headers are rejected on the synchronous resource families.
	c, w = qianwenStandardContext(t, http.MethodPost, service.QianwenNativeRerankPath, body, nil)
	c.Request.Header.Set("X-DashScope-Async", "enable")
	h.QianwenNative(c)
	require.Equal(t, 400, w.Code)
}

func TestQianwenNativeEmbeddingResourceHandlerWire(t *testing.T) {
	native := `{"status_code":200,"request_id":"emb-native","output":{"embeddings":[{"embedding":[0.5],"text_index":0}]},"usage":{"total_tokens":9}}`
	recorder := &httpUpstreamRecorderStub{body: native}
	h := newQianwenStandardTestHandler(t, recorder, []service.Account{qianwenCatalogAccount(1, "text-embedding-v4", "chat_completions")})
	body := []byte(`{"model":"text-embedding-v4","input":{"texts":["hello"]}}`)
	c, w := qianwenStandardContext(t, http.MethodPost, service.QianwenNativeEmbeddingPath, body, nil)
	h.QianwenNative(c)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, "https://dashscope.aliyuncs.com/api/v1/services/embeddings/text-embedding/text-embedding", recorder.lastURL)
	require.Equal(t, native, w.Body.String())
}

func TestQianwenNativeVoiceEnrollmentResourceHandlerWire(t *testing.T) {
	native := `{"output":{"voice":"myvoice","target_model":"qwen3-tts-vc-realtime-2026-01-15"},"usage":{"count":1},"request_id":"vc-native"}`
	recorder := &httpUpstreamRecorderStub{body: native}
	account := qianwenCatalogAccount(1, "qwen-voice-enrollment", "chat_completions")
	account.Extra["qianwen_pricing_sync"] = map[string]any{"region": "asia-pacific-china", "usd_per_cny": 1.0 / 7, "fx_source": "test operator rate", "fx_effective_at": "2026-09-13T00:00:00Z"}
	h := newQianwenStandardTestHandler(t, recorder, []service.Account{account})
	body := []byte(`{"model":"qwen-voice-enrollment","input":{"action":"create","target_model":"qwen3-tts-vc-realtime-2026-01-15","preferred_name":"myvoice","audio":{"data":"https://example.com/a.wav"}}}`)
	c, w := qianwenStandardContext(t, http.MethodPost, service.QianwenNativeVoiceEnrollmentPath, body, nil)
	h.QianwenNative(c)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, "https://dashscope.aliyuncs.com/api/v1/services/audio/tts/customization", recorder.lastURL)
	require.Equal(t, "create", gjson.GetBytes(recorder.lastBody, "input.action").String())
	require.Equal(t, native, w.Body.String())

	// A clone create without operator FX provenance fails before upstream.
	h = newQianwenStandardTestHandler(t, recorder, []service.Account{qianwenCatalogAccount(2, "qwen-voice-enrollment", "chat_completions")})
	c, w = qianwenStandardContext(t, http.MethodPost, service.QianwenNativeVoiceEnrollmentPath, body, nil)
	h.QianwenNative(c)
	require.Equal(t, 400, w.Code)
	require.Contains(t, w.Body.String(), "conversion")
	require.Equal(t, 1, recorder.calls, "the FX-rejected create must not consume upstream")
}
