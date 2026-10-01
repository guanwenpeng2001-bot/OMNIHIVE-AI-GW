package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/gatewayerror"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// OpenCode Zen / OpenCode Go 动作面。
//
// 与自建多协议供应商不同，OpenCode 这一侧**网关本身就是平台**：Zen（按量）
// 与 Go（$10/月订阅）各自公开一张固定路由表与一份固定模型目录，托管凭据
// 只能触达表内端点。底层模型厂商的全量目录不是本平台的托管授权。
//
// 证据（2026-09-21 第一方）：
//   - https://opencode.ai/docs/zen/ —— 端点表（GPT/Grok/Muse Spark →
//     /zen/v1/responses，Claude/Qwen → /zen/v1/messages，Gemini → 端点列的
//     /zen/v1/models/{model} Google 协议形态，其余 → /zen/v1/chat/completions）、
//     Jev → /zen/v1/systemone、模型目录 /zen/v1/models、每 1M token 价目表；
//     页面页脚 "Last updated: Sep 21, 2026"。
//   - https://opencode.ai/docs/go/ —— Go 端点表（/zen/go/v1/responses、
//     /zen/go/v1/messages、/zen/go/v1/chat/completions）、模型目录
//     /zen/go/v1/models、5h/weekly/monthly 额度与价目表；页脚同上。
//   - 对 https://opencode.ai 的匿名实机路由探测（未使用任何凭据、未触发计费），
//     逐条状态码与响应体样本保存在 testdata/opencode_zen_go_action_surface.json，
//     托管目录样本保存在 testdata/opencode_zen_models.json 与
//     testdata/opencode_go_models.json。
//
// Go 文档的 ZDR 注记提到 Files、Collections 与 Batch API —— 那是底层厂商被
// 零数据留存关掉的自家能力，不是 OpenCode 提供的路由；探测 /zen/v1/files 与
// /zen/v1/batches 都返回站点 404。判定以托管路由表为准。
//
// 接线边界（本包只提供适配器，不含入口注册）：Google 面要求入站侧把
// /v1beta/models/{model}:{method} 解析成动作后交给 ForwardOpenCodeGoogleAction，
// 并按平台动作表解析 opencode.generate_content。路由注册、平台硬锁移除与
// 账号计划筛选（Go 订阅没有该面）分别属于路由/调度写者；模型候选与端点证据
// 属于模型数据写者。在这些接线落地前，该动作在进程内不可达，但适配器本身的
// 协议、认证、用量与流式语义已由 opencode_actions_test.go 用脱敏上游夹具覆盖。
//
// 用量/额度面：Go 订阅的额度读取走 /zen/go/v1/usage（探测 401，路由存在），
// Zen 按量计费、没有额度窗口（/zen/v1/usage 探测 404），所以现有额度适配器
// （openCodeGoQuotaURL + parseOpenCodeGoUsageTiers）只对 Go 计划生效。
const (
	// openCodeGoogleAPIKeyHeader 是 Google 协议面的凭据头。OpenCode 的三个
	// 原生协议面各用不同的头，不能统一成 Bearer。
	openCodeGoogleAPIKeyHeader    = "x-goog-api-key"
	openCodeAnthropicAPIKeyHeader = "x-api-key"

	openCodeGoogleGenerateContentAction       = "generateContent"
	openCodeGoogleStreamGenerateContentAction = "streamGenerateContent"
	openCodeGoogleCountTokensAction           = "countTokens"

	openCodeSystemOnePath  = "/systemone"
	openCodeGoogleSSEQuery = "alt=sse"

	// openCodeEvidenceDate 是全部动作格证据的取证日。
	openCodeEvidenceDate = "2026-09-21"
)

// OpenCodeIngressAuth 是一条已核证的原生 OpenCode 入口族及其接受的凭据头。
// 同一把 Key 放错头会得到 AuthError/Missing API key，放对头才是
// AuthError/Invalid API key —— 这是探测中区分"路由存在"与"鉴权方案"的判据。
type OpenCodeIngressAuth struct {
	Ingress    string
	Path       string
	AuthHeader string
	Evidence   string
}

