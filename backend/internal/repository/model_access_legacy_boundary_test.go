package repository

import (
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestV1ClearPreservesHistoricalKeyBoundary(t *testing.T) {
	for _, mode := range []string{"allow_list", "deny_all"} {
		rule, err := preserveLegacyKeyModelBoundary(service.ModelAccessRule{}, "openai", mode, []string{"a"})
		require.NoError(t, err)
		if mode == "allow_list" {
			require.Equal(t, []string{"a"}, rule.Models)
		} else {
			require.Equal(t, []string{}, rule.Models)
		}
		_, err = preserveLegacyKeyModelBoundary(service.ModelAccessRule{Models: []string{"b"}}, "openai", mode, []string{"a"})
		require.ErrorIs(t, err, service.ErrModelAccessExpansion)
	}
	rule, err := preserveLegacyKeyModelBoundary(service.ModelAccessRule{Models: []string{catalog.StableID("openai", "a")}}, "openai", "allow_list", []string{"a"})
	require.NoError(t, err)
	require.Len(t, rule.Models, 1)
	_, err = preserveLegacyKeyModelBoundary(service.ModelAccessRule{Models: []string{catalog.StableID("anthropic", "a")}}, "openai", "allow_list", []string{"a"})
	require.ErrorIs(t, err, service.ErrModelAccessExpansion)
}
