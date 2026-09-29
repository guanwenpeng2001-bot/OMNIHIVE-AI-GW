//go:build unit

package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// 本文件的证据来源是 testdata/opencode_zen_go_action_surface.json：2026-09-21 对
// https://opencode.ai 的匿名实机路由探测（未用凭据、未触发计费）。夹具里的 401
// 是网关自身的鉴权拒绝，404 是站点未找到页（即该路由不存在）。测试不联网、不连库、
// 不连 Redis，也不产生任何计费副作用。

const openCodeSurfaceFixturePath = "testdata/opencode_zen_go_action_surface.json"

func openCodeSurfaceFixture(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(openCodeSurfaceFixturePath)
	require.NoError(t, err)
	return raw
}

func openCodeProbeStatuses(t *testing.T, section string) map[string]int {
	t.Helper()
	statuses := map[string]int{}
	for _, probe := range gjson.GetBytes(openCodeSurfaceFixture(t), section).Array() {
		statuses[probe.Get("label").String()] = int(probe.Get("status").Int())
	}
	require.NotEmpty(t, statuses)
	return statuses
}

func openCodeGoogleTestAccount() *Account {
	return &Account{
		ID:          901,
		Name:        "opencode-zen",
		Platform:    PlatformOpenCodeGo,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":      "sk-opencode",
			"account_mode": AccountModeZen,
			"api_protocol": APIProtocolAdaptive,
			"base_url":     "https://opencode.ai/zen/v1",
		},
	}
}

func openCodeGoogleTestContext(t *testing.T, body []byte) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ginContext, _ := gin.CreateTestContext(recorder)
	ginContext.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-3.8-flash:generateContent", bytes.NewReader(body))
	return ginContext, recorder
}

func TestOpenCodeActionSurfacesCoverEveryGatewayAction(t *testing.T) {
	surfaces := domain.PlatformActionMatrix(domain.ActionIdentity{Platform: PlatformOpenCodeGo})
	require.Len(t, surfaces, len(domain.GatewayActions()))

	seen := map[string]domain.ActionDescriptor{}
	implemented, conflicts, unverified := 0, 0, 0
	for _, cell := range surfaces {
		require.Equal(t, PlatformOpenCodeGo, cell.Identity.Platform)
		require.NotEmpty(t, cell.Evidence, "cell %s has no evidence", cell.Action)
		require.NotContains(t, seen, cell.Action, "duplicate cell %s", cell.Action)
		seen[cell.Action] = cell
		switch cell.Status {
		case domain.ActionImplemented:
			implemented++
			require.NotEmpty(t, cell.Adapter)
			require.NotEmpty(t, cell.CredentialPolicy)
			require.NotEmpty(t, cell.ModelPolicy)
			require.NotEmpty(t, cell.UsagePolicy)
			require.NotEmpty(t, cell.Lifecycle)
		case domain.ActionHardConflict:
			conflicts++
			require.Contains(t, cell.Evidence, openCodeEvidenceDate, "conflict %s must cite the evidence date", cell.Action)
		case domain.ActionUnverified:
			unverified++
			require.Empty(t, cell.Adapter, "unverified cell %s must not advertise an adapter", cell.Action)
			require.Contains(t, cell.Evidence, openCodeEvidenceDate, "unverified cell %s must state what was verified", cell.Action)
		default:
			t.Fatalf("cell %s has no tri-state", cell.Action)
		}
	}
	for _, action := range domain.GatewayActions() {
		require.Contains(t, seen, action, "action %s missing from the OpenCode surface", action)
	}
	require.Equal(t, 5, implemented, "implemented cells: chat, messages, responses, count_tokens, generate_content")
	require.Equal(t, 1, unverified, "System One is verified upstream but has no gateway ingress yet")
	require.Equal(t, len(domain.GatewayActions())-implemented-unverified, conflicts,
		"every other OpenCode cell is a verified absence, not a silent gap")

	// 共享主干 + Google 面 + System One 必须点名适配器与原因。
	for _, action := range []string{"chat", "messages", "responses", "count_tokens", "generate_content"} {
		require.Equal(t, domain.ActionImplemented, seen[action].Status)
		require.NotEmpty(t, seen[action].Adapter)
	}
	require.Equal(t, "opencode.generate_content", seen["generate_content"].Adapter)
	require.Contains(t, seen["generate_content"].Evidence, "x-goog-api-key")
	require.Equal(t, "openai.count_tokens", seen["count_tokens"].Adapter)
	require.Equal(t, domain.ActionHardConflict, seen["embeddings"].Status)
	require.Equal(t, domain.ActionHardConflict, seen["audio.tts"].Status)
	require.Equal(t, domain.ActionHardConflict, seen["responses.websocket"].Status)
	// native 不得虚报：上游 /zen/v1/systemone 存在，但本网关没有对应入口。
	require.Equal(t, domain.ActionUnverified, seen["native"].Status)
	require.Empty(t, seen["native"].Adapter)
	require.Contains(t, seen["native"].Evidence, "/zen/v1/systemone")
}

