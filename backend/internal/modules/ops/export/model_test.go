package export

import (
	"strings"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/audit"
	"github.com/stretchr/testify/require"
)

func TestIngressDropsImmediatelyAtCapacity(t *testing.T) {
	s := New(nil, nil, nil)
	for range cap(s.ingress) {
		require.True(t, s.Offer(audit.Observation{}))
	}
	require.False(t, s.Offer(audit.Observation{}))
	require.EqualValues(t, 1, s.Dropped())
}

func TestSamplingRatiosAndEndpointValidation(t *testing.T) {
	for raw, expected := range map[string]int{"0": 0, "0.05": 50000, "0.000001": 1, "0.999999": 999999, "1.000000": 1000000} {
		rate, err := parseRate(raw)
		require.NoError(t, err)
		require.Equal(t, expected, rate)
		roundTrip, err := parseRate(formatRate(rate))
		require.NoError(t, err)
		require.Equal(t, rate, roundTrip)
	}
	for _, raw := range []string{"", "-0.1", "1.1", ".01", "1e-1", "0.0000001", " 0.05"} {
		_, err := parseRate(raw)
		require.Error(t, err)
	}
	for _, endpoint := range []string{
		"http://collector.example.test",
		"https://127.0.0.1/collector",
		"https://[::1]/collector",
		"https://user:secret@collector.example.test",
		"https://collector.example.test?token=secret",
		"https://collector.example.test/#",
		"https://collector.example.test:99999/",
		"https://collector.example.test/" + strings.Repeat("雪", 300),
	} {
		_, err := parseEndpoint(endpoint)
		require.Error(t, err)
		require.NotContains(t, err.Error(), endpoint)
	}
	endpoint := "https://collector.example.test/a%2Fb/"
	canonical, err := parseEndpoint(endpoint)
	require.NoError(t, err)
	require.Equal(t, endpoint, canonical)
}
