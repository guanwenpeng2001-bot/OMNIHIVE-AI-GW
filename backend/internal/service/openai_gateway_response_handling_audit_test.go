//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func gatewayAuditContext(body string) (*gin.Context, *httptest.ResponseRecorder, *http.Response) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest("POST", "/", nil)
	return c, rec, &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}, "X-Request-Id": {"request-a"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestResponsesAggregateUsageEvidence(t *testing.T) {
	const progressive = "data: {\"type\":\"response.in_progress\",\"usage\":{\"input_tokens\":7,\"output_tokens\":2,\"input_tokens_details\":{\"cached_tokens\":3}}}\n\n"
	for _, tc := range []struct {
		name, prefix, usage   string
		input, output, cached int
		complete              bool
	}{
		{"empty terminal", progressive, "{}", 7, 2, 3, true},
		{"placeholder", progressive, `{"input_tokens":0,"output_tokens":0}`, 7, 2, 3, true},
		{"partial terminal", progressive, `{"output_tokens":4}`, 7, 4, 3, true},
		{"true zero", "", `{"input_tokens":0,"output_tokens":0}`, 0, 0, 0, true},
		{"missing input", "", `{"output_tokens":4}`, 0, 4, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := tc.prefix + "data: " + `{"type":"response.completed","response":{"id":"resp_test","usage":` + tc.usage + `,"output":[]}}` + "\n\n"
			c, rec, resp := gatewayAuditContext(body)
			result, err := (&OpenAIGatewayService{}).handleNonStreamingResponse(context.Background(), resp, c, &Account{}, "m", "m")
			require.NoError(t, err)
			require.Equal(t, 200, rec.Code)
			require.True(t, gjson.Valid(rec.Body.String()))
			require.Equal(t, tc.input, result.InputTokens)
			require.Equal(t, tc.output, result.OutputTokens)
			require.Equal(t, tc.cached, result.CacheReadInputTokens)
			require.Equal(t, tc.complete, result.observation.known())
		})
	}
	for _, body := range []string{"", progressive} {
		c, rec, resp := gatewayAuditContext(body)
		result, err := (&OpenAIGatewayService{}).handleNonStreamingResponse(context.Background(), resp, c, &Account{}, "m", "m")
		require.Error(t, err)
		require.NotNil(t, result)
		require.Equal(t, 502, rec.Code)
		require.Equal(t, "upstream_error", gjson.Get(rec.Body.String(), "error.type").String())
		require.NotContains(t, rec.Body.String(), "data:")
	}
}

func TestRawChatStrictInterruptedReceipt(t *testing.T) {
	for _, tail := range []string{"", "data: [DONE]\n\n"} {
		stream := "data: " + `{"choices":[{"index":0,"delta":{"content":"answer"}}],"usage":{"prompt_tokens":7}}` + "\n\n" + tail
		c, rec, resp := gatewayAuditContext(stream)
		result, err := (&OpenAIGatewayService{}).streamRawChatCompletions(c, resp, &Account{Platform: PlatformMistral}, "gpt-5.4", "", "", nil, nil, time.Now(), 0)
		require.Error(t, err)
		require.NotNil(t, result)
		require.Contains(t, rec.Body.String(), "answer")
		require.NotContains(t, rec.Body.String(), "[DONE]")
		require.True(t, result.UsageIncomplete)
		require.Equal(t, 7, result.Usage.InputTokens)
		require.Equal(t, "request-a", result.RequestID)
		repo := &settlementCapture{}
		svc := newOpenAIRecordUsageServiceForTest(&openAIRecordUsageLogRepoStub{}, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
		svc.usageSettlement = NewUsageSettlementService(repo, nil, nil)
		err = svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{Result: result, User: &User{ID: 1}, APIKey: &APIKey{ID: 2}, Account: &Account{ID: 3, Platform: PlatformMistral}})
		require.ErrorIs(t, err, ErrUsageSettlementNeedsReview)
		require.Equal(t, "unresolved", repo.receipt.ResolutionState)
		require.Equal(t, 7, repo.receipt.Usage.InputTokens)
		require.Zero(t, repo.settles)
	}
}