// 已实现格必须由探测里"路由存在"的样本支撑，硬冲突格必须由"路由不存在"的样本支撑。
// 这条把三态表和保存下来的 wire 样本绑在一起，防止只写 descriptor 不写证据。
func TestOpenCodeSurfaceEvidenceMatchesSavedProbes(t *testing.T) {
	statuses := openCodeProbeStatuses(t, "surfaceProbes")

	for label, want := range map[string]int{
		"chat_zen": 401, "chat_go": 401,
		"messages_zen": 401, "messages_go": 401,
		"responses_zen": 401, "responses_go": 401,
		"generate_content_sync": 401, "generate_content_stream": 401,
		"count_tokens_google_shape": 401,
		"native_systemone_zen":      401, "native_systemone_go": 401,
		"models_list_zen": 200, "models_list_go": 200,
		"quota_usage_go": 401,
	} {
		require.Equal(t, want, statuses[label], "probe %s", label)
	}

	for label, want := range map[string]int{
		"responses_compact": 404,
		"embeddings_zen":    404, "embeddings_go": 404,
		"rerank_zen": 404, "rerank_go": 404,
		"image_generate": 404, "image_edit": 404, "image_variation": 404, "image_batch": 404,
		"video_generate": 404, "video_generate_alt_shape": 404, "video_status": 404,
		"audio_tts": 404, "audio_stt": 404, "audio_translate": 404,
		"voice_customization": 404, "voice_alt_shape": 404,
		"realtime": 404, "search": 404, "alpha_search": 404, "live": 404,
		"files": 404, "moderations": 404, "fine_tuning_jobs": 404,
	} {
		require.Equal(t, want, statuses[label], "probe %s", label)
	}

	// Anthropic 形状的 token 计数不存在，而 Go 侧与 /v1beta 形状都没有 Google 面。
	require.Equal(t, 404, statuses["count_tokens_anthropic_shape"])
	require.Equal(t, 404, statuses["generate_content_go_plan"])
	require.Equal(t, 404, statuses["generate_content_v1beta_shape"])
	// 未知模型在鉴权之前就被拒：说明该路由确实按托管目录校验模型，
	// 而不是把任何模型名都当合法（底层厂商目录不是托管授权）。
	unknownModel := gjson.GetBytes(openCodeSurfaceFixture(t),
		`surfaceProbes.#(label=="generate_content_unknown_model").bodySample`).String()
	require.Equal(t, "ModelError", gjson.Get(unknownModel, "error.type").String())
	require.Contains(t, gjson.Get(unknownModel, "error.message").String(), "is not supported")
}

// 托管目录就是本平台的授权边界：Zen 的 Google 面只服务托管目录里的那几个 Gemini
// 模型，Go 目录里一个 Gemini 都没有，因此 Go 订阅拿不到该面；底层厂商的全量目录
// （例如 Google 自家的模型集）不是托管授权。夹具是两个已保存的托管目录样本：
// testdata/opencode_zen_models.json（/zen/v1/models）与
// testdata/opencode_go_models.json（/zen/go/v1/models）。
func TestOpenCodeHostedCatalogIsTheOnlyGrantForTheGoogleSurface(t *testing.T) {
	zen := openCodeCatalogIDs(t, "testdata/opencode_zen_models.json")
	goPlan := openCodeCatalogIDs(t, "testdata/opencode_go_models.json")
	require.Len(t, zen, 74)
	require.Len(t, goPlan, 37)

	googleColumn := []string{
		"gemini-3.8-flash", "gemini-3.7-flash", "gemini-3.6-flash", "gemini-3.5-flash",
		"gemini-3.5-flash-lite", "gemini-3.1-pro", "gemini-3-flash",
	}
	for _, model := range googleColumn {
		require.Contains(t, zen, model, "the Zen catalog serves %s on the Google column", model)
		url, err := buildOpenCodeGoogleActionURL(DefaultOpenCodeZenBaseURL, model, openCodeGoogleGenerateContentAction)
		require.NoError(t, err)
		require.Equal(t, "https://opencode.ai/zen/v1/models/"+model+":generateContent", url)
	}
	require.Zero(t, countModelsWithPrefix(goPlan, "gemini"), "the Go plan catalog carries no Google-column model")

	// 目录外的模型名不是授权：探测把它在鉴权之前就拒了（ModelError），
	// 说明托管侧按自己的目录校验模型，而不是把底层厂商目录当可服务集。
	unknown := gjson.GetBytes(openCodeSurfaceFixture(t),
		`surfaceProbes.#(label=="generate_content_unknown_model").bodySample`).String()
	require.Equal(t, "ModelError", gjson.Get(unknown, "error.type").String())
	require.False(t, strings.Contains(strings.Join(zen, " "), "does-not-exist"))

	// 计划与协议正交：Go 目录里的模型在 Go 端点上没有 Google 形状的路由（站点 404），
	// 同一个模型只存在于 Zen 的 Google 面。
	require.Contains(t, goPlan, "glm-5.3")
	require.Equal(t, 404, openCodeProbeStatuses(t, "surfaceProbes")["generate_content_go_plan"])
}

