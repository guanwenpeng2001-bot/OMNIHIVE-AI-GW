package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/tidwall/sjson"
)

const mistralDefaultBaseURL = "https://api.mistral.ai/v1"

// parseMistralModelsResponse shares discovery error classification with existing
// platforms. In particular a list-only 403 or a 429 is not model revocation.
func parseMistralModelsResponse(status int, body []byte) ([]MistralModelCard, error) {
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return nil, newUpstreamModelDiscoveryHTTPError(status, body)
	}
	if int64(len(body)) > upstreamModelsBodyLimit {
		return nil, newUpstreamModelSyncUpstreamError("Mistral model response is too large", nil)
	}
	cards, err := parseMistralModelCards(body)
	if err != nil {
		return nil, newUpstreamModelSyncUpstreamError("Invalid Mistral model response", err)
	}
	return cards, nil
}

// MistralModelCard preserves provider facts separately from discovery/eligibility.
// In particular, aliases, root and archived do not authorize a different ID or
// supply a fine-tuned model's price. Nil fields mean unknown, not false/empty.
type MistralModelCard struct {
	ID               string           `json:"id"`
	Name             string           `json:"name,omitempty"`
	Description      string           `json:"description,omitempty"`
	Type             string           `json:"type,omitempty"`
	Root             string           `json:"root,omitempty"`
	Aliases          []string         `json:"aliases"`
	Archived         *bool            `json:"archived,omitempty"`
	MaxContextLength *int64           `json:"max_context_length,omitempty"`
	Capabilities     map[string]*bool `json:"capabilities,omitempty"`
}

// parseMistralModelCards accepts the documented data envelope and the top-level
// array shown in the official example. Invalid/partial replies are errors, never
// successful empty snapshots. The caller retains its previous snapshot on error.
func parseMistralModelCards(body []byte) ([]MistralModelCard, error) {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return nil, fmt.Errorf("empty Mistral model response")
	}
	if body[0] == '{' {
		var envelope map[string]json.RawMessage
		if err := json.Unmarshal(body, &envelope); err != nil {
			return nil, fmt.Errorf("invalid Mistral model envelope")
		}
		for _, key := range []string{"error", "message", "detail"} {
			if value, present := envelope[key]; present && !bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				return nil, fmt.Errorf("Mistral model response contains an error")
			}
		}
		body = bytes.TrimSpace(envelope["data"])
	}
	if len(body) == 0 || body[0] != '[' {
		return nil, fmt.Errorf("Mistral model response must contain an array")
	}
	var cards []MistralModelCard
	if err := json.Unmarshal(body, &cards); err != nil {
		return nil, fmt.Errorf("invalid Mistral model cards")
	}
	seen := make(map[string]struct{}, len(cards))
	for _, card := range cards {
		if card.ID == "" || card.ID != strings.TrimSpace(card.ID) {
			return nil, fmt.Errorf("Mistral model card has an invalid ID")
		}
		if _, exists := seen[card.ID]; exists {
			return nil, fmt.Errorf("Mistral model response has duplicate IDs")
		}
		seen[card.ID] = struct{}{}
		if card.MaxContextLength != nil && *card.MaxContextLength <= 0 {
			return nil, fmt.Errorf("Mistral model card has an invalid context length")
		}
	}
	return cards, nil
}

// metadata overlays native facts on same-identity historical metadata using the
// existing merge policy. Callers must not pass another account/profile's facts.
func (card MistralModelCard) metadata(previous UpstreamModelMetadata) UpstreamModelMetadata {
	current := UpstreamModelMetadata{
		ID: card.ID, ProviderProfile: "mistral", DisplayName: card.Name,
		Description:             card.Description,
		SupportsFunctionCalling: card.Capabilities["function_calling"],
		SupportsVision:          card.Capabilities["vision"],
	}
	if card.MaxContextLength != nil {
		current.ContextWindow = *card.MaxContextLength
		current.MaxContextWindow = *card.MaxContextLength
		current.ContextLimitKind = "total"
	}
	if chat := card.Capabilities["completion_chat"]; chat != nil {
		current.SupportedMethods = []string{}
		if *chat {
			current.TaskType = "text_generation"
			current.SupportedMethods = []string{"chat_completions"}
		}
	}
	if previous.ID == card.ID && previous.ProviderProfile == "mistral" {
		current, _ = mergeUpstreamModelMetadata(current, previous)
	}
	return current
}

// buildMistralModelsRequest uses the same URL validation and header override
// policy as the shared model fetcher. Send through doUpstreamModelsRequest so
// proxy, TLS profile, concurrency and context cancellation remain centralized.
func (service *AccountTestService) buildMistralModelsRequest(ctx context.Context, account *Account) (*http.Request, error) {
	if account == nil || account.Type != AccountTypeAPIKey {
		return nil, newUpstreamModelSyncUnsupportedError("Mistral requires an API key account", nil)
	}
	apiKey := strings.TrimSpace(account.GetCredential("api_key"))
	if apiKey == "" {
		return nil, newUpstreamModelSyncConfigError("Mistral API key is missing", nil)
	}
	baseURL := strings.TrimSpace(account.GetCredential("base_url"))
	if baseURL == "" {
		baseURL = mistralDefaultBaseURL
	}
	baseURL, err := service.validateUpstreamBaseURL(baseURL)
	if err != nil {
		return nil, newUpstreamModelSyncConfigError("Invalid Mistral base URL", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, buildOpenAIModelsURL(baseURL), nil)
	if err != nil {
		return nil, newUpstreamModelSyncConfigError("Invalid Mistral model URL", err)
	}
	request.Header.Set("Authorization", "Bearer "+apiKey)
	request.Header.Set("Accept", "application/json")
	account.ApplyHeaderOverrides(request.Header)
	return request, nil
}

// prepareMistralChatRequest is called after shared raw-chat request transforms,
// immediately before request construction. Mistral documents stream, but not
// OpenAI's stream_options; usage is read from the response when actually present.
// The existing raw-chat pipeline owns forwarding, SSE, errors and cancellation.
func prepareMistralChatRequest(body []byte) ([]byte, error) {
	var request struct {
		Model    string            `json:"model"`
		Messages []json.RawMessage `json:"messages"`
		Tools    []struct {
			Type string `json:"type"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(body, &request); err != nil || request.Model == "" || len(request.Messages) == 0 {
		return nil, fmt.Errorf("invalid Mistral chat request")
	}
	for _, tool := range request.Tools {
		if tool.Type != "function" {
			return nil, fmt.Errorf("Mistral chat supports only function tools")
		}
	}
	return sjson.DeleteBytes(body, "stream_options")
}
