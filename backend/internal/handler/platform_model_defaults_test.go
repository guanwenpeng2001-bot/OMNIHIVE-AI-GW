package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestNewPlatformModelDefaultsDoNotInventClaude(test *testing.T) {
	for _, platform := range []string{service.PlatformVolcengine, service.PlatformQianfan, service.PlatformHunyuan, service.PlatformMistral, service.PlatformBedrock} {
		require.Empty(test, defaultModelIDsForPlatform(platform), platform)
		require.Empty(test, defaultCodexModelIDsForPlatform(platform), platform)
	}
}