// 原生用量适配器面：Go 计划有额度端点（/zen/go/v1/usage，探测 401 = 路由存在），
// Zen 是按量计费、没有额度窗口（/zen/v1/usage 探测 404）。所以额度适配器只应
// 对 Go 计划生效，否则会去轮询一个不存在的端点。
func TestOpenCodeUsageSurfaceIsGoPlanOnly(t *testing.T) {
	statuses := openCodeProbeStatuses(t, "surfaceProbes")
	require.Equal(t, 401, statuses["quota_usage_go"])
	require.Equal(t, 404, statuses["quota_usage_zen"])

	goPlan := openCodeGoogleTestAccount()
	goPlan.Credentials["account_mode"] = AccountModeGo
	require.Equal(t, PlatformOpenCodeGo, goPlan.GetCodingPlanProvider())
	require.Empty(t, openCodeGoogleTestAccount().GetCodingPlanProvider(),
		"Zen is pay-as-you-go: it has no quota endpoint to poll")
	require.Equal(t, "https://opencode.ai/zen/go/v1/usage", openCodeGoQuotaURL(DefaultOpenCodeGoBaseURL))
}

// 夹具必须自证来源与取证日：三态表里的证据日期只能来自保存下来的样本，
// 不能在别的文件里另写一个。
func TestOpenCodeSurfaceFixtureIsDatedAndFirstParty(t *testing.T) {
	fixture := openCodeSurfaceFixture(t)
	require.True(t, strings.HasPrefix(gjson.GetBytes(fixture, "retrievedAt").String(), openCodeEvidenceDate),
		"fixture retrievedAt %s must carry the evidence date %s", gjson.GetBytes(fixture, "retrievedAt").String(), openCodeEvidenceDate)
	docs := gjson.GetBytes(fixture, "docs").Array()
	require.Len(t, docs, 2)
	require.Equal(t, "https://opencode.ai/docs/zen/ (page footer: Last updated Sep 21, 2026)", docs[0].String())
	require.Equal(t, "https://opencode.ai/docs/go/ (page footer: Last updated Sep 21, 2026)", docs[1].String())
}

func openCodeCatalogIDs(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "list", gjson.GetBytes(raw, "object").String())
	var ids []string
	for _, entry := range gjson.GetBytes(raw, "data").Array() {
		require.Equal(t, "model", entry.Get("object").String())
		require.NotEmpty(t, entry.Get("id").String())
		require.Positive(t, entry.Get("created").Int())
		ids = append(ids, entry.Get("id").String())
	}
	require.NotEmpty(t, ids)
	return ids
}

func countModelsWithPrefix(models []string, prefix string) int {
	count := 0
	for _, model := range models {
		if strings.HasPrefix(model, prefix) {
			count++
		}
	}
	return count
}

func TestOpenCodeSurfaceWebsocketUpgradesAreAbsent(t *testing.T) {
	for _, probe := range gjson.GetBytes(openCodeSurfaceFixture(t), "websocketUpgradeProbes").Array() {
		require.Equal(t, 404, int(probe.Get("status").Int()), "websocket probe %s", probe.Get("label").String())
		require.Contains(t, probe.Get("upgradeHeaders").Raw, "Upgrade: websocket")
	}
}

// 鉴权方案按协议面分叉，探测里"放对头=Invalid API key / 放错头=Missing API key"
// 就是判据。契约表必须与探测一致，否则 Google 面会被错发 Bearer。
func TestOpenCodeIngressAuthContractsMatchProbes(t *testing.T) {
	messages := map[string]string{}
	for _, probe := range gjson.GetBytes(openCodeSurfaceFixture(t), "authSchemeProbes").Array() {
		messages[probe.Get("label").String()] = gjson.Get(probe.Get("bodySample").String(), "error.message").String()
	}
	require.Equal(t, "Invalid API key.", messages["auth_chat_bearer"])
	require.Equal(t, "Missing API key.", messages["auth_chat_x_api_key"])
	require.Equal(t, "Invalid API key.", messages["auth_messages_x_api_key"])
	require.Equal(t, "Missing API key.", messages["auth_messages_bearer"])
	require.Equal(t, "Invalid API key.", messages["auth_responses_bearer"])
	require.Equal(t, "Missing API key.", messages["auth_responses_x_api_key"])
	require.Equal(t, "Invalid API key.", messages["auth_generate_content_x_goog_api_key"])
	require.Equal(t, "Missing API key.", messages["auth_generate_content_bearer"])
	require.Equal(t, "Missing API key.", messages["auth_generate_content_x_api_key"])
	require.Equal(t, "Missing API key.", messages["auth_generate_content_query_key"])

	contracts := map[string]string{}
	for _, contract := range OpenCodeIngressAuthContracts() {
		require.NotEmpty(t, contract.Evidence)
		contracts[contract.Ingress] = contract.AuthHeader
	}
	require.Equal(t, "Authorization: Bearer", contracts[APIProtocolChatCompletions])
	require.Equal(t, "Authorization: Bearer", contracts[APIProtocolResponses])
	require.Equal(t, openCodeAnthropicAPIKeyHeader, contracts[APIProtocolAnthropic])
	require.Equal(t, openCodeGoogleAPIKeyHeader, contracts["google_generative"])
	require.Equal(t, "Authorization: Bearer", contracts["systemone"])
}

