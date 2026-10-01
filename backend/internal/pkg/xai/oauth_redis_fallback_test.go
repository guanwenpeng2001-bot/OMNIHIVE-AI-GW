//go:build unit

package xai

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestSessionStoreRedisFallbackIsLimitedToFailedWrites(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	store := NewRedisSessionStore(client)
	defer store.Stop()
	session := func(state string) *OAuthSession { return &OAuthSession{State: state, CreatedAt: time.Now()} }

	store.Set("remote", session("remote"))
	require.NoError(t, store.remote.Delete(context.Background(), "remote"))
	_, ok := store.Get("remote")
	require.False(t, ok, "a remote miss must not revive the stale local copy")

	mr.Close()
	store.Set("local-only", session("local"))
	got, ok := store.Get("local-only")
	require.True(t, ok)
	require.Equal(t, "local", got.State)
	require.True(t, store.TryConsumeSession("local-only"))
	require.False(t, store.TryConsumeSession("local-only"))
}

func TestSessionStoreRedisPreservesServerEgressAcrossReplicas(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	first, second := NewRedisSessionStore(client), NewRedisSessionStore(client)
	defer first.Stop()
	defer second.Stop()
	selection := &domain.OAuthEgressSelection{GroupID: 7, WorkspaceID: 1, ProxyID: 11, URL: "http://fixture.invalid:8080", Source: "group_binding"}
	first.Set("group-session", &OAuthSession{State: "fixture-state", CodeVerifier: "fixture-verifier", CreatedAt: time.Now(), ProxyURL: selection.URL, Egress: selection})
	loaded, ok := second.Get("group-session")
	require.True(t, ok)
	require.Equal(t, selection, loaded.Egress)
	require.Equal(t, selection.URL, loaded.ProxyURL)
	require.True(t, second.TryConsumeSession("group-session"))
	require.False(t, first.TryConsumeSession("group-session"))
}
