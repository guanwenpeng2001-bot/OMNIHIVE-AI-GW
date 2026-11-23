package catalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
	"sync"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	modelcapabilities "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/resources/model-capabilities"
	modelpricing "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/resources/model-pricing"
)

// RegisterReviewedMethodLookup is called at startup by the service package,
// which owns the process registry and can honor a request's frozen snapshot.
func RegisterReviewedMethodLookup(lookup func(context.Context, string, string) []string) {
	reviewedMethodLookup = lookup
}

var reviewedMethodLookup func(context.Context, string, string) []string

func ReviewedMethods(ctx context.Context, provider, model string) []string {
	if reviewedMethodLookup == nil {
		return nil
	}
	return reviewedMethodLookup(ctx, provider, model)
}

// Built-in limits and methods describe a model, independently of tenant
// endpoint availability. Match both provider and model ID exactly.
type builtinModel struct {
	Provider    string   `json:"provider"`
	Model       string   `json:"model_id"`
	DisplayName string   `json:"display_name"`
	Mode        string   `json:"mode"`
	Context     int64    `json:"context_window"`
	ContextKind string   `json:"context_limit_kind"`
	Input       int64    `json:"max_input_tokens"`
	Output      int64    `json:"max_output_tokens"`
	Methods     []string `json:"supported_methods"`
	Inputs      []string `json:"input_modalities"`
	Outputs     []string `json:"output_modalities"`
	Reasoning   *bool    `json:"supports_reasoning"`
	Vision      *bool    `json:"supports_vision"`
}

type supplementalIndex struct {
	models  map[ModelIdentity]builtinModel
	version string
}

var piModels = sync.OnceValue(func() supplementalIndex {
	var pi struct {
		SchemaVersion int            `json:"schema_version"`
		SourceVersion string         `json:"source_version"`
		Records       []builtinModel `json:"records"`
	}
	if err := json.Unmarshal(modelcapabilities.PiSupplementJSON(), &pi); err != nil {
		panic(err)
	}
	if pi.SchemaVersion != 1 {
		panic("unsupported compiled pi capability supplement schema")
	}
	index := make(map[ModelIdentity]builtinModel, len(pi.Records))
	for _, record := range pi.Records {
		index[NewModelIdentity(record.Provider, record.Model)] = record
	}
	return supplementalIndex{index, pi.SourceVersion}
})

var goSupplement = sync.OnceValue(func() supplementalIndex {
	index := make(map[ModelIdentity]builtinModel, len(supplementalBuiltinModels))
	for _, record := range supplementalBuiltinModels {
		index[NewModelIdentity(record.Provider, record.Model)] = record
	}
	return supplementalIndex{index, Digest(supplementalBuiltinModels)}
})

