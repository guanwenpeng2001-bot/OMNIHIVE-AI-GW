package handler

import (
	"context"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

func TestResolveOpsPlatformPrefersResolvedCompositeTarget(t *testing.T) {
	apiKey := &service.APIKey{Group: &service.Group{Platform: service.PlatformComposite}}
	ctx := service.WithResolvedTargetPlatform(context.Background(), service.PlatformOpenAI)

	require.Equal(t, service.PlatformOpenAI, resolveOpsPlatform(ctx, apiKey, service.PlatformAnthropic))
}
