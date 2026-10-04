package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
)

func validateGeminiTextReply(reply map[string]any) error {
	if reply["error"] != nil {
		data, _ := json.Marshal(reply)
		return fmt.Errorf("%s", ExtractUpstreamErrorMessage(data))
	}
	if candidates, ok := reply["candidates"].([]any); ok && len(candidates) > 1 {
		return fmt.Errorf("multiple Gemini candidates cannot be represented by this bridge")
	}
	for _, part := range extractGeminiParts(reply) {
		if part["inlineData"] != nil || part["fileData"] != nil || part["executableCode"] != nil || part["codeExecutionResult"] != nil {
			return fmt.Errorf("Gemini returned a content part that this text bridge cannot represent") //nolint:staticcheck // ST1005: preserve the existing externally surfaced provider error text.
		}
		if part["text"] == nil && part["functionCall"] == nil {
			return fmt.Errorf("unsupported Gemini response part")
		}
	}
	return nil
}

// ForwardAsResponses reuses the Gemini Messages transport and converters; only
// the client envelope changes. Forward remains the sole retry/credential owner.
func (s *GeminiMessagesCompatService) ForwardAsResponses(ctx context.Context, c *gin.Context, account *Account, body []byte, callerModel string) (*ForwardResult, error) {
	normalized, _, err := normalizeOpenAIResponsesLegacyIngress(body)
	if err != nil {
		writeResponsesError(c, 400, "invalid_request_error", err.Error())
		return nil, err
	}
	adapted, mapping, err := adaptResponsesClientToolsForAnthropic(normalized)
	if err != nil {
		writeResponsesError(c, 400, "invalid_request_error", err.Error())
		return nil, err
	}
	var request apicompat.ResponsesRequest
	if err = json.Unmarshal(adapted, &request); err != nil {
		writeResponsesError(c, 400, "invalid_request_error", err.Error())
		return nil, err
	}
	for _, tool := range request.Tools {
		if tool.Type != "function" {
			err = fmt.Errorf("Responses tool %q cannot be represented by the Gemini text bridge", tool.Type) //nolint:staticcheck // ST1005: preserve the existing externally surfaced provider error text.
			writeResponsesError(c, 400, "invalid_request_error", err.Error())
			return nil, err
		}
	}
	if err = validateGeminiResponsesInput(adapted); err != nil {
		writeResponsesError(c, 400, "invalid_request_error", err.Error())
		return nil, err
	}
	anthropic, err := responsesToGeminiMessages(&request)
	if err != nil {
		writeResponsesError(c, 400, "invalid_request_error", err.Error())
		return nil, err
	}
	anthropicBody, err := json.Marshal(anthropic)
	if err != nil {
		return nil, err
	}
	if request.Text != nil && len(request.Text.Format) > 0 {
		var format map[string]any
		if err = json.Unmarshal(request.Text.Format, &format); err != nil {
			writeResponsesError(c, 400, "invalid_request_error", err.Error())
			return nil, err
		}
		switch format["type"] {
		case "text", nil:
		case "json_schema", "json_object":
			var mapped map[string]any
			_ = json.Unmarshal(anthropicBody, &mapped)
			output, _ := mapped["output_config"].(map[string]any)
			if output == nil {
				output = make(map[string]any)
			}
			output["format"] = format
			mapped["output_config"] = output
			anthropicBody, _ = json.Marshal(mapped)
		default:
			err = fmt.Errorf("unsupported Responses text format")
			writeResponsesError(c, 400, "invalid_request_error", err.Error())
			return nil, err
		}
	}
	if callerModel == "" {
		callerModel = request.Model
	}
	writer := &geminiResponsesWriter{ResponseWriter: c.Writer, model: callerModel, stream: request.Stream, status: 200, state: apicompat.NewAnthropicEventToResponsesState(), mapping: mapping, restorer: apicompat.NewResponsesClientToolStreamRestorer(mapping)}
	writer.state.Model = callerModel
	c.Writer = writer
	defer func() { c.Writer = writer.ResponseWriter }()
	result, forwardErr := s.Forward(ctx, c, account, anthropicBody)
	// No bytes means the existing handler may switch accounts. Do not invent an
	// error body here, as doing so would commit an otherwise retryable attempt.
	if forwardErr != nil && writer.consumed == 0 {
		return result, forwardErr
	}
	if err = writer.finish(forwardErr); err != nil && forwardErr == nil {
		forwardErr = err
	}
	return result, forwardErr
}