var supplementalBuiltinModels = []builtinModel{
	{Provider: "openai", Model: "codex/gpt-6.2-astra", Mode: "chat", Context: 400000, ContextKind: "total", Methods: []string{"chat_completions", "responses"}, Inputs: []string{"text", "image"}, Outputs: []string{"text"}},
	{Provider: "openai", Model: "auto/gpt-6.2-astra", Mode: "chat", Context: 400000, ContextKind: "total", Methods: []string{"chat_completions", "responses"}, Inputs: []string{"text", "image"}, Outputs: []string{"text"}},
	{Provider: "openai", Model: "codex/gpt-6.2-sol", Mode: "chat", Context: 400000, ContextKind: "total", Methods: []string{"chat_completions", "responses"}, Inputs: []string{"text", "image"}, Outputs: []string{"text"}},
	{Provider: "openai", Model: "gpt-6.2-astra", Mode: "chat", Context: 400000, ContextKind: "total", Methods: []string{"chat_completions", "responses"}, Inputs: []string{"text", "image"}, Outputs: []string{"text"}},
	{Provider: "openai", Model: "agy-cli/gemini-4-carbon", Mode: "chat", Context: 1000000, ContextKind: "total", Methods: []string{"chat_completions"}, Inputs: []string{"text", "image", "audio", "video", "pdf"}, Outputs: []string{"text"}},
	{Provider: "openai", Model: "auto/gemini-4-carbon", Mode: "chat", Context: 1000000, ContextKind: "total", Methods: []string{"chat_completions"}, Inputs: []string{"text", "image", "audio", "video", "pdf"}, Outputs: []string{"text"}},
	{Provider: "openai", Model: "auto/gemini-4-neon", Mode: "chat", Context: 1000000, ContextKind: "total", Methods: []string{"chat_completions"}, Inputs: []string{"text", "image", "audio", "video", "pdf"}, Outputs: []string{"text"}},
	{Provider: "openai", Model: "agy-cli/gemini-4-argon", Mode: "chat", Context: 1000000, ContextKind: "total", Methods: []string{"chat_completions"}, Inputs: []string{"text", "image", "audio", "video", "pdf"}, Outputs: []string{"text"}},
	{Provider: "openai", Model: "auto/gemini-4-argon", Mode: "chat", Context: 1000000, ContextKind: "total", Methods: []string{"chat_completions"}, Inputs: []string{"text", "image", "audio", "video", "pdf"}, Outputs: []string{"text"}},
	{Provider: "openai", Model: "auto/gemini-4-hydrogen", Mode: "chat", Context: 1000000, ContextKind: "total", Methods: []string{"chat_completions"}, Inputs: []string{"text", "image", "audio", "video", "pdf"}, Outputs: []string{"text"}},
	{Provider: "openai", Model: "auto/gemini-3.8-flash", Mode: "chat", Context: 1000000, ContextKind: "total", Methods: []string{"chat_completions"}, Inputs: []string{"text", "image", "audio", "video", "pdf"}, Outputs: []string{"text"}},
	{Provider: "gemini", Model: "gemini-3.8-flash", Mode: "chat", Context: 1000000, ContextKind: "total", Methods: []string{"generateContent"}, Inputs: []string{"text", "image", "audio", "video", "pdf"}, Outputs: []string{"text"}},
	{Provider: "openai", Model: "auto/kimi-k3", Mode: "chat", Context: 256000, ContextKind: "total", Methods: []string{"chat_completions"}, Inputs: []string{"text", "image"}, Outputs: []string{"text"}},
	{Provider: "openai", Model: "kimi-k3", Mode: "chat", Context: 256000, ContextKind: "total", Methods: []string{"chat_completions"}, Inputs: []string{"text", "image"}, Outputs: []string{"text"}},
	{Provider: "openai", Model: "deepseek/deepseek-v4-pro", Mode: "chat", Context: 128000, ContextKind: "total", Methods: []string{"chat_completions"}, Inputs: []string{"text"}, Outputs: []string{"text"}},
	{Provider: "openai", Model: "workbuddy/deepseek-v4-pro", Mode: "chat", Context: 128000, ContextKind: "total", Methods: []string{"chat_completions"}, Inputs: []string{"text"}, Outputs: []string{"text"}},
	{Provider: "openai", Model: "workbuddy/deepseek-v4.1-flash", Mode: "chat", Context: 128000, ContextKind: "total", Methods: []string{"chat_completions"}, Inputs: []string{"text"}, Outputs: []string{"text"}},
	{Provider: "deepseek", Model: "deepseek-v4-pro", Mode: "chat", Context: 128000, ContextKind: "total", Methods: []string{"chat_completions"}, Inputs: []string{"text"}, Outputs: []string{"text"}},
}

