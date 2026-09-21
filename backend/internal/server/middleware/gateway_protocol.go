package middleware

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/gatewayerror"
	"github.com/gin-gonic/gin"
)

// GatewayUnknownRouteProtocol keeps SDK paths out of a frontend/management
// fallback even when no registered route (and therefore no group chain) matches.
func GatewayUnknownRouteProtocol() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		path := strings.TrimRight(ctx.Request.URL.Path, "/")
		if ctx.FullPath() == "" {
			for _, prefix := range []string{"/v1", "/v1beta", "/antigravity/v1", "/antigravity/v1beta", "/backend-api/codex", "/models", "/messages", "/responses", "/chat/completions"} {
				if path == prefix || strings.HasPrefix(path, prefix+"/") {
					ctx.Header("Cache-Control", "private, no-store")
					gatewayerror.Write(ctx, http.StatusNotFound, "not_found_error", "Endpoint not found")
					ctx.Abort()
					return
				}
			}
		}
		ctx.Next()
	}
}

// GatewayProtocol is installed before authentication and admission. Successful
// bodies and streaming transport stay untouched; errors have one public envelope.
func GatewayProtocol() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		// DashScope native resources retain their separately documented envelope.
		path := ctx.Request.URL.Path
		if strings.HasPrefix(path, "/api/v1/") || strings.HasPrefix(path, "/api-ws/v1/") || strings.HasPrefix(path, "/compatible-mode/v1/") {
			ctx.Next()
			return
		}
		ctx.Set("gateway_protocol_errors", true)
		ctx.Header("Cache-Control", "private, no-store")
		ctx.Writer.Header().Add("Vary", "Authorization")
		ctx.Writer.Header().Add("Vary", "X-API-Key")
		ctx.Writer.Header().Add("Vary", "X-Goog-Api-Key")
		ctx.Writer.Header().Add("Vary", "anthropic-version")
		writer := &gatewayProtocolWriter{ResponseWriter: ctx.Writer, protocol: gatewayerror.Protocol(ctx.Request)}
		ctx.Writer = writer
		ctx.Next()
		writer.finish()
	}
}

type gatewayProtocolWriter struct {
	gin.ResponseWriter
	protocol string
	body     bytes.Buffer
	finished bool
}

func (w *gatewayProtocolWriter) Write(body []byte) (int, error) {
	if w.Status() >= 400 && !w.ResponseWriter.Written() && !w.finished {
		return w.body.Write(body)
	}
	return w.ResponseWriter.Write(body)
}
func (w *gatewayProtocolWriter) WriteString(body string) (int, error) { return w.Write([]byte(body)) }
func (w *gatewayProtocolWriter) Written() bool                        { return w.body.Len() > 0 || w.ResponseWriter.Written() }
func (w *gatewayProtocolWriter) Size() int {
	if w.body.Len() > 0 {
		return w.body.Len()
	}
	return w.ResponseWriter.Size()
}
func (w *gatewayProtocolWriter) Flush() { w.finish(); w.ResponseWriter.Flush() }
func (w *gatewayProtocolWriter) finish() {
	if w.finished || w.body.Len() == 0 {
		return
	}
	w.finished = true
	reason, message := "request_failed", http.StatusText(w.Status())
	var envelope map[string]any
	if json.Unmarshal(w.body.Bytes(), &envelope) == nil {
		// Transitional readers accept management reason/code and legacy error strings.
		if value, ok := envelope["message"].(string); ok {
			message = value
		}
		if value, ok := envelope["reason"].(string); ok {
			reason = value
		} else if value, ok := envelope["code"].(string); ok {
			reason = value
		}
		switch failure := envelope["error"].(type) {
		case string:
			message = failure
		case map[string]any:
			if value, ok := failure["message"].(string); ok {
				message = value
			}
			if value, ok := failure["code"].(string); ok && value != "" {
				reason = value
			} else if value, ok := failure["type"].(string); ok {
				reason = value
			}
		}
	}
	// Legacy balance writers used permission status; retain the distinction.
	if strings.Contains(strings.ToLower(message), "insufficient account balance") {
		reason = "insufficient_quota"
	}
	status, body := gatewayerror.Envelope(w.protocol, w.Status(), reason, message)
	if reason == "insufficient_quota" || reason == "INSUFFICIENT_BALANCE" || reason == "API_KEY_QUOTA_EXHAUSTED" {
		w.Header().Del("Retry-After")
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		encoded = []byte(fmt.Sprintf(`{"error":{"message":%q}}`, http.StatusText(status)))
	}
	w.Header().Del("Content-Length")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.ResponseWriter.WriteHeader(status)
	_, _ = w.ResponseWriter.Write(encoded)
}
