package service

import (
	"bytes"
	"context"
	"errors"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGatewayRequestPlanExitMatrix(t *testing.T) {
	for _, exit := range []string{"success", "failure", "canceled", "committed"} {
		t.Run(exit, func(t *testing.T) {
			plan := NewGatewayRequestPlan(GatewayRequestSpec{UserID: 1, APIKeyID: 2})
			var released atomic.Int32
			release := plan.Acquire(func() { released.Add(1) })
			if exit == "canceled" {
				plan.Cancel()
			}
			plan.Finalize(exit == "committed")
			release()
			plan.Finalize(false)
			require.EqualValues(t, 1, released.Load())
			require.True(t, plan.Record(), "disconnect cannot discard already metered usage")
			require.False(t, plan.Record())
			require.Empty(t, plan.State().ReceiptState, "a client response is not a receipt")
			plan.setReceiptState(UsageSettlementPending)
			plan.setReceiptState(UsageSettlementSettled)
			plan.setReceiptState(UsageSettlementPending)
			require.Equal(t, UsageSettlementSettled, plan.State().ReceiptState)
			require.Equal(t, exit == "committed", plan.State().ClientCommitted)
		})
	}
}

func TestGatewayRequestPlanConcurrentAcquireCancelRecord(t *testing.T) {
	plan := NewGatewayRequestPlan(GatewayRequestSpec{})
	var released, records atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			release := plan.Acquire(func() { released.Add(1) })
			defer release()
			if plan.Record() {
				records.Add(1)
			}
			plan.Cancel()
			plan.Finalize(true)
		}()
	}
	wg.Wait()
	require.EqualValues(t, 100, released.Load())
	require.EqualValues(t, 1, records.Load())
	require.True(t, plan.State().Canceled)
	require.True(t, plan.State().ClientCommitted)
}

func TestGatewayRequestPlanFreezesPublishedPricingAndChannels(t *testing.T) {
	prices := &PricingService{localHash: "v1", pricingData: map[string]*LiteLLMModelPricing{"fixture-model": {InputCostPerToken: 0.001, OutputCostPerToken: 0.002}}}
	billing := NewBillingService(nil, prices)
	channels := &ChannelService{}
	oldChannel := newEmptyChannelCache()
	oldChannel.loadedAt = time.Now()
	channels.cache.Store(oldChannel)
	ctx, plan := captureGatewayRequestPlan(context.Background(), GatewayRequestSpec{Model: "fixture-model", MaxAccountSwitches: 3}, nil, billing, channels)
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
	resolver := NewModelPricingResolver(nil, billing)
	require.Equal(t, 0.001, resolver.Resolve(ctx, PricingInput{Model: "fixture-model"}).BasePricing.InputPricePerToken)
	captured, err := channels.loadCache(ctx)
	require.NoError(t, err)
	require.Same(t, oldChannel, captured)
	require.Equal(t, "v1:", plan.Snapshot().PriceVersion)
	copy := plan.Snapshot()
	copy.MaxAccountSwitches = 99
	require.Equal(t, 3, plan.Snapshot().MaxAccountSwitches)
	_, anthropicAt := WithGatewayTokenRequestPricing(ctx)
	_, openAIAt := (&OpenAIGatewayService{}).WithOpenAIRequestPricingContext(ctx, nil)
	require.Equal(t, plan.Snapshot().PricingAt, anthropicAt)
	require.Equal(t, anthropicAt, openAIAt)
	_, reused := (&OpenAIGatewayService{}).CaptureRequestPlan(ctx, GatewayRequestSpec{Model: "changed", MaxAccountSwitches: 99})
	require.Same(t, plan, reused, "adapter handoff must not replace the execution owner")
	require.Equal(t, "fixture-model", reused.Snapshot().Model)
}

func TestGatewayRequestPlanForwardingSnapshot(t *testing.T) {
	plan := NewGatewayRequestPlan(GatewayRequestSpec{})
	plan.forwarding = &gatewayForwardingSettingsResult{fp: true, mp: false, cch: true, clientDatelineNormalization: true}
	ctx := WithGatewayRequestPlan(context.Background(), plan)
	settings := &SettingService{}
	fp, mp, cch := settings.GetGatewayForwardingSettings(ctx)
	require.True(t, fp)
	require.False(t, mp)
	require.True(t, cch)
	require.True(t, settings.IsClientDatelineNormalizationEnabled(ctx))
}

