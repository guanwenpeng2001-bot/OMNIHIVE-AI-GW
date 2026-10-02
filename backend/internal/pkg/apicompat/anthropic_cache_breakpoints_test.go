package apicompat

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestAnthropicCacheBreakpointsOptIn(t *testing.T) {
	var req AnthropicRequest
	require.NoError(t, json.Unmarshal([]byte(`{"model":"gpt-6.1-sol","system":[{"type":"text","text":"x-anthropic-billing-header: remove","cache_control":{"type":"ephemeral"}},{"type":"text","text":"system","cache_control":{"type":"ephemeral","ttl":"1h"}}],"messages":[{"role":"user","content":[{"type":"text","text":"user","cache_control":{"type":"ephemeral","ttl":"5m"}}]},{"role":"assistant","content":[{"type":"text","text":"first","cache_control":{"type":"ephemeral"}},{"type":"text","text":"second"}]}]}`), &req))
	plain, err := AnthropicToResponses(&req)
	require.NoError(t, err)
	plainJSON, err := json.Marshal(plain)
	require.NoError(t, err)
	require.NotContains(t, string(plainJSON), "prompt_cache_")
	mapped, err := AnthropicToResponsesWithCacheControl(&req)
	require.NoError(t, err)
	encoded, err := json.Marshal(mapped)
	require.NoError(t, err)
	for _, path := range []string{"input.0.content.0", "input.1.content.0", "input.2.content.0"} {
		require.Equal(t, "explicit", gjson.GetBytes(encoded, path+".prompt_cache_breakpoint.mode").String())
	}
	require.False(t, gjson.GetBytes(encoded, "input.2.content.1.prompt_cache_breakpoint").Exists())
	require.Equal(t, "explicit", gjson.GetBytes(encoded, "prompt_cache_options.mode").String())
	require.False(t, gjson.GetBytes(encoded, "prompt_cache_options.ttl").Exists())
	require.NotContains(t, string(encoded), "billing-header")
}

func TestAnthropicCacheBreakpointsOnlySurvivingMarkers(t *testing.T) {
	for _, system := range []string{`"plain"`, `[{"type":"text","text":"x-anthropic-billing-header: removed","cache_control":{"type":"ephemeral"}}]`, `[{"type":"text","text":"","cache_control":{"type":"ephemeral"}}]`} {
		req := &AnthropicRequest{Model: "gpt-6.1-sol", System: json.RawMessage(system)}
		mapped, err := AnthropicToResponsesWithCacheControl(req)
		require.NoError(t, err)
		encoded, err := json.Marshal(mapped)
		require.NoError(t, err)
		require.NotContains(t, string(encoded), "prompt_cache_")
	}
}
