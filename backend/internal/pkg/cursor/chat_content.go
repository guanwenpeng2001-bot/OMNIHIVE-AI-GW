package cursor

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ErrNonTextContent reports a content part that the verified Run text contract
// cannot carry. The gateway rejects such a request instead of silently dropping
// media input, so an image never reaches a text-only Run unnoticed.
var ErrNonTextContent = errors.New("cursor: only text content parts are supported")

// Chat Completions accepts two content shapes: a plain string and an ordered
// array of typed parts. Cursor's verified Run contract carries text and tool
// history only (internal/pkg/cursor/nal.go), so text parts are concatenated in
// order and every other part type stays rejected. Remaining message fields keep
// the plain decoder behaviour, and an absent or empty content (assistant
// tool-call history) stays valid empty text.
func (message *ChatMessage) UnmarshalJSON(payload []byte) error {
	var decoded struct {
		Role       string          `json:"role"`
		Content    json.RawMessage `json:"content"`
		ToolCalls  json.RawMessage `json:"tool_calls"`
		ToolCallID string          `json:"tool_call_id"`
		ToolExecID string          `json:"cursor_exec_id"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return err
	}
	content, err := chatMessageText(decoded.Content)
	if err != nil {
		return err
	}
	message.Role = decoded.Role
	message.Content = content
	message.ToolCalls = decoded.ToolCalls
	message.ToolCallID = decoded.ToolCallID
	message.ToolExecID = decoded.ToolExecID
	return nil
}

// chatMessageText normalises one content field to the Run text contract.
func chatMessageText(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}
	// A JSON null is valid empty text for every message role.
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text, nil
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return "", fmt.Errorf("cursor: content must be a string or an array of content parts")
	}
	var joined strings.Builder
	for _, part := range parts {
		if part.Type != "text" {
			return "", fmt.Errorf("%w: %q", ErrNonTextContent, part.Type)
		}
		joined.WriteString(part.Text)
	}
	return joined.String(), nil
}
