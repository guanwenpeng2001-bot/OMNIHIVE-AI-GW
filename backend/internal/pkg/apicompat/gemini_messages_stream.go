package apicompat

import (
	"encoding/json"
	"errors"
	"strings"
)

type GeminiMessagesStream struct {
	emit             func(string, any)
	flush            func()
	toolID           func(map[string]any) string
	validateToolCall func(map[string]any, string, string, map[string]bool) error

	finishReason      string
	sawToolUse        bool
	nextBlockIndex    int
	openBlockIndex    int
	openBlockType     string
	seenText          string
	seenThinking      string
	thinkingSignature string
	openToolIndex     int
	openToolID        string
	seenToolIDs       map[string]bool
	openToolName      string
	seenToolJSON      string
}

func NewGeminiMessagesStream(emit func(string, any), flush func(), toolID func(map[string]any) string, validateToolCall func(map[string]any, string, string, map[string]bool) error) *GeminiMessagesStream {
	return &GeminiMessagesStream{
		emit: emit, flush: flush, toolID: toolID, validateToolCall: validateToolCall,
		openBlockIndex: -1, openToolIndex: -1, seenToolIDs: make(map[string]bool),
	}
}

func (s *GeminiMessagesStream) Start(messageID, originalModel string) {
	messageStart := map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id":            messageID,
			"type":          "message",
			"role":          "assistant",
			"model":         originalModel,
			"content":       []any{},
			"stop_reason":   nil,
			"stop_sequence": nil,
			"usage": map[string]any{
				"input_tokens":  0,
				"output_tokens": 0,
			},
		},
	}
	s.emit("message_start", messageStart)
	s.flush()
}

func (s *GeminiMessagesStream) Observe(geminiResp map[string]any, onText func()) error {
	if fr := GeminiFinishReason(geminiResp); fr != "" {
		s.finishReason = fr
	}

	parts := GeminiParts(geminiResp)
	for _, part := range parts {
		text, _ := part["text"].(string)
		thought, _ := part["thought"].(bool)
		signature, _ := part["thoughtSignature"].(string)
		if text != "" || (thought && signature != "") {
			kind, seen := "text", &s.seenText
			if thought {
				kind, seen = "thinking", &s.seenThinking
			}
			delta, updated := GeminiTextDelta(*seen, text)
			*seen = updated
			if delta == "" && (!thought || signature == "") {
				continue
			}
			if s.openToolIndex >= 0 {
				s.emit("content_block_stop", map[string]any{"type": "content_block_stop", "index": s.openToolIndex})
				s.openToolIndex = -1
				s.openToolName = ""
				s.seenToolJSON = ""
			}
			if s.openBlockType != kind || (delta != "" && s.thinkingSignature != "") {
				s.closeTextBlock()
				s.openBlockType = kind
				s.openBlockIndex = s.nextBlockIndex
				s.nextBlockIndex++
				s.emit("content_block_start", map[string]any{
					"type": "content_block_start", "index": s.openBlockIndex,
					"content_block": map[string]any{"type": kind, kind: ""},
				})
			}
			if thought && signature != "" {
				s.thinkingSignature = signature
			}
			if delta != "" {
				onText()
				s.emit("content_block_delta", map[string]any{
					"type": "content_block_delta", "index": s.openBlockIndex,
					"delta": map[string]any{"type": kind + "_delta", kind: delta},
				})
			}
			s.flush()
			continue
		}

		if fc, ok := part["functionCall"].(map[string]any); ok && fc != nil {
			activeID, activeName := s.openToolID, s.openToolName
			if s.openToolIndex < 0 {
				activeID, activeName = "", ""
			}
			if err := s.validateToolCall(fc, activeID, activeName, s.seenToolIDs); err != nil {
				s.emit("error", map[string]any{"type": "error", "error": map[string]any{"type": "upstream_error", "message": err.Error()}})
				s.flush()
				return err
			}
			name, _ := fc["name"].(string)
			args := fc["args"]
			if strings.TrimSpace(name) == "" {
				name = "tool"
			}

			s.closeTextBlock()

			// If we receive streamed tool args in pieces, keep a single tool block open and emit deltas.
			wireID, _ := fc["id"].(string)
			// An ID-less part is a distinct complete call, never a continuation
			// inferred from its name or arguments.
			if s.openToolIndex >= 0 && (wireID == "" || s.openToolID != wireID || s.openToolName != name) {
				s.emit("content_block_stop", map[string]any{
					"type":  "content_block_stop",
					"index": s.openToolIndex,
				})
				s.openToolIndex = -1
				s.openToolName = ""
				s.seenToolJSON = ""
			}

			if s.openToolIndex < 0 {
				s.openToolID = s.toolID(fc)
				s.openToolIndex = s.nextBlockIndex
				s.openToolName = name
				s.nextBlockIndex++
				s.sawToolUse = true

				s.emit("content_block_start", map[string]any{
					"type":  "content_block_start",
					"index": s.openToolIndex,
					"content_block": map[string]any{
						"type":  "tool_use",
						"id":    s.openToolID,
						"name":  name,
						"input": map[string]any{},
					},
				})
			}

			argsJSONText := "{}"
			switch v := args.(type) {
			case nil:
				// keep default "{}"
			case string:
				if strings.TrimSpace(v) != "" {
					argsJSONText = v
				}
			default:
				if b, err := json.Marshal(args); err == nil && len(b) > 0 {
					argsJSONText = string(b)
				}
			}

			delta, newSeen := GeminiTextDelta(s.seenToolJSON, argsJSONText)
			s.seenToolJSON = newSeen
			if delta != "" {
				s.emit("content_block_delta", map[string]any{
					"type":  "content_block_delta",
					"index": s.openToolIndex,
					"delta": map[string]any{
						"type":         "input_json_delta",
						"partial_json": delta,
					},
				})
			}
			s.flush()
		}
	}

	return nil
}

