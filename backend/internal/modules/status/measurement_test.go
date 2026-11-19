package status

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMeasurementSampleAndFreshness(t *testing.T) {
	at := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)
	for _, n := range []int64{0, 1, 19} {
		m := measure(bucket{Counts: Counts{Succeeded: n, Attempts: 40}}, "")
		require.Equal(t, Unknown, m.State)
		require.Equal(t, "INSUFFICIENT_SAMPLES", *m.Reason)
		require.Nil(t, m.SuccessRate)
	}
	b := bucket{Counts: Counts{Succeeded: 20, UnknownOutcomes: 1}, lastObserved: &at, lastSuccess: &at}
	m := measure(b, "")
	require.Equal(t, int64(21), m.SampleCount)
	require.Equal(t, "OUTCOME_UNKNOWN", *m.Reason)
	require.Nil(t, m.SuccessRate)
	state, expires := freshness(b, at.Add(time.Minute), at.Add(5*time.Minute))
	require.Equal(t, "stale", state)
	require.Equal(t, at.Add(5*time.Minute), *expires)
	require.Equal(t, at, *b.lastSuccess)
	b.UnknownOutcomes = 0
	require.Equal(t, Operational, measure(b, "").State)
	b.GatewayErrors = 1
	require.Equal(t, Degraded, measure(b, "").State)
	b.GatewayErrors = 10
	require.Equal(t, Outage, measure(b, "").State)
}
