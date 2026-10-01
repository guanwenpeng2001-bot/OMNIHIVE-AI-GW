package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Default new accounts to Guangzhou TokenHub; stored legacy URLs remain readable.
const hunyuanCompatibilityBaseURL = "https://tokenhub.tencentmaas.com/v1"
const hunyuanLegacyBaseURL = "https://api.hunyuan.cloud.tencent.com/v1"

// TokenHub 套餐（Coding Plan / Token Plan）的专用官方站点。腾讯云为套餐发布了独立 Base URL
// 与专属 API Key（sk-sp- / sk-tp- 前缀与按量 Key 互不通用），因此 base_url 为空时按
// account_mode 选默认值；凭据里的显式 base_url 始终优先（用户填了就用他的）。
const (
	hunyuanTokenPlanBaseURL = "https://api.lkeap.cloud.tencent.com/plan/v3"
)

// hunyuanActionBaseURL resolves the account v1 base URL shared by chat and the
// native TokenHub action families. Accounts that stored the bare legacy host
// keep resolving to the documented legacy v1 path; every other value must be
// one of the official TokenHub sites (or a documented plan site) so a custom
// URL cannot silently redirect vendor credentials to an unverified host.
func hunyuanActionBaseURL(account *Account) (string, error) {
	if account == nil || account.Platform != PlatformHunyuan || account.Type != AccountTypeAPIKey {
		return "", errors.New("hunyuan requires a hunyuan API key account")
	}
	baseURL := strings.TrimSuffix(strings.TrimSpace(account.GetCredential("base_url")), "/")
	switch baseURL {
	case "":
		baseURL = hunyuanCompatibilityBaseURL
		if account.GetAccountMode() == AccountModeCoding {
			baseURL = DefaultHunyuanCodingBaseURL
		}
	case "https://api.hunyuan.cloud.tencent.com":
		baseURL = hunyuanLegacyBaseURL
	case hunyuanCompatibilityBaseURL, "https://tokenhub.tencentmaas.cn/v1",
		"https://tokenhub-intl.tencentmaas.com/v1", "https://tokenhub-intl.tencentmaas.cn/v1", hunyuanLegacyBaseURL,
		DefaultHunyuanCodingBaseURL, DefaultHunyuanCodingAnthropicBaseURL, hunyuanTokenPlanBaseURL:
	default:
		return "", errors.New("hunyuan base URL must use an official TokenHub, Coding Plan or Token Plan endpoint")
	}
	return baseURL, nil
}

func hunyuanAccountEndpoint(account *Account) (string, string, error) {
	baseURL, err := hunyuanActionBaseURL(account)
	if err != nil {
		return "", "", err
	}
	apiKey := strings.TrimSpace(account.GetCredential("api_key"))
	if apiKey == "" || strings.ContainsAny(apiKey, "\r\n") {
		return "", "", errors.New("hunyuan API key is missing or invalid")
	}
	return baseURL, apiKey, nil
}

