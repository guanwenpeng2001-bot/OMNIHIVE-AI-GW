package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type QianwenNativeOperation string

const (
	QianwenNativeText            QianwenNativeOperation = "text_generation"
	QianwenNativeMultimodal      QianwenNativeOperation = "multimodal_generation"
	QianwenNativeEmbedding       QianwenNativeOperation = "text_embedding"
	QianwenNativeRerank          QianwenNativeOperation = "text_rerank"
	QianwenNativeVoiceEnrollment QianwenNativeOperation = "voice_enrollment"
	QianwenNativeTextPath                               = "/api/v1/services/aigc/text-generation/generation"
	QianwenNativeMultimodalPath                         = "/api/v1/services/aigc/multimodal-generation/generation"
)

func QianwenNativeOperationForPath(path string) (QianwenNativeOperation, bool) {
	switch path {
	case QianwenNativeTextPath:
		return QianwenNativeText, true
	case QianwenNativeMultimodalPath:
		return QianwenNativeMultimodal, true
	case QianwenNativeEmbeddingPath:
		return QianwenNativeEmbedding, true
	case QianwenNativeRerankPath:
		return QianwenNativeRerank, true
	case QianwenNativeVoiceEnrollmentPath:
		return QianwenNativeVoiceEnrollment, true
	default:
		return "", false
	}
}

// ParseQianwenNativeBody validates the supported native input without converting
// it to a compatible protocol. Model aliases are rewritten by normal routing.
func ParseQianwenNativeBody(body []byte) (string, error) {
	var payload struct {
		Model string `json:"model"`
		Input struct {
			Prompt   string `json:"prompt"`
			Messages []struct {
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		} `json:"input"`
		Parameters struct {
			Modalities []string `json:"modalities"`
		} `json:"parameters"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("invalid DashScope conversation JSON")
	}
	if strings.TrimSpace(payload.Model) == "" {
		return "", fmt.Errorf("DashScope model is required")
	}
	if handled, err := parseQianwenVoiceEnrollmentBody(payload.Model, body); handled {
		return payload.Model, err
	}
	if handled, err := parseQianwenNativeAudioBody(payload.Model, body); handled {
		return payload.Model, err
	}
	if handled, err := parseQianwenNativeRerankBody(payload.Model, body); handled {
		return payload.Model, err
	}
	if handled, err := parseQianwenNativeEmbeddingBody(payload.Model, body); handled {
		return payload.Model, err
	}
	if payload.Input.Prompt == "" && len(payload.Input.Messages) == 0 {
		return "", fmt.Errorf("DashScope input.prompt or input.messages is required")
	}
	for _, modality := range payload.Parameters.Modalities {
		if modality != "text" {
			return "", fmt.Errorf("unsupported DashScope output modality: %s", modality)
		}
	}
	for _, message := range payload.Input.Messages {
		var text string
		if json.Unmarshal(message.Content, &text) == nil {
			continue
		}
		var parts []map[string]json.RawMessage
		if err := json.Unmarshal(message.Content, &parts); err != nil || len(parts) == 0 {
			return "", fmt.Errorf("DashScope message content must be text or image/text parts")
		}
		for _, part := range parts {
			for key := range part {
				if key != "text" && key != "image" && key != "cache_control" {
					return "", fmt.Errorf("unsupported DashScope input part: %s", key)
				}
			}
		}
	}
	return payload.Model, nil
}

// BuildQianwenNativeRequest implements conversations and synchronous Qwen images.
// Other native families fail closed: asynchronous jobs, speech, realtime and
// agents require their own lifecycle and billing adapters.
// The gateway caller owns scheduling, response streaming and usage accounting.
func (s *OpenAIGatewayService) BuildQianwenNativeRequest(ctx context.Context, account *Account, operation QianwenNativeOperation, body []byte, stream bool) (*http.Request, error) {
	if s == nil || account == nil || account.Platform != PlatformQianwen || account.Type != AccountTypeAPIKey {
		return nil, fmt.Errorf("DashScope requires a Qianwen API-key account")
	}
	var path string
	switch operation {
	case QianwenNativeText:
		path = "/services/aigc/text-generation/generation"
	case QianwenNativeMultimodal:
		path = "/services/aigc/multimodal-generation/generation"
	case QianwenNativeEmbedding:
		path = "/services/embeddings/text-embedding/text-embedding"
	case QianwenNativeRerank:
		path = "/services/rerank/text-rerank/text-rerank"
	case QianwenNativeVoiceEnrollment:
		path = "/services/audio/tts/customization"
	default:
		return nil, fmt.Errorf("unsupported DashScope operation: %s", operation)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("invalid DashScope JSON: %w", err)
	}
	var model string
	if err := json.Unmarshal(payload["model"], &model); err != nil || strings.TrimSpace(model) == "" {
		return nil, fmt.Errorf("DashScope model is required")
	}
	if err := ValidateQianwenNativeOperation(operation, account.GetMappedModel(model), body, stream); err != nil {
		return nil, err
	}
	if mapped := account.GetMappedModel(model); mapped != model {
		payload["model"], _ = json.Marshal(mapped)
		body, _ = json.Marshal(payload)
	}
	key := strings.TrimSpace(account.GetCNAPIKey())
	if key == "" {
		return nil, fmt.Errorf("Qianwen API key is required")
	}
	base := DefaultQianwenDashScopeBaseURL
	if bases, ok := account.Credentials["api_base_urls"].(map[string]any); ok {
		if override, ok := bases["dashscope"].(string); ok && strings.TrimSpace(override) != "" {
			base = strings.TrimSpace(override)
		}
	}
	base, err := s.validateUpstreamBaseURL(base)
	if err != nil {
		return nil, fmt.Errorf("invalid DashScope base URL: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if stream {
		req.Header.Set("Accept", "text/event-stream")
		req.Header.Set("X-DashScope-SSE", "enable")
	}
	account.ApplyHeaderOverrides(req.Header)
	applyQianwenNativeAudioHeaders(req, account.GetMappedModel(model))
	// The supported native lifecycle never creates unowned asynchronous jobs.
	req.Header.Del("X-DashScope-Async")
	if IsQianwenNativeImageModel(account.GetMappedModel(model)) {
		req.Header.Del("X-DashScope-SSE")
		req.Header.Set("Accept", "application/json")
	}
	return req, nil
}

// DoQianwenNativeRequest preserves the native wire format (including error
// bodies and usage) and the configured proxy. The caller must close Body.
func (s *OpenAIGatewayService) DoQianwenNativeRequest(ctx context.Context, account *Account, operation QianwenNativeOperation, body []byte, stream bool) (*http.Response, error) {
	req, err := s.BuildQianwenNativeRequest(ctx, account, operation, body, stream)
	if err != nil {
		return nil, err
	}
	return s.doOpenAIUpstream(req, upstreamModelsProxyURL(account), account)
}
