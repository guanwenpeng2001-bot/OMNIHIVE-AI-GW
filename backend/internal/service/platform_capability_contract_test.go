package service

import (
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestPlatformCapabilityConsumers(t *testing.T) {
	require.ElementsMatch(t, domain.ConcretePlatforms(), ConcretePlatforms())
	require.ElementsMatch(t, domain.ConcretePlatforms(), schedulerSnapshotPlatforms())
	require.ElementsMatch(t, append(domain.ConcretePlatforms(), "vertex"), AllowedQuotaPlatforms)
	for _, p := range domain.PlatformDescriptors() {
		t.Run(p.ID, func(t *testing.T) {
			require.Equal(t, p.CNProvider, IsCNProvider(p.ID))
			require.Equal(t, p.MultiProtocol, IsMultiProtocolAPIKeyProvider(p.ID))
			require.Equal(t, p.SchedulingThreshold, containsPlatform(AllowedSchedulingThresholdPlatforms, p.ID))
			require.Equal(t, p.ActiveProbe || p.QuotaMonitor, validateProvider(p.ID) == nil)
			require.Equal(t, p.ActiveProbe, providerSupportsProbe(p.ID))
			_, _, adapter := providerAdapterFor(p.ID, MonitorAPIModeChatCompletions)
			require.Equal(t, p.ActiveProbe, adapter, "descriptor and real probe registry diverged")
			for _, mode := range []string{MonitorCheckModeProbe, MonitorCheckModeQuota, MonitorCheckModeQuotaProbe} {
				want := (mode == MonitorCheckModeQuota || p.ActiveProbe) && (mode == MonitorCheckModeProbe || p.QuotaMonitor)
				require.Equal(t, want, validateCheckMode(p.ID, mode) == nil, mode)
			}
			a := &Account{Platform: p.ID, Type: AccountTypeAPIKey}
			require.Equal(t, p.NativeMultiProtocolResponses, a.SupportsNativeCNResponses())
			require.Equal(t, p.ResponsesCompact, a.AllowsOpenAICompact())
		})
	}
	for _, id := range []string{"", "unknown", PlatformComposite, PlatformKiro} {
		require.False(t, isConcreteRequestPlatform(id))
		require.False(t, IsAllowedQuotaPlatform(id))
		require.Error(t, validateProvider(id))
		require.False(t, providerSupportsProbe(id))
		require.Error(t, monitorAccountQuotaCapability(&Account{Platform: id}))
		for _, mode := range []string{MonitorCheckModeProbe, MonitorCheckModeQuota, MonitorCheckModeQuotaProbe} {
			require.Error(t, validateCheckMode(id, mode))
		}
	}
	require.Error(t, monitorAccountQuotaCapability(nil))
}

func containsPlatform(platforms []string, id string) bool {
	for _, p := range platforms {
		if p == id {
			return true
		}
	}
	return false
}

func TestPlatformSpecificAccountCapabilities(t *testing.T) {
	for _, tt := range []struct {
		platform, accountType, mode string
		quota                       bool
	}{
		{PlatformOpenAI, AccountTypeOAuth, "", true},
		{PlatformOpenAI, AccountTypeAPIKey, "", false},
		{PlatformAnthropic, AccountTypeSetupToken, "", true},
		{PlatformAnthropic, AccountTypeAPIKey, "", false},
		{PlatformKimi, AccountTypeAPIKey, AccountModeCoding, true},
		{PlatformKimi, AccountTypeAPIKey, AccountModePayG, true},
		{PlatformZhipu, AccountTypeAPIKey, AccountModeCoding, true},
		{PlatformZhipu, AccountTypeAPIKey, AccountModePayG, false},
		{PlatformMiniMax, AccountTypeAPIKey, AccountModeCoding, true},
		{PlatformMiniMax, AccountTypeAPIKey, AccountModePayG, false},
		{PlatformOpenCodeGo, AccountTypeAPIKey, AccountModeGo, true},
		{PlatformOpenCodeGo, AccountTypeAPIKey, AccountModeZen, false},
		{PlatformQianwen, AccountTypeAPIKey, AccountModePayG, false},
		{PlatformCursor, AccountTypeOAuth, "", false},
		{PlatformCursor, AccountTypeUpstream, "", false},
	} {
		t.Run(tt.platform+"/"+tt.accountType+"/"+tt.mode, func(t *testing.T) {
			a := &Account{Platform: tt.platform, Type: tt.accountType, Credentials: map[string]any{"account_mode": tt.mode}}
			require.Equal(t, tt.quota, monitorAccountQuotaCapability(a) == nil)
		})
	}
	// A native platform path is only the outer bound: explicit protocol and
	// measured account capability still control effective endpoint eligibility.
	for _, mode := range []string{AccountModePayG, AccountModeCoding} {
		for _, protocol := range []string{APIProtocolChatCompletions, APIProtocolResponses, APIProtocolAdaptive} {
			a := &Account{Platform: PlatformKimi, Type: AccountTypeAPIKey, Credentials: map[string]any{"account_mode": mode, "api_protocol": protocol}}
			require.Equal(t, protocol != APIProtocolChatCompletions, a.UsesNativeCNResponses())
		}
	}
	a := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Extra: map[string]any{"openai_compact_mode": OpenAICompactModeForceOff}}
	require.False(t, a.AllowsOpenAICompact())
	a.Extra["openai_compact_mode"] = OpenAICompactModeForceOn
	require.True(t, a.AllowsOpenAICompact())
	require.False(t, (&Account{Platform: PlatformGemini, Type: AccountTypeOAuth}).AllowsOpenAICompact())
	require.False(t, (&Account{Platform: PlatformComposite}).AllowsOpenAICompact())
}

func TestPlatformPassiveMonitorConfig(t *testing.T) {
	for _, p := range domain.PlatformDescriptors() {
		cfg := ChannelTelemetryConfig{Platforms: []ChannelTelemetryPlatformConfig{{Platform: p.ID}}}
		require.Equal(t, p.PassiveMonitor, normalizeChannelTelemetryConfig(&cfg) == nil, p.ID)
	}
	for _, id := range []string{"unknown", PlatformComposite, PlatformKiro} {
		cfg := ChannelTelemetryConfig{Platforms: []ChannelTelemetryPlatformConfig{{Platform: id}}}
		require.ErrorIs(t, normalizeChannelTelemetryConfig(&cfg), ErrChannelTelemetryInvalidConfig)
	}
}
