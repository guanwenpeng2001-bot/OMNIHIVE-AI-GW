//go:build unit

package handler

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/tlsfingerprint"
	middleware "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestCompositeForwardContractHandlers(t *testing.T) {
	for _, trunk := range []string{"openai_chat", "openai_responses", "openai_messages", "gateway_chat", "gateway_responses", "gateway_messages", "gateway_gemini_messages"} {
		for _, scenario := range []string{"explicit_conflict", "empty_channel", "equal_request_conflict", "later_explicit", "later_empty_channel", "later_empty_public", "same_platform_later", "non_composite", "account_mapping"} {
			t.Run(trunk+"/"+scenario, func(t *testing.T) {
				cfg := &config.Config{RunMode: config.RunModeSimple}
				cfg.Default.RateMultiplier = 1
				groupID := int64(900)
				group := &service.Group{ID: groupID, Hydrated: true, Platform: service.PlatformComposite, Status: service.StatusActive}
				secondPlatform, secondModel := service.PlatformAnthropic, "claude-sonnet-4-5"
				if strings.HasPrefix(trunk, "openai_") {
					secondPlatform, secondModel = service.PlatformCommandCode, "wire-second"
				}
				if trunk == "gateway_gemini_messages" {
					secondPlatform, secondModel = service.PlatformGemini, "gemini-2.5-flash"
				}
				publicModel := "Exact.Public-Alias"
				inboundModel := publicModel
				selectedModel := secondModel
				if scenario == "empty_channel" || scenario == "later_empty_channel" || scenario == "later_empty_public" {
					selectedModel = ""
				}
				if scenario == "equal_request_conflict" {
					selectedModel = publicModel
				}
				targets := []service.CompositeRouteTarget{{Position: 0, TargetPlatform: secondPlatform, UpstreamModel: selectedModel, Enabled: true}}
				if strings.HasPrefix(scenario, "later_") || scenario == "same_platform_later" {
					inboundModel = "wire-first"
					targets = append([]service.CompositeRouteTarget{{Position: 0, TargetPlatform: service.PlatformOpenAI, UpstreamModel: inboundModel, Enabled: true}}, targets...)
					targets[1].Position = 1
				}
				if scenario == "same_platform_later" {
					targets[0].TargetPlatform = secondPlatform
					targets[0].AccountType = service.AccountTypeOAuth
					targets[1].AccountType = service.AccountTypeAPIKey
				}
				if scenario == "non_composite" {
					group.Platform = secondPlatform
					targets = nil
				}
				channelModel := "channel-wire"
				expectedModel := selectedModel
				if selectedModel == "" || scenario == "non_composite" {
					expectedModel = channelModel
				}
				if trunk == "gateway_gemini_messages" && scenario == "non_composite" {
					expectedModel = publicModel
				}
				if scenario == "later_empty_public" {
					expectedModel = publicModel
				}
				accountMapping := map[string]string{publicModel: publicModel, "wire-first": "wire-first", secondModel: secondModel, channelModel: channelModel}
				if scenario == "account_mapping" {
					accountMapping[secondModel] = "account-wire"
					expectedModel = "account-wire"
				}
				repo := compositeChatAccountRepository{}
				repo.accounts = []service.Account{{ID: 902, Name: "selected", Platform: secondPlatform, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Concurrency: 1, Credentials: map[string]any{"api_key": "fixture", "base_url": "https://upstream.invalid", "model_mapping": accountMapping}, AccountGroups: []service.AccountGroup{{AccountID: 902, GroupID: groupID}}}}
				channelRepo := &openAIWSUsageHandlerChannelRepoStub{channels: []service.Channel{{ID: 905, Name: "contract-channel", Status: service.StatusActive, GroupIDs: []int64{groupID}, ModelMapping: map[string]map[string]string{service.PlatformOpenAI: {inboundModel: channelModel}, secondPlatform: {inboundModel: channelModel}}, BillingModelSource: service.BillingModelSourceChannelMapped}}, groupPlatforms: map[int64]string{groupID: group.Platform}}
				if scenario == "later_empty_public" {
					channelRepo.channels[0].ModelMapping = nil
				}
				channels := service.NewChannelService(channelRepo, nil, nil, nil, nil)
				upstream := &contractForwardUpstream{}
				billingCache := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
				t.Cleanup(billingCache.Stop)
				concurrency := NewConcurrencyHelper(service.NewConcurrencyService(&concurrencyCacheMock{
					acquireUserSlotFn:    func(context.Context, int64, int, string) (bool, error) { return true, nil },
					acquireAccountSlotFn: func(context.Context, int64, int, string) (bool, error) { return true, nil },
				}), SSEPingFormatClaude, time.Second)
				recorder := httptest.NewRecorder()
				client, _ := gin.CreateTestContext(recorder)
				path, body := "/v1/chat/completions", `{"model":"Exact.Public-Alias","messages":[{"role":"user","content":"hello"}],"stream":false}`
				if strings.HasSuffix(trunk, "responses") {
					path, body = "/v1/responses", `{"model":"Exact.Public-Alias","input":"hello","stream":false}`
				}
				if strings.HasSuffix(trunk, "messages") {
					path, body = "/v1/messages", `{"model":"Exact.Public-Alias","max_tokens":16,"messages":[{"role":"user","content":"hello"}],"stream":false}`
				}
				body = strings.ReplaceAll(body, publicModel, inboundModel)
				ctx := context.Background()
				if len(targets) > 0 {
					ctx = service.WithCompositeRouteDecision(ctx, service.CompositeRouteDecision{Matched: true, PublicModel: publicModel, TargetPlatform: targets[0].TargetPlatform, UpstreamModel: targets[0].UpstreamModel, Targets: targets})
					// Messages does not walk ordered targets in the OpenAI trunk; model
					// resolution must also honor a later target already pinned by dispatch.
					if trunk == "openai_messages" || trunk == "gateway_gemini_messages" {
						ctx = service.WithCompositeTargetSelectionContext(ctx, targets[len(targets)-1])
					}
				}
				client.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)).WithContext(ctx)
				client.Request.Header.Set("Content-Type", "application/json")
				client.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 903, UserID: 904, GroupID: &groupID, Group: group, User: &service.User{ID: 904, Status: service.StatusActive}})
				client.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 904, Concurrency: 1})
				receipts := service.NewUsageSettlementService(&unlimitedTPMReceiptStub{}, nil, nil)
				if strings.HasPrefix(trunk, "openai_") {
					gateway := service.ProvideOpenAIGatewayService(repo, nil, nil, nil, nil, nil, nil, cfg, nil, nil, service.NewBillingService(cfg, nil), nil, billingCache, upstream, &service.DeferredService{}, nil, nil, nil, channels, nil, nil, nil, receipts)
					handler := &OpenAIGatewayHandler{gatewayService: gateway, billingCacheService: billingCache, concurrencyHelper: concurrency, maxAccountSwitches: 2, cfg: cfg, apiKeyService: &service.APIKeyService{}}
					switch trunk {
					case "openai_chat":
						handler.ChatCompletions(client)
					case "openai_responses":
						handler.Responses(client)
					default:
						handler.Messages(client)
					}
				} else {
					gateway := service.ProvideGatewayService(repo, &heartbeatGroupRepository{group: group}, nil, nil, nil, nil, nil, nil, cfg, nil, nil, service.NewBillingService(cfg, nil), nil, billingCache, nil, upstream, &service.DeferredService{}, nil, nil, nil, nil, nil, nil, channels, nil, nil, nil, nil, receipts)
					handler := &GatewayHandler{geminiCompatService: service.NewGeminiMessagesCompatService(repo, &heartbeatGroupRepository{group: group}, nil, nil, nil, nil, upstream, nil, cfg), gatewayService: gateway, billingCacheService: billingCache, concurrencyHelper: concurrency, maxAccountSwitches: 2, cfg: cfg}
					if trunk == "gateway_chat" {
						handler.ChatCompletions(client)
					} else if trunk == "gateway_responses" {
						handler.Responses(client)
					} else {
						handler.Messages(client)
					}
				}
				require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
				require.Equal(t, []int64{902}, upstream.accountIDs)
				require.Equal(t, []string{expectedModel}, upstream.models)
				if scenario != "non_composite" {
					require.Equal(t, []string{publicModel}, upstream.publicModels)
					require.Equal(t, []string{secondPlatform}, upstream.platforms)
					require.Equal(t, []service.CompositeRouteTarget{targets[len(targets)-1]}, upstream.targets)
				}
				mapping := channels.ResolveChannelMapping(ctx, groupID, inboundModel)
				fields := clientRequestedUsageFields(client, mapping, inboundModel, expectedModel)
				require.Equal(t, publicModel, fields.OriginalModel)
				require.Equal(t, service.BillingModelSourceChannelMapped, fields.BillingModelSource)
				require.Equal(t, int64(905), fields.ChannelID)
				if scenario != "later_empty_public" {
					require.Equal(t, channelModel, fields.ChannelMappedModel)
				}
				require.Contains(t, recorder.Body.String(), "ok")
			})
		}
	}
}

