// Package gatewayerror serializes public API errors independently of provider routing.
package gatewayerror

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/googleapi"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/util/logredact"
)

func Protocol(request *http.Request) string {
	if request == nil {
		return "openai"
	}
	path := request.URL.Path
	if strings.HasPrefix(path, "/v1beta/") || strings.HasPrefix(path, "/antigravity/v1beta/") {
		return "google"
	}
	if strings.Contains(path, "/messages") || request.Header.Get("anthropic-version") != "" {
		return "anthropic"
	}
	return "openai"
}

// Envelope retains a stable machine code while mapping types to the caller's protocol.
func Envelope(protocol string, status int, reason, message string) (int, any) {
	message = logredact.RedactDiagnostic(message)
	quota := reason == "INSUFFICIENT_BALANCE" || reason == "API_KEY_QUOTA_EXHAUSTED" || reason == "insufficient_quota"
	if quota {
		status = http.StatusTooManyRequests
	}
	errorType := "invalid_request_error"
	switch {
	case status >= 500:
		errorType = "api_error"
	case status == 401:
		errorType = "authentication_error"
	case status == 403:
		errorType = "permission_error"
	case status == 404:
		errorType = "not_found_error"
	case status == 413:
		errorType = "request_too_large"
	case status == 429:
		errorType = "rate_limit_error"
	}
	if protocol == "google" {
		return status, gin.H{"error": gin.H{"code": status, "status": googleapi.HTTPStatusToGoogleStatus(status), "message": message}}
	}
	if protocol == "anthropic" {
		if quota {
			errorType = "invalid_request_error"
		}
		return status, gin.H{"type": "error", "error": gin.H{"type": errorType, "message": message, "code": reason}}
	}
	if quota {
		errorType, reason = "insufficient_quota", "insufficient_quota"
	}
	if status >= 500 {
		errorType = "server_error"
	}
	if status == http.StatusRequestEntityTooLarge {
		errorType = "invalid_request_error"
	}
	return status, gin.H{"error": gin.H{"type": errorType, "code": reason, "param": nil, "message": message}}
}

func Write(ctx *gin.Context, status int, reason, message string) {
	ctx.Writer.Header().Add("Vary", "anthropic-version")
	status, body := Envelope(Protocol(ctx.Request), status, reason, message)
	if reason == "INSUFFICIENT_BALANCE" || reason == "API_KEY_QUOTA_EXHAUSTED" || reason == "insufficient_quota" {
		ctx.Writer.Header().Del("Retry-After")
	}
	ctx.JSON(status, body)
}

func WriteStream(ctx *gin.Context, status int, reason, message string) {
	_, envelope := Envelope(Protocol(ctx.Request), status, reason, message)
	body, err := json.Marshal(envelope)
	if err != nil {
		return
	}
	if _, err := fmt.Fprintf(ctx.Writer, "event: error\ndata: %s\n\n", body); err != nil {
		_ = ctx.Error(err)
	}
	ctx.Writer.Flush()
}
