package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// Zhipu GLM native actions (docs.bigmodel.cn open platform, /api/paas/v4).
// The dispatch table and routes are wired by multimodal-ingress; these entry
// points assume an authenticated key whose (resolved) target platform is zhipu.

// ZhipuImages handles image.generate through Zhipu groups (glm-image, cogview-4 family).
func (h *OpenAIGatewayHandler) ZhipuImages(c *gin.Context) {
	h.handleZhipuAction(c, service.ZhipuEndpointImagesGenerations, "")
}

// ZhipuVideoGeneration handles video.generate task submission (cogvideox family).
func (h *OpenAIGatewayHandler) ZhipuVideoGeneration(c *gin.Context) {
	h.handleZhipuAction(c, service.ZhipuEndpointVideosGenerations, "")
}

// ZhipuVideoStatus handles video.status via GET /paas/v4/async-result/{id}.
func (h *OpenAIGatewayHandler) ZhipuVideoStatus(c *gin.Context) {
	h.handleZhipuAction(c, service.ZhipuEndpointAsyncResult, c.Param("request_id"))
}

// ZhipuVideoContent proxies the completed task's CDN download through the owner-bound account.
func (h *OpenAIGatewayHandler) ZhipuVideoContent(c *gin.Context) {
	h.handleZhipuAction(c, service.ZhipuEndpointVideoContent, c.Param("request_id"))
}

// ZhipuEmbeddings handles embeddings (embedding-3 / embedding-2).
func (h *OpenAIGatewayHandler) ZhipuEmbeddings(c *gin.Context) {
	h.handleZhipuAction(c, service.ZhipuEndpointEmbeddings, "")
}

// ZhipuRerank handles rerank (model "rerank").
func (h *OpenAIGatewayHandler) ZhipuRerank(c *gin.Context) {
	h.handleZhipuAction(c, service.ZhipuEndpointRerank, "")
}

// ZhipuAudioSpeech handles audio.tts (glm-tts), returning audio bytes.
func (h *OpenAIGatewayHandler) ZhipuAudioSpeech(c *gin.Context) {
	h.handleZhipuAction(c, service.ZhipuEndpointAudioSpeech, "")
}

// ZhipuAudioTranscriptions handles audio.stt (glm-asr-2512), multipart upload.
func (h *OpenAIGatewayHandler) ZhipuAudioTranscriptions(c *gin.Context) {
	h.handleZhipuAction(c, service.ZhipuEndpointAudioTranscriptions, "")
}

// ZhipuVoiceClone handles voice cloning (POST /paas/v4/voice/clone).
func (h *OpenAIGatewayHandler) ZhipuVoiceClone(c *gin.Context) {
	h.handleZhipuAction(c, service.ZhipuEndpointVoiceClone, "")
}

// ZhipuVoiceList handles voice listing (GET /paas/v4/voice/list).
func (h *OpenAIGatewayHandler) ZhipuVoiceList(c *gin.Context) {
	h.handleZhipuAction(c, service.ZhipuEndpointVoiceList, "")
}

// ZhipuVoiceDelete handles voice deletion (POST /paas/v4/voice/delete).
func (h *OpenAIGatewayHandler) ZhipuVoiceDelete(c *gin.Context) {
	h.handleZhipuAction(c, service.ZhipuEndpointVoiceDelete, "")
}

// ZhipuWebSearch handles search (POST /paas/v4/web_search).
func (h *OpenAIGatewayHandler) ZhipuWebSearch(c *gin.Context) {
	h.handleZhipuAction(c, service.ZhipuEndpointWebSearch, "")
}

func zhipuActionRequiredCapability(endpoint service.ZhipuActionEndpoint) service.OpenAIEndpointCapability {
	if endpoint == service.ZhipuEndpointEmbeddings {
		return service.OpenAIEndpointCapabilityEmbeddings
	}
	return ""
}

