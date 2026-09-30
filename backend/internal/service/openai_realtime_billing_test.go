//go:build unit

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const openAIRealtimeUsageFixture = `{"total_tokens":35,"input_tokens":25,"output_tokens":10,"input_token_details":{"text_tokens":10,"audio_tokens":12,"image_tokens":3,"cached_tokens":6,"cached_tokens_details":{"text_tokens":2,"audio_tokens":3,"image_tokens":1}},"output_token_details":{"text_tokens":4,"audio_tokens":6}}`

func openAIRealtimeSessionFixture(priced bool) *UpstreamJob {
	session := &UpstreamJob{PublicRequestID: "openai-session", Platform: PlatformOpenAI, Family: "realtime_session", UserID: 1, APIKeyID: 2, AccountID: 3, Billing: UpstreamJobBilling{Model: "gpt-realtime-2.1", RequestedModel: "public-realtime", RateMultiplier: 2, AccountRateMultiplier: 3, KeyQuota: true, KeyRateLimit: true, AccountQuota: true, Realtime: &QianwenRealtimeSnapshot{SessionID: "openai-session", PriceVersion: "openai-realtime-operator-tokens"}}}
	if priced {
		sales, cost := 0.001, 0.0005
		prices := []ModelUnitPrice{}
		for _, dimension := range []string{"input_text", "input_audio", "input_image", "cached_input_text", "cached_input_audio", "cached_input_image", "output_text", "output_audio"} {
			prices = append(prices, ModelUnitPrice{Unit: "token", Dimension: dimension, Currency: "USD", Sales: &sales, Cost: &cost})
		}
		session.Billing.Realtime.TokenPricing = &ModelPricingDescriptor{Model: "gpt-realtime-2.1", Platform: PlatformOpenAI, Source: PricingSourceGroup, PricingAt: time.Now().UTC(), Variants: []ModelPriceVariant{{ID: "gpt-realtime-2.1", Canonical: "gpt-realtime-2.1", Prices: prices}}}
	}
	return session
}

func TestOpenAIRealtimeUsageSchema(t *testing.T) {
	usage, err := openAIRealtimeUsage([]byte(openAIRealtimeUsageFixture))
	require.NoError(t, err)
	require.Equal(t, 19, usage.input-usage.cached)
	require.Equal(t, 9, usage.dimensions["input_audio"])
	require.Equal(t, 3, usage.dimensions["cached_input_audio"])
	for _, raw := range []string{`null`, `{}`, `{"input_tokens":1,"output_tokens":0}`, `{"input_tokens":-1,"output_tokens":0}`, `{"input_tokens":1.5,"output_tokens":0}`, `{"input_tokens":1,"output_tokens":0,"input_tokens_details":{"text_tokens":1},"output_tokens_details":{}}`, `{"input_tokens":1,"output_tokens":0,"input_token_details":{"text_tokens":1,"cached_tokens":1},"output_token_details":{}}`, `{"total_tokens":2,"input_tokens":1,"output_tokens":0,"input_token_details":{"text_tokens":1},"output_token_details":{}}`, `{"input_tokens":1,"output_tokens":0,"input_token_details":{"text_tokens":1,"unknown":2},"output_token_details":{}}`} {
		_, err := openAIRealtimeUsage([]byte(raw))
		require.ErrorIs(t, err, ErrUpstreamJobUnsupportedUsage, raw)
	}
}

