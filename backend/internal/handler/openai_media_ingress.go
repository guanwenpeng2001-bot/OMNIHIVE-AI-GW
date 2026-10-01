package handler

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"

	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"go.uber.org/zap"
)

// This file is the ingress of the OpenAI-native media adapters. The adapters
// themselves live in service/openai_video_jobs.go (video job lifecycle) and
// service/openai_actions.go (audio.translate); until this ingress existed the
// action matrix advertised those cells while no route resolved their dispatch
// adapter, so every request failed closed with action_not_implemented.
//
// Billing and job semantics stay in those adapters and in the shared upstream
// job trunk: the ingress only resolves ownership, freezes the admission tariff
// and renders the provider-independent envelope.

const (
	// openAIMediaIngressAccountAttempts bounds the API-key account scan. The
	// OpenAI open-API adapters require a first-party API key credential, so an
	// OAuth account selected by the scheduler is skipped instead of being handed
	// to an adapter that must refuse it.
	openAIMediaIngressAccountAttempts = 4

	// openAIMediaIngressResponseLimit bounds one synchronous media response. Job
	// objects and transcripts are far smaller; a larger body is refused instead
	// of being buffered without limit.
	openAIMediaIngressResponseLimit = 8 << 20
)

// openAIVideoExtensionSeconds are the documented extension segment lengths
// (CreateVideoExtend*Body: 4, 8, 12, 16, 20). The adapter re-validates the
// billable segment; the ingress rejects the same set first so an unverified
// segment stays a client error instead of a failed job submission.
var openAIVideoExtensionSeconds = []int{4, 8, 12, 16, 20}

// OpenAIVideoGeneration serves POST /v1/videos and /v1/videos/generations for
// OpenAI API-key groups.
func (h *OpenAIGatewayHandler) OpenAIVideoGeneration(c *gin.Context) {
	h.handleOpenAIVideoSubmit(c, "video.generate")
}

// OpenAIVideoEdit serves POST /v1/videos/edits for OpenAI API-key groups.
func (h *OpenAIGatewayHandler) OpenAIVideoEdit(c *gin.Context) {
	h.handleOpenAIVideoSubmit(c, "video.edit")
}

// OpenAIVideoExtension serves POST /v1/videos/extensions for OpenAI API-key groups.
func (h *OpenAIGatewayHandler) OpenAIVideoExtension(c *gin.Context) {
	h.handleOpenAIVideoSubmit(c, "video.extend")
}

// OpenAIVideoStatus serves the owner-bound video lookups (GET /v1/videos/:request_id
// and its generation/edit/extension aliases) from durable job state. The shared
// timing wheel owns polling, so a read never triggers upstream work.
func (h *OpenAIGatewayHandler) OpenAIVideoStatus(c *gin.Context) {
	h.handleOpenAIVideoRead(c, false)
}

// OpenAIVideoContent streams one settled video's content to its owner.
func (h *OpenAIGatewayHandler) OpenAIVideoContent(c *gin.Context) {
	h.handleOpenAIVideoRead(c, true)
}

