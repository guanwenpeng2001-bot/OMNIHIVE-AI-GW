//go:build unit

package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/ctxkey"
	middleware2 "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// This repository applies only persisted priced receipts, with durable-key
// deduplication, and can fail either before commit or after committing a write.
type hunyuanSTTReceiptRepository struct {
	unlimitedTPMRepository
	service.UsageSettlementReceiptRepository
	mu                  sync.Mutex
	receipts            map[service.UsageSettlementKey]*service.UsageSettlementReceipt
	applied             map[service.UsageSettlementKey]bool
	persistFailure      bool
	lostAcknowledgement bool
	lookupFailure       bool
	debit               float64
	settles             int
}

func newHunyuanSTTReceiptRepository() *hunyuanSTTReceiptRepository {
	return &hunyuanSTTReceiptRepository{receipts: map[service.UsageSettlementKey]*service.UsageSettlementReceipt{}, applied: map[service.UsageSettlementKey]bool{}}
}
func (r *hunyuanSTTReceiptRepository) Persist(_ context.Context, receipt *service.UsageSettlementReceipt) (*service.UsageSettlementReceipt, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.persistFailure {
		return nil, errors.New("fixture unavailable")
	}
	if previous := r.receipts[receipt.Key]; previous != nil {
		if previous.Command.RequestFingerprint != receipt.Command.RequestFingerprint {
			return nil, service.ErrUsageBillingRequestConflict
		}
		return previous, nil
	}
	r.receipts[receipt.Key] = receipt
	if r.lostAcknowledgement {
		return nil, errors.New("fixture acknowledgement lost")
	}
	return receipt, nil
}
func (r *hunyuanSTTReceiptRepository) Get(_ context.Context, key service.UsageSettlementKey) (*service.UsageSettlementReceipt, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if receipt := r.receipts[key]; receipt != nil && !r.lookupFailure {
		return receipt, nil
	}
	return nil, service.ErrUsageSettlementNotFound
}
func (r *hunyuanSTTReceiptRepository) Settle(_ context.Context, key service.UsageSettlementKey, fingerprint string) (*service.UsageSettlementResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	receipt := r.receipts[key]
	if receipt == nil {
		return nil, service.ErrUsageSettlementNotFound
	}
	if receipt.State == service.UsageSettlementNeedsReview {
		return nil, service.ErrUsageSettlementNeedsReview
	}
	if receipt.Command.RequestFingerprint != fingerprint {
		return nil, service.ErrUsageBillingRequestConflict
	}
	if !r.applied[key] {
		r.debit += receipt.Command.BalanceCost
		r.settles++
		r.applied[key] = true
	}
	receipt.State = service.UsageSettlementSettled
	return &service.UsageSettlementResult{Receipt: receipt, Billing: &service.UsageBillingApplyResult{}}, nil
}

func hunyuanSTTTaskFixture(t *testing.T, repository *hunyuanSTTReceiptRepository) (*OpenAIGatewayHandler, *hunyuanActionCache, *hunyuanActionUpstream, *service.Account) {
	t.Helper()
	handler, bindings, upstream, accounts := newHunyuanActionHandler(t, repository)
	groupID := int64(24)
	require.NoError(t, handler.gatewayService.BindHunyuanTaskAccount(context.Background(), &groupID, "stt-task", 10, 20, 1))
	require.NoError(t, handler.gatewayService.StoreHunyuanTaskPendingBilling(context.Background(), "stt-task", 10, 20, service.BuildHunyuanTaskPendingBilling(
		service.HunyuanActionRequestInfo{Endpoint: service.HunyuanEndpointTranscribeAsync, Model: "hy-asr-3.0-preview"}, "hy-asr-3.0-preview", "hy-asr-3.0-preview", "hy-asr-3.0-preview", "")))
	return handler, bindings, upstream, &accounts[0]
}

