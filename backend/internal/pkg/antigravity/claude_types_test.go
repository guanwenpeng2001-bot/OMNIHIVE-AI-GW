package antigravity_test

import (
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/antigravity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"testing"
)

func TestRegistryAntigravityPreservesReviewedIDsWithoutInventingVariants(t *testing.T) {
	t.Parallel()

	models := service.AntigravityPresetModels()
	byID := make(map[string]antigravity.ClaudeModel, len(models))
	for _, m := range models {
		byID[m.ID] = m
	}

	excludedIDs := []string{
		"claude-fable-5-1",
		"claude-fable-5",
		"claude-opus-4-8",
		"claude-opus-4-6-thinking",
		"gemini-2.5-flash-image",
		"gemini-2.5-flash-image-preview",
		"gemini-3.1-flash-image",
		"gemini-3.1-flash-image-preview",
		"gemini-3-pro-image", // legacy compatibility
		"gemini-3.6-flash-high",
		"gemini-3.6-flash-low",
		"gemini-3.6-flash-medium",
		"gemini-3.6-flash-tiered",
		"gemini-3.7-flash-high",
		"gemini-3.7-flash-low",
		"gemini-3.7-flash-medium",
		"gemini-3.7-flash-tiered",
		"gemini-3.8-flash-high",
		"gemini-3.8-flash-low",
		"gemini-3.8-flash-medium",
		"gemini-3.8-flash-tiered",
	}

	// The 2026-09-20 approved review did not establish these exact wire IDs.
	for _, id := range excludedIDs {
		if _, ok := byID[id]; ok {
			t.Fatalf("unreviewed candidate alias %q was invented", id)
		}
	}
	for _, id := range []string{"claude-opus-4.6", "claude-sonnet-4.6", "gemini-3.6-flash", "gemini-3.7-flash", "gemini-3.8-flash"} {
		if _, ok := byID[id]; !ok {
			t.Fatalf("reviewed candidate %q missing", id)
		}
	}
}