// OpenAIAudioTranslations serves POST /v1/audio/translations for OpenAI API-key
// groups. The adapter owns the multipart wire shape and the upstream call; the
// ingress resolves the account and passes the provider response through.
func (h *OpenAIGatewayHandler) OpenAIAudioTranslations(c *gin.Context) {
	streamStarted := false
	requestStart := time.Now()

	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok || apiKey == nil {
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
		"handler.openai_gateway.openai_audio",
		zap.Int64("user_id", subject.UserID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.Any("group_id", apiKey.GroupID),
		zap.String("action", "audio.translate"),
	)
	if !h.ensureResponsesDependencies(c, reqLog) {
		return
	}

	body, ok := h.readOpenAIMediaBody(c)
	if !ok {
		return
	}
	contentType := c.GetHeader("Content-Type")
	model, ok := openAIMultipartModel(contentType, body)
	if !ok {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "audio translations require multipart/form-data with a model field")
		return
	}

	setOpsRequestContext(c, model, false)
	setOpsEndpointContext(c, "", int16(service.RequestTypeSync))
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
		reqLog.Info("openai_audio.billing_eligibility_check_failed", zap.Error(err))
		status, code, message, retryAfter := billingErrorDetails(err)
		if retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		h.errorResponse(c, status, code, message)
		return
	}

	sessionHash := h.gatewayService.GenerateExplicitSessionHash(c, body)
	account, accountRelease, ok := h.selectOpenAIMediaAccount(c, apiKey, reqLog, sessionHash, model)
	if !ok {
		return
	}
	if accountRelease != nil {
		defer accountRelease()
	}
	setOpsSelectedAccount(c, account.ID, account.Platform)

	// The translation body keeps the caller's model: audio.translate is a native
	// OpenAI action, so the requested model is already the upstream model.
	response, err := h.gatewayService.ForwardOpenAIAction(c.Request.Context(), account, "audio.translate", contentType, body, "")
	if err != nil {
		status, code, message := openAIActionForwardFailure(err)
		reqLog.Warn("openai_audio.forward_failed", zap.Int64("account_id", account.ID), zap.Error(err))
		h.errorResponse(c, status, code, message)
		return
	}
	defer func() { _ = response.Body.Close() }()
	h.writeOpenAIMediaActionResponse(c, reqLog, response)
}

