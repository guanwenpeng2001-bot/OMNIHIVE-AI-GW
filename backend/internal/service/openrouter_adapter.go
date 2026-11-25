package service

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/capabilitysources"
)

const openRouterDefaultBaseURL = "https://openrouter.ai/api/v1"

func (s *AccountTestService) buildOpenRouterModelsRequest(ctx context.Context, account *Account) (*http.Request, error) {
	request, err := buildOpenAIAPIKeyModelsRequest(ctx, account, s.validateUpstreamBaseURL)
	if err != nil {
		return nil, err
	}
	request.URL.Path = strings.TrimRight(request.URL.Path, "/") + "/user"
	query := request.URL.Query()
	query.Set("output_modalities", "all")
	request.URL.RawQuery = query.Encode()
	return request, nil
}

// Reuse the capability parser only for metadata, after authenticated discovery
// has independently established identity. Optional malformed fields stay unknown.
func openRouterModelMetadata(raw []byte, entry UpstreamModelMetadata) UpstreamModelMetadata {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return entry
	}
	clean := map[string]any{"id": entry.ID}
	var contextLength int64
	var params []string
	var architecture map[string]json.RawMessage
	if json.Unmarshal(fields["context_length"], &contextLength) == nil && contextLength >= 0 {
		clean["context_length"] = contextLength
	}
	if json.Unmarshal(fields["supported_parameters"], &params) == nil && params != nil {
		clean["supported_parameters"] = params
		entry.SupportedParameters = params
	}
	if json.Unmarshal(fields["architecture"], &architecture) == nil {
		modalities := map[string]any{}
		for _, field := range []string{"input_modalities", "output_modalities"} {
			var values []string
			if json.Unmarshal(architecture[field], &values) == nil && values != nil {
				modalities[field] = values
			}
		}
		clean["architecture"] = modalities
	}
	body, _ := json.Marshal(map[string]any{"data": []any{clean}})
	if observation, err := capabilitysources.Parse("openrouter", body, time.Now().UTC().Format(time.DateOnly)); err == nil {
		if facts, ok := capabilityObservationIndex(observation)[capabilityKey("openrouter", entry.ID)]; ok {
			entry.InputModalities, entry.OutputModalities = facts.InputModalities, facts.OutputModalities
			entry.ContextWindow, entry.MaxContextWindow, entry.ContextLimitKind = facts.ContextWindow, facts.ContextWindow, facts.ContextLimitKind
		}
	}
	var provider map[string]json.RawMessage
	if json.Unmarshal(fields["top_provider"], &provider) == nil {
		var limit int64
		if json.Unmarshal(provider["max_completion_tokens"], &limit) == nil && limit >= 0 {
			entry.MaxOutputTokens = limit
		}
	}
	// Only an explicitly published list is an effort enum; mandatory/boolean isn't.
	var levels []string
	if json.Unmarshal(fields["supported_reasoning_levels"], &levels) == nil {
		entry.SupportedReasoningLevels = levels
	}
	entry.ProviderProfile = PlatformOpenRouter
	freeByID := strings.HasSuffix(strings.ToLower(strings.TrimSpace(entry.ID)), ":free")
	freeByPricing, nonzeroPricing := openRouterZeroPricing(fields["pricing"])
	free := freeByPricing || (freeByID && !nonzeroPricing)
	entry.Free = &free
	return entry
}

func openRouterZeroPricing(raw json.RawMessage) (allZero, nonzero bool) {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	var pricing map[string]any
	if decoder.Decode(&pricing) != nil || len(pricing) == 0 {
		return false, false
	}
	_, allZero = zenCostVerdict(pricing, 0)
	return allZero, !allZero
}
