package catalog

import (
	"encoding/json"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	modelcapabilities "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/resources/model-capabilities"
	modelpricing "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/resources/model-pricing"
)

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
	fromPi      bool
	source      string
	version     string
}

type approvedInventory struct{ models map[[2]string]builtinModel }

var approvedModels atomic.Pointer[approvedInventory]

// PublishApprovedInventory accepts only the already verified registry projection.
// It is a compatibility index, never a second decoder or approval authority.
func PublishApprovedInventory(version string, entries []RegistryModel) {
	index := make(map[[2]string]builtinModel, len(entries))
	for _, entry := range entries {
		f := entry.Facts
		m := builtinModel{Provider: entry.Provider, Model: entry.Model, DisplayName: entry.DisplayName,
			ContextKind: f.ContextKind, Inputs: slices.Clone(f.Modalities), Reasoning: f.Reasoning, Vision: f.Vision,
			source: f.Source, version: version}
		if f.Context != nil {
			m.Context = *f.Context
		}
		if f.Output != nil {
			m.Output = *f.Output
		}
		for _, capability := range entry.Capabilities {
			switch capability.Protocol {
			case "openai_chat":
				m.Methods = append(m.Methods, "chat_completions")
			case "openai_responses":
				m.Methods = append(m.Methods, "responses")
			case "anthropic_messages":
				m.Methods = append(m.Methods, "messages")
			case "gemini_generate":
				m.Methods = append(m.Methods, "generateContent")
			case "openai_embeddings":
				m.Methods = append(m.Methods, "embeddings")
			}
		}
		key := [2]string{m.Provider, m.Model}
		if _, exists := index[key]; !exists {
			index[key] = m
		}
	}
	approvedModels.Store(&approvedInventory{models: index})
}

var referenceModels = sync.OnceValue(func() map[[2]string]builtinModel {
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
	index := make(map[[2]string]builtinModel, len(supplementalBuiltinModels)+len(pi.Records))
	for _, sup := range supplementalBuiltinModels {
		sup.source, sup.version = SourceOperator, "legacy-supplement-v1"
		key := [2]string{sup.Provider, sup.Model}
		if _, exists := index[key]; !exists {
			index[key] = sup
		}
	}
	for _, record := range pi.Records {
		key := [2]string{record.Provider, record.Model}
		if _, exists := index[key]; !exists {
			record.fromPi = true
			record.source, record.version = SourcePI, pi.SourceVersion
			index[key] = record
		}
	}
	return index
})

func builtinModels() map[[2]string]builtinModel {
	index := make(map[[2]string]builtinModel, len(referenceModels()))
	for key, model := range referenceModels() {
		index[key] = model
	}
	if approved := approvedModels.Load(); approved != nil {
		for key, model := range approved.models {
			index[key] = model
		}
	}
	return index
}

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

