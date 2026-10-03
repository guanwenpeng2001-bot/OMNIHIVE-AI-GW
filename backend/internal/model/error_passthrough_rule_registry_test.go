//go:build unit

package model

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestAllPlatformsTracksRegistryGrowth(t *testing.T) {
	before := AllPlatforms()
	providers := domain.CompatibleProviders()
	require.NotEmpty(t, providers)
	added := providers[0]
	added.ID = "passthrough_registry_fixture"
	providers = append(providers, added)
	restore := domain.SetCompatibleProvidersForTest(providers)
	t.Cleanup(restore)
	require.ElementsMatch(t, append(before, added.ID), AllPlatforms())
	require.True(t, domain.IsConcretePlatform(added.ID))
	restore()
	require.ElementsMatch(t, before, AllPlatforms())
	require.False(t, domain.IsConcretePlatform(added.ID))
}
