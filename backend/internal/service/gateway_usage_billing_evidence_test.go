//go:build unit

package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGatewaysShareConfiguredPriceSelection(t *testing.T) {
	rate := 0.01
	resolver := NewModelPricingResolver(nil, NewBillingService(nil, nil))
	g, o := &GatewayService{resolver: resolver}, &OpenAIGatewayService{resolver: resolver}
	group := &Group{ID: 2, ModelPricing: []ChannelModelPricing{{Models: []string{"custom"}, InputPrice: &rate, OutputPrice: &rate}}}
	for _, key := range []*APIKey{nil, {}, {Group: group}} {
		a := g.resolveChannelPricing(t.Context(), "custom", key)
		b := o.resolveOpenAIChannelPricing(t.Context(), "custom", key)
		require.Equal(t, a, b)
		if key != nil && key.Group != nil {
			require.NotNil(t, a)
			require.Equal(t, PricingSourceGroup, a.Source)
		} else {
			require.Nil(t, a)
		}
	}
	channel := newResolverWithChannel(t, []ChannelModelPricing{{Platform: PlatformAnthropic, Models: []string{"custom"}, InputPrice: &rate, OutputPrice: &rate}})
	g.resolver, o.resolver = channel, channel
	for _, key := range []*APIKey{{GroupID: groupIDPtr()}, {Group: &Group{ID: *groupIDPtr(), Platform: PlatformAnthropic}}} {
		left := g.resolveChannelPricing(t.Context(), "custom", key)
		right := o.resolveOpenAIChannelPricing(t.Context(), "custom", key)
		require.NotNil(t, left)
		require.Equal(t, PricingSourceChannel, left.Source)
		require.Equal(t, left, right)
	}

}

