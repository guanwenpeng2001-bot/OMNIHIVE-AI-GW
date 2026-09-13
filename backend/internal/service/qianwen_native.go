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
	QianwenNativeText       QianwenNativeOperation = "text_generation"
	QianwenNativeMultimodal QianwenNativeOperation = "multimodal_generation"
)

// BuildQianwenNativeRequest implements the verified DashScope conversation
// endpoints. Other native families deliberately fail closed: video/image jobs,
// speech, realtime and agents require their own lifecycle and billing adapters.
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
