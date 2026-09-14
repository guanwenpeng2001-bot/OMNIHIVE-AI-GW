package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/cursor"
	"github.com/gin-gonic/gin"
)

func cursorRelayURL(account *Account, endpoint string) string {
	protocol := APIProtocolChatCompletions
	if endpoint == "/messages" {
		protocol = APIProtocolAnthropic
	} else if endpoint == "/responses" {
		protocol = APIProtocolResponses
	}
	base := strings.TrimSpace(account.GetCredential("base_url"))
	if bases, ok := account.Credentials["api_base_urls"].(map[string]any); ok {
		if override, ok := bases[protocol].(string); ok && strings.TrimSpace(override) != "" {
			base = strings.TrimSpace(override)
		}
	}
	base = strings.TrimRight(base, "/")
	if !strings.HasSuffix(base, "/v1") {
		base += "/v1"
	}
	return base + endpoint
}
func (gateway *CursorGatewayService) forwardRelayChat(ctx context.Context, requestContext *gin.Context, account *Account, body []byte) (*ForwardResult, error) {
	return gateway.forwardRelayProtocol(ctx, requestContext, account, body, APIProtocolChatCompletions)
}
func (gateway *CursorGatewayService) forwardRelayChatOutput(ctx context.Context, requestContext *gin.Context, account *Account, body []byte, output *cursorOutput) (*ForwardResult, error) {
	return gateway.forwardRelay(ctx, requestContext, account, body, APIProtocolChatCompletions, output)
}
func (gateway *CursorGatewayService) forwardRelayProtocol(ctx context.Context, requestContext *gin.Context, account *Account, body []byte, protocol string) (*ForwardResult, error) {
	var metadata struct {
		Model string `json:"model"`
	}
	_ = json.Unmarshal(body, &metadata)
	outputProtocol := protocol
	if protocol == APIProtocolChatCompletions {
		outputProtocol = ""
	}
	return gateway.forwardRelay(ctx, requestContext, account, body, protocol, newCursorOutput(requestContext, outputProtocol, metadata.Model))
}

func cursorConvertRelayRequest(body []byte, from, to string) ([]byte, error) {
	if from == to {
		return body, nil
	}
	var chat *apicompat.ChatCompletionsRequest
	var err error
	switch from {
	case APIProtocolAnthropic:
		var request apicompat.AnthropicRequest
		if err = json.Unmarshal(body, &request); err == nil {
			chat, err = apicompat.AnthropicToChatCompletionsRequest(&request)
		}
	case APIProtocolResponses:
		var request apicompat.ResponsesRequest
		if err = json.Unmarshal(body, &request); err == nil {
			chat, err = apicompat.ResponsesToChatCompletionsRequest(&request)
		}
	default:
		chat = &apicompat.ChatCompletionsRequest{}
		err = json.Unmarshal(body, chat)
	}
	if err != nil {
		return nil, fmt.Errorf("cursor: invalid relay request")
	}
	if to == APIProtocolChatCompletions {
		return json.Marshal(chat)
	}
	responses, err := apicompat.ChatCompletionsToResponses(chat)
	if err != nil {
		return nil, err
	}
	if to == APIProtocolResponses {
		return json.Marshal(responses)
	}
	anthropic, err := apicompat.ResponsesToAnthropicRequest(responses)
	if err != nil {
		return nil, err
	}
	return json.Marshal(anthropic)
}

