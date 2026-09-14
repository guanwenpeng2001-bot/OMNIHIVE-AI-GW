//go:build unit

package service

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func realtimeSessionFixture() *UpstreamJob {
	return &UpstreamJob{PublicRequestID: "gateway-session-1", Platform: PlatformQianwen, Family: "realtime_session", UserID: 1, APIKeyID: 2, AccountID: 19, Status: "submitted", ExpiresAt: time.Now().Add(time.Minute), Billing: UpstreamJobBilling{Model: "qwen3-tts-flash-realtime", RequestedModel: "qwen3-tts-flash-realtime", UnitPrice: 0.143353 / 10000, RateMultiplier: 1, AccountRateMultiplier: 1, KeyQuota: true, KeyRateLimit: true, AccountQuota: true, Realtime: &QianwenRealtimeSnapshot{SessionID: "gateway-session-1", Region: "beijing", PriceVersion: QianwenRealtimePriceVersion}}}
}

func TestQianwenRealtimeUsageAndOfficialCost(t *testing.T) {
	for _, raw := range []string{`{}`, `null`, `{"characters":null}`, `{"characters":-1}`, `{"characters":1.2}`, `{"characters":"25"}`, `{"characters":2001}`, `{"characters":1,"audio_tokens":3}`, `{"total_tokens":25}`, `{"characters":1e999}`} {
		_, err := QianwenRealtimeCharacters([]byte(raw))
		require.ErrorIs(t, err, ErrUpstreamJobUnsupportedUsage, raw)
	}
	j := realtimeSessionFixture()
	j.Family = "realtime_turn"
	j.UsageJSON = json.RawMessage(`{"characters":25}`)
	log, cmd, err := QianwenRealtimeCharge(j)
	require.NoError(t, err)
	require.InDelta(t, 0.0003583825, log.TotalCost, 1e-8)
	require.Equal(t, log.ActualCost, cmd.BalanceCost)
	require.Equal(t, cmd.BalanceCost, cmd.APIKeyQuotaCost)
	require.Equal(t, cmd.BalanceCost, cmd.APIKeyRateLimitCost)
	require.Equal(t, cmd.BalanceCost, cmd.AccountQuotaCost)
	require.Equal(t, "realtime", *log.BillingMode)
	require.Equal(t, "qwen3_tts_characters:25", *log.BillingTier)
	require.Zero(t, log.InputTokens, "characters are not text tokens")
	sub := int64(7)
	j.Billing.SubscriptionID = &sub
	_, cmd, err = QianwenRealtimeCharge(j)
	require.NoError(t, err)
	require.Zero(t, cmd.BalanceCost)
	require.Equal(t, log.ActualCost, cmd.SubscriptionCost)
	j.Billing.Realtime.Region = "singapore"
	j.Billing.UnitPrice = 0.13 / 10000
	log, _, err = QianwenRealtimeCharge(j)
	require.NoError(t, err)
	require.InDelta(t, 0.000325, log.TotalCost, 1e-8)
	j.Billing.UnitPrice = 0.05
	_, _, err = QianwenRealtimeCharge(j)
	require.ErrorIs(t, err, ErrUpstreamJobTariff)
}

func TestQianwenRealtimeDurablePerTurnIdempotencyAndRestart(t *testing.T) {
	ctx := context.Background()
	session := realtimeSessionFixture()
	repo := &fileBatchMemoryRepo{jobs: map[string]*UpstreamJob{}, claims: map[string]string{}}
	svc := NewUpstreamJobService(repo, nil, QianwenAsyncJobCharge)
	var wg sync.WaitGroup
	for range 24 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			j, err := svc.RecordQianwenRealtimeTurn(ctx, session, "response-one", []byte(`{"characters":25}`), "completed")
			require.NoError(t, err)
			require.NotNil(t, j.ChargedUsageLogID)
		}()
	}
	wg.Wait()
	require.Equal(t, 1, repo.charges)
	second, err := svc.RecordQianwenRealtimeTurn(ctx, session, "response-two", []byte(`{"characters":10}`), "cancelled")
	require.NoError(t, err)
	require.NotNil(t, second.ChargedUsageLogID)
	require.Equal(t, 2, repo.charges)
	require.Equal(t, session.PublicRequestID, second.Billing.Realtime.SessionID)
	// New service instance represents process restart; the repository anchors win.
	svc = NewUpstreamJobService(repo, nil, QianwenAsyncJobCharge)
	_, err = svc.RecordQianwenRealtimeTurn(ctx, session, "response-one", []byte(`{ "characters" : 25 }`), "completed")
	require.NoError(t, err)
	require.Equal(t, 2, repo.charges)
	j, err := svc.RecordQianwenRealtimeTurn(ctx, session, "response-one", []byte(`{"characters":26}`), "completed")
	require.ErrorIs(t, err, ErrQianwenRealtimeConflict)
	require.JSONEq(t, `{"characters":25}`, string(j.UsageJSON))
	require.Equal(t, "completed", j.ResultRef)
	require.Equal(t, 2, repo.charges)
}

