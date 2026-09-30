//go:build unit

package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Command Code 动作面契约测试。证据锚点是官方 Provider API 参考
// （https://commandcode.ai/docs/provider，2026-09-21 抓取，快照见
// main-work/.pi/actions-commandcode/docs-provider.txt）：端点表、端点与模型匹配规则、
// 错误契约、以及“消息只接受文本与图片”的 FAQ 口径。

// commandCodeHeaderValue 按 HTTP 语义大小写不敏感地取值。
// 请求头覆写对未知名称按小写写入（resolveWireCasing 的既定行为），
// 而 http.Header.Get 是精确 canonical key 查找，会漏掉小写键。
func commandCodeHeaderValue(header http.Header, name string) string {
	for key, values := range header {
		if strings.EqualFold(key, name) && len(values) > 0 {
			return values[0]
		}
	}
	return ""
}

func commandCodeChatTestResponse() *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(
			`{"id":"cc1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`)),
	}
}

func TestCommandCodeDocumentedEndpointTable(t *testing.T) {
	facts := CommandCodeDocumentedEndpoints()
	require.Len(t, facts, 4)
	byPath := map[CommandCodeProviderEndpoint]CommandCodeEndpointFact{}
	for _, fact := range facts {
		require.NotEmpty(t, fact.Method, fact.Endpoint)
		require.NotEmpty(t, fact.Format, fact.Endpoint)
		require.Contains(t, fact.Evidence, "commandcode.ai/docs/provider", fact.Endpoint)
		require.Contains(t, fact.Evidence, "2026-09-21", fact.Endpoint)
		byPath[fact.Endpoint] = fact
	}
	require.Equal(t, http.MethodPost, byPath[CommandCodeEndpointChatCompletions].Method)
	require.Equal(t, http.MethodPost, byPath[CommandCodeEndpointResponses].Method)
	require.Equal(t, http.MethodPost, byPath[CommandCodeEndpointMessages].Method)
	require.Equal(t, http.MethodGet, byPath[CommandCodeEndpointModels].Method)
	// 反例：官方端点表没有图像/视频/音频/向量/重排/检索端点，
	// 因此这些动作格只能记硬冲突，不能靠“未核证”蒙混。
	for _, absent := range []CommandCodeProviderEndpoint{
		"/images/generations", "/images/edits", "/videos", "/audio/speech", "/audio/transcriptions", "/embeddings", "/rerank", "/search",
	} {
		_, documented := byPath[absent]
		require.False(t, documented, "undocumented endpoint advertised: %s", absent)
	}
}

func TestCommandCodeModelEndpointSelection(t *testing.T) {
	for _, tc := range []struct {
		model    string
		endpoint CommandCodeProviderEndpoint
		protocol string
	}{
		{"claude-sonnet-5", CommandCodeEndpointMessages, APIProtocolAnthropic},
		{"ANTHROPIC/Claude-Opus-5", CommandCodeEndpointMessages, APIProtocolAnthropic},
		{"gpt-5.6-sol", CommandCodeEndpointChatCompletions, APIProtocolChatCompletions},
		{"openai/gpt-5.6-luna", CommandCodeEndpointChatCompletions, APIProtocolChatCompletions},
		{"deepseek/deepseek-v4-flash", CommandCodeEndpointChatCompletions, APIProtocolChatCompletions},
		{"Qwen/Qwen3.8-Omni-Flash", CommandCodeEndpointChatCompletions, APIProtocolChatCompletions},
		// 反例：供应商前缀的 claude 不在内置匹配规则内，既不是官方口径的 Anthropic 服务集，
		// 也不是 OpenAI 家族，因此留在 Chat Completions，不得被送进 /messages。
		{"foo/claude-sonnet-5", CommandCodeEndpointChatCompletions, APIProtocolChatCompletions},
	} {
		account := commandCodeTestAccount()
		require.Equal(t, tc.endpoint, CommandCodeModelEndpoint(tc.model), tc.model)
		require.Equal(t, tc.protocol, CommandCodeModelProtocol(tc.model), tc.model)
		require.Equal(t, tc.protocol, account.ResolveCommandCodeUpstreamProtocol(tc.model), tc.model)
	}
}

