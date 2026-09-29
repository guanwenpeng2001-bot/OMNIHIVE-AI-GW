//go:build unit

package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// ---------------------------------------------------------------------------
// DeepSeek 全动作矩阵核证测试（actions-deepseek 交付）。
// 证据锚点见 deepseek_actions.go 头注：官方 Models & Pricing 页快照
// （2026-09-20 22:50 UTC 保存）、官方文档整站导航（同快照）、
// count_tokens 2026-08 核证注释、2026-09-10 官方降价公告。
// 支持格必须有正反例：正例证明可达真实上游形状（URL/body/header/model），
// 反例证明错误路径正确；未核证格在 dispatch 门拒绝且零上游调用。
// ---------------------------------------------------------------------------

func deepSeekActionContext(path string, body []byte) (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c, recorder
}

// DeepSeek 原生 CC usage 形状：prompt_tokens 为总输入（含缓存命中），
// prompt_cache_hit_tokens / prompt_cache_miss_tokens 拆分缓存命中/未命中
// （官方 Context Caching；计费据此分档计价，证据 2026-09-20 价页快照）。
const deepSeekNativeUsageJSON = `{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120,"prompt_cache_hit_tokens":80,"prompt_cache_miss_tokens":20}`

// --- 正例：chat（openai.chat 主干，官方 OpenAI 格式端点）---

func TestDeepSeekActionChatOfficialWire(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"deepseek-flash","messages":[{"role":"user","content":"hello"}],"stream":false}`)
	upstream := &httpUpstreamRecorder{err: errors.New("stop after capture")}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := officialDeepSeekTestAccount(9501)
	c, _ := deepSeekActionContext("/v1/chat/completions", body)

	_, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")
	require.Error(t, err)
	require.NotNil(t, upstream.lastReq)
	require.Equal(t, "https://api.deepseek.com/v1/chat/completions", upstream.lastReq.URL.String())
	require.Equal(t, "Bearer sk-test", upstream.lastReq.Header.Get("Authorization"))
	require.Equal(t, "deepseek-flash", gjson.GetBytes(upstream.lastBody, "model").String())
	require.True(t, gjson.GetBytes(upstream.lastBody, "messages").IsArray())
	require.Equal(t, "hello", gjson.GetBytes(upstream.lastBody, "messages.0.content").String())
}

// --- 正例：responses（openai.responses 主干，官方原生 /responses，无 /v1 前缀）---

func TestDeepSeekActionResponsesOfficialWire(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"deepseek-flash","input":"hello","max_output_tokens":32,"store":true,"previous_response_id":"resp_old","stream":false}`)
	upstream := &httpUpstreamRecorder{err: errors.New("stop after capture")}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := officialDeepSeekTestAccount(9502)
	account.Credentials["api_protocol"] = APIProtocolResponses
	c, _ := deepSeekActionContext("/v1/responses", body)

	_, err := svc.Forward(context.Background(), c, account, body)
	require.Error(t, err)
	require.NotNil(t, upstream.lastReq)
	// 官方口径：POST /responses（无 v1 前缀），证据见 deepseek_actions.go 头注。
	require.Equal(t, "https://api.deepseek.com/responses", upstream.lastReq.URL.String())
	require.Equal(t, "Bearer sk-test", upstream.lastReq.Header.Get("Authorization"))
	require.Equal(t, "hello", gjson.GetBytes(upstream.lastBody, "input").String())
	require.Equal(t, "deepseek-flash", gjson.GetBytes(upstream.lastBody, "model").String())
	require.Equal(t, int64(32), gjson.GetBytes(upstream.lastBody, "max_output_tokens").Int())
	// 无状态上游：store/previous_response_id 必须被剥离。
	require.False(t, gjson.GetBytes(upstream.lastBody, "store").Bool())
	require.False(t, gjson.GetBytes(upstream.lastBody, "previous_response_id").Exists())
}

// --- 正例：messages（openai.messages 主干，官方 Anthropic 格式端点零转换直通）---