// handleOpenAIVideoSubmit admits one generate, edit or extension job and hands
// it to the shared upstream job trunk. Submission is idempotent on the caller's
// body, so a retry resolves the same durable job instead of a second video.
func (h *OpenAIGatewayHandler) handleOpenAIVideoSubmit(c *gin.Context, action string) {
	streamStarted := false
	requestStart := time.Now()

	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok || apiKey == nil {
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
		"handler.openai_gateway.openai_video",
		zap.Int64("user_id", subject.UserID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.Any("group_id", apiKey.GroupID),
		zap.String("action", action),
	)
	if !h.ensureResponsesDependencies(c, reqLog) {
		return
	}
	if h.upstreamJobs == nil {
		h.errorResponse(c, http.StatusServiceUnavailable, "api_error", "Video jobs are unavailable")
		return
	}

	body, ok := h.readOpenAIMediaBody(c)
	if !ok {
		return
	}
	contentType := c.GetHeader("Content-Type")

	// The documented create body defaults to sora-2, the only priced family the
	// adapter accepts; the same default keeps account selection on that family.
	requestModel := strings.TrimSpace(gjson.GetBytes(body, "model").String())
	if requestModel == "" {
		requestModel = "sora-2"
	}
	var source *service.UpstreamJob
	billableSeconds := 0
	switch action {
	case "video.edit", "video.extend":
		source, ok = h.resolveOpenAIVideoSource(c, apiKey, body)
		if !ok {
			return
		}
		// Edit and extension inherit the source video's model and resolution.
		requestModel = source.Billing.Model
	case "video.generate":
	default:
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Unsupported video action")
		return
	}
	if action == "video.extend" {
		seconds, ok := openAIVideoExtensionSecondsFromBody(body)
		if !ok {
			h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "video extension requires a documented seconds value (4, 8, 12, 16 or 20)")
			return
		}
		billableSeconds = seconds
	}

	setOpsRequestContext(c, requestModel, false)
	setOpsEndpointContext(c, "", int16(service.RequestTypeSync))
	// Video prompts are moderated through the same media protocol the Grok media
	// path uses; a status read or a model-less request carries no prompt to audit.
	if strings.TrimSpace(gjson.GetBytes(body, "prompt").String()) != "" {
		decision := h.checkSecurityAudit(c, reqLog, apiKey, subject, service.ContentModerationProtocolOpenAIImages, requestModel, body)
		if decision != nil && !decision.AllowNextStage {
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
		reqLog.Info("openai_video.billing_eligibility_check_failed", zap.Error(err))
		status, code, message, retryAfter := billingErrorDetails(err)
		if retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		h.errorResponse(c, status, code, message)
		return
	}

	sessionHash := h.gatewayService.GenerateExplicitSessionHash(c, body)
	account, accountRelease, ok := h.selectOpenAIMediaAccount(c, apiKey, reqLog, sessionHash, requestModel)
	if !ok {
		return
	}
	if accountRelease != nil {
		defer accountRelease()
	}
	setOpsSelectedAccount(c, account.ID, account.Platform)

	// The tariff is frozen after selection: the mapped model, the resolution
	// tier and the group price all belong to the account that will serve the job.
	billing, err := service.OpenAIVideoBillingSnapshot(apiKey.Group, account, apiKey, body, subscription, source)
	if err != nil {
		h.errorResponse(c, http.StatusUnprocessableEntity, "unsupported_operation_error", err.Error())
		return
	}

	upstreamBody := body
	if source != nil {
		// The caller only ever holds the gateway's public request id, so the
		// source reference is translated to the owner-checked upstream id before
		// the adapter forwards the body. The caller's body stays the idempotency
		// fingerprint, so a retry still resolves the same job.
		rewritten, rewriteErr := openAIVideoSourceBody(body, source)
		if rewriteErr != nil {
			h.errorResponse(c, http.StatusInternalServerError, "api_error", "Unable to bind the source video")
			return
		}
		upstreamBody = rewritten
	}

	// Preserve the existing selection and tariff checks, then give the child
	// submission its TPM repository and scope before it can contact upstream.
	defer beginGatewayRequestPlan(c, h.gatewayService, apiKey, requestModel, "media", h.maxAccountSwitches)()
	template := &service.UpstreamJob{
		Platform:  service.PlatformOpenAI,
		Family:    "video",
		AccountID: account.ID,
		UserID:    apiKey.UserID,
		APIKeyID:  apiKey.ID,
		GroupID:   apiKey.GroupID,
		Billing:   billing,
	}
	job, err := h.upstreamJobs.Submit(c.Request.Context(), template, body, func(ctx context.Context) (service.UpstreamJobUpdate, error) {
		update, submitErr := h.gatewayService.SubmitOpenAIVideo(ctx, account, action, contentType, upstreamBody, billing.Model, billableSeconds)
		if submitErr != nil {
			reqLog.Warn("openai_video.adapter_submit_failed", zap.Int64("account_id", account.ID), zap.Error(submitErr))
		}
		return update, submitErr
	}, account)
	if err != nil {
		reqLog.Warn("openai_video.submit_failed", zap.Int64("account_id", account.ID), zap.Error(err))
		h.errorResponse(c, http.StatusServiceUnavailable, "api_error", "Unable to admit the video job")
		return
	}
	if job == nil {
		h.errorResponse(c, http.StatusServiceUnavailable, "api_error", "Unable to admit the video job")
		return
	}
	if h.writeOpenAIVideoSubmitFailure(c, job) {
		reqLog.Warn("openai_video.submit_rejected",
			zap.Int64("account_id", account.ID),
			zap.String("error_code", job.ErrorCode),
		)
		return
	}
	reqLog.Debug("openai_video.submitted",
		zap.Int64("account_id", account.ID),
		zap.String("request_id", job.PublicRequestID),
	)
	c.JSON(http.StatusOK, openAIVideoJobObject(job))
}

// handleOpenAIVideoRead resolves one owner-bound video job and either renders
// its durable state or streams its settled content.
func (h *OpenAIGatewayHandler) handleOpenAIVideoRead(c *gin.Context, content bool) {
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok || apiKey == nil {
		h.errorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return
	}
	reqLog := requestLogger(c, "handler.openai_gateway.openai_video", zap.Int64("api_key_id", apiKey.ID))
	if !h.ensureResponsesDependencies(c, reqLog) {
		return
	}
	if h.upstreamJobs == nil {
		h.errorResponse(c, http.StatusServiceUnavailable, "api_error", "Video jobs are unavailable")
		return
	}
	job, ok := h.resolveOpenAIVideoJob(c, apiKey, reqLog)
	if !ok {
		return
	}
	setOpsRequestContext(c, openAIVideoClientModel(job), false)
	setOpsEndpointContext(c, "", int16(service.RequestTypeSync))
	setOpsSelectedAccount(c, job.AccountID, service.PlatformOpenAI)
	if !content {
		c.JSON(http.StatusOK, openAIVideoJobObject(job))
		return
	}
	if job.Status != "succeeded" || job.ResultRef == "" {
		h.errorResponse(c, http.StatusConflict, "resource_not_ready", "Video content is not ready")
		return
	}
	response, err := h.gatewayService.OpenOpenAIVideoContent(c.Request.Context(), job)
	if err != nil {
		reqLog.Warn("openai_video.content_failed", zap.String("request_id", job.PublicRequestID), zap.Error(err))
		h.errorResponse(c, http.StatusBadGateway, "upstream_error", "Video content unavailable")
		return
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		h.errorResponse(c, http.StatusBadGateway, "upstream_error", "Video content unavailable")
		return
	}
	contentType := response.Header.Get("Content-Type")
	if strings.TrimSpace(contentType) == "" {
		contentType = "video/mp4"
	}
	c.DataFromReader(http.StatusOK, response.ContentLength, contentType, response.Body, nil)
}

// resolveOpenAIVideoJob resolves one video job for the requesting user and key.
// An unknown, foreign or non-video id is indistinguishable from a missing one.
func (h *OpenAIGatewayHandler) resolveOpenAIVideoJob(c *gin.Context, apiKey *service.APIKey, reqLog *zap.Logger) (*service.UpstreamJob, bool) {
	job, err := h.upstreamJobs.Get(c.Request.Context(), c.Param("request_id"), apiKey.UserID, apiKey.ID)
	if err != nil || job == nil || job.Platform != service.PlatformOpenAI || job.Family != "video" {
		reqLog.Debug("openai_video.lookup_miss", zap.Error(err))
		h.errorResponse(c, http.StatusNotFound, "not_found_error", "Video not found")
		return nil, false
	}
	return job, true
}

// openAIVideoSourceBody binds one edit or extension body to the owner-checked
// source video: the caller's public request id is replaced by the upstream id
// the adapter must forward. Every other admitted byte is preserved.
func openAIVideoSourceBody(body []byte, source *service.UpstreamJob) ([]byte, error) {
	if source == nil || strings.TrimSpace(source.UpstreamTaskID) == "" {
		return nil, errors.New("the source video has no upstream binding")
	}
	return sjson.SetBytes(body, "video.id", source.UpstreamTaskID)
}

// resolveOpenAIVideoSource resolves the owner-bound source video of an edit or
// extension. The source must have completed: the adapter inherits its model and
// resolution, and an unfinished source has no frozen tariff to inherit.
func (h *OpenAIGatewayHandler) resolveOpenAIVideoSource(c *gin.Context, apiKey *service.APIKey, body []byte) (*service.UpstreamJob, bool) {
	publicID := strings.TrimSpace(gjson.GetBytes(body, "video.id").String())
	if publicID == "" {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "video.id is required")
		return nil, false
	}
	source, err := h.upstreamJobs.Get(c.Request.Context(), publicID, apiKey.UserID, apiKey.ID)
	if err != nil || source == nil || source.Platform != service.PlatformOpenAI || source.Family != "video" {
		h.errorResponse(c, http.StatusNotFound, "not_found_error", "Video not found")
		return nil, false
	}
	if source.Status != "succeeded" {
		h.errorResponse(c, http.StatusConflict, "resource_not_ready", "The source video has not completed")
		return nil, false
	}
	return source, true
}