func TestQianwenRealtimeReceiptSurvivesBillingRollback(t *testing.T) {
	ctx := context.Background()
	session := realtimeSessionFixture()
	repo := &fileBatchMemoryRepo{jobs: map[string]*UpstreamJob{}, claims: map[string]string{}, failCharge: true}
	svc := NewUpstreamJobService(repo, nil, QianwenAsyncJobCharge)
	_, err := svc.RecordQianwenRealtimeTurn(ctx, session, "response-one", []byte(`{"characters":25}`), "completed")
	require.Error(t, err)
	require.Zero(t, repo.charges)
	require.Len(t, repo.jobs, 1)
	var pending *UpstreamJob
	for _, j := range repo.jobs {
		pending = j
	}
	require.Nil(t, pending.ChargedUsageLogID)
	require.JSONEq(t, `{"characters":25}`, string(pending.UsageJSON))
	require.Equal(t, "running", pending.Status)
	repo.failCharge = false
	restarted := NewUpstreamJobService(repo, func(context.Context, *UpstreamJob) (UpstreamJobUpdate, error) {
		t.Fatal("must not query or regenerate provider response")
		return UpstreamJobUpdate{}, nil
	}, QianwenAsyncJobCharge)
	settled, err := restarted.Poll(ctx, pending.PublicRequestID)
	require.NoError(t, err)
	require.NotNil(t, settled.ChargedUsageLogID)
	require.Equal(t, 1, repo.charges)
	_, err = restarted.Poll(ctx, pending.PublicRequestID)
	require.NoError(t, err)
	require.Equal(t, 1, repo.charges)
}

func TestQianwenRealtimeUnknownUsagePersistsWithoutCharging(t *testing.T) {
	for _, raw := range []string{`{"audio_seconds":3}`, `{"characters":null}`, `{"characters":-1}`, `{}`} {
		repo := &fileBatchMemoryRepo{jobs: map[string]*UpstreamJob{}, claims: map[string]string{}}
		svc := NewUpstreamJobService(repo, nil, QianwenAsyncJobCharge)
		j, err := svc.RecordQianwenRealtimeTurn(context.Background(), realtimeSessionFixture(), "response-one", []byte(raw), "completed")
		require.NoError(t, err)
		require.Nil(t, j.ChargedUsageLogID)
		require.JSONEq(t, raw, string(j.UsageJSON))
		require.Equal(t, "unsupported_usage", j.ErrorCode)
		require.Zero(t, repo.charges)
	}
}

func TestQianwenRealtimeRegionAndMappedModel(t *testing.T) {
	a := qianwenTestAccount()
	for _, base := range []string{"https://evil.test", "http://dashscope.aliyuncs.com", "https://user@dashscope.aliyuncs.com", "https://dashscope.aliyuncs.com:444", "https://dashscope.aliyuncs.com/?key=x"} {
		a.Credentials["api_base_urls"] = map[string]any{"dashscope": base}
		_, _, err := qianwenRealtimeEndpoint(a, "qwen3-tts-flash-realtime")
		require.Error(t, err, base)
	}
	a.Credentials["api_base_urls"] = map[string]any{"dashscope": "https://dashscope-intl.aliyuncs.com/compatible-mode/v1"}
	target, region, err := qianwenRealtimeEndpoint(a, "qwen3-tts-flash-realtime")
	require.NoError(t, err)
	require.Equal(t, "singapore", region)
	require.Equal(t, "wss://dashscope-intl.aliyuncs.com/api-ws/v1/realtime?model=qwen3-tts-flash-realtime", target)
	for _, model := range []string{"qwen3-omni-flash-realtime", "qwen3-asr-flash-realtime", "qwen-tts-realtime", "qwen3-tts-flash-realtime-future"} {
		_, _, err = qianwenRealtimeEndpoint(a, model)
		require.Error(t, err)
	}
}

func (repo *fileBatchMemoryRepo) InsertRealtimeReceipt(ctx context.Context, receipt *UpstreamJob) (*UpstreamJob, error) {
	stored, _, err := repo.Create(ctx, receipt)
	if err != nil {
		return nil, err
	}
	if !EqualQianwenRealtimeReceipt(stored, receipt) {
		return stored, ErrQianwenRealtimeConflict
	}
	return stored, nil
}