func TestDeepSeekActionMessagesOfficialWire(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"deepseek-flash","max_tokens":32,"messages":[{"role":"user","content":"hello"},{"role":"assistant","content":[{"type":"server_tool_use","id":"srvtoolu_1","name":"web_search","input":{"query":"x"}},{"type":"web_search_tool_result","tool_use_id":"srvtoolu_1","content":[{"type":"web_search_result","title":"t","url":"https://example.test"}]},{"type":"text","text":"done"}]}],"stream":false}`)
	upstream := &httpUpstreamRecorder{err: errors.New("stop after capture")}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := officialDeepSeekTestAccount(9503)
	// anthropic 协议账号的 base_url 即 Anthropic 端点基址（官方：BASE URL
	// (Anthropic Format) = https://api.deepseek.com/anthropic，证据见头注）。
	account.Credentials["api_protocol"] = APIProtocolAnthropic
	account.Credentials["base_url"] = "https://api.deepseek.com/anthropic"
	c, _ := deepSeekActionContext("/v1/messages", body)

	_, err := svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "")
	require.Error(t, err)
	require.NotNil(t, upstream.lastReq)
	// 官方 Anthropic 格式 Base：https://api.deepseek.com/anthropic（证据同头注）。
	require.Equal(t, "https://api.deepseek.com/anthropic/v1/messages", upstream.lastReq.URL.String())
	// DeepSeek 原生 Anthropic 端点默认 x-api-key 鉴权（anthropic_apikey_auth.go）。
	require.Equal(t, "sk-test", upstream.lastReq.Header.Get("x-api-key"))
	require.Empty(t, upstream.lastReq.Header.Get("Authorization"))
	require.Equal(t, "deepseek-flash", gjson.GetBytes(upstream.lastBody, "model").String())
	require.Equal(t, "hello", gjson.GetBytes(upstream.lastBody, "messages.0.content").String())
	// web-search 历史块必须剥离：passback-required 上游对 server_tool_use 400。
	require.Equal(t, int64(1), gjson.GetBytes(upstream.lastBody, "messages.1.content.#").Int())
	require.Equal(t, "text", gjson.GetBytes(upstream.lastBody, "messages.1.content.0.type").String())
	require.Equal(t, "done", gjson.GetBytes(upstream.lastBody, "messages.1.content.0.text").String())
}

// --- 正例：count_tokens（本地 tiktoken 估算桥；官方无此端点，2026-08 核证）---

func TestDeepSeekActionCountTokensLocalBridge(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"deepseek-flash","max_tokens":32,"messages":[{"role":"user","content":"hello"}]}`)
	upstream := &httpUpstreamRecorder{err: errors.New("upstream must not be called")}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := officialDeepSeekTestAccount(9504)
	account.Credentials["api_protocol"] = APIProtocolAnthropic
	c, recorder := deepSeekActionContext("/v1/messages/count_tokens", body)

	require.NoError(t, svc.ForwardCountTokensAsAnthropic(context.Background(), c, account, body, ""))
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Greater(t, gjson.Get(recorder.Body.String(), "input_tokens").Int(), int64(0))
	require.Empty(t, upstream.requests, "CN count_tokens 永不转发上游（DeepSeek anthropic 文档无此端点）")
}

// --- 正例：usage 解析（CC 缓冲/流式两路径均识别 DeepSeek 原生缓存别名）---

func TestDeepSeekChatUsageParsesNativeCacheAliases(t *testing.T) {
	buffered, ok := extractOpenAIUsageFromJSONBytes([]byte(`{"id":"chatcmpl-ds","object":"chat.completion","model":"deepseek-flash","choices":[],"usage":` + deepSeekNativeUsageJSON + `}`))
	require.True(t, ok)
	require.Equal(t, 100, buffered.InputTokens)
	require.Equal(t, 20, buffered.OutputTokens)
	require.Equal(t, 80, buffered.CacheReadInputTokens)

	streamed := extractCCStreamUsage(`{"id":"chatcmpl-ds","object":"chat.completion.chunk","model":"deepseek-flash","choices":[],"usage":` + deepSeekNativeUsageJSON + `}`)
	require.NotNil(t, streamed)
	require.Equal(t, buffered, *streamed)

	// 计费侧互斥桶拆分（RecordUsage：总输入 − 缓存读 − 缓存写）必须等于官方
	// 未命中量，缓存命中按 cache_read 价另计——缺别名时 100 全按未命中价计费。
	require.Equal(t, 20, buffered.InputTokens-buffered.CacheReadInputTokens-buffered.CacheCreationInputTokens)
}

func TestDeepSeekAnthropicUsageParsesNativeCacheAliases(t *testing.T) {
	usage := &ClaudeUsage{}
	require.True(t, normalizeAnthropicCompatiblePromptUsage(gjson.Parse(deepSeekNativeUsageJSON), usage))
	require.Equal(t, 20, usage.InputTokens, "prompt_cache_miss_tokens 直接给互斥未命中桶")
	require.Equal(t, 80, usage.CacheReadInputTokens)
	require.Equal(t, 0, usage.CacheCreationInputTokens)
}