func (gateway *CursorGatewayService) forwardRelay(ctx context.Context, requestContext *gin.Context, account *Account, body []byte, inbound string, output *cursorOutput) (*ForwardResult, error) {
	ctx = cursorAccountContext(ctx, account)
	started := time.Now()
	upstream := strings.TrimSpace(account.GetCredential("api_protocol"))
	if upstream == "" || upstream == APIProtocolAdaptive {
		upstream = inbound
	}
	endpoint := ""
	switch upstream {
	case APIProtocolChatCompletions:
		endpoint = "/chat/completions"
	case APIProtocolAnthropic:
		endpoint = "/messages"
	case APIProtocolResponses:
		endpoint = "/responses"
	default:
		return nil, cursorFailover(fmt.Errorf("cursor: unknown relay protocol"))
	}
	var metadata struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	if json.Unmarshal(body, &metadata) != nil {
		return nil, fmt.Errorf("cursor: malformed relay request")
	}
	converted, err := cursorConvertRelayRequest(body, inbound, upstream)
	if err != nil {
		return nil, err
	}
	var payload map[string]json.RawMessage
	if err = json.Unmarshal(converted, &payload); err != nil {
		return nil, err
	}
	mapped := account.GetMappedModel(metadata.Model)
	payload["model"], _ = json.Marshal(mapped)
	// Compatibility request converters default to streaming; preserve caller intent.
	payload["stream"], _ = json.Marshal(metadata.Stream)
	if metadata.Stream && upstream == APIProtocolChatCompletions {
		payload["stream_options"] = json.RawMessage(`{"include_usage":true}`)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, cursorRelayURL(account, endpoint), bytes.NewReader(encoded))
	if err != nil {
		return nil, cursorFailover(err)
	}
	applyCursorRelayHeaders(request.Header, account)
	// Protocol headers take precedence over account configuration.
	request.Header.Set("Accept", "application/json")
	if metadata.Stream {
		request.Header.Set("Accept", "text/event-stream")
	}
	request.Header.Set("Authorization", "Bearer "+account.GetCredential("api_key"))
	request.Header.Set("Content-Type", "application/json")
	if upstream == APIProtocolAnthropic {
		deleteHeaderAllForms(request.Header, "anthropic-version")
		request.Header.Set("anthropic-version", "2023-06-01")
	}
	response, err := cursor.Do(gateway.relayClient, request, false)
	if err != nil {
		return nil, cursorFailover(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, cursorFailover(&cursor.HTTPError{Status: response.StatusCode})
	}
	result := &ForwardResult{Model: metadata.Model, UpstreamModel: mapped, Stream: metadata.Stream, UpstreamHeaders: response.Header.Clone()}
	downstream := output.protocol
	if downstream == "" {
		downstream = APIProtocolChatCompletions
	}
	passthrough := upstream == downstream
	observe := func(data []byte) error {
		var envelope struct {
			Usage struct {
				Estimated bool `json:"estimated"`
			} `json:"usage"`
			Response *struct {
				Usage struct {
					Estimated bool `json:"estimated"`
				} `json:"usage"`
			} `json:"response"`
		}
		_ = json.Unmarshal(data, &envelope)
		output.estimated = output.estimated || envelope.Usage.Estimated || (envelope.Response != nil && envelope.Response.Usage.Estimated)
		return cursorObserveRelay(data, upstream, result)
	}
	if !metadata.Stream {
		data, err := io.ReadAll(io.LimitReader(response.Body, (16<<20)+1))
		if err != nil || len(data) > 16<<20 {
			return nil, cursorFailover(fmt.Errorf("cursor: relay body read failed"))
		}
		if err = observe(data); err != nil {
			return nil, cursorFailover(err)
		}
		if passthrough {
			requestContext.Data(http.StatusOK, "application/json", data)
		} else {
			var chat *apicompat.ChatCompletionsResponse
			switch upstream {
			case APIProtocolAnthropic:
				var source apicompat.AnthropicResponse
				err = json.Unmarshal(data, &source)
				chat = apicompat.ResponsesToChatCompletions(apicompat.AnthropicToResponsesResponse(&source), metadata.Model)
			case APIProtocolResponses:
				var source apicompat.ResponsesResponse
				err = json.Unmarshal(data, &source)
				chat = apicompat.ResponsesToChatCompletions(&source, metadata.Model)
			default:
				chat = &apicompat.ChatCompletionsResponse{}
				err = json.Unmarshal(data, chat)
			}
			if err != nil {
				return nil, cursorFailover(err)
			}
			err = output.response(chat)
		}
		result.Duration = time.Since(started)
		return result, err
	}
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 4096), 4<<20)
	responsesState := apicompat.NewResponsesEventToChatState()
	anthropicState := apicompat.NewAnthropicEventToResponsesState()
	emitted, done := false, false
	writeChunks := func(chunks []apicompat.ChatCompletionsChunk) error {
		for _, chunk := range chunks {
			if err := output.chunk(chunk); err != nil {
				return err
			}
		}
		return nil
	}
	process := func(eventName, data string) error {
		if data == "[DONE]" {
			done = true
			return nil
		}
		if data == "" {
			return nil
		}
		if err := observe([]byte(data)); err != nil {
			return err
		}
		var eventType struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal([]byte(data), &eventType)
		if eventName == "" {
			eventName = eventType.Type
		}
		if eventName == "message_stop" || eventName == "response.completed" || eventName == "response.incomplete" {
			done = true
		}
		if !emitted {
			requestContext.Header("Content-Type", "text/event-stream")
			requestContext.Header("Cache-Control", "no-cache")
			elapsed := int(time.Since(started).Milliseconds())
			result.FirstTokenMs = &elapsed
			emitted = true
		}
		if passthrough {
			var err error
			if eventName != "" {
				_, err = fmt.Fprintf(requestContext.Writer, "event: %s\n", eventName)
				if err != nil {
					return err
				}
			}
			for _, line := range strings.Split(data, "\n") {
				if _, err = fmt.Fprintf(requestContext.Writer, "data: %s\n", line); err != nil {
					return err
				}
			}
			_, err = fmt.Fprint(requestContext.Writer, "\n")
			requestContext.Writer.Flush()
			return err
		}
		switch upstream {
		case APIProtocolChatCompletions:
			var chunk apicompat.ChatCompletionsChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				return err
			}
			return output.chunk(chunk)
		case APIProtocolResponses:
			var event apicompat.ResponsesStreamEvent
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				return err
			}
			return writeChunks(apicompat.ResponsesEventToChatChunks(&event, responsesState))
		default:
			var event apicompat.AnthropicStreamEvent
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				return err
			}
			for _, converted := range apicompat.AnthropicEventToResponsesEvents(&event, anthropicState) {
				if err := writeChunks(apicompat.ResponsesEventToChatChunks(&converted, responsesState)); err != nil {
					return err
				}
			}
			return nil
		}
	}
	var eventName string
	var dataLines []string
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			err = process(eventName, strings.Join(dataLines, "\n"))
			eventName = ""
			dataLines = nil
			if err != nil || done {
				break
			}
			continue
		}
		if strings.HasPrefix(line, "event:") {
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if err == nil && !done && len(dataLines) > 0 {
		err = process(eventName, strings.Join(dataLines, "\n"))
	}
	if err == nil {
		err = scanner.Err()
	}
	if err == nil && !done {
		err = io.ErrUnexpectedEOF
	}
	result.Duration = time.Since(started)
	if err != nil {
		if !emitted {
			return nil, cursorFailover(err)
		}
		return result, err
	}
	if passthrough {
		if downstream == APIProtocolChatCompletions {
			requestContext.Header("Content-Type", "text/event-stream")
			err = output.finish(false)
		}
	} else {
		err = output.finish(false)
	}
	return result, err
}

