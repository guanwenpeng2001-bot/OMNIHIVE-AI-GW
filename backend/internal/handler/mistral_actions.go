package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

// Mistral native actions beyond chat. Endpoint shapes are fixed against the
// official specification (mistralai/platform-docs-public openapi.yaml, retrieved
// 2026-09-21) and implemented in service/mistral_actions.go: POST /v1/embeddings,
// POST /v1/audio/transcriptions, POST /v1/audio/speech and the /v1/audio/voices
// management surface. The dispatch table is wired by the multimodal ingress;
// these entry points assume an authenticated key whose resolved target platform
// is mistral.

// mistralActionKind names one Mistral data-plane action this file serves. The
// service package owns the wire contract; the kind selects the forwarder, the
// request-plan protocol and the usage gate.
type mistralActionKind string

const (
	mistralActionEmbeddings    mistralActionKind = "embeddings"
	mistralActionTranscription mistralActionKind = "audio.stt"
	mistralActionSpeech        mistralActionKind = "audio.tts"
	mistralActionVoices        mistralActionKind = "voice"
)

// mistralMultipartModelFieldLimit bounds the model form field read below; the
// audio payload is never buffered.
const mistralMultipartModelFieldLimit = 1 << 20

// MistralEmbeddings handles embeddings (POST /v1/embeddings).
func (h *OpenAIGatewayHandler) MistralEmbeddings(c *gin.Context) {
	h.handleMistralAction(c, mistralActionEmbeddings)
}

// MistralAudioTranscriptions handles speech recognition, a multipart upload
// (POST /v1/audio/transcriptions).
func (h *OpenAIGatewayHandler) MistralAudioTranscriptions(c *gin.Context) {
	h.handleMistralAction(c, mistralActionTranscription)
}

// MistralAudioSpeech handles speech synthesis (POST /v1/audio/speech).
func (h *OpenAIGatewayHandler) MistralAudioSpeech(c *gin.Context) {
	h.handleMistralAction(c, mistralActionSpeech)
}

// MistralVoices handles the native voice-management surface
// (/v1/audio/voices and its {voice_id}/sample subresource).
func (h *OpenAIGatewayHandler) MistralVoices(c *gin.Context) {
	h.handleMistralAction(c, mistralActionVoices)
}

// mistralActionRequiredCapability keeps embedding requests on accounts whose
// transport advertises embedding support; audio and voice share the
// chat-completions capability because Mistral serves them from the same host and
// credential.
func mistralActionRequiredCapability(kind mistralActionKind) service.OpenAIEndpointCapability {
	if kind == mistralActionEmbeddings {
		return service.OpenAIEndpointCapabilityEmbeddings
	}
	return ""
}

// mistralActionPlanProtocol labels the request-plan protocol dimension for this
// action, mirroring the sibling chat-only entries (embeddings / audio).
func mistralActionPlanProtocol(kind mistralActionKind) string {
	if kind == mistralActionEmbeddings {
		return "embeddings"
	}
	return "audio"
}

// mistralActionRequiresModel reports which families carry a model selector.
// Voice management is not model-scoped, so it routes without one.
func mistralActionRequiresModel(kind mistralActionKind) bool {
	return kind != mistralActionVoices
}

// mistralActionRequestModel reads the model selector the request plan, the
// moderation gate and the scheduler need before the forwarder runs.
// Transcriptions carry it as a multipart form field; embeddings and speech are
// JSON.
func mistralActionRequestModel(kind mistralActionKind, contentType string, body []byte) string {
	if kind == mistralActionTranscription {
		return mistralMultipartModelField(body, contentType)
	}
	if !gjson.ValidBytes(body) {
		return ""
	}
	return strings.TrimSpace(gjson.GetBytes(body, "model").String())
}

