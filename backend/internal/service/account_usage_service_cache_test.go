package service

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestUsageCacheCapacityExpiryAndIdentity(t *testing.T) {
	cache := NewUsageCache()
	families := []struct {
		name    string
		entries *sync.Map
		value   func(time.Time) any
		ttl     time.Duration
	}{
		{"cursor", &cache.cursorCache, func(stamp time.Time) any { return &antigravityUsageCache{timestamp: stamp, usageInfo: &UsageInfo{}} }, apiCacheTTL},
		{"api", &cache.apiCache, func(stamp time.Time) any { return &apiUsageCache{timestamp: stamp, response: &ClaudeUsageResponse{}} }, apiCacheTTL},
		{"window", &cache.windowStatsCache, func(stamp time.Time) any { return &windowStatsCache{timestamp: stamp, stats: &WindowStats{}} }, windowStatsCacheTTL},
		{"antigravity", &cache.antigravityCache, func(stamp time.Time) any { return &antigravityUsageCache{timestamp: stamp, usageInfo: &UsageInfo{}} }, apiCacheTTL},
		{"openai", &cache.openAIProbeCache, func(stamp time.Time) any { return stamp }, openAIProbeCacheTTL},
		{"grok", &cache.grokProbeCache, func(stamp time.Time) any { return stamp }, grokProbeRetryTTL},
	}
	for _, family := range families {
		t.Run(family.name, func(t *testing.T) {
			now := time.Now()
			retained := family.value(now.Add(-time.Second))
			cache.store(family.entries, "oldest", retained)
			loaded, found := cache.load(family.entries, "oldest")
			require.True(t, found)
			require.Equal(t, retained, loaded)
			for index := 0; index < usageCacheLimit; index++ {
				cache.store(family.entries, fmt.Sprint(index), family.value(now))
			}
			_, found = family.entries.Load("oldest")
			require.False(t, found)
			count := 0
			family.entries.Range(func(_, _ any) bool { count++; return true })
			require.Equal(t, usageCacheLimit, count)
			// An already returned result remains usable after its map entry is evicted.
			require.Equal(t, retained, loaded)
			cache.store(family.entries, "0", family.value(now))
			count = 0
			family.entries.Range(func(_, _ any) bool { count++; return true })
			require.Equal(t, usageCacheLimit, count, "replacement must not evict a second key")
			// Seed expired entries without sleeping; a write must sweep them all.
			family.entries.Range(func(key, _ any) bool {
				family.entries.Store(key, family.value(now.Add(-family.ttl-time.Second)))
				return true
			})
			account := &Account{ID: 42, Credentials: map[string]any{"api_key": "old-secret"}}
			key := AccountRuntimeIdentityKey(account)
			cache.store(family.entries, key, family.value(now))
			count = 0
			family.entries.Range(func(_, _ any) bool { count++; return true })
			require.Equal(t, 1, count)
			_, found = cache.load(family.entries, key)
			require.True(t, found)
			changed := *account
			changed.Credentials = map[string]any{"api_key": "new-secret"}
			_, found = cache.load(family.entries, AccountRuntimeIdentityKey(&changed))
			require.False(t, found)
			cache.InvalidateAccount(account.ID)
			_, found = cache.load(family.entries, key)
			require.False(t, found)
			family.entries.Store(key, family.value(now.Add(-family.ttl-time.Second)))
			_, found = cache.load(family.entries, key)
			require.False(t, found)
			_, found = family.entries.Load(key)
			require.False(t, found, "read removes expired reference")
		})
	}
}

func TestUsageCacheNegativeTTLPolicies(t *testing.T) {
	cache := NewUsageCache()
	stamp := time.Now().Add(-2 * time.Minute)
	cases := []struct {
		name    string
		entries *sync.Map
		value   any
		found   bool
	}{
		{"api-negative", &cache.apiCache, &apiUsageCache{timestamp: stamp, err: fmt.Errorf("rate limited")}, false},
		{"cursor-negative", &cache.cursorCache, &antigravityUsageCache{timestamp: stamp, usageInfo: &UsageInfo{ErrorCode: "retry"}}, false},
		{"cursor-error-text", &cache.cursorCache, &antigravityUsageCache{timestamp: stamp, usageInfo: &UsageInfo{Error: "text-only"}}, true},
		{"antigravity-transient", &cache.antigravityCache, &antigravityUsageCache{timestamp: stamp, usageInfo: &UsageInfo{Error: "retry"}}, false},
		{"antigravity-forbidden", &cache.antigravityCache, &antigravityUsageCache{timestamp: stamp, usageInfo: &UsageInfo{IsForbidden: true, ErrorCode: "forbidden"}}, true},
	}
	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			cache.store(scenario.entries, scenario.name, scenario.value)
			_, found := cache.load(scenario.entries, scenario.name)
			require.Equal(t, scenario.found, found)
		})
	}
}

func TestUsageCacheConcurrentAdmissionIsBounded(t *testing.T) {
	cache := NewUsageCache()
	var writers sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		writers.Add(1)
		go func(worker int) {
			defer writers.Done()
			for index := 0; index < 200; index++ {
				key := fmt.Sprintf("%d:%d", worker, index)
				cache.store(&cache.openAIProbeCache, key, time.Now())
				cache.load(&cache.openAIProbeCache, key)
			}
		}(worker)
	}
	writers.Wait()
	count := 0
	cache.openAIProbeCache.Range(func(_, _ any) bool { count++; return true })
	require.Equal(t, usageCacheLimit, count)
}

func TestUsageCacheProbeAdmissionKeepsIdentityAndForceSemantics(t *testing.T) {
	service := &AccountUsageService{cache: NewUsageCache()}
	now := time.Now()
	account := &Account{ID: 42, Credentials: map[string]any{"access_token": "first"}}
	require.True(t, service.shouldProbeOpenAICodexSnapshot(account, now))
	require.False(t, service.shouldProbeOpenAICodexSnapshot(account, now.Add(time.Second)))
	require.True(t, service.shouldProbeOpenAICodexSnapshot(account, now.Add(2*time.Second), true))
	key := AccountRuntimeIdentityKey(account)
	stamp, found := service.cache.openAIProbeCache.Load(key)
	require.True(t, found)
	require.Equal(t, now.Add(2*time.Second), stamp, "OpenAI force refreshes the attempt timestamp")
	require.True(t, service.shouldProbeGrokBilling(account, now, false))
	require.False(t, service.shouldProbeGrokBilling(account, now.Add(time.Second), false))
	require.True(t, service.shouldProbeGrokBilling(account, now.Add(2*time.Second), true))
	stamp, found = service.cache.grokProbeCache.Load(key)
	require.True(t, found)
	require.Equal(t, now, stamp, "Grok force retains its bypass semantics")
	changed := *account
	changed.Credentials = map[string]any{"access_token": "second"}
	require.True(t, service.shouldProbeOpenAICodexSnapshot(&changed, now))
	require.True(t, service.shouldProbeGrokBilling(&changed, now, false))
	for index := 0; index < usageCacheLimit; index++ {
		fresh := &Account{ID: int64(index + 100)}
		require.True(t, service.shouldProbeOpenAICodexSnapshot(fresh, now))
		require.True(t, service.shouldProbeGrokBilling(fresh, now, false))
	}
	for _, entries := range []*sync.Map{&service.cache.openAIProbeCache, &service.cache.grokProbeCache} {
		count := 0
		entries.Range(func(_, _ any) bool { count++; return true })
		require.Equal(t, usageCacheLimit, count, "production probe admission is bounded")
	}
}
