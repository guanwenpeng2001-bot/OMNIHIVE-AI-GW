package pricing

import (
	"encoding/json"
	"sync"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
	modelpricing "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/resources/model-pricing"
)

var piReferences = sync.OnceValue(func() map[string]struct {
	Provider string      `json:"litellm_provider"`
	Input    json.Number `json:"input_cost_per_token"`
	Output   json.Number `json:"output_cost_per_token"`
} {
	var data map[string]struct {
		Provider string      `json:"litellm_provider"`
		Input    json.Number `json:"input_cost_per_token"`
		Output   json.Number `json:"output_cost_per_token"`
	}
	if err := json.Unmarshal(modelpricing.PiSupplementJSON(), &data); err != nil {
		panic(err)
	}
	return data
})

// LookupReferencePrice supplies optional catalog display evidence for an exact
// provider and model. Its rates can have a different basis from the sale tariff.
func LookupReferencePrice(provider, model string) *ReferencePrice {
	card, ok := piReferences()[provider+"/"+model]
	if !ok || card.Provider != provider {
		return nil
	}
	ref := &ReferencePrice{Provider: provider, Model: model, Source: "pi-ai"}
	if rate, err := money.NewRate("USD", card.Input.String(), "input_token", "1"); err == nil {
		ref.Input = &rate
	}
	if rate, err := money.NewRate("USD", card.Output.String(), "output_token", "1"); err == nil {
		ref.Output = &rate
	}
	if ref.Input == nil && ref.Output == nil {
		return nil
	}
	return ref
}
