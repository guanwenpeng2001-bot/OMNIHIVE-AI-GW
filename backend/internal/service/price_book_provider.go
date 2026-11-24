package service

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/pricing"
	modelpricing "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/resources/model-pricing"
)

var tariffBindings = sync.OnceValue(func() struct {
	Static map[string][]string
	Bare   map[string]string
	Hosted map[string]map[string]string
} {
	var bindings struct {
		Static map[string][]string
		Bare   map[string]string
		Hosted map[string]map[string]string
	}
	if err := json.Unmarshal(modelpricing.ProviderTariffBindingsJSON(), &bindings); err != nil {
		panic(err)
	}
	return bindings
})

// These are source metadata values. A host with its own platform keeps its
// namespace even when the model is developed by another provider.
func tariffProvider(source string) string {
	source = strings.ToLower(strings.TrimSpace(source))
	switch source {
	case "text-completion-openai":
		return PlatformOpenAI
	case "vertex_ai", "vertex_ai-language-models", "vertex_ai-embedding-models", "google":
		return PlatformGemini
	case "xai":
		return PlatformGrok
	case "dashscope":
		return PlatformQianwen
	case "moonshot":
		return PlatformKimi
	case "zai", "z.ai":
		return PlatformZhipu
	case "volcengine_maas":
		return PlatformVolcengine
	}
	if slices.Contains(concretePlatformSlice(), source) {
		return source
	}
	return ""
}

func qualifiedTariffName(name string) (string, string) {
	prefix, model, found := strings.Cut(name, "/")
	if found {
		if provider := tariffProvider(prefix); provider != "" && model != "" {
			return provider, model
		}
	}
	return "", name
}

func rawTariffOwner(name string, body json.RawMessage) string {
	var metadata struct {
		Provider string `json:"litellm_provider"`
	}
	if json.Unmarshal(body, &metadata) != nil {
		return ""
	}
	namespace, model := qualifiedTariffName(strings.ToLower(strings.TrimSpace(name)))
	owner := tariffProvider(metadata.Provider)
	if namespace != "" {
		if owner != "" && owner != namespace {
			return ""
		}
		return namespace
	}
	if owner == "" && metadata.Provider == "" {
		if binding := tariffBindings().Bare[model]; binding != "" {
			return binding
		}
		for provider, models := range tariffBindings().Static {
			if slices.Contains(models, model) {
				return provider
			}
		}
	}
	return owner
}

func referenceTariffEntry(raw json.RawMessage) bool {
	var flags struct {
		Source string `json:"source"`
		Role   string `json:"price_role"`
	}
	if json.Unmarshal(raw, &flags) != nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(flags.Role), "reference") || pricing.IsReferenceSource(flags.Source)
}

type scopedPriceCard struct {
	card     *LiteLLMModelPricing
	conflict bool
}

type providerPriceIndex struct {
	exact map[[2]string]scopedPriceCard
	dates map[[2]string]scopedPriceCard
}

func mergeScopedPrice(index map[[2]string]scopedPriceCard, key [2]string, card *LiteLLMModelPricing) {
	current, exists := index[key]
	if !exists {
		index[key] = scopedPriceCard{card: card}
	} else if current.card != card {
		index[key] = scopedPriceCard{conflict: true}
	}
}

func buildProviderPriceIndex(data map[string]*LiteLLMModelPricing) *providerPriceIndex {
	index := &providerPriceIndex{exact: make(map[[2]string]scopedPriceCard), dates: make(map[[2]string]scopedPriceCard)}
	for name, card := range data {
		if card == nil {
			continue
		}
		name = strings.ToLower(strings.TrimSpace(name))
		owner := card.tariffOwner
		if owner == "" && !card.tariffOwnerChecked {
			owner = tariffProvider(card.LiteLLMProvider)
			if owner == "" && card.LiteLLMProvider == "" {
				owner = rawTariffOwner(name, json.RawMessage("{}"))
			}
			if namespace, _ := qualifiedTariffName(name); namespace != "" && (owner == "" || owner == namespace) {
				owner = namespace
			}
		}
		if owner == "" {
			continue
		}
		// Index the supplier's spelling and its namespace-relative model ID.
		namespace, model := qualifiedTariffName(name)
		mergeScopedPrice(index.exact, [2]string{owner, name}, card)
		// Preserve a provider-qualified override alongside the inherited bare
		// entry. Qualified hits are looked up before the namespace-relative ID.
		if namespace == owner && name != owner+"/"+model {
			mergeScopedPrice(index.exact, [2]string{owner, model}, card)
		}
	}
	for key, entry := range index.exact {
		_, model := qualifiedTariffName(key[1])
		base := stripTariffDate(model)
		if key[1] != key[0]+"/"+model && index.exact[[2]string{key[0], key[0] + "/" + model}].card != nil {
			continue
		}
		if !entry.conflict {
			mergeScopedPrice(index.dates, [2]string{key[0], base}, entry.card)
		} else {
			index.dates[[2]string{key[0], base}] = entry
		}
	}
	return index
}

