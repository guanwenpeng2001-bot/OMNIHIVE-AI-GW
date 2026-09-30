//go:build unit

package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const observedQianwenTTSUsage = `{"characters":5,"input_tokens":2,"total_tokens":10,"output_tokens":8,"input_tokens_details":{"text_tokens":2},"output_tokens_details":{"text_tokens":0,"audio_tokens":8}}`

func TestQianwenRealtimeCharactersAcceptsDocumentedTokenMetadata(test *testing.T) {
	for _, usage := range []string{observedQianwenTTSUsage, `{"characters":5}`, `{"characters":5,"input_tokens":2}`, `{"characters":5,"output_tokens_details":{"audio_tokens":8}}`, `{"characters":0,"total_tokens":0}`} {
		characters, err := QianwenRealtimeCharacters([]byte(usage))
		require.NoError(test, err, usage)
		if strings.Contains(usage, `"characters":0`) {
			require.Zero(test, characters)
		} else {
			require.EqualValues(test, 5, characters)
		}
	}
	for _, usage := range []string{
		`{"input_tokens":2,"output_tokens":8}`, `{"characters":5,"seconds":1}`,
		`{"characters":5,"input_tokens":null}`, `{"characters":5,"input_tokens":-1}`,
		`{"characters":5,"input_tokens":1.5}`, `{"characters":5,"input_tokens":"2"}`,
		`{"characters":5,"input_tokens":1e999}`, `{"characters":NaN}`, `{"characters":5,"input_tokens":10000001}`,
		`{"characters":5,"input_tokens_details":null}`, `{"characters":5,"input_tokens_details":[]}`,
		`{"characters":5,"input_tokens_details":{"audio_tokens":1}}`,
		`{"characters":5,"output_tokens_details":{"audio_tokens":-1}}`,
		`{"characters":5,"output_tokens_details":{"audio_tokens":null}}`,
		`{"characters":5,"output_tokens_details":{"audio_tokens":1e999}}`,
		`{"characters":5,"input_tokens":2,"input_tokens_details":{"text_tokens":3}}`,
		`{"characters":5,"output_tokens":8,"output_tokens_details":{"text_tokens":1,"audio_tokens":8}}`,
		`{"characters":5,"input_tokens":2,"output_tokens":8,"total_tokens":11}`,
	} {
		_, err := QianwenRealtimeCharacters([]byte(usage))
		require.ErrorIs(test, err, ErrUpstreamJobUnsupportedUsage, usage)
	}
}

func TestQianwenObservedTTSUsageChargesCharactersOnce(test *testing.T) {
	session := realtimeSessionFixture()
	repository := &fileBatchMemoryRepo{jobs: map[string]*UpstreamJob{}, claims: map[string]string{}}
	jobs := NewUpstreamJobService(repository, nil, QianwenAsyncJobCharge)
	receipt, err := jobs.RecordQianwenRealtimeTurn(context.Background(), session, "observed-turn", []byte(observedQianwenTTSUsage), "completed")
	require.NoError(test, err)
	require.NotNil(test, receipt.ChargedUsageLogID)
	require.Equal(test, 1, repository.charges)
	require.JSONEq(test, observedQianwenTTSUsage, string(receipt.UsageJSON))
	log, command, err := QianwenRealtimeCharge(receipt)
	require.NoError(test, err)
	require.Equal(test, QuantizeUsageBillingAmount(5*session.Billing.UnitPrice), command.BalanceCost)
	require.Equal(test, "qwen3_tts_characters:5", *log.BillingTier)
	require.Zero(test, log.InputTokens)
	require.Zero(test, log.OutputTokens)
	frozen := receipt.Billing.PricingSnapshot
	jobs = NewUpstreamJobService(repository, nil, QianwenAsyncJobCharge)
	replay, err := jobs.RecordQianwenRealtimeTurn(context.Background(), session, "observed-turn", []byte(observedQianwenTTSUsage), "completed")
	require.NoError(test, err)
	require.Equal(test, 1, repository.charges)
	require.Equal(test, frozen, replay.Billing.PricingSnapshot)
	_, err = jobs.RecordQianwenRealtimeTurn(context.Background(), session, "observed-turn", []byte(strings.Replace(observedQianwenTTSUsage, `"characters":5`, `"characters":6`, 1)), "completed")
	require.ErrorIs(test, err, ErrQianwenRealtimeConflict)
	require.Equal(test, 1, repository.charges)
}

func TestQianwenTTSUnknownUsageRemainsReviewEvidence(test *testing.T) {
	for _, usage := range []string{`{"characters":5,"unknown_meter":4}`, `{"input_tokens":2,"output_tokens":8}`, `{"characters":null}`, `{"characters":5,"output_tokens":-1}`} {
		repository := &fileBatchMemoryRepo{jobs: map[string]*UpstreamJob{}, claims: map[string]string{}}
		jobs := NewUpstreamJobService(repository, nil, QianwenAsyncJobCharge)
		receipt, err := jobs.RecordQianwenRealtimeTurn(context.Background(), realtimeSessionFixture(), "review-turn", []byte(usage), "completed")
		require.NoError(test, err)
		require.JSONEq(test, usage, string(receipt.UsageJSON))
		require.Equal(test, "unsupported_usage", receipt.ErrorCode)
		require.Equal(test, PricingStateError, receipt.Billing.PricingState)
		require.Empty(test, receipt.Billing.PricingSnapshot)
		require.Nil(test, receipt.ChargedUsageLogID)
		require.Zero(test, repository.charges)
	}
	// The TTS compatibility exception must not make character evidence valid for
	// other families, or weaken their required modality/duration contracts.
	for _, model := range []string{"qwen3-omni-flash-realtime", "qwen3-asr-flash-realtime"} {
		_, err := qianwenRealtimeQuantities(model, json.RawMessage(observedQianwenTTSUsage))
		require.ErrorIs(test, err, ErrUpstreamJobUnsupportedUsage)
	}
}
