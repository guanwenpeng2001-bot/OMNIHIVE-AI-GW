//go:build unit

package handler

import (
	"context"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/ctxkey"
	middleware "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type requestPlanHTTPFixture struct {
	service.HTTPUpstream
	t      *testing.T
	calls  int
	gemini bool
	stream bool
	plan   *service.GatewayRequestPlan
}

func (f *requestPlanHTTPFixture) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	f.calls++
	f.plan = service.GatewayRequestPlanFromContext(req.Context())
	require.NotNil(f.t, f.plan)
	require.False(f.t, f.plan.State().Finalized)
	body := `{"id":"response-fixture","model":"gpt-5.1","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":2,"output_tokens":1}}`
	contentType := "text/event-stream"
	if !f.gemini {
		body = "data: {\"type\":\"response.completed\",\"response\":" + body + "}\n\ndata: [DONE]\n\n"
	}
	if f.gemini {
		contentType = "application/json"
		body = `{"candidates":[{"content":{"role":"model","parts":[{"text":"hello"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":2,"candidatesTokenCount":1,"totalTokenCount":3}}`
		if f.stream {
			body = "data: " + body + "\n\n"
			contentType = "text/event-stream"
		}
	}
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{contentType}}, Body: io.NopCloser(strings.NewReader(body))}, nil
}
func requestPlanHTTPContext(path, body string, group *service.Group) (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxkey.Group, group))
	key := &service.APIKey{ID: 2, UserID: 1, GroupID: &group.ID, Group: group, User: &service.User{ID: 1, Status: service.StatusActive, Balance: 100}}
	c.Set(string(middleware.ContextKeyAPIKey), key)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1, Concurrency: 2})
	return c, recorder
}
func TestGatewayRequestPlanNativeHTTPIngress(t *testing.T) {
	_, stop := captureHandlerStructuredLog(t)
	defer stop()
	for _, enabled := range []bool{false, true} {
		for _, path := range []string{"/v1/messages", "/v1/chat/completions", "/v1/responses", "/responses", "/backend-api/codex/responses"} {
			t.Run(fmt.Sprintf("%t%s", enabled, path), func(t *testing.T) {
				cfg := &config.Config{RunMode: config.RunModeSimple}
				cfg.Gateway.SessionAffinityEnabled = enabled
				account := preparedGatewayFixtureAccount(service.Account{ID: 7, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true, Credentials: map[string]any{"access_token": "fixture"}, AccountGroups: []service.AccountGroup{{AccountID: 7, GroupID: 3}}})
				repo := &openAIWSUsageHandlerAccountRepoStub{account: account}
				usage := &cursorRouteUsageRepo{}
				upstream := &requestPlanHTTPFixture{t: t}
				svc := service.ProvideOpenAIGatewayService(repo, usage, nil, nil, nil, nil, nil, cfg, nil, nil, service.NewBillingService(cfg, nil), nil, nil, upstream, nil, nil, nil, nil, nil, nil, nil, nil, service.NewUsageSettlementService(&unlimitedTPMReceiptStub{}, nil, nil))
				billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
				defer billing.Stop()
				h := NewOpenAIGatewayHandler(svc, service.NewConcurrencyService(&fakeConcurrencyCache{}), billing, service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, cfg)
				group := &service.Group{ID: 3, Platform: service.PlatformOpenAI, Status: service.StatusActive, Hydrated: true, AllowMessagesDispatch: true}
				c, recorder := requestPlanHTTPContext(path, `{"model":"gpt-5.1","messages":[{"role":"user","content":"hello"}],"input":"hello","max_tokens":32,"stream":false}`, group)
				switch path {
				case "/v1/messages":
					h.Messages(c)
				case "/v1/chat/completions":
					h.ChatCompletions(c)
				default:
					h.Responses(c)
				}
				if recorder.Code != 200 {
					t.Logf("calls=%d keys=%+v", upstream.calls, c.Keys)
				}
				require.Equal(t, 200, recorder.Code, recorder.Body.String())
				require.Equal(t, 1, upstream.calls)
				require.Equal(t, path, upstream.plan.Snapshot().Route)
				require.EqualValues(t, 1, upstream.plan.Snapshot().UserID)
				require.True(t, upstream.plan.State().Finalized)
				require.True(t, upstream.plan.State().UsageClaimed)
				require.Len(t, usage.logs, 1)
			})
		}
	}
}
func TestGatewayRequestPlanGeminiHTTPIngress(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("affinity=%t/stream=%t", enabled, stream), func(t *testing.T) {
				cfg := &config.Config{RunMode: config.RunModeSimple}
				cfg.Gateway.SessionAffinityEnabled = enabled
				group := &service.Group{ID: 3, Platform: service.PlatformGemini, Status: service.StatusActive, Hydrated: true}
				account := preparedGatewayFixtureAccount(service.Account{ID: 7, Platform: service.PlatformGemini, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Credentials: map[string]any{"api_key": "fixture"}, AccountGroups: []service.AccountGroup{{AccountID: 7, GroupID: 3}}})
				catalog := service.AccountModelCatalogSnapshot{Complete: true, DiscoveredModels: []string{"gemini-2.5-flash"}, EligibleModels: []service.AccountCatalogModel{{ID: "gemini-2.5-flash", Endpoints: []string{"generate_content"}, EvidenceKind: "fixture"}}}
				account.Extra[service.UpstreamModelCatalogExtraKey] = catalog
				accounts := []*service.Account{&account}
				h, cleanup := newTestGatewayHandler(t, group, accounts)
				defer cleanup()
				h.cfg = cfg
				usage := &cursorRouteUsageRepo{}
				upstream := &requestPlanHTTPFixture{t: t, gemini: true, stream: stream}
				snapshot := service.NewSchedulerSnapshotService(&fakeSchedulerCache{accounts: accounts}, nil, nil, nil, nil)
				h.gatewayService = service.ProvideGatewayService(nil, &fakeGroupRepo{group: group}, usage, nil, nil, nil, nil, nil, cfg, snapshot, nil, service.NewBillingService(cfg, nil), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, service.NewUsageSettlementService(&unlimitedTPMReceiptStub{}, nil, nil))
				h.geminiCompatService = service.NewGeminiMessagesCompatService(nil, nil, nil, nil, nil, nil, upstream, nil, cfg)
				action := "generateContent"
				if stream {
					action = "streamGenerateContent"
				}
				path := "/v1beta/models/gemini-2.5-flash:" + action
				c, recorder := requestPlanHTTPContext(path, `{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`, group)
				c.Params = gin.Params{{Key: "modelAction", Value: "/gemini-2.5-flash:" + action}}
				h.GeminiV1BetaModels(c)
				if recorder.Code != 200 {
					t.Logf("calls=%d keys=%+v", upstream.calls, c.Keys)
				}
				require.Equal(t, 200, recorder.Code, recorder.Body.String())
				require.Equal(t, 1, upstream.calls)
				require.True(t, upstream.plan.State().Finalized)
				require.True(t, upstream.plan.State().UsageClaimed)
				require.Len(t, usage.logs, 1)
			})
		}
	}
}