// Capture the real wire request, returning the protocol requested by each trunk.
type contractForwardUpstream struct {
	compositeChatUpstream
	targets []service.CompositeRouteTarget
}

func (upstream *contractForwardUpstream) Do(request *http.Request, proxy string, accountID int64, concurrency int) (*http.Response, error) {
	if target, ok := service.ActiveCompositeTargetFromContext(request.Context()); ok {
		upstream.targets = append(upstream.targets, target)
	}
	body, _ := io.ReadAll(request.Body)
	request.Body = io.NopCloser(bytes.NewReader(body))
	response, err := upstream.compositeChatUpstream.Do(request, proxy, accountID, concurrency)
	if strings.Contains(request.URL.Path, ":generateContent") || strings.Contains(request.URL.Path, ":streamGenerateContent") {
		modelPath := strings.Split(request.URL.Path, "/models/")
		upstream.models[len(upstream.models)-1] = strings.Split(modelPath[len(modelPath)-1], ":")[0]
		response.Header.Set("Content-Type", "application/json")
		response.Body = io.NopCloser(strings.NewReader(`{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1,"totalTokenCount":2}}`))
	}
	if strings.HasSuffix(request.URL.Path, "/messages") && !gjson.GetBytes(body, "stream").Bool() {
		response.Header.Set("Content-Type", "application/json")
		response.Body = io.NopCloser(strings.NewReader(`{"id":"msg_ok","type":"message","role":"assistant","model":"claude-sonnet-4-5","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}
	if strings.HasSuffix(request.URL.Path, "/responses") {
		response.Header.Set("Content-Type", "application/json")
		response.Body = io.NopCloser(strings.NewReader(`{"id":"resp_ok","object":"response","status":"completed","model":"wire-second","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`))
	}
	return response, err
}
func (upstream *contractForwardUpstream) DoWithTLS(request *http.Request, proxy string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return upstream.Do(request, proxy, accountID, concurrency)
}
