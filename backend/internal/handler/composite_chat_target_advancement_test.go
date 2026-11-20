//go:build unit

package handler

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
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/tlsfingerprint"
	middleware "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type compositeChatAccountRepository struct {
	openAIImagesFailoverAccountRepo
}

func (repo compositeChatAccountRepository) ListModelAvailabilityCandidates(ctx context.Context, _ *int64, platforms []string, _ bool) ([]service.Account, error) {
	return repo.ListSchedulableByGroupIDAndPlatforms(ctx, 0, platforms)
}

func (repo compositeChatAccountRepository) ListSchedulableByGroupIDAndPlatforms(ctx context.Context, group int64, platforms []string) ([]service.Account, error) {
	var accounts []service.Account
	for _, platform := range platforms {
		selected, _ := repo.ListSchedulableByPlatform(ctx, platform)
		accounts = append(accounts, selected...)
	}
	return accounts, nil
}

type compositeChatUpstream struct {
	service.HTTPUpstream
	accountIDs   []int64
	models       []string
	publicModels []string
	platforms    []string
}

func (upstream *compositeChatUpstream) Do(req *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	body, _ := io.ReadAll(req.Body)
	upstream.accountIDs = append(upstream.accountIDs, accountID)
	upstream.models = append(upstream.models, gjson.GetBytes(body, "model").String())
	publicModel, _ := service.RequestedPublicModelFromContext(req.Context())
	platform, _ := service.ResolvedTargetPlatformFromContext(req.Context())
	upstream.publicModels = append(upstream.publicModels, publicModel)
	upstream.platforms = append(upstream.platforms, platform)
	payload := `{"id":"chat_ok","object":"chat.completion","model":"wire-second","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`
	contentType := "application/json"
	if strings.HasSuffix(req.URL.Path, "/messages") {
		contentType = "text/event-stream"
		payload = "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_ok\",\"model\":\"claude-sonnet-4-5\",\"role\":\"assistant\",\"content\":[],\"usage\":{\"input_tokens\":1}}}\n\nevent: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\nevent: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(payload)), Request: req}, nil
}
func (upstream *compositeChatUpstream) DoWithTLS(req *http.Request, proxy string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return upstream.Do(req, proxy, accountID, concurrency)
}

func TestCompositeChatTargetAdvancementHandlers(t *testing.T) {
	for _, trunk := range []string{"openai_chat", "gateway_chat", "gateway_responses"} {
		for _, scenario := range []string{"first_empty", "first_unselected", "exhausted"} {
			t.Run(trunk+"/"+scenario, func(t *testing.T) {
				cfg := &config.Config{RunMode: config.RunModeSimple}
				cfg.Default.RateMultiplier = 1
				groupID := int64(900)
				group := &service.Group{ID: groupID, Hydrated: true, Platform: service.PlatformComposite, Status: service.StatusActive}
				secondPlatform, secondModel := service.PlatformAnthropic, "claude-sonnet-4-5"
				if trunk == "openai_chat" {
					secondPlatform, secondModel = service.PlatformCommandCode, "wire-second"
				}
				targets := []service.CompositeRouteTarget{
					{Position: 0, TargetPlatform: service.PlatformOpenAI, UpstreamModel: "wire-first", Enabled: true},
					{Position: 1, TargetPlatform: secondPlatform, UpstreamModel: secondModel, Enabled: true},
				}
				repo := compositeChatAccountRepository{}
				if scenario == "first_unselected" {
					repo.accounts = append(repo.accounts, service.Account{ID: 901, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Credentials: map[string]any{"api_key": "fixture"}})
				}
				if scenario != "exhausted" {
					repo.accounts = append(repo.accounts, service.Account{ID: 902, Name: "selected", Platform: secondPlatform, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Concurrency: 1, Credentials: map[string]any{"api_key": "fixture", "base_url": "https://upstream.invalid", "model_mapping": map[string]string{secondModel: secondModel}}, AccountGroups: []service.AccountGroup{{AccountID: 902, GroupID: groupID}}})
				}
				upstream := &compositeChatUpstream{}
				billingCache := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
				t.Cleanup(billingCache.Stop)
				concurrency := NewConcurrencyHelper(service.NewConcurrencyService(&concurrencyCacheMock{
					acquireUserSlotFn:    func(context.Context, int64, int, string) (bool, error) { return true, nil },
					acquireAccountSlotFn: func(context.Context, int64, int, string) (bool, error) { return true, nil },
				}), SSEPingFormatClaude, time.Second)
				recorder := httptest.NewRecorder()
				client, _ := gin.CreateTestContext(recorder)
				path, body := "/v1/chat/completions", `{"model":"Exact.Public-Alias","messages":[{"role":"user","content":"hello"}],"stream":false}`
				if trunk == "gateway_responses" {
					path, body = "/v1/responses", `{"model":"Exact.Public-Alias","input":"hello","stream":false}`
				}
				ctx := service.WithCompositeRouteDecision(context.Background(), service.CompositeRouteDecision{Matched: true, PublicModel: "Exact.Public-Alias", TargetPlatform: service.PlatformOpenAI, UpstreamModel: "wire-first", Targets: targets})
				client.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)).WithContext(ctx)
				client.Request.Header.Set("Content-Type", "application/json")
				client.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 903, UserID: 904, GroupID: &groupID, Group: group, User: &service.User{ID: 904, Status: service.StatusActive}})
				client.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 904, Concurrency: 1})
				receipts := service.NewUsageSettlementService(&unlimitedTPMReceiptStub{}, nil, nil)
				if trunk == "openai_chat" {
					gateway := service.ProvideOpenAIGatewayService(repo, nil, nil, nil, nil, nil, nil, cfg, nil, nil, service.NewBillingService(cfg, nil), nil, billingCache, upstream, &service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil, receipts)
					handler := &OpenAIGatewayHandler{gatewayService: gateway, billingCacheService: billingCache, concurrencyHelper: concurrency, maxAccountSwitches: 2, cfg: cfg, apiKeyService: &service.APIKeyService{}}
					handler.ChatCompletions(client)
				} else {
					gateway := service.ProvideGatewayService(repo, &heartbeatGroupRepository{group: group}, nil, nil, nil, nil, nil, nil, cfg, nil, nil, service.NewBillingService(cfg, nil), nil, billingCache, nil, upstream, &service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, receipts)
					handler := &GatewayHandler{gatewayService: gateway, billingCacheService: billingCache, concurrencyHelper: concurrency, maxAccountSwitches: 2, cfg: cfg}
					if trunk == "gateway_chat" {
						handler.ChatCompletions(client)
					} else {
						handler.Responses(client)
					}
				}
				if scenario == "exhausted" {
					require.Equal(t, http.StatusServiceUnavailable, recorder.Code, recorder.Body.String())
					require.Empty(t, upstream.accountIDs)
					if trunk == "openai_chat" {
						require.Contains(t, recorder.Body.String(), "Service temporarily unavailable")
					} else {
						require.Contains(t, recorder.Body.String(), "No available accounts")
					}
					lastTarget, ok := service.ActiveCompositeTargetFromContext(client.Request.Context())
					require.True(t, ok)
					require.Equal(t, targets[1], lastTarget)
					publicModel, _ := service.RequestedPublicModelFromContext(client.Request.Context())
					require.Equal(t, "Exact.Public-Alias", publicModel)
					return
				}
				require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
				require.Equal(t, []int64{902}, upstream.accountIDs)
				require.Equal(t, []string{secondModel}, upstream.models)
				require.Equal(t, []string{"Exact.Public-Alias"}, upstream.publicModels)
				require.Equal(t, []string{secondPlatform}, upstream.platforms)
				require.Contains(t, recorder.Body.String(), "ok")
			})
		}
	}
}
