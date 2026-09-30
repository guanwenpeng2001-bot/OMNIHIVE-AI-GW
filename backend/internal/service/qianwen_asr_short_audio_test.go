//go:build unit

package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const observedQianwenASRResponse = `{"request_id":"asr-provider","model":"qwen3-asr-flash","output":{"choices":[{"finish_reason":"stop","message":{"annotations":[{"emotion":"neutral","language":"zh","type":"audio_info"}],"content":[{"text":"你好。"}],"role":"assistant"}}]},"usage":{"audio_tokens":14,"input_tokens":14,"input_tokens_details":{"audio_tokens":14,"text_tokens":0},"output_tokens":9,"output_tokens_details":{"text_tokens":9},"seconds":0,"total_tokens":23}}`

func TestQianwenShortASRPreservesTranscriptAndReviewReceipt(test *testing.T) {
	for _, scenario := range []struct {
		name, replacement, reason string
		present                   bool
	}{
		{"observed zero", `"seconds":0`, "zero_duration", true},
		{"missing", `"different_field":0`, "missing_duration", false},
		{"null", `"seconds":null`, "invalid_duration", true},
		{"fractional", `"seconds":0.5`, "invalid_duration", true},
		{"negative", `"seconds":-1`, "invalid_duration", true},
		{"string", `"seconds":"1"`, "invalid_duration", true},
		{"overflow", `"seconds":1e999`, "invalid_duration", true},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			payload := strings.Replace(observedQianwenASRResponse, `"seconds":0`, scenario.replacement, 1)
			repository := &durableReceiptStore{}
			settlement := NewUsageSettlementService(repository, nil, nil)
			upstream := &httpUpstreamRecorder{resp: qianwenModelPage(payload, 200)}
			upstream.resp.Header.Set("Content-Type", "application/json")
			gateway := &OpenAIGatewayService{httpUpstream: upstream, usageSettlement: settlement}
			key := audioTestKey()
			key.UserID = 1
			account := qianwenTestAccount()
			account.Status = StatusActive
			account.Schedulable = true
			account.Credentials["model_mapping"] = map[string]any{"qwen3-asr-flash": "qwen3-asr-flash"}
			plan := NewGatewayRequestPlan(GatewayRequestSpec{Model: "qwen3-asr-flash", Action: "audio.stt", Protocol: "audio"})
			plan.settlement = settlement
			ctx := WithGatewayRequestPlan(context.WithValue(context.Background(), ctxkey.RequestID, "short-audio"), plan)
			recorder := httptest.NewRecorder()
			request, _ := gin.CreateTestContext(recorder)
			request.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", nil).WithContext(ctx)
			request.Set("api_key", key)
			body, err := QianwenAudioTranscriptionBody("qwen3-asr-flash", "audio/wav", []byte("RIFFfixture"))
			require.NoError(test, err)
			result, err := gateway.ForwardQianwenAudioCompat(ctx, request, account, body, "stt")
			require.NoError(test, err)
			require.Equal(test, 200, recorder.Code, recorder.Body.String())
			require.JSONEq(test, `{"text":"你好。"}`, recorder.Body.String())
			require.True(test, result.AudioUsageUnresolved)
			require.Nil(test, result.AudioUsage)
			require.Equal(test, OpenAIUsage{}, result.Usage)
			require.NotNil(test, repository.row)
			require.Equal(test, UsageSettlementNeedsReview, repository.row.State)
			require.Equal(test, "complete", repository.row.CompletionState)
			require.Equal(test, "unresolved", repository.row.ResolutionState)
			evidence := repository.row.Usage.PricingSnapshot
			require.Equal(test, scenario.present, gjson.Get(evidence, "seconds_present").Bool())
			require.Equal(test, scenario.reason, gjson.Get(evidence, "reason").String())
			require.Equal(test, gjson.GetBytes(canonicalQianwenAudioBody([]byte(payload)), "usage").Raw, gjson.Get(evidence, "usage").Raw, "preserve raw numbers without floating-point coercion")
			require.Equal(test, PricingStateError, repository.row.Usage.PricingState)
			require.Nil(test, repository.row.UsageLogID)
			require.Zero(test, repository.applied)
			require.False(test, plan.Record(), "the ordinary queued usage recorder must not become a second financial producer")
			require.Equal(test, UsageSettlementNeedsReview, plan.State().ReceiptState)
			require.NoError(test, gateway.recordQianwenASRUnresolved(ctx, result, key, account, []byte(payload)), "identical evidence replays idempotently")
			require.ErrorIs(test, gateway.recordQianwenASRUnresolved(ctx, result, key, account, []byte(strings.Replace(payload, `"total_tokens":23`, `"total_tokens":24`, 1))), ErrUsageBillingRequestConflict)
			_, err = gateway.calculateQianwenNativeAudioCost(ctx, result, key, 1)
			require.ErrorIs(test, err, ErrUpstreamJobUnsupportedUsage, "unresolved usage cannot fall back to token/free pricing")
		})
	}
}

func TestQianwenASRReviewFailureDoesNotCommitSuccess(test *testing.T) {
	for _, mode := range []string{"missing settlement", "failed persistence"} {
		test.Run(mode, func(test *testing.T) {
			upstream := &httpUpstreamRecorder{resp: qianwenModelPage(observedQianwenASRResponse, 200)}
			upstream.resp.Header.Set("Content-Type", "application/json")
			gateway := &OpenAIGatewayService{httpUpstream: upstream}
			if mode == "failed persistence" {
				gateway.usageSettlement = NewUsageSettlementService(&durableReceiptStore{persistErr: errors.New("fixture storage unavailable"), lookupErr: ErrUsageSettlementNotFound}, nil, nil)
			}
			key := audioTestKey()
			key.UserID = 1
			recorder := httptest.NewRecorder()
			request, _ := gin.CreateTestContext(recorder)
			request.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", nil)
			request.Set("api_key", key)
			result, err := gateway.ForwardQianwenAudioCompat(context.Background(), request, qianwenTestAccount(), []byte(audioASRRequest), "stt")
			require.Error(test, err)
			require.Nil(test, result)
			require.Equal(test, 503, recorder.Code)
			require.NotContains(test, recorder.Body.String(), "你好")
		})
	}
}

func TestQianwenASRIncompleteTranscriptIsNotReviewSuccess(test *testing.T) {
	for _, payload := range []string{strings.Replace(observedQianwenASRResponse, `"stop"`, `"length"`, 1), strings.Replace(observedQianwenASRResponse, `"text":"你好。"`, `"text":null`, 1), `{"usage":{"seconds":0},"output":{"task_id":"pending"}}`} {
		result := &OpenAIForwardResult{UpstreamModel: "qwen3-asr-flash"}
		require.Error(test, observeQianwenNativeAudio([]byte(payload), result))
		require.False(test, result.AudioUsageUnresolved)
		require.Nil(test, result.AudioUsage)
	}
}