// mistralMultipartModelField reads the small model form field without buffering
// the uploaded audio. service/mistral_actions.go keeps the equivalent reader
// unexported, and the wrapper layer needs the selector before it can schedule an
// account.
func mistralMultipartModelField(body []byte, contentType string) string {
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil || params["boundary"] == "" {
		return ""
	}
	reader := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	for {
		part, err := reader.NextPart()
		if err != nil {
			return ""
		}
		if part.FormName() != "model" || part.FileName() != "" {
			continue
		}
		value, err := io.ReadAll(io.LimitReader(part, mistralMultipartModelFieldLimit))
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(value))
	}
}

// mistralSpeechAuditBody synthesizes the chat shape the moderation extractors
// read from the text a TTS request speaks, the same bridge the sibling entries
// use.
func mistralSpeechAuditBody(input string) []byte {
	payload := map[string]any{
		"messages": []map[string]any{{"role": "user", "content": input}},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil
	}
	return body
}

// mistralVoicesSubpath returns everything after the documented voice family
// marker, so the voice cell serves the CRUD subresources the official
// specification publishes (/v1/audio/voices, /v1/audio/voices/{voice_id} and
// /v1/audio/voices/{voice_id}/sample).
func mistralVoicesSubpath(c *gin.Context) string {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return ""
	}
	const marker = "/audio/voices"
	path := strings.TrimRight(c.Request.URL.Path, "/")
	index := strings.LastIndex(path, marker)
	if index < 0 {
		return ""
	}
	return strings.Trim(path[index+len(marker):], "/")
}