func TestHunyuanSTTUnknown_DoesNotClaimFinalTaskBilling(t *testing.T) {
	repository := newHunyuanSTTReceiptRepository()
	handler, bindings, upstream, account := hunyuanSTTTaskFixture(t, repository)
	upstream.call = func(*http.Request, int64) (*http.Response, error) {
		return hunyuanActionJSONResponse(`{"status":"completed","output":{"text":"private transcript"}}`), nil
	}
	request, response := hunyuanActionContext(http.MethodGet, "/v1/hunyuan/transcribe/tasks/stt-task", "", service.PlatformHunyuan)
	request.Params = gin.Params{{Key: "task_id", Value: "stt-task"}}
	handler.HunyuanAudioTranscriptionTask(request)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), "private transcript")
	require.Equal(t, 1, upstream.callCount())
	require.Empty(t, bindings.billed)
	require.Len(t, repository.receipts, 1)
	for _, receipt := range repository.receipts {
		require.Equal(t, service.UsageSettlementNeedsReview, receipt.State)
		require.NotContains(t, receipt.Usage.PricingSnapshot, "private transcript")
		require.NotEqual(t, service.HunyuanTranscribeTaskBillingRequestID("stt-task"), receipt.Key.RequestID)
	}
	pending, err := handler.gatewayService.LoadHunyuanTaskPendingBilling(context.Background(), "stt-task", 10, 20)
	require.NoError(t, err)
	require.NotNil(t, pending)
	key, _ := middleware2.GetAPIKeyFromContext(request)
	bill := handler.prepareHunyuanTranscribeCompletionBilling(context.Background(), zap.NewNop(), key, middleware2.AuthSubject{UserID: 10}, "stt-task", &service.OpenAIForwardResult{AudioUsage: &service.AudioUsage{Mode: "stt", DurationOrUnits: 1}})
	require.NotNil(t, bill)
	require.Equal(t, "hy-asr-3.0-preview", bill.Model)
	require.Equal(t, service.HunyuanTranscribeTaskBillingRequestID("stt-task"), bill.RequestID)
	require.Zero(t, repository.debit)
	handler.cfg.RunMode = "standard"
	price := 2.0
	key.Group.AudioSTTPricePerHour = &price
	key.Group.RateMultiplier = 1
	nextPoll, _ := hunyuanActionContext(http.MethodGet, "/v1/hunyuan/transcribe/tasks/stt-task", "", service.PlatformHunyuan)
	handler.recordHunyuanActionUsage(nextPoll, zap.NewNop(), key, middleware2.AuthSubject{UserID: 10}, nil, account, bill, bill.Model, nil, "stt-task")
	require.Equal(t, 2.0, repository.debit)
	require.Equal(t, 1, repository.settles)
	require.Len(t, repository.receipts, 2)
}

func TestHunyuanSTTCompletion_ConcurrentPollsBillOnce(t *testing.T) {
	repository := newHunyuanSTTReceiptRepository()
	handler, _, _, account := hunyuanSTTTaskFixture(t, repository)
	handler.cfg.RunMode = "standard"
	var workers sync.WaitGroup
	for poll := range 20 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			request, _ := hunyuanActionContext(http.MethodGet, "/v1/hunyuan/transcribe/tasks/stt-task", "", service.PlatformHunyuan)
			request.Request = request.Request.WithContext(context.WithValue(request.Request.Context(), ctxkey.ClientRequestID, fmt.Sprintf("poll-%d", poll)))
			key, _ := middleware2.GetAPIKeyFromContext(request)
			price := 2.0
			key.Group.AudioSTTPricePerHour = &price
			key.Group.RateMultiplier = 1
			subject := middleware2.AuthSubject{UserID: 10}
			result := handler.prepareHunyuanTranscribeCompletionBilling(request.Request.Context(), zap.NewNop(), key, subject, "stt-task", &service.OpenAIForwardResult{AudioUsage: &service.AudioUsage{Mode: "stt", DurationOrUnits: 0.5}})
			if result != nil {
				handler.recordHunyuanActionUsage(request, zap.NewNop(), key, subject, nil, account, result, result.Model, nil, "stt-task")
			}
		}()
	}
	workers.Wait()
	require.Equal(t, 1, repository.settles)
	require.Equal(t, 1.0, repository.debit)
	require.Len(t, repository.receipts, 1)
	for key := range repository.receipts {
		require.Equal(t, service.HunyuanTranscribeTaskBillingRequestID("stt-task"), key.RequestID)
	}
}

func TestHunyuanSTTCompletion_RecordFailureReleasesClaim(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(fmt.Sprintf("commit-%t", committed), func(t *testing.T) {
			repository := newHunyuanSTTReceiptRepository()
			repository.persistFailure = !committed
			repository.lostAcknowledgement = committed
			repository.lookupFailure = true
			handler, bindings, _, account := hunyuanSTTTaskFixture(t, repository)
			handler.cfg.RunMode = "standard"
			for attempt := range 3 {
				request, _ := hunyuanActionContext(http.MethodGet, "/v1/hunyuan/transcribe/tasks/stt-task", "", service.PlatformHunyuan)
				request.Request = request.Request.WithContext(context.WithValue(request.Request.Context(), ctxkey.ClientRequestID, fmt.Sprintf("attempt-%d", attempt)))
				key, _ := middleware2.GetAPIKeyFromContext(request)
				price := 2.0
				key.Group.AudioSTTPricePerHour = &price
				key.Group.RateMultiplier = 1
				subject := middleware2.AuthSubject{UserID: 10}
				result := handler.prepareHunyuanTranscribeCompletionBilling(request.Request.Context(), zap.NewNop(), key, subject, "stt-task", &service.OpenAIForwardResult{AudioUsage: &service.AudioUsage{Mode: "stt", DurationOrUnits: 0.5}})
				if attempt == 2 {
					require.Nil(t, result)
					break
				}
				require.NotNil(t, result)
				handler.recordHunyuanActionUsage(request, zap.NewNop(), key, subject, nil, account, result, result.Model, nil, "stt-task")
				if attempt == 0 {
					require.Empty(t, bindings.billed)
					require.Zero(t, repository.debit)
				}
				repository.persistFailure = false
				repository.lostAcknowledgement = false
				repository.lookupFailure = false
			}
			require.Equal(t, 1, repository.settles)
			require.Equal(t, 1.0, repository.debit)
			require.Len(t, repository.receipts, 1)
		})
	}
}

