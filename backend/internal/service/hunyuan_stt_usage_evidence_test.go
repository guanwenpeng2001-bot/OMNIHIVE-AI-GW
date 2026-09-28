//go:build unit

package service

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

func TestHunyuanSTTUsage_ObservedDurationIsAuthoritative(t *testing.T) {
	for _, milliseconds := range []float64{15230, 0.125, 9e15} {
		usage := estimateHunyuanAudioUsage(HunyuanEndpointTranscribeSync, []byte(strings.Repeat("audio", 100000)), []byte(fmt.Sprintf(`{"output":{"duration_ms":%g}}`, milliseconds)), 90*time.Second)
		require.NotNil(t, usage)
		require.Equal(t, "stt", usage.Mode)
		require.Equal(t, milliseconds/1000/3600, usage.DurationOrUnits)
	}
}

func TestHunyuanSTTUsage_InvalidDurationIsUnknown(t *testing.T) {
	for _, body := range []string{`{}`, `{"output":{"duration_ms":null}}`, `{"output":{"duration_ms":"15230"}}`, `{"output":{"duration_ms":true}}`, `{"output":{"duration_ms":0}}`, `{"output":{"duration_ms":-2}}`, `{"output":{"duration_ms":1e999}}`, `{"output":{"duration_ms":1e-999}}`, `{"output":{"duration_ms":3000}} trailing`} {
		t.Run(body, func(t *testing.T) {
			require.Nil(t, estimateHunyuanAudioUsage(HunyuanEndpointTranscribeSync, []byte(strings.Repeat("audio", 100000)), []byte(body), 90*time.Second))
			upstream := &hunyuanActionUpstreamStub{responses: []*http.Response{hunyuanActionJSONResponse(http.StatusOK, body)}}
			gateway := hunyuanActionTestService(upstream)
			request, response := hunyuanActionTestContext(t, http.MethodPost, "/v1/audio/transcriptions", nil)
			result, err := gateway.ForwardHunyuanAction(context.Background(), request, hunyuanActionTestAccount(), HunyuanEndpointTranscribeSync, "", []byte(`{"model":"hy-asr-3.0-preview","audio_data":"private"}`), "application/json", "")
			require.NoError(t, err)
			require.Nil(t, result.AudioUsage)
			require.True(t, result.AudioUsageUnresolved)
			require.Equal(t, body, response.Body.String())
		})
	}
}

func TestHunyuanSTTTask_RequiresCompletedStatus(t *testing.T) {
	for _, status := range []string{"queued", "in_progress", "failed", "unknown", "", "completed"} {
		for _, duration := range []string{"15230", "null"} {
			t.Run(status+duration, func(t *testing.T) {
				body := fmt.Sprintf(`{"status":%q,"output":{"duration_ms":%s},"usage":{"total_token":9999}}`, status, duration)
				upstream := &hunyuanActionUpstreamStub{responses: []*http.Response{hunyuanActionJSONResponse(http.StatusOK, body)}}
				request, _ := hunyuanActionTestContext(t, http.MethodGet, "/v1/hunyuan/transcribe/tasks/task", nil)
				result, err := hunyuanActionTestService(upstream).ForwardHunyuanAction(context.Background(), request, hunyuanActionTestAccount(), HunyuanEndpointTranscribeTask, "task", nil, "", "")
				require.NoError(t, err)
				require.Equal(t, status == "completed" && duration == "15230", result.AudioUsage != nil)
				require.Equal(t, status == "completed" && duration == "null", result.AudioUsageUnresolved)
			})
		}
	}
}

type hunyuanReviewBudgetRepository struct {
	settlementCapture
	keyBudgetReceiptStub
}

func (r *hunyuanReviewBudgetRepository) Persist(ctx context.Context, receipt *UsageSettlementReceipt) (*UsageSettlementReceipt, error) {
	return r.settlementCapture.Persist(ctx, receipt)
}
func (r *hunyuanReviewBudgetRepository) Get(ctx context.Context, key UsageSettlementKey) (*UsageSettlementReceipt, error) {
	return r.settlementCapture.Get(ctx, key)
}
func (r *hunyuanReviewBudgetRepository) Settle(ctx context.Context, key UsageSettlementKey, fingerprint string) (*UsageSettlementResult, error) {
	return r.settlementCapture.Settle(ctx, key, fingerprint)
}
func (r *hunyuanReviewBudgetRepository) ListPending(ctx context.Context, after int64, limit int) ([]*UsageSettlementReceipt, error) {
	return nil, nil
}

func TestHunyuanSTTUnknown_PersistsReviewWithoutDebit(t *testing.T) {
	repository := &hunyuanReviewBudgetRepository{}
	settlement := NewUsageSettlementService(repository, nil, nil)
	gateway := &OpenAIGatewayService{usageSettlement: settlement}
	plan := NewGatewayRequestPlan(GatewayRequestSpec{UserID: 10, APIKeyID: 20, ExecutionID: "observation", Model: "hy-asr-3.0-preview"})
	plan.settlement = settlement
	require.NoError(t, plan.ReserveBudget(context.Background(), RequestBudgetEstimate{Model: "hy-asr-3.0-preview"}))
	ctx := WithGatewayRequestPlan(context.Background(), plan)
	result := &OpenAIForwardResult{RequestID: "poll", Model: "hy-asr-3.0-preview", AudioUsageUnresolved: true, Usage: OpenAIUsage{InputTokens: 999}}
	require.NoError(t, gateway.RecordHunyuanSTTUnresolved(ctx, result, &APIKey{ID: 20, UserID: 10}, &Account{ID: 1, Platform: PlatformHunyuan}, "task"))
	receipt := repository.receipt
	require.Equal(t, "unresolved", receipt.ResolutionState)
	require.Equal(t, "complete", receipt.CompletionState)
	require.Equal(t, PricingStateError, receipt.Usage.PricingState)
	require.Equal(t, UsageSettlementNeedsReview, receipt.State)
	require.Zero(t, repository.settles)
	require.Zero(t, receipt.Command.BalanceCost)
	require.Zero(t, receipt.Command.APIKeyQuotaCost)
	require.Zero(t, receipt.Command.AccountQuotaCost)
	require.Zero(t, receipt.Usage.InputTokens)
	require.Zero(t, receipt.Usage.ActualCost)
	require.Contains(t, receipt.Usage.PricingSnapshot, `"task_id":"task"`)
	require.NotEqual(t, HunyuanTranscribeTaskBillingRequestID("task"), receipt.Key.RequestID)
	require.Equal(t, UsageSettlementNeedsReview, plan.State().ReceiptState)
	plan.Finalize(true)
	require.Equal(t, []string{"observation"}, repository.released)
}

func TestHunyuanSTTCompletion_TaskIdentityOverridesPollIdentity(t *testing.T) {
	for _, contextKey := range []any{ctxkey.ClientRequestID, ctxkey.RequestID} {
		ctx := context.WithValue(context.Background(), contextKey, "poll-request")
		stable := HunyuanTranscribeTaskBillingRequestID("stt-task")
		require.Equal(t, stable, resolveUsageBillingRequestID(ctx, stable))
	}
}
