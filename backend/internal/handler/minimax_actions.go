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
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

// MiniMax native media/voice handlers. Upstream protocol evidence: official
// MiniMax OpenAPI references captured 2026-09-26 (image-01, MiniMax-H3 video V2,
// t2a_v2, speech_to_text asr-1.0, voice_clone). See service/minimax_actions.go.

// MiniMaxImages handles image-01 generation/edits through MiniMax groups.
func (h *OpenAIGatewayHandler) MiniMaxImages(c *gin.Context) {
	endpoint := service.MiniMaxMediaEndpointImagesGenerations
	if strings.Contains(c.Request.URL.Path, "/images/edits") {
		endpoint = service.MiniMaxMediaEndpointImagesEdits
	}
	h.handleMiniMaxMedia(c, endpoint, "")
}

// MiniMaxVideoGeneration handles MiniMax-H3 async video creation.
func (h *OpenAIGatewayHandler) MiniMaxVideoGeneration(c *gin.Context) {
	h.handleMiniMaxMedia(c, service.MiniMaxMediaEndpointVideosGenerations, "")
}

// MiniMaxVideoStatus handles owner-checked MiniMax video task status polling.
func (h *OpenAIGatewayHandler) MiniMaxVideoStatus(c *gin.Context) {
	h.handleMiniMaxMedia(c, service.MiniMaxMediaEndpointVideoStatus, c.Param("request_id"))
}

// MiniMaxVideoContent proxies downloadable video content through the task's bound account.
func (h *OpenAIGatewayHandler) MiniMaxVideoContent(c *gin.Context) {
	h.handleMiniMaxMedia(c, service.MiniMaxMediaEndpointVideoContent, c.Param("request_id"))
}

// MiniMaxVideoCancel cancels a queued task (or deletes a finished record) via
// the official DELETE /v2/video_generation/{task_id}, ownership-checked.
func (h *OpenAIGatewayHandler) MiniMaxVideoCancel(c *gin.Context) {
	h.handleMiniMaxMedia(c, service.MiniMaxMediaEndpointVideoCancel, c.Param("request_id"))
}

