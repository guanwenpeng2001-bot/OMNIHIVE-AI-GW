package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

func (h *OpenAIGatewayHandler) handleQianwenFileBatch(c *gin.Context) bool {
	path := c.Request.URL.Path
	family, root := "files", "/compatible-mode/v1/files"
	if path != "" && (path == "/compatible-mode/v1/batches" || strings.HasPrefix(path, "/compatible-mode/v1/batches/")) {
		family, root = "batch", "/compatible-mode/v1/batches"
	}
	if path != root && !strings.HasPrefix(path, root+"/") {
		return false
	}
	// The original structured gate remains the fallback on unavailable services.
	if h == nil || h.upstreamJobs == nil {
		return false
	}
	method := c.Request.Method
	suffix := strings.TrimPrefix(path, root)
	manage := (method == http.MethodDelete && family == "files" && suffix != "") || (method == http.MethodPost && family == "batch" && strings.HasSuffix(suffix, "/cancel"))
	if method != http.MethodGet && (method != http.MethodPost || suffix != "") && !manage {
		return false
	}
	key, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok || key == nil || key.Group == nil || key.GroupID == nil {
		fileBatchError(c, 401, "authentication_error", "Invalid API key")
		return true
	}
	if openAICompatibleRequestPlatform(c.Request.Context(), key) != service.PlatformQianwen {
		fileBatchError(c, 404, "unsupported_operation_error", "File batches require a Qianwen group")
		return true
	}
	if manage {
		return h.manageQianwenFileBatch(c, key, family, suffix)
	}
	if method == http.MethodGet && suffix == "" {
		limit := 20
		if raw := c.Query("limit"); raw != "" {
			var err error
			limit, err = strconv.Atoi(raw)
			if err != nil || limit < 1 || limit > 100 {
				fileBatchError(c, 400, "invalid_request_error", "limit must be 1-100")
				return true
			}
		}
		listFamily := family
		if family == "files" {
			switch c.Query("purpose") {
			case "":
			case "batch":
				listFamily = "batch_input"
			case "batch_output":
				listFamily = "batch_output"
			default:
				fileBatchError(c, 400, "invalid_request_error", "Unsupported file purpose")
				return true
			}
		}
		jobs, err := h.upstreamJobs.ListResources(c.Request.Context(), key.UserID, key.ID, *key.GroupID, listFamily, c.Query("after"), limit)
		if err != nil {
			fileBatchOperationError(c, err)
			return true
		}
		more := len(jobs) > limit
		if more {
			jobs = jobs[:limit]
		}
		data := make([]gin.H, 0, len(jobs))
		for _, j := range jobs {
			if family == "files" && j.Family == "batch" {
				data = append(data, outputFileObject(j))
			} else {
				data = append(data, fileBatchObject(j))
			}
		}
		out := gin.H{"object": "list", "data": data, "has_more": more, "first_id": nil, "last_id": nil}
		if len(jobs) > 0 {
			out["first_id"] = data[0]["id"]
			out["last_id"] = data[len(data)-1]["id"]
		}
		c.JSON(200, out)
		return true
	}
	if method == http.MethodGet {
		parts := strings.Split(strings.TrimPrefix(suffix, "/"), "/")
		content := len(parts) == 2 && parts[1] == "content" && family == "files"
		if len(parts) > 1 && !content {
			return false
		}
		id := parts[0]
		expectedFamily := family
		if family == "files" && strings.HasPrefix(id, "output-") {
			id = strings.TrimPrefix(id, "output-")
			expectedFamily = "batch"
		}
		j, err := h.upstreamJobs.Get(c.Request.Context(), id, key.UserID, key.ID)
		if errors.Is(err, service.ErrUpstreamJobNotFound) || err == nil && (!ownedFileBatch(j, key, expectedFamily) || j.ResourceState.Deleted || (family == "files" && expectedFamily == "batch" && j.ResourceState.OutputDeleted)) {
			fileBatchError(c, 404, "not_found_error", "Resource not found")
			return true
		}
		if err != nil {
			fileBatchError(c, 503, "resource_store_unavailable", "Resource store unavailable")
			return true
		}
		if content {
			if j.Status != "succeeded" || j.ResultRef == "" {
				fileBatchError(c, 409, "resource_not_ready", "Resource content is not ready")
				return true
			}
			raw, err := h.gatewayService.QianwenResourceContent(c.Request.Context(), j)
			if err != nil {
				fileBatchError(c, 502, "resource_content_unavailable", "Resource content unavailable or integrity validation failed")
				return true
			}
			c.Data(200, "application/jsonl", raw)
			return true
		}
		if family == "files" && expectedFamily == "batch" {
			if j.Status != "succeeded" {
				fileBatchError(c, 409, "resource_not_ready", "Output file is not ready")
				return true
			}
			c.JSON(200, outputFileObject(j))
		} else {
			writeFileBatch(c, j, 200)
		}
		return true
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		fileBatchError(c, 401, "authentication_error", "Invalid user context")
		return true
	}
	var normalized, fingerprint []byte
	var snapshot *service.QianwenFileBatchSnapshot
	var inputFile *service.UpstreamJob
	var err error
	if family == "files" {
		// MultipartReader streams in memory with a hard limit; no temporary disk files.
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, service.QianwenFileLimit+16384)
		reader, readErr := c.Request.MultipartReader()
		if readErr != nil {
			fileBatchError(c, 400, "invalid_request_error", "Expected multipart file and purpose=batch")
			return true
		}
		purpose := ""
		seen := map[string]bool{}
		for {
			part, e := reader.NextPart()
			if e == io.EOF {
				break
			}
			if e != nil {
				err = e
				break
			}
			name := part.FormName()
			if seen[name] || (name != "file" && name != "purpose") {
				err = errors.New("only one file and purpose field are supported")
				_ = part.Close()
				break
			}
			seen[name] = true
			data, e := io.ReadAll(io.LimitReader(part, service.QianwenFileLimit+1))
			_ = part.Close()
			if e != nil || len(data) > service.QianwenFileLimit {
				err = errors.New("file exceeds 1 MiB")
				break
			}
			if name == "purpose" {
				purpose = string(data)
			} else {
				normalized = data
			}
		}
		if err != nil || purpose != "batch" || len(normalized) == 0 {
			fileBatchError(c, 400, "invalid_request_error", "Expected one batch file up to 1 MiB and purpose=batch")
			return true
		}
		normalized, snapshot, err = service.ValidateQianwenBatchFile(normalized)
		if err != nil {
			fileBatchError(c, 422, "unsupported_operation_error", err.Error())
			return true
		}
		fingerprint, _ = json.Marshal(snapshot)
	} else {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16384)
		body, e := io.ReadAll(c.Request.Body)
		if e != nil || !json.Valid(body) {
			fileBatchError(c, 400, "invalid_request_error", "Invalid batch JSON")
			return true
		}
		body, err = service.CanonicalUpstreamJobBody(body)
		if err != nil {
			fileBatchError(c, 400, "invalid_request_error", "Invalid batch JSON")
			return true
		}
		parsed := gjson.ParseBytes(body)
		unknown := false
		parsed.ForEach(func(k, v gjson.Result) bool {
			if k.String() != "input_file_id" && k.String() != "endpoint" && k.String() != "completion_window" {
				unknown = true
			}
			return true
		})
		if unknown || (parsed.Get("endpoint").String() != "/v1/chat/ds-test" && parsed.Get("endpoint").String() != "/v1/chat/completions") || parsed.Get("completion_window").String() != "24h" {
			fileBatchError(c, 422, "unsupported_operation_error", "Only owned JSONL files, supported chat endpoints and 24h are supported")
			return true
		}
		inputFile, err = h.upstreamJobs.Get(c.Request.Context(), parsed.Get("input_file_id").String(), key.UserID, key.ID)
		if errors.Is(err, service.ErrUpstreamJobNotFound) || err == nil && !ownedFileBatch(inputFile, key, "files") {
			fileBatchError(c, 404, "not_found_error", "Input file not found")
			return true
		}
		if err != nil {
			fileBatchError(c, 503, "resource_store_unavailable", "Resource store unavailable")
			return true
		}
		if inputFile.Status != "succeeded" || inputFile.ResourceState.Deleted || inputFile.Billing.FileBatch == nil {
			fileBatchError(c, 409, "resource_not_ready", "Input file is not ready")
			return true
		}
		if parsed.Get("endpoint").String() != inputFile.Billing.FileBatch.BatchEndpoint() {
			fileBatchError(c, 422, "invalid_request_error", "Endpoint must match the uploaded file")
			return true
		}
		copySnapshot := *inputFile.Billing.FileBatch
		copySnapshot.InputFileID = inputFile.PublicRequestID
		snapshot = &copySnapshot
		fingerprint = body
	}
	reqLog := requestLogger(c, "handler.qianwen_file_batch")
	// Free test requests still follow moderation and concurrency admission.
	if family == "files" {
		for _, line := range strings.Split(strings.TrimSpace(string(normalized)), "\n") {
			audit := []byte(gjson.Get(line, "body").Raw)
			if decision := h.checkSecurityAudit(c, reqLog, key, subject, service.ContentModerationProtocolOpenAIChat, snapshot.BatchModel(), audit); decision != nil && !decision.AllowNextStage {
				h.openAISecurityAuditError(c, decision)
				return true
			}
			if h.rejectIfCyberSessionBlocked(c, key, audit, snapshot.BatchModel(), cyberBlockFormatChat) {
				return true
			}
		}
	}
	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	if err = h.billingCacheService.CheckBillingEligibility(c.Request.Context(), key.User, key, key.Group, subscription, service.QuotaPlatform(c.Request.Context(), key)); err != nil {
		status, code, message, _ := billingErrorDetails(err)
		fileBatchError(c, status, code, message)
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
	sessionHash := h.gatewayService.GenerateSessionHash(c, fingerprint)
	var account *service.Account
	var accountRelease func()
	if inputFile != nil {
		account, err = h.gatewayService.QianwenResourceAccount(c.Request.Context(), inputFile, true)
		if err != nil {
			fileBatchError(c, 503, "resource_account_unavailable", "Input file account unavailable")
			return true
		}
		var acquired bool
		accountRelease, acquired, err = h.concurrencyHelper.TryAcquireAccountSlot(c.Request.Context(), account.ID, account.Concurrency)
		if err != nil || !acquired {
			fileBatchError(c, 429, "resource_account_busy", "Input file account is busy")
			return true
		}
	} else {
		// File storage has no inference model; do not remap the validated test model.
		selection, _, e := h.gatewayService.SelectAccountWithSchedulerForCapability(c.Request.Context(), key.GroupID, "", sessionHash, "", nil, service.OpenAIUpstreamTransportAny, service.OpenAIEndpointCapabilityChatCompletions, false, false, true, service.PlatformQianwen)
		if e != nil || selection == nil || selection.Account == nil {
			fileBatchError(c, 503, "resource_account_unavailable", "No file account available")
			return true
		}
		var slot openAISlotAcquireResult
		accountRelease, slot = h.acquireResponsesAccountSlot(c, key.GroupID, sessionHash, selection, false, &streamStarted, reqLog)
		if slot != openAISlotAcquireOK {
			return true
		}
		account = selection.Account
	}
	if accountRelease != nil {
		defer accountRelease()
	}
	billing, err := service.QianwenFileBatchBillingSnapshot(key.Group, account, key, subscription, snapshot)
	if err != nil {
		fileBatchError(c, 422, "unsupported_pricing", err.Error())
		return true
	}
	if snapshot.BatchModel() != service.QianwenBatchTestModel && (!account.IsModelSupported(snapshot.BatchModel()) || account.GetMappedModel(snapshot.BatchModel()) != snapshot.BatchModel()) {
		fileBatchError(c, 422, "unsupported_model", "Batch requires an exact supported native model")
		return true
	}
	submit := func() (*service.UpstreamJob, error) {
		return h.upstreamJobs.Submit(c.Request.Context(), &service.UpstreamJob{Platform: service.PlatformQianwen, Family: family, AccountID: account.ID, UserID: key.UserID, APIKeyID: key.ID, GroupID: key.GroupID, Billing: billing}, fingerprint, func(ctx context.Context) (service.UpstreamJobUpdate, error) {
			if family == "files" {
				return h.gatewayService.SubmitQianwenFile(ctx, account, normalized)
			}
			return h.gatewayService.SubmitQianwenBatch(ctx, account, inputFile)
		})
	}
	var job *service.UpstreamJob
	if inputFile != nil {
		job, err = h.upstreamJobs.WithInputFile(c.Request.Context(), inputFile.PublicRequestID, key.UserID, key.ID, *key.GroupID, submit)
	} else {
		job, err = submit()
	}
	if err != nil {
		fileBatchError(c, 503, "resource_store_unavailable", "Unable to persist resource job")
		return true
	}
	setOpsSelectedAccount(c, job.AccountID, service.PlatformQianwen)
	writeFileBatch(c, job, 202)
	return true
}
func ownedFileBatch(j *service.UpstreamJob, key *service.APIKey, family string) bool {
	return j != nil && j.Platform == service.PlatformQianwen && j.Family == family && j.GroupID != nil && key.GroupID != nil && *j.GroupID == *key.GroupID
}
func fileBatchError(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, gin.H{"error": gin.H{"type": "invalid_request_error", "code": code, "message": message}})
}
func outputFileObject(j *service.UpstreamJob) gin.H {
	return gin.H{"id": "output-" + j.PublicRequestID, "object": "file", "purpose": "batch_output", "status": "processed", "filename": "batch-output.jsonl", "created_at": j.SubmittedAt.Unix()}
}
func writeFileBatch(c *gin.Context, j *service.UpstreamJob, status int) {
	c.JSON(status, fileBatchObject(j))
}
func fileBatchObject(j *service.UpstreamJob) gin.H {
	state := j.Status
	if j.Family == "files" && state == "succeeded" {
		state = "processed"
	}
	if j.Family == "batch" {
		switch state {
		case "succeeded":
			state = "completed"
		case "submitted":
			state = "validating"
		case "running":
			state = "in_progress"
		}
		if j.ResourceState.ProviderStatus != "" && j.ErrorCode == "" {
			state = j.ResourceState.ProviderStatus
		}
	}
	out := gin.H{"id": j.PublicRequestID, "object": "batch", "status": state, "created_at": j.SubmittedAt.Unix()}
	if j.Family == "files" {
		out["object"] = "file"
		out["purpose"] = "batch"
		out["filename"] = "batch-input.jsonl"
		if j.Billing.FileBatch != nil {
			out["bytes"] = j.Billing.FileBatch.Bytes
		}
	} else {
		out["endpoint"] = j.Billing.FileBatch.BatchEndpoint()
		out["completion_window"] = "24h"
		out["output_file_id"] = nil
		out["error_file_id"] = nil
		if j.Billing.FileBatch != nil {
			out["input_file_id"] = j.Billing.FileBatch.InputFileID
		}
		if j.Status == "succeeded" && j.ResultRef != "" && !j.ResourceState.OutputDeleted {
			out["output_file_id"] = "output-" + j.PublicRequestID
		}
	}
	if j.ErrorCode != "" {
		out["error"] = gin.H{"code": j.ErrorCode, "message": j.ErrorMessage}
	}
	return out
}

