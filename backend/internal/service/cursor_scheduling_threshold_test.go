//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCursorSchedulingThresholdPersistsPauseUntilBillingReset(t *testing.T) {
	svc := newSettingServiceForPlatformThresholdTest(map[string]string{SettingKeyAccountSchedulingThresholds: `{"cursor":80}`})
	repo := &rateLimitAccountRepoStub{}
	rl := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	rl.SetSettingService(svc)
	end := time.Now().UTC().Add(time.Hour)
	percent := 80.0
	account := &Account{ID: 902, Platform: PlatformCursor, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true,
		Extra: map[string]any{"cursor_usage_snapshot": &CursorMonthlyUsage{PlanUsage: &CursorPlanUsage{TotalPercentUsed: &percent}, ResetsAt: &end}}}
	require.True(t, rl.ApplyAccountSchedulingThreshold(context.Background(), account))
	require.Equal(t, 1, repo.tempCalls)
	require.Equal(t, end, *account.TempUnschedulableUntil)
	require.True(t, IsAccountSchedulingThresholdReason(repo.lastTempReason))
	// Once the billing cycle ends, the same snapshot cannot renew the pause.
	past := time.Now().UTC().Add(-time.Second)
	end = past
	account.TempUnschedulableUntil = &past
	require.True(t, account.IsSchedulable())
	require.False(t, rl.ApplyAccountSchedulingThreshold(context.Background(), account))
	require.Equal(t, 1, repo.tempCalls)
}

func TestCursorSchedulingThreshold(t *testing.T) {
	now := time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)
	end := now.Add(24 * time.Hour)
	for _, tc := range []struct {
		name    string
		percent float64
		pause   bool
	}{
		{"below", 79.99, false}, {"boundary", 80, true}, {"above", 99, true}, {"zero", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			monthly := &CursorMonthlyUsage{PlanUsage: &CursorPlanUsage{TotalPercentUsed: &tc.percent}, ResetsAt: &end}
			// Exercise both the in-memory snapshot and its database JSON representation.
			raw, err := json.Marshal(monthly)
			require.NoError(t, err)
			var persisted map[string]any
			require.NoError(t, json.Unmarshal(raw, &persisted))
			for _, snapshot := range []any{monthly, persisted} {
				account := &Account{Platform: PlatformCursor, Type: AccountTypeOAuth, Extra: map[string]any{"cursor_usage_snapshot": snapshot}}
				decision := EvaluateAccountSchedulingThreshold(account, map[string]int{PlatformCursor: 80}, now)
				require.Equal(t, tc.pause, decision.ShouldPause)
				if tc.pause {
					require.Equal(t, "monthly", decision.Window)
					require.Equal(t, tc.percent, decision.UsedPercent)
					require.Equal(t, end, *decision.Until)
				}
				require.False(t, EvaluateAccountSchedulingThreshold(account, map[string]int{PlatformCursor: 80}, end).ShouldPause)
				require.False(t, EvaluateAccountSchedulingThreshold(account, map[string]int{PlatformCursor: 100}, now).ShouldPause)
				account.Type = AccountTypeUpstream
				require.False(t, EvaluateAccountSchedulingThreshold(account, map[string]int{PlatformCursor: 80}, now).ShouldPause)
			}
		})
	}
}

func TestCursorSchedulingThresholdUnknownDataDoesNotPause(t *testing.T) {
	now := time.Now()
	end := now.Add(time.Hour)
	percent := 99.0
	for _, snapshot := range []any{
		nil, "invalid", map[string]any{},
		&CursorMonthlyUsage{ResetsAt: &end},
		&CursorMonthlyUsage{ResetsAt: &end, PlanUsage: &CursorPlanUsage{AutoPercentUsed: &percent, APIPercentUsed: &percent}, LimitStatus: &CursorLimitStatus{IsInSlowPool: true}},
		&CursorMonthlyUsage{ResetsAt: &end, DashboardPartial: true, PlanUsage: &CursorPlanUsage{TotalPercentUsed: &percent}},
		&CursorMonthlyUsage{PlanUsage: &CursorPlanUsage{TotalPercentUsed: &percent}},
		map[string]any{"resets_at": end, "plan_usage": map[string]any{"totalPercentUsed": "unknown"}},
	} {
		account := &Account{Platform: PlatformCursor, Type: AccountTypeAPIKey, Extra: map[string]any{"cursor_usage_snapshot": snapshot}, Credentials: map[string]any{accountSchedulingThresholdCredentialKey: 80}}
		require.False(t, EvaluateAccountSchedulingThreshold(account, nil, now).ShouldPause, "%+v", snapshot)
	}
}

func TestCursorSchedulingThresholdSettingRoundTrip(t *testing.T) {
	normalized, err := validateAndNormalizeAccountSchedulingThresholds(map[string]int{PlatformCursor: 80})
	require.NoError(t, err)
	require.Equal(t, 80, normalized[PlatformCursor])
	parsed, err := parseAccountSchedulingThresholdsSetting(`{"cursor":80}`)
	require.NoError(t, err)
	require.Equal(t, 80, parsed[PlatformCursor])
	for _, invalid := range []int{0, 101} {
		_, err := validateAndNormalizeAccountSchedulingThresholds(map[string]int{PlatformCursor: invalid})
		require.Error(t, err)
	}
}
