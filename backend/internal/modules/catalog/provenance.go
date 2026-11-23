package catalog

import (
	"slices"
	"time"
)

const (
	BuiltinReviewed    = "builtin_reviewed"
	PlatformPreset     = "platform_preset"
	BuiltinSupplement  = "builtin_supplement"
	PiAIReference      = "pi_ai_reference"
	AccountDiscovery   = "account_discovery"
	AccountDeclaration = "account_declaration"
	ExternalReference  = "external_reference"
)

// Provenance describes one field observation or one unresolved field decision.
type Provenance struct {
	Field           string     `json:"field"`
	State           string     `json:"state"`
	SourceKind      string     `json:"source_kind,omitempty"`
	RecordKey       string     `json:"record_key,omitempty"`
	ArtifactVersion string     `json:"artifact_version,omitempty"`
	Provider        string     `json:"provider,omitempty"`
	Profile         string     `json:"profile,omitempty"`
	MatchRule       string     `json:"match_rule,omitempty"`
	Trust           string     `json:"trust,omitempty"`
	Scope           string     `json:"scope,omitempty"`
	ObservedAt      *time.Time `json:"observed_at,omitempty"`
	ExpiresAt       *time.Time `json:"expires_at,omitempty"`
	Value           any        `json:"value,omitempty"`
}

type factField struct {
	name string
	get  func(Facts) any
	set  func(*Facts, any)
}

type contextLimit struct {
	Tokens int64  `json:"tokens,string"`
	Basis  string `json:"basis"`
}

func optionalField[T any](name string, access func(*Facts) **T) factField {
	return factField{name, func(f Facts) any {
		if p := *access(&f); p != nil {
			return *p
		}
		return nil
	}, func(f *Facts, value any) {
		v := value.(T)
		*access(f) = &v
	}}
}

func setField(name string, access func(*Facts) *[]string) factField {
	return factField{name, func(f Facts) any {
		if values := *access(&f); values != nil {
			return stringsSorted(slices.Clone(values))
		}
		return nil
	}, func(f *Facts, v any) { *access(f) = v.([]string) }}
}

var factFields = []factField{
	{"context_window", func(f Facts) any {
		if f.Context == nil {
			return nil
		}
		basis := f.ContextKind
		if basis != "input" && basis != "total" {
			basis = "unspecified"
		}
		return contextLimit{*f.Context, basis}
	}, func(f *Facts, v any) { c := v.(contextLimit); f.Context, f.ContextKind = &c.Tokens, c.Basis }},
	optionalField("max_output_tokens", func(f *Facts) **int64 { return &f.Output }),
	optionalField("embedding_dimensions", func(f *Facts) **int64 { return &f.EmbeddingDimensions }),
	optionalField("supports_function_calling", func(f *Facts) **bool { return &f.Tools }),
	optionalField("supports_vision", func(f *Facts) **bool { return &f.Vision }),
	optionalField("supports_reasoning", func(f *Facts) **bool { return &f.Reasoning }),
	optionalField("supports_prompt_caching", func(f *Facts) **bool { return &f.Cache }),
	optionalField("supports_pdf", func(f *Facts) **bool { return &f.PDF }),
	setField("modalities", func(f *Facts) *[]string { return &f.Modalities }),
	setField("input_modalities", func(f *Facts) *[]string { return &f.InputModalities }),
	setField("output_modalities", func(f *Facts) *[]string { return &f.OutputModalities }),
	setField("supported_methods", func(f *Facts) *[]string { return &f.SupportedMethods }),
}

func sourcePriority(kind string) int {
	switch kind {
	case AccountDiscovery:
		return 6
	case AccountDeclaration:
		return 5
	case BuiltinReviewed:
		return 4
	case PlatformPreset:
		return 3
	case BuiltinSupplement:
		return 2
	case PiAIReference:
		return 1
	}
	return 0
}

