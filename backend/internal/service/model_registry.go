package service

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"

	modelcapabilities "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/resources/model-capabilities"
)

// ModelRegistrySnapshot is a single approved fact view. Candidates never grant
// account access, and pricing overrides remain owned by the pricing resolver.
// Its data is private; accessors return copies or immutable snapshots.
type ModelRegistrySnapshot struct {
	release      modelcapabilities.ReleaseIdentity
	presets      []PlatformModelPreset
	capabilities *ModelCapabilitySnapshot
}

func (s *ModelRegistrySnapshot) Generation() int64                            { return s.release.ApprovedGeneration }
func (s *ModelRegistrySnapshot) Version() string                              { return s.release.ManifestSHA256 }
func (s *ModelRegistrySnapshot) CapabilitySnapshot() *ModelCapabilitySnapshot { return s.capabilities }
func (s *ModelRegistrySnapshot) Presets() []PlatformModelPreset {
	body, _ := json.Marshal(s.presets)
	var result []PlatformModelPreset
	_ = json.Unmarshal(body, &result)
	return result
}

// CandidateIDs is for administration only. Empty profile means all reviewed
// profiles for that platform; callers selecting an account must supply its profile.
func (s *ModelRegistrySnapshot) CandidateIDs(platform, profile string) []string {
	result := []string{}
	seen := make(map[string]bool)
	for _, preset := range s.presets {
		if preset.Platform != platform && platform != PlatformComposite {
			continue
		}
		if profile != "" && preset.ProviderProfile != profile {
			continue
		}
		for _, model := range preset.Models {
			if !seen[model.ID] {
				result = append(result, model.ID)
				seen[model.ID] = true
			}
		}
	}
	return result
}

// ModelRegistry serializes approval and publishes the fully built view once.
type ModelRegistry struct {
	mu         sync.Mutex
	current    atomic.Pointer[ModelRegistrySnapshot]
	trustedPin []byte
}

var processModelRegistry = sync.OnceValue(func() *ModelRegistry {
	registry := &ModelRegistry{trustedPin: modelcapabilities.TrustedPinJSON()}
	snapshot, err := registry.build(modelcapabilities.JSON(), modelcapabilities.ManifestJSON(), platformModelPresetsJSON, nil)
	if err != nil {
		panic("invalid compiled model registry: " + err.Error())
	}
	registry.current.Store(snapshot)
	catalog.PublishApprovedInventory(snapshot.Version(), snapshot.CatalogFacts(time.Now()))
	return registry
})

// NewModelRegistry returns the process registry shared by DI and legacy service
// entry points. No consumer owns a second independently published fact table.
func NewModelRegistry() *ModelRegistry                    { return processModelRegistry() }
func (r *ModelRegistry) Snapshot() *ModelRegistrySnapshot { return r.current.Load() }
func (r *ModelRegistry) CapabilitySnapshot() *ModelCapabilitySnapshot {
	return r.Snapshot().CapabilitySnapshot()
}

type modelRegistryContextKey struct{}

func WithModelRegistrySnapshot(ctx context.Context, snapshot *ModelRegistrySnapshot) context.Context {
	return context.WithValue(ctx, modelRegistryContextKey{}, snapshot)
}
func ModelRegistrySnapshotFromContext(ctx context.Context) *ModelRegistrySnapshot {
	if ctx != nil {
		if snapshot, ok := ctx.Value(modelRegistryContextKey{}).(*ModelRegistrySnapshot); ok {
			return snapshot
		}
	}
	return NewModelRegistry().Snapshot()
}
