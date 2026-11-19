package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/apicompat"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/cursor"
)

// Only rewritten IDs need storage. Bound retained identities without a cleanup
// goroutine; expired/evicted history is rejected rather than silently injected
// under the wrong execution ID. The cache is shared across protocol adapters.
const cursorToolIDLimit = 4096

func (gateway *CursorGatewayService) publicToolID(ctx context.Context, call cursor.ToolCall) string {
	id := cursor.NormalizeToolCallID(call.CallID)
	if id == call.CallID {
		return id
	}
	// The same call label can occur in different executions; retain both
	// identities in the digest so concurrent turns cannot overwrite each other.
	id = cursor.NormalizeToolCallID(call.CallID + "\x00" + call.ExecID)
	identity := cursor.ToolIdentity{CallID: call.CallID, ExecID: call.ExecID, ExpiresAt: time.Now().Add(cursor.ToolIdentityTTL)}
	gateway.cacheToolIdentity(id, identity)
	if gateway.toolStore != nil {
		storeCtx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		if err := gateway.toolStore.Put(storeCtx, id, identity); err != nil {
			gateway.warnToolStore()
		}
	}
	return id
}

func (gateway *CursorGatewayService) warnToolStore() {
	gateway.toolStoreWarning.Do(func() {
		// Never log identifiers, Redis addresses, or errors containing credentials.
		slog.Warn("cursor: tool identity Redis unavailable; using process-local cache")
	})
}

func (gateway *CursorGatewayService) cacheToolIdentity(id string, identity cursor.ToolIdentity) {
	gateway.toolMu.Lock()
	defer gateway.toolMu.Unlock()
	if gateway.toolIDs == nil {
		gateway.toolIDs = make(map[string]cursor.ToolCall)
		gateway.toolIDExpires = make(map[string]time.Time)
	}
	if _, exists := gateway.toolIDs[id]; !exists {
		if len(gateway.toolIDOrder) == cursorToolIDLimit {
			delete(gateway.toolIDs, gateway.toolIDOrder[0])
			delete(gateway.toolIDExpires, gateway.toolIDOrder[0])
			gateway.toolIDOrder = gateway.toolIDOrder[1:]
		}
		gateway.toolIDOrder = append(gateway.toolIDOrder, id)
	}
	// Arguments can be large and are already supplied in the client's history.
	gateway.toolIDs[id] = cursor.ToolCall{CallID: identity.CallID, ExecID: identity.ExecID}
	gateway.toolIDExpires[id] = identity.ExpiresAt
}

func (gateway *CursorGatewayService) restoreToolIDs(ctx context.Context, messages []cursor.ChatMessage) error {
	resolve := func(id string) (cursor.ToolCall, error) {
		gateway.toolMu.Lock()
		call, ok := gateway.toolIDs[id]
		valid := ok && time.Now().Before(gateway.toolIDExpires[id])
		gateway.toolMu.Unlock()
		if valid {
			return call, nil
		}
		if strings.HasPrefix(id, "cursor_call_") {
			if gateway.toolStore != nil {
				storeCtx, cancel := context.WithTimeout(ctx, time.Second)
				identity, found, err := gateway.toolStore.Get(storeCtx, id)
				cancel()
				if err != nil {
					gateway.warnToolStore()
				} else if found {
					gateway.cacheToolIdentity(id, identity)
					return cursor.ToolCall{CallID: identity.CallID, ExecID: identity.ExecID}, nil
				}
			}
			return cursor.ToolCall{}, fmt.Errorf("cursor: tool call identity expired; start a new tool turn")
		}
		return cursor.ToolCall{CallID: id}, nil
	}
	for i := range messages {
		item := &messages[i]
		// Never trust an inbound value for gateway-owned execution metadata.
		item.ToolExecID = ""
		if item.ToolCallID != "" {
			call, err := resolve(item.ToolCallID)
			if err != nil {
				return err
			}
			item.ToolCallID, item.ToolExecID = call.CallID, call.ExecID
		}
		if len(item.ToolCalls) == 0 {
			continue
		}
		var calls []map[string]json.RawMessage
		if err := json.Unmarshal(item.ToolCalls, &calls); err != nil {
			return fmt.Errorf("cursor: invalid tool call history")
		}
		for _, entry := range calls {
			delete(entry, "cursor_exec_id")
			var id string
			if err := json.Unmarshal(entry["id"], &id); err != nil {
				return fmt.Errorf("cursor: invalid tool call identity")
			}
			call, err := resolve(id)
			if err != nil {
				return err
			}
			entry["id"], _ = json.Marshal(call.CallID)
			if call.ExecID != "" {
				entry["cursor_exec_id"], _ = json.Marshal(call.ExecID)
			}
		}
		item.ToolCalls, _ = json.Marshal(calls)
	}
	return nil
}

// cursorOutput adapts the shared Chat event stream without taking ownership of
// session refresh, account serialization or the single ForwardResult bill.
type cursorOutput struct {
	estimated bool
	context   *gin.Context
	protocol  string
	anthropic *apicompat.ChatCompletionsToAnthropicStreamState
	responses *apicompat.ChatCompletionsToResponsesStreamState
}

