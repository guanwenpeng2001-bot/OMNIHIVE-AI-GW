//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/repository"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func compatibleReviewProvider(protocol, auth string) domain.CompatibleProvider {
	target := map[string]string{domain.APIProtocolChatCompletions: "chat", domain.APIProtocolAnthropic: "messages", domain.APIProtocolResponses: "responses"}[protocol]
	bridges := []string{}
	for _, source := range []string{"chat", "messages", "responses", "gemini"} {
		if source != target {
			bridges = append(bridges, source+"_to_"+target)
		}
	}
	return domain.CompatibleProvider{ID: "fixture-review", Label: "Fixture", LabelZh: "Fixture", BaseSource: "preset", DefaultMode: "payg", EvidenceURL: "https://example.com", EvidenceObserved: "2026-10-03", Modes: []domain.CompatibleMode{{Mode: "payg", DefaultProtocol: protocol, Endpoints: []domain.CompatibleEndpoint{{Protocol: protocol, BaseURL: "https://example.com/v1", Auth: auth}}, Bridges: bridges, Discovery: "none", CapabilityProfile: "fixture-review"}}}
}
func compatibleReviewConfig() *config.Config {
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Default.RateMultiplier = 1
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	cfg.Security.URLAllowlist.AllowPrivateHosts = true
	cfg.Security.URLAllowlist.UpstreamHosts = []string{"127.0.0.1"}
	return cfg
}
func compatibleReviewAccount(base string) service.Account {
	return preparedGatewayFixtureAccount(service.Account{ID: 7, Platform: "fixture-review", Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Concurrency: 1, Credentials: map[string]any{"api_key": "fixture-secret", "base_url": base, "model_mapping": map[string]any{"fixture-model": "fixture-model"}}, AccountGroups: []service.AccountGroup{{AccountID: 7, GroupID: 3}}})
}
func compatibleReviewHandler(t *testing.T, base string) (*OpenAIGatewayHandler, *service.Group, *hunyuanSTTReceiptRepository, *cursorRouteUsageRepo) {
	t.Helper()
	return reviewHandlerForAccount(t, compatibleReviewAccount(base), compatibleReviewConfig())
}
func reviewHandlerForAccount(t *testing.T, account service.Account, cfg *config.Config) (*OpenAIGatewayHandler, *service.Group, *hunyuanSTTReceiptRepository, *cursorRouteUsageRepo) {
	t.Helper()
	accounts := &geminiReviewAccountRepo{openAIWSUsageHandlerAccountRepoStub: openAIWSUsageHandlerAccountRepoStub{account: account}}
	receipts := newHunyuanSTTReceiptRepository()
	usage := &cursorRouteUsageRepo{}
	billingService := service.NewBillingService(cfg, nil)
	gateway := service.ProvideOpenAIGatewayService(accounts, usage, nil, nil, nil, nil, nil, cfg, nil, nil, billingService, nil, nil, repository.NewHTTPUpstream(cfg), nil, nil, nil, service.NewModelPricingResolver(nil, billingService), nil, nil, nil, nil, service.NewUsageSettlementService(receipts, nil, nil))
	billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, &config.Config{RunMode: config.RunModeSimple}, nil)
	t.Cleanup(billing.Stop)
	handler := NewOpenAIGatewayHandler(gateway, service.NewConcurrencyService(&fakeConcurrencyCache{}), billing, service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, cfg)
	inputPrice, outputPrice := 0.5, 1.0
	group := &service.Group{ID: 3, Platform: account.Platform, Status: service.StatusActive, Hydrated: true, RateMultiplier: 1, ModelPricing: []service.ChannelModelPricing{{Models: []string{"fixture-model"}, BillingMode: service.BillingModeToken, InputPrice: &inputPrice, OutputPrice: &outputPrice}}}
	return handler, group, receipts, usage
}
func compatibleReviewReply(protocol, presence string, stream bool) string {
	usage := map[string]any{}
	if presence != "missing" {
		if protocol == domain.APIProtocolChatCompletions {
			usage["prompt_tokens"] = 0
		} else {
			usage["input_tokens"] = 0
		}
	}
	if presence == "zero" || presence == "positive" {
		if protocol == domain.APIProtocolChatCompletions {
			usage["completion_tokens"] = 0
		} else {
			usage["output_tokens"] = 0
		}
	}
	if presence == "partial" {
		if protocol == domain.APIProtocolChatCompletions {
			usage["prompt_tokens"] = 7
		} else {
			usage["input_tokens"] = 7
		}
	}
	if presence == "output_only" {
		usage = map[string]any{}
		if protocol == domain.APIProtocolChatCompletions {
			usage["completion_tokens"] = 2
		} else {
			usage["output_tokens"] = 2
		}
	}
	if presence == "positive" {
		if protocol == domain.APIProtocolChatCompletions {
			usage["prompt_tokens"] = 7
			usage["completion_tokens"] = 2
		} else {
			usage["input_tokens"] = 7
			usage["output_tokens"] = 2
		}
	}
	var response map[string]any
	switch protocol {
	case domain.APIProtocolChatCompletions:
		response = map[string]any{"id": "chat-1", "object": "chat.completion", "model": "fixture-model", "choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": "delivered"}, "finish_reason": "stop"}}}
	case domain.APIProtocolAnthropic:
		response = map[string]any{"id": "msg-1", "type": "message", "role": "assistant", "model": "fixture-model", "content": []any{map[string]any{"type": "text", "text": "delivered"}}, "stop_reason": "end_turn"}
	default:
		response = map[string]any{"id": "resp-1", "object": "response", "status": "completed", "model": "fixture-model", "output": []any{map[string]any{"id": "m1", "type": "message", "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": "delivered", "annotations": []any{}}}}}}
	}
	if presence != "missing" {
		response["usage"] = usage
	}
	raw, _ := json.Marshal(response)
	if !stream {
		return string(raw)
	}
	event := func(kind string, value any) string {
		payload, _ := json.Marshal(value)
		return "event: " + kind + "\ndata: " + string(payload) + "\n\n"
	}
	switch protocol {
	case domain.APIProtocolChatCompletions:
		chunk := map[string]any{"id": "c1", "object": "chat.completion.chunk", "model": "fixture-model", "choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": "delivered"}, "finish_reason": "stop"}}}
		if presence != "missing" {
			chunk["usage"] = usage
		}
		payload, _ := json.Marshal(chunk)
		return "data: " + string(payload) + "\n\ndata: [DONE]\n\n"
	case domain.APIProtocolAnthropic:
		message := map[string]any{"id": "m1", "type": "message", "role": "assistant", "model": "fixture-model", "content": []any{}}
		delta := map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn"}}
		if presence != "missing" {
			message["usage"] = map[string]any{"input_tokens": usage["input_tokens"]}
		}
		if output, ok := usage["output_tokens"]; ok {
			delta["usage"] = map[string]any{"output_tokens": output}
		}
		return event("message_start", map[string]any{"type": "message_start", "message": message}) + event("content_block_start", map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "text", "text": ""}}) + event("content_block_delta", map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "text_delta", "text": "delivered"}}) + event("content_block_stop", map[string]any{"type": "content_block_stop", "index": 0}) + event("message_delta", delta) + event("message_stop", map[string]any{"type": "message_stop"})
	default:
		return event("response.output_text.delta", map[string]any{"type": "response.output_text.delta", "delta": "delivered", "output_index": 0, "content_index": 0}) + event("response.completed", map[string]any{"type": "response.completed", "response": response})
	}
}
func TestCompatibleReviewHandlerUsageReceipts(t *testing.T) {
	for _, protocol := range []string{domain.APIProtocolChatCompletions, domain.APIProtocolAnthropic, domain.APIProtocolResponses} {
		t.Run(protocol, func(t *testing.T) {
			restore := domain.SetCompatibleProvidersForTest([]domain.CompatibleProvider{compatibleReviewProvider(protocol, "bearer")})
			defer restore()
			for _, path := range []string{"/v1/chat/completions", "/v1/messages", "/v1/responses"} {
				for _, stream := range []bool{false, true} {
					for _, presence := range []string{"missing", "partial", "output_only", "zero", "positive"} {
						t.Run(fmt.Sprintf("%s/%t/%s", path, stream, presence), func(t *testing.T) {
							var calls atomic.Int64
							upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
								calls.Add(1)
								var request map[string]any
								require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
								upstreamStream, _ := request["stream"].(bool)
								if upstreamStream {
									w.Header().Set("Content-Type", "text/event-stream")
								} else {
									w.Header().Set("Content-Type", "application/json")
								}
								fmt.Fprint(w, compatibleReviewReply(protocol, presence, upstreamStream))
							}))
							defer upstream.Close()
							handler, group, receipts, usage := compatibleReviewHandler(t, upstream.URL+"/v1")
							body := fmt.Sprintf(`{"model":"fixture-model","stream":%t,"input":"hi","messages":[{"role":"user","content":"hi"}],"max_tokens":32}`, stream)
							client, rec := requestPlanHTTPContext(path, body, group)
							switch path {
							case "/v1/messages":
								handler.Messages(client)
							case "/v1/chat/completions":
								handler.ChatCompletions(client)
							default:
								handler.Responses(client)
							}
							require.Equal(t, 200, rec.Code, rec.Body.String())
							require.Contains(t, rec.Body.String(), "delivered")
							require.EqualValues(t, 1, calls.Load(), "optional counters must never replay generation")
							incompleteBridge := stream && protocol == domain.APIProtocolChatCompletions && path != "/v1/chat/completions" && presence != "positive" && presence != "zero"
							if incompleteBridge {
								require.Contains(t, rec.Body.String(), map[string]string{"/v1/messages": "event: error", "/v1/responses": "event: response.failed"}[path])
								require.NotContains(t, rec.Body.String(), map[string]string{"/v1/messages": "event: message_stop", "/v1/responses": "event: response.completed"}[path])
							} else if stream {
								require.Contains(t, rec.Body.String(), map[string]string{"/v1/chat/completions": "[DONE]", "/v1/messages": "message_stop", "/v1/responses": "response.completed"}[path])
							}
							require.Len(t, receipts.receipts, 1)
							require.Empty(t, usage.logs, "receipt owns settlement, not a zero-valued direct log")
							for _, receipt := range receipts.receipts {
								if incompleteBridge {
									require.Equal(t, "interrupted", receipt.CompletionState)
								} else {
									require.Equal(t, "complete", receipt.CompletionState)
								}
								if presence == "missing" || presence == "partial" || presence == "output_only" {
									require.Equal(t, service.UsageSettlementNeedsReview, receipt.State)
									require.Equal(t, service.PricingStateUnpriced, receipt.Usage.PricingState)
									require.Zero(t, receipts.settles)
									require.Zero(t, receipts.debit)
									require.Empty(t, receipts.applied)
									require.Equal(t, "upstream_usage_unknown", gjson.Get(receipt.Usage.PricingSnapshot, "source").String())
									require.Equal(t, presence == "output_only", gjson.Get(receipt.Usage.PricingSnapshot, "token_usage_observation.output_tokens_known").Bool())
									require.Equal(t, presence == "partial", gjson.Get(receipt.Usage.PricingSnapshot, "token_usage_observation.input_tokens_known").Bool())
									if presence == "partial" {
										require.Equal(t, 7, receipt.Usage.InputTokens)
									}
									if presence == "output_only" {
										require.Equal(t, 2, receipt.Usage.OutputTokens)
									}
								} else {
									require.Equal(t, service.UsageSettlementSettled, receipt.State)
									require.Equal(t, 1, receipts.settles)
									if presence == "zero" {
										require.Zero(t, receipts.debit)
									} else {
										require.Greater(t, receipts.debit, 0.0)
									}
								}
							}
							plan := service.GatewayRequestPlanFromContext(client.Request.Context())
							require.NotNil(t, plan)
							require.Len(t, plan.Attempts(), 1)
						})
					}
				}
			}
		})
	}
}
func TestCompatibleReviewProbeRejectsRedirect(t *testing.T) {
	for _, protocol := range []string{domain.APIProtocolChatCompletions, domain.APIProtocolAnthropic, domain.APIProtocolResponses} {
		for _, auth := range []string{"bearer", "x_api_key", "api_key_header"} {
			t.Run(protocol+"/"+auth, func(t *testing.T) {
				restore := domain.SetCompatibleProvidersForTest([]domain.CompatibleProvider{compatibleReviewProvider(protocol, auth)})
				defer restore()
				var first, second atomic.Int64
				target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					second.Add(1)
					t.Errorf("redirected request reached target with headers %v", r.Header)
				}))
				defer target.Close()
				redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					first.Add(1)
					require.Equal(t, map[string]string{"bearer": "Bearer fixture-secret", "x_api_key": "fixture-secret", "api_key_header": "fixture-secret"}[auth], r.Header.Get(map[string]string{"bearer": "Authorization", "x_api_key": "x-api-key", "api_key_header": "api-key"}[auth]))
					http.Redirect(w, r, strings.Replace(target.URL, "127.0.0.1", "localhost", 1), http.StatusTemporaryRedirect)
				}))
				defer redirector.Close()
				cfg := compatibleReviewConfig()
				accounts := &openAIWSUsageHandlerAccountRepoStub{account: compatibleReviewAccount(redirector.URL + "/v1")}
				probe := service.NewAccountTestService(accounts, nil, nil, nil, nil, repository.NewHTTPUpstream(cfg), cfg, nil)
				result := probe.ProbeAccount(context.Background(), service.AccountProbeInput{AccountID: 7, ModelID: "fixture-model"}, func(service.TestEvent) {})
				require.False(t, result.Success)
				require.Contains(t, result.Error, "307")
				require.EqualValues(t, 1, first.Load())
				require.Zero(t, second.Load())
			})
		}
	}
}