// selectOpenAIMediaAccount picks one schedulable OpenAI API-key account. The
// account is held for the duration of the submit through the same slot the
// other media ingresses acquire.
func (h *OpenAIGatewayHandler) selectOpenAIMediaAccount(c *gin.Context, apiKey *service.APIKey, reqLog *zap.Logger, sessionHash, requestedModel string) (*service.Account, func(), bool) {
	excluded := map[int64]struct{}{}
	for attempt := 0; attempt < openAIMediaIngressAccountAttempts; attempt++ {
		selection, _, err := h.gatewayService.SelectAccountWithSchedulerForCapability(
			c.Request.Context(),
			apiKey.GroupID,
			"",
			sessionHash,
			requestedModel,
			excluded,
			service.OpenAIUpstreamTransportHTTPSSE,
			service.OpenAIEndpointCapabilityChatCompletions,
			false,
			false,
			false,
			service.PlatformOpenAI,
		)
		if err != nil || selection == nil || selection.Account == nil {
			reqLog.Warn("openai_media.account_select_failed", zap.Error(err), zap.Int("excluded_account_count", len(excluded)))
			h.errorResponse(c, http.StatusServiceUnavailable, "api_error", "No OpenAI account is available for this action")
			return nil, nil, false
		}
		streamStarted := false
		release, slot := h.acquireResponsesAccountSlot(c, apiKey.GroupID, sessionHash, selection, false, &streamStarted, reqLog)
		if slot != openAISlotAcquireOK {
			return nil, nil, false
		}
		if selection.Account.Type == service.AccountTypeAPIKey && selection.Account.IsActive() {
			return selection.Account, release, true
		}
		if release != nil {
			release()
		}
		excluded[selection.Account.ID] = struct{}{}
	}
	h.errorResponse(c, http.StatusServiceUnavailable, "api_error", "No OpenAI account is available for this action")
	return nil, nil, false
}