func TestOpenCodeGoogleActionReachesUpstreamShape(t *testing.T) {
	body := []byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"maxOutputTokens":32}}`)
	fixture := `{"candidates":[{"content":{"role":"model","parts":[{"text":"hello"}]}}],"usageMetadata":{"promptTokenCount":11,"candidatesTokenCount":4,"cachedContentTokenCount":3,"totalTokenCount":15},"modelVersion":"gemini-3.8-flash"}`
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": {"application/json"}, "X-Request-Id": {"req-opencode"}},
		Body:       io.NopCloser(strings.NewReader(fixture)),
	}}
	service := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	ginContext, recorder := openCodeGoogleTestContext(t, body)
	ginContext.Request.Header.Set(openCodeSessionHeader, "sess-contract")

	result, err := service.ForwardOpenCodeGoogleAction(context.Background(), ginContext, openCodeGoogleTestAccount(), "generate_content", "gemini-3.8-flash", body)
	require.NoError(t, err)
	require.Equal(t, "https://opencode.ai/zen/v1/models/gemini-3.8-flash:generateContent", upstream.lastReq.URL.String())
	require.Equal(t, "sk-opencode", upstream.lastReq.Header.Get(openCodeGoogleAPIKeyHeader))
	require.Empty(t, upstream.lastReq.Header.Get("Authorization"), "the Google surface rejects Bearer")
	require.Empty(t, upstream.lastReq.Header.Get(openCodeAnthropicAPIKeyHeader))
	require.Equal(t, "sess-contract", upstream.lastReq.Header.Get(openCodeSessionHeader))
	require.Equal(t, "application/json", upstream.lastReq.Header.Get("Content-Type"))
	require.JSONEq(t, string(body), string(upstream.lastBody))
	require.Equal(t, 11, result.Usage.InputTokens)
	require.Equal(t, 4, result.Usage.OutputTokens)
	require.Equal(t, 3, result.Usage.CacheReadInputTokens)
	require.Equal(t, "gemini-3.8-flash", result.UpstreamModel)
	require.Equal(t, "gemini-3.8-flash", result.BillingModel)
	require.Equal(t, "/models/{model}:generateContent", result.UpstreamEndpoint)
	require.Equal(t, "req-opencode", result.RequestID)
	require.False(t, result.Stream)
	require.JSONEq(t, fixture, recorder.Body.String())
	require.Len(t, upstream.requests, 1, "one forward is exactly one upstream call")
}

func TestOpenCodeGoogleActionCountTokensIsNativeAndNonBillable(t *testing.T) {
	body := []byte(`{"contents":[{"parts":[{"text":"hi"}]}]}`)
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"totalTokens":42}`)),
	}}
	service := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	ginContext, recorder := openCodeGoogleTestContext(t, body)

	result, err := service.ForwardOpenCodeGoogleAction(context.Background(), ginContext, openCodeGoogleTestAccount(), "count_tokens", "models/gemini-3.8-flash", body)
	require.NoError(t, err)
	require.Equal(t, "https://opencode.ai/zen/v1/models/gemini-3.8-flash:countTokens", upstream.lastReq.URL.String())
	require.Equal(t, 42, result.Usage.InputTokens)
	require.Zero(t, result.Usage.OutputTokens)
	require.False(t, result.Stream)
	// 入站侧靠这个标记分辨免费估算：Google 路径段里的 :countTokens 按路由会落成
	// generate_content 动作，只按动作判断会把免费计数当成可计费生成。
	require.Equal(t, "/models/{model}:countTokens", result.UpstreamEndpoint)
	// generate_content 动作永远映射到 generateContent，不会隐式落到计费之外的方法。
	method, ok := openCodeGoogleMethodForAction("generate_content")
	require.True(t, ok)
	require.Equal(t, openCodeGoogleGenerateContentAction, method)
	require.NotEqual(t, openCodeGoogleCountTokensAction, method)
	require.JSONEq(t, `{"totalTokens":42}`, recorder.Body.String())
}

func TestOpenCodeGoogleActionStreamsAltSSE(t *testing.T) {
	body := []byte(`{"contents":[{"parts":[{"text":"hi"}]}]}`)
	stream := "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hi\"}]}}],\"usageMetadata\":{\"promptTokenCount\":5,\"candidatesTokenCount\":2}}\n\n" +
		"data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\" there\"}]}}],\"usageMetadata\":{\"promptTokenCount\":5,\"candidatesTokenCount\":4}}\n\n"
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": {"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(stream)),
	}}
	service := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	ginContext, recorder := openCodeGoogleTestContext(t, body)

	result, err := service.ForwardOpenCodeGoogleAction(context.Background(), ginContext, openCodeGoogleTestAccount(), "streamGenerateContent", "gemini-3.8-flash", body)
	require.NoError(t, err)
	require.Equal(t, "https://opencode.ai/zen/v1/models/gemini-3.8-flash:streamGenerateContent?alt=sse", upstream.lastReq.URL.String())
	require.Equal(t, "text/event-stream", upstream.lastReq.Header.Get("Accept"))
	require.True(t, result.Stream)
	require.Equal(t, 4, result.Usage.OutputTokens, "the last frame's usage wins")
	require.Contains(t, recorder.Body.String(), "data: ")
	require.Contains(t, recorder.Body.String(), " there")
}

func TestOpenCodeGoogleActionTruncatedStreamIsAnError(t *testing.T) {
	body := []byte(`{"contents":[{"parts":[{"text":"hi"}]}]}`)
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": {"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader("")),
	}}
	service := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	ginContext, _ := openCodeGoogleTestContext(t, body)

	_, err := service.ForwardOpenCodeGoogleAction(context.Background(), ginContext, openCodeGoogleTestAccount(), "streamGenerateContent", "gemini-3.8-flash", body)
	require.Error(t, err, "a stream with no semantic frame is truncated, not an empty success")
}