func stripTariffDate(model string) string {
	// LiteLLM uses both YYYYMMDD and YYYY-MM-DD snapshot suffixes.
	for _, layout := range []string{"20060102", "2006-01-02"} {
		start := len(model) - len(layout)
		if start > 0 && model[start-1] == '-' {
			if _, err := time.Parse(layout, model[start:]); err == nil {
				return model[:start-1]
			}
		}
	}
	return model
}

func providerTariffNames(provider, model string) []string {
	if provider == PlatformAntigravity {
		return []string{model, normalizeAntigravityTieredModel(model), normalizeGeminiThinkingTierAlias(strings.TrimPrefix(model, "models/"))}
	}
	if provider == PlatformGemini || provider == PlatformAntigravity {
		model = strings.TrimPrefix(model, "models/")
		if strings.HasPrefix(model, "publishers/google/models/") || strings.Contains(model, "/publishers/google/models/") {
			model = normalizeModelNameForPricing(model)
		}
		return []string{model, normalizeGeminiThinkingTierAlias(model)}
	}
	if provider == PlatformOpenAI {
		if canonical := normalizeKnownOpenAICodexModel(model); canonical != "" {
			return []string{model, canonical}
		}
	}
	if provider == PlatformAnthropic {
		return []string{model, strings.ReplaceAll(model, "-4-5-", "-4.5-")}
	}
	return []string{model}
}

func normalizeAntigravityTieredModel(model string) string {
	for _, suffix := range []string{"-high", "-medium", "-low", "-thinking"} {
		if strings.HasSuffix(model, suffix) && strings.HasPrefix(model, "claude-") {
			base := strings.TrimSuffix(model, suffix)
			if base == "claude-opus-5-5" {
				return "claude-opus-5"
			}
			return base
		}
	}
	if model == "claude-opus-5-5" {
		return "claude-opus-5"
	}
	return model
}

