package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/logger"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/requestmodel"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/util/responseheaders"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"go.uber.org/zap"
)

// openaiCCRawAllowedHeaders 是 CC 直转路径专用的客户端 header 透传白名单。
//
// **关键**：不能复用 openaiAllowedHeaders——后者含 Codex 客户端专属 header
// （originator / session_id / x-codex-turn-state / x-codex-turn-metadata / conversation_id），
// 这些在 ChatGPT OAuth 上游是必需的，但透传给 DeepSeek/Kimi/GLM 等第三方
// OpenAI 兼容上游会造成：
//   - 完全忽略（多数友好厂商）——隐性污染上游统计
//   - 400 "unknown parameter"（严格上游）——可见错误
//
// 这里仅放行通用 HTTP header；content-type / authorization / accept 由上下文
// 显式设置，不依赖透传。
//
// 参见决策记录：
// pensieve/short-term/maxims/dont-reuse-shared-headers-whitelist-across-different-upstream-trust-domains
var openaiCCRawAllowedHeaders = map[string]bool{
	"accept-language": true,
	"user-agent":      true,
}

// forwardAsRawChatCompletions 直转客户端的 Chat Completions 请求到上游
// `{base_url}/v1/chat/completions`，**不**做 CC↔Responses 协议转换。
//
// 适用场景：account.platform=openai && account.type=apikey && 上游已被探测确认
// 不支持 /v1/responses 端点（如 GLM/Qwen 等第三方 OpenAI 兼容上游）；CN 供应商
// 固定 chat_completions 协议也走此路径。
//
// 与 ForwardAsChatCompletions 的关键差异：
//
//   - 不调用 apicompat.ChatCompletionsToResponses，body 仅做模型 ID 改写
//   - 上游 URL 拼到 /v1/chat/completions 而非 /v1/responses
//   - 流式响应 SSE 直接透传给客户端（上游 chunk 已是 CC 格式）
//   - 非流式响应 JSON 直接透传，仅按需提取 usage
//   - 不应用 codex OAuth transform（APIKey 路径无 OAuth）
//   - 不注入 prompt_cache_key（OAuth 专属机制）
//
// 调用入口：openai_gateway_chat_completions.go::ForwardAsChatCompletions
// 在函数顶部按 openai_compat.ShouldUseResponsesAPI 分流。
func (s *OpenAIGatewayService) forwardAsRawChatCompletions(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	defaultMappedModel string,
) (*OpenAIForwardResult, error) {
	c.Set("chat_client_include_usage", gjson.GetBytes(body, "stream_options.include_usage").Bool())
	startTime := time.Now()
	if err := requestmodel.ValidateJSON(body); err != nil {
		writeChatCompletionsError(c, http.StatusBadRequest, "invalid_request_error", "Invalid request body")
		return nil, err
	}

	// 1. Parse minimal fields needed for routing/billing
	originalModel := gjson.GetBytes(body, "model").String()
	if originalModel == "" {
		writeChatCompletionsError(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return nil, fmt.Errorf("missing model in request")
	}
	clientStream := gjson.GetBytes(body, "stream").Bool()

	// 2. Resolve model mapping (same as ForwardAsChatCompletions)
	billingModel := resolveOpenAIForwardModel(account, originalModel, defaultMappedModel)
	upstreamModel := normalizeOpenAIModelForUpstream(account, billingModel)
	SetOpsUpstreamModel(c, upstreamModel)
	grokCacheIdentity := ""
	if account.Platform == PlatformGrok {
		// Resolve before image bridging or other body rewrites so the fallback is
		// anchored to the client's stable conversation prefix.
		grokCacheIdentity = resolveGrokCacheIdentity(c, body, "", upstreamModel)
	}
	reasoningEffort := extractOpenAIReasoningEffortFromBody(body, upstreamModel, billingModel, originalModel)
	// 国产模型默认 effort 补充：需要 mappedModel 判定，推迟到 billingModel 算出之后。
	reasoningEffort = ApplyThinkingEnabledFallback(reasoningEffort, body, billingModel)

	// 3. Rewrite model in body (no protocol conversion)
	upstreamBody := body
	if upstreamModel != originalModel {
		upstreamBody = ReplaceModelInBody(body, upstreamModel)
	}
	if normalizedBody, normalized := NormalizeGLMOpenAIReasoningEffort(upstreamBody, upstreamModel); normalized {
		upstreamBody = normalizedBody
	}

	// 4. Apply OpenAI fast policy on the CC body
	updatedBody, policyErr := s.applyOpenAIFastPolicyToBody(ctx, account, upstreamModel, upstreamBody)
	if policyErr != nil {
		var blocked *OpenAIFastBlockedError
		if errors.As(policyErr, &blocked) {
			MarkOpsClientBusinessLimited(c, OpsClientBusinessLimitedReasonLocalPolicyDenied)
			writeChatCompletionsError(c, http.StatusForbidden, "permission_error", blocked.Message)
		}
		return nil, policyErr
	}
	upstreamBody = updatedBody
	// Keep the final outbound tier separate from the observed response tier so
	// usage recording can apply the selected credential's response contract.
	serviceTier := extractOpenAIServiceTierFromBody(upstreamBody)
	if account.Platform == PlatformGrok {
		strippedBody, stripErr := stripRedundantGrokChatViewImageTool(upstreamBody)
		if stripErr != nil {
			return nil, fmt.Errorf("strip redundant Grok Chat view_image tool: %w", stripErr)
		}
		upstreamBody = strippedBody
	}

	// Grok Composer does not accept image_url parts directly, but Grok Build
	// can describe the images first. Bridge only this exact failure mode.
	token, tokenKind, err := s.getRequestCredential(ctx, c, account)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(token) == "" && !compatibleSpecialKeyOptional(account) {
		return nil, fmt.Errorf("account %d missing %s credential", account.ID, tokenKind)
	}

	var bridgeUsage OpenAIUsage
	if account.Platform == PlatformGrok {
		bridgedBody, usage, bridged, bridgeErr := s.bridgeGrokComposerImageInputs(ctx, c, account, upstreamBody, token)
		if bridgeErr != nil {
			var failoverErr *UpstreamFailoverError
			if !errors.As(bridgeErr, &failoverErr) && c != nil && c.Writer != nil && !c.Writer.Written() {
				writeChatCompletionsError(c, http.StatusBadGateway, "upstream_error", bridgeErr.Error())
			}
			return nil, bridgeErr
		}
		if bridged {
			upstreamBody = bridgedBody
			addOpenAIUsage(&bridgeUsage, usage)
		}
	}

	if clientStream {
		var usageErr error
		upstreamBody, usageErr = ensureOpenAIChatStreamUsage(upstreamBody)
		if usageErr != nil {
			return nil, fmt.Errorf("enable stream usage: %w", usageErr)
		}
	}
	if account.Platform == PlatformGrok {
		upstreamBody, err = applyGrokSafetyIdentifier(upstreamBody, c)
		if err != nil {
			return nil, fmt.Errorf("apply Grok safety identifier: %w", err)
		}
		upstreamBody, err = stripGrokChatPromptCacheKey(upstreamBody)
		if err != nil {
			return nil, fmt.Errorf("remove Responses-only Grok prompt cache key: %w", err)
		}
		upstreamBody, err = normalizeGrokChatReasoningEffort(upstreamBody, upstreamModel)
		if err != nil {
			return nil, fmt.Errorf("normalize Grok chat reasoning effort: %w", err)
		}
		reasoningEffort = optionalTrimmedStringPtr(gjson.GetBytes(upstreamBody, "reasoning_effort").String())
		upstreamBody, err = sanitizeGrokUnsupportedFields(upstreamBody)
		if err != nil {
			return nil, fmt.Errorf("sanitize Grok unsupported fields: %w", err)
		}
	}
	upstreamBody = applyOllamaCloudRawChatCompletionsRequest(account, upstreamBody)
	upstreamBody = clampOllamaCloudUpstreamMaxTokens(account, upstreamBody)

	logger.L().Debug("openai chat_completions raw: forwarding without protocol conversion",
		zap.Int64("account_id", account.ID),
		zap.String("original_model", originalModel),
		zap.String("billing_model", billingModel),
		zap.String("upstream_model", upstreamModel),
		zap.Bool("stream", clientStream),
	)

	// 5. Build and send upstream request via the shared CC pipeline
	targetURL, err := s.rawChatCompletionsURL(account)
	if err != nil {
		return nil, err
	}
	if account.Platform == PlatformHunyuan {
		prepared, prepareErr := buildHunyuanChatRequest(ctx, account, upstreamBody, upstreamModel)
		if prepareErr != nil {
			writeChatCompletionsError(c, http.StatusBadRequest, "invalid_request_error", prepareErr.Error())
			return nil, prepareErr
		}
		targetURL = prepared.URL.String()
	}
	if account.Platform == PlatformQianfan {
		prepared, prepareErr := buildQianfanRequest(ctx, account, "chat/completions", upstreamBody, s.validateUpstreamBaseURL)
		if prepareErr != nil {
			return nil, prepareErr
		}
		targetURL = prepared.URL.String()
	}
	if account.Platform == PlatformVolcengine {
		prepared, prepareErr := s.prepareVolcengineChatRequest(account, upstreamBody, upstreamModel)
		if prepareErr != nil {
			writeChatCompletionsError(c, http.StatusBadRequest, "invalid_request_error", prepareErr.Error())
			return nil, prepareErr
		}
		targetURL, upstreamBody, token = prepared.TargetURL, prepared.Body, prepared.APIKey
	}
	upstreamBody, err = normalizeStrictChatDeveloperRoles(account, targetURL, upstreamBody)
	if err != nil {
		writeChatCompletionsError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return nil, err
	}
	SetActualOpenAIUpstreamEndpoint(c, grokChatRawEndpoint)
	customUA := account.GetOpenAIUserAgent()
	if customUA == "" && account.IsGrokOAuth() {
		customUA = defaultGrokUpstreamUserAgent()
	}
	if account.Platform == PlatformQianwen {
		var thinkingErr error
		var thinking *bool
		upstreamBody, thinking, thinkingErr = prepareQianwenThinkingWithPolicy(ctx, c, body, upstreamBody, upstreamModel, "enable_thinking")
		if thinkingErr == nil {
			thinkingErr = s.validateQianwenThinkingMode(ctx, c, upstreamModel, thinking)
		}
		if thinkingErr != nil {
			status, kind := nativePolicyErrorResponse(thinkingErr)
			writeChatCompletionsError(c, status, kind, thinkingErr.Error())
			return nil, thinkingErr
		}
	}
	if account.IsCommandCode() {
		upstreamBody, err = normalizeCommandCodeReasoningWithPolicy(c, upstreamBody, upstreamModel)
		if err != nil {
			status, kind := nativePolicyErrorResponse(err)
			writeChatCompletionsError(c, status, kind, err.Error())
			return nil, err
		}
		reasoningEffort = extractOpenAIReasoningEffortFromBody(upstreamBody, "", "", "")
	}
	if account.Platform == PlatformQianwen {
		if err := s.validateQianwenRequestConditions(ctx, c, upstreamModel, upstreamBody, targetURL, qianwenExplicitThinking(upstreamBody, "enable_thinking")); err != nil {
			writeChatCompletionsError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
			return nil, err
		}
	}
	upstreamCtx, headersReceived, cancelUpstream := boundedStreamContext(ctx, streamHeaderTimeout(s.cfg, true))
	defer cancelUpstream()
	resp, err := s.sendCCUpstreamRequest(upstreamCtx, c, account, targetURL, upstreamBody, clientStream, token, customUA, grokCacheIdentity)
	headersReceived()
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	// 7. Handle error response with failover
	if resp.StatusCode >= 400 {
		if account.Platform == PlatformHunyuan {
			scope := hunyuanHTTPErrorScope(resp.StatusCode)
			writeChatCompletionsError(c, resp.StatusCode, "upstream_error", "Hunyuan upstream rejected the request")
			return nil, fmt.Errorf("hunyuan HTTP %d (%s)", resp.StatusCode, scope)
		}
		respBody, upstreamMsg := s.readOpenAIUpstreamError(resp)
		if account.Platform == PlatformGrok {
			kind := "http_error"
			if s.shouldFailoverGrokUpstreamError(resp.StatusCode, respBody) {
				kind = "failover"
			}
			appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
				ProxyID:            opsUpstreamProxyID(account),
				ProxyName:          opsUpstreamProxyName(account),
				Platform:           account.Platform,
				AccountID:          account.ID,
				AccountName:        account.Name,
				UpstreamStatusCode: resp.StatusCode,
				UpstreamRequestID:  firstNonEmpty(resp.Header.Get("x-request-id"), resp.Header.Get("xai-request-id")),
				Kind:               kind,
				Message:            upstreamMsg,
			})
			s.handleGrokAccountUpstreamError(withGrokTeamRateLimitModel(ctx, upstreamModel), account, resp.StatusCode, resp.Header, respBody)
			if s.shouldFailoverGrokUpstreamError(resp.StatusCode, respBody) {
				retryable, retryDelay, retryDeadline, retryMax := grokSameAccountRetryMetadata(account, resp.StatusCode, respBody)
				return nil, &UpstreamFailoverError{
					StatusCode:               resp.StatusCode,
					ResponseBody:             respBody,
					ResponseHeaders:          resp.Header.Clone(),
					RetryableOnSameAccount:   retryable,
					RequestScopedTransient:   retryable && resp.StatusCode == http.StatusTooManyRequests,
					SameAccountRetryDelay:    retryDelay,
					SameAccountRetryDeadline: retryDeadline,
					SameAccountRetryMax:      retryMax,
				}
			}
			return s.handleChatCompletionsErrorResponse(resp, c, account, billingModel)
		}
		if foErr := s.failoverOpenAIUpstreamHTTPError(ctx, c, account, resp, respBody, upstreamMsg, upstreamModel); foErr != nil {
			return nil, foErr
		}
		return s.handleChatCompletionsErrorResponse(resp, c, account, billingModel)
	}

	if account.Platform == PlatformGrok {
		s.updateGrokUsageFromResponse(withGrokTeamRateLimitModel(ctx, upstreamModel), account, resp.Header, resp.StatusCode)
	}

	// 8. Forward response
	var result *OpenAIForwardResult
	var forwardErr error
	if clientStream {
		result, forwardErr = s.streamRawChatCompletions(c, resp, account, originalModel, billingModel, upstreamModel, reasoningEffort, serviceTier, startTime, len(body), gjson.GetBytes(body, "n").Int())
	} else {
		result, forwardErr = s.bufferRawChatCompletions(c, resp, account, originalModel, billingModel, upstreamModel, reasoningEffort, serviceTier, startTime)
	}
	if result != nil {
		if account.Platform == PlatformQianwen {
			result.ThinkingEnabled = qianwenExplicitThinking(upstreamBody, "enable_thinking")
			if account.Platform == PlatformQianwen {
				result.QianwenExplicitCache, _ = qianwenCacheMode(upstreamBody)
				result.QianwenBatchChat = qianwenBatchChatURL(targetURL)
			}
		}
		addOpenAIUsage(&result.Usage, bridgeUsage)
		result.UpstreamEndpoint = grokChatRawEndpoint
	}
	// Keep the public strict-provider contract: unknown usage is an error,
	// never a successful result. The error retains delivered usage for review.
	var usageErr *RawChatUsageError
	if errors.As(forwardErr, &usageErr) {
		return nil, forwardErr
	}
	return result, forwardErr
}