// --- 正例：完整转发链 usage（缓冲 + 流式），证明别名修复走通真实数据面 ---

func TestDeepSeekChatBufferedForwardParsesNativeCacheUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"deepseek-flash","messages":[{"role":"user","content":"hello"}],"stream":false}`)
	respBody := `{"id":"chatcmpl-ds","object":"chat.completion","created":1790000000,"model":"deepseek-flash","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":` + deepSeekNativeUsageJSON + `}`
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(respBody)),
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := officialDeepSeekTestAccount(9505)
	c, recorder := deepSeekActionContext("/v1/chat/completions", body)

	result, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 100, result.Usage.InputTokens)
	require.Equal(t, 20, result.Usage.OutputTokens)
	require.Equal(t, 80, result.Usage.CacheReadInputTokens)
	require.Equal(t, http.StatusOK, recorder.Code)
}

func TestDeepSeekChatStreamingForwardParsesNativeCacheUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"deepseek-flash","messages":[{"role":"user","content":"hello"}],"stream":true}`)
	sseBody := "data: {\"id\":\"chatcmpl-ds\",\"object\":\"chat.completion.chunk\",\"created\":1790000000,\"model\":\"deepseek-flash\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"ok\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"chatcmpl-ds\",\"object\":\"chat.completion.chunk\",\"created\":1790000000,\"model\":\"deepseek-flash\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":" + deepSeekNativeUsageJSON + "}\n\n" +
		"data: [DONE]\n\n"
	stream := newContextBoundBlockingReadCloser([]byte(sseBody))
	t.Cleanup(stream.forceUnblock)
	cfg := rawChatCompletionsTestConfig()
	cfg.Gateway.StreamKeepaliveInterval = 1
	svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: &contextBoundHTTPUpstream{body: stream}}
	account := officialDeepSeekTestAccount(9506)
	c, _ := deepSeekActionContext("/v1/chat/completions", body)

	result, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 100, result.Usage.InputTokens)
	require.Equal(t, 20, result.Usage.OutputTokens)
	require.Equal(t, 80, result.Usage.CacheReadInputTokens)
}

// --- 反例：上游错误路径（400 客户端可见、401 不盲重试；均只发一次上游请求）---

func TestDeepSeekActionChatUpstreamBadRequestSurfaces(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"deepseek-flash","messages":[{"role":"user","content":"hello"}],"stream":false}`)
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusBadRequest,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"Model Not Exist","type":"invalid_request_error"}}`)),
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := officialDeepSeekTestAccount(9507)
	c, recorder := deepSeekActionContext("/v1/chat/completions", body)

	_, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")
	require.Error(t, err)
	require.Len(t, upstream.requests, 1, "400 不得盲目重发同一凭据")
	require.Equal(t, http.StatusBadRequest, recorder.Code)
}

func TestDeepSeekActionMessagesUpstreamAuthErrorSurfaces(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"deepseek-flash","max_tokens":32,"messages":[{"role":"user","content":"hello"}],"stream":false}`)
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusUnauthorized,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":{"type":"authentication_error","message":"Authentication Fails, Your api key: ****test is invalid"}}`)),
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := officialDeepSeekTestAccount(9508)
	account.Credentials["api_protocol"] = APIProtocolAnthropic
	account.Credentials["base_url"] = "https://api.deepseek.com/anthropic"
	c, _ := deepSeekActionContext("/v1/messages", body)

	_, err := svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "")
	require.Error(t, err)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr, "401 必须产出换号信号，不得当作客户端 4xx 直接回写")
	require.Len(t, upstream.requests, 1, "401 不得盲目重发同一凭据")
	require.False(t, IsResponseCommitted(c), "未选到下一名上游前不得向客户端提交任何响应")
}

// --- 反例：未核证动作格在 dispatch 门拒绝（400 action_not_implemented），零上游调用 ---

