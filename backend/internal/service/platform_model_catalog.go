package service

import (
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/Wei-Shaw/sub2api/internal/pkg/geminicli"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
)

// DefaultModelIDsForPlatform returns the existing platform preset, never a
// different provider's catalog. An unconfigured catalog is explicitly empty.
func DefaultModelIDsForPlatform(platform string) []string {
	if platform != PlatformComposite {
		d, ok := domain.LookupPlatform(platform)
		if !ok || !d.ModelCatalog {
			return []string{}
		}
	}
	switch platform {
	case PlatformOpenAI:
		return openai.DefaultModelIDs()
	case PlatformGemini:
		ids := make([]string, 0, len(geminicli.DefaultModels))
		for _, model := range geminicli.DefaultModels {
			ids = append(ids, model.ID)
		}
		return ids
	case PlatformAntigravity:
		models := antigravity.DefaultModels()
		ids := make([]string, 0, len(models))
		for _, model := range models {
			ids = append(ids, model.ID)
		}
		return ids
	case PlatformAnthropic:
		return claude.DefaultModelIDs()
	case PlatformGrok:
		return xai.DefaultModelIDs()
	case PlatformCursor:
		return DefaultCursorModelIDs()
	case PlatformOpenCodeGo:
		return DefaultOpenCodeGoModelIDs()
	case PlatformQianwen:
		return DefaultQianwenModelIDs()
	case PlatformCommandCode:
		return DefaultCommandCodeModelIDs()
	case PlatformComposite:
		ids := make([]string, 0)
		seen := make(map[string]struct{})
		for _, concretePlatform := range domain.ConcretePlatforms() {
			for _, id := range DefaultModelIDsForPlatform(concretePlatform) {
				if _, ok := seen[id]; ok {
					continue
				}
				seen[id] = struct{}{}
				ids = append(ids, id)
			}
		}
		return ids
	default:
		return []string{}
	}
}