// Explicit rules reach the upstream even when optional metadata is missing.
func TestCommandCodeResponsesRuleHonorsExplicitChoice(t *testing.T) {
	account := commandCodeTestAccount()
	account.Credentials["protocol_rules"] = []any{map[string]any{"pattern": "*", "protocol": APIProtocolResponses}}
	require.Equal(t, APIProtocolResponses, account.ResolveCommandCodeUpstreamProtocol("claude-private"))
	require.Equal(t, APIProtocolResponses, account.ResolveCommandCodeUpstreamProtocol("private/model"))
	account.Credentials["api_protocol"] = APIProtocolAnthropic
	require.Equal(t, APIProtocolAnthropic, account.ResolveCommandCodeUpstreamProtocol("private/model"))
}

func TestCommandCodeDocumentedErrorContract(t *testing.T) {
	for _, tc := range []struct {
		name                          string
		status                        int
		body                          string
		accountFault, retryable, docd bool
	}{
		{"unsupported model", 400, `{"error":{"type":"invalid_request_error","code":"unsupported_model","message":"unknown model"}}`, false, false, true},
		{"anthropic endpoint mismatch", 400, `{"type":"error","error":{"type":"invalid_request_error","message":"Claude models are served on /messages"}}`, false, false, true},
		{"missing auth", 401, `{"error":{"type":"authentication_error","message":"Missing or invalid auth"}}`, true, false, true},
		{"go plan upgrade", 403, `{"error":{"type":"permission_error","code":"upgrade_required","message":"Upgrade to GOAT"}}`, false, false, true},
		{"plan model", 403, `{"error":{"code":"MODEL_NOT_IN_PLAN","message":"not in plan"}}`, false, false, true},
		// 403 上的凭据证据与 handleCommandCodeError 同一口径：命中即账号级故障。
		{"revoked key on forbidden", 403, `{"error":{"code":"API_KEY_REVOKED"}}`, true, false, true},
		{"invalid key on forbidden", 403, `{"error":{"code":"INVALID_API_KEY"}}`, true, false, true},
		{"plain forbidden", 403, `{"error":{"message":"permission denied"}}`, false, false, true},
		// 反例：403 上的 authentication_error 类型不是凭据证据（该类型的语义绑定 401）。
		{"auth type on forbidden", 403, `{"error":{"type":"authentication_error","message":"forbidden"}}`, false, false, true},
		{"zdr unavailable", 422, `{"error":{"type":"invalid_request_error","code":"cmd_zdr_no_providers","message":"no zdr upstream"}}`, false, false, true},
		{"other unprocessable", 422, `{"error":{"type":"invalid_request_error","message":"bad conditions"}}`, false, false, true},
		{"rate limit", 429, `{"error":{"type":"rate_limit_error","message":"slow down"}}`, false, true, true},
		{"upstream failure", 502, `{"error":{"type":"server_error","message":"upstream failed"}}`, false, true, true},
		{"anthropic api error", 500, `{"type":"error","error":{"type":"api_error","message":"boom"}}`, false, true, true},
		// 反例：不在官方契约里的状态码不得被包装成 Command Code 文档化错误。
		{"undocumented payment", 402, `{"error":{"message":"insufficient balance"}}`, false, false, false},
		{"undocumented teapot", 418, `{"error":{"message":"teapot"}}`, false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			kind, documented := commandCodeProviderError(tc.status, []byte(tc.body))
			require.Equal(t, tc.docd, documented)
			if !tc.docd {
				require.Equal(t, CommandCodeErrorKind{}, kind)
				return
			}
			require.Equal(t, tc.accountFault, kind.AccountFault)
			require.Equal(t, tc.retryable, kind.Retryable)
			require.NotEmpty(t, kind.ClientMessage)
		})
	}
	kind, _ := commandCodeProviderError(http.StatusUnprocessableEntity, []byte(`{"error":{"code":"cmd_zdr_no_providers"}}`))
	require.Equal(t, CommandCodeZDRUnavailableCode, kind.Code)
	require.Contains(t, kind.ClientMessage, CommandCodeZDRHeader)
}

