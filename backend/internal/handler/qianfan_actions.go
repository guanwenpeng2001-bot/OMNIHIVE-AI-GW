package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	pkghttputil "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/httputil"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/ip"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/logger"
	middleware2 "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"go.uber.org/zap"
)

// Qianfan native actions (Baidu ModelBuilder / 百度智能云千帆, + Baidu speech
// products). The dispatch table and routes are wired by multimodal-ingress;
// these entry points assume an authenticated key whose (resolved) target
// platform is qianfan.

// QianfanEmbeddings handles embeddings (POST /v2/embeddings).
func (h *OpenAIGatewayHandler) QianfanEmbeddings(c *gin.Context) {
	h.handleQianfanAction(c, service.QianfanEndpointEmbeddings, "")
}

// QianfanRerank handles rerank (POST /v2/rerank).
func (h *OpenAIGatewayHandler) QianfanRerank(c *gin.Context) {
	h.handleQianfanAction(c, service.QianfanEndpointRerank, "")
}

// QianfanImages handles image.generate (POST /v2/images/generations).
func (h *OpenAIGatewayHandler) QianfanImages(c *gin.Context) {
	h.handleQianfanAction(c, service.QianfanEndpointImagesGenerations, "")
}

// QianfanImageEdit handles image.edit (POST /v2/images/edits).
func (h *OpenAIGatewayHandler) QianfanImageEdit(c *gin.Context) {
	h.handleQianfanAction(c, service.QianfanEndpointImagesEdits, "")
}

// QianfanVideoGeneration handles video.generate task submission
// (POST /video/generations, musesteamer image-to-video family).
func (h *OpenAIGatewayHandler) QianfanVideoGeneration(c *gin.Context) {
	h.handleQianfanAction(c, service.QianfanEndpointVideoGenerate, "")
}

// QianfanVideoStatus handles video.status (GET /video/generations?task_id=).
func (h *OpenAIGatewayHandler) QianfanVideoStatus(c *gin.Context) {
	h.handleQianfanAction(c, service.QianfanEndpointVideoStatus, c.Param("request_id"))
}

// QianfanVideoContent proxies the completed task's video download through the
// owner-bound account.
func (h *OpenAIGatewayHandler) QianfanVideoContent(c *gin.Context) {
	h.handleQianfanAction(c, service.QianfanEndpointVideoContent, c.Param("request_id"))
}

// QianfanVideoCancel handles video task cancellation
// (DELETE /video/generations?task_id=).
func (h *OpenAIGatewayHandler) QianfanVideoCancel(c *gin.Context) {
	h.handleQianfanAction(c, service.QianfanEndpointVideoCancel, c.Param("request_id"))
}

// QianfanSearch handles search (POST /v2/ai_search/web_search). It
// authenticates through the AppBuilder header, not the ModelBuilder bearer.
func (h *OpenAIGatewayHandler) QianfanSearch(c *gin.Context) {
	h.handleQianfanAction(c, service.QianfanEndpointSearchWebSearch, "")
}

// QianfanAudioSpeech handles audio.tts (POST https://tsn.baidu.com/text2audio).
func (h *OpenAIGatewayHandler) QianfanAudioSpeech(c *gin.Context) {
	h.handleQianfanAction(c, service.QianfanEndpointAudioSpeech, "")
}

// QianfanVoiceCloneCreate handles voice enrollment
// (POST aip.baidubce.com …/voice/clone/create).
func (h *OpenAIGatewayHandler) QianfanVoiceCloneCreate(c *gin.Context) {
	h.handleQianfanAction(c, service.QianfanEndpointVoiceCloneCreate, "")
}

// QianfanVoiceCloneList handles voice listing (…/voice/clone/list).
func (h *OpenAIGatewayHandler) QianfanVoiceCloneList(c *gin.Context) {
	h.handleQianfanAction(c, service.QianfanEndpointVoiceCloneList, "")
}

func qianfanActionRequiredCapability(endpoint service.QianfanActionEndpoint) service.OpenAIEndpointCapability {
	if endpoint == service.QianfanEndpointEmbeddings {
		return service.OpenAIEndpointCapabilityEmbeddings
	}
	return ""
}

