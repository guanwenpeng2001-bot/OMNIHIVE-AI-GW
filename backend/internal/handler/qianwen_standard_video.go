package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *OpenAIGatewayHandler) QianwenVideoGeneration(c *gin.Context) {
	key, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok || key == nil || key.Group == nil {
		h.errorResponse(c, 401, "authentication_error", "Invalid API key")
		return
	}
	if h.upstreamJobs == nil {
		h.errorResponse(c, 503, "unsupported_operation_error", "Persistent async jobs unavailable")
		return
	}
	if openAICompatibleRequestPlatform(c.Request.Context(), key) != service.PlatformQianwen {
		h.errorResponse(c, 404, "unsupported_operation_error", "Video submission requires a Qianwen group")
		return
	}
	var body []byte
	var err error
	mediaType, parameters, _ := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if mediaType == "multipart/form-data" {
		limit := gatewayMaxBodySize(h.cfg)
		if limit <= 0 {
			limit = 1 << 20
		}
		body, err = io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, limit))
		if err == nil {
			body, err = qianwenVideoFormBody(body, parameters["boundary"])
		}
	} else {
		body, err = readLenientJSONRequestBodyWithPrealloc(c.Request, h.cfg)
	}
	if _, oversized := extractMaxBytesError(err); oversized {
		h.errorResponse(c, http.StatusRequestEntityTooLarge, "invalid_request_error", "Video request body is too large")
		return
	}
	if err == nil {
		body, err = service.QianwenVideoCompatBody(body)
	}
	if err != nil {
		h.errorResponse(c, 400, "invalid_request_error", "Invalid video request: "+err.Error())
		return
	}
	h.submitQianwenAsyncJob(c, key, "video", body, writeQianwenStandardVideo)
}

// Parse an already bounded body without creating temporary upload files.
func qianwenVideoFormBody(body []byte, boundary string) ([]byte, error) {
	reader := multipart.NewReader(bytes.NewReader(body), boundary)
	fields := map[string]string{}
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, errors.New("invalid multipart body")
		}
		name := part.FormName()
		if part.FileName() != "" || name == "input_reference" {
			return nil, errors.New("image references are not supported by this text-to-video adapter")
		}
		if _, exists := fields[name]; exists {
			return nil, errors.New("duplicate video field")
		}
		value, err := io.ReadAll(part)
		if err != nil {
			return nil, errors.New("invalid multipart field")
		}
		fields[name] = string(value)
	}
	return json.Marshal(fields)
}

func (h *OpenAIGatewayHandler) qianwenStandardVideoJob(c *gin.Context) *service.UpstreamJob {
	key, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok || key == nil {
		h.errorResponse(c, 401, "authentication_error", "Invalid API key")
		return nil
	}
	if h.upstreamJobs == nil {
		h.errorResponse(c, 503, "api_error", "Persistent async jobs unavailable")
		return nil
	}
	job, err := h.upstreamJobs.Get(c.Request.Context(), c.Param("request_id"), key.UserID, key.ID)
	if errors.Is(err, service.ErrUpstreamJobNotFound) || err == nil && (job == nil || job.Platform != service.PlatformQianwen || job.Family != "video") {
		h.errorResponse(c, 404, "not_found_error", "Video not found")
		return nil
	}
	if err != nil {
		h.errorResponse(c, 503, "api_error", "Task store unavailable")
		return nil
	}
	return job
}

func (h *OpenAIGatewayHandler) QianwenVideoStatus(c *gin.Context) {
	if job := h.qianwenStandardVideoJob(c); job != nil {
		writeQianwenStandardVideo(c, job, http.StatusOK)
	}
}

func (h *OpenAIGatewayHandler) QianwenVideoContent(c *gin.Context) {
	if job := h.qianwenStandardVideoJob(c); job != nil {
		h.writeQianwenVideoContent(c, job)
	}
}

func (h *OpenAIGatewayHandler) QianwenVideoCancel(c *gin.Context) {
	job := h.qianwenStandardVideoJob(c)
	if job == nil {
		return
	}
	if h.gatewayService == nil {
		h.errorResponse(c, 503, "api_error", "Video gateway unavailable")
		return
	}
	status, err := h.gatewayService.CancelQianwenVideo(c.Request.Context(), job)
	if err != nil {
		code := "upstream_error"
		if errors.Is(err, service.ErrQianwenVideoNotCancelable) {
			code = "video_not_cancelable"
		}
		h.errorResponse(c, status, code, err.Error())
		return
	}
	response := qianwenStandardVideoResponse(job)
	response["cancel_scope"] = "upstream"
	response["cancellation_acknowledged"] = true
	response["message"] = "Upstream cancellation acknowledged; status remains the last observed state until durable polling confirms the outcome."
	c.JSON(http.StatusOK, response)
}

func qianwenStandardVideoResponse(job *service.UpstreamJob) gin.H {
	response := gin.H{"id": job.PublicRequestID, "object": "video", "status": service.QianwenVideoCompatStatus(job.Status), "model": job.Billing.RequestedModel, "created_at": job.SubmittedAt.Unix()}
	if job.FinishedAt != nil {
		response["completed_at"] = job.FinishedAt.Unix()
	}
	if job.ErrorCode != "" {
		response["error"] = gin.H{"code": job.ErrorCode, "message": job.ErrorMessage}
	}
	return response
}

func writeQianwenStandardVideo(c *gin.Context, job *service.UpstreamJob, status int) {
	c.JSON(status, qianwenStandardVideoResponse(job))
}
