package repository

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestSessionFamilyRevocationSurvivesRefreshDeletion(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	cache := NewRefreshTokenCache(client)
	revocations, ok := cache.(service.SessionFamilyRevocationCache)
	require.True(t, ok)
	ctx := context.Background()
	require.NoError(t, revocations.RevokeSessionFamily(ctx, "family", time.Hour))
	require.NoError(t, cache.DeleteTokenFamily(ctx, "family"))
	revoked, err := revocations.IsSessionFamilyRevoked(ctx, "family")
	require.NoError(t, err)
	require.True(t, revoked)
	server.FastForward(2 * time.Hour)
	revoked, err = revocations.IsSessionFamilyRevoked(ctx, "family")
	require.NoError(t, err)
	require.False(t, revoked)
}