func (h *OpenAIGatewayHandler) handleMiniMaxMedia(c *gin.Context, endpoint service.MiniMaxMediaEndpoint, taskID string) {
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
		"handler.openai_gateway.minimax_media",
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
	requestInfo := service.ParseMiniMaxMediaRequest(contentType, body)
	requestModel := requestInfo.Model
	if endpoint.IsGenerationRequest() && strings.TrimSpace(requestModel) == "" {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}
	if endpoint.IsVideoLookupRequest() && strings.TrimSpace(taskID) == "" {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "request_id is required")
		return
	}

	reqLog = reqLog.With(zap.String("model", requestModel))
	defer beginGatewayRequestPlan(c, h.gatewayService, apiKey, requestModel, "media", h.maxAccountSwitches)()
	if c.IsAborted() {
		return
	}
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
		if endpoint == service.MiniMaxMediaEndpointImagesGenerations || endpoint == service.MiniMaxMediaEndpointImagesEdits {
			imageReleaseFunc, acquired := h.acquireImageGenerationSlot(c, streamStarted)
			if !acquired {
				return
			}
			if imageReleaseFunc != nil {
				defer imageReleaseFunc()
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
		reqLog.Info("minimax_media.billing_eligibility_check_failed", zap.Error(err))
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
		sessionHash = service.MiniMaxVideoRequestSessionHash(taskID, subject.UserID, apiKey.ID)
		boundLookupAccountID, err = h.gatewayService.ResolveMiniMaxVideoRequestAccount(
			c.Request.Context(), apiKey.GroupID, taskID, subject.UserID, apiKey.ID,
		)
		if err != nil || boundLookupAccountID <= 0 {
			reqLog.Info("minimax_media.video_lookup_owner_binding_missing", zap.Error(err))
			h.errorResponse(c, http.StatusNotFound, "not_found_error", "Video request not found")
			return
		}
	}
	// MiniMax 媒体（图片/视频）按媒体倍率计费，豁免文本 token 利润门；
	// 同时避免在途任务的绑定账号被利润门排除导致查询返回伪 404。
	requestCtx := service.WithOpenAIProfitControlSuppressed(c.Request.Context())
	profitVetoCount := 0
	failoverState := NewFailoverState(h.maxAccountSwitches, false)
	videoCreateStartedAt := ""
	if endpoint == service.MiniMaxMediaEndpointVideosGenerations {
		videoCreateStartedAt = service.MiniMaxVideoPendingCreatedAtNow()
	}
	routingStart := time.Now()
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
		if boundLookupAccountID > 0 {
			selection, _, err = h.gatewayService.SelectMiniMaxVideoRequestAccount(
				requestCtx, apiKey.GroupID, sessionHash, boundLookupAccountID, requestModel,
			)
		} else {
			selection, _, err = h.gatewayService.SelectAccountWithSchedulerForCapability(
				requestCtx,
				apiKey.GroupID,
				"",
				sessionHash,
				requestModel,
				failoverState.FailedAccountIDs,
				service.OpenAIUpstreamTransportHTTPSSE,
				"",
				false,
				false,
				false,
				service.PlatformMiniMax,
			)
		}
		if selection != nil && selection.Acquired {
			selection.ReleaseFunc = wrapReleaseOnDone(requestCtx, selection.ReleaseFunc)
			accountReleaseFunc = selection.ReleaseFunc
		}
		if err != nil {
			if failoverClientGone(c) {
				reqLog.Info("minimax_media.account_select_aborted_client_disconnected", zap.Error(err))
				return
			}
			if boundLookupAccountID > 0 && errors.Is(err, service.ErrNoAvailableAccounts) {
				h.errorResponse(c, http.StatusNotFound, "not_found_error", "Video request not found")
				return
			}
			reqLog.Warn("minimax_media.account_select_failed",
				zap.Error(err),
				zap.Int("excluded_account_count", len(failoverState.FailedAccountIDs)),
			)
			if endpoint.IsGenerationRequest() && errors.Is(err, service.ErrNoAvailableAccounts) && len(failoverState.FailedAccountIDs) == 0 {
				markOpsRoutingCapacityLimitedIfNoAvailable(c, err)
				h.errorResponse(c, http.StatusServiceUnavailable, "minimax_media_no_eligible_account", "No eligible MiniMax media accounts")
				return
			}
			if len(failoverState.FailedAccountIDs) == 0 {
				cls := classifyNoAccountErrorFromGin(c, h.gatewayService, apiKey, requestModel, requestModel, service.PlatformMiniMax)
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
			if endpoint.IsGenerationRequest() {
				markOpsRoutingCapacityLimited(c)
				h.errorResponse(c, http.StatusServiceUnavailable, "minimax_media_no_eligible_account", "No eligible MiniMax media accounts")
				return
			}
			cls := classifyNoAccountErrorFromGin(c, h.gatewayService, apiKey, requestModel, requestModel, service.PlatformMiniMax)
			if !cls.ModelNotFound {
				markOpsRoutingCapacityLimited(c)
			}
			h.errorResponse(c, cls.Status, cls.ErrType, cls.Message)
			return
		}
		if boundLookupAccountID > 0 && selection.Account.ID != boundLookupAccountID {
			reqLog.Warn("minimax_media.video_lookup_bound_account_unavailable",
				zap.Int64("bound_account_id", boundLookupAccountID),
				zap.Int64("selected_account_id", selection.Account.ID),
			)
			h.errorResponse(c, http.StatusNotFound, "not_found_error", "Video request not found")
			return
		}

		account := selection.Account
		if failoverClientGone(c) {
			return
		}
		sessionHash = ensureOpenAIPoolModeSessionHash(sessionHash, account)
		setOpsSelectedAccount(c, account.ID, account.Platform)

		admissionSessionHash := sessionHash
		if boundLookupAccountID > 0 {
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
		result, err := func() (*service.OpenAIForwardResult, error) {
			defer releaseAccount()
			return h.gatewayService.ForwardMiniMaxMedia(requestCtx, c, account, endpoint, taskID, body, contentType)
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
					reqLog.Info("minimax_media.failover_aborted_client_disconnected",
						zap.Int64("account_id", account.ID),
						zap.Int("upstream_status", failoverErr.StatusCode),
					)
					return
				}
				if failoverErr.ShouldReportAccountScheduleFailure() {
					h.gatewayService.ReportOpenAIAccountScheduleResult(account, minimaxMediaScheduleModel(account, requestModel, result), false, nil)
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
				reqLog.Warn("minimax_media.upstream_failover_switching",
					zap.Int64("account_id", account.ID),
					zap.Int("upstream_status", failoverErr.StatusCode),
					zap.Int("switch_count", failoverState.SwitchCount),
					zap.Int("max_switches", failoverState.MaxSwitches),
				)
				continue
			}
			h.gatewayService.ReportOpenAIAccountScheduleResult(account, minimaxMediaScheduleModel(account, requestModel, result), false, nil)
			if !service.IsResponseCommitted(c) && c.Writer.Size() == writerSizeBeforeForward {
				h.errorResponse(c, http.StatusBadGateway, "upstream_error", "Upstream request failed")
			}
			reqLog.Warn("minimax_media.forward_failed",
				zap.Int64("account_id", account.ID),
				zap.Error(err),
			)
			return
		}

		h.gatewayService.ReportOpenAIAccountScheduleResult(account, minimaxMediaScheduleModel(account, requestModel, result), true, nil)
		if endpoint == service.MiniMaxMediaEndpointVideosGenerations && strings.TrimSpace(result.ResponseID) != "" {
			if err := h.gatewayService.BindMiniMaxVideoRequestAccount(
				requestCtx, apiKey.GroupID, result.ResponseID, subject.UserID, apiKey.ID, account.ID,
			); err != nil {
				reqLog.Warn("minimax_media.bind_video_request_account_failed",
					zap.Int64("account_id", account.ID),
					zap.String("task_id", result.ResponseID),
					zap.Error(err),
				)
			}
			// Defer billing until status polling observes succeeded+content.url.
			// Persist create-time model/duration/resolution so status can still
			// price when the task body omits them. Retry once: a missing pending
			// snapshot silently underprices the job.
			pending := service.MiniMaxVideoPendingBilling{
				Model:                requestModel,
				BillingModel:         firstNonEmptyString(result.BillingModel, requestModel),
				UpstreamModel:        result.UpstreamModel,
				VideoResolution:      result.VideoResolution,
				VideoDurationSeconds: result.VideoDurationSeconds,
				OriginalModel:        clientRequestedModel(c, requestModel),
				CreatedAt:            videoCreateStartedAt,
			}
			if err := h.gatewayService.StoreMiniMaxVideoPendingBilling(requestCtx, result.ResponseID, subject.UserID, apiKey.ID, pending); err != nil {
				reqLog.Warn("minimax_media.store_video_pending_billing_failed_retrying",
					zap.Int64("account_id", account.ID),
					zap.String("task_id", result.ResponseID),
					zap.Error(err),
				)
				if err2 := h.gatewayService.StoreMiniMaxVideoPendingBilling(requestCtx, result.ResponseID, subject.UserID, apiKey.ID, pending); err2 != nil {
					reqLog.Error("minimax_media.store_video_pending_billing_failed",
						zap.Int64("account_id", account.ID),
						zap.String("task_id", result.ResponseID),
						zap.Error(err2),
					)
				}
			}
		}
		switch endpoint {
		case service.MiniMaxMediaEndpointVideoStatus, service.MiniMaxMediaEndpointVideoContent:
			// Status poll OR content download can observe succeeded+content.url.
			// Both paths share one claim key so the customer is charged once.
			videoTaskID := strings.TrimSpace(taskID)
			if billResult := h.prepareMiniMaxVideoCompletionBilling(requestCtx, reqLog, apiKey, subject, videoTaskID, result); billResult != nil {
				h.recordMiniMaxMediaUsage(c, reqLog, apiKey, subject, subscription, account, billResult, billResult.Model, body, videoTaskID)
			}
		case service.MiniMaxMediaEndpointImagesGenerations, service.MiniMaxMediaEndpointImagesEdits:
			if result.ImageCount > 0 && strings.TrimSpace(requestModel) != "" {
				h.recordMiniMaxMediaUsage(c, reqLog, apiKey, subject, subscription, account, result, requestModel, body, taskID)
			}
		}
		reqLog.Debug("minimax_media.request_completed",
			zap.Int64("account_id", account.ID),
			zap.Int("switch_count", failoverState.SwitchCount),
		)
		return
	}
}