func TestCompatibleReviewHandlerResponseOwnership(t *testing.T) {
	restore := domain.SetCompatibleProvidersForTest([]domain.CompatibleProvider{compatibleReviewProvider(domain.APIProtocolResponses, "bearer")})
	defer restore()
	for _, owner := range []string{"missing", "other-user", "same-user-other-key"} {
		t.Run(owner, func(t *testing.T) {
			var calls atomic.Int64
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var body map[string]any
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				require.Equal(t, "resp_previous", body["previous_response_id"])
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, compatibleReviewReply(domain.APIProtocolResponses, "positive", false))
			}))
			defer upstream.Close()
			handler, group, receipts, _ := compatibleReviewHandler(t, upstream.URL+"/v1")
			if owner != "missing" {
				userID := int64(99)
				if owner == "same-user-other-key" {
					userID = 1
				}
				require.NoError(t, handler.gatewayService.BindOpenAIHTTPResponseOwner(context.Background(), group.ID, "resp_previous", userID, 999))
			}
			client, rec := requestPlanHTTPContext("/v1/responses", `{"model":"fixture-model","input":"hi","previous_response_id":"resp_previous"}`, group)
			handler.Responses(client)
			if owner == "same-user-other-key" {
				require.Equal(t, 200, rec.Code, rec.Body.String())
				require.EqualValues(t, 1, calls.Load())
				require.Len(t, receipts.receipts, 1)
			} else {
				require.Equal(t, 400, rec.Code, rec.Body.String())
				require.Contains(t, rec.Body.String(), "previous_response_id is not available for this user")
				require.Zero(t, calls.Load())
				require.Empty(t, receipts.receipts)
			}
		})
	}
}
func TestCompatibleReviewMalformedGenerationStillFails(t *testing.T) {
	restore := domain.SetCompatibleProvidersForTest([]domain.CompatibleProvider{compatibleReviewProvider(domain.APIProtocolChatCompletions, "bearer")})
	defer restore()
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			var calls atomic.Int64
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if stream {
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n")
				} else {
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprint(w, `{"id":`)
				}
			}))
			defer upstream.Close()
			handler, group, receipts, _ := compatibleReviewHandler(t, upstream.URL+"/v1")
			client, rec := requestPlanHTTPContext("/v1/chat/completions", fmt.Sprintf(`{"model":"fixture-model","stream":%t,"messages":[{"role":"user","content":"hi"}]}`, stream), group)
			handler.ChatCompletions(client)
			require.EqualValues(t, 1, calls.Load())
			require.NotContains(t, rec.Body.String(), "[DONE]")
			require.Zero(t, receipts.settles)
			if !stream {
				require.Equal(t, 502, rec.Code)
				require.Empty(t, receipts.receipts)
			} else {
				require.Len(t, receipts.receipts, 1)
				for _, receipt := range receipts.receipts {
					require.Equal(t, "interrupted", receipt.CompletionState)
					require.Equal(t, service.UsageSettlementNeedsReview, receipt.State)
				}
			}
		})
	}
}