func TestRawChatChoiceCompletionByIndex(t *testing.T) {
	const start = `{"choices":[{"index":1,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{"}}]}},{"index":0,"delta":{"content":"done"},"finish_reason":"stop"}]}`
	const finish = `{"choices":[{"index":1,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"}"}}]},"finish_reason":"tool_calls"}]}`
	for _, tc := range []struct {
		tail  string
		valid bool
	}{{"", false}, {"data: [DONE]\n\n", false}, {"data: " + strings.Replace(finish, `"index":1`, `"index":2`, 1) + "\n\n", false}, {"data: " + finish + "\n\n", true}} {
		c, rec, resp := gatewayAuditContext("data: " + start + "\n\n" + tc.tail)
		_, err := (&OpenAIGatewayService{}).streamRawChatCompletions(c, resp, &Account{Platform: PlatformOpenAI}, "m", "", "", nil, nil, time.Now(), 0, 2)
		if tc.valid {
			require.NoError(t, err)
		} else {
			require.Error(t, err)
			require.NotContains(t, rec.Body.String(), "[DONE]")
		}
	}
}

func TestRawChatBufferedDelivery(t *testing.T) {
	for _, invalid := range []string{"", "<html>private diagnostic</html>", "{}", "[]", `{"error":{"message":"private diagnostic"}}`} {
		c, rec, resp := gatewayAuditContext(invalid)
		result, err := (&OpenAIGatewayService{}).bufferRawChatCompletions(c, resp, &Account{}, "m", "", "", nil, nil, time.Now())
		require.Error(t, err)
		require.Nil(t, result)
		require.Equal(t, 502, rec.Code)
		require.NotContains(t, rec.Body.String(), "private diagnostic")
	}
	c, _, resp := gatewayAuditContext(compatibleChatJSON)
	c.Writer = &openAIRawStreamDisconnectedWriter{ResponseWriter: c.Writer}
	result, err := (&OpenAIGatewayService{}).bufferRawChatCompletions(c, resp, &Account{}, "m", "", "", nil, nil, time.Now())
	require.Error(t, err)
	require.True(t, result.ClientDisconnect)
	require.Equal(t, 9, result.Usage.InputTokens)
}

func TestChatBridgeFrameBoundaries(t *testing.T) {
	for _, messages := range []bool{true, false} {
		for _, tc := range []struct {
			name, frame string
			max         int
			valid       bool
		}{
			{"multiline", "data: {\"choices\":[\ndata: {\"index\":0,\"delta\":{\"content\":\"hello\"}}]}\n\n", 1 << 20, true},
			{"large", "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"" + strings.Repeat("a", 70000) + "\"}}]}\n\n", 1 << 20, true},
			{"over limit", "data: {\"choices\":[{\"delta\":{\"content\":\"" + strings.Repeat("a", 70000) + "\"}}]}\n\n", 68000, false},
			{"broken", "data: {\"choices\":[\n\n", 1 << 20, false},
		} {
			t.Run(tc.name, func(t *testing.T) {
				c, rec, resp := gatewayAuditContext(tc.frame + "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1}}\n\ndata: [DONE]\n\n")
				cfg := rawChatCompletionsTestConfig()
				cfg.Gateway.MaxLineSize = tc.max
				svc := &OpenAIGatewayService{cfg: cfg}
				var result *OpenAIForwardResult
				var err error
				terminal := "message_stop"
				account := &Account{Platform: PlatformOpenAI}
				if messages {
					result, err = svc.streamChatCompletionsAsAnthropic(c, resp, account, "m", "", "", nil, nil, time.Now())
				} else {
					terminal = "response.completed"
					result, err = svc.streamChatCompletionsAsResponses(c, resp, account, "m", nil, nil, false, nil, "", "", nil, nil, time.Now())
				}
				if tc.valid {
					require.NoError(t, err)
					require.Contains(t, rec.Body.String(), terminal)
				} else {
					require.Error(t, err)
					require.True(t, result.UsageIncomplete)
					require.NotContains(t, rec.Body.String(), terminal)
				}
			})
		}
	}
}

func TestMessagesFallbackReusesAdmission(t *testing.T) {
	gate := NewGatewayAdmissionGate(GatewayAdmissionConfig{MaxInflight: 1})
	cfg := &config.Config{}
	cfg.RunMode = config.RunModeSimple
	svc := &BillingCacheService{cfg: cfg, admissionGate: gate}
	plan := NewGatewayRequestPlan(GatewayRequestSpec{})
	ctx := WithGatewayRequestPlan(context.Background(), plan)
	require.NoError(t, svc.CheckBillingEligibility(ctx, nil, nil, &Group{ID: 1}, nil, PlatformAnthropic))
	require.NoError(t, svc.RecheckBillingEligibility(ctx, nil, nil, &Group{ID: 2}, nil, PlatformAnthropic))
	require.Equal(t, 1, gate.Inflight())
	plan.Finalize(false)
	require.Zero(t, gate.Inflight())
}

