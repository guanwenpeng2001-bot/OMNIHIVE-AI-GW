package handler

import (
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestNewPlatformModelDefaultsDoNotInventClaude(test *testing.T) {
	for _, platform := range []string{service.PlatformVolcengine, service.PlatformQianfan, service.PlatformHunyuan, service.PlatformMistral, service.PlatformBedrock} {
		require.Empty(test, defaultModelIDsForPlatform(platform), platform)
		require.Empty(test, defaultCodexModelIDsForPlatform(platform), platform)
	}
}
