package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

	"github.com/alitto/pond/v2"
	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	infraerrors "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/errors"
	pkghttputil "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/httputil"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/logger"
	middleware2 "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"go.uber.org/zap"
)

type AsyncImageHandler struct {
	poolOnce sync.Once
	pool     pond.Pool
	tasks    *service.ImageTaskService
	openAI   *OpenAIGatewayHandler
	execute  func(platform string, c *gin.Context)
}

func NewAsyncImageHandler(tasks *service.ImageTaskService, openAI *OpenAIGatewayHandler) *AsyncImageHandler {
	h := &AsyncImageHandler{tasks: tasks, openAI: openAI}
	h.execute = h.executeWithGateway
	return h
}

// enabled reports whether the async image task feature is available. Object
// storage is the enablement gate: without it the endpoints are fully disabled
// so that large base64 results never land in Redis.
func (h *AsyncImageHandler) enabled() bool {
	return h != nil && h.tasks != nil && h.tasks.Enabled()
}

// pollable reports whether task lookups can be served. It is deliberately weaker
// than enabled(): results already written to Redis stay readable after the
// feature is switched off, so an in-flight task is never stranded.
func (h *AsyncImageHandler) pollable() bool {
	return h != nil && h.tasks != nil && h.tasks.Pollable()
}

