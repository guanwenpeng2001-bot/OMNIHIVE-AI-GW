package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// Standard-ingress image generation bridge for the qwen-image-3.0 family on
// DashScope compatible mode: POST {compatible-base}/images/generations with the
// flat OpenAI images shape. Verified against the official Qwen Image
// Generation and Editing 3.0 API reference, 2026-09-22: synchronous,
// non-streaming, n 1-6, size "WxH"; usage carries output_image_count and
// output_width/output_height. The upstream has no /images/edits endpoint and
// ignores response_format (URLs are always returned).
func IsQianwenCompatibleImageModel(model string) bool {
	return model == "qwen-image-3.0" || model == "qwen-image-3.0-pro"
}

func ValidateQianwenCompatibleImageBody(body []byte) (string, error) {
	if !gjson.ValidBytes(body) {
		return "", fmt.Errorf("invalid image generation JSON")
	}
	model := gjson.GetBytes(body, "model").String()
	if !IsQianwenCompatibleImageModel(model) {
		return "", fmt.Errorf("unsupported DashScope compatible image model: %s", model)
	}
	prompt := gjson.GetBytes(body, "prompt")
	if prompt.Type != gjson.String || strings.TrimSpace(prompt.String()) == "" || len([]rune(prompt.String())) > 4000 {
		return "", fmt.Errorf("image generation requires a non-empty prompt")
	}
	if n := gjson.GetBytes(body, "n"); n.Exists() && (n.Type != gjson.Number || n.Float() != float64(n.Int()) || n.Int() < 1 || n.Int() > 6) {
		return "", fmt.Errorf("DashScope image generation supports n between 1 and 6")
	}
	if size := gjson.GetBytes(body, "size"); size.Exists() {
		parts := strings.Split(size.String(), "x")
		if len(parts) != 2 {
			return "", fmt.Errorf("DashScope image size must be WxH")
		}
		for _, part := range parts {
			v, err := strconv.Atoi(part)
			if err != nil || v <= 0 {
				return "", fmt.Errorf("DashScope image size must be WxH")
			}
		}
	}
	if gjson.GetBytes(body, "stream").Bool() {
		return "", fmt.Errorf("DashScope image generation does not support streaming")
	}
	return model, nil
}

func (s *OpenAIGatewayService) ForwardQianwenImages(ctx context.Context, c *gin.Context, account *Account, body []byte) (*OpenAIForwardResult, error) {
	started := time.Now()
	clientCtx := ctx
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	model, err := ValidateQianwenCompatibleImageBody(body)
	if err != nil {
		writeChatCompletionsError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return nil, err
	}
	mapped := account.GetMappedModel(model)
	if !IsQianwenCompatibleImageModel(mapped) {
		err := fmt.Errorf("unsupported DashScope compatible image model: %s", mapped)
		writeChatCompletionsError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return nil, err
	}
	key := strings.TrimSpace(account.GetCNAPIKey())
	if key == "" {
		return nil, fmt.Errorf("Qianwen API key is required")
	}
	base := account.GetOpenAIFormatBaseURL()
	if base == "" {
		base = DefaultQianwenBaseURL
	}
	base, err = s.validateUpstreamBaseURL(base)
	if err != nil {
		return nil, fmt.Errorf("invalid DashScope base URL: %w", err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("invalid image generation JSON")
	}
	if mapped != model {
		payload["model"], _ = json.Marshal(mapped)
		body, _ = json.Marshal(payload)
	}
	targetURL := buildOpenAIEndpointURL(base, "/v1/images/generations")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	account.ApplyHeaderOverrides(req.Header)
	req.Header.Del("X-DashScope-Async")
	req.Header.Del("X-DashScope-SSE")
	resp, err := s.doOpenAIUpstream(req, upstreamModelsProxyURL(account), account)
	if err != nil {
		return nil, s.handleOpenAIUpstreamTransportError(ctx, c, account, err, false)
	}
	defer func() { cancel(); _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return s.handleChatCompletionsErrorResponse(resp, c, account, model)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, qianwenNativeMaxFrame+1))
	if err != nil {
		return nil, err
	}
	if len(data) > qianwenNativeMaxFrame {
		return nil, fmt.Errorf("DashScope image response exceeds size limit")
	}
	result := &OpenAIForwardResult{Model: model, BillingModel: mapped, UpstreamModel: mapped, UpstreamEndpoint: "/images/generations", UpstreamHeaders: resp.Header.Clone(), RequestID: resp.Header.Get("X-Request-Id")}
	if err := observeQianwenCompatibleImages(data, body, result); err != nil {
		return nil, err
	}
	c.Data(resp.StatusCode, "application/json", data)
	result.Duration = time.Since(started)
	result.ClientDisconnect = clientCtx.Err() != nil
	return result, nil
}

// observeQianwenCompatibleImages counts successful outputs, never the
// requested n, and cross-checks the documented usage.output_image_count.
func observeQianwenCompatibleImages(body, request []byte, result *OpenAIForwardResult) error {
	if !gjson.ValidBytes(body) {
		return fmt.Errorf("invalid DashScope image response JSON")
	}
	if code := gjson.GetBytes(body, "code").String(); code != "" {
		return fmt.Errorf("DashScope image response error: %s", code)
	}
	if gjson.GetBytes(body, "output.task_id").Exists() {
		return fmt.Errorf("unexpected asynchronous DashScope image task")
	}
	count := 0
	for _, item := range gjson.GetBytes(body, "data").Array() {
		if url := item.Get("url"); url.Type == gjson.String && strings.TrimSpace(url.String()) != "" {
			count++
		}
	}
	if count == 0 {
		return fmt.Errorf("DashScope image response has no successful image outputs")
	}
	usageCount := gjson.GetBytes(body, "usage.output_image_count")
	if usageCount.Exists() && (usageCount.Type != gjson.Number || usageCount.Float() != float64(count)) {
		return fmt.Errorf("DashScope image usage does not match successful output count")
	}
	result.ImageCount = count
	width, height := gjson.GetBytes(body, "usage.output_width").Int(), gjson.GetBytes(body, "usage.output_height").Int()
	if width > 0 && height > 0 {
		for i := 0; i < count; i++ {
			result.ImageOutputSizes = append(result.ImageOutputSizes, fmt.Sprintf("%dx%d", width, height))
		}
	}
	result.ImageInputSize = gjson.GetBytes(request, "size").String()
	ApplyOpenAIImageBillingResolution(result)
	return nil
}