func (h *OpenAIGatewayHandler) handleMistralAction(c *gin.Context, kind mistralActionKind) {
	streamStarted := false
	defer h.recoverResponsesPanic(c, &streamStarted)

	requestStart := time.Now()
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusInternalServerError, "api_error", "User context not found")
		return
	}
	reqLog := requestLogger(
		c,
		"handler.openai_gateway.mistral_action",
		zap.Int64("user_id", subject.UserID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.Any("group_id", apiKey.GroupID),
		zap.String("endpoint", string(kind)),
	)
	if !h.ensureResponsesDependencies(c, reqLog) {
		return
	}

	body, err := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
	if err != nil {
		if maxErr, ok := extractMaxBytesError(err); ok {
			h.errorResponse(c, http.StatusRequestEntityTooLarge, "invalid_request_error", buildBodyTooLargeMessage(maxErr.Limit))
			return
		}
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
		return
	}
	if len(body) == 0 {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Request body is empty")
		return
	}

	contentType := c.GetHeader("Content-Type")
	requestModel := mistralActionRequestModel(kind, contentType, body)
	if mistralActionRequiresModel(kind) && strings.TrimSpace(requestModel) == "" {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}

	reqLog = reqLog.With(zap.String("model", requestModel))
	defer beginGatewayRequestPlan(c, h.gatewayService, apiKey, requestModel, mistralActionPlanProtocol(kind), h.maxAccountSwitches)()
	// 入口前置 hold：媒体/音频/realtime/异步 job 在调用上游之前先按估算占用周期预算，
	// 结算时按实际消耗修正；没有周期预算的 Key 不产生额外工作（T3-2.2）。
	if !reserveGatewayRequestBudget(c, apiKey, requestModel, body) {
		return
	}
	setOpsRequestContext(c, requestModel, false)
	setOpsEndpointContext(c, "", int16(service.RequestTypeSync))

	if kind == mistralActionEmbeddings {
		if decision := h.checkSecurityAudit(c, reqLog, apiKey, subject, "openai_embeddings", requestModel, body); decision != nil && !decision.AllowNextStage {
			h.openAISecurityAuditError(c, decision)
			return
		}
	}
	if kind == mistralActionSpeech {
		if input := strings.TrimSpace(gjson.GetBytes(body, "input").String()); input != "" {
			// Moderation extractors read chat messages; synthesize one from the
			// spoken text, as the sibling TTS entries do.
			decision := h.checkSecurityAudit(c, reqLog, apiKey, subject, service.ContentModerationProtocolOpenAIChat, requestModel, mistralSpeechAuditBody(input))
			if decision != nil && !decision.AllowNextStage {
				h.openAISecurityAuditError(c, decision)
				return
			}
		}
	}

	if h.errorPassthroughService != nil {
		service.BindErrorPassthroughService(c, h.errorPassthroughService)
	}
	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	service.SetOpsLatencyMs(c, service.OpsAuthLatencyMsKey, time.Since(requestStart).Milliseconds())

	userReleaseFunc, acquired := h.acquireResponsesUserSlot(c, subject.UserID, subject.Concurrency, false, &streamStarted, reqLog)
	if !acquired {
		return
	}
	if userReleaseFunc != nil {
		defer userReleaseFunc()
	}

	if err := h.billingCacheService.CheckBillingEligibility(c.Request.Context(), apiKey.User, apiKey, apiKey.Group, subscription, service.QuotaPlatform(c.Request.Context(), apiKey)); err != nil {
		reqLog.Info("mistral_action.billing_eligibility_check_failed", zap.Error(err))
		status, code, message, _ := billingErrorDetails(err)
		writeAdmissionRejectionHeaders(c, err)
		h.errorResponse(c, status, code, message)
		return
	}

	sessionHash := h.gatewayService.GenerateExplicitSessionHash(c, body)
	// These four actions bill per unit (tokens, audio seconds, characters), not
	// through the text profit gate: suppress it the way the sibling media paths
	// do, so an in-flight request is never vetoed as a text request.
	requestCtx := service.WithOpenAIProfitControlSuppressed(c.Request.Context())
	profitVetoCount := 0
	failoverState := NewFailoverState(h.maxAccountSwitches, false)
	routingStart := time.Now()
	requiredCapability := mistralActionRequiredCapability(kind)
	var accountReleaseFunc func()
	releaseAccount := func() {
		if accountReleaseFunc != nil {
			accountReleaseFunc()
			accountReleaseFunc = nil
		}
	}
	defer releaseAccount()

	for {
		releaseAccount()
		if failoverClientGone(c) {
			return
		}
		selection, _, err := h.gatewayService.SelectAccountWithSchedulerForCapability(
			requestCtx,
			apiKey.GroupID,
			"",
			sessionHash,
			requestModel,
			failoverState.FailedAccountIDs,
			service.OpenAIUpstreamTransportHTTPSSE,
			requiredCapability,
			false,
			false,
			false,
			service.PlatformMistral,
		)
		// Own an eagerly acquired slot before any rejection.
		if selection != nil && selection.Acquired {
			selection.ReleaseFunc = wrapReleaseOnDone(requestCtx, selection.ReleaseFunc)
			accountReleaseFunc = selection.ReleaseFunc
		}
		if err != nil {
			if failoverClientGone(c) {
				reqLog.Info("mistral_action.account_select_aborted_client_disconnected", zap.Error(err))
				return
			}
			reqLog.Warn("mistral_action.account_select_failed",
				zap.Error(err),
				zap.Int("excluded_account_count", len(failoverState.FailedAccountIDs)),
			)
			if len(failoverState.FailedAccountIDs) == 0 {
				cls := classifyNoAccountErrorFromGin(c, h.gatewayService, apiKey, requestModel, requestModel, service.PlatformMistral)
				if !cls.ModelNotFound {
					markOpsRoutingCapacityLimitedIfNoAvailable(c, err)
				}
				h.errorResponse(c, cls.Status, cls.ErrType, cls.Message)
				return
			}
			if failoverState.LastFailoverErr != nil {
				h.handleFailoverExhausted(c, failoverState.LastFailoverErr, false)
			} else {
				h.errorResponse(c, http.StatusBadGateway, "api_error", "Upstream request failed")
			}
			return
		}
		if selection == nil || selection.Account == nil {
			cls := classifyNoAccountErrorFromGin(c, h.gatewayService, apiKey, requestModel, requestModel, service.PlatformMistral)
			if !cls.ModelNotFound {
				markOpsRoutingCapacityLimited(c)
			}
			h.errorResponse(c, cls.Status, cls.ErrType, cls.Message)
			return
		}
		if failoverClientGone(c) {
			return
		}

		account := selection.Account
		sessionHash = ensureOpenAIPoolModeSessionHash(sessionHash, account)
		setOpsSelectedAccount(c, account.ID, account.Platform)

		var slotResult openAISlotAcquireResult
		accountReleaseFunc, slotResult = h.acquireResponsesAccountSlot(c, apiKey.GroupID, sessionHash, selection, false, &streamStarted, reqLog)
		if slotResult == openAISlotAcquireProfitVetoed {
			// The action path is exempt from the profit gate; defensive fallback only.
			if !recordOpenAIProfitVeto(failoverState.FailedAccountIDs, account.ID, &profitVetoCount) {
				h.handleOpenAIProfitVetoExhausted(c, streamStarted, reqLog, profitVetoCount)
				return
			}
			continue
		}
		if slotResult != openAISlotAcquireOK {
			return
		}

		service.SetOpsLatencyMs(c, service.OpsRoutingLatencyMsKey, time.Since(routingStart).Milliseconds())
		forwardStart := time.Now()
		writerSizeBeforeForward := c.Writer.Size()
		result, err := func() (*service.OpenAIForwardResult, error) {
			defer releaseAccount()
			switch kind {
			case mistralActionEmbeddings:
				return h.gatewayService.ForwardMistralEmbeddings(requestCtx, c, account, body, "")
			case mistralActionTranscription:
				return h.gatewayService.ForwardMistralAudioTranscription(requestCtx, c, account, body, contentType, "")
			case mistralActionSpeech:
				return h.gatewayService.ForwardMistralAudioSpeech(requestCtx, c, account, body, "")
			default:
				return h.gatewayService.ForwardMistralVoices(requestCtx, c, account, mistralVoicesSubpath(c), body, contentType, c.Request.Method)
			}
		}()

		forwardDurationMs := time.Since(forwardStart).Milliseconds()
		upstreamLatencyMs, _ := getContextInt64(c, service.OpsUpstreamLatencyMsKey)
		responseLatencyMs := forwardDurationMs
		if upstreamLatencyMs > 0 && forwardDurationMs > upstreamLatencyMs {
			responseLatencyMs = forwardDurationMs - upstreamLatencyMs
		}
		service.SetOpsLatencyMs(c, service.OpsResponseLatencyMsKey, responseLatencyMs)

		if err != nil {
			var failoverErr *service.UpstreamFailoverError
			if errors.As(err, &failoverErr) {
				if failoverClientGone(c) {
					reqLog.Info("mistral_action.failover_aborted_client_disconnected",
						zap.Int64("account_id", account.ID),
						zap.Int("upstream_status", failoverErr.StatusCode),
					)
					return
				}
				if failoverErr.ShouldReportAccountScheduleFailure() {
					h.gatewayService.ReportOpenAIAccountScheduleResult(account, mistralActionScheduleModel(account, requestModel, result), false, nil)
				}
				if c.Writer.Size() != writerSizeBeforeForward {
					h.handleFailoverExhausted(c, failoverErr, true)
					return
				}
				if !failoverErr.ShouldRetryNextAccount() {
					h.handleFailoverExhausted(c, failoverErr, false)
					return
				}
				switch failoverState.HandleFailoverError(requestCtx, nil, account.ID, account.Platform, effectiveSameAccountRetryLimit(failoverErr, account), failoverErr) {
				case FailoverCanceled:
					return
				case FailoverExhausted:
					h.handleFailoverExhausted(c, failoverErr, streamStarted)
					return
				}
				reqLog.Warn("mistral_action.upstream_failover_switching",
					zap.Int64("account_id", account.ID),
					zap.Int("upstream_status", failoverErr.StatusCode),
					zap.Int("switch_count", failoverState.SwitchCount),
				)
				continue
			}
			h.gatewayService.ReportOpenAIAccountScheduleResult(account, mistralActionScheduleModel(account, requestModel, result), false, nil)
			if !service.IsResponseCommitted(c) && c.Writer.Size() == writerSizeBeforeForward {
				h.errorResponse(c, http.StatusBadGateway, "upstream_error", "Upstream request failed")
			}
			reqLog.Warn("mistral_action.forward_failed",
				zap.Int64("account_id", account.ID),
				zap.Error(err),
			)
			return
		}

		h.gatewayService.ReportOpenAIAccountScheduleResult(account, mistralActionScheduleModel(account, requestModel, result), true, nil)
		if shouldRecordMistralActionUsage(kind, result) {
			h.recordMistralActionUsage(c, reqLog, apiKey, subject, subscription, account, result, requestModel, body)
		}
		reqLog.Debug("mistral_action.request_completed",
			zap.Int64("account_id", account.ID),
			zap.Int("switch_count", failoverState.SwitchCount),
		)
		return
	}
}

