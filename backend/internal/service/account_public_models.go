package service

import (
	"context"
	"slices"
)

// restrictedPublicModels separates public request names from authorized wire IDs.
// Platform default mappings still apply in inherit mode.
type restrictedPublicModels struct {
	scope   map[string]bool
	targets map[string]bool
	direct  map[string]bool
	aliases map[string]string
}

func modelAliasTargets(mapping map[string]string) map[string]bool {
	targets := make(map[string]bool)
	for name, target := range mapping {
		if name != target {
			targets[target] = true
		}
	}
	return targets
}

func (a *Account) restrictedPublicModels() *restrictedPublicModels {
	if a.modelCatalogReadEvidence != nil {
		return a.modelCatalogReadEvidence.publicModels
	}
	var scope AccountModelAllowlist
	switch raw := a.Extra[UpstreamModelAllowlistExtraKey].(type) {
	case nil:
		return nil
	case AccountModelAllowlist:
		scope = raw
	case map[string]any:
		if enabled, _ := raw["enabled"].(bool); !enabled {
			return nil
		}
		decodeCatalogExtra(a.Extra, UpstreamModelAllowlistExtraKey, &scope)
	default:
		decodeCatalogExtra(a.Extra, UpstreamModelAllowlistExtraKey, &scope)
	}
	if !scope.Enabled || len(scope.Models) == 0 {
		return nil
	}
	mapping := configuredModelMapping(a)
	targets := modelAliasTargets(mapping)
	models := &restrictedPublicModels{
		scope:   make(map[string]bool, len(scope.Models)),
		targets: targets,
		direct:  make(map[string]bool, len(scope.Models)),
		aliases: make(map[string]string),
	}
	for _, model := range scope.Models {
		models.scope[model] = true
		if !targets[model] || mapping[model] == model {
			models.direct[model] = true
		}
	}
	for name, target := range mapping {
		if name != target && models.scope[target] {
			models.aliases[name] = target
		}
	}
	return models
}

func (m *restrictedPublicModels) resolve(name string) (string, bool) {
	if target, ok := m.aliases[name]; ok {
		return target, true
	}
	if m.direct[name] {
		return name, true
	}
	// A wildcard cannot publish a private target under its original ID or
	// override a directly selected name. An exact alias has explicit intent.
	if name == "" || m.scope[name] {
		return name, false
	}
	return matchWildcardMappingResult(m.aliases, name)
}

// ConfiguredPublicModelNames returns declarations (including wildcard patterns).
// authoritative distinguishes a configured empty result from the legacy fallback.
func (a *Account) ConfiguredPublicModelNames() (names []string, authoritative bool) {
	if a.ModelSelectionDeniesAll() {
		return []string{}, true
	}
	if m := a.restrictedPublicModels(); m != nil {
		names = make([]string, 0, len(m.direct)+len(m.aliases))
		for name := range m.direct {
			if _, alias := m.aliases[name]; !alias && m.allows(a, name, false) {
				names = append(names, name)
			}
		}
		for name := range m.aliases {
			if m.allows(a, name, false) {
				names = append(names, name)
			}
		}
		slices.Sort(names)
		return names, true
	}
	mapping := a.GetModelMapping()
	names = make([]string, 0, len(mapping))
	for name := range mapping {
		names = append(names, name)
	}
	slices.Sort(names)
	return names, len(mapping) != 0
}

func (m *restrictedPublicModels) allows(a *Account, name string, routingToScope bool) bool {
	target, ok := m.resolve(name)
	return (ok || routingToScope && m.scope[name]) && (a.Platform != PlatformQianwen ||
		(!IsRetiredQianwenModel(name) && !IsRetiredQianwenModel(target)))
}

func (m *restrictedPublicModels) allowsWireModel(a *Account, wire string) bool {
	for id := range m.scope {
		if normalizeAccountBindingWireModel(a, id) == wire {
			return true
		}
	}
	return false
}

// A group or channel can explicitly supply a public name for a scoped wire
// model. Confirm that a trusted rewrite changed the actual request name.
func accountContextRewritesModel(ctx context.Context, model, requested string) bool {
	if plan := GatewayRequestPlanFromContext(ctx); plan != nil && plan.Snapshot().Model != "" {
		requested = plan.Snapshot().Model
	}
	if publicName, ok := RequestedPublicModelFromContext(ctx); ok {
		requested = publicName
	}
	if requested == "" || requested == model {
		return false
	}
	if forward, ok := openAIForwardModelFromContext(ctx); ok && forward.model == model {
		return true
	}
	source, _ := CompositeRouteSourceFromContext(ctx)
	resolved, _ := ResolvedUpstreamModelFromContext(ctx)
	return source == CompositeRouteSourceExplicit && resolved == model
}

func (a *Account) supportsRoutedModel(ctx context.Context, model string) bool {
	if selection := a.restrictedPublicModels(); selection != nil {
		return selection.allows(a, model, accountContextRewritesModel(ctx, model, ""))
	}
	return a.IsModelSupported(model)
}

// SupportsRoutedModelTarget checks a target supplied by a trusted group route or channel.
func (a *Account) SupportsRoutedModelTarget(public, target string) bool {
	if selection := a.restrictedPublicModels(); selection != nil {
		return selection.allows(a, target, public != target)
	}
	return a.IsModelSupported(target)
}

func (a *Account) modelSelectionClaim(model string) (claimed bool, handled bool) {
	if a.ModelSelectionDeniesAll() {
		return false, true
	}
	if selection := a.restrictedPublicModels(); selection != nil {
		return selection.allows(a, model, false), true
	}
	return false, false
}
