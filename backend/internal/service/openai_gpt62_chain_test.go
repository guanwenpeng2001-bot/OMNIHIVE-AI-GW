package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/openai_compat"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Exercise Forward's actual HTTP and SSE retry loops, recording every transport
// attempt. A helper-only policy assertion cannot detect a retry bypass.
func TestGPT62ChainCompactRetryBlocksFinalModel(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		for _, stream := range []bool{false, true} {
			for _, httpFailure := range []bool{false, true} {
				t.Run(fmt.Sprintf("passthrough=%t/stream=%t/httpFailure=%t", passthrough, stream, httpFailure), func(t *testing.T) {
					body := []byte(fmt.Sprintf(`{"model":"gpt-5.5","service_tier":"ultrafast","stream":%t,"instructions":"compact-test","input":[{"type":"compaction_trigger"}]}`, stream))
					c := newOpenAICompactFallbackTestContext(t, "/v1/responses")
					c.Request.Body = io.NopCloser(bytes.NewReader(body))
					MarkOpenAINativeCompactionV2(c)
					status, contentType := http.StatusOK, "text/event-stream"
					failure := "data: {\"type\":\"response.failed\",\"response\":{\"status\":\"failed\",\"error\":{\"code\":\"context_length_exceeded\",\"message\":\"context window exceeded\"}}}\n\n"
					if httpFailure {
						status, contentType = http.StatusBadRequest, "application/json"
						failure = `{"error":{"code":"context_length_exceeded","message":"context window exceeded"}}`
					} else if stream {
						failure = "data: {\"type\":\"response.created\",\"response\":{\"model\":\"gpt-5.5\"}}\n\n" + failure
					}
					upstream := &httpUpstreamRecorder{responses: []*http.Response{
						{StatusCode: status, Header: http.Header{"Content-Type": []string{contentType}}, Body: io.NopCloser(strings.NewReader(failure))},
						openAICompatSSECompletedResponse("forbidden-retry", "gpt-6.2-sol"),
					}}
					// A rule matching only the fallback must not rewrite its forbidden
					// tier to priority. The initial legacy model matches fallback=pass.
					svc := newOpenAIGatewayServiceWithSettings(t, &OpenAIFastPolicySettings{Rules: []OpenAIFastPolicyRule{{
						ServiceTier: OpenAIFastTierUltrafast, Action: OpenAIFastPolicyActionForcePriority,
						Scope: BetaPolicyScopeAll, ModelWhitelist: []string{"gpt-6.2-sol"}, FallbackAction: BetaPolicyActionPass,
					}}})
					svc.cfg = &config.Config{Gateway: config.GatewayConfig{OpenAICompactModel: "gpt-6.2-sol"}}
					svc.httpUpstream = upstream
					account := rawGPT56ResponsesOAuthAccount("gpt-5.5", "gpt-5.5")
					if passthrough {
						account = rawGPT56ResponsesAPIKeyAccount("gpt-5.5", "gpt-5.5")
						account.Extra["openai_passthrough"] = true
					}
					result, err := svc.Forward(context.Background(), c, account, body)
					require.Error(t, err)
					require.Nil(t, result)
					require.Len(t, upstream.bodies, 1, "blocked fallback must never reach transport")
					require.Equal(t, "gpt-5.5", gjson.GetBytes(upstream.bodies[0], "model").String())
					require.Equal(t, "ultrafast", gjson.GetBytes(upstream.bodies[0], "service_tier").String())
				})
			}
		}
	}
}

func TestGPT62ChainCompactStandardRetryStillForwards(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		for _, stream := range []bool{false, true} {
			result := forwardCompactObservationRetry(t, passthrough, stream, true, "gpt-5.5", "gpt-6.2-sol", "gpt-6.2-sol", "gpt-6.2-sol")
			require.Equal(t, "gpt-6.2-sol", result.UpstreamModel)
		}
	}
}