func minimaxMediaScheduleModel(account *service.Account, routingModel string, result *service.OpenAIForwardResult) string {
	if result != nil && strings.TrimSpace(result.UpstreamModel) != "" {
		return result.UpstreamModel
	}
	if account == nil {
		return strings.TrimSpace(routingModel)
	}
	return account.GetMappedModel(routingModel)
}

// prepareMiniMaxVideoCompletionBilling claims one-shot billing for an official
// succeeded+content.url observation (status poll or content download).
func (h *OpenAIGatewayHandler) prepareMiniMaxVideoCompletionBilling(
	ctx context.Context,
	reqLog *zap.Logger,
	apiKey *service.APIKey,
	subject middleware2.AuthSubject,
	videoTaskID string,
	statusResult *service.OpenAIForwardResult,
) *service.OpenAIForwardResult {
	if h == nil || h.gatewayService == nil || apiKey == nil || statusResult == nil {
		return nil
	}
	if statusResult.VideoCount <= 0 {
		return nil
	}
	videoTaskID = strings.TrimSpace(firstNonEmptyString(videoTaskID, statusResult.ResponseID))
	if videoTaskID == "" {
		return nil
	}
	pending, loadErr := h.gatewayService.LoadMiniMaxVideoPendingBilling(ctx, videoTaskID, subject.UserID, apiKey.ID)
	if loadErr != nil {
		reqLog.Warn("minimax_media.video_pending_billing_load_failed", zap.String("task_id", videoTaskID), zap.Error(loadErr))
	}
	claimed, err := h.gatewayService.ClaimMiniMaxVideoBilling(ctx, videoTaskID, subject.UserID, apiKey.ID)
	if err != nil {
		reqLog.Warn("minimax_media.video_billing_claim_failed", zap.String("task_id", videoTaskID), zap.Error(err))
		return nil
	}
	if !claimed {
		reqLog.Debug("minimax_media.video_billing_already_claimed", zap.String("task_id", videoTaskID))
		return nil
	}
	merged := *statusResult
	if pending != nil {
		if strings.TrimSpace(merged.Model) == "" {
			merged.Model = firstNonEmptyString(pending.BillingModel, pending.Model, pending.OriginalModel)
		}
		if strings.TrimSpace(merged.BillingModel) == "" {
			merged.BillingModel = firstNonEmptyString(pending.BillingModel, pending.Model, merged.Model)
		}
		if strings.TrimSpace(merged.UpstreamModel) == "" {
			merged.UpstreamModel = pending.UpstreamModel
		}
		if strings.TrimSpace(pending.VideoResolution) != "" {
			merged.VideoResolution = pending.VideoResolution
		}
		if merged.VideoDurationSeconds <= 0 {
			merged.VideoDurationSeconds = pending.VideoDurationSeconds
		}
		if strings.TrimSpace(merged.ResponseID) == "" {
			merged.ResponseID = videoTaskID
		}
	}
	if strings.TrimSpace(merged.Model) == "" {
		merged.Model = "MiniMax-H3"
	}
	if strings.TrimSpace(merged.BillingModel) == "" {
		merged.BillingModel = merged.Model
	}
	merged.RequestID = service.StableMiniMaxVideoBillingRequestID(firstNonEmptyString(merged.ResponseID, videoTaskID))
	merged.ResponseID = firstNonEmptyString(merged.ResponseID, videoTaskID)
	merged.VideoCount = 1
	merged.ImageCount = 0
	merged.VideoResolution = service.NormalizeVideoBillingResolutionOrDefault(merged.VideoResolution)
	merged.VideoDurationSeconds = service.NormalizeVideoBillingDurationSecondsOrDefault(merged.VideoDurationSeconds)
	if pending != nil {
		if e2e := service.GrokVideoE2EDuration(pending.CreatedAt, time.Now()); e2e > 0 {
			merged.Duration = e2e
		}
	}
	return &merged
}

