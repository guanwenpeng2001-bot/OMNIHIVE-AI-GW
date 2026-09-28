package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// gatewayResourceMetadataBodyLimit bounds the batch metadata bodies. Larger
// metadata is refused explicitly instead of being partially consumed.
const gatewayResourceMetadataBodyLimit = 1 << 20

// gatewayResourceUploadOverhead is the multipart framing allowance on top of the
// admitted file size.
const gatewayResourceUploadOverhead = 1 << 20

// gatewayResourceAccountAttempts bounds the account scan for a resource action.
const gatewayResourceAccountAttempts = 4

// GatewayResourceHandler serves the standard file, batch, message batch and
// moderation resource surface. Provider wire shapes stay in the platform
// adapters; this handler owns transport, ownership and protocol rendering.
type GatewayResourceHandler struct {
	openAI       *OpenAIGatewayHandler
	upstreamJobs *service.UpstreamJobService
}

func NewGatewayResourceHandler(openAI *OpenAIGatewayHandler, upstreamJobs *service.UpstreamJobService) *GatewayResourceHandler {
	return &GatewayResourceHandler{openAI: openAI, upstreamJobs: upstreamJobs}
}

func (h *GatewayResourceHandler) ready() bool {
	return h != nil && h.openAI != nil && h.openAI.gatewayService != nil && h.upstreamJobs != nil
}

func (h *GatewayResourceHandler) openAIError(c *gin.Context, status int, code, message string) {
	h.openAI.errorResponse(c, status, code, message)
}

func (h *GatewayResourceHandler) anthropicError(c *gin.Context, status int, code, message string) {
	h.openAI.anthropicErrorResponse(c, status, code, message)
}

func (h *GatewayResourceHandler) resourceNotFound(c *gin.Context) {
	h.openAIError(c, http.StatusNotFound, "not_found_error", "Resource not found")
}

// resourceKey resolves the authenticated key, which carries the user, group and
// workspace scope used for every ownership decision.
func (h *GatewayResourceHandler) resourceKey(c *gin.Context) (*service.APIKey, bool) {
	if !h.ready() {
		h.openAIError(c, http.StatusServiceUnavailable, "service_unavailable", "Resource actions are unavailable")
		return nil, false
	}
	key, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok || key == nil || key.Group == nil || key.GroupID == nil || key.UserID <= 0 || key.ID <= 0 {
		h.openAIError(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return nil, false
	}
	return key, true
}

// resourcePlatform resolves the concrete platform serving one standard resource
// surface. These endpoints carry no model selector, so a composite group cannot
// resolve a target platform from the request itself: the surface's own platform
// is pinned into the request context — the same mechanism the model-less
// Grok-only actions use — before quota, account selection and billing observe it.
// Every other platform keeps its exact value, so a platform without a standard
// resource adapter still answers as "not found" instead of being forwarded.
func (h *GatewayResourceHandler) resourcePlatform(c *gin.Context, surface string) string {
	platform := service.GatewayTargetPlatform(c)
	if platform != service.PlatformComposite {
		return platform
	}
	c.Request = c.Request.WithContext(service.WithResolvedTargetPlatform(c.Request.Context(), surface))
	return surface
}

func (h *GatewayResourceHandler) requireOpenAIPlatform(c *gin.Context) bool {
	if h.resourcePlatform(c, service.PlatformOpenAI) != service.PlatformOpenAI {
		h.resourceNotFound(c)
		return false
	}
	return true
}

func (h *GatewayResourceHandler) requireAnthropicPlatform(c *gin.Context) bool {
	if h.resourcePlatform(c, service.PlatformAnthropic) != service.PlatformAnthropic {
		h.resourceNotFound(c)
		return false
	}
	return true
}

// readResourceBody reads a bounded request body. The route middleware already
// caps the body; this adds the resource-specific bound.
func (h *GatewayResourceHandler) readResourceBody(c *gin.Context, limit int64) ([]byte, bool) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		status, code, message := http.StatusBadRequest, "invalid_request_error", "Invalid request body"
		if limitErr, exceeded := extractMaxBytesError(err); exceeded {
			status, code, message = http.StatusRequestEntityTooLarge, "request_too_large", buildBodyTooLargeMessage(limitErr.Limit)
		}
		h.openAIError(c, status, code, message)
		return nil, false
	}
	return body, true
}