func TestCompatibleReviewFinishReasonIsTerminalWithoutDone(t *testing.T) {
	restore := domain.SetCompatibleProvidersForTest([]domain.CompatibleProvider{compatibleReviewProvider(domain.APIProtocolChatCompletions, "bearer")})
	defer restore()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, strings.TrimSuffix(compatibleReviewReply(domain.APIProtocolChatCompletions, "missing", true), "data: [DONE]\n\n"))
	}))
	defer upstream.Close()
	handler, group, receipts, _ := compatibleReviewHandler(t, upstream.URL+"/v1")
	client, rec := requestPlanHTTPContext("/v1/chat/completions", `{"model":"fixture-model","stream":true,"messages":[{"role":"user","content":"hi"}]}`, group)
	handler.ChatCompletions(client)
	require.Equal(t, 200, rec.Code)
	require.Contains(t, rec.Body.String(), "delivered")
	require.Contains(t, rec.Body.String(), `"finish_reason":"stop"`)
	require.NotContains(t, rec.Body.String(), `"error"`)
	require.Len(t, receipts.receipts, 1)
	for _, receipt := range receipts.receipts {
		require.Equal(t, "complete", receipt.CompletionState)
		require.Equal(t, service.UsageSettlementNeedsReview, receipt.State)
	}
	require.Zero(t, receipts.settles)
}