func fileBatchOperationError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrUpstreamJobNotFound):
		fileBatchError(c, 404, "not_found_error", "Resource not found")
	case errors.Is(err, service.ErrUpstreamResourceConflict):
		fileBatchError(c, 409, "resource_conflict", "Resource is not ready or is in use")
	default:
		fileBatchError(c, 503, "resource_operation_unavailable", "Resource operation unavailable")
	}
}
func (h *OpenAIGatewayHandler) manageQianwenFileBatch(c *gin.Context, key *service.APIKey, family, suffix string) bool {
	id, action := strings.TrimPrefix(suffix, "/"), "delete"
	if family == "batch" {
		id, action = strings.TrimSuffix(id, "/cancel"), "cancel"
	}
	publicID := id
	if action == "delete" && strings.HasPrefix(id, "output-") {
		id, action = strings.TrimPrefix(id, "output-"), "delete_output"
	}
	if id == "" || strings.Contains(id, "/") {
		fileBatchError(c, 404, "not_found_error", "Resource not found")
		return true
	}
	j, err := h.upstreamJobs.ManageResource(c.Request.Context(), id, key.UserID, key.ID, *key.GroupID, action, h.gatewayService.ManageQianwenResource)
	if err != nil {
		fileBatchOperationError(c, err)
		return true
	}
	if action == "cancel" {
		writeFileBatch(c, j, 200)
	} else {
		c.JSON(200, gin.H{"id": publicID, "object": "file", "deleted": true})
	}
	return true
}