// selectResourceAccount picks one schedulable account that can execute native
// resource work. Accounts without a usable API-key credential are skipped
// instead of being handed to an adapter that must refuse them.
func (h *GatewayResourceHandler) selectResourceAccount(c *gin.Context, key *service.APIKey, platform, sessionHash string) (*service.Account, func(), bool) {
	excluded := map[int64]struct{}{}
	requestLog := requestLogger(c, "handler.gateway_resource_actions")
	for attempt := 0; attempt < gatewayResourceAccountAttempts; attempt++ {
		selection, _, err := h.openAI.gatewayService.SelectAccountWithSchedulerForCapability(c.Request.Context(), key.GroupID, "", sessionHash, "", excluded, service.OpenAIUpstreamTransportAny, service.OpenAIEndpointCapabilityChatCompletions, false, false, true, platform)
		if err != nil || selection == nil || selection.Account == nil {
			h.openAIError(c, http.StatusServiceUnavailable, "resource_account_unavailable", "No resource account available")
			return nil, nil, false
		}
		streamStarted := false
		release, slot := h.openAI.acquireResponsesAccountSlot(c, key.GroupID, sessionHash, selection, false, &streamStarted, requestLog)
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
	h.openAIError(c, http.StatusServiceUnavailable, "resource_account_unavailable", "No resource account available")
	return nil, nil, false
}

// checkResourceAdmission runs the shared billing eligibility, user slot and
// content policy admission for one submit.
func (h *GatewayResourceHandler) checkResourceAdmission(c *gin.Context, key *service.APIKey, subject middleware2.AuthSubject) (func(), bool) {
	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	if err := h.openAI.billingCacheService.CheckBillingEligibility(c.Request.Context(), key.User, key, key.Group, subscription, service.QuotaPlatform(c.Request.Context(), key)); err != nil {
		status, code, message, _ := billingErrorDetails(err)
		writeAdmissionRejectionHeaders(c, err)
		h.openAIError(c, status, code, message)
		return nil, false
	}
	streamStarted := false
	release, ok := h.openAI.acquireResponsesUserSlot(c, subject.UserID, subject.Concurrency, false, &streamStarted, requestLogger(c, "handler.gateway_resource_actions"))
	if !ok {
		return nil, false
	}
	return release, true
}

func (h *GatewayResourceHandler) auditResourceBatchLines(c *gin.Context, key *service.APIKey, subject middleware2.AuthSubject, model string, jsonl []byte) bool {
	requestLog := requestLogger(c, "handler.gateway_resource_actions")
	for _, line := range strings.Split(strings.TrimSpace(string(jsonl)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var audit []byte
		var message struct {
			Body json.RawMessage `json:"body"`
		}
		if json.Unmarshal([]byte(line), &message) == nil {
			audit = message.Body
		}
		if len(audit) == 0 {
			continue
		}
		if decision := h.openAI.checkSecurityAuditStage(c, requestLog, key, subject, service.ContentModerationProtocolOpenAIChat, model, audit, "batch_item"); decision != nil && !decision.AllowNextStage {
			h.openAI.openAISecurityAuditError(c, decision)
			return false
		}
	}
	return true
}

// writeResourceSubmitFailure surfaces a durable submit-time failure through the
// unified error envelope instead of a success object that silently carries an
// error state. A provider rejection is a 422; an unverifiable or
// transport-unknown outcome is a 502. The failed receipt stays in the store.
func (h *GatewayResourceHandler) writeResourceSubmitFailure(c *gin.Context, job *service.UpstreamJob) bool {
	if job == nil || job.Status != "failed" {
		return false
	}
	status := http.StatusBadGateway
	if job.ErrorCode == "upstream_rejected" {
		status = http.StatusUnprocessableEntity
	}
	message := strings.TrimSpace(job.ErrorMessage)
	if message == "" {
		message = "The upstream rejected the resource request"
	}
	h.openAIError(c, status, job.ErrorCode, message)
	return true
}

// UploadFile handles POST /v1/files.
func (h *GatewayResourceHandler) UploadFile(c *gin.Context) {
	key, ok := h.resourceKey(c)
	if !ok || !h.requireOpenAIPlatform(c) {
		return
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.openAIError(c, http.StatusUnauthorized, "authentication_error", "Invalid user context")
		return
	}
	body, ok := h.readResourceBody(c, service.GatewayResourceFileLimit+gatewayResourceUploadOverhead)
	if !ok {
		return
	}
	purpose, filename, content, err := service.ParseGatewayResourceMultipart(c.GetHeader("Content-Type"), body, service.GatewayResourceFileLimit)
	if err != nil {
		status, code := http.StatusBadRequest, "invalid_request_error"
		if errors.Is(err, service.ErrGatewayResourceFileTooLarge) {
			status, code = http.StatusRequestEntityTooLarge, "request_too_large"
		}
		h.openAIError(c, status, code, err.Error())
		return
	}
	snapshot, err := service.ValidateGatewayResourceFile(purpose, filename, content)
	if err != nil {
		status, code := http.StatusUnprocessableEntity, "unsupported_operation_error"
		if errors.Is(err, service.ErrGatewayResourceFileTooLarge) {
			status, code = http.StatusRequestEntityTooLarge, "request_too_large"
		}
		h.openAIError(c, status, code, err.Error())
		return
	}
	if purpose == service.GatewayResourceFilePurposeBatch && !h.auditResourceBatchLines(c, key, subject, snapshot.BatchModel, content) {
		return
	}
	release, ok := h.checkResourceAdmission(c, key, subject)
	if !ok {
		return
	}
	if release != nil {
		defer release()
	}
	contentType := c.GetHeader("Content-Type")
	fingerprint, err := json.Marshal(snapshot)
	if err != nil {
		h.openAIError(c, http.StatusInternalServerError, "api_error", "Unable to admit the file")
		return
	}
	sessionHash := h.openAI.gatewayService.GenerateSessionHash(c, fingerprint)
	account, accountRelease, ok := h.selectResourceAccount(c, key, service.PlatformOpenAI, sessionHash)
	if !ok {
		return
	}
	if accountRelease != nil {
		defer accountRelease()
	}
	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	billing := service.GatewayResourceFileBillingSnapshot(snapshot, account, key, subscription)
	template := &service.UpstreamJob{Platform: service.PlatformOpenAI, Family: "files", AccountID: account.ID, UserID: key.UserID, APIKeyID: key.ID, GroupID: key.GroupID, Billing: billing}
	job, err := h.upstreamJobs.Submit(c.Request.Context(), template, fingerprint, func(ctx context.Context) (service.UpstreamJobUpdate, error) {
		return h.openAI.gatewayService.SubmitOpenAIFile(ctx, account, contentType, body)
	})
	if err != nil {
		h.openAIError(c, http.StatusServiceUnavailable, "resource_store_unavailable", "Unable to persist the file")
		return
	}
	if h.writeResourceSubmitFailure(c, job) {
		return
	}
	setOpsSelectedAccount(c, job.AccountID, service.PlatformOpenAI)
	c.JSON(http.StatusOK, gatewayResourceFileObject(job))
}

// ListFiles handles GET /v1/files.
func (h *GatewayResourceHandler) ListFiles(c *gin.Context) {
	key, ok := h.resourceKey(c)
	if !ok || !h.requireOpenAIPlatform(c) {
		return
	}
	limit, ok := h.resourceListLimit(c)
	if !ok {
		return
	}
	purpose, family := c.Query("purpose"), "files"
	switch purpose {
	case "", service.GatewayResourceFilePurposeBatch:
	case "batch_output":
		family = "batch_output"
	default:
		h.openAIError(c, http.StatusBadRequest, "invalid_request_error", "Unsupported file purpose")
		return
	}
	cursor, ok := h.resourceCursor(c, false)
	if !ok {
		return
	}
	jobs, err := h.upstreamJobs.ListGatewayResources(c.Request.Context(), service.PlatformOpenAI, key.UserID, key.ID, *key.GroupID, family, purpose, cursor, limit)
	if err != nil {
		h.resourceStoreError(c, err)
		return
	}
	more := len(jobs) > limit
	if more {
		jobs = jobs[:limit]
	}
	data := make([]gin.H, 0, len(jobs))
	for _, job := range jobs {
		if job.Family == "batch" {
			data = append(data, gatewayResourceOutputFileObject(job))
			continue
		}
		data = append(data, gatewayResourceFileObject(job))
	}
	h.writeResourceList(c, data, more, true)
}

// GetFile handles GET /v1/files/:file_id.
func (h *GatewayResourceHandler) GetFile(c *gin.Context) {
	key, ok := h.resourceKey(c)
	if !ok || !h.requireOpenAIPlatform(c) {
		return
	}
	job, ok := h.resolveOpenAIFile(c, key, c.Param("file_id"))
	if !ok {
		return
	}
	if job.Family == "batch" {
		if job.Status != "succeeded" {
			h.openAIError(c, http.StatusConflict, "resource_not_ready", "Output file is not ready")
			return
		}
		c.JSON(http.StatusOK, gatewayResourceOutputFileObject(job))
		return
	}
	c.JSON(http.StatusOK, gatewayResourceFileObject(job))
}

// DownloadFileContent handles GET /v1/files/:file_id/content.
func (h *GatewayResourceHandler) DownloadFileContent(c *gin.Context) {
	key, ok := h.resourceKey(c)
	if !ok || !h.requireOpenAIPlatform(c) {
		return
	}
	job, ok := h.resolveOpenAIFile(c, key, c.Param("file_id"))
	if !ok {
		return
	}
	if job.Family == "batch" {
		if job.Status != "succeeded" || job.ResultRef == "" {
			h.openAIError(c, http.StatusConflict, "resource_not_ready", "Resource content is not ready")
			return
		}
	} else if job.Status != "succeeded" || job.UpstreamTaskID == "" {
		h.openAIError(c, http.StatusConflict, "resource_not_ready", "Resource content is not ready")
		return
	}
	response, err := h.openAI.gatewayService.OpenOpenAIResourceContent(c.Request.Context(), job)
	if err != nil {
		h.resourceUpstreamError(c, err)
		return
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		h.openAIError(c, http.StatusBadGateway, "resource_content_unavailable", "Resource content unavailable")
		return
	}
	h.streamResourceContent(c, response, "application/jsonl")
}

// DeleteFile handles DELETE /v1/files/:file_id, including batch outputs.
func (h *GatewayResourceHandler) DeleteFile(c *gin.Context) {
	key, ok := h.resourceKey(c)
	if !ok || !h.requireOpenAIPlatform(c) {
		return
	}
	id := c.Param("file_id")
	family, action, publicID := "files", "delete", id
	if strings.HasPrefix(id, service.GatewayResourceOutputPrefix) {
		family, action, publicID = "batch", "delete_output", id
		id = strings.TrimPrefix(id, service.GatewayResourceOutputPrefix)
	}
	if _, err := h.upstreamJobs.ManageGatewayResource(c.Request.Context(), id, service.PlatformOpenAI, family, key.UserID, key.ID, *key.GroupID, action, h.openAI.gatewayService.ManageOpenAIResource); err != nil {
		h.resourceOperationError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": publicID, "object": "file", "deleted": true})
}

// CreateBatch handles POST /v1/batches.
func (h *GatewayResourceHandler) CreateBatch(c *gin.Context) {
	key, ok := h.resourceKey(c)
	if !ok || !h.requireOpenAIPlatform(c) {
		return
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.openAIError(c, http.StatusUnauthorized, "authentication_error", "Invalid user context")
		return
	}
	body, ok := h.readResourceBody(c, gatewayResourceMetadataBodyLimit)
	if !ok {
		return
	}
	request, err := service.ParseGatewayResourceBatchRequest(body)
	if err != nil {
		h.openAIError(c, http.StatusUnprocessableEntity, "unsupported_operation_error", err.Error())
		return
	}
	inputFile, ok := h.resolveOpenAIFile(c, key, request.InputFileID)
	if !ok {
		return
	}
	if inputFile.Family != "files" {
		h.openAIError(c, http.StatusNotFound, "not_found_error", "Input file not found")
		return
	}
	if inputFile.Status != "succeeded" || inputFile.Billing.ResourceFile == nil || inputFile.Billing.ResourceFile.Purpose != service.GatewayResourceFilePurposeBatch {
		h.openAIError(c, http.StatusConflict, "resource_not_ready", "Input file is not a ready batch input")
		return
	}
	if request.Endpoint != inputFile.Billing.ResourceFile.BatchEndpoint {
		h.openAIError(c, http.StatusUnprocessableEntity, "invalid_request_error", "Endpoint must match the uploaded file")
		return
	}
	account, err := h.openAI.gatewayService.ResolveOpenAIResourceAccount(c.Request.Context(), inputFile, true)
	if err != nil {
		h.openAIError(c, http.StatusServiceUnavailable, "resource_account_unavailable", "Input file account unavailable")
		return
	}
	model, err := h.authorizeBatchModel(c, account, inputFile.Billing.ResourceFile.BatchModel, request.Endpoint)
	if err != nil {
		h.openAIError(c, http.StatusUnprocessableEntity, "unsupported_model", err.Error())
		return
	}
	// The submission's child job plan inherits TPM storage and frozen scope from
	// this ingress plan, including keys without a monetary period budget.
	defer beginGatewayRequestPlan(c, h.openAI.gatewayService, key, model, "resources", h.openAI.maxAccountSwitches)()
	release, ok := h.checkResourceAdmission(c, key, subject)
	if !ok {
		return
	}
	if release != nil {
		defer release()
	}
	// The batch runs on the account that already holds the validated input file,
	// so it takes that account's slot instead of scheduling a new one.
	accountRelease, acquired, err := h.openAI.concurrencyHelper.TryAcquireAccountSlot(c.Request.Context(), account.ID, account.Concurrency)
	if err != nil || !acquired {
		h.openAIError(c, http.StatusTooManyRequests, "resource_account_busy", "Input file account is busy")
		return
	}
	if accountRelease != nil {
		defer accountRelease()
	}
	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	billing, err := h.batchBilling(c, key, account, subscription, service.PlatformOpenAI, model, request.Endpoint, inputFile.Billing.ResourceFile.CustomIDHashes, inputFile.Billing.ResourceFile.Lines, inputFile.PublicRequestID)
	if err != nil {
		h.openAIError(c, http.StatusUnprocessableEntity, "unsupported_pricing", err.Error())
		return
	}
	fingerprint, err := service.CanonicalUpstreamJobBody(body)
	if err != nil {
		h.openAIError(c, http.StatusBadRequest, "invalid_request_error", "Invalid batch JSON")
		return
	}
	template := &service.UpstreamJob{Platform: service.PlatformOpenAI, Family: "batch", AccountID: account.ID, UserID: key.UserID, APIKeyID: key.ID, GroupID: key.GroupID, Billing: billing}
	requestBody := append([]byte(nil), fingerprint...)
	// The provider receives its own input file id; the public id stays inside the
	// gateway, so a stored file can never be addressed by guessing a file id.
	upstreamBody, err := service.GatewayResourceBatchUpstreamBody(requestBody, inputFile.UpstreamTaskID)
	if err != nil {
		h.resourceOperationError(c, err)
		return
	}
	job, err := h.upstreamJobs.WithGatewayResourceInput(c.Request.Context(), inputFile.PublicRequestID, service.PlatformOpenAI, key.UserID, key.ID, *key.GroupID, func() (*service.UpstreamJob, error) {
		return h.upstreamJobs.Submit(c.Request.Context(), template, requestBody, func(ctx context.Context) (service.UpstreamJobUpdate, error) {
			return h.openAI.gatewayService.SubmitOpenAIBatch(ctx, account, upstreamBody)
		})
	})
	if err != nil {
		h.resourceOperationError(c, err)
		return
	}
	if h.writeResourceSubmitFailure(c, job) {
		return
	}
	setOpsSelectedAccount(c, job.AccountID, service.PlatformOpenAI)
	c.JSON(http.StatusOK, gatewayResourceBatchObject(job))
}

// ListBatches handles GET /v1/batches.
func (h *GatewayResourceHandler) ListBatches(c *gin.Context) {
	key, ok := h.resourceKey(c)
	if !ok || !h.requireOpenAIPlatform(c) {
		return
	}
	limit, ok := h.resourceListLimit(c)
	if !ok {
		return
	}
	cursor, ok := h.resourceCursor(c, false)
	if !ok {
		return
	}
	jobs, err := h.upstreamJobs.ListGatewayResources(c.Request.Context(), service.PlatformOpenAI, key.UserID, key.ID, *key.GroupID, "batch", "", cursor, limit)
	if err != nil {
		h.resourceStoreError(c, err)
		return
	}
	more := len(jobs) > limit
	if more {
		jobs = jobs[:limit]
	}
	data := make([]gin.H, 0, len(jobs))
	for _, job := range jobs {
		data = append(data, gatewayResourceBatchObject(job))
	}
	h.writeResourceList(c, data, more, true)
}

// GetBatch handles GET /v1/batches/:batch_id.
func (h *GatewayResourceHandler) GetBatch(c *gin.Context) {
	key, ok := h.resourceKey(c)
	if !ok || !h.requireOpenAIPlatform(c) {
		return
	}
	job, err := h.upstreamJobs.GetGatewayResource(c.Request.Context(), c.Param("batch_id"), key.UserID, key.ID, service.PlatformOpenAI, "batch")
	if err != nil {
		h.resourceStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gatewayResourceBatchObject(job))
}

// CancelBatch handles POST /v1/batches/:batch_id/cancel.
func (h *GatewayResourceHandler) CancelBatch(c *gin.Context) {
	key, ok := h.resourceKey(c)
	if !ok || !h.requireOpenAIPlatform(c) {
		return
	}
	job, err := h.upstreamJobs.ManageGatewayResource(c.Request.Context(), c.Param("batch_id"), service.PlatformOpenAI, "batch", key.UserID, key.ID, *key.GroupID, "cancel", h.openAI.gatewayService.ManageOpenAIResource)
	if err != nil {
		h.resourceOperationError(c, err)
		return
	}
	c.JSON(http.StatusOK, gatewayResourceBatchObject(job))
}

// Moderations handles POST /v1/moderations.
func (h *GatewayResourceHandler) Moderations(c *gin.Context) {
	key, ok := h.resourceKey(c)
	if !ok || !h.requireOpenAIPlatform(c) {
		return
	}
	body, ok := h.readResourceBody(c, gatewayResourceMetadataBodyLimit)
	if !ok {
		return
	}
	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	if err := h.openAI.billingCacheService.CheckBillingEligibility(c.Request.Context(), key.User, key, key.Group, subscription, service.QuotaPlatform(c.Request.Context(), key)); err != nil {
		status, code, message, _ := billingErrorDetails(err)
		writeAdmissionRejectionHeaders(c, err)
		h.openAIError(c, status, code, message)
		return
	}
	sessionHash := h.openAI.gatewayService.GenerateSessionHash(c, body)
	account, accountRelease, ok := h.selectResourceAccount(c, key, service.PlatformOpenAI, sessionHash)
	if !ok {
		return
	}
	if accountRelease != nil {
		defer accountRelease()
	}
	response, err := h.openAI.gatewayService.ForwardOpenAIModeration(c.Request.Context(), account, body)
	if err != nil {
		h.openAIError(c, http.StatusBadGateway, "upstream_error", "Moderation request failed")
		return
	}
	defer func() { _ = response.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(response.Body, gatewayResourceMetadataBodyLimit+1))
	if err != nil || int64(len(raw)) > gatewayResourceMetadataBodyLimit {
		h.openAIError(c, http.StatusBadGateway, "upstream_error", "Moderation response unavailable")
		return
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		h.openAIError(c, http.StatusBadGateway, "upstream_error", "Moderation request rejected upstream")
		return
	}
	setOpsSelectedAccount(c, account.ID, service.PlatformOpenAI)
	c.Data(response.StatusCode, "application/json", raw)
}

// CreateMessageBatch handles POST /v1/messages/batches.
func (h *GatewayResourceHandler) CreateMessageBatch(c *gin.Context) {
	key, ok := h.resourceKey(c)
	if !ok || !h.requireAnthropicPlatform(c) {
		return
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.anthropicError(c, http.StatusUnauthorized, "authentication_error", "Invalid user context")
		return
	}
	body, ok := h.readResourceBody(c, service.GatewayResourceFileLimit)
	if !ok {
		return
	}
	admission, err := service.ValidateAnthropicMessageBatchAdmission(body)
	if err != nil {
		h.anthropicError(c, http.StatusUnprocessableEntity, "invalid_request_error", err.Error())
		return
	}
	// Each item is an independent prompt. A batch must not reuse the first
	// item's HTTP audit completion cache for the remaining requests.
	var auditBatch struct {
		Requests []struct {
			Params json.RawMessage `json:"params"`
		} `json:"requests"`
	}
	if err := json.Unmarshal(body, &auditBatch); err != nil {
		h.anthropicError(c, http.StatusBadRequest, "invalid_request_error", "Invalid batch JSON")
		return
	}
	for _, item := range auditBatch.Requests {
		decision := h.openAI.checkSecurityAuditStage(c, requestLogger(c, "handler.gateway_resource_actions"), key, subject,
			service.ContentModerationProtocolAnthropicMessages, admission.Model, item.Params, "batch_item")
		if decision != nil && !decision.AllowNextStage {
			h.openAI.openAISecurityAuditError(c, decision)
			return
		}
	}
	canonical, err := service.CanonicalUpstreamJobBody(body)
	if err != nil {
		h.anthropicError(c, http.StatusBadRequest, "invalid_request_error", "Invalid batch JSON")
		return
	}
	sessionHash := h.openAI.gatewayService.GenerateSessionHash(c, canonical)
	account, accountRelease, ok := h.selectResourceAccount(c, key, service.PlatformAnthropic, sessionHash)
	if !ok {
		return
	}
	if accountRelease != nil {
		defer accountRelease()
	}
	model, err := h.authorizeBatchModel(c, account, admission.Model, service.CompositeRouteEndpointMessages)
	if err != nil {
		h.anthropicError(c, http.StatusUnprocessableEntity, "invalid_request_error", err.Error())
		return
	}
	release, ok := h.checkResourceAdmission(c, key, subject)
	if !ok {
		return
	}
	if release != nil {
		defer release()
	}
	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	billing, err := h.batchBilling(c, key, account, subscription, service.PlatformAnthropic, model, service.CompositeRouteEndpointMessages, admission.CustomIDHashes, admission.RequestCount, "")
	if err != nil {
		h.anthropicError(c, http.StatusUnprocessableEntity, "invalid_request_error", err.Error())
		return
	}
	template := &service.UpstreamJob{Platform: service.PlatformAnthropic, Family: "batch", AccountID: account.ID, UserID: key.UserID, APIKeyID: key.ID, GroupID: key.GroupID, Billing: billing}
	job, err := h.upstreamJobs.Submit(c.Request.Context(), template, canonical, func(ctx context.Context) (service.UpstreamJobUpdate, error) {
		return h.openAI.gatewayService.SubmitAnthropicMessageBatch(ctx, account, template, canonical)
	})
	if err != nil {
		h.anthropicError(c, http.StatusServiceUnavailable, "api_error", "Unable to persist the message batch")
		return
	}
	if h.writeResourceSubmitFailure(c, job) {
		return
	}
	setOpsSelectedAccount(c, job.AccountID, service.PlatformAnthropic)
	c.JSON(http.StatusOK, gatewayResourceMessageBatchObject(job))
}

// ListMessageBatches handles GET /v1/messages/batches. The durable list is
// scoped to the caller's workspace, so it never exposes another tenant's batch.
func (h *GatewayResourceHandler) ListMessageBatches(c *gin.Context) {
	key, ok := h.resourceKey(c)
	if !ok || !h.requireAnthropicPlatform(c) {
		return
	}
	limit, ok := h.resourceListLimit(c)
	if !ok {
		return
	}
	cursor, ok := h.resourceCursor(c, true)
	if !ok {
		return
	}
	jobs, err := h.upstreamJobs.ListGatewayResources(c.Request.Context(), service.PlatformAnthropic, key.UserID, key.ID, *key.GroupID, "batch", "", cursor, limit)
	if err != nil {
		h.resourceStoreError(c, err)
		return
	}
	more := len(jobs) > limit
	if more {
		jobs = jobs[:limit]
	}
	data := make([]gin.H, 0, len(jobs))
	for _, job := range jobs {
		data = append(data, gatewayResourceMessageBatchObject(job))
	}
	h.writeResourceList(c, data, more, false)
}

// GetMessageBatch handles GET /v1/messages/batches/:message_batch_id.
func (h *GatewayResourceHandler) GetMessageBatch(c *gin.Context) {
	key, ok := h.resourceKey(c)
	if !ok || !h.requireAnthropicPlatform(c) {
		return
	}
	job, err := h.upstreamJobs.GetGatewayResource(c.Request.Context(), c.Param("message_batch_id"), key.UserID, key.ID, service.PlatformAnthropic, "batch")
	if err != nil {
		h.resourceStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gatewayResourceMessageBatchObject(job))
}

// CancelMessageBatch handles POST /v1/messages/batches/:message_batch_id/cancel.
func (h *GatewayResourceHandler) CancelMessageBatch(c *gin.Context) {
	key, ok := h.resourceKey(c)
	if !ok || !h.requireAnthropicPlatform(c) {
		return
	}
	job, err := h.upstreamJobs.ManageGatewayResource(c.Request.Context(), c.Param("message_batch_id"), service.PlatformAnthropic, "batch", key.UserID, key.ID, *key.GroupID, "cancel", h.openAI.gatewayService.ManageAnthropicResource)
	if err != nil {
		h.resourceOperationError(c, err)
		return
	}
	c.JSON(http.StatusOK, gatewayResourceMessageBatchObject(job))
}

// DeleteMessageBatch handles DELETE /v1/messages/batches/:message_batch_id.
func (h *GatewayResourceHandler) DeleteMessageBatch(c *gin.Context) {
	key, ok := h.resourceKey(c)
	if !ok || !h.requireAnthropicPlatform(c) {
		return
	}
	id := c.Param("message_batch_id")
	job, err := h.upstreamJobs.ManageGatewayResource(c.Request.Context(), id, service.PlatformAnthropic, "batch", key.UserID, key.ID, *key.GroupID, "delete", h.openAI.gatewayService.ManageAnthropicResource)
	if err != nil {
		h.resourceOperationError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": job.PublicRequestID, "type": "message_batch_deleted"})
}

// MessageBatchResults handles GET /v1/messages/batches/:message_batch_id/results.
func (h *GatewayResourceHandler) MessageBatchResults(c *gin.Context) {
	key, ok := h.resourceKey(c)
	if !ok || !h.requireAnthropicPlatform(c) {
		return
	}
	job, err := h.upstreamJobs.GetGatewayResource(c.Request.Context(), c.Param("message_batch_id"), key.UserID, key.ID, service.PlatformAnthropic, "batch")
	if err != nil {
		h.resourceStoreError(c, err)
		return
	}
	if job.Status != "succeeded" {
		h.anthropicError(c, http.StatusConflict, "invalid_request_error", "Message batch results are not ready")
		return
	}
	response, err := h.openAI.gatewayService.OpenAnthropicMessageBatchResults(c.Request.Context(), job)
	if err != nil {
		h.resourceUpstreamError(c, err)
		return
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		h.anthropicError(c, http.StatusBadGateway, "api_error", "Message batch results unavailable")
		return
	}
	h.streamResourceContent(c, response, "application/x-jsonl")
}

// authorizeBatchModel admits the served model for one batch and returns it.
func (h *GatewayResourceHandler) authorizeBatchModel(c *gin.Context, account *service.Account, model, endpoint string) (string, error) {
	return h.openAI.gatewayService.AuthorizeGatewayResourceNativeModel(c.Request.Context(), account, model, service.GatewayResourceBindingEndpoint(endpoint))
}

// batchBilling freezes the tariff for one admitted batch. Token targets require a
// resolved standard price; free targets (moderations) carry an explicit free
// state instead of an invented tariff.
func (h *GatewayResourceHandler) batchBilling(c *gin.Context, key *service.APIKey, account *service.Account, subscription *service.UserSubscription, platform, model, endpoint string, customIDHashes []string, requestCount int, inputFileID string) (service.UpstreamJobBilling, error) {
	admission := service.GatewayResourceBatchAdmission{
		Platform: platform, Model: model, UpstreamModel: model, Endpoint: endpoint,
		RequestCount: requestCount, CustomIDHashes: customIDHashes, InputFileID: inputFileID,
		PricingAt: time.Now(),
	}
	if platform == service.PlatformOpenAI && endpoint == "/v1/moderations" {
		admission.PricingMode = service.GatewayResourcePricingModeFree
		return service.GatewayResourceBatchBillingSnapshot(admission, key.Group, account, key, subscription)
	}
	pricing, source, err := h.openAI.gatewayService.ResolveGatewayResourceStandardPricing(c.Request.Context(), key.Group, model)
	if err != nil {
		return service.UpstreamJobBilling{}, err
	}
	admission.PricingMode = service.GatewayResourcePricingModeTokens
	admission.StandardPrices = pricing
	admission.PriceSource = source
	admission.UserGroupRateMultiplier = h.openAI.gatewayService.ResolveUserGroupRateMultiplier(c.Request.Context(), key.UserID, *key.GroupID, key.Group.RateMultiplier)
	return service.GatewayResourceBatchBillingSnapshot(admission, key.Group, account, key, subscription)
}

// resolveOpenAIFile resolves a public file id, including the virtual batch output
// files, under the caller's ownership. A deleted resource is reported exactly
// like a missing one: the provider file no longer exists.
func (h *GatewayResourceHandler) resolveOpenAIFile(c *gin.Context, key *service.APIKey, id string) (*service.UpstreamJob, bool) {
	family, output := "files", false
	if strings.HasPrefix(id, service.GatewayResourceOutputPrefix) {
		family, output, id = "batch", true, strings.TrimPrefix(id, service.GatewayResourceOutputPrefix)
	}
	job, err := h.upstreamJobs.GetGatewayResource(c.Request.Context(), id, key.UserID, key.ID, service.PlatformOpenAI, family)
	if err != nil || job.ResourceState.Deleted || output && job.ResourceState.OutputDeleted {
		h.resourceNotFound(c)
		return nil, false
	}
	return job, true
}

// resourceListLimit validates the page size and the documented sort order. An
// unsupported order is refused instead of being silently served in another one.
func (h *GatewayResourceHandler) resourceListLimit(c *gin.Context) (int, bool) {
	limit := 20
	if raw := c.Query("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			h.openAIError(c, http.StatusBadRequest, "invalid_request_error", "limit must be 1-100")
			return 0, false
		}
		limit = parsed
	}
	if order := c.Query("order"); order != "" && order != "desc" {
		h.openAIError(c, http.StatusBadRequest, "invalid_request_error", "order must be desc")
		return 0, false
	}
	return limit, true
}

// resourceCursor returns the page cursor of one list request. Anthropic pages
// with after_id/before_id; the OpenAI surface uses after. A backwards page is
// refused explicitly instead of answering with another direction's page.
func (h *GatewayResourceHandler) resourceCursor(c *gin.Context, anthropic bool) (string, bool) {
	cursor := c.Query("after")
	if !anthropic {
		return cursor, true
	}
	if c.Query("before_id") != "" || c.Query("before") != "" {
		h.openAIError(c, http.StatusBadRequest, "invalid_pagination", "before_id is not supported; page forward with after_id")
		return "", false
	}
	if afterID := c.Query("after_id"); afterID != "" {
		cursor = afterID
	}
	return cursor, true
}

func (h *GatewayResourceHandler) resourceStoreError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrUpstreamJobNotFound):
		h.resourceNotFound(c)
	case errors.Is(err, service.ErrGatewayResourcePagination):
		h.openAIError(c, http.StatusBadRequest, "invalid_pagination", "Pagination cursor is not available")
	case errors.Is(err, service.ErrUpstreamResourceConflict):
		h.openAIError(c, http.StatusConflict, "resource_conflict", "Resource is not ready or is in use")
	default:
		h.openAIError(c, http.StatusServiceUnavailable, "resource_store_unavailable", "Resource store unavailable")
	}
}

