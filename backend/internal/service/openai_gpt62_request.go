package service

import (
	"strings"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/openai"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func openAIGPT62RequestModel(account *Account, model string) string {
	if account == nil || !account.IsOpenAI() {
		return ""
	}
	return openai.GPT62ModelID(model)
}

// Effort aliases are intentionally unavailable until every native transport can
// carry the derived effort on the wire and in the receipt consistently.
func rejectOpenAIGPT62Alias(account *Account, model string) *OpenAIFastBlockedError {
	if base := openAIGPT62RequestModel(account, model); base != "" && strings.TrimSpace(model) != base {
		return &OpenAIFastBlockedError{Message: "GPT-6.2 model aliases are not supported; use the published model ID and an explicit reasoning effort"}
	}
	return nil
}

func validateOpenAIGPT62RequestBody(account *Account, model string, body []byte) *OpenAIFastBlockedError {
	if openAIGPT62RequestModel(account, model) == "" {
		return nil
	}
	if err := rejectOpenAIGPT62Alias(account, model); err != nil {
		return err
	}
	for _, path := range []string{"reasoning.effort", "reasoning_effort"} {
		if err := openai.ValidateGPT62ReasoningEffort(model, gjson.GetBytes(body, path).String()); err != nil {
			return &OpenAIFastBlockedError{Message: err.Error()}
		}
	}
	return nil
}

// Validate each final outbound attempt without rewriting its body or evaluating
// configurable Fast rules. Those rules have already applied their first match;
// re-running them on a filtered/forced tier would change the request and charge.
func validateOpenAIGPT62OutboundRequest(account *Account, model string, body []byte) *OpenAIFastBlockedError {
	if err := validateOpenAIGPT62RequestBody(account, model, body); err != nil {
		return err
	}
	if openAIGPT62RequestModel(account, model) != "" && normalizedOpenAIServiceTierValue(gjson.GetBytes(body, "service_tier").String()) == OpenAIFastTierUltrafast {
		return &OpenAIFastBlockedError{Message: "GPT-6.2 Ultrafast is not available on this gateway"}
	}
	return nil
}

// Normalize the original request only after mapping and provider selection.
// Generic protocol converters cannot establish either of those identities.
func prepareOpenAIGPT62RequestBody(account *Account, model string, body []byte) ([]byte, error) {
	if openAIGPT62RequestModel(account, model) == "" {
		return body, nil
	}
	if err := validateOpenAIGPT62RequestBody(account, model, body); err != nil {
		return body, err
	}
	updated := body
	for _, field := range []string{"temperature", "top_p"} {
		if gjson.GetBytes(updated, field).Exists() {
			var err error
			updated, err = sjson.DeleteBytes(updated, field)
			if err != nil {
				return body, err
			}
		}
	}
	return updated, nil
}
