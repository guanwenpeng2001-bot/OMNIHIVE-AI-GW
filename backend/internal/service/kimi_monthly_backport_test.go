package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestKimiMonthlyParsingBackport(t *testing.T) {
	body := `{"usages":{"limit_5h":{"used_ratio":0},"limit_7d":{"used_ratio":"0.25"},"limit_month_total":{"used_ratio":1,"reset_time":"2026-12-01T00:00:00Z"},"limit_month_code":{"used_ratio":0.5,"reset_time":"2026-12-04T00:00:00Z"}},"usage":{"limit":10,"remaining":0}}`
	tiers := parseKimiUsageTiers([]byte(body))
	require.Equal(t, []CNQuotaTier{
		{Window: "5h", UsedPercent: 0}, {Window: "weekly", UsedPercent: 25},
		{Window: "monthly", UsedPercent: 100, ResetAt: "2026-12-04T00:00:00Z"},
	}, tiers)
	for _, key := range []string{"limit_month_total", "limit_month_code"} {
		tiers := parseKimiUsageTiers([]byte(`{"usages":{"` + key + `":{"used_ratio":0.5}}}`))
		require.Equal(t, []CNQuotaTier{{Window: "monthly", UsedPercent: 50}}, tiers)
	}
}

func TestKimiInvalidWindowsBackport(t *testing.T) {
	for _, body := range []string{
		`{}`, `{"limits":[{"detail":{}}]}`, `{"usage":{"limit":0,"remaining":0}}`,
		`{"usage":{"limit":100,"remaining":-1}}`, `{"usage":{"limit":100,"remaining":101}}`,
		`{"usage":{"limit":"NaN","remaining":1}}`,
		`{"usages":{"limit_month_total":{"used_ratio":1}}`,
		`{"usages":{"limit_month_total":{"used_ratio":1}}} trailing`,
	} {
		t.Run(body, func(t *testing.T) { require.Empty(t, parseKimiUsageTiers([]byte(body))) })
	}
	for _, value := range []string{`null`, `"NaN"`, `"Inf"`, `-0.1`, `1.1`, `{}`, `true`} {
		// A malformed advertised window must not be treated as an absent one,
		// even when another window was parsed successfully.
		t.Run(value, func(t *testing.T) {
			require.Empty(t, parseKimiUsageTiers([]byte(`{"usages":{"limit_5h":{"used_ratio":0.1},"limit_month_total":{"used_ratio":`+value+`}}}`)))
		})
	}
}

type kimiSnapshotBackportRepo struct {
	AccountRepository
	writes  int
	updates map[string]any
	err     error
}

func (r *kimiSnapshotBackportRepo) UpdateAccountRuntimeSnapshot(_ context.Context, _ *Account, updates map[string]any, _ *time.Time) error {
	r.writes++
	r.updates = updates
	return r.err
}