// 文档化错误分类与账号恢复路径必须同口径：只要 handleCommandCodeError 会因为
// 某个响应停调账号，commandCodeProviderError 就不能把同一响应说成“没有凭据故障”。
func TestCommandCodeForbiddenCredentialClassificationMatchesRecovery(t *testing.T) {
	for _, body := range []string{
		`{"error":{"code":"INVALID_API_KEY"}}`,
		`{"error":{"code":"INVALID_KEY"}}`,
		`{"error":{"code":"API_KEY_REVOKED"}}`,
		`{"error":{"code":"KEY_REVOKED"}}`,
		`{"error":{"message":"Invalid API key"}}`,
	} {
		require.True(t, isCommandCodeInvalidCredential(http.StatusForbidden, []byte(body)), body)
		kind, documented := commandCodeProviderError(http.StatusForbidden, []byte(body))
		require.True(t, documented, body)
		require.True(t, kind.AccountFault, body)
		require.NotContains(t, kind.ClientMessage, "without establishing", body)
	}
	// 反例：套餐限制与普通 403 不得被提升成凭据故障，也不得被说成凭据故障。
	for _, body := range []string{
		`{"error":{"code":"MODEL_NOT_IN_PLAN"}}`,
		`{"error":{"code":"upgrade_required"}}`,
		`{"error":{"message":"permission denied"}}`,
		`{"error":{"type":"permission_error","message":"denied"}}`,
	} {
		require.False(t, isCommandCodeInvalidCredential(http.StatusForbidden, []byte(body)), body)
		kind, documented := commandCodeProviderError(http.StatusForbidden, []byte(body))
		require.True(t, documented, body)
		require.False(t, kind.AccountFault, body)
	}
}

// 422 cmd_zdr_no_providers 是请求条件问题（客户端开了 ZDR 而该模型无 ZDR 上游），
// 不是账号故障：默认策略下不得停调账号；操作者显式把 422 写进自定义错误码时，
// 该策略保持原义（本平台不吞掉它）。
func TestCommandCodeZDRUnavailableDoesNotBecomeAccountFault(t *testing.T) {
	body := []byte(`{"error":{"type":"invalid_request_error","code":"cmd_zdr_no_providers","message":"no zdr upstream"}}`)
	account := commandCodeRecoveryAccount()
	account.Credentials["header_override_enabled"] = true
	account.Credentials["header_overrides"] = map[string]any{CommandCodeZDRHeader: "1"}
	repo := &commandCodeRecoveryRepo{}
	gateway := &OpenAIGatewayService{rateLimitService: NewRateLimitService(repo, nil, &config.Config{}, nil, nil)}

	require.False(t, gateway.handleOpenAIAccountUpstreamError(context.Background(), account, http.StatusUnprocessableEntity, http.Header{}, body))
	require.Zero(t, repo.permanent)
	require.True(t, repo.temporaryUntil.IsZero())
	require.True(t, repo.rateUntil.IsZero())
	require.Equal(t, StatusActive, account.Status)
	require.True(t, account.Schedulable)

	account.Credentials["custom_error_codes_enabled"] = true
	account.Credentials["custom_error_codes"] = []any{float64(http.StatusUnprocessableEntity)}
	require.True(t, gateway.handleOpenAIAccountUpstreamError(context.Background(), account, http.StatusUnprocessableEntity, http.Header{}, body))
	require.Equal(t, 1, repo.permanent)
}

// 官方错误契约把 401 的信封类型定为 authentication_error（缺失或无效鉴权）。
// 类型本身即凭据证据，不再依赖 message 是否恰好包含 "invalid api key" 文案。
func TestCommandCodeAuthenticationErrorTypeIsCredentialEvidence(t *testing.T) {
	body := []byte(`{"error":{"type":"authentication_error","message":"Missing or invalid auth"}}`)
	require.True(t, isCommandCodeInvalidCredential(http.StatusUnauthorized, body))

	repo := &commandCodeRecoveryRepo{}
	gateway := &OpenAIGatewayService{rateLimitService: NewRateLimitService(repo, nil, &config.Config{}, nil, nil)}
	require.True(t, gateway.handleOpenAIAccountUpstreamError(context.Background(), commandCodeRecoveryAccount(), http.StatusUnauthorized, http.Header{}, body))
	require.Equal(t, 1, repo.permanent)

	// 反例：非文档化 401（边缘 HTML、无类型 JSON、空体）不得被当成凭据故障。
	for _, negative := range []string{`<html>unauthorized</html>`, `{"error":{"message":"permission denied"}}`, `site owner has blocked access`, ``} {
		require.False(t, isCommandCodeInvalidCredential(http.StatusUnauthorized, []byte(negative)), negative)
	}
	// 反例：同样的类型出现在 403 上不算凭据证据——403 的语义是套餐/权限。
	require.False(t, isCommandCodeInvalidCredential(http.StatusForbidden, body))
}