// 反例：Google 面只认 Google 用量字段。把 OpenAI 形状的 usage 当成 Google usageMetadata
// 会把 token 数静默读成 0，等于把计费凭证丢掉；宁可不认（known=false），
// 由调用方按“未知用量不得落成零成本成功”处理。
func TestOpenCodeGoogleActionUsageDoesNotCrossReadShapes(t *testing.T) {
	for _, payload := range []string{
		`{"usage":{"prompt_tokens":11,"completion_tokens":4}}`,
		`{"usage":{"input_tokens":11,"output_tokens":4}}`,
		`{"usageMetadata":{"cachedContentTokenCount":3}}`,
		`{"usageMetadata":{"promptTokenCount":0,"candidatesTokenCount":0,"cachedContentTokenCount":7}}`,
	} {
		usage, known := openCodeGoogleActionUsage(openCodeGoogleGenerateContentAction, []byte(payload))
		require.False(t, known, "payload %s must not settle as a known usage", payload)
		require.Zero(t, usage)
	}
	// 已知字段与未知字段并存时，只取 Google 字段。
	usage, known := openCodeGoogleActionUsage(openCodeGoogleGenerateContentAction, []byte(`{"usage":{"prompt_tokens":99},"usageMetadata":{"promptTokenCount":11,"candidatesTokenCount":4}}`))
	require.True(t, known)
	require.Equal(t, 11, usage.InputTokens)
	require.Equal(t, 4, usage.OutputTokens)
	require.Zero(t, usage.CacheReadInputTokens)
}

// 价格解析依赖的用量口径：Google 的 promptTokenCount 含 cachedContentTokenCount，
// 而 OpenAI 形状的结算把 InputTokens 当总输入并自行减去缓存桶
// （service/openai_gateway_usage.go）。这条把适配器输出与结算桶对齐：
// 未减缓存的总输入 + 并列的缓存读 ⇒ 结算后只有非缓存部分按全价，缓存读按缓存价，
// 既不会重复计费也不会少计。
func TestOpenCodeGoogleUsageMatchesOpenAISettlementBuckets(t *testing.T) {
	body := []byte(`{"usageMetadata":{"promptTokenCount":1200,"cachedContentTokenCount":1000,"candidatesTokenCount":30,"thoughtsTokenCount":10}}`)
	usage, known := openCodeGoogleActionUsage(openCodeGoogleGenerateContentAction, body)
	require.True(t, known)
	require.Equal(t, 1200, usage.InputTokens, "promptTokenCount is the total input in the OpenAI shape")
	require.Equal(t, 1000, usage.CacheReadInputTokens)
	require.Equal(t, 40, usage.OutputTokens, "candidates and thoughts are both billed output")

	// 结算公式（openai_gateway_usage.go 的互斥桶拆分）在适配器输出上的结果：
	// 200 个非缓存输入按全价、1000 个缓存读按缓存价、40 个输出按输出价。
	actualInput := usage.InputTokens - usage.CacheReadInputTokens - usage.CacheCreationInputTokens
	require.Equal(t, 200, actualInput)
	require.Zero(t, usage.CacheCreationInputTokens, "Google reports no cache-write bucket")
	require.Equal(t, 1200, actualInput+usage.CacheReadInputTokens, "input buckets add back up to the upstream total")
}

// 客户端在流中断开：不再继续读上游，但已经观察到的用量与已写出的流头必须保留，
// 且不得把断线误报成上游截断错误（否则会被上层当成可重试的上游故障）。
func TestOpenCodeGoogleActionClientDisconnectKeepsObservedUsage(t *testing.T) {
	body := []byte(`{"contents":[{"parts":[{"text":"hi"}]}]}`)
	stream := "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hi\"}]}}],\"usageMetadata\":{\"promptTokenCount\":5,\"candidatesTokenCount\":2}}\n\n" +
		"data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\" there\"}]}}],\"usageMetadata\":{\"promptTokenCount\":5,\"candidatesTokenCount\":9}}\n\n"
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": {"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(stream)),
	}}
	service := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	ginContext, recorder := openCodeGoogleTestContext(t, body)
	ginContext.Writer = &openCodeDisconnectingStreamWriter{ResponseWriter: ginContext.Writer, writableFrames: 1}

	result, err := service.ForwardOpenCodeGoogleAction(context.Background(), ginContext, openCodeGoogleTestAccount(), "streamGenerateContent", "gemini-3.8-flash", body)
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled, "client disconnect is a cancellation, not an upstream stream failure")
	require.NotNil(t, result)
	require.True(t, result.Stream)
	// 触发断线的那一帧已经被观察到（写失败发生在采集之后），因此按已观察用量结算；
	// 断线之后的帧不再读取。
	require.Equal(t, 9, result.Usage.OutputTokens, "observed usage survives the disconnect")
	require.Equal(t, 5, result.Usage.InputTokens)
	require.Contains(t, recorder.Body.String(), "data: ")
	require.NotContains(t, recorder.Body.String(), " there", "the frames after the disconnect are not written to the gone client")
}