func TestCompositePreflightUsesSchedulerFilter(t *testing.T) {
	first := compositeOrderedTargetTestAccount(801, PlatformOpenAI, AccountTypeAPIKey)
	first.Credentials = map[string]any{"model_mapping": map[string]string{"wire": "wire"}}
	target := CompositeRouteTarget{TargetPlatform: PlatformOpenAI, UpstreamModel: "wire", Enabled: true}
	svc := &OpenAIGatewayService{accountRepo: &compositeOrderedTargetAccountRepo{byPlatform: map[string][]Account{PlatformOpenAI: {first}}}}
	ctx := context.Background()
	ok, err := svc.compositeTargetHasCandidate(ctx, nil, target, "wire")
	require.NoError(t, err)
	require.True(t, ok)
	privacy := context.WithValue(ctx, openAIGroupPrivacyRequirementContextKey{}, openAIGroupPrivacyRequirement{required: true})
	ok, err = svc.compositeTargetHasCandidate(privacy, nil, target, "wire")
	require.NoError(t, err)
	require.False(t, ok)
	for range 2 {
		svc.recordOpenAIAccountModelTransientFailure(&first, "wire", time.Now())
	}
	ok, err = svc.compositeTargetHasCandidate(ctx, nil, target, "wire")
	require.NoError(t, err)
	require.False(t, ok)
}

func TestCompositePreflightUsesSchedulerFilterAfterSelectionExhaustion(t *testing.T) {
	first := CompositeRouteTarget{Position: 0, TargetPlatform: PlatformOpenAI, UpstreamModel: "first"}
	next := CompositeRouteTarget{Position: 1, TargetPlatform: PlatformOpenAI, UpstreamModel: "second"}
	ctx := WithCompositeTargetAdvancement(WithCompositeRouteTargets(context.Background(), []CompositeRouteTarget{first, next}))
	ctx, ok := RemainingCompositeTargets(WithCompositeTargetSelectionContext(ctx, first))
	require.True(t, ok)
	target, ok, err := (&OpenAIGatewayService{}).CompositeTargetForAttempt(ctx, nil)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, next, target)
	_, ok = RemainingCompositeTargets(WithCompositeTargetSelectionContext(ctx, next))
	require.False(t, ok)
}

func TestGeminiMessagesUpstreamErrorIsPublic(t *testing.T) {
	c, rec, resp := gatewayAuditContext(`{"error":{"message":"private diagnostic"}}`)
	_, err := (&GeminiMessagesCompatService{}).handleNonStreamingResponse(c, resp, "m")
	require.Error(t, err)
	require.Equal(t, 502, rec.Code)
	require.NotContains(t, rec.Body.String(), "private diagnostic")
}

func TestWSReceiptDistinguishesMissingFromZero(t *testing.T) {
	for _, tc := range []struct {
		name        string
		observation *TokenUsageObservation
		review      bool
	}{
		{"missing", nil, true}, {"input only", &TokenUsageObservation{InputTokensKnown: true}, true}, {"known zero", &TokenUsageObservation{InputTokensKnown: true, OutputTokensKnown: true}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &settlementCapture{}
			gateway := newOpenAIRecordUsageServiceForTest(&openAIRecordUsageLogRepoStub{}, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
			gateway.usageSettlement = NewUsageSettlementService(repo, nil, nil)
			result := &OpenAIForwardResult{Model: "gpt-5.4", RequestID: "resp", OpenAIWSMode: true, Stream: true, TokenUsageObservation: tc.observation, ClientDisconnect: true}
			err := gateway.RecordUsage(context.Background(), &OpenAIRecordUsageInput{Result: result, User: &User{ID: 1}, APIKey: &APIKey{ID: 2}, Account: &Account{ID: 3, Platform: PlatformOpenAI}})
			if tc.review {
				require.ErrorIs(t, err, ErrUsageSettlementNeedsReview)
				require.Equal(t, "unresolved", repo.receipt.ResolutionState)
				require.Zero(t, repo.settles)
			} else {
				require.NoError(t, err)
				require.Equal(t, "resolved", repo.receipt.ResolutionState)
				require.Equal(t, 1, repo.settles)
			}
		})
	}
}