// Submit accepts the same payload as the synchronous Images endpoint and
// returns before the upstream image generation begins.
func (h *AsyncImageHandler) Submit(c *gin.Context) {
	if !h.enabled() {
		imageTaskJSONError(c, http.StatusNotFound, "not_found_error", "async image tasks are not enabled")
		return
	}
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok || apiKey == nil || apiKey.UserID <= 0 || apiKey.ID <= 0 {
		imageTaskError(c, service.ErrImageTaskForbidden)
		return
	}
	// The async submit resolves its target platform exactly like the synchronous
	// Images endpoint: a Composite group carries its resolved target in the
	// request context, so rejecting on apiKey.Group.Platform would refuse every
	// Composite task the synchronous route accepts.
	platform := service.GatewayTargetPlatform(c)
	if platform != service.PlatformOpenAI && platform != service.PlatformGrok {
		imageTaskJSONError(c, http.StatusNotFound, "not_found_error", "Images API is not supported for this platform")
		return
	}
	if !service.GroupAllowsImageGeneration(apiKey.Group) {
		imageTaskJSONError(c, http.StatusForbidden, "permission_error", service.ImageGenerationPermissionMessage())
		return
	}
	if h == nil || h.tasks == nil || h.execute == nil {
		imageTaskError(c, service.ErrImageTaskUnavailable)
		return
	}

	body, err := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
	if err != nil {
		if maxErr, ok := extractMaxBytesError(err); ok {
			imageTaskJSONError(c, http.StatusRequestEntityTooLarge, "invalid_request_error", buildBodyTooLargeMessage(maxErr.Limit))
			return
		}
		imageTaskJSONError(c, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
		return
	}
	if len(body) == 0 {
		imageTaskJSONError(c, http.StatusBadRequest, "invalid_request_error", "Request body is empty")
		return
	}
	if asyncImageRequestStreams(c.GetHeader("Content-Type"), body) {
		imageTaskJSONError(c, http.StatusBadRequest, "invalid_request_error", "streaming image requests cannot be submitted as asynchronous tasks")
		return
	}
	model, err := h.validateRequest(c, platform, body)
	if err != nil {
		imageTaskJSONError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	if !h.checkSecurityAuditBeforeSubmit(c, apiKey, platform, body) {
		return
	}

	publicModel := service.ParseGrokMediaRequest(c.GetHeader("Content-Type"), body).Model
	if platform == service.PlatformOpenAI && h.openAI != nil && h.openAI.gatewayService != nil {
		if parsed, parseErr := h.openAI.gatewayService.ParseOpenAIImagesRequest(c, body); parseErr == nil {
			publicModel = parsed.Model
		}
	}
	task, err := h.tasks.Create(c.Request.Context(), imageTaskOwner(apiKey), publicModel)
	if err != nil {
		imageTaskError(c, err)
		return
	}
	spec := service.GatewayRequestSpec{UserID: apiKey.UserID, APIKeyID: apiKey.ID, Model: model,
		Route: strings.TrimSuffix(c.Request.URL.Path, "/async"), Protocol: "images", ExecutionID: task.ID}
	if platform == service.PlatformGrok {
		spec.Protocol = "media"
	}
	if apiKey.GroupID != nil {
		spec.BillingGroupID = *apiKey.GroupID
	}
	var gateway *service.OpenAIGatewayService
	if h.openAI != nil {
		gateway = h.openAI.gatewayService
		spec.MaxAccountSwitches = h.openAI.maxAccountSwitches
	}
	executionCtx, cancel := gateway.CaptureAsyncImageTaskPlan(c.Request.Context(), spec, h.tasks.ExecutionTimeout())
	if admission, err := service.GatewayRequestPlanFromContext(executionCtx).AdmittedExecution(); err != nil {
		cancel()
		h.failTask(task.ID, http.StatusServiceUnavailable, imageTaskErrorPayload("server_error", "Execution admission unavailable"))
		imageTaskError(c, service.ErrImageTaskUnavailable)
		return
	} else if admission.RequestID != "" {
		c.Header("X-Execution-ID", admission.RequestID)
	}
	taskCtx, recorder := newAsyncImageContext(c, body, executionCtx)

	h.poolOnce.Do(func() {
		if h.pool == nil {
			h.pool = pond.NewPool(8, pond.WithQueueSize(32))
		}
	})
	if _, accepted := h.pool.TrySubmit(func() {
		if taskCtx.Request.Context().Err() != nil {
			cancel()
			h.failTask(task.ID, http.StatusGatewayTimeout, imageTaskErrorPayload("timeout_error", "image task expired before execution"))
			return
		}
		h.run(task.ID, platform, taskCtx, recorder, cancel)
	}); !accepted {
		cancel()
		h.failTask(task.ID, http.StatusServiceUnavailable, imageTaskErrorPayload("queue_full", "image task queue is full"))
		imageTaskError(c, service.ErrImageTaskUnavailable)
		return
	}
	pollURL := imageTaskPollURL(c.Request.URL.Path, task.ID)
	c.Header("Cache-Control", "no-store")
	c.Header("Location", pollURL)
	c.Header("Retry-After", "3")
	c.JSON(http.StatusAccepted, gin.H{
		"id":         task.ID,
		"task_id":    task.TaskID,
		"object":     task.Object,
		"status":     task.Status,
		"created_at": task.CreatedAt,
		"expires_at": task.ExpiresAt,
		"poll_url":   pollURL,
	})

}

func (h *AsyncImageHandler) checkSecurityAuditBeforeSubmit(c *gin.Context, apiKey *service.APIKey, platform string, body []byte) bool {
	if h == nil || h.openAI == nil {
		return true
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		imageTaskJSONError(c, http.StatusInternalServerError, "api_error", "User context not found")
		return false
	}
	model := ""
	moderationBody := body
	if platform == service.PlatformGrok {
		parsed := service.ParseGrokMediaRequest(c.GetHeader("Content-Type"), body)
		model, moderationBody = parsed.Model, parsed.ModerationBody()
	} else if h.openAI.gatewayService != nil {
		parsed, err := h.openAI.gatewayService.ParseOpenAIImagesRequest(c, body)
		if err != nil {
			imageTaskJSONError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
			return false
		}
		model, moderationBody = parsed.Model, parsed.ModerationBody()
	}
	if len(moderationBody) == 0 {
		c.Set(securityAuditCompletedContextKey, true)
		return true
	}
	reqLog := requestLogger(c, "handler.async_image.security_audit",
		zap.Int64("user_id", subject.UserID), zap.Int64("api_key_id", apiKey.ID), zap.String("model", model))
	decision := h.openAI.checkSecurityAudit(c, reqLog, apiKey, subject, service.ContentModerationProtocolOpenAIImages, model, moderationBody)
	if decision != nil && !decision.AllowNextStage {
		h.openAI.openAISecurityAuditError(c, decision)
		return false
	}
	return true
}

func (h *AsyncImageHandler) Get(c *gin.Context) {
	// Polling deliberately does not require the feature to be enabled, only that
	// the task store is reachable. Turning the switch off in the admin UI must not
	// strand tasks that were already accepted — their results are still in Redis
	// and their submitters are still polling.
	if !h.pollable() {
		imageTaskJSONError(c, http.StatusNotFound, "not_found_error", "async image tasks are not enabled")
		return
	}
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok || apiKey == nil || apiKey.UserID <= 0 || apiKey.ID <= 0 {
		imageTaskError(c, service.ErrImageTaskForbidden)
		return
	}
	task, err := h.tasks.Get(c.Request.Context(), imageTaskOwner(apiKey), c.Param("task_id"))
	if err != nil {
		imageTaskError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	if task.Status == service.ImageTaskStatusProcessing {
		c.Header("Retry-After", "3")
	}
	c.JSON(http.StatusOK, task)
}

func (h *AsyncImageHandler) validateRequest(c *gin.Context, platform string, body []byte) (string, error) {
	if h.openAI == nil || h.openAI.gatewayService == nil {
		return service.ParseGrokMediaRequest(c.GetHeader("Content-Type"), body).Model, nil
	}
	if platform == service.PlatformGrok {
		parsed := service.ParseGrokMediaRequest(c.GetHeader("Content-Type"), body)
		if strings.TrimSpace(parsed.Model) == "" {
			return "", errors.New("model is required")
		}
		return parsed.Model, nil
	}
	parsed, err := h.openAI.gatewayService.ParseOpenAIImagesRequest(c, body)
	if err != nil {
		return "", err
	}
	if parsed.Stream {
		return "", errors.New("streaming image requests cannot be submitted as asynchronous tasks")
	}
	return parsed.Model, nil
}

func (h *AsyncImageHandler) executeWithGateway(platform string, c *gin.Context) {
	if h.openAI == nil {
		imageTaskJSONError(c, http.StatusServiceUnavailable, "api_error", "image gateway is unavailable")
		return
	}
	if platform == service.PlatformGrok {
		h.openAI.GrokImages(c)
		return
	}
	h.openAI.Images(c)
}

func (h *AsyncImageHandler) run(taskID, platform string, taskCtx *gin.Context, recorder *httptest.ResponseRecorder, cancel context.CancelFunc) {
	defer cancel()
	defer func() {
		if recovered := recover(); recovered != nil {
			logger.L().Error("image_task.execution_panicked", zap.String("task_id", taskID), zap.Any("panic", recovered))
			h.failTask(taskID, http.StatusInternalServerError, imageTaskErrorPayload("api_error", "image generation task panicked"))
		}
	}()

	h.execute(platform, taskCtx)
	body := bytes.TrimSpace(recorder.Body.Bytes())
	if err := taskCtx.Request.Context().Err(); err != nil && len(body) == 0 {
		h.failTask(taskID, http.StatusGatewayTimeout, imageTaskErrorPayload("timeout_error", "image generation task timed out"))
		return
	}
	statusCode := recorder.Code
	if statusCode == 0 {
		statusCode = http.StatusOK
	}
	if statusCode >= http.StatusOK && statusCode < http.StatusMultipleChoices {
		if len(body) == 0 || !json.Valid(body) {
			h.failTask(taskID, http.StatusBadGateway, imageTaskErrorPayload("api_error", "upstream returned an invalid image response"))
			return
		}
		var accountID int64
		if plan := service.GatewayRequestPlanFromContext(taskCtx.Request.Context()); plan != nil {
			accountID = plan.Snapshot().UpstreamAccountID
		}
		persistCtx, persistCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer persistCancel()
		if err := h.tasks.Complete(persistCtx, taskID, statusCode, json.RawMessage(body), accountID); err != nil {
			logger.L().Error("image_task.complete_store_failed", zap.String("task_id", taskID), zap.Error(err))
		}
		return
	}
	h.failTask(taskID, statusCode, extractImageTaskError(body))
}

func (h *AsyncImageHandler) failTask(taskID string, statusCode int, taskErr json.RawMessage) {
	persistCtx, persistCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer persistCancel()
	if err := h.tasks.Fail(persistCtx, taskID, statusCode, taskErr); err != nil {
		logger.L().Error("image_task.failure_store_failed", zap.String("task_id", taskID), zap.Error(err))
	}
}

func newAsyncImageContext(c *gin.Context, body []byte, executionCtx context.Context) (*gin.Context, *httptest.ResponseRecorder) {
	request := c.Request.Clone(executionCtx)
	request.Body = io.NopCloser(bytes.NewReader(body))
	request.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(body)), nil
	}
	request.ContentLength = int64(len(body))
	request.URL.Path = strings.TrimSuffix(request.URL.Path, "/async")

	taskCtx := c.Copy()
	recorder := httptest.NewRecorder()
	recorderCtx, _ := gin.CreateTestContext(recorder)
	taskCtx.Writer = recorderCtx.Writer
	taskCtx.Request = request
	return taskCtx, recorder
}

func asyncImageRequestStreams(contentType string, body []byte) bool {
	if isMultipartImagesContentType(contentType) {
		return false
	}
	var envelope struct {
		Stream bool `json:"stream"`
	}
	return json.Unmarshal(body, &envelope) == nil && envelope.Stream
}

func imageTaskPollURL(submitPath, taskID string) string {
	if strings.HasPrefix(submitPath, "/v1/") {
		return "/v1/images/tasks/" + taskID
	}
	return "/images/tasks/" + taskID
}

func extractImageTaskError(body []byte) json.RawMessage {
	if json.Valid(body) {
		var envelope struct {
			Error json.RawMessage `json:"error"`
		}
		if json.Unmarshal(body, &envelope) == nil && len(envelope.Error) > 0 && json.Valid(envelope.Error) {
			return envelope.Error
		}
		return json.RawMessage(body)
	}
	return imageTaskErrorPayload("api_error", "image generation failed")
}

func imageTaskErrorPayload(errorType, message string) json.RawMessage {
	data, _ := json.Marshal(gin.H{"type": errorType, "message": message})
	return data
}

func imageTaskError(c *gin.Context, err error) {
	status := infraerrors.Code(err)
	code := infraerrors.Reason(err)
	message := infraerrors.Message(err)
	if status <= 0 {
		status = http.StatusInternalServerError
	}
	if strings.TrimSpace(code) == "" {
		code = "IMAGE_TASK_ERROR"
	}
	imageTaskJSONError(c, status, code, message)
}

func imageTaskJSONError(c *gin.Context, status int, code, message string) {
	c.Header("Cache-Control", "no-store")
	c.JSON(status, gin.H{"error": gin.H{"type": code, "code": code, "message": message}})
}

func imageTaskOwner(apiKey *service.APIKey) service.ImageTaskOwner {
	workspaceID := apiKey.WorkspaceID
	if workspaceID <= 0 {
		workspaceID = domain.DefaultWorkspaceID
	}
	return service.ImageTaskOwner{UserID: apiKey.UserID, APIKeyID: apiKey.ID, WorkspaceID: workspaceID}
}
