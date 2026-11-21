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
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

type heartbeatFailoverUpstream struct {
	first      string
	ambiguous  bool
	accountIDs []int64
}
type heartbeatReadFailure struct{}

func (heartbeatReadFailure) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (upstream *heartbeatFailoverUpstream) Do(request *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	upstream.accountIDs = append(upstream.accountIDs, accountID)
	payload := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_ok\",\"model\":\"claude-sonnet-4-5\",\"usage\":{\"input_tokens\":1}}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	var body io.Reader
	if len(upstream.accountIDs) == 1 {
		payload = upstream.first
		body = strings.NewReader(payload)
		if upstream.ambiguous {
			body = io.MultiReader(body, heartbeatReadFailure{})
		}
	} else {
		body = strings.NewReader(payload)
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(body), Request: request}, nil
}
func (upstream *heartbeatFailoverUpstream) DoWithTLS(request *http.Request, proxy string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return upstream.Do(request, proxy, accountID, concurrency)
}

type heartbeatGroupRepository struct {
	service.GroupRepository
	group *service.Group
}

func (repository *heartbeatGroupRepository) GetByID(context.Context, int64) (*service.Group, error) {
	return repository.group, nil
}

func (repository *heartbeatGroupRepository) GetByIDLite(context.Context, int64) (*service.Group, error) {
	return repository.group, nil
}

type heartbeatAccountRepository struct {
	openAIWSFailoverHandlerAccountRepoStub
	platform string
}

func (repository *heartbeatAccountRepository) ListSchedulableByGroupID(ctx context.Context, _ int64) ([]service.Account, error) {
	return repository.ListSchedulableByPlatform(ctx, repository.platform)
}
func (repository *heartbeatAccountRepository) ListSchedulable(ctx context.Context) ([]service.Account, error) {
	return repository.ListSchedulableByPlatform(ctx, repository.platform)
}

func (repository *heartbeatAccountRepository) ListSchedulableByGroupIDAndPlatforms(ctx context.Context, _ int64, _ []string) ([]service.Account, error) {
	return repository.ListSchedulableByPlatform(ctx, repository.platform)
}

func (repository *heartbeatAccountRepository) SetOverloaded(context.Context, int64, time.Time) error {
	return nil
}

func TestHeartbeatFailoverThroughHandlers(test *testing.T) {
	ping := "event: ping\ndata: {\"type\":\"ping\"}\n\n"
	content := "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\n"
	rejected := "event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"request rejected before generation\"}}\n\n"
	for _, route := range []string{"anthropic", "passthrough", "native"} {
		for _, scenario := range []struct {
			name, first string
			ambiguous   bool
			sends       int
		}{
			{"heartbeat_then_rejection", ping + rejected, false, 2},
			{"content_then_rejection", ping + content + rejected, false, 1},
			{"heartbeat_then_unknown_eof", ping, true, 1},
			{"no_output_unknown_eof", "", true, 1},
			{"content_then_unknown_eof", ping + content, true, 1},
			{"heartbeat_then_unknown_error", ping + strings.ReplaceAll(rejected, "overloaded_error", "api_error"), false, 1},
		} {
			test.Run(route+"/"+scenario.name, func(test *testing.T) {
				cfg := &config.Config{RunMode: config.RunModeSimple}
				cfg.Default.RateMultiplier = 1
				cfg.Security.URLAllowlist.Enabled = false
				cfg.Security.URLAllowlist.AllowInsecureHTTP = true
				platform := service.PlatformAnthropic
				model := "claude-sonnet-4-5"
				if route == "native" {
					platform = service.PlatformKimi
					model = "gpt-5.1"
				}
				groupID := int64(900)
				group := &service.Group{ID: groupID, Hydrated: true, Platform: platform, Status: service.StatusActive, AllowMessagesDispatch: true}
				repository := &heartbeatAccountRepository{platform: platform}
				for _, accountID := range []int64{901, 902} {
					account := service.Account{ID: accountID, Name: "heartbeat", Platform: platform, Type: service.AccountTypeAPIKey,
						Status: service.StatusActive, Schedulable: true, Concurrency: 1, Priority: int(accountID),
						Credentials: map[string]any{"api_key": "test", "base_url": "http://upstream.invalid", "api_protocol": "anthropic"},
						Extra:       map[string]any{"anthropic_passthrough": route == "passthrough"}}
					repository.accounts = append(repository.accounts, account)
				}
				upstream := &heartbeatFailoverUpstream{first: scenario.first, ambiguous: scenario.ambiguous}
				billingCache := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
				test.Cleanup(billingCache.Stop)
				usageRepository := &openAIWSUsageHandlerUsageLogRepoStub{}
				concurrency := NewConcurrencyHelper(service.NewConcurrencyService(&concurrencyCacheMock{
					acquireUserSlotFn:    func(context.Context, int64, int, string) (bool, error) { return true, nil },
					acquireAccountSlotFn: func(context.Context, int64, int, string) (bool, error) { return true, nil },
				}), SSEPingFormatClaude, time.Second)
				recorder := httptest.NewRecorder()
				client, _ := gin.CreateTestContext(recorder)
				client.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"`+model+`","stream":true,"max_tokens":10,"messages":[{"role":"user","content":"hello"}]}`))
				client.Request.Header.Set("Content-Type", "application/json")
				finishBudget := installAttemptBudget(client)
				defer finishBudget()
				client.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 903, UserID: 904, GroupID: &groupID, Group: group, User: &service.User{ID: 904, Status: service.StatusActive}})
				client.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 904, Concurrency: 1})
				if route == "native" {
					gateway := service.ProvideOpenAIGatewayService(repository, usageRepository, nil, nil, nil, nil, nil, cfg, nil, nil, service.NewBillingService(cfg, nil), nil, billingCache, upstream, &service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil, service.NewUsageSettlementService(&unlimitedTPMReceiptStub{}, nil, nil))
					handler := &OpenAIGatewayHandler{gatewayService: gateway, billingCacheService: billingCache, concurrencyHelper: concurrency, maxAccountSwitches: 1, cfg: cfg, apiKeyService: &service.APIKeyService{}}
					handler.Messages(client)
				} else {
					gateway := service.ProvideGatewayService(repository, &heartbeatGroupRepository{group: group}, usageRepository, nil, nil, nil, nil, nil, cfg, nil, nil, service.NewBillingService(cfg, nil), service.NewRateLimitService(repository, nil, cfg, nil, nil), billingCache, nil, upstream, &service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, service.NewUsageSettlementService(&unlimitedTPMReceiptStub{}, nil, nil))
					handler := &GatewayHandler{gatewayService: gateway, billingCacheService: billingCache, concurrencyHelper: concurrency, maxAccountSwitches: 1}
					handler.Messages(client)
				}
				require.Len(test, upstream.accountIDs, scenario.sends, recorder.Body.String())
				if scenario.sends == 2 {
					require.NotEqual(test, upstream.accountIDs[0], upstream.accountIDs[1])
					require.Contains(test, recorder.Body.String(), "message_stop")
				} else if strings.Contains(scenario.name, "content") {
					require.Contains(test, recorder.Body.String(), "hello")
				}
			})
		}
	}
}
