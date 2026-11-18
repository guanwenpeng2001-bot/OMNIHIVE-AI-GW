//go:build unit

package service

import (
	"context"
	"encoding/json"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/usagestats"
	"github.com/stretchr/testify/require"
	"testing"
)

type userPendingReader struct {
	UsageLogRepository
	userID int64
	limit  int
}

func (r *userPendingReader) ListUserPendingSettlement(_ context.Context, userID int64, _ usagestats.UsageLogFilters, cursor UserPendingSettlementCursor) (*UserPendingSettlementPage, error) {
	r.userID = userID
	r.limit = cursor.Limit
	return &UserPendingSettlementPage{}, nil
}
func TestUserPendingSettlementService(t *testing.T) {
	reader := &userPendingReader{}
	svc := NewUsageService(reader, nil, nil, nil)
	_, err := svc.ListUserPendingSettlement(context.Background(), 7, usagestats.UsageLogFilters{UserID: 99}, UserPendingSettlementCursor{Limit: 1000})
	require.NoError(t, err)
	require.Equal(t, int64(7), reader.userID)
	require.Equal(t, 100, reader.limit)
	_, err = svc.ListUserPendingSettlement(context.Background(), 0, usagestats.UsageLogFilters{}, UserPendingSettlementCursor{})
	require.ErrorIs(t, err, ErrUsageLogNotFound)
	_, err = NewUsageService(nil, nil, nil, nil).ListUserPendingSettlement(context.Background(), 7, usagestats.UsageLogFilters{}, UserPendingSettlementCursor{})
	require.Error(t, err)
}
func TestPendingSettlementFromRealtimeJob(t *testing.T) {
	job := &UpstreamJob{ID: 13, UserID: 7, UsageJSON: json.RawMessage(`{"input_tokens":12,"output_tokens":3}`), Billing: UpstreamJobBilling{RequestedModel: "public", Model: "wire", PricingState: PricingStateUnpriced}}
	receipt, err := PendingSettlementFromRealtimeJob(job)
	require.NoError(t, err)
	require.Equal(t, 12, *receipt.InputTokens)
	require.Equal(t, 3, *receipt.OutputTokens)
	require.Equal(t, "public", receipt.Usage.RequestedModel)
	require.Equal(t, "wire", *receipt.Usage.UpstreamModel)
	require.Nil(t, receipt.Usage.UpstreamResponseModel)
	job.UsageJSON = json.RawMessage(`{"characters":20}`)
	receipt, err = PendingSettlementFromRealtimeJob(job)
	require.NoError(t, err)
	require.Nil(t, receipt.InputTokens)
	job.ResultRef = "cancelled"
	job.ErrorCode = "unsupported_usage"
	job.UsageJSON = json.RawMessage(`{"input_tokens":"malformed"}`)
	receipt, err = PendingSettlementFromRealtimeJob(job)
	require.NoError(t, err)
	require.Nil(t, receipt.InputTokens)
	require.Equal(t, PricingStateError, receipt.Usage.PricingState)
	require.Equal(t, "cancelled", receipt.CompletionStatus)
}

func TestPendingSettlementFromRealtimeJSON(t *testing.T) {
	for _, evidence := range []struct {
		raw      string
		expected *string
	}{
		{raw: `{"model":"wire","requested_model":"public","pricing_state":"unpriced","realtime":{"upstream_response_model":"declared"}}`, expected: func() *string { value := "declared"; return &value }()},
		{raw: `{"model":"wire","requested_model":"public","pricing_state":"unpriced"}`},
	} {
		raw := []byte(`{"id":42,"result_ref":"incomplete","usage_json":{"input_tokens":10},"billing_snapshot":` + evidence.raw + `}`)
		item, err := PendingSettlementFromRealtimeJSON(raw)
		require.NoError(t, err)
		require.Equal(t, evidence.expected, item.Usage.UpstreamResponseModel)
		require.Equal(t, "public", item.Usage.RequestedModel)
		require.Equal(t, "wire", *item.Usage.UpstreamModel)
		require.Equal(t, "incomplete", item.CompletionStatus)
		require.Nil(t, item.OutputTokens)
	}
}
