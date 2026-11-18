package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/apicompat"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/googleapi"
)

// GeminiProtocolWriter only adapts the existing Messages/count/embedding
// writers. It never owns transport, scheduling, retries or settlement.
type GeminiProtocolWriter struct {
	openRouterSource    *openRouterContinuationSource
	openRouterDetails   []json.RawMessage
	openRouterToolParts []any
	openRouterToolBytes int
	gin.ResponseWriter
	model, action                        string
	stream                               bool
	includeThoughts                      bool
	jsonStream, jsonStarted, upstreamSSE bool
	count                                int
	status, consumed                     int
	buffer                               []byte
	err                                  error
	terminal                             bool
	usage                                apicompat.AnthropicUsage
	stop                                 string
	blocks                               map[int]*apicompat.AnthropicContentBlock
	args                                 map[int]string
}

// UseJSONStream matches Google's default/alt=json array representation while
// still forwarding each upstream chunk immediately.
func (w *GeminiProtocolWriter) UseJSONStream() { w.jsonStream = true }

// Only actual upstream summaries may be returned, and only when requested.
// Opaque provider signatures are never converted into Google signatures.
func (w *GeminiProtocolWriter) ConfigureThinking(body []byte) {
	var request map[string]any
	if json.Unmarshal(body, &request) != nil || normalizeGeminiProtoJSON(request) != nil {
		return
	}
	if config, ok := request["generationConfig"].(map[string]any); ok {
		if thinking, ok := config["thinkingConfig"].(map[string]any); ok {
			w.includeThoughts, _ = thinking["includeThoughts"].(bool)
		}
	}
}

