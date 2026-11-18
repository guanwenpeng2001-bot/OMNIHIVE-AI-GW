package service

import (
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestProbeRetryAfterBackportCeiling(t *testing.T) {
	for _, delay := range []time.Duration{-time.Hour, 0, 15 * time.Minute, 48 * time.Hour, time.Duration(1<<63 - 1)} {
		for _, next := range []func(int, time.Duration) time.Duration{nextProbeDelay, unsupportedProbeDelay} {
			for i := 0; i < 20; i++ {
				got := next(5, delay)
				require.LessOrEqual(t, got, 24*time.Hour, "untrusted Retry-After must not freeze auto-refresh indefinitely")
				require.GreaterOrEqual(t, got, time.Minute)
				if delay > 0 && delay <= 24*time.Hour {
					require.GreaterOrEqual(t, got, delay)
				}
			}
		}
	}
}