func (h *OpenAIGatewayHandler) recordMiniMaxMediaUsage(
	c *gin.Context,
	reqLog *zap.Logger,
	apiKey *service.APIKey,
	subject middleware2.AuthSubject,
	subscription *service.UserSubscription,
	account *service.Account,
	result *service.OpenAIForwardResult,
	requestModel string,
	body []byte,
	videoTaskID string,
) {
	userAgent := c.GetHeader("User-Agent")
	clientIP := ip.GetClientIP(c)
	sessionID := service.ExtractClientSessionID(c)
	payloadForHash := body
	if len(payloadForHash) == 0 && strings.TrimSpace(videoTaskID) != "" {
		payloadForHash = []byte(videoTaskID)
	}
	inboundEndpoint := GetInboundEndpoint(c)
	upstreamEndpoint := GetUpstreamEndpoint(c, account.Platform)
	quotaPlatform := service.QuotaPlatform(c.Request.Context(), apiKey)
	channelUsageFields := service.ChannelUsageFields{
		OriginalModel:      clientRequestedModel(c, requestModel),
		ChannelMappedModel: requestModel,
	}
	if result != nil && result.VideoCount > 0 {
		if stable := service.StableMiniMaxVideoBillingRequestID(firstNonEmptyString(result.ResponseID, videoTaskID)); stable != "" {
			result.RequestID = stable
		}
	}
	h.submitMandatoryUsageRecordTask(c.Request.Context(), func(ctx context.Context) {
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
			if strings.TrimSpace(videoTaskID) != "" {
				if releaseErr := h.gatewayService.ReleaseMiniMaxVideoBilling(ctx, videoTaskID, subject.UserID, apiKey.ID); releaseErr != nil {
					reqLog.Warn("minimax_media.video_billing_claim_release_failed",
						zap.String("task_id", videoTaskID),
						zap.Error(releaseErr),
					)
				}
			}
			logger.L().With(
				zap.String("component", "handler.openai_gateway.minimax_media"),
				zap.Int64("user_id", subject.UserID),
				zap.Int64("api_key_id", apiKey.ID),
				zap.Any("group_id", apiKey.GroupID),
				zap.String("model", requestModel),
				zap.Int64("account_id", account.ID),
			).Error("minimax_media.record_usage_failed", zap.Error(err))
		}
	})
}