func fillBuiltinFacts(d *Detail, contexts []ModelFacts) {
	// The acquired revision is the only authority for approved facts. Reference
	// inventory is allowed to describe otherwise unknown candidates, not replace
	// deliberate omissions or suppressed fields from a reviewed entry.
	if slices.ContainsFunc(d.Facts, func(f ModelFacts) bool {
		return PublicEvidence(f.Evidence) && f.Source != SourcePI && f.Source != SourceOperator
	}) {
		return
	}
	m, ok := referenceModels()[[2]string{d.Model.Provider, d.Model.Model}]
	if !ok {
		return
	}
	missingModalities := len(d.Model.Modalities) == 0
	modalities, actions := TaskPresentation(m.Mode)
	d.Model.Actions = append(d.Model.Actions, actions...)
	d.Model.Modalities = append(d.Model.Modalities, modalities...)
	evidence := Evidence{Source: m.source, Maturity: "unknown", State: "unknown"}
	evidence.Provenance = map[string]Provenance{"protocol": {SourceKind: m.source, SourceRevision: m.version, FieldPath: "protocol", Approval: "reference", SelectedReason: "descriptive_reference_only"}}
	facts := ModelFacts{Evidence: evidence}
	if m.fromPi {
		if d.Model.DisplayName == d.Model.Model {
			d.Model.DisplayName = m.DisplayName
		}
		if m.Output > 0 && !slices.ContainsFunc(d.Facts, func(f ModelFacts) bool { return f.Output != nil }) {
			facts.Output = &m.Output
		}
		if m.Reasoning != nil && !slices.ContainsFunc(d.Facts, func(f ModelFacts) bool { return f.Reasoning != nil }) {
			reasoning := *m.Reasoning
			facts.Reasoning = &reasoning
		}
		if m.Vision != nil && !slices.ContainsFunc(d.Facts, func(f ModelFacts) bool { return f.Vision != nil }) {
			vision := *m.Vision
			facts.Vision = &vision
		}
	}
	if d.Model.Context == nil && !slices.ContainsFunc(contexts, func(f ModelFacts) bool { return f.Context != nil }) {
		if m.Context > 0 {
			facts.Context, facts.ContextKind = &m.Context, m.ContextKind
			if facts.ContextKind != "input" && facts.ContextKind != "total" {
				facts.ContextKind = "unspecified"
			}
		} else if m.Input > 0 {
			facts.Context, facts.ContextKind = &m.Input, "input"
		}
	}
	if missingModalities {
		for _, group := range [][]string{m.Inputs, m.Outputs} {
			for _, modality := range group {
				switch modality {
				case "text", "image", "audio", "video", "pdf", "embedding", "3d":
					facts.Modalities = append(facts.Modalities, modality)
				}
			}
		}
		if len(facts.Modalities) == 0 && (m.Mode == "chat" || m.Mode == "responses") {
			facts.Modalities = append(facts.Modalities, "text")
		}
	}
	if facts.Context != nil || len(facts.Modalities) != 0 || facts.Output != nil || facts.Reasoning != nil || facts.Vision != nil {
		AttachProvenance(&facts, m.version, "reference", "fills_unreviewed_candidate")
		d.Facts = append(d.Facts, facts)
		if facts.Context != nil {
			d.Model.Context = facts.Context
		}
		d.Model.Modalities = append(d.Model.Modalities, facts.Modalities...)
	}
	if len(d.Capabilities) != 0 {
		return
	}

	for _, method := range builtinMethods(m) {
		action, protocol, adapter := GatewayAction(method)
		identity := domain.ActionIdentity{Platform: m.Provider, HostedAccountType: domain.HostedAccountType(m.Provider)}
		if adapter != "" && domain.PlatformAction(identity, adapter).Status == domain.ActionImplemented {
			d.Capabilities = append(d.Capabilities, Capability{Action: action, Protocol: protocol, Mode: "unknown", Tools: "unknown", Stream: "unknown", Limitations: []string{}, Evidence: evidence})
			d.Model.Protocols = append(d.Model.Protocols, protocol)
			d.Model.Actions = append(d.Model.Actions, action)
		}
	}
}

// BuiltinMethods projects the exact built-in record onto gateway methods. A
// chat-mode record can supply the provider’s standard text method when the
// release has no method list. Endpoint availability is checked separately.
func BuiltinMethods(provider, model string) []string {
	key := [2]string{provider, model}
	if approved := approvedModels.Load(); approved != nil {
		if m, ok := approved.models[key]; ok {
			return builtinMethods(m)
		}
	}
	m, ok := referenceModels()[key]
	if !ok {
		return nil
	}
	return builtinMethods(m)
}

func builtinMethods(m builtinModel) []string {
	methods := m.Methods
	if len(methods) == 0 && m.Mode == "chat" {
		switch m.Provider {
		case "openai", "deepseek", "mistral":
			methods = []string{"chat_completions"}
		case "anthropic":
			methods = []string{"messages"}
		case "gemini":
			methods = []string{"generateContent"}
		}
	}
	return slices.Clone(methods)
}

type piTokenRates struct {
	Provider string      `json:"litellm_provider"`
	Input    json.Number `json:"input_cost_per_token"`
	Output   json.Number `json:"output_cost_per_token"`
}

var piTokenPrices = sync.OnceValue(func() map[string]piTokenRates {
	var prices map[string]piTokenRates
	if err := json.Unmarshal(modelpricing.PiSupplementJSON(), &prices); err != nil {
		panic(err)
	}
	return prices
})

var referenceInventoryVersion = sync.OnceValue(func() string {
	return Digest([2]string{string(modelcapabilities.PiSupplementJSON()), string(modelpricing.PiSupplementJSON())})
})

func ReferenceInventoryVersion() string { return referenceInventoryVersion() }

func piReferencePrice(provider, model string) *Price {
	rates, ok := piTokenPrices()[provider+"/"+model]
	if !ok || rates.Provider != provider {
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
	return &Price{Kind: "reference", State: "reference", Source: "pi-ai", Input: &in, Output: &out}
}