func TestGatewayRequestPlanWebSocketIngressTwoTurns(t *testing.T) {
	for _, mode := range []string{service.OpenAIWSIngressModePassthrough, service.OpenAIWSIngressModeCtxPool} {
		t.Run(mode, func(t *testing.T) {
			got := runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{affinityEnabled: true, ingressMode: mode, firstPayload: `{"type":"response.create","model":"gpt-5.1","input":[{"role":"user","content":"anchor"}],"stream":false}`, secondPayload: `{"type":"response.create","model":"gpt-5.1","input":[{"role":"user","content":"anchor"},{"role":"user","content":"next"}],"stream":false}`})
			require.Len(t, got.logs, 2)
			require.Len(t, got.plans, 2)
			require.NotNil(t, got.plans[0])
			require.NotNil(t, got.plans[1])
			require.NotSame(t, got.plans[0], got.plans[1])
			require.True(t, got.plans[0].State().UsageClaimed)
			require.True(t, got.plans[1].State().UsageClaimed)
			require.NotEqual(t, got.logs[0].RequestID, got.logs[1].RequestID)
		})
	}
}

func TestDuplicateModelHTTPIngressMappingMatrix(t *testing.T) {
	for _, mapped := range []string{"gpt-5.1", "gpt-5.2"} {
		for _, path := range []string{"/v1/messages", "/v1/chat/completions", "/v1/responses"} {
			t.Run(mapped+path, func(t *testing.T) {
				cfg := &config.Config{RunMode: config.RunModeSimple}
				account := preparedGatewayFixtureAccount(service.Account{ID: 7, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Credentials: map[string]any{"api_key": "fixture", "model_mapping": map[string]any{"gpt-5.1": mapped}}, AccountGroups: []service.AccountGroup{{AccountID: 7, GroupID: 3}}})
				repo := &openAIWSUsageHandlerAccountRepoStub{account: account}
				upstream := &requestPlanHTTPFixture{t: t}
				svc := service.NewOpenAIGatewayService(repo, nil, nil, nil, nil, nil, nil, cfg, nil, nil, service.NewBillingService(cfg, nil), nil, nil, upstream, nil, nil, nil, nil, nil, nil, nil, nil)
				billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
				defer billing.Stop()
				h := NewOpenAIGatewayHandler(svc, service.NewConcurrencyService(&fakeConcurrencyCache{}), billing, service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, cfg)
				group := &service.Group{ID: 3, Platform: service.PlatformOpenAI, Status: service.StatusActive, Hydrated: true, AllowMessagesDispatch: true}
				c, rec := requestPlanHTTPContext(path, `{"model":"gpt-5.1","messages":[{"role":"user","content":"hello"}],"input":"hello","max_tokens":32,"model":"revoked"}`, group)
				switch path {
				case "/v1/messages":
					h.Messages(c)
				case "/v1/chat/completions":
					h.ChatCompletions(c)
				default:
					h.Responses(c)
				}
				require.Equal(t, http.StatusBadRequest, rec.Code)
				require.Zero(t, upstream.calls)
				require.NotContains(t, rec.Body.String(), "revoked")
			})
		}
	}
}

