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

// Tencent Hunyuan / TokenHub native actions (Tencent Cloud product 1823).
// Wire shapes follow the saved first-party TokenHub documentation captured on
// 2026-09-21 (1823/135745 image, /137202 video, /135791-/135792 speech
// recognition, /135796 speech synthesis, /133515 vector, /130078 protocol).
//
// The dispatch table and routes are wired by multimodal-ingress; these entry
// points assume an authenticated key whose resolved target platform is hunyuan.

// HunyuanImageGeneration handles image.generate through the synchronous
// Hy-Image-3.0 endpoint (POST /wand/hunyuan-image/v3-generation).
func (h *OpenAIGatewayHandler) HunyuanImageGeneration(c *gin.Context) {
	h.handleHunyuanAction(c, service.HunyuanEndpointImageGeneration, "")
}

// HunyuanImageEdit handles image.edit. TokenHub serves no OpenAI /images/edits
// shape; the documented reference-image ("参考生图") form of the same
// Hy-Image-3.0 endpoint accepts 0-3 source images plus a prompt and returns the
// identical data[].url envelope, so edit requests are mapped onto it.
func (h *OpenAIGatewayHandler) HunyuanImageEdit(c *gin.Context) {
	h.handleHunyuanAction(c, service.HunyuanEndpointImageGeneration, "")
}

// HunyuanImagePanorama handles the native Hy-World-2.1 360 panorama image
// endpoint (POST /wand/hunyuan-image/world2-panorama).
func (h *OpenAIGatewayHandler) HunyuanImagePanorama(c *gin.Context) {
	h.handleHunyuanAction(c, service.HunyuanEndpointImagePanorama, "")
}

// HunyuanVideoGeneration submits an asynchronous Hy-Video-1.5 task
// (POST /wand/hunyuan-video/generation) and returns the vendor task id.
func (h *OpenAIGatewayHandler) HunyuanVideoGeneration(c *gin.Context) {
	h.handleHunyuanAction(c, service.HunyuanEndpointVideoGeneration, "")
}

// HunyuanVideoScene submits the asynchronous Hy-World-2.1 3D scene task.
func (h *OpenAIGatewayHandler) HunyuanVideoScene(c *gin.Context) {
	h.handleHunyuanAction(c, service.HunyuanEndpointVideoScene, "")
}

// HunyuanVideoStatus reads one owner-bound task (GET /wand/hunyuan-video/tasks/{id}).
func (h *OpenAIGatewayHandler) HunyuanVideoStatus(c *gin.Context) {
	h.handleHunyuanAction(c, service.HunyuanEndpointVideoTask, c.Param("task_id"))
}

// HunyuanVideoContent streams the settled video through the owner-bound task.
func (h *OpenAIGatewayHandler) HunyuanVideoContent(c *gin.Context) {
	h.handleHunyuanAction(c, service.HunyuanEndpointVideoContent, c.Param("task_id"))
}

// HunyuanAudioTranscriptions handles the synchronous speech recognition
// endpoint (POST /wand/asrproxy/sync_transcribe).
func (h *OpenAIGatewayHandler) HunyuanAudioTranscriptions(c *gin.Context) {
	h.handleHunyuanAction(c, service.HunyuanEndpointTranscribeSync, "")
}

// HunyuanAudioTranscriptionCreate submits the asynchronous recognition task.
func (h *OpenAIGatewayHandler) HunyuanAudioTranscriptionCreate(c *gin.Context) {
	h.handleHunyuanAction(c, service.HunyuanEndpointTranscribeAsync, "")
}

// HunyuanAudioTranscriptionTask reads one owner-bound recognition task.
func (h *OpenAIGatewayHandler) HunyuanAudioTranscriptionTask(c *gin.Context) {
	h.handleHunyuanAction(c, service.HunyuanEndpointTranscribeTask, c.Param("task_id"))
}

// HunyuanAudioSpeech handles synchronous speech synthesis
// (POST /wand/minimax-tts/sync_tts).
func (h *OpenAIGatewayHandler) HunyuanAudioSpeech(c *gin.Context) {
	h.handleHunyuanAction(c, service.HunyuanEndpointSpeechSync, "")
}

