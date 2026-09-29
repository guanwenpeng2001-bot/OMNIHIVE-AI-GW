package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLegacyBudgetAnchorPresetsKeepHistoricalWindowSemantics(t *testing.T) {
	cases := []struct {
		anchor         string
		seconds        int64
		durationAnchor string
		timezone       string
	}{
		{APIKeyLegacyBudgetAnchor5h, int64((5 * time.Hour) / time.Second), APIKeyBudgetAnchorFirstUse, "UTC"},
		{APIKeyLegacyBudgetAnchor1d, int64((24 * time.Hour) / time.Second), APIKeyBudgetAnchorDay, "UTC"},
		{APIKeyLegacyBudgetAnchor7d, int64((7 * 24 * time.Hour) / time.Second), APIKeyBudgetAnchorDay, "UTC"},
	}
	for _, tc := range cases {
		policy, ok := LegacyAPIKeyBudgetPolicy(tc.anchor, 10, false)
		require.True(t, ok, "anchor %s must stay resolvable", tc.anchor)
		normalized := policy.Normalize()
		require.Equal(t, APIKeyBudgetModeDuration, normalized.Mode)
		require.Equal(t, tc.seconds, normalized.DurationSeconds)
		require.Equal(t, tc.durationAnchor, normalized.Anchor)
		require.Equal(t, tc.timezone, normalized.EffectiveTimezoneName())
		require.Equal(t, tc.anchor, LegacyBudgetAnchorForPolicy(normalized))
		require.Equal(t, tc.anchor+" rolling window", DescribeAPIKeyBudgetPolicy(normalized))
	}
}

// The legacy 1d/7d windows anchored at date_trunc('day', NOW()); the period
// engine must keep that anchor so existing keys see the same reset instant.
func TestLegacyDayAnchoredDurationMatchesDayTruncatedWindow(t *testing.T) {
	policy, ok := LegacyAPIKeyBudgetPolicy(APIKeyLegacyBudgetAnchor7d, 25, false)
	require.True(t, ok)

	firstUse := time.Date(2026, 3, 10, 17, 42, 11, 0, time.UTC)
	start, end, rolled := policy.PeriodBounds(firstUse, nil)
	require.True(t, rolled, "the first request opens the first window")
	require.Equal(t, time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC), start, "the window starts at the UTC day boundary")
	require.Equal(t, time.Date(2026, 3, 17, 0, 0, 0, 0, time.UTC), end)

	// Inside the window nothing rolls, even much later on the same day.
	startAgain, endAgain, rolledAgain := policy.PeriodBounds(time.Date(2026, 3, 16, 23, 59, 59, 0, time.UTC), &start)
	require.False(t, rolledAgain)
	require.Equal(t, start, startAgain)
	require.Equal(t, end, endAgain)

	// At the boundary the next window starts on the following day boundary.
	nextStart, nextEnd, rolledNext := policy.PeriodBounds(end, &start)
	require.True(t, rolledNext)
	require.Equal(t, time.Date(2026, 3, 17, 0, 0, 0, 0, time.UTC), nextStart)
	require.Equal(t, time.Date(2026, 3, 24, 0, 0, 0, 0, time.UTC), nextEnd)
}

// The legacy 5h window is a rolling window anchored at the first request.
func TestLegacy5hWindowRollsFromFirstUse(t *testing.T) {
	policy, ok := LegacyAPIKeyBudgetPolicy(APIKeyLegacyBudgetAnchor5h, 3, false)
	require.True(t, ok)
	firstUse := time.Date(2026, 3, 10, 9, 15, 0, 0, time.UTC)
	start, end, rolled := policy.PeriodBounds(firstUse, nil)
	require.True(t, rolled)
	require.Equal(t, firstUse, start)
	require.Equal(t, firstUse.Add(5*time.Hour), end)

	_, _, rolledInside := policy.PeriodBounds(end.Add(-time.Second), &start)
	require.False(t, rolledInside)
	nextStart, _, rolledAfter := policy.PeriodBounds(end, &start)
	require.True(t, rolledAfter)
	require.Equal(t, end, nextStart, "a rolling window restarts at the request that crossed the boundary")
}