func NewGeminiProtocolWriter(w gin.ResponseWriter, model, action string, stream bool, count int) *GeminiProtocolWriter {
	return &GeminiProtocolWriter{ResponseWriter: w, model: model, action: action, stream: stream, count: count, status: http.StatusOK, blocks: map[int]*apicompat.AnthropicContentBlock{}, args: map[int]string{}}
}
func (w *GeminiProtocolWriter) WriteHeader(status int) { w.status = status }
func (w *GeminiProtocolWriter) WriteHeaderNow()        {}
func (w *GeminiProtocolWriter) Status() int            { return w.status }
func (w *GeminiProtocolWriter) Size() int {
	if w.stream && w.status < 400 && (w.upstreamSSE || w.consumed == 0) {
		return w.ResponseWriter.Size()
	}
	if w.consumed > 0 {
		return w.consumed
	}
	return w.ResponseWriter.Size()
}
func (w *GeminiProtocolWriter) Written() bool {
	if w.stream && w.status < 400 && (w.upstreamSSE || w.consumed == 0) {
		return w.ResponseWriter.Written()
	}
	return w.consumed > 0 || w.ResponseWriter.Written()
}
func (w *GeminiProtocolWriter) WriteString(s string) (int, error) { return w.Write([]byte(s)) }
func (w *GeminiProtocolWriter) Flush() {
	if w.ResponseWriter.Written() {
		w.ResponseWriter.Flush()
	}
}
func (w *GeminiProtocolWriter) Write(p []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	w.consumed += len(p)
	w.buffer = append(w.buffer, p...)
	if len(w.buffer) > googleBridgeResponseLimit {
		w.err = fmt.Errorf("bridge response exceeds limit")
		return 0, w.err
	}
	if strings.Contains(w.Header().Get("Content-Type"), "text/event-stream") {
		w.upstreamSSE = true
	}
	if w.upstreamSSE && w.status < 400 {
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
			if err := w.event(&event); err != nil {
				w.err = err
				return 0, err
			}
		}
	}
	return len(p), nil
}
func geminiBridgeSSEFrame(buffer []byte) (string, []byte, bool) {
	index, size := bytes.Index(buffer, []byte("\n\n")), 2
	if crlf := bytes.Index(buffer, []byte("\r\n\r\n")); crlf >= 0 && (index < 0 || crlf < index) {
		index, size = crlf, 4
	}
	if index < 0 {
		return "", buffer, false
	}
	return string(buffer[:index]), buffer[index+size:], true
}
func geminiBridgeSSEData(frame string) string {
	var data []string
	for _, line := range strings.Split(frame, "\n") {
		if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	return strings.Join(data, "\n")
}
func (w *GeminiProtocolWriter) emit(value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	w.Header().Del("Content-Length")
	if w.stream && (w.status < 400 || w.ResponseWriter.Written()) {
		if w.jsonStream {
			w.Header().Set("Content-Type", "application/json")
			w.ResponseWriter.WriteHeader(200)
			separator := ","
			if !w.jsonStarted {
				separator = "["
				w.jsonStarted = true
			}
			_, err = fmt.Fprintf(w.ResponseWriter, "%s%s", separator, data)
			w.ResponseWriter.Flush()
			return err
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.ResponseWriter.WriteHeader(http.StatusOK)
		_, err = fmt.Fprintf(w.ResponseWriter, "data: %s\n\n", data)
		w.ResponseWriter.Flush()
	} else {
		w.Header().Set("Content-Type", "application/json")
		w.ResponseWriter.WriteHeader(w.status)
		_, err = w.ResponseWriter.Write(data)
	}
	return err
}
func (w *GeminiProtocolWriter) fail(message string) error {
	if w.terminal {
		return nil
	}
	w.terminal = true
	status := w.status
	if status < 400 {
		status = http.StatusBadGateway
		if !w.ResponseWriter.Written() {
			w.status = status
		}
	}
	if err := w.emit(map[string]any{"error": map[string]any{"code": status, "status": googleapi.HTTPStatusToGoogleStatus(status), "message": message}}); err != nil {
		return err
	}
	return fmt.Errorf("%s", message)
}
func (w *GeminiProtocolWriter) chunk(parts []any, finish string) map[string]any {
	candidate := map[string]any{"index": 0, "content": map[string]any{"role": "model", "parts": parts}}
	if finish != "" {
		candidate["finishReason"] = finish
	}
	return map[string]any{"candidates": []any{candidate}, "modelVersion": w.model}
}
func geminiFinishReason(reason string) string {
	if reason == "max_tokens" {
		return "MAX_TOKENS"
	}
	if reason == "refusal" {
		return "SAFETY"
	}
	return "STOP"
}
func geminiUsage(usage apicompat.AnthropicUsage) map[string]any {
	in := usage.InputTokens + usage.CacheReadInputTokens + usage.CacheCreationInputTokens
	return map[string]any{"promptTokenCount": in, "candidatesTokenCount": usage.OutputTokens, "totalTokenCount": in + usage.OutputTokens, "cachedContentTokenCount": usage.CacheReadInputTokens}
}
func messagesBlockToGeminiPart(block apicompat.AnthropicContentBlock) (map[string]any, error) {
	switch block.Type {
	case "text":
		return map[string]any{"text": block.Text}, nil
	case "thinking":
		return map[string]any{"text": block.Thinking, "thought": true}, nil
	case "tool_use":
		args := map[string]any{}
		if len(block.Input) > 0 {
			if err := json.Unmarshal(block.Input, &args); err != nil {
				return nil, err
			}
		}
		if args == nil {
			return nil, fmt.Errorf("tool input must be an object")
		}
		return map[string]any{"functionCall": map[string]any{"id": block.ID, "name": block.Name, "args": args}}, nil
	default:
		return nil, fmt.Errorf("unsupported Messages output block %q", block.Type)
	}
}
func (w *GeminiProtocolWriter) event(e *apicompat.AnthropicStreamEvent) error {
	if w.terminal {
		return nil
	}
	index := 0
	if e.Index != nil {
		index = *e.Index
	}
	switch e.Type {
	case "ping":
		return nil
	case "error":
		message := "Upstream stream failed"
		if e.Error != nil {
			message = e.Error.Message
		}
		return w.fail(message)
	case "message_start":
		if e.Message != nil {
			w.usage = e.Message.Usage
		}
	case "content_block_start":
		if e.ContentBlock == nil {
			return fmt.Errorf("missing content block")
		}
		w.blocks[index] = e.ContentBlock
		if w.openRouterSource != nil && e.ContentBlock.Type == "tool_use" {
			w.openRouterToolBytes += len(e.ContentBlock.Input) + len(e.ContentBlock.ID) + len(e.ContentBlock.Name)
			if w.openRouterToolBytes > googleBridgeResponseLimit {
				return fmt.Errorf("tool state exceeds bridge limit")
			}
		}
		if e.ContentBlock.Type == "thinking" && !w.includeThoughts {
			return nil
		}
		if e.ContentBlock.Type == "tool_use" {
			return nil
		}
		part, err := messagesBlockToGeminiPart(*e.ContentBlock)
		if err != nil {
			return err
		}
		if part["text"] != "" {
			return w.emit(w.chunk([]any{part}, ""))
		}
	case "content_block_delta":
		if e.Delta == nil {
			return fmt.Errorf("missing content delta")
		}
		switch e.Delta.Type {
		case "text_delta":
			return w.emit(w.chunk([]any{map[string]any{"text": e.Delta.Text}}, ""))
		case "thinking_delta":
			if !w.includeThoughts {
				return nil
			}
			return w.emit(w.chunk([]any{map[string]any{"text": e.Delta.Thinking, "thought": true}}, ""))
		case "input_json_delta":
			if block := w.blocks[index]; block == nil || block.Type != "tool_use" {
				return fmt.Errorf("tool argument delta has no tool call")
			}
			if w.openRouterSource != nil {
				w.openRouterToolBytes += len(e.Delta.PartialJSON)
				if w.openRouterToolBytes > googleBridgeResponseLimit {
					return fmt.Errorf("tool state exceeds bridge limit")
				}
			}
			w.args[index] += e.Delta.PartialJSON
		case "signature_delta":
			return nil
		default:
			return fmt.Errorf("unsupported Messages delta %q", e.Delta.Type)
		}
	case "content_block_stop":
		if block := w.blocks[index]; block != nil && block.Type == "tool_use" {
			if args := w.args[index]; args != "" {
				block.Input = json.RawMessage(args)
			}
			part, err := messagesBlockToGeminiPart(*block)
			if err != nil {
				return err
			}
			if w.openRouterSource != nil {
				w.openRouterToolParts = append(w.openRouterToolParts, part)
				return nil
			}
			return w.emit(w.chunk([]any{part}, ""))
		}
	case "message_delta":
		if e.Delta != nil {
			w.stop = e.Delta.StopReason
		}
		if e.Usage != nil {
			w.usage.OutputTokens = e.Usage.OutputTokens
			if e.Usage.InputTokens > 0 {
				w.usage.InputTokens = e.Usage.InputTokens
			}
			if e.Usage.CacheReadInputTokens > 0 {
				w.usage.CacheReadInputTokens = e.Usage.CacheReadInputTokens
			}
		}
	case "message_stop":
		if w.stop == "" {
			return fmt.Errorf("upstream stream ended without stop reason")
		}
		parts := []any{}
		var err error
		if w.openRouterSource != nil {
			parts, err = w.attachOpenRouterContinuation(w.openRouterToolParts)
		}
		if err != nil {
			return err
		}
		result := w.chunk(parts, geminiFinishReason(w.stop))
		result["usageMetadata"] = geminiUsage(w.usage)
		w.terminal = true
		return w.emit(result)
	default:
		return fmt.Errorf("unsupported Messages stream event %q", e.Type)
	}
	return nil
}

// Finish must run after the delegated handler. Missing stream terminals are
// errors, never synthesized successful replies.
func (w *GeminiProtocolWriter) Finish() (finishErr error) {
	defer func() {
		if w.jsonStarted {
			_, err := w.ResponseWriter.WriteString("]")
			if finishErr == nil {
				finishErr = err
			}
			w.ResponseWriter.Flush()
			w.jsonStarted = false
		}
	}()
	if w.err != nil {
		_ = w.fail(w.err.Error())
		return w.err
	}
	if w.terminal {
		return nil
	}
	if w.status >= 400 {
		return w.fail(ExtractUpstreamErrorMessage(w.buffer))
	}
	if w.stream {
		return w.fail("Upstream stream ended before message_stop")
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(w.buffer, &raw); err != nil {
		return w.fail("Invalid upstream JSON response")
	}
	if raw["error"] != nil {
		return w.fail(ExtractUpstreamErrorMessage(w.buffer))
	}
	switch w.action {
	case "countTokens":
		var tokens int
		if err := json.Unmarshal(raw["input_tokens"], &tokens); err != nil || tokens < 0 {
			return w.fail("Invalid token count response")
		}
		out := map[string]any{"totalTokens": tokens}
		if w.Header().Get("X-Token-Count-Estimated") == "true" {
			out["estimated"] = true
		}
		return w.emit(out)
	case "embedContent", "batchEmbedContents":
		var rows []struct {
			Index     *int              `json:"index"`
			Embedding []json.RawMessage `json:"embedding"`
		}
		if err := json.Unmarshal(raw["data"], &rows); err != nil || len(rows) != w.count {
			return w.fail("Invalid embedding response count")
		}
		vectors := make([]any, w.count)
		dimensions := 0
		for _, row := range rows {
			if row.Index == nil || *row.Index < 0 || *row.Index >= w.count || vectors[*row.Index] != nil || len(row.Embedding) == 0 {
				return w.fail("Invalid embedding index or empty vector")
			}
			if dimensions > 0 && dimensions != len(row.Embedding) {
				return w.fail("Inconsistent embedding vector dimensions")
			}
			dimensions = len(row.Embedding)
			values := make([]float64, len(row.Embedding))
			for i, value := range row.Embedding {
				number, err := strconv.ParseFloat(string(value), 64)
				if err != nil {
					return w.fail("Embedding values must be numbers")
				}
				values[i] = number
			}
			vectors[*row.Index] = map[string]any{"values": values}
		}
		if w.action == "embedContent" {
			return w.emit(map[string]any{"embedding": vectors[0]})
		}
		return w.emit(map[string]any{"embeddings": vectors})
	default:
		var response apicompat.AnthropicResponse
		if err := json.Unmarshal(w.buffer, &response); err != nil || response.Type != "message" {
			return w.fail("Invalid Messages response")
		}
		parts := make([]any, 0, len(response.Content))
		for _, block := range response.Content {
			if block.Type == "thinking" && !w.includeThoughts {
				continue
			}
			part, err := messagesBlockToGeminiPart(block)
			if err != nil {
				return w.fail(err.Error())
			}
			parts = append(parts, part)
		}
		parts, err := w.attachOpenRouterContinuation(parts)
		if err != nil {
			return w.fail(err.Error())
		}
		result := w.chunk(parts, geminiFinishReason(apicompat.AnthropicStopReasonString(response.StopReason)))
		result["usageMetadata"] = geminiUsage(response.Usage)
		return w.emit(result)
	}
}