// HunyuanVoiceClone handles voice creation from a reference audio sample
// (POST /wand/minimax-tts/sync_clone, model minimax-voice-clone).
func (h *OpenAIGatewayHandler) HunyuanVoiceClone(c *gin.Context) {
	h.handleHunyuanAction(c, service.HunyuanEndpointVoiceClone, "")
}

// HunyuanEmbeddings handles the OpenAI-compatible text embedding endpoint.
func (h *OpenAIGatewayHandler) HunyuanEmbeddings(c *gin.Context) {
	h.handleHunyuanAction(c, service.HunyuanEndpointTextEmbeddings, "")
}

// HunyuanMultimodalEmbeddings handles the native fused text/image/video
// embedding endpoint (POST /embeddings/multimodal).
func (h *OpenAIGatewayHandler) HunyuanMultimodalEmbeddings(c *gin.Context) {
	h.handleHunyuanAction(c, service.HunyuanEndpointMultimodalEmbeddings, "")
}

// Hunyuan3DSubmit submits the HY-3D task (POST /api/3d/submit).
func (h *OpenAIGatewayHandler) Hunyuan3DSubmit(c *gin.Context) {
	h.handleHunyuanAction(c, service.HunyuanEndpoint3DSubmit, "")
}

// Hunyuan3DQuery reads one HY-3D task (POST /api/3d/query with {model, id}).
func (h *OpenAIGatewayHandler) Hunyuan3DQuery(c *gin.Context) {
	h.handleHunyuanAction(c, service.HunyuanEndpoint3DQuery, "")
}

// hunyuanActionRequiredCapability keeps embedding requests on accounts whose
// transport advertises embedding support; every other native family shares the
// chat-completions capability because TokenHub serves them from the same host
// and credential.
func hunyuanActionRequiredCapability(endpoint service.HunyuanActionEndpoint) service.OpenAIEndpointCapability {
	switch endpoint {
	case service.HunyuanEndpointTextEmbeddings, service.HunyuanEndpointMultimodalEmbeddings:
		return service.OpenAIEndpointCapabilityEmbeddings
	default:
		return ""
	}
}

