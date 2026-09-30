package domain

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestKimiSearchAndQianfanCancelMatrix(t *testing.T) {
	for _, scenario := range []struct{ platform, action, adapter string }{
		{PlatformKimi, "search", "kimi.search"},
		{PlatformQianfan, "video.cancel", "qianfan.video.cancel"},
	} {
		require.Contains(t, GatewayActions(), scenario.action)
		for _, accountType := range []string{"", AccountTypeAPIKey} {
			cell := PlatformAction(ActionIdentity{Platform: scenario.platform, HostedAccountType: accountType}, scenario.action)
			require.Equal(t, ActionImplemented, cell.Status)
			require.Equal(t, scenario.adapter, cell.Adapter)
			require.Contains(t, cell.Evidence, "2026-09-21")
			require.NotEmpty(t, cell.CredentialPolicy)
			require.NotEmpty(t, cell.UsagePolicy)
		}
	}
	require.Empty(t, PlatformAction(ActionIdentity{Platform: PlatformKimi, HostedAccountType: AccountTypeOAuth}, "search").Adapter)
	for _, platform := range ConcretePlatforms() {
		if platform != PlatformQianfan && platform != PlatformQianwen {
			require.Empty(t, PlatformAction(ActionIdentity{Platform: platform}, "video.cancel").Adapter)
		}
	}
}