func supplementalFacts(id ModelIdentity) []RegistryModel {
	var out []RegistryModel
	for _, src := range []struct {
		index supplementalIndex
		kind  string
	}{{goSupplement(), BuiltinSupplement}, {piModels(), PiAIReference}} {
		m, ok := src.index.models[id]
		if !ok {
			continue
		}
		modalities, actions := TaskPresentation(m.Mode)
		f := Facts{SupportedMethods: slices.Clone(m.Methods)}
		if m.Reasoning != nil {
			reasoning := *m.Reasoning
			f.Reasoning = &reasoning
		}
		if m.Vision != nil {
			vision := *m.Vision
			f.Vision = &vision
		}
		if m.Context > 0 {
			f.Context, f.ContextKind = &m.Context, m.ContextKind
		} else if m.Input > 0 {
			f.Context, f.ContextKind = &m.Input, "input"
		}
		if m.Output > 0 {
			f.Output = &m.Output
		}
		if m.Inputs != nil || m.Outputs != nil || len(modalities) != 0 {
			f.Modalities = []string{}
			for _, modality := range slices.Concat(m.Inputs, m.Outputs, modalities) {
				switch modality {
				case "text", "image", "audio", "video", "pdf", "embedding", "3d":
					f.Modalities = append(f.Modalities, modality)
				}
			}
		}
		f.InputModalities, f.OutputModalities = slices.Clone(m.Inputs), slices.Clone(m.Outputs)
		e := Evidence{Source: src.kind, Maturity: "unknown", State: "unknown"}
		origin := Provenance{SourceKind: src.kind, RecordKey: id.Provider + "/" + id.Model, Provider: id.Provider,
			ArtifactVersion: src.index.version, MatchRule: "exact_public_name", Trust: "reference", Scope: "public"}
		out = append(out, RegistryModel{Provider: id.Provider, Model: id.Model, DisplayName: m.DisplayName,
			Facts: ModelFacts{Facts: f, Evidence: e, Origin: origin}, Methods: m.Methods,
			Capabilities: MethodCapabilities(id.Provider, m.Methods, e), Actions: actions})
	}
	return out
}

func MethodCapabilities(provider string, methods []string, evidence Evidence) []Capability {
	out := []Capability{}
	identity := domain.ActionIdentity{Platform: provider, HostedAccountType: domain.HostedAccountType(provider)}
	for _, method := range methods {
		action, protocol, adapter := GatewayAction(method)
		if adapter != "" && domain.PlatformAction(identity, adapter).Status == domain.ActionImplemented {
			out = append(out, Capability{Action: action, Protocol: protocol, Mode: "unknown", Tools: "unknown",
				Stream: "unknown", Limitations: []string{}, Evidence: evidence})
		}
	}
	return out
}

// SupplementalMethods is a descriptive exact-match fallback.
func SupplementalMethods(provider, model string) []string {
	for _, entry := range supplementalFacts(NewModelIdentity(provider, model)) {
		if entry.Methods != nil {
			return slices.Clone(entry.Methods)
		}
	}
	return nil
}

type piTokenRates struct {
	Provider string      `json:"litellm_provider"`
	Input    json.Number `json:"input_cost_per_token"`
	Output   json.Number `json:"output_cost_per_token"`
}

type piPriceIndex struct {
	prices  map[ModelIdentity]piTokenRates
	version string
}

var piTokenPrices = sync.OnceValue(func() piPriceIndex {
	var raw map[string]piTokenRates
	artifact := modelpricing.PiSupplementJSON()
	if err := json.Unmarshal(artifact, &raw); err != nil {
		panic(err)
	}
	prices := make(map[ModelIdentity]piTokenRates)
	for key, rate := range raw {
		provider, model, ok := strings.Cut(key, "/")
		if ok && provider == rate.Provider {
			prices[NewModelIdentity(provider, model)] = rate
		}
	}
	hash := sha256.Sum256(artifact)
	return piPriceIndex{prices: prices, version: hex.EncodeToString(hash[:])}
})

func piReferencePrice(provider, model string) *Price {
	rates, ok := piTokenPrices().prices[NewModelIdentity(provider, model)]
	if !ok {
		return nil
	}
	in, err := money.NewRate("USD", rates.Input.String(), "input_token", "1")
	if err != nil {
		return nil
	}
	out, err := money.NewRate("USD", rates.Output.String(), "output_token", "1")
	if err != nil {
		return nil
	}
	return &Price{State: "reference", Source: "pi-ai", Input: &in, Output: &out}
}
