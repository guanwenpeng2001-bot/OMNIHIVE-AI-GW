//go:build unit

package service

import (
	"context"
	"strings"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestMonitorAndQuotaPlatformAuthorities(t *testing.T) {
	require.ElementsMatch(t, append(expectedServicePlatforms(), "vertex"), AllowedQuotaPlatforms)
	// Original monitor eligibility is literal, independent of descriptor flags.
	expectedMonitors := []string{"anthropic", "gemini", "openai", "antigravity", "grok", "kimi", "zhipu", "deepseek", "minimax", "opencode_go", "qianwen", "commandcode", "workbuddy", "kilo"}
	actual := []string{}
	for _, platform := range ConcretePlatforms() {
		if validateProvider(platform) == nil {
			actual = append(actual, platform)
		}
	}
	require.ElementsMatch(t, expectedMonitors, actual)
	for _, provider := range domain.CompatibleProviders() {
		t.Run(provider.ID, func(t *testing.T) {
			requireCompatibleServiceIdentity(t, provider.ID)
		})
	}
}

func requireCompatibleServiceIdentity(t *testing.T, platform string) {
	t.Helper()
	require.True(t, IsAllowedQuotaPlatform(platform))
	require.True(t, isConcreteRequestPlatform(platform))
	require.Equal(t, platform, domain.CanonicalPlatform(platform))
	require.Equal(t, platform, domain.HostedQuotaKey(platform, AccountTypeAPIKey))
	require.Equal(t, platform, QuotaPlatform(context.Background(), &APIKey{Group: &Group{Platform: platform}}))
	require.ErrorIs(t, validateProvider(platform), ErrChannelMonitorInvalidProvider)
	require.False(t, providerSupportsProbe(platform))
	for _, mode := range []string{MonitorCheckModeProbe, MonitorCheckModeQuota, MonitorCheckModeQuotaProbe} {
		require.Error(t, validateCheckMode(platform, mode))
	}
	descriptor, ok := domain.LookupPlatform(platform)
	require.True(t, ok)
	require.False(t, descriptor.OAuth)
	require.False(t, descriptor.ActiveProbe)
	require.False(t, descriptor.QuotaMonitor)
	require.True(t, descriptor.PassiveMonitor)
	require.Empty(t, DefaultModelIDsForPlatform(platform))
	for _, invalid := range []string{strings.ToUpper(platform), " " + platform, platform + " "} {
		require.False(t, IsAllowedQuotaPlatform(invalid))
		require.False(t, isConcreteRequestPlatform(invalid))
		require.Error(t, validateProvider(invalid))
	}
}

func TestServiceRegistryGrowthPreservesSupplierIdentity(t *testing.T) {
	rows := domain.CompatibleProviders()
	require.NotEmpty(t, rows)
	// A synthetic identity reuses a valid admitted transport fixture solely to
	// prove discovery of a future row; it adds no production supplier metadata.
	future := rows[0]
	future.ID = "service_growth_fixture"
	rows = append(rows, future)
	restore := domain.SetCompatibleProvidersForTest(rows)
	t.Cleanup(restore)
	require.Contains(t, matchingPlatforms(PlatformComposite), future.ID)
	require.Equal(t, []string{future.ID}, matchingPlatforms(future.ID))
	require.Contains(t, schedulerCanonicalBuckets(99), SchedulerBucket{GroupID: 99, Platform: future.ID, Mode: SchedulerModeSingle})
	require.Contains(t, schedulerCanonicalBuckets(99), SchedulerBucket{GroupID: 99, Platform: future.ID, Mode: SchedulerModeForced})
	requireCompatibleServiceIdentity(t, future.ID)
	// Supplier presets are optional (F2); adding a platform must not fabricate one.
	t.Run("candidate presets", TestPlatformModelPresetCandidateSchema)
	t.Run("trusted presets", TestPlatformModelPresetCoverageAndIsolation)
	presets, err := PlatformModelPresets()
	require.NoError(t, err)
	for _, preset := range presets {
		require.NotEqual(t, future.ID, preset.Platform)
	}
	account := &Account{Platform: future.ID, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true}
	require.False(t, AccountSupportsResolvedModel(context.Background(), account, "gpt-4o", AccountModelBindingOptions{}))
}
