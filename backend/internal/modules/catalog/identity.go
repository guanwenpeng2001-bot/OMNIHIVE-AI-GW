package catalog

import (
	"strings"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
)

// ModelIdentity is the exact public name within an OmniHive provider. Hosted
// providers keep their own namespace.
type ModelIdentity struct {
	Provider string
	Model    string
}

func NewModelIdentity(provider, model string) ModelIdentity {
	normalized := strings.ToLower(strings.TrimSpace(provider))
	if domain.IsConcretePlatform(normalized) {
		provider = normalized
	}
	return ModelIdentity{Provider: provider, Model: model}
}

func (id ModelIdentity) ID() string { return StableID(id.Provider, id.Model) }

func CanonicalName(model string, targets []string) string {
	if len(targets) == 0 {
		return model
	}
	for _, target := range targets[1:] {
		if target != targets[0] {
			return model
		}
	}
	return targets[0]
}

// Find resolves a visible ID or exact qualified slug before applying the
// legacy slash-containing bare-name fallback.
func (s Snapshot) Find(value string) (Detail, bool) {
	provider, model, slug := strings.Cut(value, "/")
	identity := NewModelIdentity(provider, model)
	var fallback Detail
	matches := 0
	for _, d := range s.Details {
		if d.Model.ID == value || slug && d.Model.Provider == identity.Provider && d.Model.Model == identity.Model {
			return d, true
		}
		if slug && d.Model.Model == value {
			fallback = d
			matches++
		}
	}
	return fallback, matches == 1
}
