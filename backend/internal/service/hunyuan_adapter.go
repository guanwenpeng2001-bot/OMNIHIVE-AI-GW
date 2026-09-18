package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

const hunyuanCompatibilityBaseURL = "https://api.hunyuan.cloud.tencent.com/v1"

var (
	errHunyuanDiscoveryUnsupported = errors.New("hunyuan model discovery is unverified; use explicit model availability declarations")
	errHunyuanStreamUnsupported    = errors.New("hunyuan compatibility streaming contract is unverified")
)

// buildHunyuanChatRequest implements the legacy compatibility contract frozen in
// platform-contracts.json (2026-09-19), sourced from Tencent document 1729/111007.
// It does not authorize the target: the caller must first enforce the existing
// account/model/endpoint eligibility gate. Send through the existing HTTP upstream
// client with its proxy and account concurrency policy; do not create a new client.
// TokenHub and native TC3 credentials are deliberately not accepted here.
func buildHunyuanChatRequest(ctx context.Context, account *Account, body []byte, upstreamModel string) (*http.Request, error) {
	if account == nil || account.Platform != "hunyuan" || account.Type != AccountTypeAPIKey {
		return nil, errors.New("hunyuan compatibility requires a hunyuan API key account")
	}
	baseURL := strings.TrimRight(strings.TrimSpace(account.GetCredential("base_url")), "/")
	if baseURL != "" && baseURL != hunyuanCompatibilityBaseURL && baseURL != strings.TrimSuffix(hunyuanCompatibilityBaseURL, "/v1") {
		return nil, errors.New("hunyuan compatibility base URL must use the confirmed legacy endpoint")
	}
	apiKey := strings.TrimSpace(account.GetCredential("api_key"))
	if apiKey == "" || strings.ContainsAny(apiKey, "\r\n") {
		return nil, errors.New("hunyuan compatibility API key is missing or invalid")
	}
	var request struct {
		Model    string            `json:"model"`
		Messages []json.RawMessage `json:"messages"`
		Stream   bool              `json:"stream"`
	}
	if err := json.Unmarshal(body, &request); err != nil {
		return nil, errors.New("invalid hunyuan chat request")
	}
	if request.Stream {
		return nil, errHunyuanStreamUnsupported
	}
	if strings.TrimSpace(request.Model) == "" || len(request.Messages) == 0 || strings.TrimSpace(upstreamModel) == "" {
		return nil, errors.New("hunyuan chat requires model, messages and an eligible upstream model")
	}
	// Preserve reasoning, tools, tool_call_id, and unknown JSON fields. Do not
	// canonicalize the explicitly selected model or convert through Responses.
	upstreamBody := bytes.Clone(body)
	if request.Model != upstreamModel {
		upstreamBody = ReplaceModelInBody(upstreamBody, upstreamModel)
	}
	upstreamRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, hunyuanCompatibilityBaseURL+"/chat/completions", bytes.NewReader(upstreamBody))
	if err != nil {
		return nil, fmt.Errorf("build hunyuan chat request: %w", err)
	}
	upstreamRequest.Header.Set("Authorization", "Bearer "+apiKey)
	upstreamRequest.Header.Set("Content-Type", "application/json")
	upstreamRequest.Header.Set("Accept", "application/json")
	return upstreamRequest, nil
}

// hunyuanDiscoveredModels must not return an empty successful snapshot: that
// would erase previously confirmed discovery and misrepresent unknown as empty.
func hunyuanDiscoveredModels() ([]string, error) {
	return nil, errHunyuanDiscoveryUnsupported
}

// parseHunyuanChatUsage reads only documented non-stream token counters. The
// caller forwards the original response bytes, retaining reasoning/content and
// tool_calls without conflation. Missing usage is unknown, never free/unlimited.
// Cache and reasoning detail counters remain raw, not invented billing inputs.
func parseHunyuanChatUsage(body []byte) (*OpenAIUsage, error) {
	var response struct {
		Usage *struct {
			PromptTokens     *int `json:"prompt_tokens"`
			CompletionTokens *int `json:"completion_tokens"`
			TotalTokens      *int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, errors.New("invalid hunyuan chat usage response")
	}
	if response.Usage == nil {
		return nil, nil
	}
	usage := response.Usage
	if usage.PromptTokens == nil || usage.CompletionTokens == nil || usage.TotalTokens == nil {
		return nil, nil
	}
	if *usage.PromptTokens < 0 || *usage.CompletionTokens < 0 || *usage.TotalTokens < 0 {
		return nil, errors.New("invalid negative hunyuan token count")
	}
	return &OpenAIUsage{InputTokens: *usage.PromptTokens, OutputTokens: *usage.CompletionTokens}, nil
}

// hunyuanHTTPErrorScope is a conservative HTTP scope, not a vendor error-code
// decoder. None of these results proves model absence, balance, or permanent
// account revocation. In particular 404 is request-scoped and 429 must not remove
// a model or mark an account unlimited/exhausted based on undocumented JSON.
func hunyuanHTTPErrorScope(statusCode int) string {
	switch statusCode {
	case http.StatusUnauthorized:
		return "account_authentication"
	case http.StatusTooManyRequests:
		return "rate_limit"
	default:
		return "request"
	}
}
