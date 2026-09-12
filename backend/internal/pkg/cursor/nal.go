package cursor

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

// AgentService wire contract references the Cursor 3.16 protocol described in
// Wei-Shaw/sub2api PR 6289. Field numbers stay within this protocol package.
const (
	clientRun           = 1
	clientKV            = 3
	clientHeartbeat     = 7
	serverInteraction   = 1
	serverKV            = 4
	runState            = 1
	runAction           = 2
	runModel            = 3
	runTools            = 4
	runConversationID   = 5
	runRequestedModel   = 9
	runIdentifier       = 25
	stateMode           = 10
	askMode             = 2
	actionUserMessage   = 1
	userActionMessage   = 1
	userActionContext   = 2
	userActionPrepend   = 4
	userMessageText     = 1
	userMessageID       = 2
	userMessageMode     = 4
	contextEnvironment  = 4
	environmentTimezone = 10
	modelIdentifier     = 1
	kvIdentifier        = 1
	kvGet               = 2
	kvSet               = 3
	blobIdentifier      = 1
	blobContents        = 2
	getResultContents   = 1
	getResultError      = 2
	errorMessage        = 1
)

type ChatMessage struct {
	Role       string          `json:"role"`
	Content    string          `json:"content"`
	ToolCalls  json.RawMessage `json:"tool_calls,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
	// ToolExecID is internal history metadata, restored by the gateway from the
	// client-visible ID. Native execution acknowledgements retain the same ID.
	ToolExecID string `json:"cursor_exec_id,omitempty"`
}

func encodeUserMessage(text string) message {
	var encoded message
	encoded.text(userMessageText, text)
	encoded.text(userMessageID, uuid.NewString())
	encoded.number(userMessageMode, askMode)
	return encoded
}

// isInstructionRole 标记只承载指令、不参与"当前动作"选择的消息角色。
func isInstructionRole(role string) bool {
	return role == "system" || role == "developer"
}

// encodeRun 把对话编码为 AgentService Run 请求。
// Cursor 当前服务端拒绝 Run 的 system 字段（field 8）：填入任何值都会返回
// invalid_argument "unknown option '--system-prompt'"（2026-09-12 真机验证），
// 因此 system/developer 消息按既有 "role: text" 约定折进对话的 prepend 条目。
// 指令角色不参与"最后一条 = 当前动作"的选择：没有可动作消息时动作文本为空。
func encodeRun(messages []ChatMessage, model string, tools ...ToolDefinition) (message, string) {
	actionIndex := -1
	for index, item := range messages {
		if !isInstructionRole(item.Role) {
			actionIndex = index
		}
	}
	var userAction, environment, requestContext message
	environment.text(environmentTimezone, "UTC")
	requestContext.bytes(contextEnvironment, environment)
	userAction.bytes(userActionContext, requestContext)
	for index, item := range messages {
		if index == actionIndex || (isInstructionRole(item.Role) && item.Content == "") {
			continue
		}
		text := item.historyText()
		if item.Role != "user" {
			text = item.Role + ": " + text
		}
		userAction.bytes(userActionPrepend, encodeUserMessage(text))
	}
	actionText := ""
	if actionIndex >= 0 {
		actionText = messages[actionIndex].historyText()
	}
	userAction.bytes(userActionMessage, encodeUserMessage(actionText))
	var action, state, modelDetails, run, client message
	action.bytes(actionUserMessage, userAction)
	state.number(stateMode, askMode)
	modelDetails.text(modelIdentifier, model)
	run.bytes(runState, state)
	run.bytes(runAction, action)
	run.bytes(runModel, modelDetails)
	run.bytes(runTools, encodeTools(tools))
	run.text(runConversationID, uuid.NewString())
	run.bytes(runRequestedModel, modelDetails)
	runID := uuid.NewString()
	run.text(runIdentifier, runID)
	client.bytes(clientRun, run)
	return client, runID
}

// replyKV only retains blobs for one Run. Limits bound memory even if a server
// sends many distinct set operations; cancellation discards the entire map.
func replyKV(payload []byte, blobs map[string][]byte, storedBytes *int) ([]byte, bool, error) {
	decoded, err := fields(payload)
	if err != nil {
		return nil, false, err
	}
	kv := findField(decoded, serverKV)
	if kv.number == 0 {
		return nil, false, nil
	}
	operation, err := fields(kv.data)
	if err != nil {
		return nil, true, err
	}
	var result message
	result.number(kvIdentifier, findField(operation, kvIdentifier).integer)
	if setter := findField(operation, kvSet); setter.number != 0 {
		arguments, err := fields(setter.data)
		if err != nil {
			return nil, true, err
		}
		identifier := string(findField(arguments, blobIdentifier).data)
		contents := findField(arguments, blobContents).data
		*storedBytes += len(contents) - len(blobs[identifier])
		if *storedBytes > 64<<20 {
			return nil, true, fmt.Errorf("cursor: Run blob limit exceeded")
		}
		blobs[identifier] = append([]byte(nil), contents...)
		result.bytes(kvSet, nil)
	} else if getter := findField(operation, kvGet); getter.number != 0 {
		arguments, err := fields(getter.data)
		if err != nil {
			return nil, true, err
		}
		var response message
		contents, exists := blobs[string(findField(arguments, blobIdentifier).data)]
		if exists {
			response.bytes(getResultContents, contents)
		} else {
			var missing message
			missing.text(errorMessage, "blob not found")
			response.bytes(getResultError, missing)
		}
		result.bytes(kvGet, response)
	} else {
		return nil, true, fmt.Errorf("cursor: unsupported KV operation")
	}
	var client message
	client.bytes(clientKV, result)
	return client, true, nil
}

// Tool history remains structured JSON with the exact call ID. It is folded
// into the existing user/prepend envelope, without inventing a Run state schema.
func (item ChatMessage) historyText() string {
	if len(item.ToolCalls) == 0 && item.ToolCallID == "" {
		return item.Content
	}
	encoded, _ := json.Marshal(item)
	return string(encoded)
}