func TestNativeChatEmptyBodyReview(t *testing.T) {
	for _, tt := range []struct {
		name, body string
		state      service.UsageSettlementState
	}{
		{"empty", "", service.UsageSettlementNeedsReview},
		{"partial", `{"usage":{"prompt_tokens":0}}`, service.UsageSettlementNeedsReview},
		{"wrapped_zero", `{"data":{"usage":{"prompt_tokens":0,"completion_tokens":0}}}`, service.UsageSettlementSettled},
	} {
		t.Run(tt.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/chat/completions" {
					t.Errorf("unexpected upstream path %s", r.URL.Path)
				}
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer upstream.Close()
			account := compatibleReviewAccount(upstream.URL + "/v1")
			account.Platform = service.PlatformOpenAI
			account.Extra["openai_responses_supported"] = false
			handler, group, receipts, _ := reviewHandlerForAccount(t, account, compatibleReviewConfig())
			client, _ := requestPlanHTTPContext("/v1/chat/completions", `{"model":"fixture-model","messages":[{"role":"user","content":"hi"}]}`, group)
			handler.ChatCompletions(client)
			require.Len(t, receipts.receipts, 1)
			for _, receipt := range receipts.receipts {
				require.Equal(t, tt.state, receipt.State)
			}
			if tt.state == service.UsageSettlementNeedsReview {
				require.Zero(t, receipts.settles)
			} else {
				require.Equal(t, 1, receipts.settles)
			}
			require.Zero(t, receipts.debit)
		})
	}
}