func TestGPT62ChainExplicitNativeEffortMatchesReceipt(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, model := range []string{"gpt-6.2-sol", "gpt-6.2-astra"} {
			t.Run(fmt.Sprintf("%s/stream=%t", model, stream), func(t *testing.T) {
				body := []byte(fmt.Sprintf(`{"model":%q,"instructions":"test","input":"hello","reasoning":{"effort":"max"},"stream":%t}`, model, stream))
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
				c.Request.Header.Set("User-Agent", "codex_cli_rs/0.169.0")
				c.Request.Header.Set("originator", "codex_cli_rs")
				SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
				upstream := &httpUpstreamRecorder{resp: openAICompatSSECompletedResponse("gpt62-explicit-effort", model)}
				forwarder := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
				account := rawGPT56ResponsesOAuthAccount(model, model)
				result, err := forwarder.Forward(context.Background(), c, account, body)
				require.NoError(t, err)
				require.Len(t, upstream.bodies, 1)
				require.Equal(t, "max", gjson.GetBytes(upstream.lastBody, "reasoning.effort").String())
				require.Equal(t, "max", optionalStringValue(result.ReasoningEffort))
				usage := &openAIRecordUsageLogRepoStub{inserted: true}
				billing := &openAIRecordUsageBillingRepoStub{result: &UsageBillingApplyResult{Applied: true}}
				settlement := newOpenAIRecordUsageServiceWithBillingRepoForTest(usage, billing, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
				settlement.billingService = NewBillingService(settlement.cfg, newStubPricingServiceFromJSON(t, gpt62VerifiedStandardPriceFixture))
				err = settlement.RecordUsage(context.Background(), &OpenAIRecordUsageInput{Result: result, Account: account, User: &User{ID: 20}, APIKey: &APIKey{ID: 10}})
				require.NoError(t, err)
				require.Equal(t, 1, billing.calls)
				require.Equal(t, "max", optionalStringValue(receiptFixture(t, settlement).receipt.Usage.ReasoningEffort))
			})
		}
	}
}

func TestGPT62ChainRecordUsageUsesActualProviderAndRequest(t *testing.T) {
	for _, resolver := range []bool{false, true} {
		for _, scenario := range []struct {
			name, platform, requested, sent, original, response, source string
			blocked                                                     bool
		}{
			{"requested_card_first", PlatformOpenAI, "gpt-5.5", "gpt-6.2-sol", "gpt-5.5", "", BillingModelSourceRequested, true},
			{"fallback_after_unpriced_card", PlatformOpenAI, "gpt-6.2-sol", "gpt-6.2-sol", "gpt-5.5", "", "", true},
			{"response_card_cannot_rescue", PlatformOpenAI, "gpt-5.5", "gpt-6.2-sol", "gpt-5.5", "gpt-5.4", BillingModelSourceResponse, true},
			{"actual_provider_beats_qualifier", PlatformOpenAI, "gpt-5.5", "other-provider/gpt-6.2-sol", "gpt-5.5", "", "", true},
			{"openrouter_bare_id", PlatformOpenRouter, "gpt-6.2-sol", "gpt-6.2-sol", "", "", "", false},
		} {
			t.Run(fmt.Sprintf("resolver=%t/%s", resolver, scenario.name), func(t *testing.T) {
				usage := &openAIRecordUsageLogRepoStub{inserted: true}
				billing := &openAIRecordUsageBillingRepoStub{result: &UsageBillingApplyResult{Applied: true}}
				users, subs, quota := &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, &openAIRecordUsageAPIKeyQuotaStub{}
				svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(usage, billing, users, subs, nil)
				svc.cfg.Default.RateMultiplier = 1
				priceFixture := gpt62VerifiedStandardPriceFixture
				if scenario.platform == PlatformOpenRouter {
					priceFixture = `{"openrouter/gpt-6.2-sol":{"litellm_provider":"openrouter","mode":"chat","input_cost_per_token":0.000002,"output_cost_per_token":0.00001}}`
				}
				svc.billingService = NewBillingService(svc.cfg, newStubPricingServiceFromJSON(t, priceFixture))
				if resolver {
					svc.resolver = NewModelPricingResolver(nil, svc.billingService)
				}
				tier := "ultrafast"
				input := &OpenAIRecordUsageInput{
					Result:        &OpenAIForwardResult{RequestID: "gpt62-chain-" + scenario.name, Model: scenario.requested, UpstreamModel: scenario.sent, UpstreamResponseModel: scenario.response, ServiceTier: &tier, Usage: OpenAIUsage{InputTokens: 1000, OutputTokens: 1000}, Duration: time.Second},
					OriginalModel: scenario.original, BillingModelSource: scenario.source,
					APIKey: &APIKey{ID: 10, Quota: 100, Group: &Group{ID: 1, Platform: PlatformComposite, RateMultiplier: 1}}, User: &User{ID: 20},
					Account: &Account{ID: 30, Platform: scenario.platform, Type: AccountTypeAPIKey}, APIKeyService: quota,
				}
				if scenario.blocked {
					input.Result.UpstreamResponseServiceTier = "flex"
				}
				err := svc.RecordUsage(context.Background(), input)
				receipt := receiptFixture(t, svc).receipt
				if scenario.blocked {
					require.ErrorIs(t, err, ErrUsageSettlementNeedsReview)
					require.Equal(t, UsageSettlementNeedsReview, receipt.State)
					require.Equal(t, PricingStateUnpriced, receipt.Usage.PricingState)
					require.Equal(t, scenario.sent, *receipt.Usage.UpstreamModel)
					require.Zero(t, receipt.Usage.ActualCost)
					require.Zero(t, billing.calls)
					require.Zero(t, usage.calls)
					require.Zero(t, users.deductCalls)
					require.Zero(t, subs.incrementCalls)
					require.Zero(t, quota.quotaCalls)
					require.Equal(t, "ultrafast", optionalStringValue(receipt.Usage.ServiceTier), "retain actual outbound tier for review/replay")
					require.ErrorIs(t, svc.RecordUsage(context.Background(), input), ErrUsageSettlementNeedsReview)
					require.Zero(t, billing.calls, "repeated settlement cannot turn a blocked request into a priced receipt")
				} else {
					require.NoError(t, err)
					require.Equal(t, 1, billing.calls)
					require.NotNil(t, usage.lastLog)
					require.Equal(t, PricingStatePriced, receipt.Usage.PricingState)
					require.InDelta(t, 0.024, usage.lastLog.ActualCost, 1e-12, "retain this provider's pre-existing 2x tariff")
				}
			})
		}
	}
}