// OpenCodeIngressAuthContracts 返回三个原生入口族的鉴权契约。
func OpenCodeIngressAuthContracts() []OpenCodeIngressAuth {
	evidence := "anonymous probe " + openCodeEvidenceDate + ": the same unknown key returns AuthError/Invalid API key in the accepted header and AuthError/Missing API key in the other headers"
	return []OpenCodeIngressAuth{
		{Ingress: APIProtocolChatCompletions, Path: "/zen/v1/chat/completions", AuthHeader: "Authorization: Bearer", Evidence: evidence},
		{Ingress: APIProtocolResponses, Path: "/zen/v1/responses", AuthHeader: "Authorization: Bearer", Evidence: evidence},
		{Ingress: APIProtocolAnthropic, Path: "/zen/v1/messages", AuthHeader: openCodeAnthropicAPIKeyHeader, Evidence: evidence},
		{Ingress: "google_generative", Path: "/zen/v1/models/{model}:{method}", AuthHeader: openCodeGoogleAPIKeyHeader, Evidence: evidence},
		{Ingress: "systemone", Path: openCodeSystemOnePath, AuthHeader: "Authorization: Bearer", Evidence: evidence},
	}
}

// openCodeGoogleMethodForAction 把网关动作映射到已核证的 Google 协议方法。
// 只有这三个方法在 OpenCode 侧有 Google 形态的路由。
func openCodeGoogleMethodForAction(action string) (string, bool) {
	switch strings.TrimSpace(action) {
	case "generate_content", openCodeGoogleGenerateContentAction:
		return openCodeGoogleGenerateContentAction, true
	case openCodeGoogleStreamGenerateContentAction:
		return openCodeGoogleStreamGenerateContentAction, true
	case "count_tokens", openCodeGoogleCountTokensAction:
		return openCodeGoogleCountTokensAction, true
	default:
		return "", false
	}
}

func openCodeGoogleMethodIsStream(method string) bool {
	return method == openCodeGoogleStreamGenerateContentAction
}

// OpenCodeGoogleActionForMethod is the inbound inverse of
// openCodeGoogleMethodForAction: the Google "models/{model}:{method}" ingress
// path segment carries a method, not a gateway action, and :countTokens must not
// be mistaken for generate_content because it is a non-billable estimate.
func OpenCodeGoogleActionForMethod(method string) (action string, stream bool, ok bool) {
	switch strings.TrimSpace(method) {
	case openCodeGoogleGenerateContentAction:
		return "generate_content", false, true
	case openCodeGoogleStreamGenerateContentAction:
		return openCodeGoogleStreamGenerateContentAction, true, true
	case openCodeGoogleCountTokensAction:
		return "count_tokens", false, true
	default:
		return "", false, false
	}
}

// openCodeGoogleActionAccount 把 Google 协议面限定在 OpenCode API Key 账号：
// 托管凭据就是 API Key，本网关没有 OAuth 或上游透传形态。
func openCodeGoogleActionAccount(account *Account) error {
	if account == nil || !account.IsOpenCodeGo() {
		return errors.New("opencode google-protocol action requires an opencode_go account")
	}
	if account.Type != AccountTypeAPIKey {
		return errors.New("opencode google-protocol action requires an API key account")
	}
	return nil
}

// NativeOpenCodeGoogleModel checks the selected account's native Google column,
// not the public Google-to-text conversion capability. ResolveAccountModelBinding
// retains alias, catalog membership and account authorization checks.
func NativeOpenCodeGoogleModel(ctx context.Context, account *Account, model string) bool {
	if openCodeGoogleActionAccount(account) != nil || account.IsOpenCodeGoPlan() || !account.IsAdaptiveAPIProtocol() {
		return false
	}
	binding := ResolveAccountModelBinding(ctx, account, model, AccountModelBindingOptions{Endpoint: "generate_content"})
	if !binding.Supported {
		return false
	}
	if snapshot := account.EffectiveUpstreamModelCatalog(); snapshot != nil {
		for _, member := range snapshot.EligibleModels {
			if member.ID == binding.UpstreamModel && catalogEndpointsContain(member.Endpoints, "generate_content") {
				return true
			}
		}
	}
	return false
}