// openCodeDisconnectingStreamWriter 模拟客户端在写到第 writableFrames 帧后断开：
// 第一次写成功，后续写入返回错误（真实链路里是连接被重置）。
type openCodeDisconnectingStreamWriter struct {
	gin.ResponseWriter
	writableFrames int
}

func (w *openCodeDisconnectingStreamWriter) WriteString(payload string) (int, error) {
	if w.writableFrames <= 0 {
		return 0, errors.New("client disconnected")
	}
	w.writableFrames--
	return w.ResponseWriter.WriteString(payload)
}

func TestOpenCodeGoogleActionRejectsBeforeUpstream(t *testing.T) {
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"totalTokens":1}`)),
	}}
	service := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}

	goPlan := openCodeGoogleTestAccount()
	goPlan.Credentials["account_mode"] = AccountModeGo
	oauthAccount := openCodeGoogleTestAccount()
	oauthAccount.Type = AccountTypeOAuth
	foreign := &Account{ID: 902, Platform: PlatformGemini, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "sk-gemini"}}
	body := []byte(`{"contents":[{"parts":[{"text":"hi"}]}]}`)

	for _, testCase := range []struct {
		name    string
		account *Account
		action  string
		model   string
	}{
		{name: "no account", account: nil, action: "generate_content", model: "gemini-3.8-flash"},
		{name: "foreign platform account cannot borrow the credential", account: foreign, action: "generate_content", model: "gemini-3.8-flash"},
		{name: "oauth credential has no google surface", account: oauthAccount, action: "generate_content", model: "gemini-3.8-flash"},
		{name: "go plan publishes no google route", account: goPlan, action: "generate_content", model: "gemini-3.8-flash"},
		{name: "action outside the google surface", account: openCodeGoogleTestAccount(), action: "image.generate", model: "gemini-3.8-flash"},
		{name: "model path traversal", account: openCodeGoogleTestAccount(), action: "generate_content", model: "../glm-5.3"},
		{name: "blank model", account: openCodeGoogleTestAccount(), action: "generate_content", model: "   "},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			ginContext, _ := openCodeGoogleTestContext(t, body)
			_, err := service.ForwardOpenCodeGoogleAction(context.Background(), ginContext, testCase.account, testCase.action, testCase.model, body)
			require.Error(t, err)
		})
	}
	require.Empty(t, upstream.requests, "a rejected request must never reach the upstream")
}

func TestOpenCodeGoogleActionRejectsInvalidModelSegment(t *testing.T) {
	for _, model := range []string{"../glm-5.3", "models/../../etc", "gem ini", "gemini/3.8", ".."} {
		_, err := buildOpenCodeGoogleActionURL("https://opencode.ai/zen/v1", model, openCodeGoogleGenerateContentAction)
		require.Error(t, err, "model %q must not become a URL path", model)
	}
	url, err := buildOpenCodeGoogleActionURL("https://opencode.ai/zen/v1/", "models/gemini-3.8-flash", openCodeGoogleGenerateContentAction)
	require.NoError(t, err)
	require.Equal(t, "https://opencode.ai/zen/v1/models/gemini-3.8-flash:generateContent", url)
}

func TestOpenCodeGoogleActionPreCanceledRequestIsNeverSent(t *testing.T) {
	upstream := &httpUpstreamRecorder{err: context.Canceled}
	service := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	body := []byte(`{"contents":[{"parts":[{"text":"hi"}]}]}`)
	ginContext, _ := openCodeGoogleTestContext(t, body)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ginContext.Request = ginContext.Request.WithContext(ctx)

	_, err := service.ForwardOpenCodeGoogleAction(ctx, ginContext, openCodeGoogleTestAccount(), "generate_content", "gemini-3.8-flash", body)
	require.Error(t, err)
	require.Nil(t, upstream.lastReq, "an already canceled request must not be sent")
}

// 同一会话内的重试必须复用同一个 x-opencode-session：上游按它做路由与 prompt cache，
// 每轮换一个新 UUID 会击穿缓存，也让"重试"变成两条不同的上游请求。
func TestOpenCodeGoogleActionRetryReusesSession(t *testing.T) {
	body := []byte(`{"contents":[{"parts":[{"text":"hi"}]}]}`)
	fixture := `{"candidates":[{"content":{"parts":[{"text":"hi"}]}}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1}}`
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(fixture))},
		{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(fixture))},
	}}
	service := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}

	var sessions []string
	for attempt := 0; attempt < 2; attempt++ {
		ginContext, _ := openCodeGoogleTestContext(t, body)
		ginContext.Request.Header.Set(openCodeSessionHeader, "sess-stable")
		_, err := service.ForwardOpenCodeGoogleAction(context.Background(), ginContext, openCodeGoogleTestAccount(), "generate_content", "gemini-3.8-flash", body)
		require.NoError(t, err)
		sessions = append(sessions, upstream.lastReq.Header.Get(openCodeSessionHeader))
	}
	require.Len(t, upstream.requests, 2)
	require.Equal(t, []string{"sess-stable", "sess-stable"}, sessions)
}

// 未知用量绝不允许落成"零成本成功"：非 countTokens 的 200 缺少 usageMetadata 时，
// 客户端拿到 502，而不是一个 Usage 全零的 200。
func TestOpenCodeGoogleActionUnknownUsageNeverSettlesZero(t *testing.T) {
	for _, payload := range []string{`{}`, `{"usageMetadata":{}}`, `{"candidates":[{"content":{"parts":[{"text":"hi"}]}}]}`} {
		upstream := &httpUpstreamRecorder{resp: &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       io.NopCloser(strings.NewReader(payload)),
		}}
		service := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
		body := []byte(`{"contents":[{"parts":[{"text":"hi"}]}]}`)
		ginContext, recorder := openCodeGoogleTestContext(t, body)

		result, err := service.ForwardOpenCodeGoogleAction(context.Background(), ginContext, openCodeGoogleTestAccount(), "generate_content", "gemini-3.8-flash", body)
		require.Error(t, err, "payload %s must not settle as a zero-cost success", payload)
		require.Nil(t, result)
		require.Equal(t, http.StatusBadGateway, recorder.Code)
		require.Equal(t, float64(http.StatusBadGateway), gjson.Get(recorder.Body.String(), "error.code").Float())
		require.NotEmpty(t, gjson.Get(recorder.Body.String(), "error.status").String())
	}

	usage, known := openCodeGoogleActionUsage(openCodeGoogleCountTokensAction, []byte(`{"totalTokens":0}`))
	require.False(t, known, "a zero total is unknown, not a billable zero")
	require.Zero(t, usage)

	usage, known = openCodeGoogleActionUsage(openCodeGoogleGenerateContentAction, []byte(`{"usageMetadata":{"promptTokenCount":7,"candidatesTokenCount":2,"cachedContentTokenCount":3}}`))
	require.True(t, known)
	require.Equal(t, OpenAIUsage{InputTokens: 7, OutputTokens: 2, CacheReadInputTokens: 3}, usage)

	// 只有思考 token 的帧也算已知用量：思考按输出计费，落成未知会把费用丢掉。
	usage, known = openCodeGoogleActionUsage(openCodeGoogleGenerateContentAction, []byte(`{"usageMetadata":{"thoughtsTokenCount":5}}`))
	require.True(t, known)
	require.Equal(t, OpenAIUsage{OutputTokens: 5}, usage)
}

// 上游错误要么按账号策略升级成 failover（保留上游状态码给账号循环），要么以
// Google 封套回写客户端 —— 该面的客户端是 Google 协议客户端，用 OpenAI 形状的
// 错误体会让下游解析不到 code/status。两条路都不允许静默 200：failover 路径尚未
// 选定下一名上游，因此不得向客户端提交任何响应；非 failover 路径必须以错误状态
// 和 Google 封套回写。
//
// 判定顺序很重要：gin 测试 recorder 的默认状态码就是 200，因此必须先区分
// failover 哨兵，再断言“已回写的状态码不为 200”。
func TestOpenCodeGoogleActionUpstreamErrorIsSurfaced(t *testing.T) {
	for _, status := range []int{400, 401, 403, 404, 429, 500} {
		upstream := &httpUpstreamRecorder{resp: &http.Response{
			StatusCode: status,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"type":"error","error":{"type":"AuthError","message":"Invalid API key."}}`)),
		}}
		service := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
		body := []byte(`{"contents":[{"parts":[{"text":"hi"}]}]}`)
		ginContext, recorder := openCodeGoogleTestContext(t, body)

		_, err := service.ForwardOpenCodeGoogleAction(context.Background(), ginContext, openCodeGoogleTestAccount(), "generate_content", "gemini-3.8-flash", body)
		require.Error(t, err, status)
		require.Len(t, upstream.requests, 1, "no blind retry inside the adapter")

		var failoverErr *UpstreamFailoverError
		if errors.As(err, &failoverErr) {
			require.Equal(t, status, failoverErr.StatusCode, "failover keeps the upstream status for the account policy loop")
			require.False(t, IsResponseCommitted(ginContext), "未选到下一名上游前不得向客户端提交任何响应")
			require.Empty(t, recorder.Body.String(), "failover must not write a body the next attempt cannot replace")
			continue
		}
		require.Equal(t, status, recorder.Code, "a non-failover upstream failure is written with its own status")
		require.Equal(t, "Invalid API key.", gjson.Get(recorder.Body.String(), "error.message").String())
		require.NotEmpty(t, gjson.Get(recorder.Body.String(), "error.status").String(), "google envelope carries status")
		require.Equal(t, float64(status), gjson.Get(recorder.Body.String(), "error.code").Float())
	}
}