// shouldRecordMistralActionUsage gates immediate usage writes. Voice management
// is not a billable generation action, so its cell records no usage at all.
func shouldRecordMistralActionUsage(kind mistralActionKind, result *service.OpenAIForwardResult) bool {
	if result == nil {
		return false
	}
	switch kind {
	case mistralActionEmbeddings:
		return result.Usage.InputTokens > 0 || result.Usage.OutputTokens > 0
	case mistralActionTranscription, mistralActionSpeech:
		return result.AudioUsage != nil
	default:
		return false
	}
}

// recordMistralActionUsage writes one usage record for the settled forward. The
// forwarder already extracted the billable quantity (tokens, recognised audio
// seconds, counted characters), so this hands the same result to the shared
// settlement the sibling entries use.
func (h *OpenAIGatewayHandler) recordMistralActionUsage(
	c *gin.Context,
	reqLog *zap.Logger,
	apiKey *service.APIKey,
	subject middleware2.AuthSubject,
	subscription *service.UserSubscription,
	account *service.Account,
	result *service.OpenAIForwardResult,
	requestModel string,
	body []byte,
) {
	userAgent := c.GetHeader("User-Agent")
	clientIP := ip.GetClientIP(c)
	sessionID := service.ExtractClientSessionID(c)
	inboundEndpoint := GetInboundEndpoint(c)
	upstreamEndpoint := GetUpstreamEndpoint(c, account.Platform)
	quotaPlatform := service.QuotaPlatform(c.Request.Context(), apiKey)
	channelUsageFields := service.ChannelUsageFields{
		OriginalModel:      clientRequestedModel(c, requestModel),
		ChannelMappedModel: requestModel,
	}
	h.submitOpenAIUsageRecordTask(c.Request.Context(), result, func(ctx context.Context) {
		if err := h.gatewayService.RecordUsage(ctx, &service.OpenAIRecordUsageInput{
			Result:             result,
			APIKey:             apiKey,
			User:               apiKey.User,
			Account:            account,
			Subscription:       subscription,
			InboundEndpoint:    inboundEndpoint,
			UpstreamEndpoint:   upstreamEndpoint,
			UserAgent:          userAgent,
			IPAddress:          clientIP,
			RequestPayloadHash: service.HashUsageRequestPayload(body),
			APIKeyService:      h.apiKeyService,
			QuotaPlatform:      quotaPlatform,
			SessionID:          sessionID,
			ChannelUsageFields: channelUsageFields,
		}); err != nil {
			logger.L().With(
				zap.String("component", "handler.openai_gateway.mistral_action"),
				zap.Int64("user_id", subject.UserID),
				zap.Int64("api_key_id", apiKey.ID),
				zap.Any("group_id", apiKey.GroupID),
				zap.String("model", requestModel),
				zap.Int64("account_id", account.ID),
			).Error("mistral_action.record_usage_failed", zap.Error(err))
		}
	})
}

func mistralActionScheduleModel(account *service.Account, requestModel string, result *service.OpenAIForwardResult) string {
	if result != nil && strings.TrimSpace(result.UpstreamModel) != "" {
		return result.UpstreamModel
	}
	if account == nil {
		return strings.TrimSpace(requestModel)
	}
	return account.GetMappedModel(requestModel)
}
