//go:build unit

package service

import (
	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestMonitorAndQuotaPlatformAuthorities(t *testing.T) {
	concrete := model.AllPlatforms()
	require.ElementsMatch(t, concrete, AllowedQuotaPlatforms)
	expectedMonitors := make([]string, 0, len(concrete)-1)
	for _, platform := range concrete {
		if platform != PlatformCursor && platform != PlatformBedrock && !IsChatOnlyPlatform(platform) {
			expectedMonitors = append(expectedMonitors, platform)
		}
	}
	actual := make([]string, 0)
	for _, platform := range ConcretePlatforms() {
		if validateProvider(platform) == nil {
			actual = append(actual, platform)
		}
	}
	require.ElementsMatch(t, expectedMonitors, actual)
}