func (h *OpenAIGatewayHandler) handleQianfanAction(c *gin.Context, endpoint service.QianfanActionEndpoint, taskID string) {
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
		"handler.openai_gateway.qianfan_action",
		zap.Int64("user_id", subject.UserID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.Any("group_id", apiKey.GroupID),
		zap.String("endpoint", string(endpoint)),
	)
	if !h.ensureResponsesDependencies(c, reqLog) {
		return
	}

	var body []byte
	var err error
	if endpoint.RequiresRequestBody() {
		body, err = pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
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
	}

	contentType := c.GetHeader("Content-Type")
	requestInfo := service.ParseQianfanActionRequest(endpoint, contentType, body)
	requestModel := requestInfo.Model
	if endpoint.IsGenerationRequest() && strings.TrimSpace(requestModel) == "" {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}
	if (endpoint == service.QianfanEndpointEmbeddings || endpoint == service.QianfanEndpointRerank) && strings.TrimSpace(requestModel) == "" {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}
	if endpoint.IsVideoLookupRequest() && strings.TrimSpace(taskID) == "" {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "request_id is required")
		return
	}

	reqLog = reqLog.With(zap.String("model", requestModel))
	defer beginGatewayRequestPlan(c, h.gatewayService, apiKey, requestModel, endpoint.PlanProtocol(), h.maxAccountSwitches)()
	// 入口前置 hold：媒体/音频/realtime/异步 job 在调用上游之前先按估算占用周期预算，
	// 结算时按实际消耗修正；没有周期预算的 Key 不产生额外工作（T3-2.2）。
	if !reserveGatewayRequestBudget(c, apiKey, requestModel, body) {
		return
	}
	setOpsRequestContext(c, requestModel, false)
	setOpsEndpointContext(c, "", int16(service.RequestTypeSync))

	if endpoint.IsGenerationRequest() {
		if !service.GroupAllowsImageGeneration(apiKey.Group) {
			h.errorResponse(c, http.StatusForbidden, "permission_error", service.ImageGenerationPermissionMessage())
			return
		}
		if moderationBody := requestInfo.ModerationBody(); len(moderationBody) > 0 {
			decision := h.checkSecurityAudit(c, reqLog, apiKey, subject, service.ContentModerationProtocolOpenAIImages, requestModel, moderationBody)
			if decision != nil && !decision.AllowNextStage {
				h.openAISecurityAuditError(c, decision)
				return
			}
		}
		imageReleaseFunc, acquired := h.acquireImageGenerationSlot(c, streamStarted)
		if !acquired {
			return
		}
		if imageReleaseFunc != nil {
			defer imageReleaseFunc()
		}
	}
	if endpoint == service.QianfanEndpointAudioSpeech && strings.TrimSpace(requestInfo.InputText) != "" {
		auditBody := qianfanSpeechAuditBody(requestInfo.InputText)
		auditModel := requestModel
		if auditModel == "" {
			auditModel = "qianfan-tts"
		}
		decision := h.checkSecurityAudit(c, reqLog, apiKey, subject, service.ContentModerationProtocolOpenAIChat, auditModel, auditBody)
		if decision != nil && !decision.AllowNextStage {
			h.openAISecurityAuditError(c, decision)
			return
		}
	}
	if endpoint == service.QianfanEndpointSearchWebSearch {
		if decision := h.checkSecurityAudit(c, reqLog, apiKey, subject, "openai_chat", requestModel, body); decision != nil && !decision.AllowNextStage {
			h.openAISecurityAuditError(c, decision)
			return
		}
	}
	if endpoint == service.QianfanEndpointEmbeddings || endpoint == service.QianfanEndpointRerank {
		if decision := h.checkSecurityAudit(c, reqLog, apiKey, subject, "openai_embeddings", requestModel, body); decision != nil && !decision.AllowNextStage {
			h.openAISecurityAuditError(c, decision)
			return
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
		reqLog.Info("qianfan_action.billing_eligibility_check_failed", zap.Error(err))
		status, code, message, _ := billingErrorDetails(err)
		writeAdmissionRejectionHeaders(c, err)
		h.errorResponse(c, status, code, message)
		return
	}

	sessionSeed := body
	if len(sessionSeed) == 0 && strings.TrimSpace(taskID) != "" {
		sessionSeed = []byte(taskID)
	}
	sessionHash := h.gatewayService.GenerateExplicitSessionHash(c, sessionSeed)
	boundLookupAccountID := int64(0)
	if endpoint.IsVideoLookupRequest() {
		sessionHash = service.QianfanVideoTaskSessionHash(taskID, subject.UserID, apiKey.ID)
		boundLookupAccountID, err = h.gatewayService.ResolveQianfanVideoTaskAccount(
			c.Request.Context(), apiKey.GroupID, taskID, subject.UserID, apiKey.ID,
		)
		if err != nil || boundLookupAccountID <= 0 {
			reqLog.Info("qianfan_action.video_lookup_owner_binding_missing", zap.Error(err))
			h.errorResponse(c, http.StatusNotFound, "not_found_error", "Video request not found")
			return
		}
	}

	// Media/search/audio actions bill per unit, not per token profit gate:
	// suppress the text profit gate like the Grok media path does.
	requestCtx := service.WithOpenAIProfitControlSuppressed(c.Request.Context())
	profitVetoCount := 0
	failoverState := NewFailoverState(h.maxAccountSwitches, false)
	routingStart := time.Now()
	requiredCapability := qianfanActionRequiredCapability(endpoint)
	var accountReleaseFunc func()
	releaseAccount := func() {
		if accountReleaseFunc != nil {
			accountReleaseFunc()
			accountReleaseFunc = nil
		}
	}
	defer releaseAccount()

	for iter := 0; ; iter++ {
		releaseAccount()
		if failoverClientGone(c) {
			return
		}
		var selection *service.AccountSelectionResult
		var scheduleDecision service.OpenAIAccountScheduleDecision
		if boundLookupAccountID > 0 {
			selection, scheduleDecision, err = h.gatewayService.SelectQianfanVideoTaskAccount(
				requestCtx, apiKey.GroupID, sessionHash, boundLookupAccountID, requestModel,
			)
		} else {
			selection, scheduleDecision, err = h.gatewayService.SelectAccountWithSchedulerForCapability(
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
				service.PlatformQianfan,
			)
		}
		if selection != nil && selection.Acquired {
			selection.ReleaseFunc = wrapReleaseOnDone(requestCtx, selection.ReleaseFunc)
			accountReleaseFunc = selection.ReleaseFunc
		}
		if err != nil {
			if failoverClientGone(c) {
				reqLog.Info("qianfan_action.account_select_aborted_client_disconnected", zap.Error(err))
				return
			}
			if boundLookupAccountID > 0 && errors.Is(err, service.ErrNoAvailableAccounts) {
				h.errorResponse(c, http.StatusNotFound, "not_found_error", "Video request not found")
				return
			}
			reqLog.Warn("qianfan_action.account_select_failed",
				zap.Error(err),
				zap.Int("excluded_account_count", len(failoverState.FailedAccountIDs)),
			)
			if len(failoverState.FailedAccountIDs) == 0 {
				cls := classifyNoAccountErrorFromGin(c, h.gatewayService, apiKey, requestModel, requestModel, service.PlatformQianfan)
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
			cls := classifyNoAccountErrorFromGin(c, h.gatewayService, apiKey, requestModel, requestModel, service.PlatformQianfan)
			if !cls.ModelNotFound {
				markOpsRoutingCapacityLimited(c)
			}
			h.errorResponse(c, cls.Status, cls.ErrType, cls.Message)
			return
		}
		if failoverClientGone(c) {
			return
		}
		if boundLookupAccountID > 0 && selection.Account.ID != boundLookupAccountID {
			reqLog.Warn("qianfan_action.video_lookup_bound_account_unavailable",
				zap.Int64("bound_account_id", boundLookupAccountID),
				zap.Int64("selected_account_id", selection.Account.ID),
			)
			h.errorResponse(c, http.StatusNotFound, "not_found_error", "Video request not found")
			return
		}

		reqLog.Debug("qianfan_action.account_schedule_decision",
			zap.String("layer", scheduleDecision.Layer),
			zap.Bool("sticky_session_hit", scheduleDecision.StickySessionHit),
			zap.Int("candidate_count", scheduleDecision.CandidateCount),
		)

		account := selection.Account
		sessionHash = ensureOpenAIPoolModeSessionHash(sessionHash, account)
		setOpsSelectedAccount(c, account.ID, account.Platform)

		admissionSessionHash := sessionHash
		if boundLookupAccountID > 0 {
			// Waiting must not replace the video ownership TTL with text sticky TTL.
			admissionSessionHash = ""
		}
		var slotResult openAISlotAcquireResult
		accountReleaseFunc, slotResult = h.acquireResponsesAccountSlot(c, apiKey.GroupID, admissionSessionHash, selection, false, &streamStarted, reqLog)
		if slotResult == openAISlotAcquireProfitVetoed {
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
		videoCreateStartedAt := ""
		if endpoint.IsVideoCreateRequest() {
			videoCreateStartedAt = service.QianfanVideoPendingCreatedAtNow()
		}
		result, err := func() (*service.OpenAIForwardResult, error) {
			defer releaseAccount()
			return h.gatewayService.ForwardQianfanAction(requestCtx, c, account, endpoint, taskID, body, contentType, c.Request.URL.RawQuery)
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
					reqLog.Info("qianfan_action.failover_aborted_client_disconnected",
						zap.Int64("account_id", account.ID),
						zap.Int("upstream_status", failoverErr.StatusCode),
					)
					return
				}
				if failoverErr.ShouldReportAccountScheduleFailure() {
					h.gatewayService.ReportOpenAIAccountScheduleResult(account, qianfanActionScheduleModel(account, requestModel, result), false, nil)
				}
				if c.Writer.Size() != writerSizeBeforeForward {
					h.handleFailoverExhausted(c, failoverErr, true)
					return
				}
				if !failoverErr.ShouldRetryNextAccount() {
					h.handleFailoverExhausted(c, failoverErr, false)
					return
				}
				if endpoint.IsVideoLookupRequest() {
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
				reqLog.Warn("qianfan_action.upstream_failover_switching",
					zap.Int64("account_id", account.ID),
					zap.Int("upstream_status", failoverErr.StatusCode),
					zap.Int("switch_count", failoverState.SwitchCount),
				)
				continue
			}
			h.gatewayService.ReportOpenAIAccountScheduleResult(account, qianfanActionScheduleModel(account, requestModel, result), false, nil)
			if !service.IsResponseCommitted(c) && c.Writer.Size() == writerSizeBeforeForward {
				h.errorResponse(c, http.StatusBadGateway, "upstream_error", "Upstream request failed")
			}
			reqLog.Warn("qianfan_action.forward_failed",
				zap.Int64("account_id", account.ID),
				zap.Error(err),
			)
			return
		}

		h.gatewayService.ReportOpenAIAccountScheduleResult(account, qianfanActionScheduleModel(account, requestModel, result), true, nil)
		if endpoint.IsVideoCreateRequest() && result != nil && strings.TrimSpace(result.ResponseID) != "" {
			if err := h.gatewayService.BindQianfanVideoTaskAccount(
				requestCtx, apiKey.GroupID, result.ResponseID, subject.UserID, apiKey.ID, account.ID,
			); err != nil {
				reqLog.Warn("qianfan_action.bind_video_task_account_failed",
					zap.Int64("account_id", account.ID),
					zap.String("task_id", result.ResponseID),
					zap.Error(err),
				)
			}
			pending := service.BuildQianfanVideoPendingBilling(
				requestInfo,
				qianfanFirstNonEmpty(result.BillingModel, requestModel),
				result.UpstreamModel,
				videoCreateStartedAt,
			)
			if err := h.gatewayService.StoreQianfanVideoPendingBilling(requestCtx, result.ResponseID, subject.UserID, apiKey.ID, pending); err != nil {
				reqLog.Warn("qianfan_action.store_video_pending_billing_failed_retrying",
					zap.Int64("account_id", account.ID),
					zap.String("task_id", result.ResponseID),
					zap.Error(err),
				)
				if err2 := h.gatewayService.StoreQianfanVideoPendingBilling(requestCtx, result.ResponseID, subject.UserID, apiKey.ID, pending); err2 != nil {
					reqLog.Error("qianfan_action.store_video_pending_billing_failed",
						zap.Int64("account_id", account.ID),
						zap.String("task_id", result.ResponseID),
						zap.Error(err2),
					)
				}
			}
		}
		if endpoint == service.QianfanEndpointVideoStatus || endpoint == service.QianfanEndpointVideoContent {
			if billResult := h.prepareQianfanVideoCompletionBilling(requestCtx, reqLog, apiKey, subject, taskID, result); billResult != nil {
				h.recordQianfanActionUsage(c, reqLog, apiKey, subject, subscription, account, billResult, billResult.Model, body, taskID)
			}
		} else if shouldRecordQianfanActionUsage(endpoint, requestModel, result) {
			h.recordQianfanActionUsage(c, reqLog, apiKey, subject, subscription, account, result, requestModel, body, taskID)
		}
		reqLog.Debug("qianfan_action.request_completed",
			zap.Int64("account_id", account.ID),
			zap.Int("switch_count", failoverState.SwitchCount),
		)
		return
	}
}

// shouldRecordQianfanActionUsage gates immediate usage writes. Async video
// create never bills here — status/content polls claim completion billing.
func shouldRecordQianfanActionUsage(endpoint service.QianfanActionEndpoint, requestModel string, result *service.OpenAIForwardResult) bool {
	if result == nil {
		return false
	}
	switch endpoint {
	case service.QianfanEndpointImagesGenerations, service.QianfanEndpointImagesEdits:
		return result.ImageCount > 0
	case service.QianfanEndpointEmbeddings, service.QianfanEndpointRerank:
		return result.Usage.InputTokens > 0 || result.Usage.OutputTokens > 0
	case service.QianfanEndpointAudioSpeech:
		return result.AudioUsage != nil
	case service.QianfanEndpointSearchWebSearch:
		return result.WebSearchCalls > 0
	case service.QianfanEndpointVoiceCloneCreate:
		return strings.TrimSpace(result.ResponseID) != ""
	default:
		return false
	}
}

// prepareQianfanVideoCompletionBilling claims one-shot billing for the first
// observed succeeded task. Without the create-time snapshot the task cannot be
// priced honestly, so billing fails closed.
func (h *OpenAIGatewayHandler) prepareQianfanVideoCompletionBilling(
	ctx context.Context,
	reqLog *zap.Logger,
	apiKey *service.APIKey,
	subject middleware2.AuthSubject,
	taskID string,
	statusResult *service.OpenAIForwardResult,
) *service.OpenAIForwardResult {
	if h == nil || h.gatewayService == nil || apiKey == nil || statusResult == nil {
		return nil
	}
	if statusResult.VideoCount <= 0 {
		return nil
	}
	taskID = strings.TrimSpace(qianfanFirstNonEmpty(taskID, statusResult.ResponseID))
	if taskID == "" {
		return nil
	}
	pending, loadErr := h.gatewayService.LoadQianfanVideoPendingBilling(ctx, taskID, subject.UserID, apiKey.ID)
	if loadErr != nil {
		reqLog.Warn("qianfan_action.video_pending_billing_load_failed", zap.String("task_id", taskID), zap.Error(loadErr))
	}
	if pending == nil {
		reqLog.Error("qianfan_action.video_billing_skipped_missing_pending",
			zap.String("task_id", taskID),
			zap.String("reason", "no create-time snapshot"),
		)
		return nil
	}
	claimed, err := h.gatewayService.ClaimQianfanVideoBilling(ctx, taskID, subject.UserID, apiKey.ID)
	if err != nil {
		reqLog.Warn("qianfan_action.video_billing_claim_failed", zap.String("task_id", taskID), zap.Error(err))
		return nil
	}
	if !claimed {
		reqLog.Debug("qianfan_action.video_billing_already_claimed", zap.String("task_id", taskID))
		return nil
	}
	merged := service.MergeQianfanVideoPendingBilling(statusResult, pending, taskID)
	merged.VideoCount = 1
	merged.ImageCount = 0
	merged.RequestID = service.StableQianfanVideoBillingRequestID(taskID)
	merged.ResponseID = qianfanFirstNonEmpty(merged.ResponseID, taskID)
	return merged
}

// recordQianfanActionUsage writes one usage record and releases the video
// billing claim when durable recording fails so a later poll can retry.
func (h *OpenAIGatewayHandler) recordQianfanActionUsage(
	c *gin.Context,
	reqLog *zap.Logger,
	apiKey *service.APIKey,
	subject middleware2.AuthSubject,
	subscription *service.UserSubscription,
	account *service.Account,
	result *service.OpenAIForwardResult,
	requestModel string,
	body []byte,
	taskID string,
) {
	userAgent := c.GetHeader("User-Agent")
	clientIP := ip.GetClientIP(c)
	sessionID := service.ExtractClientSessionID(c)
	payloadForHash := body
	if len(payloadForHash) == 0 && strings.TrimSpace(taskID) != "" {
		payloadForHash = []byte(taskID)
	}
	inboundEndpoint := GetInboundEndpoint(c)
	upstreamEndpoint := GetUpstreamEndpoint(c, account.Platform)
	quotaPlatform := service.QuotaPlatform(c.Request.Context(), apiKey)
	channelUsageFields := service.ChannelUsageFields{
		OriginalModel:      clientRequestedModel(c, requestModel),
		ChannelMappedModel: requestModel,
	}
	videoTaskID := ""
	if result != nil && result.VideoCount > 0 {
		videoTaskID = strings.TrimSpace(qianfanFirstNonEmpty(taskID, result.ResponseID))
		if stable := service.StableQianfanVideoBillingRequestID(qianfanFirstNonEmpty(result.ResponseID, taskID)); stable != "" {
			result.RequestID = stable
		}
		if len(body) == 0 && videoTaskID != "" {
			payloadForHash = []byte(videoTaskID)
		}
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
			RequestPayloadHash: service.HashUsageRequestPayload(payloadForHash),
			APIKeyService:      h.apiKeyService,
			QuotaPlatform:      quotaPlatform,
			SessionID:          sessionID,
			ChannelUsageFields: channelUsageFields,
		}); err != nil {
			if videoTaskID != "" {
				if releaseErr := h.gatewayService.ReleaseQianfanVideoBilling(ctx, videoTaskID, subject.UserID, apiKey.ID); releaseErr != nil {
					reqLog.Warn("qianfan_action.video_billing_claim_release_failed",
						zap.String("task_id", videoTaskID),
						zap.Error(releaseErr),
					)
				}
			}
			logger.L().With(
				zap.String("component", "handler.openai_gateway.qianfan_action"),
				zap.Int64("user_id", subject.UserID),
				zap.Int64("api_key_id", apiKey.ID),
				zap.Any("group_id", apiKey.GroupID),
				zap.String("model", requestModel),
				zap.Int64("account_id", account.ID),
			).Error("qianfan_action.record_usage_failed", zap.Error(err))
		}
	})
}

func qianfanActionScheduleModel(account *service.Account, requestModel string, result *service.OpenAIForwardResult) string {
	if result != nil && strings.TrimSpace(result.UpstreamModel) != "" {
		return result.UpstreamModel
	}
	if account == nil {
		return strings.TrimSpace(requestModel)
	}
	return account.GetMappedModel(requestModel)
}

func qianfanFirstNonEmpty(values ...string) string {
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

func qianfanSpeechAuditBody(input string) []byte {
	payload := map[string]any{
		"messages": []map[string]any{{"role": "user", "content": input}},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil
	}
	return body
}