// 已实现格必须真正可达已注册 adapter：dispatch 门在平台×动作上选中 OpenCode 自己的
// 适配器名，并把请求交给对应 handler（本地截图证明，不做上游调用）；已核证缺失的格
// 与尚未接线的 native 必须在门前就被拒绝，且零 handler、零上游调用。
func TestOpenCodeDispatchSelectsImplementedAdaptersAndRejectsVerifiedAbsences(t *testing.T) {
	gin.SetMode(gin.TestMode)
	calls := map[string]int{}
	callback := func(adapter string) gin.HandlerFunc {
		return func(c *gin.Context) {
			calls[adapter]++
			c.Status(http.StatusNoContent)
		}
	}
	dispatch := NewGatewayActionDispatch(GatewayActionHandlers{
		"openai.chat":               callback("openai.chat"),
		"openai.messages":           callback("openai.messages"),
		"openai.responses":          callback("openai.responses"),
		"openai.count_tokens":       callback("openai.count_tokens"),
		"opencode.generate_content": callback("opencode.generate_content"),
	})
	router := gin.New()
	router.Use(func(c *gin.Context) {
		// 组上的 allow_image_generation 只控制暴露；即使打开，OpenCode 也不因此
		// 获得图片 adapter（已核证缺失格保持硬冲突）。
		c.Set("api_key", &APIKey{Group: &Group{Platform: PlatformOpenCodeGo, AllowImageGeneration: true}})
	}, GatewayActionRequirement)
	for path, action := range map[string]string{
		"/v1/chat/completions":        "chat",
		"/v1/messages":                "messages",
		"/v1/responses":               "responses",
		"/v1/messages/count_tokens":   "count_tokens",
		"/v1beta/models/:modelAction": "generate_content",
	} {
		router.POST(path, dispatch.Handler(action))
	}

	implemented := []struct{ path, adapter string }{
		{"/v1/chat/completions", "openai.chat"},
		{"/v1/messages", "openai.messages"},
		{"/v1/responses", "openai.responses"},
		{"/v1/messages/count_tokens", "openai.count_tokens"},
		{"/v1beta/models/gemini-3.8-flash:generateContent", "opencode.generate_content"},
	}
	for _, cell := range implemented {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, cell.path, strings.NewReader(`{"model":"glm-5.3"}`)))
		require.Equal(t, http.StatusNoContent, recorder.Code, "%s must reach %s: %s", cell.path, cell.adapter, recorder.Body.String())
		require.Equal(t, 1, calls[cell.adapter], "%s must select %s", cell.path, cell.adapter)
	}
	require.Len(t, calls, len(implemented), "no other adapter may serve an OpenCode request")

	// 已核证缺失的格与未接线的 native：本地拒绝，不触达任何 adapter/上游。
	rejected := []struct{ method, path, action string }{
		{http.MethodPost, "/v1/images/generations", "image.generate"},
		{http.MethodPost, "/v1/embeddings", "embeddings"},
		{http.MethodPost, "/v1/audio/speech", "audio.tts"},
		{http.MethodPost, "/v1/videos", "video.generate"},
		{http.MethodGet, "/v1/realtime", "realtime"},
		{http.MethodPost, "/api/v1/systemone", "native"},
	}
	for _, cell := range rejected {
		router.Handle(cell.method, cell.path, dispatch.Handler(cell.action))
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(cell.method, cell.path, strings.NewReader(`{"model":"glm-5.3"}`)))
		require.Equal(t, http.StatusBadRequest, recorder.Code, "%s (%s) must be rejected locally", cell.path, cell.action)
		require.Contains(t, recorder.Body.String(), "action_not_implemented")
	}
	require.Len(t, calls, len(implemented), "a rejected action must never reach an adapter")
}

