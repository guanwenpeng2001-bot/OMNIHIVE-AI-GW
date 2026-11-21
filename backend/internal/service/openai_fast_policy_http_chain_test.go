package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Verify first-match semantics through the real forwarding loops and receipt
// entry, including a retry onto another legacy model. Re-evaluating rewritten
// tiers can both reintroduce a filtered priority and block a forced priority.
func TestOpenAIFastFirstMatchForwardAndRecordUsage(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		for _, retry := range []bool{false, true} {
			for _, stream := range []bool{false, true} {
				for _, scenario := range []struct {
					name, requestedTier, sentTier string
					rules                         []OpenAIFastPolicyRule
					cost                          float64
				}{
					{
						name: "priority_filter_then_missing_force", requestedTier: "priority", sentTier: "", cost: 0.035,
						rules: []OpenAIFastPolicyRule{
							{ServiceTier: OpenAIFastTierPriority, Action: BetaPolicyActionFilter, Scope: BetaPolicyScopeAll},
							{ServiceTier: OpenAIFastTierMissing, Action: OpenAIFastPolicyActionForcePriority, Scope: BetaPolicyScopeAll},
						},
					},
					{
						name: "missing_force_then_priority_block", sentTier: "priority", cost: 0.0875,
						rules: []OpenAIFastPolicyRule{
							{ServiceTier: OpenAIFastTierMissing, Action: OpenAIFastPolicyActionForcePriority, Scope: BetaPolicyScopeAll},
							{ServiceTier: OpenAIFastTierPriority, Action: BetaPolicyActionBlock, Scope: BetaPolicyScopeAll},
						},
					},
				} {
					t.Run(fmt.Sprintf("passthrough=%t/retry=%t/stream=%t/%s", passthrough, retry, stream, scenario.name), func(t *testing.T) {
						request := map[string]any{"model": "gpt-5.5", "instructions": "test", "stream": stream, "input": "hello"}
						if scenario.requestedTier != "" {
							request["service_tier"] = scenario.requestedTier
						}
						finalModel, attempts := "gpt-5.5", 1
						if retry {
							request["input"] = []any{map[string]any{"type": "compaction_trigger"}}
							finalModel, attempts = "gpt-5.4", 2
						}
						body, err := json.Marshal(request)
						require.NoError(t, err)
						c, _ := gin.CreateTestContext(httptest.NewRecorder())
						c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
						SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
						if retry {
							MarkOpenAINativeCompactionV2(c)
						}
						response := fmt.Sprintf(`{"id":"first-match-result","model":%q,"status":"completed","output":[],"usage":{"input_tokens":1000,"output_tokens":1000}}`, finalModel)
						contentType := "application/json"
						if stream {
							response = "data: {\"type\":\"response.completed\",\"response\":" + response + "}\n\ndata: [DONE]\n\n"
							contentType = "text/event-stream"
						}
						upstream := &httpUpstreamRecorder{}
						if retry {
							upstream.responses = append(upstream.responses, &http.Response{StatusCode: http.StatusBadRequest, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"code":"context_length_exceeded","message":"context window exceeded"}}`))})
						}
						upstream.responses = append(upstream.responses, &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{contentType}}, Body: io.NopCloser(strings.NewReader(response))})
						svc := newOpenAIGatewayServiceWithSettings(t, &OpenAIFastPolicySettings{Rules: scenario.rules})
						svc.cfg = &config.Config{Gateway: config.GatewayConfig{OpenAICompactModel: "gpt-5.4"}}
						svc.httpUpstream = upstream
						account := rawGPT56ResponsesOAuthAccount("gpt-5.5", "gpt-5.5")
						if passthrough {
							account = rawGPT56ResponsesAPIKeyAccount("gpt-5.5", "gpt-5.5")
							account.Extra["openai_passthrough"] = true
						}
						result, err := svc.Forward(context.Background(), c, account, body)
						require.NoError(t, err, "a transformed tier must not enter the rule engine again")
						require.NotNil(t, result)
						require.Len(t, upstream.bodies, attempts)
						for _, sent := range upstream.bodies {
							require.Equal(t, scenario.sentTier, gjson.GetBytes(sent, "service_tier").String())
							if scenario.sentTier == "" {
								require.False(t, gjson.GetBytes(sent, "service_tier").Exists())
							}
						}
						require.Equal(t, finalModel, gjson.GetBytes(upstream.lastBody, "model").String())
						require.Equal(t, scenario.sentTier, optionalStringValue(result.ServiceTier))
						for _, resolver := range []bool{false, true} {
							usage := &openAIRecordUsageLogRepoStub{inserted: true}
							billing := &openAIRecordUsageBillingRepoStub{result: &UsageBillingApplyResult{Applied: true}}
							settlement := newOpenAIRecordUsageServiceWithBillingRepoForTest(usage, billing, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
							settlement.cfg.Default.RateMultiplier = 1
							if resolver {
								settlement.resolver = NewModelPricingResolver(nil, settlement.billingService)
							}
							input := &OpenAIRecordUsageInput{Result: result, Account: account, User: &User{ID: 20}, APIKey: &APIKey{ID: 10, Group: &Group{ID: 1}}, OriginalModel: "gpt-5.5", BillingModelSource: BillingModelSourceRequested}
							require.NoError(t, settlement.RecordUsage(context.Background(), input))
							require.Equal(t, 1, billing.calls)
							receipt := receiptFixture(t, settlement).receipt
							require.Equal(t, PricingStatePriced, receipt.Usage.PricingState)
							require.Equal(t, scenario.sentTier, optionalStringValue(receipt.Usage.ServiceTier))
							// Existing GPT-5.5 card: $5/$30 per MTok; priority is 2.5x.
							require.InDelta(t, scenario.cost, receipt.Usage.ActualCost, 1e-12)
							require.InDelta(t, scenario.cost, usage.lastLog.ActualCost, 1e-12)
						}
					})
				}
			}
		}
	}
}