// readOpenAIMediaBody reads one bounded media body. The route middleware already
// caps the body; this turns a truncated or unparseable body into the protocol
// error the caller can act on.
func (h *OpenAIGatewayHandler) readOpenAIMediaBody(c *gin.Context) ([]byte, bool) {
	body, err := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
	if err != nil {
		if maxErr, ok := extractMaxBytesError(err); ok {
			h.errorResponse(c, http.StatusRequestEntityTooLarge, "invalid_request_error", buildBodyTooLargeMessage(maxErr.Limit))
			return nil, false
		}
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
		return nil, false
	}
	if len(body) == 0 {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Request body is empty")
		return nil, false
	}
	return body, true
}

// writeOpenAIMediaActionResponse passes one synchronous provider response back
// to the caller with a bounded body. The provider's own status and error
// envelope stay visible; no gateway-side billing is invented for it.
func (h *OpenAIGatewayHandler) writeOpenAIMediaActionResponse(c *gin.Context, reqLog *zap.Logger, response *http.Response) {
	raw, err := io.ReadAll(io.LimitReader(response.Body, openAIMediaIngressResponseLimit+1))
	if err != nil {
		reqLog.Warn("openai_media.response_read_failed", zap.Error(err))
		h.errorResponse(c, http.StatusBadGateway, "upstream_error", "Upstream response could not be read")
		return
	}
	if len(raw) > openAIMediaIngressResponseLimit {
		h.errorResponse(c, http.StatusBadGateway, "upstream_error", "Upstream response exceeds the gateway limit")
		return
	}
	contentType := strings.TrimSpace(response.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = "application/json"
	}
	c.Data(response.StatusCode, contentType, raw)
}

// writeOpenAIVideoSubmitFailure surfaces a durable submit-time failure through
// the error envelope instead of a success object that silently carries an error
// state. A provider rejection is a 422; an unknown submission outcome is a 502.
func (h *OpenAIGatewayHandler) writeOpenAIVideoSubmitFailure(c *gin.Context, job *service.UpstreamJob) bool {
	if job == nil || job.Status != "failed" {
		return false
	}
	status := http.StatusBadGateway
	if job.ErrorCode == "upstream_rejected" {
		status = http.StatusUnprocessableEntity
	}
	message := strings.TrimSpace(job.ErrorMessage)
	if message == "" {
		message = "The upstream rejected the video request"
	}
	errorType := strings.TrimSpace(job.ErrorCode)
	if errorType == "" {
		errorType = "upstream_error"
	}
	h.errorResponse(c, status, errorType, message)
	return true
}