func TestCustomDurationBudgetPeriod(t *testing.T) {
	policy := APIKeyBudgetPolicy{
		Mode:            APIKeyBudgetModeDuration,
		LimitUSD:        12.5,
		DurationSeconds: int64((90 * time.Minute) / time.Second),
		Anchor:          APIKeyBudgetAnchorFirstUse,
	}.Normalize()
	require.NoError(t, policy.Validate())
	require.Equal(t, "every 90m, rolling from first use", DescribeAPIKeyBudgetPolicy(policy))

	start := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	windowStart, windowEnd, rolled := policy.PeriodBounds(start, nil)
	require.True(t, rolled)
	require.Equal(t, start, windowStart)
	require.Equal(t, start.Add(90*time.Minute), windowEnd)
}

func TestCalendarBudgetPeriodUsesTimezoneAndCalendarUnits(t *testing.T) {
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)

	dayPolicy := APIKeyBudgetPolicy{Mode: APIKeyBudgetModeCalendar, LimitUSD: 5, CalendarUnit: APIKeyBudgetCalendarDay, Timezone: "Asia/Shanghai"}.Normalize()
	require.Equal(t, "calendar day in Asia/Shanghai", DescribeAPIKeyBudgetPolicy(dayPolicy))
	// 2026-05-01 02:00 Shanghai is still 2026-04-30 in UTC; the window must
	// follow the configured timezone, not UTC.
	instant := time.Date(2026, 4, 30, 18, 0, 0, 0, time.UTC)
	start, end, rolled := dayPolicy.PeriodBounds(instant, nil)
	require.True(t, rolled)
	require.Equal(t, time.Date(2026, 5, 1, 0, 0, 0, 0, shanghai).UTC(), start)
	require.Equal(t, time.Date(2026, 5, 2, 0, 0, 0, 0, shanghai).UTC(), end)

	weekPolicy := APIKeyBudgetPolicy{Mode: APIKeyBudgetModeCalendar, LimitUSD: 5, CalendarUnit: APIKeyBudgetCalendarWeek, Timezone: "Asia/Shanghai"}.Normalize()
	// 2026-05-03 is a Sunday; the week window must start on Monday 2026-04-27.
	weekStart, weekEnd, _ := weekPolicy.PeriodBounds(time.Date(2026, 5, 3, 12, 0, 0, 0, shanghai).UTC(), nil)
	require.Equal(t, time.Date(2026, 4, 27, 0, 0, 0, 0, shanghai).UTC(), weekStart)
	require.Equal(t, time.Date(2026, 5, 4, 0, 0, 0, 0, shanghai).UTC(), weekEnd)

	monthPolicy := APIKeyBudgetPolicy{Mode: APIKeyBudgetModeCalendar, LimitUSD: 5, CalendarUnit: APIKeyBudgetCalendarMonth, Timezone: "Asia/Shanghai"}.Normalize()
	monthStart, monthEnd, _ := monthPolicy.PeriodBounds(time.Date(2026, 5, 31, 12, 0, 0, 0, shanghai).UTC(), nil)
	require.Equal(t, time.Date(2026, 5, 1, 0, 0, 0, 0, shanghai).UTC(), monthStart)
	require.Equal(t, time.Date(2026, 6, 1, 0, 0, 0, 0, shanghai).UTC(), monthEnd)

	// A stored window that is not the current calendar window rolls.
	stale := time.Date(2026, 4, 1, 0, 0, 0, 0, shanghai).UTC()
	_, _, rolled = monthPolicy.PeriodBounds(time.Date(2026, 5, 31, 12, 0, 0, 0, shanghai).UTC(), &stale)
	require.True(t, rolled)
}

