package domain

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestKaggleCompatibleProviderUsesAccountBase(t *testing.T) {
	provider, ok := LookupCompatibleProvider("kaggle")
	require.True(t, ok)
	require.Equal(t, "Kaggle", provider.Label)
	require.Equal(t, "account", provider.BaseSource)
	require.Equal(t, "payg", provider.DefaultMode)
	require.Len(t, provider.Modes, 1)
	mode := provider.Modes[0]
	require.Equal(t, APIProtocolChatCompletions, mode.DefaultProtocol)
	require.Equal(t, "none", mode.Discovery)
	endpoint, ok := mode.LookupEndpoint(APIProtocolChatCompletions)
	require.True(t, ok)
	require.Equal(t, "/v1", endpoint.Path)
	require.Empty(t, endpoint.BaseURL)
	require.Equal(t, "bearer", endpoint.Auth)
	_, ok = mode.LookupEndpoint(APIProtocolResponses)
	require.False(t, ok, "self-hosted notebooks expose Chat Completions only")
	require.Contains(t, ConcretePlatforms(), "kaggle")
}