func (s *OpenAIGatewayService) rawChatCompletionsURL(account *Account) (string, error) {
	if account.Platform == PlatformGrok {
		targetURL, err := buildGrokChatCompletionsURL(account, s.cfg, s.settingService)
		if err != nil {
			return "", fmt.Errorf("invalid grok base_url: %w", err)
		}
		return targetURL, nil
	}

	return s.openAIChatCompletionsTargetURL(account)
}

// RawChatUsageError carries the receipt for a delivered strict-provider stream
// whose counters are incomplete. Callers must persist Result for review even
// though the public forwarding method returns a nil result.
type RawChatUsageError struct {
	Result *OpenAIForwardResult
	cause  error
}

func (e *RawChatUsageError) Error() string { return e.cause.Error() }
func (e *RawChatUsageError) Unwrap() error { return e.cause }

// streamRawChatCompletions 透传上游 CC SSE 流到客户端，并提取 usage（包括
// 末尾 [DONE] 之前的 chunk 中的 usage 字段，按 OpenAI CC 协议）。
//
// 按完整 SSE 事件合并快照；EOF 只接受已完成的候选。
// 网关对上游强制打开 include_usage 以保证计费完整；只在客户端显式请求时透传 usage，
// 让级联代理或下游计费系统也能拿到完整用量。
func (s *OpenAIGatewayService) streamRawChatCompletions(
	c *gin.Context,
	resp *http.Response,
	account *Account,
	originalModel string,
	billingModel string,
	upstreamModel string,
	reasoningEffort *string,
	serviceTier *string,
	startTime time.Time,
	requestBodyLen int,
	requestedChoices ...int64,
) (*OpenAIForwardResult, error) {
	observer := upstreamResponseModelObserverFromContext(c)
	if observer == nil {
		observer = beginUpstreamResponseModelObservation(c)
	}
	requestID := resp.Header.Get("x-request-id")
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

	var usage OpenAIUsage
	var firstTokenMs *int
	observation := &TokenUsageObservation{}
	strict := account.Platform == PlatformMistral || account.Platform == PlatformOpenRouter || account.Platform == PlatformKilo || account.Platform == PlatformWorkBuddy || account.Platform == PlatformHunyuan
	hunyuanTotal := false
	usageKnown := func() bool { return observation.known() && (account.Platform != PlatformHunyuan || hunyuanTotal) }
	clientDisconnected := false
	clientOutputStarted := false
	pendingLines := make([]string, 0, 8)
	refusalDetector := newOpenAIChatSilentRefusalDetector(requestBodyLen)
	var terminal rawChatChoices
	if len(requestedChoices) > 0 {
		terminal.expected = requestedChoices[0]
	}
	var frameErr error

	writeLine := func(line string) {
		if clientDisconnected {
			return
		}
		if !clientOutputStarted && !refusalDetector.ShouldReleaseClientOutput() {
			pendingLines = append(pendingLines, line)
			return
		}
		if !clientOutputStarted {
			writeStreamHeaders()
			for _, pending := range pendingLines {
				if _, werr := c.Writer.WriteString(pending + "\n"); werr != nil {
					clientDisconnected = true
					logger.L().Debug("openai chat_completions raw: client disconnected, closing upstream with observed usage",
						zap.Error(werr),
						zap.String("request_id", requestID),
					)
					return
				}
			}
			pendingLines = pendingLines[:0]
			clientOutputStarted = true
		}
		if _, werr := c.Writer.WriteString(line + "\n"); werr != nil {
			clientDisconnected = true
			logger.L().Debug("openai chat_completions raw: client disconnected, closing upstream with observed usage",
				zap.Error(werr),
				zap.String("request_id", requestID),
			)
		}
	}

	for scanner.Scan() {
		payload := strings.TrimSpace(scanner.frame.Data)
		line := "data: " + payload
		if payload != "[DONE]" {
			if account.Platform == PlatformWorkBuddy && (gjson.Get(payload, "error").Exists() || gjson.Get(payload, "code").Int() != 0) {
				_ = s.workBuddyFailover(c.Request.Context(), account, resp, []byte(payload))
				frameErr = fmt.Errorf("WorkBuddy stream failed")
				break
			}

			if (account.Platform == PlatformOpenRouter || account.Platform == PlatformKilo) && openRouterStreamError(payload) {
				frameErr = fmt.Errorf("%s stream failed", account.Platform)
				break
			}
			if !gjson.Valid(payload) || gjson.Get(payload, "error").Exists() {
				frameErr = fmt.Errorf("invalid or error Chat Completions frame")
				break
			}
		}
		if payload == "[DONE]" && strict && !usageKnown() {
			break
		}
		if payload != "[DONE]" {
			data := []byte(payload)
			observer.ObserveOpenAI(data, strings.TrimSpace(gjson.Get(payload, "type").String()))
			mergeOpenAIFrameUsage(data, &usage, observation, "")
			if account.Platform == PlatformHunyuan && gjson.Get(payload, "usage.total_tokens").Exists() {
				_, hunyuanTotal = openRouterCounter(data, "usage.total_tokens")
			}
		}
		if frameErr = terminal.observe(payload); frameErr != nil {
			break
		}
		if firstTokenMs == nil && terminal.semanticOutput {
			scanner.Semantic()
			elapsed := int(time.Since(startTime).Milliseconds())
			firstTokenMs = &elapsed
		}
		refusalDetector.ObserveSSELine(line)
		if !c.GetBool("chat_client_include_usage") && payload != "[DONE]" {
			if isOpenAIChatUsageOnlyStreamChunk(payload) {
				continue
			}
			if gjson.Get(payload, "usage").Exists() {
				filtered, err := sjson.Delete(payload, "usage")
				if err == nil {
					line = "data: " + filtered
				}
			}
		}
		line = applyOllamaCloudRawChatCompletionsSSELine(account, line)
		line = stripEmptyChatToolCallIdentityFromSSELine(line)

		writeLine(strings.ReplaceAll(line, "\n", "\ndata: "))
		writeLine("")
		if clientDisconnected {
			scanner.Disconnected()
		}
		if terminal.sawDone {
			if !clientDisconnected && clientOutputStarted {
				c.Writer.Flush()
			}
			break
		}
		if !clientDisconnected && clientOutputStarted {
			c.Writer.Flush()
		}
	}

	scanErr := scanner.Err()
	if frameErr != nil {
		scanErr = frameErr
	}
	resultWithUsage := func() *OpenAIForwardResult {
		var evidence *TokenUsageObservation
		if strict || account.Platform == PlatformOpenAI || IsCompatibleProvider(account.Platform) {
			evidence = observation
		}
		return &OpenAIForwardResult{
			RequestID:                     requestID,
			UpstreamHeaders:               resp.Header,
			Usage:                         usage,
			TokenUsageObservation:         evidence,
			UsageIncomplete:               !evidence.known() || strict && (!usageKnown() || !terminal.complete() || scanErr != nil),
			Model:                         originalModel,
			BillingModel:                  billingModel,
			UpstreamModel:                 upstreamModel,
			UpstreamResponseModel:         observedUpstreamResponseModel(c),
			UpstreamResponseModelConflict: observedUpstreamResponseModelConflict(c),
			UpstreamResponseServiceTier:   observedUpstreamResponseServiceTier(c),
			ReasoningEffort:               reasoningEffort,
			ServiceTier:                   resolvedOpenAIUpstreamServiceTier(c, serviceTier),
			Stream:                        true,
			ClientDisconnect:              clientDisconnected || c.Request.Context().Err() != nil,
			Duration:                      time.Since(startTime),
			FirstTokenMs:                  firstTokenMs,
		}
	}

	if scanErr != nil && !errors.Is(scanErr, context.Canceled) && !errors.Is(scanErr, context.DeadlineExceeded) {
		logger.L().Warn("openai chat_completions raw: stream read error",
			zap.Error(scanErr),
			zap.String("request_id", requestID),
		)
	}

	if strict && !usageKnown() {
		err := newOpenAIUpstreamStreamReadError(fmt.Errorf("upstream chat usage is unknown"))
		if clientOutputStarted || c.Writer.Size() > 0 {
			result := resultWithUsage()
			return result, &RawChatUsageError{Result: result, cause: err}
		}
		return nil, err
	}
	clientAborted := clientDisconnected || c.Request.Context().Err() != nil

	// 上游在任何终止信号之前结束：连接被 reset（scanErr != nil）或干净 EOF。
	// 两者都不能再记成功——此前统一返回 nil error，把上游截断伪装成
	// `HTTP 200 + usage 0/0`，客户端收到半截回答且 Ops 侧完全无感。
	if !clientAborted && (scanErr != nil || !terminal.complete()) {
		cause := scanErr
		if cause == nil {
			cause = ErrOpenAIUpstreamStreamTruncated
		}
		logger.L().Warn("openai chat_completions raw: upstream stream truncated before terminal chunk",
			zap.Error(cause),
			zap.String("request_id", requestID),
			zap.Int64("account_id", account.ID),
			zap.String("upstream_model", upstreamModel),
			zap.Bool("saw_choices", len(terminal.finished) > 0),
			zap.Bool("client_output_started", clientOutputStarted),
		)

		// 已写出语义字节：无法再 failover，改为带类型的上游错误。handler 会据此
		// 补发 SSE error 帧并把本次请求计入 SLA 失败。
		recordOpenAIRawStreamTruncation(c, account, requestID, cause, "http_error")
		return resultWithUsage(), newOpenAIUpstreamStreamReadError(cause)
	}

	if scanErr == nil && !clientDisconnected && !clientOutputStarted {
		if len(pendingLines) > 0 {
			writeStreamHeaders()
			for _, pending := range pendingLines {
				if _, werr := c.Writer.WriteString(pending + "\n"); werr != nil {
					clientDisconnected = true
					logger.L().Debug("openai chat_completions raw: client disconnected during final flush",
						zap.Error(werr),
						zap.String("request_id", requestID),
					)
					break
				}
			}
			if !clientDisconnected {
				c.Writer.Flush()
				clientOutputStarted = true
			}
		}
	}

	if clientAborted || clientDisconnected {
		return resultWithUsage(), context.Canceled
	}
	return resultWithUsage(), nil
}