// openCodeGoogleBaseURL 解析 Google 协议面的 base URL。OpenCode 把该面挂在
// /zen/v1 下（不是 Google 的 /v1beta：探测 /zen/v1beta/models/{model}:generateContent
// 是站点 404，而 /zen/v1/models/{model}:generateContent 是 401 鉴权拒绝），
// 且 Go 侧完全没有这一面（/zen/go/v1/models/{model}:generateContent 为 404），
// 因此 Go 订阅账号在此显式拒绝而不是把 Zen 地址硬套上去。
func (s *OpenAIGatewayService) openCodeGoogleBaseURL(account *Account) (string, error) {
	if account.IsOpenCodeGoPlan() {
		return "", errors.New("opencode go publishes no google-shaped route; use the responses, chat or messages ingress")
	}
	baseURL := strings.TrimSpace(account.GetOpenAIFormatBaseURL())
	if baseURL == "" {
		baseURL = account.openCodeDefaultChatBaseURL()
	}
	validated, err := s.validateUpstreamBaseURL(baseURL)
	if err != nil {
		return "", fmt.Errorf("invalid base_url: %w", err)
	}
	return validated, nil
}

// buildOpenCodeGoogleActionURL 组装 {base}/models/{model}:{method}。
// 模型名必须是一个合法路径段：它直接进入 URL，拼接越界即等于改选上游模型。
func buildOpenCodeGoogleActionURL(baseURL, model, method string) (string, error) {
	trimmedBase := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if trimmedBase == "" {
		return "", errors.New("opencode google-protocol base url is required")
	}
	trimmedModel := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(model), "models/"))
	if trimmedModel == "" {
		return "", errors.New("opencode google-protocol model is required")
	}
	if err := validateUpstreamPathSegment("opencode model", trimmedModel); err != nil {
		return "", err
	}
	switch method {
	case openCodeGoogleGenerateContentAction, openCodeGoogleStreamGenerateContentAction, openCodeGoogleCountTokensAction:
	default:
		return "", fmt.Errorf("unsupported opencode google-protocol method: %s", method)
	}
	return fmt.Sprintf("%s/models/%s:%s", trimmedBase, trimmedModel, method), nil
}

// openCodeGoogleActionUsage 读取 Google 形态的用量字段。OpenCode 的 Google 面
// 回的是 Google 字段，用 chat/responses 主干那套 OpenAI 形状的提取器会静默结算成 0。
//
// 口径按 OpenAIUsage（OpenAI 形状）而不是 ClaudeUsage（Claude 形状）记，
// 两者对总输入的约定相反：
//   - OpenAI 形状里 InputTokens 是**总输入**，结算路径自己拆互斥桶
//     （billing/openai_gateway_usage.go: actualInputTokens = InputTokens -
//     CacheReadInputTokens - CacheCreationInputTokens）。所以这里必须原样上报
//     promptTokenCount，把 cachedContentTokenCount 并列进缓存读桶。
//   - Claude 形状里 input_tokens 不含 cache_read_input_tokens，既有 Gemini 主干
//     （extractGeminiUsage）因此要先减去缓存。若在这里也照减，结算会减第二次，
//     缓存读被少计；反之若只报总输入又不并列缓存，缓存读会按全价重复计费。
//
// thoughtsTokenCount 计入输出：OpenAIUsage 没有独立的 reasoning 桶，而 Google 把
// 思考 token 与候选 token 同为计费输出（既有 Gemini 主干同样把 thought 计入输出）。
// 未知用量返回 known=false，绝不允许落成"零成本成功"。
func openCodeGoogleActionUsage(method string, body []byte) (OpenAIUsage, bool) {
	if method == openCodeGoogleCountTokensAction {
		total := int(gjson.GetBytes(body, "totalTokens").Int())
		if total <= 0 {
			return OpenAIUsage{}, false
		}
		return OpenAIUsage{InputTokens: total}, true
	}
	metadata := gjson.GetBytes(body, "usageMetadata")
	if !metadata.Exists() {
		return OpenAIUsage{}, false
	}
	prompt := int(metadata.Get("promptTokenCount").Int())
	candidates := int(metadata.Get("candidatesTokenCount").Int())
	thoughts := int(metadata.Get("thoughtsTokenCount").Int())
	if prompt == 0 && candidates == 0 && thoughts == 0 {
		return OpenAIUsage{}, false
	}
	return OpenAIUsage{
		InputTokens:          prompt,
		OutputTokens:         candidates + thoughts,
		CacheReadInputTokens: int(metadata.Get("cachedContentTokenCount").Int()),
	}, true
}

