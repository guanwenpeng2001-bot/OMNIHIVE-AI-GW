package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/cursor"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type cursorNativeDriver interface {
	Run(context.Context, cursor.Credentials, []cursor.ChatMessage, string, func(cursor.StreamEvent) error) (cursor.TokenUsage, error)
	AvailableModels(context.Context, cursor.Credentials) ([]cursor.AvailableModel, error)
}

type CursorGatewayService struct {
	sessions    *CursorSessionProvider
	native      cursorNativeDriver
	settings    SettingRepository
	relayClient *http.Client
	locks       sync.Map
	catalogMu   sync.Mutex
	catalog     map[string]cursorCatalogEntry
}
type cursorCatalogEntry struct {
	models  []cursor.AvailableModel
	expires time.Time
}

func NewCursorGatewayService(sessions *CursorSessionProvider, settings SettingRepository) *CursorGatewayService {
	return &CursorGatewayService{sessions: sessions, settings: settings, native: cursor.NewClient(), relayClient: &http.Client{Timeout: 5 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, catalog: make(map[string]cursorCatalogEntry)}
}

func cursorFailover(err error) error {
	status := http.StatusBadGateway
	var upstream *cursor.HTTPError
	if errors.As(err, &upstream) {
		status = upstream.Status
	}
	return &UpstreamFailoverError{StatusCode: status, ResponseBody: []byte("Cursor upstream request failed")}
}

func cursorAuthFailure(err error) bool {
	var upstream *cursor.HTTPError
	var connect *cursor.ConnectError
	return (errors.As(err, &upstream) && (upstream.Status == 401 || upstream.Status == 403)) || (errors.As(err, &connect) && connect.Authentication())
}

func (gateway *CursorGatewayService) credentials(ctx context.Context, account *Account, token string) cursor.Credentials {
	version := account.GetCredential("client_version")
	if version == "" && gateway.settings != nil {
		version, _ = gateway.settings.GetValue(ctx, "cursor_client_version")
	}
	return cursor.Credentials{AccessToken: token, MachineID: account.GetCredential("machine_id"), MacMachineID: account.GetCredential("mac_machine_id"), ClientVersion: version}
}

func (gateway *CursorGatewayService) picker(ctx context.Context, account *Account, credentials cursor.Credentials) []cursor.AvailableModel {
	key := cursorSessionKey(account)
	gateway.catalogMu.Lock()
	cached := gateway.catalog[key]
	gateway.catalogMu.Unlock()
	if time.Now().Before(cached.expires) {
		return cached.models
	}
	models, err := gateway.native.AvailableModels(ctx, credentials)
	if err != nil {
		models = cursor.FallbackModels()
	}
	gateway.catalogMu.Lock()
	if gateway.catalog == nil {
		gateway.catalog = make(map[string]cursorCatalogEntry)
	}
	gateway.catalog[key] = cursorCatalogEntry{models: models, expires: time.Now().Add(5 * time.Minute)}
	gateway.catalogMu.Unlock()
	return models
}

func (gateway *CursorGatewayService) ForwardAsChatCompletions(ctx context.Context, requestContext *gin.Context, account *Account, body []byte) (*ForwardResult, error) {
	return gateway.forwardChat(ctx, requestContext, account, body, &cursorOutput{context: requestContext})
}

func (gateway *CursorGatewayService) forwardChat(ctx context.Context, requestContext *gin.Context, account *Account, body []byte, output *cursorOutput) (*ForwardResult, error) {
	if gateway == nil {
		return nil, cursorFailover(errors.New("unavailable"))
	}
	if account.Type == AccountTypeUpstream {
		return gateway.forwardRelayChatOutput(ctx, requestContext, account, body, output)
	}
	if account.Type != AccountTypeAPIKey && account.Type != AccountTypeOAuth {
		return nil, cursorFailover(errors.New("unsupported account"))
	}
	var request struct {
		Model           string               `json:"model"`
		Messages        []cursor.ChatMessage `json:"messages"`
		Stream          bool                 `json:"stream"`
		ReasoningEffort string               `json:"reasoning_effort"`
		Fast            *bool                `json:"fast"`
		Thinking        *bool                `json:"cursor_thinking"`
		ThinkingConfig  json.RawMessage      `json:"thinking"`
		Reasoning       struct {
			Effort string `json:"effort"`
		} `json:"reasoning"`
		Tools         []apicompat.ChatTool `json:"tools"`
		StreamOptions struct {
			IncludeUsage bool `json:"include_usage"`
		} `json:"stream_options"`
	}
	if err := json.Unmarshal(body, &request); err != nil || request.Model == "" || len(request.Messages) == 0 {
		requestContext.JSON(http.StatusBadRequest, cursorErrorPayload(output.protocol, gin.H{"type": "invalid_request_error", "message": "Cursor requires valid text messages and function tools"}))
		return nil, fmt.Errorf("cursor: invalid Chat request")
	}
	var definitions []cursor.ToolDefinition
	names := make(map[string]bool)
	for _, tool := range request.Tools {
		if tool.Type != "function" || tool.Function == nil || tool.Function.Name == "" || names[tool.Function.Name] {
			requestContext.JSON(http.StatusBadRequest, cursorErrorPayload(output.protocol, gin.H{"type": "invalid_request_error", "message": "Cursor requires unique function tools"}))
			return nil, fmt.Errorf("cursor: invalid tools")
		}
		schema := tool.Function.Parameters
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		var object map[string]any
		if json.Unmarshal(schema, &object) != nil || object == nil {
			requestContext.JSON(http.StatusBadRequest, cursorErrorPayload(output.protocol, gin.H{"type": "invalid_request_error", "message": "Cursor requires an object tool schema"}))
			return nil, fmt.Errorf("cursor: invalid tool schema")
		}
		names[tool.Function.Name] = true
		definitions = append(definitions, cursor.ToolDefinition{Name: tool.Function.Name, Description: tool.Function.Description, Server: "api", InputSchema: schema})
	}
	if request.Thinking == nil && len(request.ThinkingConfig) > 0 && string(request.ThinkingConfig) != "null" {
		var enabled bool
		if json.Unmarshal(request.ThinkingConfig, &enabled) != nil {
			var thinking struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(request.ThinkingConfig, &thinking) != nil || (thinking.Type != "enabled" && thinking.Type != "adaptive" && thinking.Type != "disabled") {
				requestContext.JSON(http.StatusBadRequest, cursorErrorPayload(output.protocol, gin.H{"type": "invalid_request_error", "message": "cursor: invalid thinking option"}))
				return nil, fmt.Errorf("cursor: invalid thinking option")
			}
			enabled = thinking.Type != "disabled"
		}
		request.Thinking = &enabled
	}
	// Also guard direct callers (account probes) that do not pass the scheduler.
	lockValue, _ := gateway.locks.LoadOrStore(account.ID, make(chan struct{}, 1))
	lock := lockValue.(chan struct{})
	select {
	case lock <- struct{}{}:
		defer func() { <-lock }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	started := time.Now()
	token, err := gateway.sessions.GetSessionToken(ctx, account, "")
	if err != nil {
		return nil, cursorFailover(err)
	}
	credentials := gateway.credentials(ctx, account, token)
	effort := request.ReasoningEffort
	if effort == "" {
		effort = request.Reasoning.Effort
	}
	model, err := cursor.ResolveModelVariant(account.GetMappedModel(request.Model), cursor.RunOptions{Effort: effort, Fast: request.Fast, Thinking: request.Thinking}, gateway.picker(ctx, account, credentials))
	if err != nil {
		requestContext.JSON(http.StatusBadRequest, cursorErrorPayload(output.protocol, gin.H{"type": "invalid_request_error", "message": err.Error()}))
		return nil, err
	}
	completionID := "chatcmpl-" + uuid.NewString()
	if output.protocol == APIProtocolAnthropic {
		completionID = "msg-" + uuid.NewString()
	} else if output.protocol == APIProtocolResponses {
		completionID = "resp_" + uuid.NewString()
	}
	var text, thinking strings.Builder
	var toolCalls []gin.H
	seenCalls := make(map[string]bool)
	var firstToken *int
	emitted := false
	// Standard API clients execute tools after this response closes. The native
	// driver acknowledges deferred_to_client immediately and drains to turn_ended.
	// Keep every tool call, but hide text generated in response to that deferral.
	writeEvent := func(event cursor.StreamEvent) error {
		if event.ToolCall == nil && (event.Text == "" || len(toolCalls) > 0) {
			return nil
		}
		if firstToken == nil {
			elapsed := int(time.Since(started).Milliseconds())
			firstToken = &elapsed
		}
		if event.ToolCall != nil {
			call := event.ToolCall
			if seenCalls[call.CallID] {
				return nil
			}
			name := call.ToolName
			if name == "" {
				name = strings.TrimPrefix(call.Name, "api-")
			}
			if !names[name] {
				return fmt.Errorf("cursor: upstream requested undeclared tool")
			}
			seenCalls[call.CallID] = true
			toolCalls = append(toolCalls, gin.H{"id": call.CallID, "type": "function", "function": gin.H{"name": name, "arguments": string(call.Arguments)}})
		} else if event.Type == "thinking" {
			thinking.WriteString(event.Text)
		} else {
			text.WriteString(event.Text)
		}
		if !request.Stream {
			return nil
		}
		if !emitted {
			requestContext.Header("Content-Type", "text/event-stream")
			requestContext.Header("Cache-Control", "no-cache")
			emitted = true
		}
		delta := gin.H{"content": event.Text}
		if event.Type == "thinking" {
			delta = gin.H{"reasoning_content": event.Text}
		}
		if event.ToolCall != nil {
			call := toolCalls[len(toolCalls)-1]
			delta = gin.H{"tool_calls": []any{gin.H{"index": len(toolCalls) - 1, "id": call["id"], "type": "function", "function": call["function"]}}}
		}
		return output.chunk(gin.H{"id": completionID, "object": "chat.completion.chunk", "created": started.Unix(), "model": request.Model, "choices": []any{gin.H{"index": 0, "delta": delta, "finish_reason": nil}}})
	}
	run := func() (cursor.TokenUsage, error) {
		if len(definitions) == 0 {
			return gateway.native.Run(ctx, credentials, request.Messages, model, writeEvent)
		}
		driver, ok := gateway.native.(interface {
			RunWithTools(context.Context, cursor.Credentials, []cursor.ChatMessage, string, cursor.ToolOptions, func(cursor.StreamEvent) error) (cursor.TokenUsage, error)
		})
		if !ok {
			return cursor.TokenUsage{}, fmt.Errorf("cursor: native tool driver unavailable")
		}
		return driver.RunWithTools(ctx, credentials, request.Messages, model, cursor.ToolOptions{Definitions: definitions}, writeEvent)
	}
	usage, err := run()
	if err != nil && !emitted && text.Len() == 0 && thinking.Len() == 0 && len(toolCalls) == 0 && cursorAuthFailure(err) {
		token, refreshErr := gateway.sessions.GetSessionToken(ctx, account, token)
		if refreshErr != nil {
			return nil, cursorFailover(refreshErr)
		}
		credentials.AccessToken = token
		usage, err = run()
	}
	if err != nil && !emitted {
		return nil, cursorFailover(err)
	}
	if text.Len() == 0 && thinking.Len() == 0 && len(toolCalls) == 0 && usage.Estimated && err == nil {
		return nil, cursorFailover(fmt.Errorf("cursor: empty response"))
	}
	if usage.OutputTokens == 0 && usage.ReasoningTokens > 0 {
		usage.OutputTokens = usage.ReasoningTokens
	}
	if usage.Estimated {
		inputCharacters := 0
		for _, message := range request.Messages {
			inputCharacters += utf8.RuneCountInString(message.Content)
		}
		usage.InputTokens = max(1, (inputCharacters+3)/4)
		if usage.OutputTokens == 0 {
			usage.OutputTokens = max(1, (utf8.RuneCountInString(text.String())+utf8.RuneCountInString(thinking.String())+3)/4)
		}
		slog.InfoContext(ctx, "Cursor usage estimated", "account_id", account.ID, "usage_source", "estimated_missing_turn_ended")
	}
	result := &ForwardResult{RequestID: completionID, Model: request.Model, UpstreamModel: model, Stream: request.Stream, Duration: time.Since(started), FirstTokenMs: firstToken, Usage: ClaudeUsage{InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens, CacheReadInputTokens: usage.CacheReadTokens, CacheCreationInputTokens: usage.CacheWriteTokens}}
	chatUsage := cursorChatUsage(usage)
	finishReason := "stop"
	if len(toolCalls) > 0 {
		finishReason = "tool_calls"
	}
	if request.Stream {
		if !emitted {
			requestContext.Header("Content-Type", "text/event-stream")
		}
		if err != nil {
			_ = output.chunk(gin.H{"error": gin.H{"type": "upstream_error", "message": "Cursor stream interrupted"}})
			return result, err
		}
		if writeErr := output.chunk(gin.H{"id": completionID, "object": "chat.completion.chunk", "created": started.Unix(), "model": request.Model, "choices": []any{gin.H{"index": 0, "delta": gin.H{}, "finish_reason": finishReason}}}); writeErr != nil {
			result.ClientDisconnect = true
			return result, writeErr
		}
		if request.StreamOptions.IncludeUsage || output.protocol != "" {
			if writeErr := output.chunk(gin.H{"id": completionID, "object": "chat.completion.chunk", "model": request.Model, "choices": []any{}, "usage": chatUsage}); writeErr != nil {
				result.ClientDisconnect = true
				return result, writeErr
			}
		}
		err = output.finish(usage.Estimated)
	} else {
		message := gin.H{"role": "assistant", "content": text.String()}
		if len(toolCalls) > 0 {
			message["tool_calls"] = toolCalls
		}
		if thinking.Len() > 0 {
			message["reasoning_content"] = thinking.String()
		}
		if usage.Estimated {
			requestContext.Header("X-Sub2API-Usage-Source", "estimated_missing_turn_ended")
		}
		err = output.response(gin.H{"id": completionID, "object": "chat.completion", "created": started.Unix(), "model": request.Model, "choices": []any{gin.H{"index": 0, "message": message, "finish_reason": finishReason}}, "usage": chatUsage})
	}
	return result, err
}

func cursorWriteSSE(requestContext *gin.Context, payload any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(requestContext.Writer, "data: %s\n\n", encoded)
	requestContext.Writer.Flush()
	return err
}

func cursorChatUsage(usage cursor.TokenUsage) gin.H {
	prompt := usage.InputTokens + usage.CacheReadTokens + usage.CacheWriteTokens
	return gin.H{"prompt_tokens": prompt, "completion_tokens": usage.OutputTokens, "total_tokens": prompt + usage.OutputTokens, "prompt_tokens_details": gin.H{"cached_tokens": usage.CacheReadTokens, "cache_write_tokens": usage.CacheWriteTokens}, "completion_tokens_details": gin.H{"reasoning_tokens": usage.ReasoningTokens}, "estimated": usage.Estimated}
}