func (s *PricingService) providerModelPricing(provider, model string, exact bool) (*LiteLLMModelPricing, error) {
	if s == nil {
		return nil, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	index := s.providerPrices
	if index == nil {
		// Some embedded callers supply their own immutable data directly.
		index = buildProviderPriceIndex(s.pricingData)
	}
	return index.lookup(provider, model, exact)
}

func (i *providerPriceIndex) lookup(provider, model string, exact bool) (*LiteLLMModelPricing, error) {
	model = strings.ToLower(strings.TrimSpace(model))
	namespace, native := qualifiedTariffName(model)
	if namespace == provider {
		card := i.exact[[2]string{provider, model}]
		if card.conflict {
			return nil, pricing.ErrAmbiguous
		}
		if card.card != nil {
			return card.card, nil
		}
		model = native
	}
	names := providerTariffNames(provider, model)
	if exact {
		names = names[:1]
	}
	for _, name := range names {
		bare, qualified := i.exact[[2]string{provider, name}], i.exact[[2]string{provider, provider + "/" + name}]
		if qualified.card != nil && qualified.card.tariffPatched {
			bare, qualified = qualified, bare
		}
		for _, card := range []scopedPriceCard{bare, qualified} {
			if card.conflict {
				return nil, pricing.ErrAmbiguous
			}
			if card.card != nil {
				return card.card, nil
			}
		}
	}
	if !exact {
		base := stripTariffDate(names[0])
		if base != names[0] {
			if direct, err := i.lookup(provider, base, true); direct != nil || err != nil {
				return direct, err
			}
		}
		card := i.dates[[2]string{provider, base}]
		if card.conflict {
			return nil, pricing.ErrAmbiguous
		}
		if card.card != nil {
			return card.card, nil
		}
	}
	for _, name := range names {
		if origin := tariffBindings().Hosted[provider][name]; origin != "" {
			target := name
			if provider == PlatformAntigravity {
				target = normalizeAntigravityTieredModel(name)
			}
			return i.lookup(origin, target, exact)
		}
	}
	return nil, nil
}

func scopedFallback(s *BillingService, provider, model string) *ModelPricing {
	model = strings.ToLower(strings.TrimSpace(model))
	if namespace, native := qualifiedTariffName(model); namespace == provider {
		model = native
	}
	names := providerTariffNames(provider, model)
	for _, name := range names {
		if origin := tariffBindings().Hosted[provider][name]; origin != "" {
			target := name
			if provider == PlatformAntigravity {
				target = normalizeAntigravityTieredModel(name)
			}
			return scopedFallback(s, origin, target)
		}
		base := stripTariffDate(name)
		if provider == PlatformAntigravity {
			base = normalizeAntigravityTieredModel(base)
		}
		if slices.Contains(tariffBindings().Static[provider], base) {
			return s.fallbackPrices[base]
		}
	}
	// Keep the existing named Grok family, including its documented forward
	// default, only on its native platform. DeepSeek's sunset IDs have a
	// published compatibility rate on its native platform.
	if provider == PlatformGrok && (!strings.Contains(model, "/") || strings.HasPrefix(model, "x-ai/")) {
		return s.getFallbackPricing(model)
	}
	if provider == PlatformDeepseek && (model == "deepseek-chat" || model == "deepseek-reasoner") {
		return s.fallbackPrices["deepseek-v4-flash"]
	}
	if provider == PlatformOpenAI && model == "gpt-5.3-codex-spark" {
		return s.fallbackPrices["gpt-5.3-codex"]
	}
	return nil
}

// Match each patch in its original namespace before parsing price fields.
func providerOverrideTargets(data map[string]json.RawMessage, name string) []string {
	provider, model := qualifiedTariffName(strings.ToLower(strings.TrimSpace(name)))
	if provider != "" {
		if base, exists := data[name]; exists && rawTariffOwner(name, base) == provider {
			return []string{name}
		}
		if base, exists := data[model]; exists && rawTariffOwner(model, base) == provider {
			data[name] = base
			return []string{name}
		}
		return nil
	}

	owner := ""
	for key, raw := range data {
		_, native := qualifiedTariffName(strings.ToLower(strings.TrimSpace(key)))
		if native != model {
			continue
		}
		current := rawTariffOwner(key, raw)
		if current == "" {
			continue
		}
		if owner != "" && owner != current {
			return nil
		}
		owner = current
	}
	// Keep a bare patch bound to the original bare record. Prefix-specific
	// imports may have different units, fields or effective price bands.
	if _, exists := data[name]; exists {
		return []string{name}
	}
	return nil
}

type tariffPatch struct {
	previouslyPriced bool
}

func hasSourceTariff(model string, raw json.RawMessage) bool {
	for _, quote := range pricingEntryQuotes(model, raw) {
		if quote.Value != nil {
			return true
		}
	}
	var entry LiteLLMRawEntry
	return unmarshalPricingEntry(raw, &entry) == nil && len(entry.PriceVariants) != 0
}

func patchedPricingEntries(data, overrides map[string]json.RawMessage) (map[string]json.RawMessage, map[string]tariffPatch) {
	if len(overrides) == 0 {
		return data, nil
	}
	// Detect owners from the original fields. An override cannot reassign
	// ownership, and the qualified patch wins over a shared bare patch.
	keys := slices.Sorted(maps.Keys(overrides))
	original := maps.Clone(data)
	targets := make(map[string][]string, len(keys))
	patched := make(map[string]tariffPatch, len(keys))
	for _, name := range keys {
		owner, _ := qualifiedTariffName(strings.ToLower(strings.TrimSpace(name)))
		if owner != "" {
			providerOverrideTargets(original, name)
		}
	}
	for _, name := range keys {
		targets[name] = providerOverrideTargets(original, name)
	}
	for _, qualified := range []bool{false, true} {
		for _, name := range keys {
			owner, _ := qualifiedTariffName(strings.ToLower(strings.TrimSpace(name)))
			if qualified != (owner != "") {
				continue
			}
			if referenceTariffEntry(overrides[name]) {
				continue
			}
			for _, target := range targets[name] {
				base := data[target]
				if base == nil {
					_, native := qualifiedTariffName(target)
					base = data[native]
					if base == nil {
						base = original[target]
					}
				}
				if merged, ok := mergePricingOverrideEntry(base, overrides[name]); ok && rawTariffOwner(target, original[target]) == rawTariffOwner(target, merged) {
					data[target] = merged
					patched[target] = tariffPatch{previouslyPriced: hasSourceTariff(target, original[target])}
				}
			}
		}
	}
	return data, patched
}
