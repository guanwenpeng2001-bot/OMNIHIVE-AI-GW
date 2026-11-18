package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/apicompat"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const googleBridgeResponseLimit = 8 << 20
const openRouterContinuationNamespace = "sub2api.reasoning-continuation"

type openRouterContinuationSource struct {
	Platform string `json:"platform"`
	Protocol string `json:"protocol"`
	Endpoint string `json:"endpoint"`
}
type openRouterContinuation struct {
	Namespace string                       `json:"namespace"`
	Source    openRouterContinuationSource `json:"source"`
	Details   json.RawMessage              `json:"details"`
}
type openRouterGoogleHistory struct {
	AssistantIndex int
	Continuation   openRouterContinuation
}
type openRouterGoogleHistoryKey struct{}

type geminiProtocolWriterKey struct{}

// Keep the selected Google adapter reachable through budget/observability
// ResponseWriter wrappers without coupling either wrapper to this protocol.
func WithGeminiProtocolWriter(ctx context.Context, writer *GeminiProtocolWriter) context.Context {
	return context.WithValue(ctx, geminiProtocolWriterKey{}, writer)
}
func googleContinuationWriter(request *gin.Context) *GeminiProtocolWriter {
	if request.Request != nil {
		if writer, ok := request.Request.Context().Value(geminiProtocolWriterKey{}).(*GeminiProtocolWriter); ok {
			return writer
		}
	}
	writer, _ := request.Writer.(*GeminiProtocolWriter)
	return writer
}