// Validate before the shared converter, whose permissive handling of future
// content types must not turn unsupported media into an empty successful call.
func validateGeminiResponsesInput(body []byte) error {
	var req map[string]any
	_ = json.Unmarshal(body, &req)
	for _, key := range []string{"previous_response_id", "conversation"} {
		if req[key] != nil && req[key] != "" {
			return fmt.Errorf("%s requires upstream Responses state and cannot be represented by Gemini", key)
		}
	}
	checkParts := func(content any, toolResult bool) error {
		if _, ok := content.(string); ok {
			return nil
		}
		if content == nil {
			return nil
		}
		parts, ok := content.([]any)
		if !ok {
			return fmt.Errorf("unsupported Responses content")
		}
		for _, raw := range parts {
			part, ok := raw.(map[string]any)
			if !ok {
				return fmt.Errorf("invalid Responses content part")
			}
			switch part["type"] {
			case "input_text", "output_text", "text":
			case "input_image":
				if toolResult {
					return fmt.Errorf("media tool output cannot be represented by Gemini Messages conversion")
				}
				url, _ := part["image_url"].(string)
				if !strings.HasPrefix(url, "data:image/") {
					return fmt.Errorf("Gemini bridge requires an inline data image") //nolint:staticcheck // ST1005: preserve the existing externally surfaced provider error text.
				}
			default:
				return fmt.Errorf("unsupported Responses content part %v", part["type"])
			}
		}
		return nil
	}
	if input, ok := req["input"].([]any); ok {
		for _, raw := range input {
			item, ok := raw.(map[string]any)
			if !ok {
				return fmt.Errorf("invalid Responses input item")
			}
			switch item["type"] {
			case nil, "message":
				if err := checkParts(item["content"], false); err != nil {
					return err
				}
			case "function_call":
			case "function_call_output":
				if _, ok := item["output"].([]any); ok {
					if err := checkParts(item["output"], true); err != nil {
						return err
					}
				}
			case "reasoning":
				return fmt.Errorf("Responses reasoning replay cannot be represented by Gemini") //nolint:staticcheck // ST1005: preserve the existing externally surfaced provider error text.
			default:
				return fmt.Errorf("unsupported Responses input item %v", item["type"])
			}
		}
	}
	return nil
}

type geminiResponsesWriter struct {
	gin.ResponseWriter
	model            string
	stream           bool
	status, consumed int
	buffer           []byte
	state            *apicompat.AnthropicEventToResponsesState
	mapping          apicompat.ResponsesClientToolMapping
	restorer         *apicompat.ResponsesClientToolStreamRestorer
	err              error
	failed           bool
}

