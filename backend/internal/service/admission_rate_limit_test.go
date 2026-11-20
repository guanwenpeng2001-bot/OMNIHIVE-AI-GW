//go:build unit

package service

import (
	"net/http"
	"testing"
	"time"

	infraerrors "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestResolveEffectiveRPMLimits_Priority(t *testing.T) {
	override := 7

	t.Run("override replaces the group capacity", func(t *testing.T) {
		limits := ResolveEffectiveRPMLimits(100, 11, 22, 50, &override)
		require.Len(t, limits, 2)
		require.Equal(t, AdmissionRateLimitSourceUserGroupOverride, limits[0].Source)
		require.Equal(t, 7, limits[0].Limit)
		require.Equal(t, int64(22), limits[0].ScopeID)
		require.Equal(t, AdmissionRateLimitSourceUser, limits[1].Source)
		require.Equal(t, 100, limits[1].Limit,
			"the user ceiling must stay in force no matter what the override says")
	})

	t.Run("group capacity applies when no override exists", func(t *testing.T) {
		limits := ResolveEffectiveRPMLimits(100, 11, 22, 50, nil)
		require.Len(t, limits, 2)
		require.Equal(t, AdmissionRateLimitSourceGroup, limits[0].Source)
		require.Equal(t, 50, limits[0].Limit)
		require.Equal(t, AdmissionRateLimitSourceUser, limits[1].Source)
	})

	t.Run("zero override exempts the group dimension only", func(t *testing.T) {
		zero := 0
		limits := ResolveEffectiveRPMLimits(100, 11, 22, 50, &zero)
		require.Len(t, limits, 1)
		require.Equal(t, AdmissionRateLimitSourceUser, limits[0].Source,
			"override=0 exempts the group but never the user ceiling")
	})

	t.Run("no configured limit yields an empty list", func(t *testing.T) {
		require.Empty(t, ResolveEffectiveRPMLimits(0, 11, 22, 0, nil))
	})

	t.Run("window length matches the counter window", func(t *testing.T) {
		limits := ResolveEffectiveRPMLimits(60, 1, 2, 0, nil)
		require.Equal(t, 60, limits[0].WindowSeconds)
	})
}

func TestRPMWindowResetAt_IsTheNextMinuteBoundary(t *testing.T) {
	now := time.Date(2026, 9, 21, 10, 30, 41, 500*int(time.Millisecond), time.UTC)
	resetAt := RPMWindowResetAt(now)
	require.Equal(t, time.Date(2026, 9, 21, 10, 31, 0, 0, time.UTC), resetAt)
	require.Equal(t, 19, RetryAfterSecondsUntil(resetAt, now), "sub-second remainder rounds up")
}

func TestRetryAfterSecondsUntil_NoEvidenceYieldsZero(t *testing.T) {
	now := time.Now()
	require.Zero(t, RetryAfterSecondsUntil(time.Time{}, now), "no window means no Retry-After")
	require.Zero(t, RetryAfterSecondsUntil(now.Add(-time.Second), now), "an elapsed window has no Retry-After")
}

func TestApplyRateLimitHeaders_OnlyRealWindowFacts(t *testing.T) {
	now := time.Now()

	t.Run("real window yields Retry-After plus limit headers", func(t *testing.T) {
		header := http.Header{}
		ApplyRateLimitHeaders(header, RateLimitHeaderValues{
			Limit:     60,
			Remaining: 0,
			ResetAt:   now.Add(30 * time.Second),
		}, now)
		require.Equal(t, "30", header.Get("Retry-After"))
		require.Equal(t, "30", header.Get("X-RateLimit-Reset-Requests"))
		require.Equal(t, "60", header.Get("X-RateLimit-Limit-Requests"))
		require.Equal(t, "0", header.Get("X-RateLimit-Remaining-Requests"))
	})

	t.Run("no reset evidence emits no retry hint", func(t *testing.T) {
		header := http.Header{}
		ApplyRateLimitHeaders(header, RateLimitHeaderValues{}, now)
		require.Empty(t, header.Get("Retry-After"))
		require.Empty(t, header.Get("X-RateLimit-Reset-Requests"))
		require.Empty(t, header.Get("X-RateLimit-Limit-Requests"))
	})

	t.Run("remaining never goes negative or above the limit", func(t *testing.T) {
		header := http.Header{}
		ApplyRateLimitHeaders(header, RateLimitHeaderValues{Limit: 10, Remaining: -3, ResetAt: now.Add(time.Second)}, now)
		require.Equal(t, "0", header.Get("X-RateLimit-Remaining-Requests"))

		over := http.Header{}
		ApplyRateLimitHeaders(over, RateLimitHeaderValues{Limit: 10, Remaining: 999, ResetAt: now.Add(time.Second)}, now)
		require.Equal(t, "10", over.Get("X-RateLimit-Remaining-Requests"))
	})
}

