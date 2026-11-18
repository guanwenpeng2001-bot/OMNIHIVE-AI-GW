package service

import (
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

// Request eligibility is independent of the name or provider on a candidate
// price card. Only the selected account and actual outbound model/tier count.
func validateOpenAIGPT62Usage(account *Account, result *OpenAIForwardResult) error {
	if result == nil || account == nil || !account.IsOpenAI() {
		return nil
	}
	model := upstreamSentModel(result.Model, result.UpstreamModel)
	if openai.GPT62ModelID(model) != "" && normalizeBillingServiceTier(optionalStringValue(result.ServiceTier)) == OpenAIFastTierUltrafast {
		return fmt.Errorf("GPT-6.2 Ultrafast outbound request has no integrated tariff: %w", ErrModelPricingUnavailable)
	}
	return nil
}

// Until request-time Ultrafast rates and receipt replay are integrated, a
// configured Standard price must not accidentally turn into a legacy 2x quote.
// Explicitly qualified non-OpenAI price cards retain their provider semantics.
func validateOpenAIGPT62BillingTier(model, tier string, providers ...string) error {
	if normalizeBillingServiceTier(tier) != OpenAIFastTierUltrafast {
		return nil
	}
	if len(providers) > 0 {
		if provider := strings.TrimSpace(providers[0]); provider != "" && !strings.EqualFold(provider, PlatformOpenAI) {
			return nil
		}
	}
	model = strings.TrimSpace(model)
	if slash := strings.IndexByte(model, '/'); slash >= 0 && !strings.EqualFold(model[:slash], PlatformOpenAI) {
		return nil
	}
	if openai.GPT62ModelID(model) != "" {
		return fmt.Errorf("GPT-6.2 Ultrafast request-time tariff is not integrated: %w", ErrModelPricingUnavailable)
	}
	return nil
}