// A non-authenticating source label, not a MAC or vendor signature. Exclude
// credentials and private infrastructure names from the client-carried token.
func openRouterContinuationSourceForTarget(target string) (openRouterContinuationSource, error) {
	parsed, err := url.Parse(target)
	if err != nil || parsed.Host == "" {
		return openRouterContinuationSource{}, fmt.Errorf("invalid continuation source endpoint")
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	if parsed.Scheme == "https" {
		parsed.Host = strings.TrimSuffix(parsed.Host, ":443")
	}
	if parsed.Scheme == "http" {
		parsed.Host = strings.TrimSuffix(parsed.Host, ":80")
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return openRouterContinuationSource{Platform: PlatformOpenRouter, Protocol: APIProtocolChatCompletions, Endpoint: capabilityHash([]byte(parsed.String()))}, nil
}
func validateOpenRouterDetails(raw json.RawMessage) error {
	if len(raw) > googleBridgeResponseLimit || !gjson.ValidBytes(raw) || !gjson.ParseBytes(raw).IsArray() {
		return fmt.Errorf("invalid gateway continuation reasoning sequence")
	}
	for _, detail := range gjson.ParseBytes(raw).Array() {
		if !detail.IsObject() {
			return fmt.Errorf("reasoning details must contain objects")
		}
	}
	return nil
}
func decodeOpenRouterContinuation(value string) (openRouterContinuation, bool, error) {
	var token openRouterContinuation
	if len(value) > googleBridgeResponseLimit {
		prefix, _ := base64.StdEncoding.DecodeString(value[:512])
		if gjson.GetBytes(prefix, "namespace").String() == openRouterContinuationNamespace {
			return token, true, fmt.Errorf("gateway continuation exceeds bridge response limit")
		}
		return token, false, nil
	}
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		// ProtoJSON bytes accept both base64 alphabets and optional padding;
		// Google SDK serializers may choose URL-safe encoding on replay.
		for _, encoding := range []*base64.Encoding{base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
			if candidate, decodeErr := encoding.DecodeString(value); decodeErr == nil {
				decoded, err = candidate, nil
				break
			}
		}
	}
	parseErr := json.Unmarshal(decoded, &token)
	recognized := token.Namespace == openRouterContinuationNamespace
	if parseErr != nil {
		recognized = gjson.GetBytes(decoded, "namespace").String() == openRouterContinuationNamespace
	}
	if !recognized {
		return token, false, nil
	}
	if err != nil || parseErr != nil {
		return token, true, fmt.Errorf("malformed gateway continuation")
	}

	if token.Source.Platform != PlatformOpenRouter || token.Source.Protocol != APIProtocolChatCompletions || token.Source.Endpoint == "" {
		return token, true, fmt.Errorf("gateway continuation source does not match OpenRouter Chat")
	}
	if err := validateOpenRouterDetails(token.Details); err != nil {
		return token, true, err
	}
	return token, true, nil
}

// Strip only our recognized carrier before the existing Google converter runs.
// Legacy vendor signatures retain their behavior; opaque data is request-local.
func PrepareGoogleContinuation(ctx context.Context, body []byte, platform string) (context.Context, []byte, error) {
	var root map[string]any
	if json.Unmarshal(body, &root) != nil {
		return ctx, body, nil
	}
	if err := normalizeGeminiProtoJSON(root); err != nil {
		return ctx, nil, err
	}
	history := []openRouterGoogleHistory{}
	assistantIndex := 0
	changed := false
	contents, _ := root["contents"].([]any)
	for _, rawContent := range contents {
		content, ok := rawContent.(map[string]any)
		if !ok {
			continue
		}
		role, _ := content["role"].(string)
		parts, _ := content["parts"].([]any)
		var previous *openRouterContinuation
		for _, rawPart := range parts {
			part, ok := rawPart.(map[string]any)
			if !ok {
				continue
			}
			value, ok := part["thoughtSignature"].(string)
			if !ok {
				continue
			}
			token, recognized, err := decodeOpenRouterContinuation(value)
			if err != nil {
				return ctx, nil, err
			}
			if !recognized {
				continue
			}
			if platform != PlatformOpenRouter || role != "model" {
				return ctx, nil, fmt.Errorf("gateway continuation is not valid for this source route")
			}
			if previous != nil && (previous.Source != token.Source || string(previous.Details) != string(token.Details)) {
				return ctx, nil, fmt.Errorf("conflicting gateway continuation tokens")
			}
			if previous == nil {
				history = append(history, openRouterGoogleHistory{AssistantIndex: assistantIndex, Continuation: token})
			}
			previous = &token
			delete(part, "thoughtSignature")
			changed = true
		}
		if role == "model" {
			assistantIndex++
		}
	}
	if !changed {
		return ctx, body, nil
	}
	cleaned, err := json.Marshal(root)
	if err != nil {
		return ctx, nil, err
	}
	return context.WithValue(ctx, openRouterGoogleHistoryKey{}, history), cleaned, nil
}

func (gateway *OpenAIGatewayService) prepareOpenRouterGoogleReplay(ctx context.Context, request *gin.Context, account *Account, chatBody []byte) ([]byte, error) {
	if !openRouterGoogleIngress(ctx) {
		return chatBody, nil
	}
	target, err := gateway.openAIChatCompletionsTargetURL(account)
	if err != nil {
		return nil, err
	}
	source, err := openRouterContinuationSourceForTarget(target)
	if err != nil {
		return nil, err
	}
	if writer := googleContinuationWriter(request); writer != nil {
		if writer.ResponseWriter.Written() {
			return nil, fmt.Errorf("cannot replay an OpenRouter attempt after client output")
		}
		writer.buffer = nil
		writer.err = nil
		writer.terminal = false
		writer.consumed = 0
		writer.status = http.StatusOK
		writer.upstreamSSE = false
		writer.blocks = map[int]*apicompat.AnthropicContentBlock{}
		writer.args = map[int]string{}
		writer.usage = apicompat.AnthropicUsage{}
		writer.stop = ""
		writer.openRouterSource = &source
		writer.openRouterDetails = nil
		writer.openRouterToolParts = nil
		writer.openRouterToolBytes = 0
	}
	history, _ := ctx.Value(openRouterGoogleHistoryKey{}).([]openRouterGoogleHistory)
	indexes := []int{}
	for index, message := range gjson.GetBytes(chatBody, "messages").Array() {
		if message.Get("role").String() == "assistant" {
			indexes = append(indexes, index)
		}
	}
	for _, entry := range history {
		if entry.Continuation.Source != source {
			return nil, fmt.Errorf("gateway continuation endpoint source mismatch")
		}
		if entry.AssistantIndex < 0 || entry.AssistantIndex >= len(indexes) {
			return nil, fmt.Errorf("gateway continuation lost its assistant message")
		}
		chatBody, err = sjson.SetRawBytes(chatBody, fmt.Sprintf("messages.%d.reasoning_details", indexes[entry.AssistantIndex]), entry.Continuation.Details)
		if err != nil {
			return nil, err
		}
	}
	return chatBody, nil
}

// Capture arrays before typed compatibility structs discard unknown fields.
// This hook is only on the OpenRouter Google writer, never the raw Chat relay.
func captureOpenRouterGoogleDetails(request *gin.Context, raw []byte, stream bool) error {
	writer := googleContinuationWriter(request)
	if writer == nil || writer.openRouterSource == nil {
		return nil
	}
	if !gjson.ValidBytes(raw) || !gjson.ParseBytes(raw).IsObject() || !gjson.GetBytes(raw, "choices").IsArray() {
		return fmt.Errorf("invalid OpenRouter Chat frame")
	}
	path := "choices.0.message.reasoning_details"
	if stream {
		path = "choices.0.delta.reasoning_details"
	}
	details := gjson.GetBytes(raw, path)
	if !details.Exists() {
		return nil
	}
	if err := validateOpenRouterDetails([]byte(details.Raw)); err != nil {
		return err
	}
	for _, detail := range details.Array() {
		writer.openRouterDetails = append(writer.openRouterDetails, json.RawMessage(detail.Raw))
	}
	encoded, err := json.Marshal(writer.openRouterDetails)
	if err != nil || len(encoded) > googleBridgeResponseLimit {
		return fmt.Errorf("OpenRouter reasoning sequence exceeds bridge limit")
	}
	return nil
}

func (writer *GeminiProtocolWriter) attachOpenRouterContinuation(parts []any) ([]any, error) {
	if writer.openRouterSource == nil || len(writer.openRouterDetails) == 0 {
		return parts, nil
	}
	details, err := json.Marshal(writer.openRouterDetails)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(openRouterContinuation{Namespace: openRouterContinuationNamespace, Source: *writer.openRouterSource, Details: details})
	if err != nil {
		return nil, err
	}
	token := base64.StdEncoding.EncodeToString(encoded)
	if len(token) > googleBridgeResponseLimit {
		return nil, fmt.Errorf("gateway continuation exceeds bridge limit")
	}
	if len(parts) == 0 {
		parts = []any{map[string]any{"text": ""}}
	}
	target := 0
	for index, part := range parts {
		if value, ok := part.(map[string]any); ok && value["functionCall"] != nil {
			target = index
			break
		}
	}
	part, ok := parts[target].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid continuation output part")
	}
	part["thoughtSignature"] = token
	bounded, err := json.Marshal(parts)
	if err != nil || len(bounded) > googleBridgeResponseLimit {
		return nil, fmt.Errorf("continuation output exceeds bridge limit")
	}
	return parts, nil
}