func buildHunyuanModelsRequest(ctx context.Context, account *Account) (*http.Request, error) {
	baseURL, apiKey, err := hunyuanAccountEndpoint(account)
	if err != nil {
		return nil, newUpstreamModelSyncConfigError("Invalid hunyuan discovery configuration", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+apiKey)
	request.Header.Set("Accept", "application/json")
	return request, nil
}

// fetchHunyuanModels uses the account discovery transport and its redirect,
// proxy, timeout and concurrency policies. The shared discovery entry point
// must dispatch here after its account validation, before candidate fallback.
//
//nolint:unused // referenced only from //go:build unit tests, so the default lint pass cannot see its callers.
func (s *AccountTestService) fetchHunyuanModels(ctx context.Context, account *Account) ([]string, []byte, error) {
	request, err := buildHunyuanModelsRequest(ctx, account)
	if err != nil {
		return nil, nil, err
	}
	if s.httpUpstream == nil {
		return nil, nil, newUpstreamModelSyncConfigError("Upstream HTTP client is not configured", nil)
	}
	accountProxyURL, egressErr := account.EgressProxyURL(ctx)
	if egressErr != nil {
		return nil, nil, egressErr
	}
	response, err := s.doUpstreamModelsRequest(request, accountProxyURL, account)
	if err != nil || response == nil || response.Body == nil {
		return nil, nil, newUpstreamModelSyncUpstreamError("Failed to request TokenHub models", err)
	}
	defer func() { _ = response.Body.Close() }()
	limit := resolveModelsListReadLimit(s.cfg)
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || int64(len(body)) > limit {
		return nil, nil, newUpstreamModelSyncUpstreamError("TokenHub model list is unreadable or exceeds the size limit", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, nil, newUpstreamModelDiscoveryHTTPError(response.StatusCode, body)
	}
	models, err := hunyuanDiscoveredModels(body)
	if err != nil {
		return nil, nil, err
	}
	// Keep metadata for online entries only, preserving vendor fields verbatim.
	var catalog struct {
		Data []json.RawMessage `json:"data"`
	}
	_ = json.Unmarshal(body, &catalog)
	onlineEntries := make([]json.RawMessage, 0, len(models))
	for _, entry := range catalog.Data {
		var model struct {
			Status string `json:"status"`
		}
		_ = json.Unmarshal(entry, &model)
		if model.Status == "online" {
			onlineEntries = append(onlineEntries, entry)
		}
	}
	catalog.Data = onlineEntries
	filteredBody, err := json.Marshal(catalog)
	return models, filteredBody, err
}

// buildHunyuanChatRequest follows TokenHub documents 1823/130079 and 130058.
// The caller enforces eligibility and sends through the shared upstream client
// with its existing proxy, timeout and account concurrency policy.
func buildHunyuanChatRequest(ctx context.Context, account *Account, body []byte, upstreamModel string) (*http.Request, error) {
	baseURL, apiKey, err := hunyuanAccountEndpoint(account)
	if err != nil {
		return nil, err
	}
	var request struct {
		Model    string            `json:"model"`
		Messages []json.RawMessage `json:"messages"`
		Stream   bool              `json:"stream"`
	}
	if err := json.Unmarshal(body, &request); err != nil {
		return nil, errors.New("invalid hunyuan chat request")
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
	upstreamRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(upstreamBody))
	if err != nil {
		return nil, fmt.Errorf("build hunyuan chat request: %w", err)
	}
	upstreamRequest.Header.Set("Authorization", "Bearer "+apiKey)
	upstreamRequest.Header.Set("Content-Type", "application/json")
	upstreamRequest.Header.Set("Accept", "application/json")
	if request.Stream {
		upstreamRequest.Header.Set("Accept", "text/event-stream")
	}
	return upstreamRequest, nil
}

// hunyuanDiscoveredModels rejects empty/unknown snapshots so failed discovery
// cannot erase previously confirmed availability or fall back to old presets.
//
//nolint:unused // referenced only from //go:build unit tests, so the default lint pass cannot see its callers.
func hunyuanDiscoveredModels(body []byte) ([]string, error) {
	var response struct {
		Data []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, newUpstreamModelSyncUpstreamError("Invalid TokenHub model list", err)
	}
	models := []string{}
	for _, model := range response.Data {
		if strings.TrimSpace(model.ID) == "" || model.Status == "" {
			return nil, newUpstreamModelSyncUpstreamError("TokenHub model identity or status is missing", nil)
		}
		if model.Status == "online" {
			models = append(models, model.ID)
		}
	}
	if len(models) == 0 {
		return nil, newUpstreamModelSyncUpstreamError("TokenHub returned no online models; previous discovery retained", nil)
	}
	return dedupeAndSortModelIDs(models), nil
}

// parseHunyuanChatUsage reads documented response or SSE tail token counters. The
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