// sendOpenCodeActionRequest 是 OpenCode 动作面的发送侧，与 Mistral/Grok 的同名
// helper 同构：分离的上游上下文、OpenAI HTTP profile、客户端头白名单、账号头覆盖
// 与代理路由都和 OpenCode chat 主干一致，避免错误策略在动作之间分叉。凭据头按
// 协议面选择（Google 面 x-goog-api-key），并补上文档要求的 x-opencode-session。
func (s *OpenAIGatewayService) sendOpenCodeActionRequest(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	targetURL string,
	body []byte,
	authHeader string,
	accept string,
	token string,
	endpoint string,
) (*http.Response, error) {
	upstreamCtx, releaseUpstreamCtx := detachUpstreamContext(ctx)
	upstreamReq, err := http.NewRequestWithContext(upstreamCtx, http.MethodPost, targetURL, bytes.NewReader(body))
	releaseUpstreamCtx()
	if err != nil {
		return nil, fmt.Errorf("build upstream request: %w", err)
	}
	SetActualOpenAIUpstreamEndpoint(c, endpoint)
	upstreamReq = upstreamReq.WithContext(WithHTTPUpstreamProfile(upstreamReq.Context(), HTTPUpstreamProfileOpenAI))
	upstreamReq.Header.Set("Content-Type", "application/json")
	upstreamReq.Header.Set(authHeader, token)
	if accept != "" {
		upstreamReq.Header.Set("Accept", accept)
	}
	if c != nil && c.Request != nil {
		for key, values := range c.Request.Header {
			if openaiCCRawAllowedHeaders[strings.ToLower(key)] {
				for _, value := range values {
					upstreamReq.Header.Add(key, value)
				}
			}
		}
	}
	if userAgent := account.GetOpenAIUserAgent(); userAgent != "" {
		upstreamReq.Header.Set("user-agent", userAgent)
	}
	account.ApplyHeaderOverrides(upstreamReq.Header)
	// x-opencode-session 必须在头覆盖之后写入：账号覆盖不应让会话标识丢失，
	// 上游按它做路由与 prompt cache，跨轮次复用同一值才是幂等提交的锚点。
	applyOpenCodeSessionHeader(c, account, targetURL, upstreamReq.Header, body)

	proxyURL, egressErr := account.EgressProxyURL(ctx)
	if egressErr != nil {
		return nil, egressErr
	}
	resp, err := s.doOpenAIUpstream(upstreamReq, proxyURL, account)
	if err != nil {
		return nil, s.handleOpenAIUpstreamTransportError(ctx, c, account, err, false)
	}
	return resp, nil
}