func TestKimiProbeSnapshotBackport(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		success    bool
	}{
		{"new monthly", `{"usages":{"limit_month_code":{"used_ratio":1,"reset_time":"2099-12-04T00:00:00Z"}}}`, 200, true},
		{"empty", `{}`, 200, false},
		{"malformed legacy", `{"usage":{}}`, 200, false},
		{"mixed invalid", `{"usages":{"limit_5h":{"used_ratio":0},"limit_month_code":{"used_ratio":"NaN"}}}`, 200, false},
		{"rate limited", `{}`, 429, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := &Account{ID: 123, Platform: PlatformKimi, Type: AccountTypeAPIKey,
				Credentials: map[string]any{"account_mode": "coding", "api_key": "test-only", "base_url": "https://api.kimi.com/coding/v1"},
				Extra:       map[string]any{"kimi_weekly_used_percent": float64(100), "kimi_weekly_reset_at": "2099-12-01T00:00:00Z", "kimi_usage_updated_at": "2026-11-01T00:00:00Z"}}
			before, err := json.Marshal(account.Extra)
			require.NoError(t, err)
			repo := &kimiSnapshotBackportRepo{}
			result, err := NewCNProviderQuotaService(repo, nil, &cnBalanceResponseUpstream{statusCode: tc.status, body: tc.body}, nil).QueryUsageForAccount(context.Background(), account)
			require.NoError(t, err)
			require.Equal(t, tc.success, result.Success)
			require.Equal(t, tc.success, result.Persisted)
			if !tc.success {
				require.Zero(t, repo.writes)
				require.NotEmpty(t, result.Error)
				after, err := json.Marshal(account.Extra)
				require.NoError(t, err)
				require.JSONEq(t, string(before), string(after))
				return
			}
			require.Equal(t, 1, repo.writes)
			for _, key := range []string{"kimi_5h_used_percent", "kimi_5h_reset_at", "kimi_weekly_used_percent", "kimi_weekly_reset_at"} {
				require.Contains(t, repo.updates, key)
				require.Nil(t, repo.updates[key])
			}
			require.Equal(t, float64(100), repo.updates["kimi_monthly_used_percent"])
			require.NotEqual(t, account.Extra["kimi_usage_updated_at"], repo.updates["kimi_usage_updated_at"])
			account.Extra = repo.updates
			now := time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC)
			candidate := pickLatestResetSchedulingCandidate(cnProviderThresholdCandidates(account, PlatformKimi), 95, now)
			require.NotNil(t, candidate)
			require.Equal(t, "monthly", candidate.window)
			require.Equal(t, candidate.until, cnProviderQuotaSnapshotReset(account, now))
		})
	}
}

func TestKimiIdentityChangeBackport(t *testing.T) {
	account := &Account{ID: 124, Platform: PlatformKimi, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"account_mode": "coding", "api_key": "test-only", "base_url": "https://api.kimi.com/coding/v1"}}
	repo := &kimiSnapshotBackportRepo{err: ErrAccountRuntimeIdentityChanged}
	result, err := NewCNProviderQuotaService(repo, nil, &cnBalanceResponseUpstream{statusCode: 200,
		body: `{"usages":{"limit_month_total":{"used_ratio":1}}}`}, nil).QueryUsageForAccount(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, 1, repo.writes)
	require.False(t, result.Success)
	require.False(t, result.Persisted)
	require.Equal(t, "identity_changed", result.Status)
	require.Empty(t, result.Tiers)
}

func TestKimiLegacyTransitionBackport(t *testing.T) {
	tiers := parseKimiUsageTiers([]byte(`{"limits":[{"detail":{"limit":100,"remaining":100}}],"usage":{"limit":1000,"remaining":750}}`))
	require.Equal(t, []CNQuotaTier{{Window: "5h", UsedPercent: 0}, {Window: "weekly", UsedPercent: 25}}, tiers)
	updates := cnQuotaExtraUpdates(PlatformKimi, tiers, time.Now())
	require.Contains(t, updates, "kimi_monthly_used_percent")
	require.Nil(t, updates["kimi_monthly_used_percent"])
	require.Nil(t, updates["kimi_monthly_reset_at"])
	// Other providers keep their existing snapshot merge policy.
	other := cnQuotaExtraUpdates(PlatformZhipu, tiers, time.Now())
	require.NotContains(t, other, "zhipu_monthly_used_percent")
	require.NotContains(t, other, "zhipu_5h_reset_at")
}

func TestKimiSnapshotAbsentResetBackport(t *testing.T) {
	updates := cnQuotaExtraUpdates(PlatformKimi, []CNQuotaTier{{Window: "5h", UsedPercent: 25}}, time.Now())
	for _, key := range []string{"kimi_5h_reset_at", "kimi_weekly_used_percent", "kimi_weekly_reset_at", "kimi_monthly_used_percent", "kimi_monthly_reset_at"} {
		require.Contains(t, updates, key)
		require.Nil(t, updates[key])
	}
	require.Empty(t, cnQuotaExtraUpdates(PlatformKimi, nil, time.Now()), "empty observations must not advance freshness or clear snapshots")
}
