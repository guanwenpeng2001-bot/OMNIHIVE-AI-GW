package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// Volcengine Ark native media/embedding handlers. Evidence: captured official Ark
// model list page (updated 2026.09.17, captured 2026-09-20) which records the
// Video Generation API (docs/82379/1520757), Image Generation API
// Video Generation API (docs/82379/1520757), Image Generation API
// (docs/82379/1541523) and the Embeddings API (text docs/82379/1521765,
// multimodal docs/82379/1409290) with per-model abilities; evidence captured
// 2026-09-21. See service/volcengine_actions.go.

// VolcengineImages handles Seedream generation (doubao-seedream-*) and editing
// (图生图 / 多参考图生图) through Volcengine groups.
func (h *OpenAIGatewayHandler) VolcengineImages(c *gin.Context) {
	endpoint := service.VolcengineMediaEndpointImagesGenerations
	if strings.Contains(c.Request.URL.Path, "/images/edits") {
		endpoint = service.VolcengineMediaEndpointImagesEdits
	}
	h.handleVolcengineMedia(c, endpoint, "")
}

// VolcengineVideoGeneration handles Seedance async video creation.
func (h *OpenAIGatewayHandler) VolcengineVideoGeneration(c *gin.Context) {
	h.handleVolcengineMedia(c, service.VolcengineMediaEndpointVideosGenerations, "")
}

// VolcengineVideoEdit handles Seedance 编辑视频 (文档 82379/1520757).
func (h *OpenAIGatewayHandler) VolcengineVideoEdit(c *gin.Context) {
	h.handleVolcengineMedia(c, service.VolcengineMediaEndpointVideoEdit, "")
}

// VolcengineVideoExtend handles Seedance 延长视频 (文档 82379/1520757).
func (h *OpenAIGatewayHandler) VolcengineVideoExtend(c *gin.Context) {
	h.handleVolcengineMedia(c, service.VolcengineMediaEndpointVideoExtend, "")
}

// VolcengineVideoStatus handles owner-checked Ark video task status polling.
func (h *OpenAIGatewayHandler) VolcengineVideoStatus(c *gin.Context) {
	h.handleVolcengineMedia(c, service.VolcengineMediaEndpointVideoStatus, c.Param("request_id"))
}

// VolcengineVideoContent proxies downloadable video content through the task's bound account.
func (h *OpenAIGatewayHandler) VolcengineVideoContent(c *gin.Context) {
	h.handleVolcengineMedia(c, service.VolcengineMediaEndpointVideoContent, c.Param("request_id"))
}

// VolcengineVideoCancel cancels a queued Ark video task through the task's bound
// account (DELETE /contents/generations/tasks/{id}).
func (h *OpenAIGatewayHandler) VolcengineVideoCancel(c *gin.Context) {
	h.handleVolcengineMedia(c, service.VolcengineMediaEndpointVideoCancel, c.Param("request_id"))
}

// VolcengineEmbeddings handles Ark multimodal embeddings (doubao-embedding-vision-*).
func (h *OpenAIGatewayHandler) VolcengineEmbeddings(c *gin.Context) {
	h.handleVolcengineMedia(c, service.VolcengineMediaEndpointEmbeddings, "")
}