func TestGPT62ChainAnthropicUsesFinalMappingAndProvider(t *testing.T) {
	for _, rawChat := range []bool{false, true} {
		for _, scenario := range []struct {
			name, platform, requested, mapped, effort string
			blocked, sampling                         bool
		}{
			{"mapped_none", PlatformOpenAI, "public-model", "gpt-6.2-sol", "none", true, false},
			{"mapped_minimal", PlatformOpenAI, "public-model", "gpt-6.2-sol", "minimal", true, false},
			{"mapped_sampling", PlatformOpenAI, "public-model", "gpt-6.2-sol", "max", false, false},
			{"mapping_away", PlatformOpenAI, "gpt-6.2-sol", "custom-model", "none", false, true},
			{"other_provider_qualified", PlatformGrok, "other-provider/gpt-6.2-sol", "other-provider/gpt-6.2-sol", "none", false, true},
			{"other_provider_bare", PlatformGrok, "gpt-6.2-sol", "gpt-6.2-sol", "none", false, true},
		} {
			t.Run(fmt.Sprintf("chat=%t/%s", rawChat, scenario.name), func(t *testing.T) {
				body := []byte(fmt.Sprintf(`{"model":%q,"max_tokens":16,"messages":[{"role":"user","content":"hello"}],"temperature":0.3,"top_p":0.8,"output_config":{"effort":%q},"stream":false}`, scenario.requested, scenario.effort))
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
				upstream := &httpUpstreamRecorder{resp: openAICompatSSECompletedResponse("gpt62-mapping", scenario.mapped)}
				svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
				account := rawGPT56ResponsesAPIKeyAccount(scenario.requested, scenario.mapped)
				account.Platform = scenario.platform
				effortField := "reasoning.effort"
				if rawChat {
					account.Extra = map[string]any{openai_compat.ExtraKeyResponsesMode: string(openai_compat.ResponsesSupportModeForceChatCompletions)}
					upstream.resp = &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{"id":"chat-response","object":"chat.completion","model":%q,"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}}`, scenario.mapped)))}
					effortField = "reasoning_effort"
				}
				result, err := svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "")
				if scenario.blocked {
					require.Error(t, err)
					require.Nil(t, result)
					require.Empty(t, upstream.bodies)
					return
				}
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, scenario.mapped, gjson.GetBytes(upstream.lastBody, "model").String())
				require.Equal(t, scenario.sampling, gjson.GetBytes(upstream.lastBody, "temperature").Exists())
				require.Equal(t, scenario.sampling, gjson.GetBytes(upstream.lastBody, "top_p").Exists())
				require.Equal(t, scenario.effort, gjson.GetBytes(upstream.lastBody, effortField).String())
			})
		}
	}
}

func TestGPT62ChainNativeCodexEffortAliasIsNotSilentlyNormalized(t *testing.T) {
	for _, model := range []string{"gpt-6.2-sol-max", "gpt-6.2-astra-max"} {
		body := []byte(fmt.Sprintf(`{"model":%q,"instructions":"test","input":"hello","stream":false}`, model))
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
		SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
		c.Request.Header.Set("User-Agent", "codex_cli_rs/0.169.0")
		c.Request.Header.Set("originator", "codex_cli_rs")
		upstream := &httpUpstreamRecorder{resp: openAICompatSSECompletedResponse("gpt62-alias", "gpt-6.2-sol")}
		svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
		result, err := svc.Forward(context.Background(), c, rawGPT56ResponsesOAuthAccount(model, model), body)
		require.Error(t, err, "unsupported aliases must not yield a successful max receipt")
		require.Nil(t, result)
		require.Empty(t, upstream.bodies)
	}
}
