package readmodel

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCalendarReset(t *testing.T) {
	for _, tc := range []struct{ instant, period, start, end string }{
		{"2026-03-08T16:00:00Z", "calendar_day", "2026-03-08T05:00:00Z", "2026-03-09T04:00:00Z"},
		{"2026-11-01T06:30:00Z", "calendar_day", "2026-11-01T04:00:00Z", "2026-11-02T05:00:00Z"},
		{"2026-11-02T05:00:00Z", "calendar_week", "2026-11-02T05:00:00Z", "2026-11-09T05:00:00Z"},
		{"2026-03-31T23:30:00Z", "calendar_month", "2026-03-01T05:00:00Z", "2026-04-01T04:00:00Z"},
	} {
		at, e := time.Parse(time.RFC3339, tc.instant)
		require.NoError(t, e)
		a, b, e := bounds(at, "America/New_York", tc.period)
		require.NoError(t, e)
		require.Equal(t, tc.start, a.Format(time.RFC3339))
		require.Equal(t, tc.end, b.Format(time.RFC3339))
	}
}
