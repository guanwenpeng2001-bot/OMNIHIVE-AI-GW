package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	pkghttputil "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/httputil"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

// GeminiProtocolBridge adapts Google ingress to the existing bound action
// handlers. Its callbacks must be the action dispatchers, not a guessed vendor
// endpoint. They retain authorization, scheduling, failover and billing ownership.
func GeminiProtocolBridge(messages, countTokens, embeddings gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		model, action, err := parseGeminiModelAction(strings.TrimPrefix(c.Param("modelAction"), "/"))
		if err != nil {
			googleError(c, http.StatusNotFound, err.Error())
			return
		}
		callerModel := clientRequestedModel(c, model)
		if alt := c.Query("alt"); alt != "" && alt != "json" && alt != "sse" {
			googleError(c, 400, "alt must be json or sse")
			return
		}
		body, err := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
		if err != nil {
			if maxErr, ok := extractMaxBytesError(err); ok {
				googleError(c, 413, buildBodyTooLargeMessage(maxErr.Limit))
			} else {
				googleError(c, 400, "Failed to read request body")
			}
			return
		}
		continuationContext, cleanedBody, continuationErr := service.PrepareGoogleContinuation(c.Request.Context(), body, service.GatewayTargetPlatform(c))
		if continuationErr != nil {
			googleError(c, 400, continuationErr.Error())
			return
		}
		c.Request = c.Request.WithContext(continuationContext)
		body = cleanedBody
		// The public URL remains intact for action requirements and audit. The
		// resolved model belongs in the transformed body, never in shared key state.
		if resolved, ok := service.ResolvedUpstreamModelFromContext(c.Request.Context()); ok && resolved != "" {
			model = resolved
		}
		var next gin.HandlerFunc
		var converted []byte
		count := 0
		stream := action == "streamGenerateContent"
		switch action {
		case "generateContent", "streamGenerateContent":
			next = messages
			converted, err = service.GeminiToMessages(body, model, stream)
		case "countTokens":
			next = countTokens
			var request map[string]json.RawMessage
			if err = json.Unmarshal(body, &request); err == nil {
				nested := request["generateContentRequest"]
				if len(nested) == 0 {
					nested = request["generate_content_request"]
				}
				if len(nested) > 0 {
					body = nested
				}
				converted, err = service.GeminiToMessages(body, model, false)
			}
		case "embedContent", "batchEmbedContents":
			next = embeddings
			converted, count, err = service.GeminiToEmbeddings(body, callerModel, action == "batchEmbedContents")
			if err == nil && model != callerModel {
				var request map[string]any
				_ = json.Unmarshal(converted, &request)
				request["model"] = model
				converted, err = json.Marshal(request)
			}
		default:
			googleError(c, 404, "Unsupported Gemini action")
			return
		}
		if err != nil {
			googleError(c, 400, err.Error())
			return
		}
		if next == nil {
			googleError(c, 400, "No bound gateway adapter for this action")
			return
		}
		originalBody, originalLength := c.Request.Body, c.Request.ContentLength
		originalHeader := c.Request.Header
		c.Request.Header = originalHeader.Clone()
		c.Request.Header.Del("Content-Encoding")
		c.Request.Header.Del("Content-Length")
		c.Request.Header.Set("Content-Type", "application/json")
		c.Request.Body = pkghttputil.NewPrereadBody(converted)
		c.Request.ContentLength = int64(len(converted))
		cachedBody, hadCachedBody := c.Get(gin.BodyBytesKey)
		c.Set(gin.BodyBytesKey, converted)
		writer := service.NewGeminiProtocolWriter(c.Writer, callerModel, action, stream, count)
		writer.ConfigureThinking(body)
		c.Request = c.Request.WithContext(service.WithGeminiProtocolWriter(c.Request.Context(), writer))
		if stream && c.Query("alt") != "sse" {
			writer.UseJSONStream()
		}
		c.Writer = writer
		defer func() {
			c.Writer = writer.ResponseWriter
			c.Request.Body = originalBody
			c.Request.ContentLength = originalLength
			c.Request.Header = originalHeader
			if hadCachedBody {
				c.Set(gin.BodyBytesKey, cachedBody)
			} else {
				delete(c.Keys, gin.BodyBytesKey)
			}
		}()
		next(c)
		_ = writer.Finish()
	}
}
