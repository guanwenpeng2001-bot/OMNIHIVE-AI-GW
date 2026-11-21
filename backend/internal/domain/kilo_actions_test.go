package domain

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestKiloActions(t *testing.T) {
	d, ok := LookupPlatform(PlatformKilo)
	require.True(t, ok)
	require.True(t, d.ChatOnly)
	require.True(t, d.ActiveProbe)
	require.True(t, d.PassiveMonitor)
	require.True(t, d.ModelCatalog)
	require.False(t, d.OAuth)
	require.False(t, d.QuotaMonitor)
	require.Equal(t, EndpointBridge, d.MessagesPOST)
	require.Equal(t, EndpointBridge, d.ResponsesPOST)
	for _, a := range GatewayActions() {
		row := PlatformAction(ActionIdentity{Platform: PlatformKilo, HostedAccountType: AccountTypeAPIKey}, a)
		if a == "chat" || a == "responses" || a == "messages" {
			require.Equal(t, ActionImplemented, row.Status)
			require.NotEmpty(t, row.Adapter)
		} else {
			require.Equal(t, ActionHardConflict, row.Status)
			require.Empty(t, row.Adapter)
			require.NotEmpty(t, row.Evidence)
		}
	}
	require.NotEqual(t, ActionImplemented, PlatformAction(ActionIdentity{Platform: PlatformKilo, HostedAccountType: AccountTypeOAuth}, "chat").Status)
}