func (h *OpenAIGatewayHandler) handleVolcengineMedia(c *gin.Context, endpoint service.VolcengineMediaEndpoint, taskID string) {
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
		"handler.openai_gateway.volcengine_media",
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
	requestInfo := service.ParseVolcengineMediaRequest(contentType, body)
	requestModel := requestInfo.Model
	if endpoint.IsGenerationRequest() && strings.TrimSpace(requestModel) == "" {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}
	if endpoint == service.VolcengineMediaEndpointEmbeddings && strings.TrimSpace(requestModel) == "" {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}
	if endpoint.IsVideoLookupRequest() && strings.TrimSpace(taskID) == "" {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "request_id is required")
		return
	}

	reqLog = reqLog.With(zap.String("model", requestModel))
	defer beginGatewayRequestPlan(c, h.gatewayService, apiKey, requestModel, "media", h.maxAccountSwitches)()
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
		if endpoint == service.VolcengineMediaEndpointImagesGenerations || endpoint == service.VolcengineMediaEndpointImagesEdits {
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
		reqLog.Info("volcengine_media.billing_eligibility_check_failed", zap.Error(err))
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
		sessionHash = service.VolcengineVideoRequestSessionHash(taskID, subject.UserID, apiKey.ID)
		boundLookupAccountID, err = h.gatewayService.ResolveVolcengineVideoRequestAccount(
			c.Request.Context(), apiKey.GroupID, taskID, subject.UserID, apiKey.ID,
		)
		if err != nil || boundLookupAccountID <= 0 {
			reqLog.Info("volcengine_media.video_lookup_owner_binding_missing", zap.Error(err))
			h.errorResponse(c, http.StatusNotFound, "not_found_error", "Video request not found")
			return
		}
	}
	// Volcengine media (image/video) bills on media tiers and embeddings bill on
	// tokens; both suppress the text-token profit gate so an in-flight task's
	// bound account is never wrongly excluded and returned as a false 404.
	requestCtx := service.WithOpenAIProfitControlSuppressed(c.Request.Context())
	profitVetoCount := 0
	failoverState := NewFailoverState(h.maxAccountSwitches, false)
	videoCreateStartedAt := ""
	if endpoint.IsVideoCreateRequest() {
		videoCreateStartedAt = service.VolcengineVideoPendingCreatedAtNow()
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
			selection, _, err = h.gatewayService.SelectVolcengineVideoRequestAccount(
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
				service.PlatformVolcengine,
			)
		}
		if selection != nil && selection.Acquired {
			selection.ReleaseFunc = wrapReleaseOnDone(requestCtx, selection.ReleaseFunc)
			accountReleaseFunc = selection.ReleaseFunc
		}
		if err != nil {
			if failoverClientGone(c) {
				reqLog.Info("volcengine_media.account_select_aborted_client_disconnected", zap.Error(err))
				return
			}
			if boundLookupAccountID > 0 && errors.Is(err, service.ErrNoAvailableAccounts) {
				h.errorResponse(c, http.StatusNotFound, "not_found_error", "Video request not found")
				return
			}
			reqLog.Warn("volcengine_media.account_select_failed",
				zap.Error(err),
				zap.Int("excluded_account_count", len(failoverState.FailedAccountIDs)),
			)
			if endpoint.IsGenerationRequest() && errors.Is(err, service.ErrNoAvailableAccounts) && len(failoverState.FailedAccountIDs) == 0 {
				markOpsRoutingCapacityLimitedIfNoAvailable(c, err)
				h.errorResponse(c, http.StatusServiceUnavailable, "volcengine_media_no_eligible_account", "No eligible Volcengine media accounts")
				return
			}
			if len(failoverState.FailedAccountIDs) == 0 {
				cls := classifyNoAccountErrorFromGin(c, h.gatewayService, apiKey, requestModel, requestModel, service.PlatformVolcengine)
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
				h.errorResponse(c, http.StatusServiceUnavailable, "volcengine_media_no_eligible_account", "No eligible Volcengine media accounts")
				return
			}
			cls := classifyNoAccountErrorFromGin(c, h.gatewayService, apiKey, requestModel, requestModel, service.PlatformVolcengine)
			if !cls.ModelNotFound {
				markOpsRoutingCapacityLimited(c)
			}
			h.errorResponse(c, cls.Status, cls.ErrType, cls.Message)
			return
		}
		if boundLookupAccountID > 0 && selection.Account.ID != boundLookupAccountID {
			reqLog.Warn("volcengine_media.video_lookup_bound_account_unavailable",
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
			if endpoint == service.VolcengineMediaEndpointEmbeddings {
				return h.gatewayService.ForwardVolcengineEmbeddings(requestCtx, c, account, body, contentType)
			}
			return h.gatewayService.ForwardVolcengineMedia(requestCtx, c, account, endpoint, taskID, body, contentType)
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
					reqLog.Info("volcengine_media.failover_aborted_client_disconnected",
						zap.Int64("account_id", account.ID),
						zap.Int("upstream_status", failoverErr.StatusCode),
					)
					return
				}
				if failoverErr.ShouldReportAccountScheduleFailure() {
					h.gatewayService.ReportOpenAIAccountScheduleResult(account, volcengineMediaScheduleModel(account, requestModel, result), false, nil)
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
				reqLog.Warn("volcengine_media.upstream_failover_switching",
					zap.Int64("account_id", account.ID),
					zap.Int("upstream_status", failoverErr.StatusCode),
					zap.Int("switch_count", failoverState.SwitchCount),
					zap.Int("max_switches", failoverState.MaxSwitches),
				)
				continue
			}
			h.gatewayService.ReportOpenAIAccountScheduleResult(account, volcengineMediaScheduleModel(account, requestModel, result), false, nil)
			if !service.IsResponseCommitted(c) && c.Writer.Size() == writerSizeBeforeForward {
				h.errorResponse(c, http.StatusBadGateway, "upstream_error", "Upstream request failed")
			}
			reqLog.Warn("volcengine_media.forward_failed",
				zap.Int64("account_id", account.ID),
				zap.Error(err),
			)
			return
		}

		h.gatewayService.ReportOpenAIAccountScheduleResult(account, volcengineMediaScheduleModel(account, requestModel, result), true, nil)
		if endpoint.IsVideoCreateRequest() && strings.TrimSpace(result.ResponseID) != "" {
			if err := h.gatewayService.BindVolcengineVideoRequestAccount(
				requestCtx, apiKey.GroupID, result.ResponseID, subject.UserID, apiKey.ID, account.ID,
			); err != nil {
				reqLog.Warn("volcengine_media.bind_video_request_account_failed",
					zap.Int64("account_id", account.ID),
					zap.String("task_id", result.ResponseID),
					zap.Error(err),
				)
			}
			// Defer billing until status polling observes succeeded+content.video_url.
			// Persist create-time model/duration/resolution so status can still price
			// when the task body omits them. Retry once: a missing pending snapshot
			// silently underprices the job.
			pending := service.VolcengineVideoPendingBilling{
				Model:                requestModel,
				BillingModel:         firstNonEmptyString(result.BillingModel, requestModel),
				UpstreamModel:        result.UpstreamModel,
				VideoResolution:      result.VideoResolution,
				VideoDurationSeconds: result.VideoDurationSeconds,
				OriginalModel:        clientRequestedModel(c, requestModel),
				CreatedAt:            videoCreateStartedAt,
			}
			if err := h.gatewayService.StoreVolcengineVideoPendingBilling(requestCtx, result.ResponseID, subject.UserID, apiKey.ID, pending); err != nil {
				reqLog.Warn("volcengine_media.store_video_pending_billing_failed_retrying",
					zap.Int64("account_id", account.ID),
					zap.String("task_id", result.ResponseID),
					zap.Error(err),
				)
				if err2 := h.gatewayService.StoreVolcengineVideoPendingBilling(requestCtx, result.ResponseID, subject.UserID, apiKey.ID, pending); err2 != nil {
					reqLog.Error("volcengine_media.store_video_pending_billing_failed",
						zap.Int64("account_id", account.ID),
						zap.String("task_id", result.ResponseID),
						zap.Error(err2),
					)
				}
			}
		}
		switch endpoint {
		case service.VolcengineMediaEndpointVideoStatus, service.VolcengineMediaEndpointVideoContent:
			// Status poll OR content download can observe succeeded+content.video_url.
			// Both paths share one claim key so the customer is charged once.
			videoTaskID := strings.TrimSpace(taskID)
			if billResult := h.prepareVolcengineVideoCompletionBilling(requestCtx, reqLog, apiKey, subject, videoTaskID, result); billResult != nil {
				h.recordVolcengineUsage(c, reqLog, apiKey, subject, subscription, account, billResult, billResult.Model, body, videoTaskID)
			}
		case service.VolcengineMediaEndpointImagesGenerations, service.VolcengineMediaEndpointImagesEdits:
			if result.ImageCount > 0 && strings.TrimSpace(requestModel) != "" {
				h.recordVolcengineUsage(c, reqLog, apiKey, subject, subscription, account, result, requestModel, body, taskID)
			}
		case service.VolcengineMediaEndpointEmbeddings:
			if result.Usage.InputTokens > 0 || result.Usage.OutputTokens > 0 {
				h.recordVolcengineUsage(c, reqLog, apiKey, subject, subscription, account, result, requestModel, body, taskID)
			}
		}
		reqLog.Debug("volcengine_media.request_completed",
			zap.Int64("account_id", account.ID),
			zap.Int("switch_count", failoverState.SwitchCount),
		)
		return
	}
}