func observe(m ModelFacts, field factField) (Provenance, bool) {
	o := m.Origin
	if fieldOrigin, ok := m.FieldOrigins[field.name]; ok {
		o = fieldOrigin
	}
	if o.SourceKind == "" {
		switch m.Source {
		case "approved_registry":
			o.SourceKind = BuiltinReviewed
		case "upstream_discovery":
			o.SourceKind = AccountDiscovery
		default:
			o.SourceKind = m.Source
		}
	}
	o.Field = field.name
	if o.State == "" {
		o.State, o.ObservedAt, o.ExpiresAt = "known", m.ObservedAt, m.ExpiresAt
	}
	o.Value = field.get(m.Facts)
	switch {
	case slices.Contains(m.Suppressed, field.name):
		o.State, o.Value = "suppressed", nil
	case o.Value == nil:
		return Provenance{}, false
	}
	if m.Evidence.State == "stale" && o.SourceKind != AccountDeclaration {
		o.State = "stale"
	}
	return o, true
}

func resolveFacts(descriptive []ModelFacts, bindings []BindingFacts) (Facts, []Provenance) {
	active := make([]BindingFacts, 0, len(bindings))
	var historical []ModelFacts
	for _, binding := range bindings {
		if binding.Historical {
			historical = append(historical, binding.Facts...)
		} else {
			active = append(active, binding)
		}
	}
	bindings = active
	if len(bindings) == 0 {
		profiles := []string{}
		for _, f := range descriptive {
			if f.Origin.Profile != "" {
				profiles = append(profiles, f.Origin.Profile)
			}
		}
		for _, profile := range stringsSorted(profiles) {
			bindings = append(bindings, BindingFacts{Profile: profile})
		}
		if len(bindings) == 0 {
			bindings = []BindingFacts{{}}
		}
	}
	var common Facts
	provenance := []Provenance{}
	for _, field := range factFields {
		for _, m := range historical {
			if o, ok := observe(m, field); ok {
				provenance = append(provenance, o)
			}
		}
		var value any
		var signature string
		conflict, blocked := false, false
		for _, binding := range bindings {
			priority := -1
			var selected []Provenance
			for _, m := range slices.Concat(descriptive, binding.Facts) {
				if m.Origin.Profile != "" && binding.Profile != "" && m.Origin.Profile != binding.Profile {
					continue
				}
				o, ok := observe(m, field)
				if !ok {
					continue
				}
				provenance = append(provenance, o)
				if o.State == "stale" {
					continue
				}
				rank := sourcePriority(o.SourceKind)
				if rank < priority {
					continue
				}
				if rank > priority {
					selected, priority = selected[:0], rank
				}
				selected = append(selected, o)
			}
			for _, o := range selected {
				key := Digest([3]any{o.Value, o.Scope, o.State})
				conflict = conflict || signature != "" && signature != key
				signature, value = key, o.Value
				blocked = blocked || o.State == "suppressed"
			}
		}
		switch {
		case conflict:
			provenance = append(provenance, Provenance{Field: field.name, State: "conflict"})
		case value != nil && !blocked:
			field.set(&common, value)
		case signature == "":
			provenance = append(provenance, Provenance{Field: field.name, State: "unknown"})
		}
	}
	if common.Modalities == nil && (common.InputModalities != nil || common.OutputModalities != nil) {
		common.Modalities = stringsSorted(append(append([]string{}, common.InputModalities...), common.OutputModalities...))
	}
	return common, unique(provenance)
}

func redactProvenance(p Perspective, records []Provenance) []Provenance {
	out := make([]Provenance, 0, len(records))
	for _, record := range records {
		switch {
		case p.Public():
			out = append(out, Provenance{Field: record.Field, State: record.State, SourceKind: record.SourceKind,
				ArtifactVersion: record.ArtifactVersion, ObservedAt: record.ObservedAt})
		case !p.EndpointMetadata && record.Scope == "account":
			continue
		default:
			out = append(out, record)
		}
	}
	return unique(out)
}
