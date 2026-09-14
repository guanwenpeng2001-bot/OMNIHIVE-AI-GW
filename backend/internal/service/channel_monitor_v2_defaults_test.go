//go:build unit

package service

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestCursorMonitorDefaultSeedPreservesOperatorChoices(t *testing.T) {
	var platforms []ChannelMonitorV2PlatformConfig
	require.NoError(t, json.Unmarshal(preCursorFactoryPlatformSeed, &platforms))
	cfg := ChannelMonitorV2Config{Platforms: platforms}
	SeedCursorDimensionIntoFactoryConfig(&cfg)
	require.Len(t, cfg.Platforms, len(platforms)+1)
	require.Equal(t, PlatformCursor, cfg.Platforms[len(platforms)].Platform)
	SeedCursorDimensionIntoFactoryConfig(&cfg)
	require.Len(t, cfg.Platforms, len(platforms)+1)
	actor := int64(1)
	cfg = ChannelMonitorV2Config{Platforms: platforms, UpdatedBy: &actor}
	SeedCursorDimensionIntoFactoryConfig(&cfg)
	require.Len(t, cfg.Platforms, len(platforms))
	cfg = ChannelMonitorV2Config{Platforms: platforms}
	cfg.Platforms[0].Enabled = false
	SeedCursorDimensionIntoFactoryConfig(&cfg)
	require.Len(t, cfg.Platforms, len(platforms))
}