func (s *GeminiMessagesStream) closeTextBlock() {
	if s.openBlockIndex < 0 {
		return
	}
	if s.thinkingSignature != "" {
		s.emit("content_block_delta", map[string]any{
			"type": "content_block_delta", "index": s.openBlockIndex,
			"delta": map[string]any{"type": "signature_delta", "signature": s.thinkingSignature},
		})
		s.thinkingSignature = ""
	}
	s.emit("content_block_stop", map[string]any{"type": "content_block_stop", "index": s.openBlockIndex})
	s.openBlockType = ""
	s.openBlockIndex = -1
}

func (s *GeminiMessagesStream) Finish(inputTokens, outputTokens, cacheReadTokens int) error {
	s.closeTextBlock()
	if s.openToolIndex >= 0 {
		s.emit("content_block_stop", map[string]any{
			"type":  "content_block_stop",
			"index": s.openToolIndex,
		})
	}

	if s.finishReason == "" {
		return errors.New("Gemini stream ended without finishReason") //nolint:staticcheck // ST1005: preserve the existing externally surfaced provider error text.
	}
	stopReason := GeminiFinishReasonToMessagesStopReason(s.finishReason)
	if s.sawToolUse {
		stopReason = "tool_use"
	}

	usageObj := map[string]any{
		"output_tokens":           outputTokens,
		"cache_read_input_tokens": cacheReadTokens,
	}
	if inputTokens > 0 {
		usageObj["input_tokens"] = inputTokens
	}
	s.emit("message_delta", map[string]any{
		"type": "message_delta",
		"delta": map[string]any{
			"stop_reason":   stopReason,
			"stop_sequence": nil,
		},
		"usage": usageObj,
	})
	s.emit("message_stop", map[string]any{
		"type": "message_stop",
	})
	s.flush()

	return nil
}

func GeminiParts(geminiResp map[string]any) []map[string]any {
	if candidates, ok := geminiResp["candidates"].([]any); ok && len(candidates) > 0 {
		if cand, ok := candidates[0].(map[string]any); ok {
			if content, ok := cand["content"].(map[string]any); ok {
				if partsAny, ok := content["parts"].([]any); ok && len(partsAny) > 0 {
					out := make([]map[string]any, 0, len(partsAny))
					for _, p := range partsAny {
						pm, ok := p.(map[string]any)
						if !ok {
							continue
						}
						out = append(out, pm)
					}
					return out
				}
			}
		}
	}
	return nil
}

func GeminiTextDelta(seen, incoming string) (delta, newSeen string) {
	incoming = strings.TrimSuffix(incoming, "\u0000")
	if incoming == "" {
		return "", seen
	}

	// Cumulative mode: incoming contains full text so far.
	if strings.HasPrefix(incoming, seen) {
		return strings.TrimPrefix(incoming, seen), incoming
	}
	// Duplicate/rewind: ignore.
	if strings.HasPrefix(seen, incoming) {
		return "", seen
	}
	// Delta mode: treat incoming as incremental chunk.
	return incoming, seen + incoming
}