// openAIVideoJobObject renders one durable video job in the documented
// VideoResource shape. The upstream task id never reaches the caller: the public
// request id is the only id accepted by status, content, edit and extension.
func openAIVideoJobObject(job *service.UpstreamJob) gin.H {
	object := gin.H{
		"id":         job.PublicRequestID,
		"object":     "video",
		"status":     openAIVideoClientStatus(job.Status),
		"created_at": job.SubmittedAt.Unix(),
		"model":      openAIVideoClientModel(job),
	}
	if len(job.UsageJSON) > 0 && gjson.ValidBytes(job.UsageJSON) {
		if seconds := gjson.GetBytes(job.UsageJSON, "seconds"); seconds.Exists() {
			object["seconds"] = seconds.String()
		}
		if size := gjson.GetBytes(job.UsageJSON, "size"); size.Exists() {
			object["size"] = size.String()
		}
	}
	if job.ErrorCode != "" {
		object["error"] = gin.H{"code": job.ErrorCode, "message": job.ErrorMessage}
	}
	return object
}

// openAIVideoClientStatus maps the durable job state onto the documented
// VideoStatus values.
func openAIVideoClientStatus(status string) string {
	switch status {
	case "submitted":
		return "queued"
	case "running":
		return "in_progress"
	case "succeeded":
		return "completed"
	default:
		return "failed"
	}
}

// openAIVideoClientModel reports the caller's model when it was recorded, so a
// mapped upstream id is never mistaken for the requested family.
func openAIVideoClientModel(job *service.UpstreamJob) string {
	if strings.TrimSpace(job.Billing.RequestedModel) != "" {
		return job.Billing.RequestedModel
	}
	return job.Billing.Model
}

// openAIVideoExtensionSecondsFromBody reads the documented extension segment.
// The adapter requires the same value to reach the upstream unchanged.
func openAIVideoExtensionSecondsFromBody(body []byte) (int, bool) {
	seconds, err := strconv.Atoi(strings.TrimSpace(gjson.GetBytes(body, "seconds").String()))
	if err != nil {
		return 0, false
	}
	for _, documented := range openAIVideoExtensionSeconds {
		if documented == seconds {
			return seconds, true
		}
	}
	return 0, false
}

// openAIMultipartModel reads the single model form field of a multipart action
// body. Account selection needs the model before the adapter parses the body;
// the adapter still enforces its own single-field rule when it forwards.
func openAIMultipartModel(contentType string, body []byte) (string, bool) {
	mediaType, parameters, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "multipart/form-data" || parameters["boundary"] == "" {
		return "", false
	}
	reader := multipart.NewReader(bytes.NewReader(body), parameters["boundary"])
	model := ""
	for {
		part, partErr := reader.NextPart()
		if partErr == io.EOF {
			break
		}
		if partErr != nil {
			return "", false
		}
		if part.FormName() == "model" && part.FileName() == "" {
			value, readErr := io.ReadAll(io.LimitReader(part, 1024))
			_ = part.Close()
			if readErr != nil || model != "" {
				return "", false
			}
			model = strings.TrimSpace(string(value))
			continue
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(part, 1<<20))
		_ = part.Close()
	}
	return model, model != ""
}

// openAIActionForwardFailure classifies one synchronous action failure. The
// adapter validates credentials and the client body before it opens a socket, so
// a missing API-key credential and a model the account cannot serve are client
// visible; everything else is reported as an upstream failure.
func openAIActionForwardFailure(err error) (int, string, string) {
	switch {
	case errors.Is(err, service.ErrOpenAIActionCredential):
		return http.StatusServiceUnavailable, "api_error", "No OpenAI API key account is available for this action"
	case errors.Is(err, service.ErrAccountModelUnsupported):
		return http.StatusUnprocessableEntity, string(service.ModelBindingRejectedReason), service.ModelBindingRejectedClientMessage
	default:
		return http.StatusBadGateway, "upstream_error", "Upstream request failed"
	}
}