func TestGatewaysAssembleIdenticalFrozenReceiptFromEquivalentUsage(t *testing.T) {
	ctx := t.Context()
	when := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	logs := &openAIRecordUsageLogRepoStub{inserted: true}
	g, key := newGatewayRecordUsageServiceWithResolverForTest(logs)
	o := newOpenAIRecordUsageServiceForTest(logs, nil, nil, nil)
	o.resolver = g.resolver
	account := &Account{ID: 3, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	user := &User{ID: 2}
	groupPrice := 0.00001
	key.Group.ModelPricing = []ChannelModelPricing{{Models: []string{"custom-meter"}, InputPrice: &groupPrice, OutputPrice: &groupPrice, CacheReadPrice: &groupPrice}}
	effort := "high"
	require.NoError(t, g.RecordUsage(ctx, &RecordUsageInput{APIKey: key, User: user, Account: account, PricingAt: when,
		Result: &ForwardResult{RequestID: "same", Model: "custom-meter", UpstreamModel: "custom-meter", UpstreamResponseModel: "custom-meter", ReasoningEffort: &effort,
			Usage: ClaudeUsage{InputTokens: 7, CacheReadInputTokens: 3, OutputTokens: 4}}}))
	require.NoError(t, o.RecordUsage(ctx, &OpenAIRecordUsageInput{APIKey: key, User: user, Account: account, PricingAt: when,
		Result: &OpenAIForwardResult{RequestID: "same", Model: "custom-meter", UpstreamModel: "custom-meter", UpstreamResponseModel: "custom-meter", ReasoningEffort: &effort,
			Usage: OpenAIUsage{InputTokens: 10, CacheReadInputTokens: 3, OutputTokens: 4}}}))
	first := g.usageSettlement.repo.(*recordUsageReceiptFixture).receipt
	second := receiptFixture(t, o).receipt
	require.NotNil(t, first)
	require.NotNil(t, second)
	require.Equal(t, UsageSettlementSettled, first.State)
	require.Equal(t, first.Command, second.Command)
	require.Equal(t, 7, first.Usage.InputTokens)
	require.Equal(t, 3, first.Usage.CacheReadTokens)
	require.Equal(t, first.Usage.PricingState, second.Usage.PricingState)
	require.Equal(t, first.Usage.PricingSnapshot, second.Usage.PricingSnapshot)
	require.Equal(t, first.Usage.UpstreamResponseModel, second.Usage.UpstreamResponseModel)
	require.Equal(t, first.Usage.ReasoningEffort, second.Usage.ReasoningEffort)
}

func TestInterruptedStreamMissingOutputNeedsReview(t *testing.T) {
	for _, endpoint := range []string{"/v1/chat/completions", "/v1/responses", "/v1/messages"} {
		t.Run(endpoint, func(t *testing.T) {
			logs := &openAIRecordUsageLogRepoStub{inserted: true}
			g, key := newGatewayRecordUsageServiceWithResolverForTest(logs)
			user := &User{ID: 2}
			account := &Account{ID: 3, Type: AccountTypeAPIKey, Platform: PlatformOpenAI}
			var receipt *UsageSettlementReceipt
			var err error
			if endpoint == "/v1/messages" {
				account.Platform = PlatformAnthropic
				err = g.RecordUsage(t.Context(), &RecordUsageInput{APIKey: key, User: user, Account: account, InboundEndpoint: endpoint, Result: &ForwardResult{Model: "claude-sonnet-4", Stream: true, ClientDisconnect: true, RequestID: "interrupted", Usage: ClaudeUsage{InputTokens: 7}}})
				receipt = g.usageSettlement.repo.(*recordUsageReceiptFixture).receipt
			} else {
				o := newOpenAIRecordUsageServiceForTest(logs, nil, nil, nil)
				err = o.RecordUsage(t.Context(), &OpenAIRecordUsageInput{APIKey: key, User: user, Account: account, InboundEndpoint: endpoint, Result: &OpenAIForwardResult{Model: "gpt-5", Stream: true, ClientDisconnect: true, RequestID: "interrupted"}})
				receipt = receiptFixture(t, o).receipt
			}
			require.ErrorIs(t, err, ErrUsageSettlementNeedsReview)
			require.NotNil(t, receipt)
			require.Equal(t, UsageSettlementNeedsReview, receipt.State)
			require.Equal(t, "unresolved", receipt.ResolutionState)
		})
	}
}

type disconnectOnThirdChunk struct {
	*httptest.ResponseRecorder
	cancel context.CancelFunc
}

func (w disconnectOnThirdChunk) Write(p []byte) (int, error) {
	n, err := w.ResponseRecorder.Write(p)
	if strings.Contains(string(p), "visible-three") {
		w.cancel()
	}
	return n, err
}
func (w disconnectOnThirdChunk) WriteString(s string) (int, error) { return w.Write([]byte(s)) }

func TestDeliveredStreamWithCanceledUpstreamProducesReview(t *testing.T) {
	for _, endpoint := range []string{"/v1/chat/completions", "/v1/responses", "/v1/messages"} {
		t.Run(endpoint, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(disconnectOnThirdChunk{rec, cancel})
			body := []byte(`{"model":"gpt-5.4","messages":[{"role":"user","content":"hi"}],"max_tokens":8,"stream":true}`)
			if endpoint == "/v1/responses" {
				body = []byte(`{"model":"gpt-5.4","input":"hi","max_output_tokens":8,"stream":true}`)
			}
			c.Request = httptest.NewRequestWithContext(ctx, "POST", endpoint, strings.NewReader(string(body)))
			var frames strings.Builder
			for _, word := range []string{"visible-one", "visible-two", "visible-three"} {
				fmt.Fprintf(&frames, "data: {\"id\":\"chatcmpl-stream\",\"model\":\"gpt-5.4\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":%q}}]}\n\n", word)
			}
			up := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(frames.String()))}}
			svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: up}
			account := forceChatMessagesFallbackAccount()
			var res *OpenAIForwardResult
			switch endpoint {
			case "/v1/chat/completions":
				res, _ = svc.ForwardAsChatCompletions(ctx, c, account, body, "", "")
			case "/v1/responses":
				res, _ = svc.Forward(ctx, c, account, body)
			default:
				res, _ = svc.ForwardAsAnthropic(ctx, c, account, body, "", "")
			}
			require.Contains(t, rec.Body.String(), "visible-three")
			require.ErrorIs(t, ctx.Err(), context.Canceled)
			require.NotNil(t, res)
			require.True(t, res.ClientDisconnect)
			logs := &openAIRecordUsageLogRepoStub{inserted: true}
			billing := newOpenAIRecordUsageServiceForTest(logs, nil, nil, nil)
			_, key := newGatewayRecordUsageServiceWithResolverForTest(logs)
			err := billing.RecordUsage(context.WithoutCancel(ctx), &OpenAIRecordUsageInput{APIKey: key, User: &User{ID: 2}, Account: account, InboundEndpoint: endpoint, Result: res})
			require.ErrorIs(t, err, ErrUsageSettlementNeedsReview)
			require.Equal(t, UsageSettlementNeedsReview, receiptFixture(t, billing).receipt.State)
		})
	}
}