// Usage snapshots replace earlier snapshots; Anthropic message_delta only
// supplies fields that changed, so merge present fields with message_start.
func cursorObserveRelay(data []byte, protocol string, result *ForwardResult) error {
	var object map[string]json.RawMessage
	if json.Unmarshal(data, &object) != nil {
		return fmt.Errorf("cursor: malformed relay response")
	}
	if value := object["error"]; len(value) > 0 && string(value) != "null" {
		return fmt.Errorf("cursor: relay error")
	}
	var eventType string
	_ = json.Unmarshal(object["type"], &eventType)
	if eventType == "response.failed" || eventType == "error" {
		return fmt.Errorf("cursor: relay stream failed")
	}
	if value := object["response"]; len(value) > 0 {
		if json.Unmarshal(value, &object) != nil {
			return fmt.Errorf("cursor: malformed relay response")
		}
	}
	if value := object["message"]; len(value) > 0 {
		if json.Unmarshal(value, &object) != nil {
			return fmt.Errorf("cursor: malformed relay message")
		}
	}
	var identifier string
	_ = json.Unmarshal(object["id"], &identifier)
	if identifier != "" {
		result.RequestID = identifier
	}
	raw := object["usage"]
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	if protocol == APIProtocolAnthropic {
		var values map[string]json.RawMessage
		if json.Unmarshal(raw, &values) != nil {
			return fmt.Errorf("cursor: malformed relay usage")
		}
		usage := make(map[string]int)
		for _, key := range []string{"input_tokens", "output_tokens", "cache_read_input_tokens", "cache_creation_input_tokens"} {
			if value, ok := values[key]; ok {
				var tokens int
				if json.Unmarshal(value, &tokens) != nil {
					return fmt.Errorf("cursor: malformed relay usage")
				}
				usage[key] = tokens
			}
		}

		if value, ok := usage["input_tokens"]; ok {
			result.Usage.InputTokens = value
		}
		if value, ok := usage["output_tokens"]; ok {
			result.Usage.OutputTokens = value
		}
		if value, ok := usage["cache_read_input_tokens"]; ok {
			result.Usage.CacheReadInputTokens = value
		}
		if value, ok := usage["cache_creation_input_tokens"]; ok {
			result.Usage.CacheCreationInputTokens = value
		}
	} else {
		var usage apicompat.ResponsesUsage
		if json.Unmarshal(raw, &usage) != nil {
			return fmt.Errorf("cursor: malformed relay usage")
		}
		cached, created := 0, usage.CacheCreationInputTokens
		if usage.InputTokensDetails != nil {
			cached = usage.InputTokensDetails.CachedTokens
		}
		result.Usage = ClaudeUsage{InputTokens: max(0, usage.InputTokens-cached-created), OutputTokens: usage.OutputTokens, CacheReadInputTokens: cached, CacheCreationInputTokens: created}
	}
	return nil
}