// MiniMaxVoice handles MiniMax voice endpoints: "tts" (t2a_v2), "stt"
// (speech_to_text), and "custom-voices" (voice_clone management).
func (h *OpenAIGatewayHandler) MiniMaxVoice(c *gin.Context, endpoint string) {
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

	body, err := readGrokVoiceGatewayBody(c)
	if err != nil {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	reqLog := requestLogger(c, "handler.openai_gateway.minimax_voice", zap.String("endpoint", endpoint))
	if endpoint == "tts" {
		// t2a_v2 bodies carry {"text"|"input":"..."}; normalize to chat messages
		// so content moderation extractors see the spoken text.
		auditBody := body
		if input := extractGrokTTSInputText(body); input != "" {
			if rewritten, jsonErr := json.Marshal(map[string]any{
				"messages": []map[string]any{{"role": "user", "content": input}},
			}); jsonErr == nil {
				auditBody = rewritten
			}
		}
		if decision := h.checkSecurityAudit(c, reqLog, apiKey, subject, service.ContentModerationProtocolOpenAIChat, "minimax-speech", auditBody); decision != nil && !decision.AllowNextStage {
			h.openAISecurityAuditError(c, decision)
			return
		}
	}
	contentType := c.GetHeader("Content-Type")
	if strings.TrimSpace(contentType) == "" {
		contentType = "application/json"
	}

	// Select on the real upstream model so catalog evidence and model mapping
	// route correctly: TTS takes the request model (default speech-2.8-hd), STT
	// has the single official asr-1.0, and voice management rides the account's
	// universal text model (it is not model-bound upstream).
	selectionModel := "MiniMax-M2"
	switch endpoint {
	case "tts":
		if requested := strings.TrimSpace(gjson.GetBytes(body, "model").String()); requested != "" {
			selectionModel = requested
		} else {
			selectionModel = "speech-2.8-hd"
		}
	case "stt":
		selectionModel = "asr-1.0"
	}
	defer beginGatewayRequestPlan(c, h.gatewayService, apiKey, selectionModel, "audio", h.maxAccountSwitches)()
	if c.IsAborted() {
		return
	}
	// 入口前置 hold：媒体/音频/realtime/异步 job 在调用上游之前先按估算占用周期预算，
	// 结算时按实际消耗修正；没有周期预算的 Key 不产生额外工作（T3-2.2）。
	if !reserveGatewayRequestBudget(c, apiKey, selectionModel, body) {
		return
	}
	if h.errorPassthroughService != nil {
		service.BindErrorPassthroughService(c, h.errorPassthroughService)
	}

	failoverState := NewFailoverState(h.maxAccountSwitches, false)
	failed := failoverState.FailedAccountIDs
	for attempts := 0; attempts <= h.maxAccountSwitches; attempts++ {
		selection, _, selectErr := h.gatewayService.SelectAccountWithSchedulerForCapability(
			c.Request.Context(),
			apiKey.GroupID,
			"",
			"",
			selectionModel,
			failed,
			service.OpenAIUpstreamTransportHTTPSSE,
			"",
			false,
			false,
			false,
			service.PlatformMiniMax,
		)
		if selectErr != nil || selection == nil || selection.Account == nil {
			if failoverState.LastFailoverErr != nil {
				h.handleFailoverExhausted(c, failoverState.LastFailoverErr, false)
			} else {
				h.errorResponse(c, http.StatusServiceUnavailable, "api_error", "No available MiniMax accounts")
			}
			return
		}
		account := selection.Account
		var started bool
		release, status := h.acquireResponsesAccountSlot(c, apiKey.GroupID, "", selection, false, &started, reqLog)
		if status == openAISlotAcquireProfitVetoed {
			failed[account.ID] = struct{}{}
			continue
		}
		if status != openAISlotAcquireOK {
			if status == openAISlotAcquireFailed && len(failed) == 0 {
				return
			}
			failed[account.ID] = struct{}{}
			continue
		}
		result, forwardErr := func() (*service.OpenAIForwardResult, error) {
			defer release()
			return h.gatewayService.ForwardMiniMaxVoice(c.Request.Context(), c, account, endpoint, body, contentType)
		}()
		if forwardErr == nil {
			h.recordMiniMaxVoiceUsage(c, apiKey, account, subscription, endpoint, body, result)
			return
		}
		var failoverErr *service.UpstreamFailoverError
		if errors.As(forwardErr, &failoverErr) && failoverErr.ShouldRetryNextAccount() {
			if failoverState.HandleFailoverError(c.Request.Context(), nil, account.ID, account.Platform, 0, failoverErr) != FailoverContinue {
				h.handleFailoverExhausted(c, failoverErr, false)
				return
			}
			continue
		}
		// Non-failover errors already wrote the response (client error or committed upstream error).
		return
	}
	if failoverState.LastFailoverErr != nil {
		h.handleFailoverExhausted(c, failoverState.LastFailoverErr, false)
	}
}

// recordMiniMaxVoiceUsage bills TTS/STT via group audio prices when AudioUsage is set.
func (h *OpenAIGatewayHandler) recordMiniMaxVoiceUsage(
	c *gin.Context,
	apiKey *service.APIKey,
	account *service.Account,
	subscription *service.UserSubscription,
	endpoint string,
	body []byte,
	result *service.OpenAIForwardResult,
) {
	if h == nil || c == nil || apiKey == nil || account == nil || result == nil {
		return
	}
	if result.AudioUsage == nil {
		return
	}
	result.RequestID = service.StableMiniMaxAudioBillingRequestID(result.RequestID)
	userAgent := c.GetHeader("User-Agent")
	clientIP := ip.GetClientIP(c)
	sessionID := service.ExtractClientSessionID(c)
	requestPayloadHash := service.HashUsageRequestPayload(body)
	if requestPayloadHash == "" {
		requestPayloadHash = service.HashUsageRequestPayload([]byte(endpoint))
	}
	inboundEndpoint := GetInboundEndpoint(c)
	upstreamEndpoint := GetUpstreamEndpoint(c, account.Platform)
	quotaPlatform := service.QuotaPlatform(c.Request.Context(), apiKey)
	model := strings.TrimSpace(result.Model)
	if model == "" {
		model = endpoint
	}

	h.submitMandatoryUsageRecordTask(c.Request.Context(), func(ctx context.Context) {
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
			RequestPayloadHash: requestPayloadHash,
			APIKeyService:      h.apiKeyService,
			QuotaPlatform:      quotaPlatform,
			SessionID:          sessionID,
			ChannelUsageFields: clientRequestedUsageFields(c, service.ChannelMappingResult{}, model, result.UpstreamModel),
		}); err != nil {
			logger.L().With(
				zap.String("component", "handler.openai_gateway.minimax_voice"),
				zap.Int64("user_id", apiKey.User.ID),
				zap.Int64("api_key_id", apiKey.ID),
				zap.Any("group_id", apiKey.GroupID),
				zap.String("endpoint", endpoint),
				zap.Int64("account_id", account.ID),
			).Error("minimax_voice.record_usage_failed", zap.Error(err))
		}
	})
}