// 反例：OpenCode 自己的 Google 入口不能被别的平台继承。
func TestOpenCodeOwnedAdapterIsNotReachableFromAnotherPlatform(t *testing.T) {
	gin.SetMode(gin.TestMode)
	calls := 0
	dispatch := NewGatewayActionDispatch(GatewayActionHandlers{
		"opencode.generate_content": func(c *gin.Context) { calls++; c.Status(http.StatusNoContent) },
	})
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("api_key", &APIKey{Group: &Group{Platform: PlatformMistral}})
	}, GatewayActionRequirement)
	router.POST("/v1beta/models/:modelAction", dispatch.Handler("generate_content"))

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-3.8-flash:generateContent", strings.NewReader(`{}`)))
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), "action_not_implemented")
	require.Zero(t, calls, "another platform must not borrow the OpenCode Google ingress")
}

// Composite 组把已解析目标写进请求上下文后，OpenCode 动作按该目标解析 adapter——
// 跨平台回退必须与直接选 OpenCode 账号走同一条动作解析；未解析的 Composite 不得在
// 本地门被当成 OpenCode（否则会拿错平台的 adapter 或凭空放行）。
func TestOpenCodeActionResolvesOnlyForAResolvedCompositeTarget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	calls := 0
	dispatch := NewGatewayActionDispatch(GatewayActionHandlers{
		"opencode.generate_content": func(c *gin.Context) { calls++; c.Status(http.StatusNoContent) },
	})
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("api_key", &APIKey{Group: &Group{Platform: PlatformComposite}})
	}, GatewayActionRequirement)
	router.POST("/v1beta/models/:modelAction", dispatch.Handler("generate_content"))
	body := strings.NewReader(`{"contents":[]}`)

	unresolved := httptest.NewRecorder()
	router.ServeHTTP(unresolved, httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-3.8-flash:generateContent", body))
	require.Equal(t, http.StatusBadRequest, unresolved.Code, "a composite group without a resolved target cannot be admitted as opencode")
	require.Zero(t, calls)

	resolvedRequest := httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-3.8-flash:generateContent", strings.NewReader(`{"contents":[]}`))
	resolvedRequest = resolvedRequest.WithContext(WithResolvedTargetPlatform(resolvedRequest.Context(), PlatformOpenCodeGo))
	resolved := httptest.NewRecorder()
	router.ServeHTTP(resolved, resolvedRequest)
	require.Equal(t, http.StatusNoContent, resolved.Code, "a composite target resolved to opencode must use the OpenCode adapter")
	require.Equal(t, 1, calls)
}