// ensureOpenAIChatStreamUsage 确保 raw Chat Completions 流式请求会让上游返回 usage。
// 下游仅在客户端显式请求 include_usage 时收到用量；服务端计费始终保留。
func ensureOpenAIChatStreamUsage(body []byte) ([]byte, error) {
	updated, err := sjson.SetBytes(body, "stream_options.include_usage", true)
	if err != nil {
		return body, err
	}
	return updated, nil
}

func isOpenAIChatUsageOnlyStreamChunk(payload string) bool {
	if strings.TrimSpace(payload) == "" {
		return false
	}
	if !gjson.Get(payload, "usage").Exists() {
		return false
	}
	choices := gjson.Get(payload, "choices")
	return choices.Exists() && choices.IsArray() && len(choices.Array()) == 0
}

// A single-choice compatibility stream may use [DONE] as its only finish
// marker. In multi-choice streams each observed generation must finish.
type rawChatChoices struct {
	expected             int64
	finished             map[int64]bool
	tools                map[int64]map[int64]*strings.Builder
	sawEnvelope, sawDone bool
	semanticOutput       bool
}

func (s *rawChatChoices) complete() bool {
	if !s.sawEnvelope || int64(len(s.finished)) < s.expected {
		return false
	}
	if len(s.finished) == 1 && s.sawDone {
		return true
	}
	for _, done := range s.finished {
		if !done {
			return false
		}
	}
	return len(s.finished) > 0
}

