//go:build unit

package service

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func qianwenNativeResourceTestContext(path string) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", path, nil)
	return c, w
}

func TestQianwenNativeRerankForwardWireAndUsage(t *testing.T) {
	payload := `{"request_id":"rr-1","output":{"results":[{"index":0,"relevance_score":0.93,"document":{"text":"a"}},{"index":2,"relevance_score":0.41}]},"usage":{"total_tokens":79}}`
	u := &httpUpstreamRecorder{resp: qianwenModelPage(payload, 200)}
	s := &OpenAIGatewayService{httpUpstream: u}
	a := qianwenTestAccount()
	c, w := qianwenNativeResourceTestContext(QianwenNativeRerankPath)
	body := []byte(`{"model":"gte-rerank-v2","input":{"query":"what is rerank","documents":["a","b","c"]},"parameters":{"top_n":2,"return_documents":true}}`)
	result, err := s.ForwardQianwenNative(context.Background(), c, a, QianwenNativeRerank, body, false)
	require.NoError(t, err)
	require.Equal(t, 200, w.Code)
	require.Equal(t, "/api/v1/services/rerank/text-rerank/text-rerank", u.lastReq.URL.Path)
	require.Equal(t, "Bearer sk-sp-test", u.lastReq.Header.Get("Authorization"))
	require.Empty(t, u.lastReq.Header.Get("X-DashScope-Async"))
	require.Empty(t, u.lastReq.Header.Get("X-DashScope-SSE"))
	require.Equal(t, "gte-rerank-v2", gjson.GetBytes(u.lastBody, "model").String())
	require.Equal(t, "what is rerank", gjson.GetBytes(u.lastBody, "input.query").String())
	require.Equal(t, 3, len(gjson.GetBytes(u.lastBody, "input.documents").Array()))
	require.Equal(t, 79, result.Usage.InputTokens)
	require.Zero(t, result.Usage.OutputTokens)
	require.Equal(t, "rr-1", result.RequestID)
	require.Equal(t, payload, w.Body.String(), "native wire shape passes through verbatim")
}