func (gateway *CursorGatewayService) ForwardAsAnthropic(ctx context.Context, requestContext *gin.Context, account *Account, body []byte) (*ForwardResult, error) {
	return gateway.forwardProtocol(ctx, requestContext, account, body, APIProtocolAnthropic)
}
func (gateway *CursorGatewayService) ForwardAsResponses(ctx context.Context, requestContext *gin.Context, account *Account, body []byte) (*ForwardResult, error) {
	return gateway.forwardProtocol(ctx, requestContext, account, body, APIProtocolResponses)
}

func (gateway *CursorGatewayService) forwardProtocol(ctx context.Context, requestContext *gin.Context, account *Account, body []byte, protocol string) (*ForwardResult, error) {
	if gateway == nil {
		return nil, cursorFailover(fmt.Errorf("unavailable"))
	}
	if account.Type == AccountTypeUpstream {
		return gateway.forwardRelayProtocol(ctx, requestContext, account, body, protocol)
	}
	encoded, model, err := cursorNormalizeRequest(body, protocol)
	if err != nil {
		requestContext.JSON(http.StatusBadRequest, cursorErrorPayload(protocol, gin.H{"type": "invalid_request_error", "message": err.Error()}))
		return nil, err
	}
	output := newCursorOutput(requestContext, protocol, model)
	return gateway.forwardChat(ctx, requestContext, account, encoded, output)
}

func newCursorOutput(requestContext *gin.Context, protocol, model string) *cursorOutput {
	return &cursorOutput{context: requestContext, protocol: protocol, anthropic: apicompat.NewChatCompletionsToAnthropicStreamState(model), responses: apicompat.NewChatCompletionsToResponsesStreamState(model)}
}

func cursorNormalizeRequest(body []byte, protocol string) ([]byte, string, error) {
	var chat *apicompat.ChatCompletionsRequest
	var err error
	var thinking *bool
	switch protocol {
	case APIProtocolAnthropic:
		var request apicompat.AnthropicRequest
		if json.Unmarshal(body, &request) != nil {
			return nil, "", fmt.Errorf("cursor: malformed Messages request")
		}
		for _, tool := range request.Tools {
			if (tool.Type != "" && tool.Type != "custom") || tool.Name == "" {
				return nil, "", fmt.Errorf("cursor: only custom function tools are supported")
			}
		}
		chat, err = apicompat.AnthropicToChatCompletionsRequest(&request)
		// Cursor has its own max variant; preserve explicit effort on that scale.
		if err == nil && request.OutputConfig != nil && request.OutputConfig.Effort != "" && (request.Thinking == nil || request.Thinking.Type != "disabled") {
			chat.ReasoningEffort = request.OutputConfig.Effort
		}
		if request.Thinking != nil {
			enabled := request.Thinking.Type == "enabled" || request.Thinking.Type == "adaptive"
			thinking = &enabled
		}

	case APIProtocolResponses:
		var request apicompat.ResponsesRequest
		if json.Unmarshal(body, &request) != nil {
			return nil, "", fmt.Errorf("cursor: malformed Responses request")
		}
		tools, toolErr := apicompat.EffectiveResponsesTools(&request)
		if toolErr != nil {
			return nil, "", fmt.Errorf("cursor: tools are not supported")
		}
		for _, tool := range tools {
			if tool.Type != "function" || tool.Name == "" {
				return nil, "", fmt.Errorf("cursor: only function tools are supported")
			}
		}
		chat, err = apicompat.ResponsesToChatCompletionsRequest(&request)
	default:
		return body, "", nil
	}
	if err != nil {
		return nil, "", fmt.Errorf("cursor: %w", err)
	}
	messages := make([]cursor.ChatMessage, 0, len(chat.Messages))
	for _, item := range chat.Messages {
		var content string
		if len(item.Content) > 0 && json.Unmarshal(item.Content, &content) != nil {
			var parts []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}
			if json.Unmarshal(item.Content, &parts) != nil {
				return nil, "", fmt.Errorf("cursor: invalid message content")
			}
			for _, part := range parts {
				if part.Type != "text" {
					return nil, "", fmt.Errorf("cursor: only text content is supported")
				}
				content += part.Text
			}
		}
		var calls json.RawMessage
		if len(item.ToolCalls) > 0 {
			calls, _ = json.Marshal(item.ToolCalls)
		}
		messages = append(messages, cursor.ChatMessage{Role: item.Role, Content: content, ToolCalls: calls, ToolCallID: item.ToolCallID})
	}
	var extras map[string]json.RawMessage
	_ = json.Unmarshal(body, &extras)
	encoded, err := json.Marshal(gin.H{"model": chat.Model, "tools": chat.Tools, "messages": messages, "stream": chat.Stream, "reasoning_effort": chat.ReasoningEffort, "cursor_thinking": thinking, "fast": extras["fast"], "stream_options": gin.H{"include_usage": true}})
	return encoded, chat.Model, err
}

func cursorErrorPayload(protocol string, detail any) gin.H {
	payload := gin.H{"error": detail}
	if protocol == APIProtocolAnthropic {
		payload["type"] = "error"
	}
	return payload
}