func TestOpenAIRealtimeReceiptPricingReplayAndUnknownUsage(t *testing.T) {
	repository := &fileBatchMemoryRepo{jobs: map[string]*UpstreamJob{}, claims: map[string]string{}}
	jobs := NewUpstreamJobService(repository, nil, GatewayResourceJobCharge)
	session := openAIRealtimeSessionFixture(true)
	first, err := jobs.RecordOpenAIRealtimeTurn(context.Background(), session, "response-priced", []byte(openAIRealtimeUsageFixture), "completed")
	require.NoError(t, err)
	require.NotNil(t, first.ChargedUsageLogID)
	require.Equal(t, 1, repository.charges)
	log, command, err := OpenAIRealtimeCharge(first)
	require.NoError(t, err)
	require.InDelta(t, 0.035, log.TotalCost, 1e-8)
	require.InDelta(t, 0.07, command.BalanceCost, 1e-8)
	require.InDelta(t, 0.0525, command.AccountQuotaCost, 1e-8)
	require.Equal(t, 19, log.InputTokens)
	require.Equal(t, 6, log.CacheReadTokens)
	recalculated, err := RecalculatePricingSnapshot(first.Billing.PricingSnapshot)
	require.NoError(t, err)
	require.InDelta(t, log.ActualCost, recalculated.ActualCost, 1e-8)
	_, err = jobs.RecordOpenAIRealtimeTurn(context.Background(), session, "response-priced", []byte(openAIRealtimeUsageFixture), "completed")
	require.NoError(t, err)
	require.Equal(t, 1, repository.charges)
	_, err = jobs.RecordOpenAIRealtimeTurn(context.Background(), session, "response-priced", []byte(`{"changed":true}`), "completed")
	require.ErrorIs(t, err, ErrQianwenRealtimeConflict)
	for index, raw := range []string{`null`, `{"seconds":5}`, openAIRealtimeUsageFixture} {
		job, err := jobs.RecordOpenAIRealtimeTurn(context.Background(), openAIRealtimeSessionFixture(false), fmt.Sprintf("response-unpriced-%d", index), []byte(raw), "completed")
		require.NoError(t, err)
		require.Nil(t, job.ChargedUsageLogID)
		require.JSONEq(t, raw, string(job.UsageJSON))
		require.True(t, job.Terminal())
		if raw == openAIRealtimeUsageFixture {
			require.Equal(t, PricingStateUnpriced, job.Billing.PricingState)
		} else {
			require.Equal(t, PricingStateError, job.Billing.PricingState)
		}
	}
	require.Equal(t, 1, repository.charges)
}

func TestOpenAIRealtimeSnapshotRoundTrip(t *testing.T) {
	fixture := openAIRealtimeSessionFixture(true)
	group := &Group{Platform: PlatformOpenAI, RateMultiplier: 1, ModelPricing: []ChannelModelPricing{{Platform: PlatformOpenAI, PriceVariants: fixture.Billing.Realtime.TokenPricing.Variants}}}
	billing, err := OpenAIRealtimeBillingSnapshot(context.Background(), group, openAIActionAccount(), &APIKey{}, "gpt-realtime-2.1", nil)
	require.NoError(t, err)
	require.NotNil(t, billing.Realtime.TokenPricing)
	encoded, err := json.Marshal(billing)
	require.NoError(t, err)
	var restored UpstreamJobBilling
	require.NoError(t, json.Unmarshal(encoded, &restored))
	require.Equal(t, billing, restored)
}

func TestOpenAIRealtimeDeclaredModelIsSeparateImmutableEvidence(t *testing.T) {
	repository := &fileBatchMemoryRepo{jobs: map[string]*UpstreamJob{}, claims: map[string]string{}}
	jobs := NewUpstreamJobService(repository, nil, GatewayResourceJobCharge)
	session := openAIRealtimeSessionFixture(true)
	job, err := jobs.RecordOpenAIRealtimeTurn(context.Background(), session, "response-declared", []byte(openAIRealtimeUsageFixture), "completed", "provider-declared-snapshot")
	require.NoError(t, err)
	require.Equal(t, "public-realtime", job.Billing.RequestedModel)
	require.Equal(t, "gpt-realtime-2.1", job.Billing.Model)
	require.Equal(t, "provider-declared-snapshot", job.Billing.Realtime.UpstreamResponseModel)
	require.Empty(t, session.Billing.Realtime.UpstreamResponseModel)
	log, _, err := OpenAIRealtimeCharge(job)
	require.NoError(t, err)
	require.Equal(t, "provider-declared-snapshot", *log.UpstreamResponseModel)
	_, err = jobs.RecordOpenAIRealtimeTurn(context.Background(), session, "response-declared", []byte(openAIRealtimeUsageFixture), "completed", "provider-declared-snapshot")
	require.NoError(t, err)
	require.Equal(t, 1, repository.charges)
	_, err = jobs.RecordOpenAIRealtimeTurn(context.Background(), session, "response-declared", []byte(openAIRealtimeUsageFixture), "completed", "different-declaration")
	require.ErrorIs(t, err, ErrQianwenRealtimeConflict)
	missing, err := jobs.RecordOpenAIRealtimeTurn(context.Background(), session, "response-unknown", []byte(openAIRealtimeUsageFixture), "completed")
	require.NoError(t, err)
	require.Empty(t, missing.Billing.Realtime.UpstreamResponseModel)
	log, _, err = OpenAIRealtimeCharge(missing)
	require.NoError(t, err)
	require.Nil(t, log.UpstreamResponseModel)
}