func TestGatewayTPM_AllTokenEntrypointsReserveBeforeUpstream(t *testing.T) {
	for _, path := range []string{"/v1/messages", "/v1/chat/completions", "/v1/responses", "/responses", "/backend-api/codex/responses"} {
		t.Run(path, func(t *testing.T) {
			cfg := &config.Config{RunMode: config.RunModeSimple}
			account := preparedGatewayFixtureAccount(service.Account{ID: 7, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true, Credentials: map[string]any{"access_token": "fixture"}, AccountGroups: []service.AccountGroup{{AccountID: 7, GroupID: 3}}})
			repository := &openAIWSUsageHandlerAccountRepoStub{account: account}
			upstream := &requestPlanHTTPFixture{t: t}
			receiptStore := &unlimitedTPMReceiptStub{unlimitedTPMRepository: unlimitedTPMRepository{rejection: &service.TPMLimitExceeded{Scope: service.TPMScopeGroup}}}
			gateway := service.ProvideOpenAIGatewayService(repository, nil, nil, nil, nil, nil, nil, cfg, nil, nil, service.NewBillingService(cfg, nil), nil, nil, upstream, nil, nil, nil, nil, nil, nil, nil, nil, service.NewUsageSettlementService(receiptStore, nil, nil))
			billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
			defer billing.Stop()
			handler := NewOpenAIGatewayHandler(gateway, service.NewConcurrencyService(&fakeConcurrencyCache{}), billing, service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, cfg)
			group := &service.Group{ID: 3, Platform: service.PlatformOpenAI, Status: service.StatusActive, Hydrated: true, AllowMessagesDispatch: true}
			request, response := requestPlanHTTPContext(path, `{"model":"gpt-5.1","messages":[{"role":"user","content":"hello"}],"input":"hello","max_tokens":32,"stream":false}`, group)
			switch path {
			case "/v1/messages":
				handler.Messages(request)
			case "/v1/chat/completions":
				handler.ChatCompletions(request)
			default:
				handler.Responses(request)
			}
			require.Equal(t, 429, response.Code, response.Body.String())
			require.Contains(t, response.Body.String(), "GROUP_TPM_EXCEEDED")
			require.Zero(t, upstream.calls)
		})
	}
}