func (h *OpenAIGatewayHandler) handleZhipuAction(c *gin.Context, endpoint service.ZhipuActionEndpoint, taskID string) {
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
		"handler.openai_gateway.zhipu_action",
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
	requestInfo := service.ParseZhipuActionRequest(endpoint, contentType, body)
	requestModel := requestInfo.Model
	if (endpoint.IsGenerationRequest() || endpoint == service.ZhipuEndpointEmbeddings || endpoint == service.ZhipuEndpointRerank) && strings.TrimSpace(requestModel) == "" {
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
	if endpoint == service.ZhipuEndpointAudioSpeech && strings.TrimSpace(requestInfo.InputText) != "" {
		// Moderation extractors see chat messages; synthesize one from the spoken text.
		auditBody := zhipuTTSAuditBody(requestInfo.InputText)
		auditModel := requestModel
		if auditModel == "" {
			auditModel = "glm-tts"
		}
		decision := h.checkSecurityAudit(c, reqLog, apiKey, subject, service.ContentModerationProtocolOpenAIChat, auditModel, auditBody)
		if decision != nil && !decision.AllowNextStage {
			h.openAISecurityAuditError(c, decision)
			return
		}
	}
	if endpoint == service.ZhipuEndpointEmbeddings {
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
		reqLog.Info("zhipu_action.billing_eligibility_check_failed", zap.Error(err))
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
		sessionHash = service.ZhipuVideoTaskSessionHash(taskID, subject.UserID, apiKey.ID)
		boundLookupAccountID, err = h.gatewayService.ResolveZhipuVideoTaskAccount(
			c.Request.Context(), apiKey.GroupID, taskID, subject.UserID, apiKey.ID,
		)
		if err != nil || boundLookupAccountID <= 0 {
			reqLog.Info("zhipu_action.video_lookup_owner_binding_missing", zap.Error(err))
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
	requiredCapability := zhipuActionRequiredCapability(endpoint)
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
		var selection *service.AccountSelectionResult
		var scheduleDecision service.OpenAIAccountScheduleDecision
		if boundLookupAccountID > 0 {
			selection, scheduleDecision, err = h.gatewayService.SelectZhipuVideoTaskAccount(
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
				service.PlatformZhipu,
			)
		}
		// Own an eagerly acquired slot before any rejection.
		if selection != nil && selection.Acquired {
			selection.ReleaseFunc = wrapReleaseOnDone(requestCtx, selection.ReleaseFunc)
			accountReleaseFunc = selection.ReleaseFunc
		}
		if err != nil {
			if failoverClientGone(c) {
				reqLog.Info("zhipu_action.account_select_aborted_client_disconnected", zap.Error(err))
				return
			}
			if boundLookupAccountID > 0 && errors.Is(err, service.ErrNoAvailableAccounts) {
				h.errorResponse(c, http.StatusNotFound, "not_found_error", "Video request not found")
				return
			}
			reqLog.Warn("zhipu_action.account_select_failed",
				zap.Error(err),
				zap.Int("excluded_account_count", len(failoverState.FailedAccountIDs)),
			)
			if len(failoverState.FailedAccountIDs) == 0 {
				cls := classifyNoAccountErrorFromGin(c, h.gatewayService, apiKey, requestModel, requestModel, service.PlatformZhipu)
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
			cls := classifyNoAccountErrorFromGin(c, h.gatewayService, apiKey, requestModel, requestModel, service.PlatformZhipu)
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
			reqLog.Warn("zhipu_action.video_lookup_bound_account_unavailable",
				zap.Int64("bound_account_id", boundLookupAccountID),
				zap.Int64("selected_account_id", selection.Account.ID),
			)
			h.errorResponse(c, http.StatusNotFound, "not_found_error", "Video request not found")
			return
		}

		reqLog.Debug("zhipu_action.account_schedule_decision",
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
			// Media path is exempt from the profit gate; defensive fallback only.
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
			videoCreateStartedAt = service.ZhipuVideoPendingCreatedAtNow()
		}
		result, err := func() (*service.OpenAIForwardResult, error) {
			defer releaseAccount()
			return h.gatewayService.ForwardZhipuAction(requestCtx, c, account, endpoint, taskID, body, contentType, c.Request.URL.RawQuery)
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
					reqLog.Info("zhipu_action.failover_aborted_client_disconnected",
						zap.Int64("account_id", account.ID),
						zap.Int("upstream_status", failoverErr.StatusCode),
					)
					return
				}
				if failoverErr.ShouldReportAccountScheduleFailure() {
					h.gatewayService.ReportOpenAIAccountScheduleResult(account, zhipuActionScheduleModel(account, requestModel, result), false, nil)
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
				reqLog.Warn("zhipu_action.upstream_failover_switching",
					zap.Int64("account_id", account.ID),
					zap.Int("upstream_status", failoverErr.StatusCode),
					zap.Int("switch_count", failoverState.SwitchCount),
				)
				continue
			}
			h.gatewayService.ReportOpenAIAccountScheduleResult(account, zhipuActionScheduleModel(account, requestModel, result), false, nil)
			if !service.IsResponseCommitted(c) && c.Writer.Size() == writerSizeBeforeForward {
				h.errorResponse(c, http.StatusBadGateway, "upstream_error", "Upstream request failed")
			}
			reqLog.Warn("zhipu_action.forward_failed",
				zap.Int64("account_id", account.ID),
				zap.Error(err),
			)
			return
		}

		h.gatewayService.ReportOpenAIAccountScheduleResult(account, zhipuActionScheduleModel(account, requestModel, result), true, nil)
		if endpoint.IsVideoCreateRequest() && result != nil && strings.TrimSpace(result.ResponseID) != "" {
			if err := h.gatewayService.BindZhipuVideoTaskAccount(
				requestCtx, apiKey.GroupID, result.ResponseID, subject.UserID, apiKey.ID, account.ID,
			); err != nil {
				reqLog.Warn("zhipu_action.bind_video_task_account_failed",
					zap.Int64("account_id", account.ID),
					zap.String("task_id", result.ResponseID),
					zap.Error(err),
				)
			}
			// Defer billing until a status/content poll observes SUCCESS+video_result.
			// The status body carries neither duration nor size, so snapshot the
			// create-time pricing evidence now (retry once: a missing pending
			// silently underprices the deferred completion).
			pending := service.BuildZhipuVideoPendingBilling(
				requestInfo,
				zhipuFirstNonEmpty(result.BillingModel, requestModel),
				result.UpstreamModel,
				clientRequestedModel(c, requestModel),
				videoCreateStartedAt,
			)
			if err := h.gatewayService.StoreZhipuVideoPendingBilling(requestCtx, result.ResponseID, subject.UserID, apiKey.ID, pending); err != nil {
				reqLog.Warn("zhipu_action.store_video_pending_billing_failed_retrying",
					zap.Int64("account_id", account.ID),
					zap.String("task_id", result.ResponseID),
					zap.Error(err),
				)
				if err2 := h.gatewayService.StoreZhipuVideoPendingBilling(requestCtx, result.ResponseID, subject.UserID, apiKey.ID, pending); err2 != nil {
					reqLog.Error("zhipu_action.store_video_pending_billing_failed",
						zap.Int64("account_id", account.ID),
						zap.String("task_id", result.ResponseID),
						zap.Error(err2),
					)
				}
			}
		}
		if endpoint == service.ZhipuEndpointAsyncResult || endpoint == service.ZhipuEndpointVideoContent {
			if billResult := h.prepareZhipuVideoCompletionBilling(requestCtx, reqLog, apiKey, subject, taskID, result); billResult != nil {
				h.recordZhipuActionUsage(c, reqLog, apiKey, subject, subscription, account, billResult, billResult.Model, body, taskID)
			}
		} else if shouldRecordZhipuActionUsage(endpoint, requestModel, result) {
			h.recordZhipuActionUsage(c, reqLog, apiKey, subject, subscription, account, result, requestModel, body, taskID)
		}
		reqLog.Debug("zhipu_action.request_completed",
			zap.Int64("account_id", account.ID),
			zap.Int("switch_count", failoverState.SwitchCount),
		)
		return
	}
}

// shouldRecordZhipuActionUsage gates immediate usage writes. Async video
// create never bills here — status/content polls claim completion billing.
func shouldRecordZhipuActionUsage(endpoint service.ZhipuActionEndpoint, requestModel string, result *service.OpenAIForwardResult) bool {
	if result == nil {
		return false
	}
	switch endpoint {
	case service.ZhipuEndpointImagesGenerations:
		return result.ImageCount > 0
	case service.ZhipuEndpointEmbeddings, service.ZhipuEndpointRerank:
		return result.Usage.InputTokens > 0 || result.Usage.OutputTokens > 0
	case service.ZhipuEndpointAudioSpeech, service.ZhipuEndpointAudioTranscriptions:
		return result.AudioUsage != nil
	case service.ZhipuEndpointWebSearch:
		return result.WebSearchCalls > 0
	default:
		return false
	}
}

// prepareZhipuVideoCompletionBilling claims one-shot billing for the first
// observed SUCCESS+video_result (status poll or content download). Without
// the create-time snapshot the task cannot be priced honestly (the official
// status body carries no duration/size), so billing fails closed.
func (h *OpenAIGatewayHandler) prepareZhipuVideoCompletionBilling(
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
	taskID = strings.TrimSpace(zhipuFirstNonEmpty(taskID, statusResult.ResponseID))
	if taskID == "" {
		return nil
	}
	pending, loadErr := h.gatewayService.LoadZhipuVideoPendingBilling(ctx, taskID, subject.UserID, apiKey.ID)
	if loadErr != nil {
		reqLog.Warn("zhipu_action.video_pending_billing_load_failed", zap.String("task_id", taskID), zap.Error(loadErr))
	}
	if pending == nil {
		reqLog.Error("zhipu_action.video_billing_skipped_missing_pending",
			zap.String("task_id", taskID),
			zap.String("reason", "no create-time snapshot; status body has no duration/size"),
		)
		return nil
	}
	claimed, err := h.gatewayService.ClaimZhipuVideoBilling(ctx, taskID, subject.UserID, apiKey.ID)
	if err != nil {
		reqLog.Warn("zhipu_action.video_billing_claim_failed", zap.String("task_id", taskID), zap.Error(err))
		return nil
	}
	if !claimed {
		reqLog.Debug("zhipu_action.video_billing_already_claimed", zap.String("task_id", taskID))
		return nil
	}
	merged := service.MergeZhipuVideoPendingBilling(statusResult, pending, taskID)
	merged.VideoCount = 1
	merged.ImageCount = 0
	merged.RequestID = service.StableZhipuVideoBillingRequestID(taskID)
	merged.ResponseID = zhipuFirstNonEmpty(merged.ResponseID, taskID)
	return merged
}

// recordZhipuActionUsage writes one usage record and releases the video
// billing claim when durable recording fails so a later poll can retry.
func (h *OpenAIGatewayHandler) recordZhipuActionUsage(
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
		videoTaskID = strings.TrimSpace(zhipuFirstNonEmpty(taskID, result.ResponseID))
		if stable := service.StableZhipuVideoBillingRequestID(zhipuFirstNonEmpty(result.ResponseID, taskID)); stable != "" {
			result.RequestID = stable
		}
		if len(body) == 0 && videoTaskID != "" {
			payloadForHash = []byte(videoTaskID)
		}
	}
	if result != nil && result.AudioUsage != nil {
		if mode := strings.TrimSpace(result.AudioUsage.Mode); mode == "realtime" {
			result.RequestID = service.StableZhipuRealtimeBillingRequestID(result.RequestID)
		} else {
			result.RequestID = service.StableZhipuAudioBillingRequestID(result.RequestID)
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
				if releaseErr := h.gatewayService.ReleaseZhipuVideoBilling(ctx, videoTaskID, subject.UserID, apiKey.ID); releaseErr != nil {
					reqLog.Warn("zhipu_action.video_billing_claim_release_failed",
						zap.String("task_id", videoTaskID),
						zap.Error(releaseErr),
					)
				}
			}
			logger.L().With(
				zap.String("component", "handler.openai_gateway.zhipu_action"),
				zap.Int64("user_id", subject.UserID),
				zap.Int64("api_key_id", apiKey.ID),
				zap.Any("group_id", apiKey.GroupID),
				zap.String("model", requestModel),
				zap.Int64("account_id", account.ID),
			).Error("zhipu_action.record_usage_failed", zap.Error(err))
		}
	})
}

