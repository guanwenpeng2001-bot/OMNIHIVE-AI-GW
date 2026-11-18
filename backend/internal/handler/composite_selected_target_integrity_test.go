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
)

type selectedTargetHandlerSessionCache struct {
	service.SessionLimitCache
	rejected []int64
}

func (cache *selectedTargetHandlerSessionCache) RegisterSession(_ context.Context, accountID int64, _ string, _ int, _ time.Duration) (bool, error) {
	cache.rejected = append(cache.rejected, accountID)
	return false, nil
}
func (*selectedTargetHandlerSessionCache) GetWindowCostBatch(context.Context, []int64) (map[int64]float64, error) {
	return nil, nil
}

type selectedTargetHandlerUpstream struct {
	compositeChatUpstream
	bodies []string
}

func (upstream *selectedTargetHandlerUpstream) Do(req *http.Request, proxy string, accountID int64, concurrency int) (*http.Response, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	upstream.bodies = append(upstream.bodies, string(body))
	req.Body = io.NopCloser(strings.NewReader(string(body)))
	if strings.Contains(req.URL.Path, "GenerateContent") || strings.Contains(req.URL.Path, "generateContent") {
		upstream.accountIDs = append(upstream.accountIDs, accountID)
		modelPath := strings.Split(req.URL.Path, "models/")[1]
		upstream.models = append(upstream.models, strings.Split(modelPath, ":")[0])
		public, _ := service.RequestedPublicModelFromContext(req.Context())
		platform, _ := service.ResolvedTargetPlatformFromContext(req.Context())
		upstream.publicModels = append(upstream.publicModels, public)
		upstream.platforms = append(upstream.platforms, platform)
		payload := `{"candidates":[{"content":{"parts":[{"text":"ok"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1}}`
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(payload)), Request: req}, nil
	}
	return upstream.compositeChatUpstream.Do(req, proxy, accountID, concurrency)
}

func (upstream *selectedTargetHandlerUpstream) DoWithTLS(req *http.Request, proxy string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return upstream.Do(req, proxy, accountID, concurrency)
}

func TestCompositeSelectedTargetAfterSessionCap(t *testing.T) {
	for _, trunk := range []string{"gateway_chat", "gateway_responses"} {
		scenarios := []string{"same_platform", "exhausted"}
		if trunk == "gateway_chat" {
			scenarios = append(scenarios, "cross_platform")
		}
		for _, scenario := range scenarios {
			t.Run(trunk+"/"+scenario, func(t *testing.T) {
				cfg := &config.Config{RunMode: config.RunModeSimple}
				cfg.Default.RateMultiplier = 1
				cfg.Gateway.Scheduling.LoadBatchEnabled = false
				groupID := int64(900)
				group := &service.Group{ID: groupID, Hydrated: true, Platform: service.PlatformComposite, Status: service.StatusActive}
				secondPlatform, secondModel := service.PlatformAnthropic, "claude-sonnet-4-5"
				if scenario == "cross_platform" {
					secondPlatform, secondModel = service.PlatformGemini, "gemini-2.5-flash"
				}

				targets := []service.CompositeRouteTarget{
					{Position: 0, TargetPlatform: service.PlatformAnthropic, AccountType: service.AccountTypeOAuth, UpstreamModel: "claude-sonnet-4-6", Enabled: true},
					{Position: 1, TargetPlatform: secondPlatform, AccountType: service.AccountTypeAPIKey, UpstreamModel: secondModel, Enabled: true},
				}
				repo := compositeChatAccountRepository{}
				repo.accounts = append(repo.accounts, service.Account{ID: 901, Platform: service.PlatformAnthropic, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true, Concurrency: 1, Extra: map[string]any{"max_sessions": 1}, Credentials: map[string]any{"access_token": "fixture", "model_mapping": map[string]string{"Exact.Public-Alias": "claude-sonnet-4-6", "claude-sonnet-4-6": "claude-sonnet-4-6"}}, AccountGroups: []service.AccountGroup{{AccountID: 901, GroupID: groupID}}})

				if scenario != "exhausted" {
					repo.accounts = append(repo.accounts, service.Account{ID: 902, Name: "selected", Platform: secondPlatform, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Concurrency: 1, Credentials: map[string]any{"api_key": "fixture", "base_url": "https://upstream.invalid", "model_mapping": map[string]string{secondModel: secondModel}}, AccountGroups: []service.AccountGroup{{AccountID: 902, GroupID: groupID}}})
				}
				upstream := &selectedTargetHandlerUpstream{}
				sessions := &selectedTargetHandlerSessionCache{}
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
				ctx := service.WithCompositeRouteDecision(context.Background(), service.CompositeRouteDecision{Matched: true, PublicModel: "Exact.Public-Alias", TargetPlatform: service.PlatformAnthropic, UpstreamModel: "claude-sonnet-4-6", Targets: targets})
				client.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)).WithContext(ctx)
				client.Request.Header.Set("Content-Type", "application/json")
				client.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 903, UserID: 904, GroupID: &groupID, Group: group, User: &service.User{ID: 904, Status: service.StatusActive}})
				client.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 904, Concurrency: 1})
				receipts := service.NewUsageSettlementService(&unlimitedTPMReceiptStub{}, nil, nil)
				gateway := service.ProvideGatewayService(repo, &heartbeatGroupRepository{group: group}, nil, nil, nil, nil, nil, nil, cfg, nil, nil, service.NewBillingService(cfg, nil), nil, billingCache, nil, upstream, &service.DeferredService{}, nil, sessions, nil, nil, nil, nil, nil, nil, nil, nil, nil, receipts)
				handler := &GatewayHandler{gatewayService: gateway, billingCacheService: billingCache, concurrencyHelper: concurrency, maxAccountSwitches: 2, cfg: cfg, geminiCompatService: service.NewGeminiMessagesCompatService(repo, nil, nil, nil, nil, nil, upstream, nil, cfg)}
				if trunk == "gateway_chat" {
					handler.ChatCompletions(client)
				} else {
					handler.Responses(client)
				}
				require.Equal(t, []int64{901}, sessions.rejected)
				if scenario == "exhausted" {
					require.Equal(t, http.StatusServiceUnavailable, recorder.Code, recorder.Body.String())
					require.Empty(t, upstream.accountIDs)
					require.Contains(t, recorder.Body.String(), "No available accounts")
					lastTarget, ok := service.ActiveCompositeTargetFromContext(client.Request.Context())
					require.True(t, ok)
					require.Equal(t, targets[0], lastTarget)
					publicModel, _ := service.RequestedPublicModelFromContext(client.Request.Context())
					require.Equal(t, "Exact.Public-Alias", publicModel)
					return
				}
				require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
				require.Equal(t, []int64{902}, upstream.accountIDs)
				require.Len(t, upstream.bodies, 1)
				require.Contains(t, upstream.bodies[0], "hello")
				require.NotContains(t, upstream.bodies[0], "claude-sonnet-4-6")
				require.Equal(t, []string{secondModel}, upstream.models)
				require.Equal(t, []string{"Exact.Public-Alias"}, upstream.publicModels)
				require.Equal(t, []string{secondPlatform}, upstream.platforms)
				actual, ok := service.ActiveCompositeTargetFromContext(client.Request.Context())
				require.True(t, ok)
				require.Equal(t, targets[1], actual)
			})
		}
	}
}
