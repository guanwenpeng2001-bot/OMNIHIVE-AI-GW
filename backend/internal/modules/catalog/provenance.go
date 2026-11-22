package catalog

import "slices"

const (
	SourceBuiltin  = "builtin_registry"
	SourcePreset   = "platform_preset"
	SourcePI       = "pi_ai"
	SourceOperator = "operator_override"
)

type Provenance struct {
	SourceKind     string `json:"source_kind"`
	SourceRevision string `json:"source_revision"`
	FieldPath      string `json:"field_path"`
	Approval       string `json:"approval"`
	SelectedReason string `json:"selected_reason"`
}

// PublicEvidence is a positive allow-list. Approval is independent of origin:
// public reference facts may be shown, but never grant access or routing.
func PublicEvidence(e Evidence) bool {
	if !slices.Contains([]string{"approved_registry", SourceBuiltin, SourcePreset, SourcePI, SourceOperator}, e.Source) {
		return false
	}
	for _, p := range e.Provenance {
		if !slices.Contains([]string{SourceBuiltin, SourcePreset, SourcePI, SourceOperator}, p.SourceKind) {
			return false
		}
	}
	return true
}

func AttachProvenance(f *ModelFacts, version, approval, reason string) {
	fields := []string{}
	if f.Context != nil {
		fields = append(fields, "context_tokens", "context_kind")
	}
	if f.Output != nil {
		fields = append(fields, "max_output_tokens")
	}
	if f.EmbeddingDimensions != nil {
		fields = append(fields, "embedding_dimensions")
	}
	if f.Tools != nil {
		fields = append(fields, "tools")
	}
	if f.Vision != nil {
		fields = append(fields, "vision")
	}
	if f.Reasoning != nil {
		fields = append(fields, "reasoning")
	}
	if f.Cache != nil {
		fields = append(fields, "prompt_caching")
	}
	if f.PDF != nil {
		fields = append(fields, "pdf")
	}
	if len(f.Modalities) > 0 {
		fields = append(fields, "modalities")
	}
	f.Provenance = make(map[string]Provenance, len(fields))
	for _, field := range fields {
		f.Provenance[field] = Provenance{SourceKind: f.Source, SourceRevision: version, FieldPath: field, Approval: approval, SelectedReason: reason}
	}
}
