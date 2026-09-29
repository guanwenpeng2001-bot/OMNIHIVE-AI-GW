package handler

import (
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// Resource objects follow the provider documented shapes. Upstream identifiers
// never appear: outputs are addressed by the durable public id instead.

func gatewayResourceFileObject(job *service.UpstreamJob) gin.H {
	status := "uploaded"
	switch job.Status {
	case "succeeded":
		status = "processed"
	case "failed", "expired":
		status = "error"
	}
	out := gin.H{"id": job.PublicRequestID, "object": "file", "status": status, "created_at": job.SubmittedAt.Unix()}
	if snapshot := job.Billing.ResourceFile; snapshot != nil {
		out["purpose"] = snapshot.Purpose
		out["filename"] = snapshot.Filename
		out["bytes"] = snapshot.Bytes
	}
	return out
}

func gatewayResourceOutputFileObject(job *service.UpstreamJob) gin.H {
	return gin.H{
		"id": service.GatewayResourceOutputPrefix + job.PublicRequestID, "object": "file", "purpose": "batch_output",
		"status": "processed", "filename": "batch-output.jsonl", "created_at": job.SubmittedAt.Unix(),
	}
}

func gatewayResourceBatchStatus(job *service.UpstreamJob) string {
	// The provider word is authoritative while it agrees with the durable state;
	// a partially settled cancellation must keep reporting the cancellation.
	switch job.ResourceState.ProviderStatus {
	case "cancelling":
		if job.Status == "submitted" || job.Status == "running" {
			return "cancelling"
		}
	case "validating", "in_progress", "finalizing":
		if job.Status == "submitted" || job.Status == "running" {
			return job.ResourceState.ProviderStatus
		}
	case "completed", "cancelled", "failed", "expired":
		if job.Terminal() {
			return job.ResourceState.ProviderStatus
		}
	}
	switch job.Status {
	case "submitted":
		return "validating"
	case "running":
		if job.ResourceState.CancelRequested {
			return "cancelling"
		}
		return "in_progress"
	case "succeeded":
		return "completed"
	case "failed":
		if job.ErrorCode == "upstream_cancelled" {
			return "cancelled"
		}
		return "failed"
	case "expired":
		return "expired"
	default:
		return job.Status
	}
}

func gatewayResourceBatchObject(job *service.UpstreamJob) gin.H {
	status := gatewayResourceBatchStatus(job)
	out := gin.H{
		"id": job.PublicRequestID, "object": "batch", "status": status, "created_at": job.SubmittedAt.Unix(),
		"expires_at": job.ExpiresAt.Unix(), "completion_window": "24h", "errors": nil, "error_file_id": nil, "output_file_id": nil,
	}
	if snapshot := job.Billing.ResourceBatch; snapshot != nil {
		out["endpoint"] = snapshot.Endpoint
		out["input_file_id"] = snapshot.InputFileID
	}
	if job.Status == "succeeded" && job.ResultRef != "" && !job.ResourceState.OutputDeleted {
		out["output_file_id"] = service.GatewayResourceOutputPrefix + job.PublicRequestID
	}
	if counts := service.GatewayResourceBatchRequestCounts(job); counts != nil {
		out["request_counts"] = counts
	}
	if job.FinishedAt != nil {
		switch status {
		case "completed":
			out["completed_at"] = job.FinishedAt.Unix()
		case "failed":
			out["failed_at"] = job.FinishedAt.Unix()
		case "expired":
			out["expired_at"] = job.FinishedAt.Unix()
		case "cancelled":
			out["cancelled_at"] = job.FinishedAt.Unix()
		}
	}
	if job.ErrorCode != "" {
		out["error"] = gin.H{"code": job.ErrorCode, "message": job.ErrorMessage}
	}
	return out
}

func gatewayResourceMessageBatchStatus(job *service.UpstreamJob) string {
	switch job.Status {
	case "submitted", "running":
		if job.ResourceState.CancelRequested {
			return "canceling"
		}
		return "in_progress"
	default:
		return "ended"
	}
}

func gatewayResourceMessageBatchObject(job *service.UpstreamJob) gin.H {
	counts := gin.H{"processing": 0, "succeeded": 0, "errored": 0, "canceled": 0, "expired": 0}
	for name, count := range service.GatewayResourceBatchRequestCounts(job) {
		if _, known := counts[name]; known {
			counts[name] = count
		}
	}
	out := gin.H{
		"id": job.PublicRequestID, "type": "message_batch", "processing_status": gatewayResourceMessageBatchStatus(job),
		"request_counts": counts, "created_at": job.SubmittedAt.UTC().Format(time.RFC3339),
		"expires_at":  job.ExpiresAt.UTC().Format(time.RFC3339),
		"results_url": "/v1/messages/batches/" + job.PublicRequestID + "/results",
		// Both fields are nullable in the documented shape and are reported as
		// unknown instead of being filled with an invented timestamp.
		"archived_at": nil, "cancel_initiated_at": nil,
	}
	if job.Status == "succeeded" || job.Status == "failed" || job.Status == "expired" {
		if job.FinishedAt != nil {
			out["ended_at"] = job.FinishedAt.UTC().Format(time.RFC3339)
		}
	}
	if job.ErrorCode != "" {
		out["error"] = gin.H{"type": job.ErrorCode, "message": job.ErrorMessage}
	}
	return out
}