func (w *geminiResponsesWriter) WriteHeader(status int) { w.status = status }
func (w *geminiResponsesWriter) WriteHeaderNow()        {}
func (w *geminiResponsesWriter) Status() int            { return w.status }
func (w *geminiResponsesWriter) Size() int {
	if w.consumed > 0 {
		return w.consumed
	}
	return w.ResponseWriter.Size()
}
func (w *geminiResponsesWriter) Written() bool                     { return w.consumed > 0 || w.ResponseWriter.Written() }
func (w *geminiResponsesWriter) WriteString(s string) (int, error) { return w.Write([]byte(s)) }
func (w *geminiResponsesWriter) Flush() {
	if w.ResponseWriter.Written() {
		w.ResponseWriter.Flush()
	}
}
func (w *geminiResponsesWriter) Write(p []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	w.consumed += len(p)
	w.buffer = append(w.buffer, p...)
	if len(w.buffer) > 8<<20 {
		w.err = fmt.Errorf("bridge response exceeds limit")
		return 0, w.err
	}
	if w.stream && w.status < 400 && strings.Contains(w.Header().Get("Content-Type"), "text/event-stream") {
		for {
			frame, rest, ok := geminiBridgeSSEFrame(w.buffer)
			if !ok {
				break
			}
			w.buffer = rest
			data := geminiBridgeSSEData(frame)
			if data == "" {
				continue
			}
			var event apicompat.AnthropicStreamEvent
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				w.err = err
				return 0, err
			}
			if event.Type == "error" {
				message := "Upstream stream failed"
				if event.Error != nil {
					message = event.Error.Message
				}
				w.err = fmt.Errorf("%s", message)
				_ = w.fail(message)
				return len(p), nil
			}
			for _, converted := range apicompat.AnthropicEventToResponsesEvents(&event, w.state) {
				for _, restored := range w.restorer.Restore(converted) {
					wire, err := apicompat.ResponsesEventToSSE(restored)
					if err != nil {
						w.err = err
						return 0, err
					}
					w.Header().Del("Content-Length")
					w.ResponseWriter.WriteHeader(200)
					if _, err = w.ResponseWriter.WriteString(wire); err != nil {
						w.err = err
						return 0, err
					}
				}
			}
		}
	}
	return len(p), nil
}
func (w *geminiResponsesWriter) fail(message string) error {
	if w.failed {
		return nil
	}
	w.failed = true
	status := w.status
	if status < 400 {
		status = http.StatusBadGateway
	}
	if w.ResponseWriter.Written() && w.stream {
		payload := map[string]any{"type": "response.failed", "sequence_number": w.state.SequenceNumber, "response": map[string]any{"id": w.state.ResponseID, "object": "response", "model": w.model, "status": "failed", "error": map[string]any{"code": "upstream_error", "message": message}}}
		data, _ := json.Marshal(payload)
		_, err := fmt.Fprintf(w.ResponseWriter, "event: response.failed\ndata: %s\n\n", data)
		w.ResponseWriter.Flush()
		if err != nil {
			return err
		}
		return fmt.Errorf("%s", message)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Del("Content-Length")
	w.ResponseWriter.WriteHeader(status)
	code := "upstream_error"
	switch status {
	case http.StatusBadRequest:
		code = "invalid_request_error"
	case http.StatusTooManyRequests:
		code = "rate_limit_error"
	}
	data, _ := json.Marshal(map[string]any{"error": map[string]any{"code": code, "message": message}})
	_, err := w.ResponseWriter.Write(data)
	if err != nil {
		return err
	}
	return fmt.Errorf("%s", message)
}
func (w *geminiResponsesWriter) finish(forwardErr error) error {
	if w.err != nil {
		_ = w.fail(w.err.Error())
		return w.err
	}
	if forwardErr != nil {
		_ = w.fail(ExtractUpstreamErrorMessage(w.buffer))
		return forwardErr
	}
	if w.status >= 400 {
		return w.fail(ExtractUpstreamErrorMessage(w.buffer))
	}
	if w.stream {
		if !w.state.CompletedSent {
			err := fmt.Errorf("Upstream stream ended without a terminal event") //nolint:staticcheck // ST1005: preserve the existing externally surfaced provider error text.
			_ = w.fail(err.Error())
			return err
		}
		return nil
	}
	var response apicompat.AnthropicResponse
	if err := json.Unmarshal(w.buffer, &response); err != nil || response.Type != "message" {
		return w.fail("Invalid upstream Messages response")
	}
	response.Model = w.model
	data, err := json.Marshal(apicompat.AnthropicToResponsesResponse(&response))
	if err != nil {
		return err
	}
	data, _, err = apicompat.RestoreResponsesClientToolPayload(data, w.mapping)
	if err != nil {
		return err
	}
	w.Header().Del("Content-Length")
	w.Header().Set("Content-Type", "application/json")
	w.ResponseWriter.WriteHeader(200)
	_, err = w.ResponseWriter.Write(data)
	return err
}