func (h *OpenAIGatewayHandler) handleHunyuanAction(c *gin.Context, endpoint service.HunyuanActionEndpoint, taskID string) {
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
		"handler.openai_gateway.hunyuan_action",
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
	requestInfo := service.ParseHunyuanActionRequest(endpoint, contentType, body)
	requestModel := requestInfo.Model
	if endpoint.IsTaskLookupRequest() {
		// The HY-3D query carries its task id in the body; path-based lookups
		// (video, speech recognition) keep the route parameter.
		taskID = hunyuanFirstNonEmpty(taskID, requestInfo.TaskID)
	}
	if hunyuanActionRequiresModel(endpoint) && strings.TrimSpace(requestModel) == "" {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}
	if endpoint.IsTaskLookupRequest() && strings.TrimSpace(taskID) == "" {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "task_id is required")
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

	if endpoint.IsImageGenerationRequest() {
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
	if (endpoint == service.HunyuanEndpointSpeechSync || endpoint == service.HunyuanEndpointVoiceClone) &&
		strings.TrimSpace(requestInfo.InputText) != "" {
		// Moderation extractors read chat messages; synthesize one from the text.
		auditModel := requestModel
		if auditModel == "" {
			auditModel = "minimax-speech-2.8-hd"
		}
		decision := h.checkSecurityAudit(c, reqLog, apiKey, subject, service.ContentModerationProtocolOpenAIChat, auditModel, hunyuanTTSAuditBody(requestInfo.InputText))
		if decision != nil && !decision.AllowNextStage {
			h.openAISecurityAuditError(c, decision)
			return
		}
	}
	if endpoint == service.HunyuanEndpointTextEmbeddings || endpoint == service.HunyuanEndpointMultimodalEmbeddings {
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
		reqLog.Info("hunyuan_action.billing_eligibility_check_failed", zap.Error(err))
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
	// Owner-bound task reads resolve through the stored binding only: the model
	// written in the read request must not re-filter the bound account, and the
	// HY-3D query names its model in the body purely for the vendor contract.
	routeModel := requestModel
	boundLookupAccountID := int64(0)
	if endpoint.IsTaskLookupRequest() {
		routeModel = ""
		sessionHash = service.HunyuanTaskSessionHash(taskID, subject.UserID, apiKey.ID)
		boundLookupAccountID, err = h.gatewayService.ResolveHunyuanTaskAccount(
			c.Request.Context(), apiKey.GroupID, taskID, subject.UserID, apiKey.ID,
		)
		if err != nil || boundLookupAccountID <= 0 {
			reqLog.Info("hunyuan_action.task_lookup_owner_binding_missing", zap.Error(err))
			h.errorResponse(c, http.StatusNotFound, "not_found_error", "Task not found")
			return
		}
	}

	// Media, audio and search actions bill per unit, not per token profit gate:
	// suppress the text profit gate the way the Grok media path does.
	requestCtx := service.WithOpenAIProfitControlSuppressed(c.Request.Context())
	profitVetoCount := 0
	failoverState := NewFailoverState(h.maxAccountSwitches, false)
	routingStart := time.Now()
	requiredCapability := hunyuanActionRequiredCapability(endpoint)
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
			selection, scheduleDecision, err = h.gatewayService.SelectHunyuanTaskAccount(
				requestCtx, apiKey.GroupID, sessionHash, boundLookupAccountID, routeModel,
			)
		} else {
			selection, scheduleDecision, err = h.gatewayService.SelectAccountWithSchedulerForCapability(
				requestCtx,
				apiKey.GroupID,
				"",
				sessionHash,
				routeModel,
				failoverState.FailedAccountIDs,
				service.OpenAIUpstreamTransportHTTPSSE,
				requiredCapability,
				false,
				false,
				false,
				service.PlatformHunyuan,
			)
		}
		// Own an eagerly acquired slot before any rejection.
		if selection != nil && selection.Acquired {
			selection.ReleaseFunc = wrapReleaseOnDone(requestCtx, selection.ReleaseFunc)
			accountReleaseFunc = selection.ReleaseFunc
		}
		if err != nil {
			if failoverClientGone(c) {
				reqLog.Info("hunyuan_action.account_select_aborted_client_disconnected", zap.Error(err))
				return
			}
			if boundLookupAccountID > 0 && errors.Is(err, service.ErrNoAvailableAccounts) {
				h.errorResponse(c, http.StatusNotFound, "not_found_error", "Task not found")
				return
			}
			reqLog.Warn("hunyuan_action.account_select_failed",
				zap.Error(err),
				zap.Int("excluded_account_count", len(failoverState.FailedAccountIDs)),
			)
			if len(failoverState.FailedAccountIDs) == 0 {
				cls := classifyNoAccountErrorFromGin(c, h.gatewayService, apiKey, requestModel, requestModel, service.PlatformHunyuan)
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
			cls := classifyNoAccountErrorFromGin(c, h.gatewayService, apiKey, requestModel, requestModel, service.PlatformHunyuan)
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
			reqLog.Warn("hunyuan_action.task_lookup_bound_account_unavailable",
				zap.Int64("bound_account_id", boundLookupAccountID),
				zap.Int64("selected_account_id", selection.Account.ID),
			)
			h.errorResponse(c, http.StatusNotFound, "not_found_error", "Task not found")
			return
		}

		reqLog.Debug("hunyuan_action.account_schedule_decision",
			zap.String("layer", scheduleDecision.Layer),
			zap.Bool("sticky_session_hit", scheduleDecision.StickySessionHit),
			zap.Int("candidate_count", scheduleDecision.CandidateCount),
		)

		account := selection.Account
		sessionHash = ensureOpenAIPoolModeSessionHash(sessionHash, account)
		setOpsSelectedAccount(c, account.ID, account.Platform)

		admissionSessionHash := sessionHash
		if boundLookupAccountID > 0 {
			// Waiting must not replace the task ownership TTL with the text sticky TTL.
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
		taskCreateStartedAt := ""
		if endpoint.IsTaskCreateRequest() {
			taskCreateStartedAt = service.HunyuanTaskPendingCreatedAtNow()
		}
		result, err := func() (*service.OpenAIForwardResult, error) {
			defer releaseAccount()
			return h.gatewayService.ForwardHunyuanAction(requestCtx, c, account, endpoint, taskID, body, contentType, c.Request.URL.RawQuery)
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
					reqLog.Info("hunyuan_action.failover_aborted_client_disconnected",
						zap.Int64("account_id", account.ID),
						zap.Int("upstream_status", failoverErr.StatusCode),
					)
					return
				}
				if failoverErr.ShouldReportAccountScheduleFailure() {
					h.gatewayService.ReportOpenAIAccountScheduleResult(account, hunyuanActionScheduleModel(account, requestModel, result), false, nil)
				}
				if c.Writer.Size() != writerSizeBeforeForward {
					h.handleFailoverExhausted(c, failoverErr, true)
					return
				}
				if !failoverErr.ShouldRetryNextAccount() {
					h.handleFailoverExhausted(c, failoverErr, false)
					return
				}
				if endpoint.IsTaskLookupRequest() {
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
				reqLog.Warn("hunyuan_action.upstream_failover_switching",
					zap.Int64("account_id", account.ID),
					zap.Int("upstream_status", failoverErr.StatusCode),
					zap.Int("switch_count", failoverState.SwitchCount),
				)
				continue
			}
			h.gatewayService.ReportOpenAIAccountScheduleResult(account, hunyuanActionScheduleModel(account, requestModel, result), false, nil)
			if !service.IsResponseCommitted(c) && c.Writer.Size() == writerSizeBeforeForward {
				h.errorResponse(c, http.StatusBadGateway, "upstream_error", "Upstream request failed")
			}
			reqLog.Warn("hunyuan_action.forward_failed",
				zap.Int64("account_id", account.ID),
				zap.Error(err),
			)
			return
		}

		h.gatewayService.ReportOpenAIAccountScheduleResult(account, hunyuanActionScheduleModel(account, requestModel, result), true, nil)
		if endpoint.IsTaskCreateRequest() && result != nil && strings.TrimSpace(result.ResponseID) != "" {
			if err := h.gatewayService.BindHunyuanTaskAccount(
				requestCtx, apiKey.GroupID, result.ResponseID, subject.UserID, apiKey.ID, account.ID,
			); err != nil {
				reqLog.Warn("hunyuan_action.bind_task_account_failed",
					zap.Int64("account_id", account.ID),
					zap.String("task_id", result.ResponseID),
					zap.Error(err),
				)
			}
			// Defer billing until a status/content read observes success: the task
			// status body carries neither model nor (for video) duration and size,
			// so the accepted submit request is the only billing evidence.
			pending := service.BuildHunyuanTaskPendingBilling(
				requestInfo,
				hunyuanFirstNonEmpty(result.BillingModel, requestModel),
				result.UpstreamModel,
				clientRequestedModel(c, requestModel),
				taskCreateStartedAt,
			)
			if err := h.gatewayService.StoreHunyuanTaskPendingBilling(requestCtx, result.ResponseID, subject.UserID, apiKey.ID, pending); err != nil {
				reqLog.Warn("hunyuan_action.store_task_pending_billing_failed_retrying",
					zap.Int64("account_id", account.ID),
					zap.String("task_id", result.ResponseID),
					zap.Error(err),
				)
				if retryErr := h.gatewayService.StoreHunyuanTaskPendingBilling(requestCtx, result.ResponseID, subject.UserID, apiKey.ID, pending); retryErr != nil {
					reqLog.Error("hunyuan_action.store_task_pending_billing_failed",
						zap.Int64("account_id", account.ID),
						zap.String("task_id", result.ResponseID),
						zap.Error(retryErr),
					)
				}
			}
		}
		switch endpoint {
		case service.HunyuanEndpointVideoTask, service.HunyuanEndpointVideoContent:
			if billResult := h.prepareHunyuanVideoCompletionBilling(requestCtx, reqLog, apiKey, subject, taskID, result); billResult != nil {
				h.recordHunyuanActionUsage(c, reqLog, apiKey, subject, subscription, account, billResult, billResult.Model, body, taskID)
			}
		case service.HunyuanEndpointTranscribeTask:
			if billResult := h.prepareHunyuanTranscribeCompletionBilling(requestCtx, reqLog, apiKey, subject, taskID, result); billResult != nil {
				h.recordHunyuanActionUsage(c, reqLog, apiKey, subject, subscription, account, billResult, billResult.Model, body, taskID)
			}
		default:
			if shouldRecordHunyuanActionUsage(endpoint, result) {
				h.recordHunyuanActionUsage(c, reqLog, apiKey, subject, subscription, account, result, requestModel, body, taskID)
			}
		}
		reqLog.Debug("hunyuan_action.request_completed",
			zap.Int64("account_id", account.ID),
			zap.Int("switch_count", failoverState.SwitchCount),
		)
		return
	}
}

// hunyuanActionRequiresModel reports which families need a model selector. The
// task lookup routes carry the model inside the stored snapshot instead, except
// the HY-3D query whose body always names the model next to the task id.
func hunyuanActionRequiresModel(endpoint service.HunyuanActionEndpoint) bool {
	switch endpoint {
	case service.HunyuanEndpointVideoTask, service.HunyuanEndpointVideoContent,
		service.HunyuanEndpointTranscribeTask:
		return false
	default:
		return true
	}
}

// shouldRecordHunyuanActionUsage gates immediate usage writes. Asynchronous
// submissions never bill here; their status reads claim completion billing.
func shouldRecordHunyuanActionUsage(endpoint service.HunyuanActionEndpoint, result *service.OpenAIForwardResult) bool {
	if result == nil {
		return false
	}
	switch endpoint {
	case service.HunyuanEndpointImageGeneration, service.HunyuanEndpointImagePanorama:
		return result.ImageCount > 0
	case service.HunyuanEndpointTextEmbeddings, service.HunyuanEndpointMultimodalEmbeddings:
		return result.Usage.InputTokens > 0 || result.Usage.OutputTokens > 0
	case service.HunyuanEndpointSpeechSync, service.HunyuanEndpointTranscribeSync:
		return result.AudioUsage != nil || result.AudioUsageUnresolved
	case service.HunyuanEndpointVoiceClone:
		// Only the optional demo audio carries gateway units; the clone fee is
		// charged upstream on the first synthesis that uses the voice_id.
		return result.AudioUsage != nil
	default:
		return false
	}
}

// prepareHunyuanVideoCompletionBilling claims one-shot billing for the first
// observed status=succeeded with a downloadable video. Without the submit-time
// snapshot the task cannot be priced honestly, so billing fails closed.
func (h *OpenAIGatewayHandler) prepareHunyuanVideoCompletionBilling(
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
	taskID = strings.TrimSpace(hunyuanFirstNonEmpty(taskID, statusResult.ResponseID))
	if taskID == "" {
		return nil
	}
	pending, loadErr := h.gatewayService.LoadHunyuanTaskPendingBilling(ctx, taskID, subject.UserID, apiKey.ID)
	if loadErr != nil {
		reqLog.Warn("hunyuan_action.video_pending_billing_load_failed", zap.String("task_id", taskID), zap.Error(loadErr))
	}
	if pending == nil {
		reqLog.Error("hunyuan_action.video_billing_skipped_missing_pending",
			zap.String("task_id", taskID),
			zap.String("reason", "no submit-time snapshot; status body has no duration or size"),
		)
		return nil
	}
	claimed, err := h.gatewayService.ClaimHunyuanTaskBilling(ctx, taskID, subject.UserID, apiKey.ID)
	if err != nil {
		reqLog.Warn("hunyuan_action.video_billing_claim_failed", zap.String("task_id", taskID), zap.Error(err))
		return nil
	}
	if !claimed {
		reqLog.Debug("hunyuan_action.video_billing_already_claimed", zap.String("task_id", taskID))
		return nil
	}
	merged := service.MergeHunyuanTaskPendingBilling(statusResult, pending, taskID)
	merged.VideoCount = 1
	merged.ImageCount = 0
	merged.ResponseID = hunyuanFirstNonEmpty(merged.ResponseID, taskID)
	return merged
}

// prepareHunyuanTranscribeCompletionBilling restores the owner-scoped model
// snapshot. Unknown completed usage retains this poll as review evidence; only
// an observed duration claims the stable task charge.
func (h *OpenAIGatewayHandler) prepareHunyuanTranscribeCompletionBilling(
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
	if statusResult.AudioUsage == nil && !statusResult.AudioUsageUnresolved {
		return nil
	}
	taskID = strings.TrimSpace(hunyuanFirstNonEmpty(taskID, statusResult.ResponseID))
	if taskID == "" {
		return nil
	}
	pending, loadErr := h.gatewayService.LoadHunyuanTaskPendingBilling(ctx, taskID, subject.UserID, apiKey.ID)
	if loadErr != nil || pending == nil {
		reqLog.Error("hunyuan_action.transcribe_pending_billing_unavailable", zap.String("task_id", taskID))
		return nil
	}
	merged := service.MergeHunyuanTaskPendingBilling(statusResult, pending, taskID)
	merged.ResponseID = taskID
	if merged.AudioUsageUnresolved {
		// Preserve this poll's evidence ID and the submit snapshot; no final claim.
		return merged
	}
	claimed, err := h.gatewayService.ClaimHunyuanTaskBilling(ctx, taskID, subject.UserID, apiKey.ID)
	if err != nil {
		reqLog.Warn("hunyuan_action.transcribe_billing_claim_failed", zap.String("task_id", taskID), zap.Error(err))
		return nil
	}
	if !claimed {
		reqLog.Debug("hunyuan_action.transcribe_billing_already_claimed", zap.String("task_id", taskID))
		return nil
	}
	merged.RequestID = service.HunyuanTranscribeTaskBillingRequestID(taskID)
	return merged
}

// recordHunyuanActionUsage writes one usage record and releases the task billing
// claim when durable recording fails so a later read can retry.
func (h *OpenAIGatewayHandler) recordHunyuanActionUsage(
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
	if result != nil && result.AudioUsageUnresolved {
		h.submitOpenAIUsageRecordTask(c.Request.Context(), result, func(ctx context.Context) {
			if err := h.gatewayService.RecordHunyuanSTTUnresolved(ctx, result, apiKey, account, taskID); err != nil {
				reqLog.Error("hunyuan_action.stt_unresolved_persistence_failed",
					zap.String("task_id", taskID), zap.String("request_id", result.RequestID),
					zap.Int64("account_id", account.ID), zap.String("model", result.Model))
			}
		})
		return
	}
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
	billableTaskID := ""
	if result != nil && result.VideoCount > 0 {
		billableTaskID = strings.TrimSpace(hunyuanFirstNonEmpty(taskID, result.ResponseID))
		if stable := service.StableHunyuanVideoBillingRequestID(hunyuanFirstNonEmpty(result.ResponseID, taskID)); stable != "" {
			result.RequestID = stable
		}
		if len(body) == 0 && billableTaskID != "" {
			payloadForHash = []byte(billableTaskID)
		}
	}
	if result != nil && result.AudioUsage != nil {
		if strings.HasPrefix(strings.TrimSpace(result.RequestID), "hunyuan-transcribe:") {
			// Speech recognition task polling already pinned its durable id.
			billableTaskID = strings.TrimSpace(hunyuanFirstNonEmpty(taskID, result.ResponseID))
		} else {
			result.RequestID = service.StableHunyuanAudioBillingRequestID(result.RequestID)
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
			if billableTaskID != "" {
				if releaseErr := h.gatewayService.ReleaseHunyuanTaskBilling(ctx, billableTaskID, subject.UserID, apiKey.ID); releaseErr != nil {
					reqLog.Warn("hunyuan_action.task_billing_claim_release_failed",
						zap.String("task_id", billableTaskID),
						zap.Error(releaseErr),
					)
				}
			}
			logger.L().With(
				zap.String("component", "handler.openai_gateway.hunyuan_action"),
				zap.Int64("user_id", subject.UserID),
				zap.Int64("api_key_id", apiKey.ID),
				zap.Any("group_id", apiKey.GroupID),
				zap.String("model", requestModel),
				zap.Int64("account_id", account.ID),
			).Error("hunyuan_action.record_usage_failed", zap.Error(err))
		}
	})
}

func hunyuanActionScheduleModel(account *service.Account, requestModel string, result *service.OpenAIForwardResult) string {
	if result != nil && strings.TrimSpace(result.UpstreamModel) != "" {
		return result.UpstreamModel
	}
	if account == nil {
		return strings.TrimSpace(requestModel)
	}
	return account.GetMappedModel(requestModel)
}

func hunyuanFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func hunyuanTTSAuditBody(input string) []byte {
	payload := map[string]any{
		"messages": []map[string]any{{"role": "user", "content": input}},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil
	}
	return body
}