func (h *GatewayResourceHandler) resourceOperationError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrUpstreamJobNotFound):
		h.resourceNotFound(c)
	case errors.Is(err, service.ErrUpstreamResourceConflict):
		h.openAIError(c, http.StatusConflict, "resource_conflict", "Resource is not ready or is in use")
	default:
		h.openAIError(c, http.StatusBadGateway, "resource_operation_unavailable", "Resource operation rejected upstream")
	}
}

func (h *GatewayResourceHandler) resourceUpstreamError(c *gin.Context, err error) {
	if errors.Is(err, service.ErrUpstreamJobNotFound) {
		h.resourceNotFound(c)
		return
	}
	if errors.Is(err, service.ErrUpstreamResourceConflict) {
		h.openAIError(c, http.StatusConflict, "resource_not_ready", "Resource content is not ready")
		return
	}
	h.openAIError(c, http.StatusBadGateway, "resource_content_unavailable", "Resource content unavailable")
}

// streamResourceContent copies one upstream stream under an explicit bound. A
// violation aborts the response instead of presenting a truncated body as
// complete.
func (h *GatewayResourceHandler) streamResourceContent(c *gin.Context, response *http.Response, contentType string) {
	if length := response.ContentLength; length > service.GatewayResourceDownloadLimit {
		h.openAIError(c, http.StatusRequestEntityTooLarge, "request_too_large", "Resource content exceeds the download limit")
		return
	}
	c.Header("Content-Type", contentType)
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Status(http.StatusOK)
	written, err := io.Copy(c.Writer, io.LimitReader(response.Body, service.GatewayResourceDownloadLimit+1))
	if err != nil || written > service.GatewayResourceDownloadLimit {
		// The body is already streaming; end the transfer visibly instead of
		// presenting a truncated download as complete.
		h.abortResourceStream(c)
	}
}