// relayOpenCodeGoogleEventStream 逐帧转发 Google 协议 SSE（alt=sse），同时
// 采集最后一个 usageMetadata。客户端断开即停止读取上游（Disconnected 让读循环
// 立刻结束）并保留已经观察到的用量与已写出的流头，不把断线伪装成上游截断错误；
// 一帧语义内容都没有就结束的流按截断处理，不当成成功的空回答。
func (s *OpenAIGatewayService) relayOpenCodeGoogleEventStream(
	c *gin.Context,
	resp *http.Response,
	method string,
) (usage OpenAIUsage, usageKnown bool, clientAborted bool, err error) {
	writeStreamHeaders := s.newStreamHeaderWriter(c, resp.Header)
	maxSize, idle := defaultMaxLineSize, 300*time.Second
	if s.cfg != nil {
		if s.cfg.Gateway.MaxLineSize > 0 {
			maxSize = s.cfg.Gateway.MaxLineSize
		}
		if s.cfg.Gateway.StreamDataIntervalTimeout > 0 {
			idle = time.Duration(s.cfg.Gateway.StreamDataIntervalTimeout) * time.Second
		}
	}
	scanner := newUpstreamFrameReader(resp.Body, maxSize, c.Request.Context(), idle)
	defer scanner.Close()

	clientDisconnected := false
	clientOutputStarted := false
	sawSemanticFrame := false
	semanticMarked := false
	var frameErr error

	for scanner.Scan() {
		payload := strings.TrimSpace(scanner.frame.Data)
		if payload == "" || payload == "[DONE]" {
			continue
		}
		if !gjson.Valid(payload) {
			frameErr = errors.New("invalid opencode google-protocol stream frame")
			break
		}
		if !semanticMarked {
			scanner.Semantic()
			semanticMarked = true
		}
		sawSemanticFrame = true
		if frameUsage, known := openCodeGoogleActionUsage(method, []byte(payload)); known {
			usage, usageKnown = frameUsage, true
		}
		if !clientDisconnected {
			if !clientOutputStarted {
				writeStreamHeaders()
				clientOutputStarted = true
			}
			if _, writeErr := c.Writer.WriteString("data: " + payload + "\n\n"); writeErr != nil {
				clientDisconnected = true
			} else {
				c.Writer.Flush()
			}
		}
		if clientDisconnected {
			scanner.Disconnected()
		}
	}

	scanErr := scanner.Err()
	if frameErr != nil {
		scanErr = frameErr
	}
	clientAborted = clientDisconnected || c.Request.Context().Err() != nil
	if clientAborted {
		return usage, usageKnown, true, context.Canceled
	}
	if scanErr != nil {
		return usage, usageKnown, false, newOpenAIUpstreamStreamReadError(scanErr)
	}
	if !sawSemanticFrame {
		return usage, usageKnown, false, newOpenAIUpstreamStreamReadError(ErrOpenAIUpstreamStreamTruncated)
	}
	return usage, usageKnown, false, nil
}

// writeOpenCodeGoogleError 以 Google 错误封套回写，因为该面的客户端是
// Google 协议客户端；用 OpenAI 形状的错误体会让下游解析不到 code/status。
func writeOpenCodeGoogleError(c *gin.Context, statusCode int, reason, message string) {
	if c == nil {
		return
	}
	status, envelope := gatewayerror.Envelope("google", statusCode, reason, message)
	c.JSON(status, envelope)
}

