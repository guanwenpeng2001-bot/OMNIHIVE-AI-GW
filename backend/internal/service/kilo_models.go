package service

var kiloFreeModelIDs = []string{
	"kilo-auto/free", "openrouter/free", "nvidia/nemotron-3.5-super-vl:free", "nvidia/nemotron-3.5-lightning:free", "nvidia/nemotron-3-ultra-550b-a55b:free", "nvidia/nemotron-3-nano-omni-30b-a3b-reasoning:free", "baidu/ernie-5.5-flash-preview:free", "liquid/lfm-3-1.7b:free", "inclusionai/ling-3.2-flash:free", "particle-ai/particle-1:free", "recursive/flint-v1.0:free", "reflection/beam-free", "stealth/glyph-cluster",
}

// Adapter candidates are intentionally outside the signed pi-ai release. They
// populate administrative catalogs but never grant account/model entitlement.
func kiloModelPresets() []PlatformModelPreset {
	names := []string{"Kilo Auto Free", "OpenRouter Free", "Nemotron 3.5 Super VL", "Nemotron 3.5 Lightning", "Nemotron 3 Ultra 550B A55B", "Nemotron 3 Nano Omni 30B A3B Reasoning", "ERNIE 5.5 Flash Preview", "LFM 3 1.7B", "Ling 3.2 Flash", "Particle 1", "Flint v1.0", "Beam Free", "Glyph Cluster"}
	p := freeLanePreset(PlatformKilo, "kilo", "our-free-model/src/kilo.mjs")
	for i, id := range kiloFreeModelIDs {
		m := freeLanePresetModel(id, names[i], p.SourceRefs[0])
		if id == "kilo-auto/free" {
			m.ContextWindow = 256000
		}
		p.Models = append(p.Models, m)
	}
	return []PlatformModelPreset{p}
}
func freeLanePreset(platform, profile, source string) PlatformModelPreset {
	return PlatformModelPreset{Platform: platform, ProviderProfile: profile, Revision: "v0.3.2-free-models", CoverageStatus: "partial", CheckedAt: "2026-11-22T00:00:00Z", SourceRefs: []string{source}, Gaps: []string{"Bootstrap candidates; live account discovery is authoritative for availability and optional capabilities"}}
}
func freeLanePresetModel(id, name, source string) PlatformPresetModel {
	return PlatformPresetModel{ID: id, DisplayName: name, TaskType: "chat", InputModalities: []string{"text"}, OutputModalities: []string{"text"}, Methods: []string{"chat_completions", "responses", "messages"}, Lifecycle: "active", FieldSources: map[string]string{"identity": source}}
}
