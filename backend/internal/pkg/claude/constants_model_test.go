package claude_test

import (
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultModelsContainsClaudeFable51(t *testing.T) {
	t.Parallel()

	for _, model := range service.ClaudePresetModels() {
		if model.ID == "claude-fable-5-1" {
			require.Equal(t, "Claude Fable 5.1", model.DisplayName)
			require.Empty(t, model.CreatedAt, "a preset does not invent a creation date")
			return
		}
	}
	t.Fatal("claude-fable-5-1 missing from DefaultModels")
}
