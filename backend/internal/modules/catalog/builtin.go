package catalog

import (
	"encoding/json"
	"slices"
	"sync"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	modelcapabilities "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/resources/model-capabilities"
)

// Built-in limits and methods describe a model, independently of tenant
// endpoint availability. Match both provider and model ID exactly.
type builtinModel struct {
	Provider    string   `json:"provider"`
	Model       string   `json:"model_id"`
	Mode        string   `json:"mode"`
	Context     int64    `json:"context_window"`
	ContextKind string   `json:"context_limit_kind"`
	Input       int64    `json:"max_input_tokens"`
	Methods     []string `json:"supported_methods"`
	Inputs      []string `json:"input_modalities"`
	Outputs     []string `json:"output_modalities"`
}

var builtinModels = sync.OnceValue(func() map[[2]string]builtinModel {
	var release struct {
		Records []builtinModel `json:"records"`
	}
	if err := json.Unmarshal(modelcapabilities.JSON(), &release); err != nil {
		panic(err)
	}
	index := make(map[[2]string]builtinModel, len(release.Records))
	for _, record := range release.Records {
		index[[2]string{record.Provider, record.Model}] = record
	}
	return index
})

func fillBuiltinFacts(d *Detail, contexts []ModelFacts) {
	m, ok := builtinModels()[[2]string{d.Model.Provider, d.Model.Model}]
	if !ok {
		return
	}
	evidence := Evidence{Source: "approved_registry", Maturity: "unknown", State: "unknown"}
	facts := ModelFacts{Evidence: evidence}
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
	if len(d.Model.Modalities) == 0 {
		for _, group := range [][]string{m.Inputs, m.Outputs} {
			for _, modality := range group {
				switch modality {
				case "text", "image", "audio", "video", "pdf", "embedding":
					facts.Modalities = append(facts.Modalities, modality)
				}
			}
		}
		if len(facts.Modalities) == 0 && (m.Mode == "chat" || m.Mode == "responses") {
			facts.Modalities = append(facts.Modalities, "text")
		}
	}
	if facts.Context != nil || len(facts.Modalities) != 0 {
		d.Facts = append(d.Facts, facts)
		if facts.Context != nil {
			d.Model.Context = facts.Context
		}
		d.Model.Modalities = append(d.Model.Modalities, facts.Modalities...)
	}
	if len(d.Capabilities) != 0 {
		return
	}

	for _, method := range BuiltinMethods(m.Provider, m.Model) {
		action, protocol, adapter := GatewayAction(method)
		identity := domain.ActionIdentity{Platform: m.Provider, HostedAccountType: domain.HostedAccountType(m.Provider)}
		if adapter != "" && domain.PlatformAction(identity, adapter).Status == domain.ActionImplemented {
			d.Capabilities = append(d.Capabilities, Capability{Action: action, Protocol: protocol, Mode: "unknown", Tools: "unknown", Stream: "unknown", Limitations: []string{}, Evidence: evidence})
			d.Model.Protocols = append(d.Model.Protocols, protocol)
			d.Model.Actions = append(d.Model.Actions, action)
		}
	}
}

// BuiltinMethods projects the exact release record onto gateway methods. A
// chat-mode record can supply the provider’s standard text method when the
// release has no method list. Endpoint availability is checked separately.
func BuiltinMethods(provider, model string) []string {
	m, ok := builtinModels()[[2]string{provider, model}]
	if !ok {
		return nil
	}
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