func TestGatewayRequestPlanRetainsRootAttemptBudget(t *testing.T) {
	ctx, cancel := WithAttemptBudget(context.Background(), 1, time.Minute)
	defer cancel()
	ctx, plan := captureGatewayRequestPlan(ctx, GatewayRequestSpec{}, nil, nil, nil)
	finish, err := BeginGenerationAttempt(ctx, 7)
	require.NoError(t, err)
	finish(503, nil)
	require.Equal(t, 1, plan.Snapshot().GenerationAttempts)
	require.Len(t, plan.Attempts(), 1)
	detached := context.WithoutCancel(ctx)
	_, err = BeginGenerationAttempt(detached, 8)
	require.ErrorIs(t, err, ErrAttemptBudgetExhausted)
	require.Len(t, plan.Attempts(), 1)
}

func TestGatewayRequestPlanAffinityIngressMatrix(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.SessionAffinityEnabled = true
	cfg.Gateway.AffinityHMACKey, cfg.Gateway.AffinityHMACKeyVersion = "fixture-secret", "test"
	svc := &GatewayService{cfg: cfg}
	for _, tc := range []struct{ protocol, route, first, second string }{
		{"anthropic", "/v1/messages", `{"model":"m","messages":[{"role":"user","content":"anchor"}]}`, `{"model":"m","messages":[{"role":"user","content":"anchor"},{"role":"assistant","content":"reply"},{"role":"user","content":"next"}]}`},
		{"openai", "/v1/chat/completions", `{"model":"m","messages":[{"role":"user","content":[{"type":"text","text":"anchor"}]}]}`, `{"model":"m","messages":[{"role":"user","content":[{"type":"text","text":"anchor"}]},{"role":"user","content":"next"}]}`},
		{"responses", "/v1/responses", `{"model":"m","input":[{"role":"user","content":"anchor"}]}`, `{"model":"m","input":[{"role":"user","content":"anchor"},{"role":"user","content":"next"}]}`},
		{"gemini", "/v1beta/models/m:generateContent", `{"contents":[{"role":"user","parts":[{"inlineData":{"mimeType":"image/png","data":"AA=="}}]}]}`, `{"contents":[{"role":"user","parts":[{"inlineData":{"mimeType":"image/png","data":"AA=="}}]},{"role":"user","parts":[{"text":"next"}]}]}`},
	} {
		t.Run(tc.protocol, func(t *testing.T) {
			ctx, _ := svc.CaptureRequestPlan(context.Background(), GatewayRequestSpec{UserID: 1, APIKeyID: 2, BillingGroupID: 3, Protocol: tc.protocol, Route: tc.route, Model: "m"})
			first, err := ParseGatewayRequest(NewRequestBodyRef([]byte(tc.first)), tc.protocol)
			require.NoError(t, err)
			second, err := ParseGatewayRequest(NewRequestBodyRef([]byte(tc.second)), tc.protocol)
			require.NoError(t, err)
			hash := svc.GenerateRequestSessionHash(ctx, first, "")
			require.Contains(t, hash, "v2:binding:hmac:")
			require.Equal(t, hash, svc.GenerateRequestSessionHash(ctx, second, ""))
			require.True(t, bytes.Equal([]byte(tc.first), first.Body.Bytes()), "identity must not rewrite prefix")
			require.EqualValues(t, 1, first.SessionContext.OwnerID)
			require.Equal(t, tc.route, first.SessionContext.RouteScope)
			other, _ := svc.CaptureRequestPlan(context.Background(), GatewayRequestSpec{UserID: 9, APIKeyID: 2, BillingGroupID: 3, Protocol: tc.protocol, Route: tc.route, Model: "m"})
			require.NotEqual(t, hash, svc.GenerateRequestSessionHash(other, first, ""))
		})
	}
}
func TestGatewayRequestPlanAffinityDisabledMatchesLegacy(t *testing.T) {
	svc := &GatewayService{cfg: &config.Config{}}
	parsed, err := ParseGatewayRequest(NewRequestBodyRef([]byte(`{"model":"m","messages":[{"role":"user","content":"hello"}]}`)), "anthropic")
	require.NoError(t, err)
	parsed.SessionContext = &SessionContext{ClientIP: "1.2.3.4", UserAgent: "client", APIKeyID: 2}
	ctx, _ := svc.CaptureRequestPlan(context.Background(), GatewayRequestSpec{UserID: 1, APIKeyID: 2, Protocol: "anthropic", Route: "/v1/messages", Model: "m"})
	require.Equal(t, svc.GenerateSessionHash(parsed), svc.GenerateRequestSessionHash(ctx, parsed, "ignored"))
	require.Zero(t, parsed.SessionContext.OwnerID, "disabled keeps NEXT context/hash behavior")
	svc.cfg.Gateway.SessionAffinityEnabled = true
	empty, err := ParseGatewayRequest(NewRequestBodyRef([]byte(`{"model":"m","messages":[]}`)), "anthropic")
	require.NoError(t, err)
	require.Empty(t, svc.GenerateRequestSessionHash(ctx, empty, ""))
}
func TestGatewayRequestPlanWebSocketTwoTurnsAndRetry(t *testing.T) {
	prices := &PricingService{localHash: "first", pricingData: map[string]*LiteLLMModelPricing{"m": {InputCostPerToken: 0.001}}}
	svc := &OpenAIGatewayService{billingService: NewBillingService(nil, prices)}
	spec := GatewayRequestSpec{UserID: 1, APIKeyID: 2, Model: "m", Protocol: "responses"}
	ctx, first := svc.CaptureRequestPlan(context.Background(), spec)
	prices.mu.Lock()
	prices.pricingData = map[string]*LiteLLMModelPricing{"m": {InputCostPerToken: 0.5}}
	prices.localHash = "second"
	prices.mu.Unlock()
	_, retry := svc.CaptureRequestPlan(ctx, spec)
	require.Same(t, first, retry)
	price, err := first.billing.GetModelPricing("m")
	require.NoError(t, err)
	require.Equal(t, 0.001, price.InputPricePerToken)
	next, second := svc.CaptureRequestTurn(ctx, spec)
	require.NotSame(t, first, second)
	require.NotSame(t, AffinityBindingStateFromContext(ctx), AffinityBindingStateFromContext(next))
	price, err = second.billing.GetModelPricing("m")
	require.NoError(t, err)
	require.Equal(t, 0.5, price.InputPricePerToken)
	require.True(t, first.Record())
	require.False(t, retry.Record())
	require.True(t, second.Record())
}
func TestGatewayRequestPlanDurableReceiptFaults(t *testing.T) {
	for _, tc := range []struct {
		name                           string
		ackLost, writeFails, readFails bool
	}{
		{name: "success"}, {name: "ack lost", ackLost: true, writeFails: true}, {name: "write failed", writeFails: true}, {name: "unknown commit", ackLost: true, writeFails: true, readFails: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan := NewGatewayRequestPlan(GatewayRequestSpec{})
			ctx := WithGatewayRequestPlan(context.Background(), plan)
			repo := &durableReceiptStore{commitBeforeError: tc.ackLost}
			if tc.writeFails {
				repo.persistErr = errors.New("write failed")
			}
			if tc.readFails {
				repo.lookupErr = errors.New("read failed")
			}
			svc := NewUsageSettlementService(repo, nil, nil)
			require.True(t, plan.Record())
			err := svc.Submit(ctx, durableReceiptFixture(), nil)
			if tc.writeFails && (!tc.ackLost || tc.readFails) {
				require.Error(t, err)
				require.Empty(t, plan.State().ReceiptState)
			} else {
				require.NoError(t, err)
				require.Equal(t, UsageSettlementSettled, plan.State().ReceiptState)
				require.Equal(t, "durable", plan.State().ReceiptKey.RequestID)
			}
			require.False(t, plan.Record(), "producer claim is not persistence acknowledgement")
			if tc.ackLost {
				repo.lookupErr = nil
				require.NoError(t, svc.Recover(context.Background()))
				require.Equal(t, 1, repo.applied)
			}
		})
	}
}