func zhipuActionScheduleModel(account *service.Account, requestModel string, result *service.OpenAIForwardResult) string {
	if result != nil && strings.TrimSpace(result.UpstreamModel) != "" {
		return result.UpstreamModel
	}
	if account == nil {
		return strings.TrimSpace(requestModel)
	}
	return account.GetMappedModel(requestModel)
}

func zhipuFirstNonEmpty(values ...string) string {
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

func zhipuTTSAuditBody(input string) []byte {
	payload := map[string]any{
		"messages": []map[string]any{{"role": "user", "content": input}},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil
	}
	return body
}

// ZhipuRealtime exposes GLM-Realtime over WebSocket (wss /paas/v4/realtime).
func (h *OpenAIGatewayHandler) ZhipuRealtime(c *gin.Context) {
	if c == nil || c.Request == nil || !isOpenAIWSUpgradeRequest(c.Request) {
		h.errorResponse(c, http.StatusUpgradeRequired, "invalid_request_error", "WebSocket upgrade required (Upgrade: websocket)")
		return
	}
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok || service.GatewayTargetPlatform(c) != service.PlatformZhipu {
		h.errorResponse(c, http.StatusNotFound, "not_found_error", "Realtime API is not supported for this platform")
		return
	}
	if !h.ensureResponsesDependencies(c, nil) {
		return
	}
	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	if err := h.billingCacheService.CheckBillingEligibility(c.Request.Context(), apiKey.User, apiKey, apiKey.Group, subscription, service.QuotaPlatform(c.Request.Context(), apiKey)); err != nil {
		status, code, message, _ := billingErrorDetails(err)
		writeAdmissionRejectionHeaders(c, err)
		h.errorResponse(c, status, code, message)
		return
	}

	reqLog := requestLogger(c, "handler.openai_gateway.zhipu_realtime")
	// The session model travels inside the client's session.update event; the
	// query parameter is a label for plan/billing only.
	model := c.Query("model")
	if strings.TrimSpace(model) == "" {
		model = "glm-realtime"
	}
	defer beginGatewayRequestPlan(c, h.gatewayService, apiKey, model, "realtime", h.maxAccountSwitches)()
	// 入口前置 hold：媒体/音频/realtime/异步 job 在调用上游之前先按估算占用周期预算，
	// 结算时按实际消耗修正；没有周期预算的 Key 不产生额外工作（T3-2.2）。
	if !reserveGatewayRequestBudget(c, apiKey, model, nil) {
		return
	}

	failed := map[int64]struct{}{}
	var selection *service.AccountSelectionResult
	var release func()
	var upstream *service.ZhipuRealtimeUpstream
	var candidateSeen bool
	for attempts := 0; attempts <= h.maxAccountSwitches; attempts++ {
		candidate, _, selectErr := h.gatewayService.SelectAccountWithSchedulerForCapability(
			c.Request.Context(), apiKey.GroupID, "", "", "", failed,
			service.OpenAIUpstreamTransportHTTPSSE,
			service.OpenAIEndpointCapabilityChatCompletions,
			false, false, false, service.PlatformZhipu,
		)
		if selectErr != nil || candidate == nil || candidate.Account == nil {
			break
		}
		candidateSeen = true
		account := candidate.Account
		var streamStarted bool
		var slotStatus openAISlotAcquireResult
		release, slotStatus = h.acquireResponsesAccountSlot(c, apiKey.GroupID, "", candidate, false, &streamStarted, reqLog)
		if slotStatus != openAISlotAcquireOK {
			if slotStatus == openAISlotAcquireFailed {
				return
			}
			failed[account.ID] = struct{}{}
			continue
		}
		token, _, credErr := h.gatewayService.GetRequestCredential(c.Request.Context(), c, account)
		if credErr != nil {
			release()
			release = nil
			failed[account.ID] = struct{}{}
			continue
		}
		probeCtx, cancelProbe := context.WithTimeout(c.Request.Context(), service.DefaultZhipuRealtimeDialTimeout)
		candidateUpstream, openErr := h.gatewayService.OpenZhipuRealtime(probeCtx, account, token)
		cancelProbe()
		if openErr != nil {
			reqLog.Warn("zhipu_realtime.pre_accept_failed", zap.Int64("account_id", account.ID), zap.Error(openErr))
			h.gatewayService.ReportOpenAIAccountScheduleResult(account, model, false, nil)
			release()
			release = nil
			failed[account.ID] = struct{}{}
			continue
		}
		selection, upstream = candidate, candidateUpstream
		break
	}
	if selection == nil || selection.Account == nil || release == nil || upstream == nil {
		if !candidateSeen {
			h.errorResponse(c, http.StatusServiceUnavailable, "api_error", "No available Zhipu accounts")
		} else {
			h.errorResponse(c, http.StatusBadGateway, "upstream_error", "Zhipu realtime upstream unavailable")
		}
		return
	}
	defer release()
	defer func() { _ = upstream.Close() }()

	conn, err := coderws.Accept(c.Writer, c.Request, &coderws.AcceptOptions{CompressionMode: coderws.CompressionContextTakeover})
	if err != nil {
		return
	}
	defer func() { _ = conn.CloseNow() }()

	started := time.Now()
	audioObserved, proxyErr := h.gatewayService.ProxyZhipuRealtimeConn(c.Request.Context(), c, conn, upstream)
	elapsed := time.Since(started)
	if proxyErr != nil {
		reqLog.Info("zhipu_realtime.proxy_failed", zap.Error(proxyErr))
		if !isExpectedZhipuRealtimeClose(proxyErr) {
			_ = conn.Close(coderws.StatusInternalError, "upstream realtime websocket failed")
			return
		}
	}
	if result := zhipuRealtimeBillingResult(model, elapsed, audioObserved); result != nil {
		subject, _ := middleware2.GetAuthSubjectFromContext(c)
		h.recordZhipuActionUsage(c, reqLog, apiKey, subject, subscription, selection.Account, result, model, nil, "")
	}
}

func zhipuRealtimeBillingResult(model string, elapsed time.Duration, audioObserved bool) *service.OpenAIForwardResult {
	if !audioObserved || elapsed <= 0 {
		return nil
	}
	return &service.OpenAIForwardResult{
		RequestID:  service.StableZhipuRealtimeBillingRequestID(""),
		Model:      model,
		Duration:   elapsed,
		AudioUsage: &service.AudioUsage{Mode: "realtime", DurationOrUnits: elapsed.Minutes()},
	}
}

func isExpectedZhipuRealtimeClose(err error) bool {
	if err == nil {
		return true
	}
	switch coderws.CloseStatus(err) {
	case coderws.StatusNormalClosure, coderws.StatusGoingAway,
		coderws.StatusNoStatusRcvd, coderws.StatusAbnormalClosure:
		return true
	default:
		return false
	}
}