func (s *rawChatChoices) observe(payload string) error {
	if payload == "[DONE]" {
		s.sawDone = true
		if !s.complete() {
			return ErrOpenAIUpstreamStreamTruncated
		}
		return s.validateTools()
	}
	for _, choice := range gjson.Get(payload, "choices").Array() {
		field := choice.Get("index")
		index := field.Int()
		if (s.expected > 1 || field.Exists()) && (field.Type != gjson.Number || field.Float() != float64(index) || index < 0 || s.expected > 0 && index >= s.expected) {
			return fmt.Errorf("invalid Chat Completions choice index")
		}
		if s.finished == nil {
			s.finished = make(map[int64]bool)
			s.tools = make(map[int64]map[int64]*strings.Builder)
		}
		if delta := choice.Get("delta"); delta.IsObject() {
			if delta.Get("content").String() != "" || delta.Get("reasoning_content").String() != "" || delta.Get("tool_calls").Exists() || choice.Get("finish_reason").String() != "" {
				s.semanticOutput = true
			}
			s.sawEnvelope = true
		}
		for _, tool := range choice.Get("delta.tool_calls").Array() {
			if s.tools[index] == nil {
				s.tools[index] = make(map[int64]*strings.Builder)
			}
			key := tool.Get("index").Int()
			if s.tools[index][key] == nil {
				s.tools[index][key] = &strings.Builder{}
			}
			s.tools[index][key].WriteString(tool.Get("function.arguments").String())
		}
		s.finished[index] = s.finished[index] || choice.Get("finish_reason").String() != ""
		if s.finished[index] {
			if err := validateRawChatToolArguments(s.tools[index]); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *rawChatChoices) validateTools() error {
	for _, tools := range s.tools {
		if err := validateRawChatToolArguments(tools); err != nil {
			return err
		}
	}
	return nil
}

func validateRawChatToolArguments(tools map[int64]*strings.Builder) error {
	for _, args := range tools {
		if args.Len() > 0 && !gjson.Valid(args.String()) {
			return fmt.Errorf("incomplete Chat Completions tool arguments")
		}
	}
	return nil
}

// extractCCStreamUsage 从单个 CC 流式 chunk 的 payload 中提取 usage 字段。
// 上游可多次发送用量帧；调用方负责跨帧合并。
func extractCCStreamUsage(payload string) *OpenAIUsage {
	usageResult := gjson.Get(payload, "usage")
	if !usageResult.Exists() || !usageResult.IsObject() {
		return nil
	}
	u, ok := openAIUsageFromGJSON(usageResult)
	if !ok {
		return nil
	}
	return &u
}

// bufferRawChatCompletions 透传上游 CC 非流式 JSON 响应。
func (s *OpenAIGatewayService) bufferRawChatCompletions(
	c *gin.Context,
	resp *http.Response,
	account *Account,
	originalModel string,
	billingModel string,
	upstreamModel string,
	reasoningEffort *string,
	serviceTier *string,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	requestID := resp.Header.Get("x-request-id")

	respBody, err := ReadUpstreamResponseBody(resp.Body, s.cfg, c, openAITooLargeError)
	if err != nil {
		if !errors.Is(err, ErrUpstreamResponseBodyTooLarge) {
			writeChatCompletionsError(c, http.StatusBadGateway, "api_error", "Failed to read upstream response")
		}
		return nil, fmt.Errorf("read upstream body: %w", err)
	}
	var usage OpenAIUsage
	observed := &TokenUsageObservation{}
	if parsedUsage, ok := extractObservedOpenAIUsage(respBody, observed); ok {
		usage = parsedUsage
	}
	choices := gjson.GetBytes(respBody, "choices")
	if !choices.Exists() {
		choices = gjson.GetBytes(respBody, "data.choices")
	}
	// Some upstreams return a usage-only receipt, including a data wrapper.
	// Empty choices are valid with complete counters; populated choices still
	// need a Chat message envelope before any bytes are delivered.
	validEnvelope := choices.IsArray() && choices.Get("0.message").IsObject()
	if !choices.Exists() || choices.IsArray() && len(choices.Array()) == 0 {
		validEnvelope = observed.known()
	}
	if !gjson.ValidBytes(respBody) || !validEnvelope || gjson.GetBytes(respBody, "error").Exists() || gjson.GetBytes(respBody, "data.error").Exists() {
		writeChatCompletionsError(c, http.StatusBadGateway, "upstream_error", "Invalid upstream Chat Completions response")
		return nil, fmt.Errorf("invalid upstream Chat Completions response")
	}
	observer := upstreamResponseModelObserverFromContext(c)
	if observer == nil {
		observer = beginUpstreamResponseModelObservation(c)
	}
	observer.ObserveOpenAI(respBody, strings.TrimSpace(gjson.GetBytes(respBody, "type").String()))

	var observation *TokenUsageObservation
	if account.Platform == PlatformOpenAI {
		observation = observed
	}
	if (account.Platform == PlatformMistral || account.Platform == PlatformOpenRouter || (account.Platform == PlatformWorkBuddy || account.Platform == PlatformKilo)) && !mistralChatUsageKnown(respBody) {
		writeChatCompletionsError(c, http.StatusBadGateway, "upstream_error", "Upstream chat usage is unknown")
		return nil, fmt.Errorf("upstream chat usage is unknown")
	}
	if account.Platform == PlatformHunyuan {
		reported, usageErr := parseHunyuanChatUsage(respBody)
		if usageErr != nil || reported == nil {
			writeChatCompletionsError(c, http.StatusBadGateway, "upstream_error", "Upstream usage is unknown")
			return nil, fmt.Errorf("hunyuan usage unknown or invalid: %v", usageErr)
		}
		usage = *reported
	}
	responseModel := gjson.GetBytes(respBody, "model").String()
	if requiresBillableGrokChatUsage(account, billingModel, upstreamModel, responseModel) && !hasBillableGrokChatUsage(usage) {
		upstreamRequestID := firstNonEmpty(requestID, resp.Header.Get("xai-request-id"))
		return nil, newGrokMissingUsageFailoverError(c, account, upstreamRequestID)
	}
	respBody = applyOllamaCloudRawChatCompletionsResponse(account, respBody)

	if s.responseHeaderFilter != nil {
		responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, s.responseHeaderFilter)
	}
	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(http.StatusOK)
	n, writeErr := c.Writer.Write(respBody)
	if writeErr == nil && n != len(respBody) {
		writeErr = io.ErrShortWrite
	}

	return &OpenAIForwardResult{
		RequestID:                     requestID,
		UpstreamHeaders:               resp.Header,
		TokenUsageObservation:         observation,
		Usage:                         usage,
		Model:                         originalModel,
		BillingModel:                  billingModel,
		UpstreamModel:                 upstreamModel,
		UpstreamResponseModel:         observedUpstreamResponseModel(c),
		UpstreamResponseModelConflict: observedUpstreamResponseModelConflict(c),
		UpstreamResponseServiceTier:   observedUpstreamResponseServiceTier(c),
		ReasoningEffort:               reasoningEffort,
		ServiceTier:                   resolvedOpenAIUpstreamServiceTier(c, serviceTier),
		ClientDisconnect:              writeErr != nil,
		Stream:                        false,
		Duration:                      time.Since(startTime),
	}, writeErr
}

// buildOpenAIChatCompletionsURL 拼接上游 Chat Completions 端点 URL。
//
//   - base 已是 /chat/completions：原样返回
//   - base 以 /v1 结尾：追加 /chat/completions
//   - base 以其他版本段结尾（如 /v4）：追加 /chat/completions
//   - 其他情况：追加 /v1/chat/completions
//
// 与 buildOpenAIResponsesURL 是姐妹函数。
func buildOpenAIChatCompletionsURL(base string) string {
	return buildOpenAIEndpointURL(base, "/v1/chat/completions")
}