// HeaderOverride 是 ZDR 模式与中转自定义头的受支持通道：账号级覆写必须真的
// 出现在 Command Code 上游请求上（正向），未配置时不得凭空多发头（反向）。
func TestCommandCodeHeaderOverrideReachesUpstream(t *testing.T) {
	body := []byte(`{"model":"Qwen/Qwen3.7-Max","messages":[{"role":"user","content":"hi"}]}`)
	for _, tc := range []struct {
		name      string
		overrides map[string]any
		headers   map[string]string
	}{
		{"zdr opt-in", map[string]any{CommandCodeZDRHeader: "1"}, map[string]string{CommandCodeZDRHeader: "1"}},
		{"relay identity", map[string]any{"User-Agent": "relay-client", "X-Relay-Tenant": "tenant-a"}, map[string]string{"User-Agent": "relay-client", "X-Relay-Tenant": "tenant-a"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := commandCodeTestAccount()
			account.Credentials[credKeyHeaderOverrideEnabled] = true
			account.Credentials[credKeyHeaderOverrides] = tc.overrides
			upstream := &httpUpstreamRecorder{resp: commandCodeChatTestResponse()}
			svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
			c := adaptiveProtocolTestContext("/v1/chat/completions", body)
			_, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")
			require.NoError(t, err)
			require.Equal(t, DefaultCommandCodeBaseURL+"/chat/completions", upstream.lastReq.URL.String())
			require.Equal(t, "cc-test", upstream.lastReq.Header.Get("x-api-key"))
			for name, value := range tc.headers {
				require.Equal(t, value, commandCodeHeaderValue(upstream.lastReq.Header, name), name)
			}
		})
	}
	t.Run("no override configured", func(t *testing.T) {
		account := commandCodeTestAccount()
		upstream := &httpUpstreamRecorder{resp: commandCodeChatTestResponse()}
		svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
		c := adaptiveProtocolTestContext("/v1/chat/completions", body)
		_, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")
		require.NoError(t, err)
		require.False(t, account.IsHeaderOverrideEnabled())
		require.Empty(t, commandCodeHeaderValue(upstream.lastReq.Header, CommandCodeZDRHeader))
		require.Empty(t, commandCodeHeaderValue(upstream.lastReq.Header, "X-Relay-Tenant"))
		require.Equal(t, "cc-test", upstream.lastReq.Header.Get("x-api-key"))
		require.Equal(t, "Qwen/Qwen3.7-Max", gjson.GetBytes(upstream.lastBody, "model").String())
	})
}