func TestAdmissionRateLimitFacts_NeverLeaksUpstreamAccountQuota(t *testing.T) {
	// 反例：只有上游账号维度的键时，不产出任何客户端可见事实。
	upstreamOnly := infraerrors.TooManyRequests("UPSTREAM_ACCOUNT_QUOTA", "upstream account quota exhausted")
	upstreamOnly = upstreamOnly.WithMetadata(map[string]string{
		"upstream_account_remaining": "0",
		"upstream_account_limit":     "1000",
		"x-ratelimit-remaining":      "0",
	})
	facts, ok := AdmissionRateLimitFacts(upstreamOnly)
	require.False(t, ok, "upstream account quota must not become a client-visible rate limit fact")
	require.Zero(t, facts.Limit)

	// 正例：只有用户/组窗口证据时才产出事实，且不包含任何上游字段。
	userScoped := ErrGroupRPMExceeded.WithMetadata(map[string]string{
		AdmissionWindowResetsAtMetadataKey: time.Now().Add(20 * time.Second).UTC().Format(time.RFC3339),
		AdmissionRateLimitLimitKey:         "60",
		AdmissionRateLimitUsedKey:          "61",
	})
	facts, ok = AdmissionRateLimitFacts(userScoped)
	require.True(t, ok)
	require.Equal(t, 60, facts.Limit)
	require.Equal(t, -1, facts.Remaining)

	header := http.Header{}
	ApplyRateLimitHeaders(header, facts, time.Now())
	require.Equal(t, "60", header.Get("X-RateLimit-Limit-Requests"))
	require.Equal(t, "0", header.Get("X-RateLimit-Remaining-Requests"))
	require.Empty(t, header.Get("upstream_account_remaining"))
	require.Empty(t, header.Get("X-RateLimit-Remaining-Tokens"))
}

func TestAntigravityModelRateLimitKeys_ThinkingAliasSharesOneDomain(t *testing.T) {
	base := antigravityModelRateLimitDomain("claude-sonnet-4-5")
	alias := antigravityModelRateLimitDomain("claude-sonnet-4-5-thinking")
	require.Equal(t, "claude-sonnet-4-5", base)
	require.Equal(t, base, alias,
		"the thinking variant is the same upstream model, so one cooldown covers both spellings")
	require.Equal(t, []string{"claude-sonnet-4-5"}, antigravityModelRateLimitKeys("claude-sonnet-4-5-thinking"))

	read := antigravityModelRateLimitReadKeys("claude-sonnet-4-5-thinking")
	require.Contains(t, read, "claude-sonnet-4-5",
		"the canonical domain is read first")
	require.Contains(t, read, "claude-sonnet-4-5-thinking",
		"pre-existing rows written under the alias spelling stay effective")
}

func TestAntigravityModelRateLimitKeys_RealThinkingModelsKeepTheirOwnScope(t *testing.T) {
	// claude-opus-4-6-thinking 是真实的不同上游模型，不是别名。
	require.Equal(t, "claude-opus-4-6-thinking", antigravityModelRateLimitDomain("claude-opus-4-6-thinking"))
}

func TestAntigravityModelRateLimitKeys_GeminiHasNoFabricatedFamilyScope(t *testing.T) {
	// 单模型证据只证明这一个模型：不得再推断出整族 Gemini 的冷却。
	keys := antigravityModelRateLimitKeys("gemini-3-pro-high")
	require.Equal(t, []string{"gemini-3-pro-high"}, keys)
	for _, key := range keys {
		require.NotContains(t, key, "antigravity:", "no family scope may be derived from per-model evidence")
	}

	// 读路径同样只有该模型自己的域，另一个 Gemini 模型不受连坐。
	read := antigravityModelRateLimitReadKeys("gemini-3-pro-high")
	require.Equal(t, []string{"gemini-3-pro-high"}, read)

	account := &Account{
		Platform: PlatformAntigravity,
		Extra: map[string]any{
			modelRateLimitsKey: map[string]any{
				"gemini-3-pro-high": map[string]any{
					"rate_limit_reset_at": time.Now().Add(10 * time.Minute).Format(time.RFC3339),
				},
			},
		},
	}
	require.True(t, account.isModelRateLimitedWithContext(t.Context(), "gemini-3-pro-preview"))
	require.False(t, account.isModelRateLimitedWithContext(t.Context(), "gemini-3-flash"),
		"a per-model cooldown must not punish the rest of the Gemini family")
}
