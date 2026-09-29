package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestModelsDevCacheSingleSlotIdentityAndExpiry(t *testing.T) {
	t.Setenv("MODEL_CAPABILITY_OFFLINE", "false")
	upstream := &capabilityRegistryHTTP{release: make(chan struct{})}
	close(upstream.release)
	service := &AccountTestService{httpUpstream: upstream}
	first := &Account{ID: 1}
	registry, err := service.fetchModelsDevRegistry(context.Background(), first)
	require.NoError(t, err)
	require.NotEmpty(t, registry)
	_, err = service.fetchModelsDevRegistry(context.Background(), first)
	require.NoError(t, err)
	require.Equal(t, int32(1), upstream.calls.Load())
	proxyID := int64(7)
	second := &Account{ID: 2, ProxyID: &proxyID, Proxy: &Proxy{Protocol: "http", Host: "other-proxy", Port: 8080}}
	_, err = service.fetchModelsDevRegistry(context.Background(), second)
	require.NoError(t, err)
	require.Equal(t, int32(2), upstream.calls.Load())
	firstKey := service.modelsDevDownloadSnapshot(first).downloadKey
	secondKey := service.modelsDevDownloadSnapshot(second).downloadKey
	require.NotEqual(t, firstKey, secondKey)
	missing, _ := service.boundedModelsDevRegistry(firstKey)
	require.Nil(t, missing, "single slot replaces rather than retains prior identity")
	require.Equal(t, secondKey, service.modelMetadataRegistryKey)
	require.NotEmpty(t, registry, "prior caller retains usable data after replacement")
	service.modelMetadataRegistryAt = time.Now().Add(-modelsDevRegistryTTL - time.Hour)
	stale, loaded := service.boundedModelsDevRegistry(secondKey)
	require.NotEmpty(t, stale)
	require.Equal(t, service.modelMetadataRegistryAt, loaded, "reads do not renew freshness")
	service.modelMetadataRegistryAt = time.Now().Add(-modelsDevRegistryTTL - modelsDevStaleGrace - time.Second)
	missing, _ = service.boundedModelsDevRegistry(firstKey)
	require.Nil(t, missing)
	require.Nil(t, service.modelMetadataRegistry)
	require.Empty(t, service.modelMetadataRegistryKey)
	require.True(t, service.modelMetadataRegistryAt.IsZero())
	require.NotEmpty(t, stale, "expired reference remains usable by an existing reader")
}

func TestModelsDevCacheExpiryPreservesDownload(t *testing.T) {
	t.Setenv("MODEL_CAPABILITY_OFFLINE", "false")
	upstream := &capabilityRegistryHTTP{start: make(chan struct{}, 1), release: make(chan struct{})}
	service := &AccountTestService{httpUpstream: upstream}
	account := &Account{}
	key := service.modelsDevDownloadSnapshot(account).downloadKey
	service.modelMetadataRegistryKey = key
	service.modelMetadataRegistryAt = time.Now().Add(-modelsDevRegistryTTL - modelsDevStaleGrace - time.Second)
	service.modelMetadataRegistry = map[string]modelsDevProvider{"old": {Version: "expired"}}
	result := make(chan error, 1)
	go func() {
		registry, err := service.fetchModelsDevRegistry(context.Background(), account)
		if err == nil && len(registry) == 0 {
			err = fmt.Errorf("download returned no registry")
		}
		result <- err
	}()
	select {
	case <-upstream.start:
	case <-time.After(5 * time.Second):
		t.Fatal("download did not start")
	}
	cached, _ := service.boundedModelsDevRegistry(key)
	require.Nil(t, cached)
	close(upstream.release)
	select {
	case err := <-result:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("eviction interrupted the active download")
	}
	cached, _ = service.boundedModelsDevRegistry(key)
	require.NotEmpty(t, cached)
	require.Equal(t, int32(1), upstream.calls.Load())
}

