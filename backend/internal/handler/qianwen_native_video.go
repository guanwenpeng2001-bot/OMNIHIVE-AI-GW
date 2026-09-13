package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// Ownership is checked by opaque public ID before exposing state or results.
// GET reads durable state; the timing wheel polls independently of the client.
func (h *OpenAIGatewayHandler) handleQianwenNativeAsyncJobs(c *gin.Context) bool {
	path := c.Request.URL.Path
	family := service.QianwenAsyncFamilyForPath(path)
	submit := family != ""
	lookup := strings.HasPrefix(path, "/api/v1/tasks/") && c.Request.Method == http.MethodGet
	if !submit && !lookup {
		return false
	}
	if submit && c.Request.Method != http.MethodPost {
		h.errorResponse(c, 404, "unsupported_operation_error", "Async submission requires POST")
		return true
	}
	if h.upstreamJobs == nil {
		h.errorResponse(c, 503, "unsupported_operation_error", "Persistent async jobs unavailable")
		return true
	}
	key, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok || key == nil || key.Group == nil {
		h.errorResponse(c, 401, "authentication_error", "Invalid API key")
		return true
	}
	if lookup {
		id := strings.TrimPrefix(path, "/api/v1/tasks/")
		job, err := h.upstreamJobs.Get(c.Request.Context(), id, key.UserID, key.ID)
		if errors.Is(err, service.ErrUpstreamJobNotFound) || err == nil && (job.Platform != service.PlatformQianwen || job.Family != "video" && job.Family != "3d" && job.Family != "asr") {
			h.errorResponse(c, 404, "not_found_error", "Task not found")
			return true
		}
		if err != nil {
			h.errorResponse(c, 503, "api_error", "Task store unavailable")
			return true
		}
		writeQianwenAsyncJob(c, job, http.StatusOK)
		return true
	}
	if openAICompatibleRequestPlatform(c.Request.Context(), key) != service.PlatformQianwen {
		h.errorResponse(c, 404, "unsupported_operation_error", "Async submission requires a Qianwen group")
		return true
	}
	body, err := readLenientJSONRequestBodyWithPrealloc(c.Request, h.cfg)
	if err != nil || !gjson.ValidBytes(body) {
		h.errorResponse(c, 400, "invalid_request_error", "Invalid async JSON")
		return true
	}
	body, err = service.CanonicalUpstreamJobBody(body)
	if err != nil {
		h.errorResponse(c, 400, "invalid_request_error", "Invalid async JSON")
		return true
	}
	model := gjson.GetBytes(body, "model").String()
	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	// Fail before account admission when no explicit model/resolution rate exists.
	if _, err = service.QianwenAsyncBillingSnapshot(family, key.Group, nil, key, body, subscription); err != nil {
		h.errorResponse(c, 422, "unsupported_operation_error", err.Error())
		return true
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.errorResponse(c, 401, "authentication_error", "Invalid user context")
		return true
	}
	reqLog := requestLogger(c, "handler.qianwen_async")
	if decision := h.checkSecurityAudit(c, reqLog, key, subject, service.ContentModerationProtocolOpenAIChat, model, qianwenNativeAuditBody(body)); decision != nil && !decision.AllowNextStage {
		h.openAISecurityAuditError(c, decision)
		return true
	}
	if h.rejectIfCyberSessionBlocked(c, key, body, model, cyberBlockFormatChat) {
		return true
	}
	if err = h.billingCacheService.CheckBillingEligibility(c.Request.Context(), key.User, key, key.Group, subscription, service.QuotaPlatform(c.Request.Context(), key)); err != nil {
		status, code, message, _ := billingErrorDetails(err)
		h.errorResponse(c, status, code, message)
		return true
	}
	streamStarted := false
	release, ok := h.acquireResponsesUserSlot(c, subject.UserID, subject.Concurrency, false, &streamStarted, reqLog)
	if !ok {
		return true
	}
	if release != nil {
		defer release()
	}
	sessionHash := h.gatewayService.GenerateSessionHash(c, body)
	selection, _, err := h.gatewayService.SelectAccountWithSchedulerForCapability(c.Request.Context(), key.GroupID, "", sessionHash, model, nil, service.OpenAIUpstreamTransportAny, service.OpenAIEndpointCapabilityChatCompletions, false, false, true, service.PlatformQianwen)
	if err != nil || selection == nil || selection.Account == nil {
		h.errorResponse(c, 503, "api_error", "No async account available")
		return true
	}
	account := selection.Account
	accountRelease, slot := h.acquireResponsesAccountSlot(c, key.GroupID, sessionHash, selection, false, &streamStarted, reqLog)
	if slot != openAISlotAcquireOK {
		return true
	}
	if accountRelease != nil {
		defer accountRelease()
	}
	snapshot, err := service.QianwenAsyncBillingSnapshot(family, key.Group, account, key, body, subscription)
	if err != nil {
		h.errorResponse(c, 422, "unsupported_operation_error", err.Error())
		return true
	}
	if (family != "video" || !key.Group.VideoRateIndependent) && key.GroupID != nil {
		snapshot.RateMultiplier = h.gatewayService.ResolveUserGroupRateMultiplier(c.Request.Context(), key.UserID, *key.GroupID, key.Group.RateMultiplier) * key.Group.PeakMultiplierAt(time.Now())
	}
	job, err := h.upstreamJobs.Submit(c.Request.Context(), &service.UpstreamJob{Platform: service.PlatformQianwen, Family: family, AccountID: account.ID, UserID: key.UserID, APIKeyID: key.ID, GroupID: key.GroupID, Billing: snapshot}, body, func(ctx context.Context) (service.UpstreamJobUpdate, error) {
		return h.gatewayService.SubmitQianwenAsync(ctx, account, family, body)
	})
	if err != nil {
		h.errorResponse(c, 503, "api_error", "Unable to persist async task")
		return true
	}
	setOpsSelectedAccount(c, account.ID, account.Platform)
	writeQianwenAsyncJob(c, job, http.StatusAccepted)
	return true
}

func writeQianwenAsyncJob(c *gin.Context, j *service.UpstreamJob, status int) {
	state := strings.ToUpper(j.Status)
	if j.Status == "submitted" {
		state = "PENDING"
	}
	output := gin.H{"task_id": j.PublicRequestID, "task_status": state}
	if j.Status == "succeeded" {
		if j.Family == "video" {
			output["video_url"] = j.ResultRef
		} else {
			output["results"] = json.RawMessage(j.ResultRef)
		}
	}
	if j.ErrorCode != "" {
		output["code"] = j.ErrorCode
		output["message"] = j.ErrorMessage
	}
	c.JSON(status, gin.H{"request_id": j.PublicRequestID, "output": output, "usage": j.UsageJSON})
}