func TestDeepSeekUnverifiedActionsRejectedAtDispatch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("api_key", &APIKey{Group: &Group{Platform: PlatformDeepseek}})
	}, GatewayActionRequirement)
	calls := 0
	callback := func(c *gin.Context) {
		calls++
		c.Status(http.StatusNoContent)
	}
	dispatch := NewGatewayActionDispatch(GatewayActionHandlers{
		"openai.chat":         callback,
		"openai.messages":     callback,
		"openai.responses":    callback,
		"openai.count_tokens": callback,
	})
	router.POST("/chat/completions", dispatch.Handler("chat"))
	router.POST("/messages", dispatch.Handler("messages"))
	router.POST("/responses", dispatch.Handler("responses"))
	router.POST("/messages/count_tokens", dispatch.Handler("count_tokens"))
	router.POST("/images/generations", dispatch.Handler("image.generate"))
	router.POST("/images/edits", dispatch.Handler("image.edit"))
	router.POST("/videos", dispatch.Handler("video.generate"))
	router.POST("/audio/speech", dispatch.Handler("audio.tts"))
	router.POST("/audio/transcriptions", dispatch.Handler("audio.stt"))
	router.POST("/embeddings", dispatch.Handler("embeddings"))
	router.POST("/rerank", dispatch.Handler("rerank"))
	router.POST("/realtime", dispatch.Handler("realtime"))
	router.POST("/web_search", dispatch.Handler("search"))

	for _, path := range []string{"/chat/completions", "/messages", "/responses", "/messages/count_tokens"} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"deepseek-flash"}`)))
		require.Equal(t, http.StatusNoContent, recorder.Code, "已实现动作 %s 必须可达已注册 adapter", path)
	}
	require.Equal(t, 4, calls)

	for _, path := range []string{"/images/generations", "/images/edits", "/videos", "/audio/speech", "/audio/transcriptions", "/embeddings", "/rerank", "/realtime", "/web_search"} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"deepseek-flash"}`)))
		require.Equal(t, http.StatusBadRequest, recorder.Code, "未核证动作 %s 必须在 dispatch 门拒绝", path)
		require.Contains(t, recorder.Body.String(), "action_not_implemented")
	}
	require.Equal(t, 4, calls, "被拒绝的动作不得触达任何 adapter/上游")
}

// --- 反例：responses 上游 429 与 count_tokens 畸形 body ---

func TestDeepSeekActionResponsesUpstreamRateLimitSurfaces(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"deepseek-flash","input":"hello","stream":false}`)
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"Rate limit reached","type":"rate_limit_error"}}`)),
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := officialDeepSeekTestAccount(9511)
	account.Credentials["api_protocol"] = APIProtocolResponses
	c, recorder := deepSeekActionContext("/v1/responses", body)

	_, err := svc.Forward(context.Background(), c, account, body)
	require.Error(t, err)
	require.Len(t, upstream.requests, 1, "429 不得在同一凭据上盲目重发")
	var failoverErr *UpstreamFailoverError
	if errors.As(err, &failoverErr) {
		require.False(t, IsResponseCommitted(c), "换号信号未消费前不得向客户端提交响应")
	} else {
		require.GreaterOrEqual(t, recorder.Code, 400, "非换号错误必须以错误状态回写客户端")
	}
}

func TestDeepSeekActionCountTokensRejectsMalformedBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"","messages":"not-an-array"}`)
	upstream := &httpUpstreamRecorder{err: errors.New("upstream must not be called")}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := officialDeepSeekTestAccount(9512)
	c, recorder := deepSeekActionContext("/v1/messages/count_tokens", body)

	err := svc.ForwardCountTokensAsAnthropic(context.Background(), c, account, body, "")
	require.Error(t, err)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Empty(t, upstream.requests, "畸形 body 在本地拒绝，不触达上游")
}

// --- 取消：客户端取消保持传到上游，流式读取随 ctx 释放 ---

func TestDeepSeekChatCompletionsClientCancellationReleasesUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"deepseek-flash","messages":[{"role":"user","content":"hello"}],"stream":true}`)
	stream := newContextBoundBlockingReadCloser(nil)
	t.Cleanup(stream.forceUnblock)
	cfg := rawChatCompletionsTestConfig()
	cfg.Gateway.StreamKeepaliveInterval = 1
	svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: &contextBoundHTTPUpstream{body: stream}}
	account := officialDeepSeekTestAccount(9509)
	c, _ := deepSeekActionContext("/v1/chat/completions", body)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := svc.ForwardAsChatCompletions(ctx, c, account, body, "", "")
		done <- err
	}()
	cancel()
	select {
	case err := <-done:
		require.Error(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("client cancellation did not release upstream")
	}
}

// --- 幂等：失败重试产生字节一致的上游请求体，入站 body 不被改写 ---

