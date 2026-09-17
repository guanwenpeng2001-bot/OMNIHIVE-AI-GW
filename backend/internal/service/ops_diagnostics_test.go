package service

import (
	"encoding/json"
	"math"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpsDiagnosticsCountingBoundaries(t *testing.T) {
	before := SnapshotOpsDiagnostics()
	finish := BeginOpsIngress()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	BeginOpsStreamTurn(c, 1)
	BeginOpsStreamTurn(c, 1)
	BeginOpsStreamTurn(c, 2)
	BeginOpsStreamTurn(c, 1)
	BeginOpsStreamTurn(c, 2)
	// A local business rejection is neither a send nor a transport failure.
	MarkOpsClientBusinessLimited(c, OpsClientBusinessLimitedReasonLocalPolicyDenied)
	finish()
	finish()
	after := SnapshotOpsDiagnostics()
	require.Equal(t, uint64(1), after.IngressStarted-before.IngressStarted)
	require.Equal(t, uint64(1), after.IngressCompleted-before.IngressCompleted)
	require.Equal(t, uint64(2), after.WSTurns-before.WSTurns)
}

func TestOpsDiagnosticsSuppressedEventsRetainCoarseHealthWithoutPayload(t *testing.T) {
	before := SnapshotOpsDiagnostics()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{UpstreamStatusCode: 503, SkipMonitoring: true,
		Message: "secret-prompt", Detail: "raw-reasoning", UpstreamResponseBody: "sk-private-key"})
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{Kind: "request_error", SkipMonitoring: true})
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{Kind: "request_error", Stage: string(GatewayFailureStageAccountAuth)})
	after := SnapshotOpsDiagnostics()
	require.Equal(t, uint64(3), after.UpstreamErrorEvents-before.UpstreamErrorEvents)
	require.Equal(t, uint64(2), after.SuppressedUpstreamEvents-before.SuppressedUpstreamEvents)
	require.Equal(t, uint64(1), after.UpstreamHTTPErrorEvents-before.UpstreamHTTPErrorEvents)
	require.Equal(t, uint64(1), after.UpstreamTransportErrorEvents-before.UpstreamTransportErrorEvents)
	body, err := json.Marshal(after)
	require.NoError(t, err)
	for _, secret := range []string{"secret-prompt", "raw-reasoning", "sk-private-key"} {
		require.NotContains(t, string(body), secret)
	}
}

func TestOpsDiagnosticsBillingAdjustmentsAreNotRawUsage(t *testing.T) {
	before := SnapshotOpsDiagnostics()
	usage := ClaudeUsage{InputTokens: 100, CacheReadInputTokens: 20, CacheCreationInputTokens: 30, CacheCreation5mTokens: 30}
	input := opsClaudeCacheUsage(usage)
	usage.CacheReadInputTokens += usage.InputTokens
	usage.InputTokens = 0
	applyCacheTTLOverride(&usage, "1h")
	RecordOpsBillingCacheAdjustment(input, opsClaudeCacheUsage(usage))
	after := SnapshotOpsDiagnostics()
	require.Equal(t, uint64(1), after.BillingAdjustmentObservations-before.BillingAdjustmentObservations)
	require.Equal(t, int64(100), after.BillingInputCacheUsage.Input-before.BillingInputCacheUsage.Input)
	require.Equal(t, int64(120), after.BillingAdjustedCacheUsage.Read-before.BillingAdjustedCacheUsage.Read)
	require.Equal(t, int64(30), after.BillingAdjustedCacheUsage.Creation1h-before.BillingAdjustedCacheUsage.Creation1h)
	require.Equal(t, "not_instrumented", after.AttemptCoverage)
	require.Equal(t, "not_instrumented", after.RawUsageCoverage)
	body, err := json.Marshal(after)
	require.NoError(t, err)
	for _, absent := range []string{"attempts_started", "raw_cache_usage", "billing_cache_usage", "http_failures"} {
		require.NotContains(t, string(body), `"`+absent+`"`)
	}
}

func TestOpsDiagnosticsConcurrentStickySnapshotsConserveCounts(t *testing.T) {
	s := &defaultOpenAIAccountScheduler{stats: newOpenAIAccountRuntimeStats()}
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 5000 {
				s.metrics.recordSelect(OpenAIAccountScheduleDecision{StickyPreviousHit: true, StickySessionHit: true})
			}
		}()
	}
	for range 5000 {
		snapshot := s.SnapshotMetrics()
		require.LessOrEqual(t, snapshot.StickyHitRatio, 1.0)
		require.Equal(t, snapshot.SelectTotal, snapshot.StickySessionHitTotal)
		require.Equal(t, snapshot.SelectTotal, snapshot.StickyPreviousHitTotal)
	}
	wg.Wait()
	require.Equal(t, int64(20000), s.SnapshotMetrics().SelectTotal)
}

func TestOpsDiagnosticsBalanceExposureUsesExistingThreshold(t *testing.T) {
	threshold := 10.0
	info := &UserConcurrencyInfo{CurrentInUse: 3, MaxCapacity: 2}
	u := User{Balance: 5, BalanceNotifyThreshold: &threshold}
	setOpsBalanceExposure(info, u, nil)
	require.True(t, *info.LowBalanceConcurrent)
	require.True(t, info.ConcurrencyLimitExceeded)
	require.Equal(t, 10.0, *info.LowBalanceThreshold)
	require.InDelta(t, 5.0/3, *info.BalancePerActiveRequest, 1e-10)
	require.Equal(t, 5.0, u.Balance)
	unknown := &UserConcurrencyInfo{CurrentInUse: 2}
	setOpsBalanceExposure(unknown, User{Balance: 5}, nil)
	require.Nil(t, unknown.LowBalanceConcurrent)
	depleted := &UserConcurrencyInfo{CurrentInUse: 2}
	setOpsBalanceExposure(depleted, User{Balance: 0}, nil)
	require.True(t, *depleted.LowBalanceConcurrent)
}

func TestOpsDiagnosticsBalanceThresholdBoundaries(t *testing.T) {
	global := 10.0
	invalid := math.NaN()
	percent := 5.0
	for _, tc := range []struct {
		name      string
		user      User
		active    int64
		threshold *float64
		exposed   *bool
	}{
		{name: "global threshold", user: User{Balance: 9}, active: 2, threshold: &global, exposed: opsDiagnosticBool(true)},
		{name: "at threshold", user: User{Balance: 10}, active: 2, threshold: &global, exposed: opsDiagnosticBool(false)},
		{name: "single active request", user: User{Balance: 1}, active: 1, threshold: &global, exposed: opsDiagnosticBool(false)},
		{name: "percentage", user: User{Balance: 9, TotalRecharged: 200, BalanceNotifyThreshold: &percent, BalanceNotifyThresholdType: thresholdTypePercentage}, active: 2, threshold: &global, exposed: opsDiagnosticBool(true)},
		{name: "invalid user threshold stays unknown", user: User{Balance: 1, BalanceNotifyThreshold: &invalid}, active: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := &UserConcurrencyInfo{CurrentInUse: tc.active}
			setOpsBalanceExposure(info, tc.user, &global)
			require.Equal(t, tc.threshold, info.LowBalanceThreshold)
			require.Equal(t, tc.exposed, info.LowBalanceConcurrent)
		})
	}
}

func opsDiagnosticBool(v bool) *bool { return &v }