// 客户端取消必须原样传播到上游调用方，既不能变成伪成功，也不能被记成账号故障。
func TestCommandCodeClientCancelPropagatesWithoutAccountFault(t *testing.T) {
	account := commandCodeTestAccount()
	body := []byte(`{"model":"Qwen/Qwen3.7-Max","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	upstream := &httpUpstreamRecorder{err: context.Canceled}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c := adaptiveProtocolTestContext("/v1/chat/completions", body)
	_, err := svc.ForwardAsChatCompletions(ctx, c, account, body, "", "")
	require.Error(t, err)
	require.True(t, errors.Is(err, context.Canceled), "cancel must surface as cancellation, got %v", err)
	require.False(t, c.Writer.Written(), "no response must be committed for a cancelled request")
}

// Command Code 没有任务面：官方端点表全是同步请求/流式（三个 POST 加一个 GET 列表），
// 既没有任务创建/查询/取消端点，也没有任务型媒体动作。因此本平台不存在上游任务
// owner、幂等提交键或任务生命周期——这是“任务/幂等”两类的反向证明，而不是漏做。
func TestCommandCodeHasNoAsyncTaskSurface(t *testing.T) {
	for _, fact := range CommandCodeDocumentedEndpoints() {
		require.NotContains(t, string(fact.Endpoint), "tasks", fact.Endpoint)
		require.NotContains(t, string(fact.Endpoint), "jobs", fact.Endpoint)
	}
	cells := commandCodeActionCellsByAction(t)
	for _, action := range []string{"image.async", "image.batch", "video.generate", "video.edit", "video.extend", "video.status", "video.content"} {
		require.Equal(t, domain.ActionHardConflict, cells[action].Status, action)
	}
}

func TestCommandCodeActionCellsTriState(t *testing.T) {
	cells := CommandCodeActionCells()
	require.Len(t, cells, len(domain.GatewayActions()))
	byAction := map[string]domain.ActionDescriptor{}
	for _, cell := range cells {
		require.Equal(t, PlatformCommandCode, cell.Identity.Platform, cell.Action)
		require.Empty(t, cell.Identity.HostedAccountType, cell.Action)
		require.NotEmpty(t, cell.Evidence, cell.Action)
		require.Contains(t, cell.Evidence, "2026-", cell.Action)
		byAction[cell.Action] = cell
	}
	for _, action := range []string{"chat", "messages", "responses", "count_tokens"} {
		cell := byAction[action]
		require.Equal(t, domain.ActionImplemented, cell.Status, action)
		require.NotEmpty(t, cell.Adapter, action)
		require.NotEmpty(t, cell.CredentialPolicy, action)
		require.NotEmpty(t, cell.ModelPolicy, action)
		require.NotEmpty(t, cell.UsagePolicy, action)
		require.NotEmpty(t, cell.Lifecycle, action)
	}
	for _, action := range []string{
		"responses.compact", "generate_content", "embeddings", "rerank",
		"image.generate", "image.edit", "image.variation", "image.async", "image.batch",
		"video.generate", "video.edit", "video.extend", "video.status", "video.content",
		"audio.tts", "audio.stt", "audio.translate", "voice", "realtime",
		"search", "alpha_search", "live", "responses.websocket", "native",
	} {
		cell := byAction[action]
		require.Equal(t, domain.ActionHardConflict, cell.Status, action)
		require.Empty(t, cell.Adapter, action)
		require.NotEmpty(t, cell.Evidence, action)
	}
	// 反向：本平台不留未核证格；已支持格也不得被写成硬冲突。
	for action, cell := range byAction {
		require.NotEqual(t, domain.ActionUnverified, cell.Status, action)
	}
	require.NotEqual(t, domain.ActionHardConflict, byAction["chat"].Status)
	require.NotEqual(t, domain.ActionImplemented, byAction["image.generate"].Status)
	require.Equal(t, "openai.chat", byAction["chat"].Adapter)
	require.Equal(t, "openai.responses", byAction["responses"].Adapter)
}

// 客户端取消必须在三个入站入口上都原样传播，且不得写成伪成功或账号故障。
// 取消语义由共享传输层提供，本测试证明 Command Code 适配没有把它吞掉。
func TestCommandCodeClientCancelPropagatesAllIngresses(t *testing.T) {
	for _, ingress := range []string{"chat", "messages", "responses"} {
		t.Run(ingress, func(t *testing.T) {
			account := commandCodeTestAccount()
			body := []byte(`{"model":"Qwen/Qwen3.7-Max","max_tokens":32,"stream":true,"messages":[{"role":"user","content":"hi"}]}`)
			if ingress == "responses" {
				body = []byte(`{"model":"Qwen/Qwen3.7-Max","stream":true,"input":"hi"}`)
			}
			upstream := &httpUpstreamRecorder{err: context.Canceled}
			svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			c := adaptiveProtocolTestContext("/v1/"+ingress, body)
			var err error
			switch ingress {
			case "chat":
				_, err = svc.ForwardAsChatCompletions(ctx, c, account, body, "", "")
			case "messages":
				_, err = svc.ForwardAsAnthropic(ctx, c, account, body, "", "")
			case "responses":
				SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
				_, err = svc.Forward(ctx, c, account, body)
			}
			require.Error(t, err)
			require.True(t, errors.Is(err, context.Canceled), "cancel must surface as cancellation, got %v", err)
			require.False(t, c.Writer.Written(), "no response must be committed for a cancelled request")
			var failover *UpstreamFailoverError
			require.NotErrorAs(t, err, &failover)
		})
	}
}

// 同步动作没有“提交幂等键”可写：上游不提供任务创建面，也就没有可重放的提交凭据。
// 可以证明且必须证明的是不重复提交——一次 forward 只向真实上游发起一次请求，
// 同一 body 的两次请求各自提交一次（不存在跨请求缓存默默吞掉一次计费调用）。
func TestCommandCodeSynchronousActionsSubmitExactlyOnce(t *testing.T) {
	for _, ingress := range []string{"chat", "messages", "responses"} {
		t.Run(ingress, func(t *testing.T) {
			account := commandCodeTestAccount()
			body := []byte(`{"model":"Qwen/Qwen3.7-Max","max_tokens":32,"stream":false,"messages":[{"role":"user","content":"hi"}]}`)
			if ingress == "responses" {
				body = []byte(`{"model":"Qwen/Qwen3.7-Max","stream":false,"input":"hi"}`)
			}
			// 三个入站入口在非流式下都把上游的 CC 响应桥回各自协议。
			freshResponse := commandCodeChatTestResponse
			upstream := &httpUpstreamRecorder{responses: []*http.Response{freshResponse(), freshResponse()}}
			svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
			for attempt := 0; attempt < 2; attempt++ {
				c := adaptiveProtocolTestContext("/v1/"+ingress, body)
				var err error
				switch ingress {
				case "chat":
					_, err = svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")
				case "messages":
					_, err = svc.ForwardAsAnthropic(context.Background(), c, commandCodeTestAccount(), body, "", "")
				case "responses":
					SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
					_, err = svc.Forward(context.Background(), c, account, body)
				}
				require.NoError(t, err)
				require.Len(t, upstream.requests, attempt+1, "one forward submits exactly one upstream request")
			}
			// 两次请求都真实打到同一个文档化端点上，没有隐藏缓存短路。
			for _, request := range upstream.requests {
				require.Equal(t, DefaultCommandCodeBaseURL+"/chat/completions", request.URL.String())
			}
		})
	}
}

// Composite 显式目标集合必须能把本平台动作当作有序目标调用：显式路由按端点解析到
// Command Code，账号级 model_mapping 归属也能解析到它，且它仍在调度桶与可复制平台内。
func TestCommandCodeCompositeTargetParity(t *testing.T) {
	for _, endpoint := range []string{CompositeRouteEndpointChatCompletions, CompositeRouteEndpointMessages, CompositeRouteEndpointResponses} {
		t.Run(endpoint, func(t *testing.T) {
			resolver := NewCompositeRouteResolver(compositeRouteRepoStub{routes: []CompositeModelRoute{{GroupID: 7, PublicModel: "commandcode-model", MatchType: CompositeRouteMatchExact, TargetPlatform: PlatformCommandCode, UpstreamModel: "claude-sonnet-5", Endpoint: CompositeRouteEndpointAny, Enabled: true}}})
			decision, err := resolver.Resolve(context.Background(), 7, "commandcode-model", endpoint)
			require.NoError(t, err)
			require.True(t, decision.Matched)
			require.Equal(t, PlatformCommandCode, decision.TargetPlatform)
			require.Equal(t, "claude-sonnet-5", decision.UpstreamModel)

			gateway := &GatewayService{accountRepo: &compositeOwnershipAccountRepo{accounts: []Account{{Platform: PlatformCommandCode, Credentials: map[string]any{"model_mapping": map[string]any{"cc-alias": "gpt-5.6-luna"}}}}}}
			ownership, err := gateway.resolveCompositeModelOwnership(context.Background(), 7, "cc-alias")
			require.NoError(t, err)
			require.True(t, ownership.Matched)
			require.Equal(t, PlatformCommandCode, ownership.TargetPlatform)
		})
	}
	require.Contains(t, schedulerSnapshotPlatforms(), PlatformCommandCode)
	require.True(t, canCopyAccountsFromGroupPlatform(PlatformComposite, PlatformCommandCode))
}

// 价格解析的证据必须落在 catalog 真实存在的免费 ID 上，而不是靠后缀猜测：
// 2026-09-21 定价页与 2026-09-20 catalog（revision ca7ed6d1fd2478fa）都只把
// ling-3.0-flash-sante:free 与 laguna-s-2.1-free 标成免费；付费的 LongCat 2.0
// 不带免费后缀，不得被白名单判成 $0（否则等于白送付费模型）。
func TestCommandCodeFreePricingMatchesCatalogIDs(t *testing.T) {
	for _, model := range []string{"inclusionai/ling-3.0-flash-sante:free", "poolside/laguna-s-2.1-free"} {
		require.True(t, commandCodeOfficialFreeModel(model), model)
	}
	for _, model := range []string{
		"meituan/LongCat-2.0",
		"meituan/LongCat-2.0-preview",
		"poolside/laguna-s-2.1",
		"invented:free",
	} {
		require.False(t, commandCodeOfficialFreeModel(model), model)
	}
	catalog := NewModelRegistry().Snapshot().CandidateIDs(PlatformCommandCode, "")
	if len(catalog) > 0 {
		require.Contains(t, catalog, "inclusionai/ling-3.0-flash-sante:free")
		require.Contains(t, catalog, "poolside/laguna-s-2.1-free")
		require.Contains(t, catalog, "meituan/LongCat-2.0")
		require.NotContains(t, catalog, "meituan/LongCat-2.0:free")
	}
}

func commandCodeActionCellsByAction(t *testing.T) map[string]domain.ActionDescriptor {
	t.Helper()
	byAction := map[string]domain.ActionDescriptor{}
	for _, cell := range CommandCodeActionCells() {
		byAction[cell.Action] = cell
	}
	return byAction
}