func (output *cursorOutput) event(eventType string, payload any, estimated *bool) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if estimated != nil {
		var object map[string]any
		if err = json.Unmarshal(encoded, &object); err != nil {
			return err
		}
		target := object
		if response, ok := object["response"].(map[string]any); ok {
			target = response
		}
		if usage, ok := target["usage"].(map[string]any); ok {
			usage["estimated"] = *estimated
		}
		encoded, err = json.Marshal(object)
		if err != nil {
			return err
		}
	}
	_, err = fmt.Fprintf(output.context.Writer, "event: %s\ndata: %s\n\n", eventType, encoded)
	output.context.Writer.Flush()
	return err
}
func (output *cursorOutput) chunk(payload any) error {
	if output.protocol == "" {
		if output.estimated {
			annotated, err := cursorEstimatedPayload(payload)
			if err != nil {
				return err
			}
			payload = annotated
		}
		return cursorWriteSSE(output.context, payload)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	var envelope struct {
		Error json.RawMessage `json:"error"`
	}
	_ = json.Unmarshal(encoded, &envelope)
	if len(envelope.Error) > 0 {
		if output.protocol == APIProtocolAnthropic {
			payload = cursorErrorPayload(output.protocol, envelope.Error)
		}
		err := output.event("error", payload, nil)
		if err == nil && output.protocol == APIProtocolAnthropic {
			// The Messages handler must not append a second terminal error.
			MarkResponseCommitted(output.context)
		}
		return err
	}
	var chunk apicompat.ChatCompletionsChunk
	if err = json.Unmarshal(encoded, &chunk); err != nil {
		return err
	}
	if output.protocol == APIProtocolAnthropic {
		for _, event := range apicompat.ChatCompletionsChunkToAnthropicEvents(&chunk, output.anthropic) {
			if err := output.event(event.Type, event, nil); err != nil {
				return err
			}
		}
	} else {
		for _, event := range apicompat.ChatCompletionsChunkToResponsesEvents(&chunk, output.responses) {
			if err := output.event(event.Type, event, nil); err != nil {
				return err
			}
		}
	}
	return nil
}
func (output *cursorOutput) finish(estimated bool) error {
	estimated = estimated || output.estimated
	if estimated {
		if _, err := fmt.Fprint(output.context.Writer, ": usage_source=estimated_missing_turn_ended\n\n"); err != nil {
			return err
		}
	}
	if output.protocol == "" {
		_, err := fmt.Fprint(output.context.Writer, "data: [DONE]\n\n")
		output.context.Writer.Flush()
		return err
	}
	if output.protocol == APIProtocolAnthropic {
		for _, event := range apicompat.FinalizeChatCompletionsAnthropicStream(output.anthropic) {
			if err := output.event(event.Type, event, &estimated); err != nil {
				return err
			}
		}
	} else {
		for _, event := range apicompat.FinalizeChatCompletionsResponsesStream(output.responses) {
			if err := output.event(event.Type, event, &estimated); err != nil {
				return err
			}
		}
	}
	return nil
}
func (output *cursorOutput) response(payload any) error {
	if output.protocol == "" {
		if output.estimated {
			annotated, err := cursorEstimatedPayload(payload)
			if err != nil {
				return err
			}
			payload = annotated
			writeCursorEstimatedUsageHeaders(output.context)
		}
		output.context.JSON(http.StatusOK, payload)
		return nil
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	var chat apicompat.ChatCompletionsResponse
	if err = json.Unmarshal(encoded, &chat); err != nil {
		return err
	}
	var source struct {
		Usage struct {
			Estimated bool `json:"estimated"`
		} `json:"usage"`
	}
	_ = json.Unmarshal(encoded, &source)
	var response any
	if output.protocol == APIProtocolAnthropic {
		response = apicompat.ChatCompletionsResponseToAnthropic(&chat, chat.Model)
	} else {
		response = apicompat.ChatCompletionsResponseToResponses(&chat, chat.Model, nil, nil, false, nil)
	}
	encoded, err = json.Marshal(response)
	if err != nil {
		return err
	}
	var object map[string]any
	if err = json.Unmarshal(encoded, &object); err != nil {
		return err
	}
	if usage, ok := object["usage"].(map[string]any); ok {
		usage["estimated"] = source.Usage.Estimated || output.estimated
	}
	output.context.JSON(http.StatusOK, object)
	return nil
}

// Chat structs intentionally omit vendor extension fields; restore the upstream
// estimate marker when a relay protocol conversion crossed that typed boundary.
func cursorEstimatedPayload(payload any) (map[string]any, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	var object map[string]any
	if err = json.Unmarshal(encoded, &object); err != nil {
		return nil, err
	}
	if usage, ok := object["usage"].(map[string]any); ok {
		usage["estimated"] = true
	}
	return object, nil
}

func writeCursorEstimatedUsageHeaders(c *gin.Context) {
	const source = "estimated_missing_turn_ended"
	c.Header("X-OmniHive-Usage-Source", source)
	c.Header("X-Sub2API-Usage-Source", source)
}