func TestDeepSeekResponsesForwardRetryByteIdentical(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"deepseek-flash","input":"hello","max_output_tokens":32,"stream":false}`)
	upstream := &httpUpstreamRecorder{err: errors.New("injected upstream failure")}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := officialDeepSeekTestAccount(9510)
	account.Credentials["api_protocol"] = APIProtocolResponses

	for i := 0; i < 2; i++ {
		c, _ := deepSeekActionContext("/v1/responses", body)
		_, err := svc.Forward(context.Background(), c, account, body)
		require.Error(t, err)
	}
	require.Len(t, upstream.bodies, 2)
	require.Equal(t, upstream.bodies[0], upstream.bodies[1], "重试不得改写/复制上游请求体")
	require.Equal(t, `{"model":"deepseek-flash","input":"hello","max_output_tokens":32,"stream":false}`, string(body), "入站 body 不得被规范化改写")
}

// --- 价格：官方峰谷口径含中国法定节假日低谷（证据 2026-09-20 价页快照）---

func TestDeepseekPeakMultiplierAtChinesePublicHoliday(t *testing.T) {
	for _, tc := range []struct {
		name string
		now  time.Time
		want float64
	}{
		// 节假日工作日的高峰窗口 → 低谷（"excluding Chinese public holidays"）
		{"mid-autumn 2026-09-25 friday 02:30 utc", time.Date(2026, 9, 25, 2, 30, 0, 0, time.UTC), 1.0},
		{"mid-autumn 2026-09-25 friday 07:00 utc", time.Date(2026, 9, 25, 7, 0, 0, 0, time.UTC), 1.0},
		{"national day 2026-10-01 thursday 02:00 utc", time.Date(2026, 10, 1, 2, 0, 0, 0, time.UTC), 1.0},
		{"national holiday 2026-10-05 monday 07:00 utc", time.Date(2026, 10, 5, 7, 0, 0, 0, time.UTC), 1.0},
		{"national holiday 2026-10-07 wednesday 03:00 utc", time.Date(2026, 10, 7, 3, 0, 0, 0, time.UTC), 1.0},
		{"spring festival 2026-02-17 tuesday 03:00 utc", time.Date(2026, 2, 17, 3, 0, 0, 0, time.UTC), 1.0},
		{"spring festival eve 2026-02-16 monday 08:00 utc", time.Date(2026, 2, 16, 8, 0, 0, 0, time.UTC), 1.0},
		{"new year 2026-01-01 thursday 02:00 utc", time.Date(2026, 1, 1, 2, 0, 0, 0, time.UTC), 1.0},
		{"new year 2027-01-01 friday 02:00 utc", time.Date(2027, 1, 1, 2, 0, 0, 0, time.UTC), 1.0},
		// 节假日非高峰窗口仍低谷（非倍增，非新行为）
		{"national day 2026-10-01 thursday 12:00 utc", time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC), 1.0},
		// 对照：假日前后普通工作日高峰窗口仍 2×
		{"friday before mid-autumn 2026-09-18 02:30 utc", time.Date(2026, 9, 18, 2, 30, 0, 0, time.UTC), 2.0},
		{"workday after national holiday 2026-10-08 thursday 02:00 utc", time.Date(2026, 10, 8, 2, 0, 0, 0, time.UTC), 2.0},
		{"workday after spring festival 2026-02-24 tuesday 03:00 utc", time.Date(2026, 2, 24, 3, 0, 0, 0, time.UTC), 2.0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, deepseekPeakMultiplierAt(tc.now))
		})
	}
}

func TestCalculateCostUnified_DeepseekHolidayPeakWindowBillsOffPeak(t *testing.T) {
	bs := newTestBillingService()
	resolver := NewModelPricingResolver(nil, bs)

	tokens := UsageTokens{InputTokens: 1000, OutputTokens: 500, CacheReadTokens: 1000}
	offPeakTotal := 1000*1.5e-7 + 500*6e-7 + 1000*3e-9

	// 2026-10-01（周四，国庆法定假日）02:00 UTC 处于工作日高峰窗口，
	// 官方口径按低谷计费（证据 2026-09-20 价页快照）。
	cost, err := bs.CalculateCostUnified(CostInput{
		Ctx: context.Background(), Model: "deepseek-flash", Tokens: tokens,
		RateMultiplier: 1.0, Resolver: resolver,
		PricingAt: time.Date(2026, 10, 1, 2, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	require.InDelta(t, offPeakTotal, cost.TotalCost, 1e-10)

	// 对照：2026-10-08（周四，节后工作日）同一窗口仍为高峰 2×。
	peak, err := bs.CalculateCostUnified(CostInput{
		Ctx: context.Background(), Model: "deepseek-flash", Tokens: tokens,
		RateMultiplier: 1.0, Resolver: resolver,
		PricingAt: time.Date(2026, 10, 8, 2, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	require.InDelta(t, offPeakTotal*2, peak.TotalCost, 1e-10)
}