// abortResourceStream closes an in-flight stream that exceeded its bound. A
// hijackable connection is closed so the client observes an incomplete
// transfer; other transports abort the handler.
func (h *GatewayResourceHandler) abortResourceStream(c *gin.Context) {
	if h.hijackResourceStream(c) {
		return
	}
	panic(http.ErrAbortHandler)
}

// hijackResourceStream closes the connection of an aborted stream. Only HTTP/1.x
// writers support hijacking; gin's writer panics instead of reporting an error
// for every other transport, so the attempt is recovered and reported as "not
// hijackable" instead of replacing the abort with a type assertion panic.
func (h *GatewayResourceHandler) hijackResourceStream(c *gin.Context) (closed bool) {
	hijacker, ok := c.Writer.(http.Hijacker)
	if !ok {
		return false
	}
	defer func() {
		if recover() != nil {
			closed = false
		}
	}()
	connection, _, err := hijacker.Hijack()
	if err != nil {
		return false
	}
	_ = connection.Close()
	return true
}

func (h *GatewayResourceHandler) writeResourceList(c *gin.Context, data []gin.H, more, openAIEnvelope bool) {
	out := gin.H{"data": data, "has_more": more, "first_id": nil, "last_id": nil}
	if openAIEnvelope {
		out["object"] = "list"
	}
	if len(data) > 0 {
		out["first_id"] = data[0]["id"]
		out["last_id"] = data[len(data)-1]["id"]
	}
	c.JSON(http.StatusOK, out)
}
