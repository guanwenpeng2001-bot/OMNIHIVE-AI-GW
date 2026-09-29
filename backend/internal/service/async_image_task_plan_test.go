package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAsyncImageTaskPlanOwnsExecutionAndFrozenFacts(t *testing.T) {
	prices := &PricingService{localHash: "v1", pricingData: map[string]*LiteLLMModelPricing{"fixture-model": {InputCostPerToken: 0.001}}}
	billing := NewBillingService(nil, prices)
	channels := &ChannelService{}
	oldChannel := newEmptyChannelCache()
	oldChannel.loadedAt = time.Now()
	channels.cache.Store(oldChannel)
	svc := &OpenAIGatewayService{billingService: billing, channelService: channels, usageSettlement: NewUsageSettlementService(unlimitedTPMRepository{}, nil, nil)}
	parent, cancelParent := WithAttemptBudget(context.Background(), 1, time.Minute)
	defer cancelParent()
	parent, parentPlan := svc.CaptureRequestPlan(parent, GatewayRequestSpec{ExecutionID: "submission"})
	ctx, cancelTask := svc.CaptureAsyncImageTaskPlan(parent, GatewayRequestSpec{
		ExecutionID: "imgtask_accepted", Model: "fixture-model", MaxAccountSwitches: 1,
	}, time.Hour)
	defer cancelTask()
	plan := GatewayRequestPlanFromContext(ctx)
	require.NotSame(t, parentPlan, plan)
	require.NotSame(t, AttemptBudgetFromContext(parent), AttemptBudgetFromContext(ctx))
	require.NotSame(t, parentPlan.affinityState, plan.affinityState)
	require.Equal(t, 2, plan.Snapshot().GenerationAttempts)
	require.Equal(t, "job", plan.Snapshot().ExecutionKind)
	require.True(t, plan.Snapshot().Deadline.After(time.Now().Add(50*time.Minute)))
	CommitAttemptResponse(parent)
	parentPlan.Finalize(true)
	cancelParent()
	prices.mu.Lock()
	prices.pricingData = map[string]*LiteLLMModelPricing{"fixture-model": {InputCostPerToken: 0.5}}
	prices.localHash = "v2"
	prices.mu.Unlock()
	newChannel := newEmptyChannelCache()
	newChannel.loadedAt = time.Now()
	channels.cache.Store(newChannel)
	price, err := billing.forGatewayRequest(ctx).GetModelPricing("fixture-model")
	require.NoError(t, err)
	require.Equal(t, 0.001, price.InputPricePerToken)
	captured, err := channels.loadCache(ctx)
	require.NoError(t, err)
	require.Same(t, oldChannel, captured)
	require.Equal(t, "v1:", plan.Snapshot().PriceVersion)
	require.Same(t, plan.registrySnapshot, ModelRegistrySnapshotFromContext(ctx))
	_, reused := svc.CaptureRequestPlan(ctx, GatewayRequestSpec{MaxAccountSwitches: 99})
	require.Same(t, plan, reused)
	finish, err := BeginGenerationAttempt(ctx, 42)
	require.NoError(t, err)
	finish(http.StatusBadGateway, nil)
	transport, cancelTransport := AttemptExecutionContext(context.WithoutCancel(ctx))
	defer cancelTransport()
	require.NoError(t, transport.Err(), "submission cancellation must not bind to the task transport")
	cancelTask()
	select {
	case <-transport.Done():
	case <-time.After(time.Second):
		t.Fatal("task cancellation did not reach detached transport")
	}
	_, err = BeginGenerationAttempt(context.WithoutCancel(ctx), 42)
	require.ErrorIs(t, err, context.Canceled)
	require.Len(t, plan.Attempts(), 1)
	require.Empty(t, parentPlan.Attempts())
}
