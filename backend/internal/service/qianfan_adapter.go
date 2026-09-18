package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const qianfanDefaultBaseURL = "https://qianfan.baidubce.com/v2"

// buildQianfanRequest only builds domestic API-key models/chat requests. The
// caller supplies the existing operator URL policy and HTTPUpstream transport;
// this adapter neither registers a platform nor grants model eligibility.
func buildQianfanRequest(ctx context.Context, account *Account, endpoint string, body []byte, validateBaseURL func(string) (string, error)) (*http.Request, error) {
	if account == nil || account.Type != AccountTypeAPIKey {
		return nil, newUpstreamModelSyncUnsupportedError("Qianfan requires an API-key account", nil)
	}
	method := http.MethodGet
	switch endpoint {
	case "models":
		if len(body) != 0 {
			return nil, errors.New("Qianfan models request must not have a body")
		}
	case "chat/completions":
		method = http.MethodPost
		var request struct {
			Model    string            `json:"model"`
			Messages []json.RawMessage `json:"messages"`
		}
		if json.Unmarshal(body, &request) != nil || strings.TrimSpace(request.Model) == "" || len(request.Messages) == 0 {
			return nil, errors.New("Qianfan chat requires model and messages")
		}
	default:
		return nil, newUpstreamModelSyncUnsupportedError("Unsupported Qianfan endpoint", nil)
	}
	apiKey := strings.TrimSpace(account.GetCredential("api_key"))
	if apiKey == "" || strings.ContainsAny(apiKey, "\r\n") {
		return nil, newUpstreamModelSyncConfigError("Qianfan API key is missing or invalid", nil)
	}
	baseURL := strings.TrimSpace(account.GetCredential("base_url"))
	if baseURL == "" {
		baseURL = qianfanDefaultBaseURL
	}
	if validateBaseURL == nil {
		return nil, newUpstreamModelSyncConfigError("Qianfan URL policy is required", nil)
	}
	validated, err := validateBaseURL(baseURL)
	if err != nil {
		return nil, newUpstreamModelSyncConfigError("Qianfan base URL rejected by URL policy", nil)
	}
	parsed, err := url.Parse(validated)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.RawPath != "" {
		return nil, newUpstreamModelSyncConfigError("Invalid Qianfan base URL", nil)
	}
	// A custom relay may have a prefix, but the domestic version is always v2.
	basePath := strings.TrimRight(parsed.Path, "/")
	versionSegments := 0
	for _, segment := range strings.Split(basePath, "/") {
		if segment == "v2" {
			versionSegments++
		}
		if segment == "." || segment == ".." || (strings.HasPrefix(segment, "v") && len(segment) > 1 && segment[1] >= '0' && segment[1] <= '9' && segment != "v2") {
			return nil, newUpstreamModelSyncConfigError("Invalid Qianfan API version path", nil)
		}
	}
	if versionSegments > 1 || (versionSegments == 1 && !strings.HasSuffix(basePath, "/v2")) {
		return nil, newUpstreamModelSyncConfigError("Invalid Qianfan API version path", nil)
	}
	if !strings.HasSuffix(basePath, "/v2") {
		basePath += "/v2"
	}
	parsed.Path = basePath + "/" + endpoint
	req, err := http.NewRequestWithContext(ctx, method, parsed.String(), bytes.NewReader(body))
	if err != nil {
		return nil, newUpstreamModelSyncConfigError("Invalid Qianfan request", nil)
	}
	account.ApplyHeaderOverrides(req.Header)
	// Freeze authentication and content negotiation after custom headers.
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
		var options struct {
			Stream bool `json:"stream"`
		}
		_ = json.Unmarshal(body, &options)
		if options.Stream {
			req.Header.Set("Accept", "text/event-stream")
		}
	}
	return req, nil
}

// Raw retains absent/null/false/empty and future rich fields without converting
// provider metadata or pricing into permission or billing configuration.
type qianfanModel struct {
	ID                   string          `json:"id"`
	Object               string          `json:"object"`
	OwnedBy              string          `json:"owned_by"`
	Created              *int64          `json:"created"`
	Type                 string          `json:"type"`
	ContextLength        *int64          `json:"context_length"`
	MaxCompletionsTokens *int64          `json:"max_completions_tokens"`
	MaxTokens            *int64          `json:"max_tokens"`
	PromptTokens         *int64          `json:"prompt_tokens"`
	Architecture         json.RawMessage `json:"architecture"`
	Raw                  json.RawMessage `json:"-"`
}

type qianfanModelList struct {
	Models   []qianfanModel
	Complete bool
}

// parseQianfanModelsResponse leaves body ownership with the HTTP caller. Any
// error, including a partially malformed list, returns no replacement snapshot.
func parseQianfanModelsResponse(response *http.Response) (*qianfanModelList, error) {
	if response == nil || response.Body == nil {
		return nil, newUpstreamModelSyncUpstreamError("Missing Qianfan models response", nil)
	}
	if response.StatusCode != http.StatusOK {
		return nil, newUpstreamModelSyncUpstreamError(fmt.Sprintf("Qianfan models HTTP status %d", response.StatusCode), nil)
	}
	body, err := readUpstreamResponseBodyLimited(response.Body, upstreamModelsBodyLimit)
	if err != nil {
		return nil, newUpstreamModelSyncUpstreamError("Cannot read Qianfan models response", err)
	}
	var envelope struct {
		Object     string            `json:"object"`
		Data       []json.RawMessage `json:"data"`
		Error      json.RawMessage   `json:"error"`
		HasMore    bool              `json:"has_more"`
		Next       json.RawMessage   `json:"next"`
		NextCursor json.RawMessage   `json:"next_cursor"`
	}
	if json.Unmarshal(body, &envelope) != nil || envelope.Object != "list" || envelope.Data == nil || qianfanHasValue(envelope.Error) {
		return nil, newUpstreamModelSyncUpstreamError("Invalid Qianfan models envelope", nil)
	}
	// Pagination is not part of the frozen single-list contract. Never accept a
	// visible continuation as a complete replacement or guess a cursor request.
	if envelope.HasMore || qianfanHasValue(envelope.Next) || qianfanHasValue(envelope.NextCursor) {
		return nil, newUpstreamModelSyncUpstreamError("Incomplete Qianfan models response: unsupported pagination", nil)
	}
	result := &qianfanModelList{Models: make([]qianfanModel, 0, len(envelope.Data)), Complete: true}
	seen := make(map[string]bool, len(envelope.Data))
	for index, raw := range envelope.Data {
		var model qianfanModel
		if json.Unmarshal(raw, &model) != nil || strings.TrimSpace(model.ID) == "" || seen[model.ID] {
			return nil, newUpstreamModelSyncUpstreamError(fmt.Sprintf("Invalid Qianfan model at index %d; previous snapshot must be retained", index), nil)
		}
		seen[model.ID] = true
		model.Raw = append(json.RawMessage(nil), raw...)
		result.Models = append(result.Models, model)
	}
	return result, nil
}

func qianfanHasValue(value json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(value))
	return trimmed != "" && trimmed != "null" && trimmed != `""`
}
