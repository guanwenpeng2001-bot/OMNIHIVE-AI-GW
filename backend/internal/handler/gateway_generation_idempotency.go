package handler

import (
	"bytes"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	infraerrors "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/errors"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/logger"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

// GatewayGenerationIdempotency runs after authentication and model authorization,
// before composite rewriting, request plans, leases and dispatch. resolveScope
// must use trusted ownership (never a request header); an empty Action skips
// non-generation routes. Actions must identify the protocol and canonical route,
// including model/resource path parameters and semantic query options, but NOT
// query credentials. APIKeyID is the persistent key ID, not its secret.
func GatewayGenerationIdempotency(resolveScope func(*gin.Context) (service.GatewayGenerationScope, error)) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetHeader("Idempotency-Key") == "" {
			c.Next()
			return
		}
		if resolveScope == nil {
			writeGenerationIdempotencyError(c, service.ErrGatewayGenerationScope)
			return
		}
		scope, err := resolveScope(c)
		if err != nil {
			writeGenerationIdempotencyError(c, err)
			return
		}
		if scope.Action == "" {
			c.Next()
			return
		}
		// RequestBodyLimit must precede this middleware; preserve the exact body
		// for the existing ingress parser, including multipart/binary payloads.
		var body []byte
		if c.Request.Body != nil {
			body, err = io.ReadAll(c.Request.Body)
		}
		if err != nil {
			status := http.StatusBadRequest
			if _, tooLarge := extractMaxBytesError(err); tooLarge {
				status = http.StatusRequestEntityTooLarge
			}
			writeGenerationIdempotencyError(c, infraerrors.New(status, "IDEMPOTENCY_BODY_INVALID", "could not read request body"))
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(body))
		claim, err := service.DefaultIdempotencyCoordinator().BeginGatewayGeneration(c.Request.Context(), scope, c.GetHeader("Idempotency-Key"), body)
		if err != nil {
			writeGenerationIdempotencyError(c, err)
			return
		}
		if claim.Replay != nil {
			stored := claim.Replay
			c.Header("X-Idempotency-Replayed", "true")
			writeGenerationReceiptHeaders(c, stored)
			if stored.State != "completed" {
				c.Header("X-Idempotency-State", stored.State)
				writeGenerationIdempotencyError(c, infraerrors.Conflict("IDEMPOTENCY_"+strings.ToUpper(stored.State), "original execution cannot be replayed; automatic regeneration is disabled"))
				return
			}
			c.Data(stored.Status, stored.ContentType, stored.Body)
			c.Abort()
			return
		}
		originalWriter := c.Writer
		capture := &generationResponseCapture{ResponseWriter: originalWriter}
		c.Writer = capture
		finished := false
		defer func() {
			c.Writer = originalWriter
			response := service.GatewayGenerationResponse{Status: capture.Status(), ContentType: capture.Header().Get("Content-Type"), Body: capture.body.Bytes(), State: "completed"}
			if capture.streaming || strings.Contains(response.ContentType, "text/event-stream") {
				response.State = "stream_not_replayable"
			}
			if capture.overflow {
				response.State = "response_unavailable"
			}
			if !finished || capture.writeFailed || c.Request.Context().Err() != nil {
				response.State = "interrupted"
			}
			if plan := service.GatewayRequestPlanFromContext(c.Request.Context()); plan != nil {
				state, spec := plan.State(), plan.Snapshot()
				response.ReceiptKey, response.ReceiptState = state.ReceiptKey, state.ReceiptState
				// Async/job settlement may use a distinct durable receipt key.
				// Keep the execution reference without inventing a receipt.
				response.ExecutionID = spec.ExecutionID
			}
			if err := claim.Complete(c.Request.Context(), response); err != nil {
				logger.LegacyPrintf("handler.idempotency", "generation result persistence failed; durable claim retained: %v", err)
			}
		}()
		c.Next()
		finished = true
	}
}

func writeGenerationReceiptHeaders(c *gin.Context, response *service.GatewayGenerationResponse) {
	if response.ExecutionID != "" {
		c.Header("X-Idempotency-Original-Request-ID", response.ExecutionID)
	}
	if response.ReceiptKey.RequestID != "" {
		c.Header("X-Idempotency-Receipt-ID", response.ReceiptKey.RequestID)
	}
	if response.ReceiptState != "" {
		c.Header("X-Idempotency-Receipt-State", string(response.ReceiptState))
	}
}

// Error code is stable across protocol envelopes; HTTP 409 represents body
// conflict, inflight and terminal non-replayable outcomes, never a new dispatch.
func writeGenerationIdempotencyError(c *gin.Context, err error) {
	status, reason, message := infraerrors.Code(err), infraerrors.Reason(err), infraerrors.Message(err)
	errorBody := gin.H{"type": "invalid_request_error", "code": reason, "message": message}
	if status >= 500 {
		errorBody["type"] = "api_error"
	}
	if strings.Contains(c.Request.URL.Path, "/v1beta/") {
		googleStatus := "FAILED_PRECONDITION"
		if status >= 500 {
			googleStatus = "UNAVAILABLE"
		} else if status == 400 {
			googleStatus = "INVALID_ARGUMENT"
		} else if status == 413 {
			googleStatus = "RESOURCE_EXHAUSTED"
		}
		c.AbortWithStatusJSON(status, gin.H{"error": gin.H{"code": status, "status": googleStatus, "message": message, "details": []gin.H{{"@type": "type.googleapis.com/google.rpc.ErrorInfo", "reason": reason, "domain": "sub2api"}}}})
		return
	}
	if c.GetHeader("anthropic-version") != "" || strings.HasSuffix(c.Request.URL.Path, "/messages") {
		c.AbortWithStatusJSON(status, gin.H{"type": "error", "error": errorBody})
		return
	}
	c.AbortWithStatusJSON(status, gin.H{"error": errorBody})
}

type generationResponseCapture struct {
	gin.ResponseWriter
	body                             bytes.Buffer
	streaming, overflow, writeFailed bool
}

func (writer *generationResponseCapture) Write(data []byte) (int, error) {
	if strings.Contains(writer.Header().Get("Content-Type"), "text/event-stream") {
		writer.streaming = true
	}
	written, err := writer.ResponseWriter.Write(data)
	if err != nil {
		writer.writeFailed = true
	}
	if !writer.streaming && !writer.overflow {
		if writer.body.Len()+written > service.GatewayGenerationMaxResponseBytes {
			writer.overflow = true
			writer.body.Reset()
		} else {
			_, _ = writer.body.Write(data[:written])
		}
	}
	return written, err
}

func (writer *generationResponseCapture) WriteString(data string) (int, error) {
	return writer.Write([]byte(data))
}
func (writer *generationResponseCapture) Flush() {
	writer.streaming = true
	writer.body.Reset()
	writer.ResponseWriter.Flush()
}