func TestQianwenNativeRerankValidationRejections(t *testing.T) {
	for _, tc := range []struct{ name, model, body string }{
		{"discontinued v1", "gte-rerank", `{"model":"gte-rerank","input":{"query":"q","documents":["a"]}}`},
		{"flat qwen3-rerank shape unverified", "qwen3-rerank", `{"model":"qwen3-rerank","query":"q","documents":["a"]}`},
		{"empty query", "gte-rerank-v2", `{"model":"gte-rerank-v2","input":{"query":" ","documents":["a"]}}`},
		{"no documents", "gte-rerank-v2", `{"model":"gte-rerank-v2","input":{"query":"q"}}`},
		{"object documents need multimodal adapter", "qwen3-vl-rerank", `{"model":"qwen3-vl-rerank","input":{"query":"q","documents":[{"text":"a"}]}}`},
		{"too many documents", "qwen3-vl-rerank", `{"model":"qwen3-vl-rerank","input":{"query":"q","documents":[` + strings.Repeat(`"d",`, 100) + `"d"]}}`},
		{"top_n beyond documents", "gte-rerank-v2", `{"model":"gte-rerank-v2","input":{"query":"q","documents":["a"]},"parameters":{"top_n":3}}`},
		{"instruct needs vl model", "gte-rerank-v2", `{"model":"gte-rerank-v2","input":{"query":"q","documents":["a"]},"parameters":{"instruct":"rank by recency"}}`},
		{"unknown parameter", "gte-rerank-v2", `{"model":"gte-rerank-v2","input":{"query":"q","documents":["a"]},"parameters":{"fps":1}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u := &httpUpstreamRecorder{resp: qianwenModelPage(`{}`, 200)}
			s := &OpenAIGatewayService{httpUpstream: u}
			c, _ := qianwenNativeResourceTestContext(QianwenNativeRerankPath)
			_, err := s.ForwardQianwenNative(context.Background(), c, qianwenTestAccount(), QianwenNativeRerank, []byte(tc.body), false)
			require.Error(t, err)
			require.Empty(t, u.requests, "rejected before any upstream call")
		})
	}
	// The rerank endpoint never accepts a conversation model, and a rerank model
	// never enters the conversation endpoint.
	err := ValidateQianwenNativeOperation(QianwenNativeRerank, "qwen-plus", []byte(`{"model":"qwen-plus","input":{"query":"q","documents":["a"]}}`), false)
	require.ErrorContains(t, err, "rerank model")
	err = ValidateQianwenNativeOperation(QianwenNativeText, "gte-rerank-v2", []byte(`{"model":"gte-rerank-v2","input":{"query":"q","documents":["a"]}}`), false)
	require.ErrorContains(t, err, "rerank endpoint")
}

func TestQianwenNativeEmbeddingForwardWireAndUsage(t *testing.T) {
	payload := `{"status_code":200,"request_id":"emb-1","output":{"embeddings":[{"embedding":[-0.0069,0.012],"text_index":0}]},"usage":{"total_tokens":27}}`
	u := &httpUpstreamRecorder{resp: qianwenModelPage(payload, 200)}
	s := &OpenAIGatewayService{httpUpstream: u}
	c, w := qianwenNativeResourceTestContext(QianwenNativeEmbeddingPath)
	body := []byte(`{"model":"text-embedding-v4","input":{"texts":["风急天高猿啸哀"]},"parameters":{"dimension":1024,"text_type":"document"}}`)
	result, err := s.ForwardQianwenNative(context.Background(), c, qianwenTestAccount(), QianwenNativeEmbedding, body, false)
	require.NoError(t, err)
	require.Equal(t, "/api/v1/services/embeddings/text-embedding/text-embedding", u.lastReq.URL.Path)
	require.Equal(t, "Bearer sk-sp-test", u.lastReq.Header.Get("Authorization"))
	require.Equal(t, "text-embedding-v4", gjson.GetBytes(u.lastBody, "model").String())
	require.Equal(t, 27, result.Usage.InputTokens)
	require.Equal(t, payload, w.Body.String())
}

func TestQianwenNativeEmbeddingValidationRejections(t *testing.T) {
	for _, tc := range []struct{ name, model, body string }{
		{"retired v1", "text-embedding-v1", `{"model":"text-embedding-v1","input":{"texts":["a"]}}`},
		{"empty texts", "text-embedding-v4", `{"model":"text-embedding-v4","input":{"texts":[]}}`},
		{"too many texts for v4", "text-embedding-v4", `{"model":"text-embedding-v4","input":{"texts":["1","2","3","4","5","6","7","8","9","10","11"]}}`},
		{"blank text", "text-embedding-v4", `{"model":"text-embedding-v4","input":{"texts":[""]}}`},
		{"dimension 2560 needs qwen3.7", "text-embedding-v4", `{"model":"text-embedding-v4","input":{"texts":["a"]},"parameters":{"dimension":2560}}`},
		{"dimension 2048 needs v4", "qwen3.7-text-embedding", `{"model":"qwen3.7-text-embedding","input":{"texts":["a"]},"parameters":{"dimension":2048}}`},
		{"bad dimension", "text-embedding-v4", `{"model":"text-embedding-v4","input":{"texts":["a"]},"parameters":{"dimension":1000}}`},
		{"instruct needs query type", "text-embedding-v4", `{"model":"text-embedding-v4","input":{"texts":["a"]},"parameters":{"instruct":"find similar"}}`},
		{"bad text_type", "text-embedding-v4", `{"model":"text-embedding-v4","input":{"texts":["a"]},"parameters":{"text_type":"passage"}}`},
		{"unknown parameter", "text-embedding-v4", `{"model":"text-embedding-v4","input":{"texts":["a"]},"parameters":{"task":"retrieval"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u := &httpUpstreamRecorder{resp: qianwenModelPage(`{}`, 200)}
			s := &OpenAIGatewayService{httpUpstream: u}
			c, _ := qianwenNativeResourceTestContext(QianwenNativeEmbeddingPath)
			_, err := s.ForwardQianwenNative(context.Background(), c, qianwenTestAccount(), QianwenNativeEmbedding, []byte(tc.body), false)
			require.Error(t, err)
			require.Empty(t, u.requests)
		})
	}
	// qwen3.7 admits 20 texts and the 2560 dimension.
	require.NoError(t, validateQianwenNativeEmbeddingBody("qwen3.7-text-embedding", []byte(`{"model":"qwen3.7-text-embedding","input":{"texts":["a"]},"parameters":{"dimension":2560,"text_type":"query","instruct":"retrieve"}}`)))
}

func TestQianwenNativeVoiceEnrollmentCreateWireAndBilling(t *testing.T) {
	payload := `{"output":{"voice":"myvoice","target_model":"qwen3-tts-vc-realtime-2026-01-15"},"usage":{"count":1},"request_id":"vc-1"}`
	u := &httpUpstreamRecorder{resp: qianwenModelPage(payload, 200)}
	s := &OpenAIGatewayService{httpUpstream: u}
	a := qianwenTestAccount()
	a.Extra = map[string]any{"qianwen_pricing_sync": map[string]any{"region": "asia-pacific-china", "usd_per_cny": 1.0 / 7, "fx_source": "test operator rate", "fx_effective_at": "2026-09-13T00:00:00Z"}}
	c, w := qianwenNativeResourceTestContext(QianwenNativeVoiceEnrollmentPath)
	body := []byte(`{"model":"qwen-voice-enrollment","input":{"action":"create","target_model":"qwen3-tts-vc-realtime-2026-01-15","preferred_name":"myvoice","audio":{"data":"https://example.com/sample.wav"},"language":"zh"}}`)
	result, err := s.ForwardQianwenNative(context.Background(), c, a, QianwenNativeVoiceEnrollment, body, false)
	require.NoError(t, err)
	require.Equal(t, "/api/v1/services/audio/tts/customization", u.lastReq.URL.Path)
	require.Empty(t, u.lastReq.Header.Get("X-DashScope-Async"))
	require.Equal(t, "create", gjson.GetBytes(u.lastBody, "input.action").String())
	require.NotNil(t, result.AudioUsage)
	require.Equal(t, "voice_clone", result.AudioUsage.Mode)
	require.Equal(t, 1.0, result.AudioUsage.DurationOrUnits)
	require.NotNil(t, result.QianwenVoiceCloneUSDPerClone)
	require.InDelta(t, 0.01/7, *result.QianwenVoiceCloneUSDPerClone, 1e-12)
	cost, err := s.calculateQianwenVoiceEnrollmentCost(result, 2)
	require.NoError(t, err)
	require.Equal(t, QuantizeUsageBillingAmount(0.01/7), cost.TotalCost)
	require.Equal(t, QuantizeUsageBillingAmount(QuantizeUsageBillingAmount(0.01/7)*2), cost.ActualCost)
	require.Equal(t, "per_request", cost.BillingMode)
	require.Equal(t, payload, w.Body.String())
	// Free operations never need a resolved clone price.
	free := &OpenAIForwardResult{UpstreamEndpoint: "/api/v1/services/audio/tts/customization", AudioUsage: &AudioUsage{Mode: "voice_clone"}}
	cost, err = s.calculateQianwenVoiceEnrollmentCost(free, 1)
	require.NoError(t, err)
	require.Zero(t, cost.TotalCost)
	// A created clone without a resolved price is a billing error, never free.
	billed := &OpenAIForwardResult{UpstreamEndpoint: "/api/v1/services/audio/tts/customization", AudioUsage: &AudioUsage{Mode: "voice_clone", DurationOrUnits: 1}}
	_, err = s.calculateQianwenVoiceEnrollmentCost(billed, 1)
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
}

func TestQianwenNativeVoiceEnrollmentRejections(t *testing.T) {
	for _, tc := range []struct{ name, model, body string }{
		{"cosyvoice enrollment unpriced", "voice-enrollment", `{"model":"voice-enrollment","input":{"action":"create_voice","target_model":"cosyvoice-v3-plus","prefix":"mv","url":"https://example.com/a.wav"}}`},
		{"unknown action", "qwen-voice-enrollment", `{"model":"qwen-voice-enrollment","input":{"action":"query_voice","voice":"v"}}`},
		{"bad preferred_name", "qwen-voice-enrollment", `{"model":"qwen-voice-enrollment","input":{"action":"create","target_model":"qwen3-tts-vc-realtime-2026-01-15","preferred_name":"has-dash-","audio":{"data":"https://example.com/a.wav"}}}`},
		{"undocumented target", "qwen-voice-enrollment", `{"model":"qwen-voice-enrollment","input":{"action":"create","target_model":"qwen3-tts-flash","preferred_name":"mv","audio":{"data":"https://example.com/a.wav"}}}`},
		{"missing audio", "qwen-voice-enrollment", `{"model":"qwen-voice-enrollment","input":{"action":"create","target_model":"qwen3-tts-vc-realtime-2026-01-15","preferred_name":"mv"}}`},
		{"delete needs voice", "qwen-voice-enrollment", `{"model":"qwen-voice-enrollment","input":{"action":"delete"}}`},
		{"unknown input field", "qwen-voice-enrollment", `{"model":"qwen-voice-enrollment","input":{"action":"list","prefix":"mv"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u := &httpUpstreamRecorder{resp: qianwenModelPage(`{}`, 200)}
			s := &OpenAIGatewayService{httpUpstream: u}
			c, _ := qianwenNativeResourceTestContext(QianwenNativeVoiceEnrollmentPath)
			_, err := s.ForwardQianwenNative(context.Background(), c, qianwenTestAccount(), QianwenNativeVoiceEnrollment, []byte(tc.body), false)
			require.Error(t, err)
			require.Empty(t, u.requests)
		})
	}
	// Create without operator FX provenance fails before consuming upstream.
	u := &httpUpstreamRecorder{resp: qianwenModelPage(`{"output":{"voice":"v"},"usage":{"count":1}}`, 200)}
	s := &OpenAIGatewayService{httpUpstream: u}
	c, _ := qianwenNativeResourceTestContext(QianwenNativeVoiceEnrollmentPath)
	_, err := s.ForwardQianwenNative(context.Background(), c, qianwenTestAccount(), QianwenNativeVoiceEnrollment, []byte(`{"model":"qwen-voice-enrollment","input":{"action":"create","target_model":"qwen3-tts-vc-realtime-2026-01-15","preferred_name":"mv","audio":{"data":"https://example.com/a.wav"}}}`), false)
	require.ErrorContains(t, err, "conversion")
	require.Empty(t, u.requests)
	// The conversation endpoint never claims a voice enrollment model.
	err = ValidateQianwenNativeOperation(QianwenNativeMultimodal, "qwen-voice-enrollment", []byte(`{"model":"qwen-voice-enrollment","input":{"action":"list"}}`), false)
	require.ErrorContains(t, err, "customization")
}

func TestQianwenNativeResourceUpstreamErrorPassthrough(t *testing.T) {
	u := &httpUpstreamRecorder{resp: qianwenModelPage(`{"code":"InvalidApiKey","message":"Invalid API-key provided.","request_id":"e-1"}`, 401)}
	s := &OpenAIGatewayService{httpUpstream: u}
	c, w := qianwenNativeResourceTestContext(QianwenNativeRerankPath)
	result, err := s.ForwardQianwenNative(context.Background(), c, qianwenTestAccount(), QianwenNativeRerank, []byte(`{"model":"gte-rerank-v2","input":{"query":"q","documents":["a"]}}`), false)
	require.Error(t, err)
	require.Nil(t, result)
	require.Equal(t, 401, w.Code)
}

func TestQianwenNativeResourceRejectsAsyncAndBadUsage(t *testing.T) {
	// An async task response inside the synchronous resource path is a protocol
	// violation and never bills.
	u := &httpUpstreamRecorder{resp: qianwenModelPage(`{"output":{"task_id":"t-1","task_status":"PENDING"}}`, 200)}
	s := &OpenAIGatewayService{httpUpstream: u}
	c, _ := qianwenNativeResourceTestContext(QianwenNativeEmbeddingPath)
	_, err := s.ForwardQianwenNative(context.Background(), c, qianwenTestAccount(), QianwenNativeEmbedding, []byte(`{"model":"text-embedding-v4","input":{"texts":["a"]}}`), false)
	require.ErrorContains(t, err, "async")
	// usage.total_tokens is the only billing evidence; fractional or missing
	// values are rejected instead of estimated.
	for _, payload := range []string{
		`{"output":{"embeddings":[{"embedding":[0.1],"text_index":0}]},"usage":{"total_tokens":1.5}}`,
		`{"output":{"embeddings":[{"embedding":[0.1],"text_index":0}]}}`,
		`{"output":{"results":[{"index":0}]},"usage":{"total_tokens":3}}`,
	} {
		result := &OpenAIForwardResult{}
		if strings.Contains(payload, "results") {
			err = observeQianwenNativeRerank([]byte(payload), result)
		} else {
			err = observeQianwenNativeEmbedding([]byte(payload), result)
		}
		require.Error(t, err, payload)
	}
}