func TestHunyuanSTTUnknown_PersistenceFailureIsObservable(t *testing.T) {
	repository := newHunyuanSTTReceiptRepository()
	repository.persistFailure = true
	handler, _, upstream, account := hunyuanSTTTaskFixture(t, repository)
	upstream.call = func(*http.Request, int64) (*http.Response, error) {
		return hunyuanActionJSONResponse(`{"output":{"text":"private transcript"}}`), nil
	}
	request, response := hunyuanActionContext(http.MethodPost, "/v1/audio/transcriptions", `{"model":"hy-asr-3.0-preview","audio_data":"private audio"}`, service.PlatformHunyuan)
	handler.HunyuanAudioTranscriptions(request)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Equal(t, 1, upstream.callCount())
	require.Contains(t, response.Body.String(), "private transcript")
	logs, observed := observer.New(zap.ErrorLevel)
	key, _ := middleware2.GetAPIKeyFromContext(request)
	observation, _ := hunyuanActionContext(http.MethodPost, "/v1/audio/transcriptions", "", service.PlatformHunyuan)
	handler.recordHunyuanActionUsage(observation, zap.New(logs), key, middleware2.AuthSubject{UserID: 10}, nil, account, &service.OpenAIForwardResult{Model: "hy-asr-3.0-preview", AudioUsageUnresolved: true}, "hy-asr-3.0-preview", nil, "")
	require.Equal(t, 1, observed.FilterMessage("hunyuan_action.stt_unresolved_persistence_failed").Len())
	require.NotContains(t, fmt.Sprint(observed.All()), "private")
	require.Empty(t, repository.receipts)
	require.Zero(t, repository.debit)
}

func TestHunyuanSTTTask_OwnerIsolation(t *testing.T) {
	for _, mismatch := range []string{"user", "key", "group", "task"} {
		t.Run(mismatch, func(t *testing.T) {
			repository := newHunyuanSTTReceiptRepository()
			handler, _, upstream, _ := hunyuanSTTTaskFixture(t, repository)
			request, response := hunyuanActionContext(http.MethodGet, "/v1/hunyuan/transcribe/tasks/stt-task", "", service.PlatformHunyuan)
			request.Params = gin.Params{{Key: "task_id", Value: "stt-task"}}
			key, _ := middleware2.GetAPIKeyFromContext(request)
			switch mismatch {
			case "user":
				request.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 11})
			case "key":
				key.ID = 21
			case "group":
				groupID := int64(25)
				key.GroupID = &groupID
			case "task":
				request.Params[0].Value = "other-task"
			}
			handler.HunyuanAudioTranscriptionTask(request)
			require.Equal(t, http.StatusNotFound, response.Code)
			require.Zero(t, upstream.callCount())
			require.Empty(t, repository.receipts)
		})
	}
}

func TestHunyuanSTTUnknown_SynchronousResponseRetainsReview(t *testing.T) {
	repository := newHunyuanSTTReceiptRepository()
	handler, _, upstream, _ := hunyuanSTTTaskFixture(t, repository)
	body := `{"output":{"text":"private transcript","duration_ms":"15230"},"usage":{"total_token":500}}`
	upstream.call = func(*http.Request, int64) (*http.Response, error) { return hunyuanActionJSONResponse(body), nil }
	for range 2 {
		request, response := hunyuanActionContext(http.MethodPost, "/v1/audio/transcriptions", `{"model":"hy-asr-3.0-preview","audio_data":"private audio"}`, service.PlatformHunyuan)
		request.Request = request.Request.WithContext(context.WithValue(request.Request.Context(), ctxkey.ClientRequestID, "same-observation"))
		handler.HunyuanAudioTranscriptions(request)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		require.Equal(t, body, response.Body.String())
	}
	require.Equal(t, 2, upstream.callCount(), "one upstream call per incoming request")
	require.Len(t, repository.receipts, 1, "the same request-level observation is idempotent")
	require.Zero(t, repository.settles)
	require.Zero(t, repository.debit)
	for _, receipt := range repository.receipts {
		require.Equal(t, "complete", receipt.CompletionState)
		require.Equal(t, "unresolved", receipt.ResolutionState)
		require.Equal(t, service.UsageSettlementNeedsReview, receipt.State)
		require.Zero(t, receipt.Usage.InputTokens)
		require.Zero(t, receipt.Command.APIKeyQuotaCost)
		require.Zero(t, receipt.Command.APIKeyRateLimitCost)
		require.Zero(t, receipt.Command.AccountQuotaCost)
		require.NotContains(t, fmt.Sprint(receipt), "private")
	}
}