// ForwardOpenCodeGoogleAction 执行一次原生 Google 协议 OpenCode Zen 动作。
// 入站侧仍负责授权、账号选择与冻结报价；本方法从不选账号。action 接受网关动作名
// （generate_content / count_tokens）或原生方法名。模型来自入站路径段，与 Google
// 协议一致（该协议不在 body 里带 model）。
//
// 计费边界：只有 generateContent / streamGenerateContent 产生可计费用量。
// :countTokens 是免费估算（totalTokens 只是输入规模），入站侧不得为它记用量——
// 它的 UpstreamEndpoint 标记为 /models/{model}:countTokens，且 OutputTokens 恒为 0。
// 另注意 Google 路径段里的 :countTokens 按路由判定会落成 generate_content 动作，
// 所以这条规则必须由入站侧按 method 判断，不能只按动作判断。
func (s *OpenAIGatewayService) ForwardOpenCodeGoogleAction(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	action string,
	model string,
	body []byte,
) (*OpenAIForwardResult, error) {
	if err := openCodeGoogleActionAccount(account); err != nil {
		return nil, err
	}
	method, ok := openCodeGoogleMethodForAction(action)
	if !ok {
		return nil, fmt.Errorf("opencode has no google-protocol route for action %s", action)
	}
	startTime := time.Now()
	baseURL, err := s.openCodeGoogleBaseURL(account)
	if err != nil {
		return nil, err
	}
	targetURL, err := buildOpenCodeGoogleActionURL(baseURL, model, method)
	if err != nil {
		writeOpenCodeGoogleError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return nil, err
	}
	stream := openCodeGoogleMethodIsStream(method)
	if stream {
		targetURL += "?" + openCodeGoogleSSEQuery
	}
	token, tokenKind, err := s.getRequestCredential(ctx, c, account)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("account %d missing %s credential", account.ID, tokenKind)
	}
	accept := "application/json"
	if stream {
		accept = "text/event-stream"
	}
	endpoint := "/models/{model}:" + method
	resp, err := s.sendOpenCodeActionRequest(ctx, c, account, targetURL, body, openCodeGoogleAPIKeyHeader, accept, token, endpoint)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	upstreamModel := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(model), "models/"))
	if resp.StatusCode >= 400 {
		respBody, upstreamMsg := s.readOpenAIUpstreamError(resp)
		if failoverErr := s.failoverOpenAIUpstreamHTTPError(ctx, c, account, resp, respBody, upstreamMsg, upstreamModel); failoverErr != nil {
			return nil, failoverErr
		}
		message := strings.TrimSpace(upstreamMsg)
		if message == "" {
			message = "OpenCode upstream rejected the request"
		}
		writeOpenCodeGoogleError(c, resp.StatusCode, "upstream_error", message)
		return &OpenAIForwardResult{
			UpstreamHeaders:  resp.Header,
			Model:            upstreamModel,
			BillingModel:     upstreamModel,
			UpstreamModel:    upstreamModel,
			UpstreamEndpoint: endpoint,
			Stream:           stream,
			Duration:         time.Since(startTime),
		}, fmt.Errorf("opencode google-protocol upstream status %d", resp.StatusCode)
	}

	if stream {
		usage, usageKnown, _, relayErr := s.relayOpenCodeGoogleEventStream(c, resp, method)
		result := &OpenAIForwardResult{
			UpstreamHeaders:  resp.Header,
			Usage:            usage,
			Model:            upstreamModel,
			BillingModel:     upstreamModel,
			UpstreamModel:    upstreamModel,
			UpstreamEndpoint: endpoint,
			Stream:           true,
			Duration:         time.Since(startTime),
		}
		if !usageKnown {
			result.Usage = OpenAIUsage{}
		}
		return result, relayErr
	}

	respBody, err := ReadUpstreamResponseBody(resp.Body, s.cfg, c, openAITooLargeError)
	if err != nil {
		return nil, fmt.Errorf("read upstream body: %w", err)
	}
	usage, usageKnown := openCodeGoogleActionUsage(method, respBody)
	if method != openCodeGoogleCountTokensAction && !usageKnown {
		writeOpenCodeGoogleError(c, http.StatusBadGateway, "upstream_error", "OpenCode usage is unknown")
		return nil, errors.New("opencode usage is unknown")
	}
	writeOpenAIEmbeddingsUpstreamResponse(c, resp, respBody, s.responseHeaderFilter)
	return &OpenAIForwardResult{
		RequestID:        firstNonEmptyString(resp.Header.Get("x-request-id"), resp.Header.Get("request-id")),
		UpstreamHeaders:  resp.Header,
		Usage:            usage,
		Model:            upstreamModel,
		BillingModel:     upstreamModel,
		UpstreamModel:    upstreamModel,
		UpstreamEndpoint: endpoint,
		Stream:           false,
		Duration:         time.Since(startTime),
	}, nil
}
