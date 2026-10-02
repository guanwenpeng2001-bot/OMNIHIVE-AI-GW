//go:build unit

package service

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"testing"
)

func TestOpenRouterReviewedMetadataAndRemaining(t *testing.T) {
	t.Run("explicit effort survives mandatory object", func(t *testing.T) {
		_, metadata, err := extractUpstreamModelCatalog([]byte(`{"data":[{"id":"vendor/future:free","reasoning":{"mandatory":true,"effort":{"valid":["low","high"],"default":"low"}}}]}`), false, PlatformOpenRouter)
		require.NoError(t, err)
		entry := metadata["vendor/future:free"]
		require.Equal(t, []string{"low", "high"}, entry.SupportedReasoningLevels)
		require.Equal(t, "low", entry.DefaultReasoningLevel)
		require.Nil(t, entry.Reasoning)
	})
	t.Run("actual public OpenRouter card fields", func(t *testing.T) {
		_, metadata, err := extractUpstreamModelCatalog([]byte(`{"data":[{"id":"openai/gpt-6.1-sol","reasoning":{"mandatory":true,"default_enabled":true,"supported_efforts":["max","xhigh","high","medium","low"],"default_effort":"medium"}}]}`), false, PlatformOpenRouter)
		require.NoError(t, err)
		entry := metadata["openai/gpt-6.1-sol"]
		require.Equal(t, []string{"max", "xhigh", "high", "medium", "low"}, entry.SupportedReasoningLevels)
		require.Equal(t, "medium", entry.DefaultReasoningLevel)
		require.Nil(t, entry.Reasoning)
	})
	t.Run("explicit top-level efforts remain supported", func(t *testing.T) {
		_, metadata, err := extractUpstreamModelCatalog([]byte(`{"data":[{"id":"vendor/future:free","supported_reasoning_levels":["low","high"],"default_reasoning_level":"high"}]}`), false, PlatformOpenRouter)
		require.NoError(t, err)
		entry := metadata["vendor/future:free"]
		require.Equal(t, []string{"low", "high"}, entry.SupportedReasoningLevels)
		require.Equal(t, "high", entry.DefaultReasoningLevel)
	})
	for _, fixture := range []struct {
		body      string
		unlimited bool
	}{{`{"data":{"limit":10,"limit_remaining":null,"usage":1}}`, true}, {`{"data":{"limit":10,"usage":1}}`, false}, {`{"data":{"limit":null,"limit_remaining":0,"usage":1}}`, false}} {
		t.Run(fixture.body, func(t *testing.T) {
			usage, err := parseOpenRouterKeyUsage([]byte(fixture.body))
			require.NoError(t, err)
			encoded, err := json.Marshal(usage)
			require.NoError(t, err)
			require.Equal(t, fixture.unlimited, gjson.GetBytes(encoded, "remaining_unlimited").Bool())
		})
	}
}
