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

	"github.com/Wei-Shaw/sub2api/internal/pkg/cursor"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestGrokMessagesBudgetObeysGroupPolicy(t *testing.T) {
	for _, model := range []string{"grok-4.6", "grok-4.7", "private-vendor/unknown-exact-id"} {
		for _, chat := range []bool{false, true} {
			for _, policy := range []string{"cap", "minimal", "deny", "mapping", "unbound"} {
				t.Run(fmt.Sprintf("%s/chat=%t/%s", model, chat, policy), func(t *testing.T) {
					body := []byte(`{"model":"grok-4.6","max_tokens":64000,"thinking":{"type":"enabled","budget_tokens":32768},"messages":[{"role":"user","content":"hi"}]}`)
					account := rawChatCompletionsTestAccount()
					upstream := &httpUpstreamRecorder{resp: openAICompatSSECompletedResponse("budget", model)}
					field := "reasoning.effort"
					if chat {
						account = forceChatMessagesFallbackAccount()
						field = "reasoning_effort"
						upstream.resp = &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"chat","model":"` + model + `","choices":[{"index":0,"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))}
					}
					body = []byte(strings.ReplaceAll(string(body), "grok-4.6", model))
					account.Platform = PlatformGrok
					ctx := context.Background()
					action := ReasoningEffortOverLimitDowngrade
					var mappings []ReasoningEffortMapping
					if policy == "deny" {
						action = ReasoningEffortOverLimitDeny
					}
					if policy == "mapping" {
						mappings = []ReasoningEffortMapping{{From: "high", To: ReasoningEffortMappingDeny}}
					}
					if policy != "unbound" {
						ceiling := "medium"
						if policy == "minimal" {
							ceiling = "minimal"
						}
						ctx = WithOpenAIReasoningEffortPolicy(ctx, ceiling, mappings, action)
					}
					c := adaptiveProtocolTestContext("/v1/messages", body)
					c.Request = c.Request.WithContext(ctx)
					gateway := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
					result, err := gateway.ForwardAsAnthropic(ctx, c, account, body, "", "")
					if policy == "deny" || policy == "mapping" {
						require.True(t, IsReasoningEffortPolicyDenied(err))
						require.Nil(t, upstream.lastReq)
						return
					}
					require.NoError(t, err)
					want := "medium"
					if policy == "minimal" {
						want = "minimal"
					}
					if policy == "unbound" {
						want = "high"
					}
					require.Equal(t, want, gjson.GetBytes(upstream.lastBody, field).String())
					require.Equal(t, model, gjson.GetBytes(upstream.lastBody, "model").String())
					if chat {
						require.Equal(t, "upstream.example", upstream.lastReq.URL.Host)
						require.Equal(t, "/v1/chat/completions", upstream.lastReq.URL.Path)
						require.Equal(t, "Bearer sk-test", upstream.lastReq.Header.Get("Authorization"))
					}
					require.Equal(t, want, *result.ReasoningEffort)
					require.Nil(t, result.RequestedReasoningEffort)
				})
			}
		}
	}
}

func TestCursorDisabledKnownAndUnknownControlBeforeRun(t *testing.T) {
	for _, tc := range []struct {
		model, slug string
		slugs       []string
	}{
		{"grok-4.7", "", []string{"cursor-grok-4.7-low", "cursor-grok-4.7-high"}},
		{"composer-2.5", "composer-2.5-fast", []string{"composer-2.5-fast"}},
		{"off-model", "off-model-none", []string{"off-model-none", "off-model-low"}},
	} {
		for _, protocol := range []string{APIProtocolAnthropic, APIProtocolResponses, APIProtocolChatCompletions} {
			t.Run(tc.model+"/"+protocol, func(t *testing.T) {
				gateway, account, _ := cursorTestGateway()
				calls := 0
				gateway.native = &cursorCatalogDriver{available: []cursor.AvailableModel{{ID: tc.model, LegacySlugs: tc.slugs}}, cursorDriverStub: cursorDriverStub{run: func(_ context.Context, _ cursor.Credentials, _ []cursor.ChatMessage, slug string, emit func(cursor.StreamEvent) error) (cursor.TokenUsage, error) {
					calls++
					require.Equal(t, tc.slug, slug)
					require.NoError(t, emit(cursor.StreamEvent{Type: "text", Text: "ok"}))
					return cursor.TokenUsage{InputTokens: 1, OutputTokens: 1}, nil
				}}}
				body := `{"model":"` + tc.model + `","messages":[{"role":"user","content":"hi"}],"reasoning_effort":"none"}`
				if protocol == APIProtocolAnthropic {
					body = `{"model":"` + tc.model + `","max_tokens":16,"messages":[{"role":"user","content":"hi"}],"thinking":{"type":"disabled"}}`
				}
				if protocol == APIProtocolResponses {
					body = `{"model":"` + tc.model + `","input":"hi","reasoning":{"effort":"none"}}`
				}
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				result, err := callCursorProtocol(gateway, c, account, []byte(body), protocol)
				if tc.slug == "" {
					var fail *UpstreamFailoverError
					require.ErrorAs(t, err, &fail)
					require.Equal(t, RequestCompatibilityRejectedReason, fail.Reason)
					require.True(t, fail.ShouldRetryNextAccount())
					require.Zero(t, calls)
					require.False(t, c.Writer.Written())
					return
				}
				require.NoError(t, err)
				require.Equal(t, 1, calls)
				if tc.model == "composer-2.5" {
					require.Nil(t, result.ReasoningEffort)
				} else {
					require.Equal(t, "none", *result.ReasoningEffort)
				}
			})
		}
	}
}

func TestExplicitNoneRequestedEffortPersistsWithoutCeilingExpansion(t *testing.T) {
	for _, platform := range []string{PlatformOpenAI, PlatformCommandCode} {
		for _, body := range []string{`{"reasoning_effort":"none"}`, `{"reasoning":{"effort":" NoNe "}}`, `{"output_config":{"effort":"none"}}`} {
			requested := CanonicalRequestedReasoningEffort([]byte(body))
			require.NotNil(t, requested)
			require.Equal(t, "none", *requested)
			outbound := []byte(`{"reasoning_effort":"none"}`)
			if platform == PlatformCommandCode {
				var err error
				outbound, err = normalizeCommandCodeReasoningWithPolicy(nil, outbound, "unknown-model")
				require.NoError(t, err)
			}
			effective := extractOpenAIReasoningEffortFromBody(outbound)
			require.NotNil(t, effective)
			require.Equal(t, "none", *effective)
			repo := &openAIRecordUsageLogRepoStub{inserted: true}
			svc := newOpenAIRecordUsageServiceForTest(repo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
			err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{Result: &OpenAIForwardResult{RequestID: "none-record", Model: "gpt-5.4", RequestedReasoningEffort: requested, ReasoningEffort: effective, Usage: OpenAIUsage{InputTokens: 1, OutputTokens: 1}}, Account: &Account{ID: 30, Platform: platform}, APIKey: &APIKey{ID: 10}, User: &User{ID: 20}})
			require.NoError(t, err)
			require.Equal(t, "none", *repo.lastLog.RequestedReasoningEffort)
			require.Equal(t, "none", *repo.lastLog.ReasoningEffort)
		}
	}
	require.Empty(t, NormalizeMaxReasoningEffort("none"))
	require.Nil(t, CanonicalRequestedReasoningEffort([]byte(`{"thinking":{"type":"enabled","budget_tokens":32768}}`)))
	require.Nil(t, CanonicalRequestedReasoningEffort([]byte(`{"thinking":{"type":"disabled"}}`)))
}

func TestGrokNormalizersPreserveUnlistedModelEffort(t *testing.T) {
	for _, model := range []string{"grok-future-unlisted", "private-vendor/unknown-exact-id"} {
		for _, effort := range []string{"none", "low", "medium", "high", "xhigh", "future-effort"} {
			t.Run(model+"/"+effort, func(t *testing.T) {
				for _, field := range []string{"reasoning_effort", "reasoningEffort"} {
					body := []byte(`{"model":"` + model + `","input":"hi","` + field + `":"` + effort + `"}`)
					responses, err := patchGrokResponsesBody(body, model)
					require.NoError(t, err)
					require.Equal(t, effort, gjson.GetBytes(responses, "reasoning_effort").String())
					chat, err := normalizeGrokChatReasoningEffort(body, model)
					require.NoError(t, err)
					require.Equal(t, effort, gjson.GetBytes(chat, "reasoning_effort").String())
				}
				body := []byte(`{"model":"` + model + `","input":"hi","reasoning":{"effort":"` + effort + `"}}`)
				filtered, err := filterOpenAIResponsesNoneReasoningEffortForAccount(&Account{Platform: PlatformGrok, Type: AccountTypeAPIKey}, body)
				require.NoError(t, err)
				patched, err := patchGrokResponsesBody(filtered, model)
				require.NoError(t, err)
				require.Equal(t, effort, gjson.GetBytes(patched, "reasoning.effort").String())
				require.Equal(t, model, gjson.GetBytes(patched, "model").String())
			})
		}
	}
}

func TestGrokDirectChatRecordsNormalizedEffort(t *testing.T) {
	for _, tc := range []struct{ model, effort, want string }{{"grok-4.5", "xhigh", "high"}, {"grok-4.7", "xhigh", "xhigh"}, {"private-vendor/unknown-exact-id", "high", "high"}, {"grok-composer-2.5-fast", "high", ""}} {
		t.Run(tc.model, func(t *testing.T) {
			account := rawChatCompletionsTestAccount()
			account.Platform = PlatformGrok
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"chat","choices":[{"index":0,"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))}}
			body := []byte(`{"model":"` + tc.model + `","messages":[{"role":"user","content":"hi"}],"reasoning_effort":"` + tc.effort + `"}`)
			c := adaptiveProtocolTestContext("/v1/chat/completions", body)
			gateway := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
			result, err := gateway.forwardAsRawChatCompletions(context.Background(), c, account, body, "")
			require.NoError(t, err)
			require.Equal(t, tc.want, gjson.GetBytes(upstream.lastBody, "reasoning_effort").String())
			require.Equal(t, tc.want, optionalStringValue(result.ReasoningEffort))
			require.Equal(t, "upstream.example", upstream.lastReq.URL.Host)
			require.Equal(t, "/v1/chat/completions", upstream.lastReq.URL.Path)
		})
	}
}

func TestGrokDirectResponsesPreservesUnlistedEffort(t *testing.T) {
	for _, model := range []string{"grok-future-unlisted", "private-vendor/unknown-exact-id"} {
		for _, effort := range []string{"none", "high", "xhigh", "max", "extra-high", "future-effort", "ultra"} {
			for _, field := range []string{"reasoning.effort", "reasoning_effort"} {
				t.Run(model+"/"+effort+"/"+field, func(t *testing.T) {
					account := rawChatCompletionsTestAccount()
					account.Platform = PlatformGrok
					upstream := &httpUpstreamRecorder{resp: openAICompatSSECompletedResponse("direct-grok", model)}
					gateway := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
					body := []byte(`{"model":"` + model + `","input":"hi","reasoning":{"effort":"` + effort + `"}}`)
					if field == "reasoning_effort" {
						body = []byte(`{"model":"` + model + `","input":"hi","reasoning_effort":"` + effort + `"}`)
					}
					c := adaptiveProtocolTestContext("/v1/responses", body)
					result, err := gateway.Forward(context.Background(), c, account, body)
					require.NoError(t, err)
					require.Equal(t, model, gjson.GetBytes(upstream.lastBody, "model").String())
					outboundEffort := gjson.GetBytes(upstream.lastBody, field).String()
					require.Equal(t, effort, outboundEffort)
					requestedEffort := effort
					switch effort {
					case "extra-high":
						requestedEffort = "xhigh"
					case "future-effort", "ultra":
						requestedEffort = ""
					}
					require.Equal(t, requestedEffort, optionalStringValue(result.RequestedReasoningEffort))
					require.Equal(t, outboundEffort, optionalStringValue(result.ReasoningEffort))
				})
			}
		}
	}
}

func TestGrokDirectResponsesRecordsFinalEffortWithSuffixFallback(t *testing.T) {
	for _, scenario := range []struct {
		name, model, controls, outbound, recorded string
	}{
		{"known alias normalization", "grok-4.5", `,"reasoning":{"effort":"max"}`, "high", "high"},
		{"known composer stripping", "grok-composer-2.5-fast", `,"reasoning":{"effort":"high"}`, "", ""},
		{"omitted effort suffix", "private-vendor/unknown-high", "", "", "high"},
		{"explicit effort overrides suffix", "private-vendor/unknown-high", `,"reasoning":{"effort":"ultra"}`, "ultra", "ultra"},
		{"nested effort takes precedence", "grok-future-unlisted", `,"reasoning":{"effort":"max"},"reasoning_effort":"ultra"`, "max", "max"},
		{"empty nested effort uses flat field", "grok-future-unlisted", `,"reasoning":{"effort":" "},"reasoning_effort":"future-effort"`, "future-effort", "future-effort"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			account := rawChatCompletionsTestAccount()
			account.Platform = PlatformGrok
			upstream := &httpUpstreamRecorder{resp: openAICompatSSECompletedResponse("final-effort", scenario.model)}
			gateway := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
			body := []byte(`{"model":"` + scenario.model + `","input":"hi"` + scenario.controls + `}`)
			requestContext := adaptiveProtocolTestContext("/v1/responses", body)
			result, err := gateway.Forward(context.Background(), requestContext, account, body)
			require.NoError(t, err)
			outboundEffort := strings.TrimSpace(gjson.GetBytes(upstream.lastBody, "reasoning.effort").String())
			if outboundEffort == "" {
				outboundEffort = strings.TrimSpace(gjson.GetBytes(upstream.lastBody, "reasoning_effort").String())
			}
			require.Equal(t, scenario.outbound, outboundEffort)
			require.Equal(t, scenario.recorded, optionalStringValue(result.ReasoningEffort))
			if outboundEffort != "" {
				require.Equal(t, outboundEffort, optionalStringValue(result.ReasoningEffort))
			}
		})
	}
}