func volcengineMediaScheduleModel(account *service.Account, routingModel string, result *service.OpenAIForwardResult) string {
	if result != nil && strings.TrimSpace(result.UpstreamModel) != "" {
		return result.UpstreamModel
	}
	if account == nil {
		return strings.TrimSpace(routingModel)
	}
	return account.GetMappedModel(routingModel)
}

// prepareVolcengineVideoCompletionBilling claims one-shot billing for an
// official succeeded+content.video_url observation (status poll or content download).
func (h *OpenAIGatewayHandler) prepareVolcengineVideoCompletionBilling(
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
	pending, loadErr := h.gatewayService.LoadVolcengineVideoPendingBilling(ctx, videoTaskID, subject.UserID, apiKey.ID)
	if loadErr != nil {
		reqLog.Warn("volcengine_media.video_pending_billing_load_failed", zap.String("task_id", videoTaskID), zap.Error(loadErr))
	}
	claimed, err := h.gatewayService.ClaimVolcengineVideoBilling(ctx, videoTaskID, subject.UserID, apiKey.ID)
	if err != nil {
		reqLog.Warn("volcengine_media.video_billing_claim_failed", zap.String("task_id", videoTaskID), zap.Error(err))
		return nil
	}
	if !claimed {
		reqLog.Debug("volcengine_media.video_billing_already_claimed", zap.String("task_id", videoTaskID))
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
	merged.RequestID = service.StableVolcengineVideoBillingRequestID(firstNonEmptyString(merged.ResponseID, videoTaskID))
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

func (h *OpenAIGatewayHandler) recordVolcengineUsage(
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
		if stable := service.StableVolcengineVideoBillingRequestID(firstNonEmptyString(result.ResponseID, videoTaskID)); stable != "" {
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
				if releaseErr := h.gatewayService.ReleaseVolcengineVideoBilling(ctx, videoTaskID, subject.UserID, apiKey.ID); releaseErr != nil {
					reqLog.Warn("volcengine_media.video_billing_claim_release_failed",
						zap.String("task_id", videoTaskID),
						zap.Error(releaseErr),
					)
				}
			}
			logger.L().With(
				zap.String("component", "handler.openai_gateway.volcengine_media"),
				zap.Int64("user_id", subject.UserID),
				zap.Int64("api_key_id", apiKey.ID),
				zap.Any("group_id", apiKey.GroupID),
				zap.String("model", requestModel),
				zap.Int64("account_id", account.ID),
			).Error("volcengine_media.record_usage_failed", zap.Error(err))
		}
	})
}
