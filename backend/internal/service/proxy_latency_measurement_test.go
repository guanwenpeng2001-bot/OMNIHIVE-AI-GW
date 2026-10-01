package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type latencyStatsRepository struct{ qualityProxyRepository }

func (*latencyStatsRepository) CountAccountsByProxyID(context.Context, int64) (int64, error) {
	return 0, nil
}
func (*latencyStatsRepository) CountAccountsByProxyIDAndStatus(context.Context, int64, string) (int64, error) {
	return 0, nil
}
func (*latencyStatsRepository) GetProxyUsageTotals(context.Context, int64) (*ProxyUsageTotals, error) {
	return &ProxyUsageTotals{}, nil
}

type latencyMeasurementProber struct{ method string }

func (p latencyMeasurementProber) ProbeProxy(context.Context, string) (*ProxyExitInfo, int64, error) {
	available := false
	return &ProxyExitInfo{ProxyProbeMeasurement: ProxyProbeMeasurement{
		LatencyMethod: p.method, ExitInfoAvailable: &available,
	}}, 0, nil
}

func TestProxyLatencyLegacyCacheAndStats(t *testing.T) {
	var legacy ProxyLatencyInfo
	require.NoError(t, json.Unmarshal([]byte(`{"success":true,"latency_ms":432,"updated_at":"2026-10-01T01:00:00Z"}`), &legacy))
	cache := &qualityMemoryCache{snapshot: &legacy}
	svc := &adminServiceImpl{proxyLatencyCache: cache, proxyRepo: &latencyStatsRepository{qualityProxyRepository{proxy: &Proxy{ID: 1}}}}
	proxies := []ProxyWithAccountCount{{Proxy: Proxy{ID: 1}}}
	svc.attachProxyLatency(context.Background(), proxies)
	require.Empty(t, proxies[0].LatencyMethod)
	require.EqualValues(t, 432, *proxies[0].LatencyMs, "retain the old value only for the labeled tooltip")
	require.Equal(t, legacy.UpdatedAt, *proxies[0].LatencyCheckedAt)
	stats, err := svc.GetProxyStats(context.Background(), 1)
	require.NoError(t, err)
	require.Nil(t, stats.AverageLatencyMs)
	legacy.LatencyMethod = "warmed_http"
	stats, err = svc.GetProxyStats(context.Background(), 1)
	require.NoError(t, err)
	require.EqualValues(t, 432, *stats.AverageLatencyMs)

	svc.saveProxyLatency(context.Background(), 1, &ProxyLatencyInfo{Success: true,
		ProxyProbeMeasurement: ProxyProbeMeasurement{LatencyMethod: "unavailable"}, UpdatedAt: time.Now()})
	require.Nil(t, cache.snapshot.LatencyMs, "a new unavailable measurement must not reuse the previous timing")
}

func TestProxyLatencyZeroAndUnavailableJSON(t *testing.T) {
	for _, method := range []string{"warmed_http", "unavailable", ""} {
		t.Run(method, func(t *testing.T) {
			cache := &qualityMemoryCache{}
			svc := &adminServiceImpl{proxyLatencyCache: cache, proxyRepo: &qualityProxyRepository{proxy: &Proxy{ID: 1}}, proxyProber: latencyMeasurementProber{method}}
			result, err := svc.TestProxy(context.Background(), 1)
			require.NoError(t, err)
			require.True(t, result.Success)
			payload, err := json.Marshal(result)
			require.NoError(t, err)
			var decoded map[string]any
			require.NoError(t, json.Unmarshal(payload, &decoded))
			if method == "warmed_http" {
				require.Equal(t, float64(0), decoded["latency_ms"])
				require.NotNil(t, cache.snapshot.LatencyMs)
			} else {
				require.NotContains(t, decoded, "latency_ms")
			}
			quality := &ProxyQualityCheckResult{ProxyProbeMeasurement: result.ProxyProbeMeasurement,
				BaseLatencyMs: result.LatencyMs, PassedCount: 1, Items: []ProxyQualityCheckItem{{Target: "base_connectivity", Status: "pass"}}}
			svc.saveProxyQualitySnapshot(context.Background(), 1, quality, nil)
			require.Equal(t, result.LatencyMs, cache.snapshot.LatencyMs)
			payload, err = json.Marshal(quality)
			require.NoError(t, err)
			if method == "warmed_http" {
				require.Contains(t, string(payload), `"base_latency_ms":0`)
			} else {
				require.NotContains(t, string(payload), `"base_latency_ms"`)
			}
		})
	}
}