func TestModelsDevCacheStaleFailureDoesNotRenewLifetime(t *testing.T) {
	t.Setenv("MODEL_CAPABILITY_OFFLINE", "false")
	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 429, Body: io.NopCloser(strings.NewReader("rate limited"))}}
	key := ModelRegistryDownloadKey(modelsDevRegistryURL, "", "verified-tls")
	stamp := time.Now().Add(-modelsDevRegistryTTL - time.Hour)
	service := &AccountTestService{httpUpstream: upstream, modelMetadataRegistryKey: key, modelMetadataRegistryAt: stamp, modelMetadataRegistry: map[string]modelsDevProvider{"openai": {Version: "retained"}}}
	registry, err := service.fetchModelsDevRegistry(context.Background(), &Account{})
	require.Error(t, err)
	require.True(t, registry["openai"].Stale)
	require.Equal(t, stamp, service.modelMetadataRegistryAt)
	require.False(t, service.modelMetadataRegistry["openai"].Stale)
}

func TestUpstreamRuntimeCooldownCapacityExpiryAndIdentity(t *testing.T) {
	runtimeCooldownFallback.Lock()
	saved := runtimeCooldownFallback.entries
	runtimeCooldownFallback.entries = make(map[string]time.Time)
	runtimeCooldownFallback.Unlock()
	t.Cleanup(func() {
		runtimeCooldownFallback.Lock()
		runtimeCooldownFallback.entries = saved
		runtimeCooldownFallback.Unlock()
	})
	service := &AntigravityGatewayService{}
	ctx := context.Background()
	account := &Account{ID: 42, Credentials: map[string]any{"api_key": "first"}}
	key := AccountRuntimeCooldownKey(7, account, "actual-model")
	until := time.Now().Add(time.Minute)
	service.setRuntimeCooldown(ctx, key, until)
	cooldownUntil, recorded := service.runtimeCooldown(ctx, key)
	require.Equal(t, until, cooldownUntil)
	require.True(t, recorded)
	other := *account
	other.Credentials = map[string]any{"api_key": "replacement"}
	_, otherRecorded := service.runtimeCooldown(ctx, AccountRuntimeCooldownKey(7, &other, "actual-model"))
	require.False(t, otherRecorded)
	_, groupRecorded := service.runtimeCooldown(ctx, AccountRuntimeCooldownKey(8, account, "actual-model"))
	require.False(t, groupRecorded)
	_, modelRecorded := service.runtimeCooldown(ctx, AccountRuntimeCooldownKey(7, account, "other-model"))
	require.False(t, modelRecorded)
	for index := 0; index < runtimeCooldownFallbackLimit; index++ {
		service.setRuntimeCooldown(ctx, fmt.Sprint(index), until.Add(time.Minute))
	}
	runtimeCooldownFallback.Lock()
	count := len(runtimeCooldownFallback.entries)
	_, retained := runtimeCooldownFallback.entries[key]
	for cachedKey := range runtimeCooldownFallback.entries {
		runtimeCooldownFallback.entries[cachedKey] = time.Now().Add(-time.Second)
	}
	runtimeCooldownFallback.Unlock()
	require.Equal(t, runtimeCooldownFallbackLimit, count)
	require.False(t, retained, "earliest expiry is evicted at capacity")
	service.setRuntimeCooldown(ctx, key, until)
	runtimeCooldownFallback.Lock()
	count = len(runtimeCooldownFallback.entries)
	runtimeCooldownFallback.entries[key] = time.Now().Add(-time.Second)
	runtimeCooldownFallback.Unlock()
	require.Equal(t, 1, count, "write sweeps all expired entries")
	expiredUntil, expiredRecorded := service.runtimeCooldown(ctx, key)
	require.False(t, expiredRecorded, "read removes expired entry")
	require.True(t, expiredUntil.IsZero())
	runtimeCooldownFallback.Lock()
	count = len(runtimeCooldownFallback.entries)
	runtimeCooldownFallback.Unlock()
	require.Zero(t, count, "read removes expired entry")
}