// A calendar day must follow the local day length: the spring-forward day is 23h
// and the fall-back day is 25h, never a fixed 24h.
func TestCalendarDayPeriodFollowsDaylightSavingTransitions(t *testing.T) {
	newYork, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	policy := APIKeyBudgetPolicy{Mode: APIKeyBudgetModeCalendar, LimitUSD: 5, CalendarUnit: APIKeyBudgetCalendarDay, Timezone: "America/New_York"}.Normalize()

	springStart, springEnd, _ := policy.PeriodBounds(time.Date(2026, 3, 8, 12, 0, 0, 0, newYork).UTC(), nil)
	require.Equal(t, time.Date(2026, 3, 8, 0, 0, 0, 0, newYork).UTC(), springStart)
	require.Equal(t, 23*time.Hour, springEnd.Sub(springStart))

	fallStart, fallEnd, _ := policy.PeriodBounds(time.Date(2026, 11, 1, 12, 0, 0, 0, newYork).UTC(), nil)
	require.Equal(t, time.Date(2026, 11, 1, 0, 0, 0, 0, newYork).UTC(), fallStart)
	require.Equal(t, 25*time.Hour, fallEnd.Sub(fallStart))
}

func TestAPIKeyBudgetPolicyValidation(t *testing.T) {
	require.NoError(t, APIKeyBudgetPolicy{Mode: APIKeyBudgetModeOff}.Validate())
	require.ErrorIs(t, APIKeyBudgetPolicy{Mode: APIKeyBudgetModeDuration, LimitUSD: 5}.Validate(), ErrAPIKeyBudgetInvalid)
	require.NoError(t, APIKeyBudgetPolicy{Mode: APIKeyBudgetModeDuration, LimitUSD: 5, DurationSeconds: 60}.Validate())
	require.ErrorIs(t, APIKeyBudgetPolicy{Mode: APIKeyBudgetModeDuration, LimitUSD: 5, DurationSeconds: int64((400 * 24 * time.Hour) / time.Second)}.Validate(), ErrAPIKeyBudgetInvalid)
	require.ErrorIs(t, APIKeyBudgetPolicy{Mode: APIKeyBudgetModeCalendar, LimitUSD: 5, CalendarUnit: APIKeyBudgetCalendarDay, Timezone: "Not/AZone"}.Validate(), ErrAPIKeyBudgetTimezoneInvalid)
	require.ErrorIs(t, APIKeyBudgetPolicy{Mode: "hourly", LimitUSD: 5}.Validate(), ErrAPIKeyBudgetInvalid)
	require.NoError(t, APIKeyBudgetPolicy{Mode: APIKeyBudgetModeCalendar, LimitUSD: 5, CalendarUnit: APIKeyBudgetCalendarDay, Timezone: "Asia/Shanghai"}.Validate())
	require.NoError(t, APIKeyBudgetPolicy{Mode: APIKeyBudgetModeDuration, LimitUSD: 5, DurationSeconds: 60, Anchor: APIKeyBudgetAnchorDay}.Validate())
}

func TestAPIKeyBudgetStateReportsRemainingAfterReservations(t *testing.T) {
	policy := APIKeyBudgetPolicy{Mode: APIKeyBudgetModeDuration, LimitUSD: 10, DurationSeconds: 3600}.Normalize()
	start := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	state := NewAPIKeyBudgetState(policy, &start, &end, 4, 2.5, 0)
	require.InDelta(t, 3.5, state.RemainingUSD, 1e-9)
	require.InDelta(t, 3.5, state.RemainingAfterReservations(), 1e-9)

	exhausted := NewAPIKeyBudgetState(policy, &start, &end, 9, 4, 1)
	require.InDelta(t, 0, exhausted.RemainingUSD, 1e-9)
	require.InDelta(t, 1, exhausted.OverrunUSD, 1e-9)

	unlimited := NewAPIKeyBudgetState(APIKeyBudgetPolicy{Mode: APIKeyBudgetModeOff}, nil, nil, 0, 0, 0)
	require.Equal(t, float64(-1), unlimited.RemainingUSD)
}

func TestAPIKeyHasPeriodBudget(t *testing.T) {
	key := &APIKey{}
	require.False(t, key.HasPeriodBudget())
	key.BudgetPolicy = APIKeyBudgetPolicy{Mode: APIKeyBudgetModeDuration, LimitUSD: 1, DurationSeconds: 60}
	require.True(t, key.HasPeriodBudget())
	key.BudgetPolicy = APIKeyBudgetPolicy{Mode: APIKeyBudgetModeDuration, LimitUSD: 0, DurationSeconds: 60}
	require.False(t, key.HasPeriodBudget(), "a zero limit is not an active budget")
}
