//go:build unit

package admin

import (
	"testing"

	"github.com/gin-gonic/gin/binding"
	"github.com/stretchr/testify/require"
)

func TestChannelMonitorProviderBindings(t *testing.T) {
	for _, provider := range []string{"openai", "anthropic", "gemini", "grok", "antigravity", "kimi", "zhipu", "deepseek", "qianwen", "commandcode", "minimax", "opencode_go"} {
		t.Run(provider, func(t *testing.T) {
			require.NoError(t, binding.Validator.ValidateStruct(channelMonitorCreateRequest{Name: "test", Provider: provider, IntervalSeconds: 60}))
			require.NoError(t, binding.Validator.ValidateStruct(channelMonitorUpdateRequest{Provider: &provider}))
			require.NoError(t, binding.Validator.ValidateStruct(channelMonitorTemplateCreateRequest{Name: "test", Provider: provider}))
		})
	}
	for _, provider := range []string{"cursor", "unknown"} {
		t.Run(provider, func(t *testing.T) {
			require.Error(t, binding.Validator.ValidateStruct(channelMonitorCreateRequest{Name: "test", Provider: provider, IntervalSeconds: 60}))
			require.Error(t, binding.Validator.ValidateStruct(channelMonitorUpdateRequest{Provider: &provider}))
			require.Error(t, binding.Validator.ValidateStruct(channelMonitorTemplateCreateRequest{Name: "test", Provider: provider}))
		})
	}
	require.NoError(t, binding.Validator.ValidateStruct(channelMonitorUpdateRequest{}))
}
