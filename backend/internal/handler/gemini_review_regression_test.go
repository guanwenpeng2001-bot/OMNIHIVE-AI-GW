//go:build unit

package handler

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type geminiReviewUpstream struct {
	requestPlanHTTPFixture
	body       []byte
	path       string
	embeddings bool
	chat       bool
}

func (u *geminiReviewUpstream) Do(req *http.Request, proxy string, id int64, concurrency int) (*http.Response, error) {
	var err error
	u.body, err = io.ReadAll(req.Body)
	require.NoError(u.t, err)
	u.path = req.URL.Path
	if u.embeddings || u.chat {
		u.calls++
		u.plan = service.GatewayRequestPlanFromContext(req.Context())
		reply := `{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1,0.2]}],"usage":{"prompt_tokens":2,"total_tokens":2}}`
		if u.chat {
			reply = `{"id":"chat-fixture","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(reply))}, nil
	}
	req.Body = io.NopCloser(bytes.NewReader(u.body))
	return u.requestPlanHTTPFixture.Do(req, proxy, id, concurrency)
}

func TestGeminiReviewEmbeddingSelection(t *testing.T) {
	for _, action := range []string{"embedContent", "batchEmbedContents"} {
		for _, family := range []string{"embeddings", "responses"} {
			t.Run(action+"/"+family, func(t *testing.T) {
				cfg := &config.Config{RunMode: config.RunModeSimple}
				account := preparedGatewayFixtureAccount(service.Account{ID: 7, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Credentials: map[string]any{"api_key": "fixture"}, AccountGroups: []service.AccountGroup{{AccountID: 7, GroupID: 3}}})
				account.Extra[service.UpstreamModelCatalogExtraKey] = service.AccountModelCatalogSnapshot{Complete: true, DiscoveredModels: []string{"custom-embedding"}, EligibleModels: []service.AccountCatalogModel{{ID: "custom-embedding", Endpoints: []string{family}, EvidenceKind: "fixture"}}}
				repo := &geminiReviewAccountRepo{openAIWSUsageHandlerAccountRepoStub: openAIWSUsageHandlerAccountRepoStub{account: account}}
				usage := &cursorRouteUsageRepo{}
				receipts := newHunyuanSTTReceiptRepository()
				upstream := &geminiReviewUpstream{requestPlanHTTPFixture: requestPlanHTTPFixture{t: t}, embeddings: true}
				svc := service.ProvideOpenAIGatewayService(repo, usage, nil, nil, nil, nil, nil, cfg, nil, nil, service.NewBillingService(cfg, nil), nil, nil, upstream, nil, nil, nil, nil, nil, nil, nil, nil, service.NewUsageSettlementService(receipts, nil, nil))
				billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
				defer billing.Stop()
				h := NewOpenAIGatewayHandler(svc, service.NewConcurrencyService(&fakeConcurrencyCache{}), billing, service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, cfg)
				group := &service.Group{ID: 3, Platform: service.PlatformOpenAI, Status: service.StatusActive, Hydrated: true}
				body := `{"content":{"parts":[{"text":"hello"}]}}`
				if action == "batchEmbedContents" {
					body = `{"requests":[{"content":{"parts":[{"text":"hello"}]}}]}`
				}
				c, rec := requestPlanHTTPContext("/v1beta/models/custom-embedding:"+action, body, group)
				c.Params = gin.Params{{Key: "modelAction", Value: "/custom-embedding:" + action}}
				GeminiProtocolBridge(h.Messages, h.CountTokens, h.Embeddings)(c)
				plan := service.GatewayRequestPlanFromContext(c.Request.Context())
				require.NotNil(t, plan)
				require.Equal(t, "embeddings", plan.Snapshot().Protocol)
				if family == "responses" {
					require.NotEqual(t, 200, rec.Code, rec.Body.String())
					require.Zero(t, upstream.calls)
					return
				}
				require.Equal(t, 200, rec.Code, rec.Body.String())
				require.Equal(t, 1, upstream.calls)
				require.Equal(t, "/v1/embeddings", upstream.path)
				require.Contains(t, rec.Body.String(), "values")
				require.True(t, plan.State().UsageClaimed)
				require.Equal(t, 1, len(usage.logs)+len(receipts.receipts))
			})
		}
	}
}

func TestGeminiReviewReasoningHandlerForwardBody(t *testing.T) {
	for _, model := range []string{"gemini-2.5-flash", "gemini-3-flash-preview", "unknown-custom-model", "claude-sonnet-4-6"} {
		for _, protocol := range []string{"responses", "chat"} {
			for _, effort := range []string{"minimal", "low", "medium", "high", "none", "turbo"} {
				t.Run(model+"/"+protocol+"/"+effort, func(t *testing.T) {
					cfg := &config.Config{RunMode: config.RunModeSimple}
					group := &service.Group{ID: 3, Platform: service.PlatformGemini, Status: service.StatusActive, Hydrated: true}
					account := preparedGatewayFixtureAccount(service.Account{ID: 7, Platform: service.PlatformGemini, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Credentials: map[string]any{"api_key": "fixture", "model_mapping": map[string]any{model: "gemini-3-flash-preview"}}, AccountGroups: []service.AccountGroup{{AccountID: 7, GroupID: 3}}})
					accounts := []*service.Account{&account}
					h, cleanup := newTestGatewayHandler(t, group, accounts)
					defer cleanup()
					h.cfg = cfg
					usage := &cursorRouteUsageRepo{}
					upstream := &geminiReviewUpstream{requestPlanHTTPFixture: requestPlanHTTPFixture{t: t, gemini: true}}
					snapshot := service.NewSchedulerSnapshotService(&fakeSchedulerCache{accounts: accounts}, nil, nil, nil, nil)
					h.gatewayService = service.ProvideGatewayService(nil, &fakeGroupRepo{group: group}, usage, nil, nil, nil, nil, nil, cfg, snapshot, nil, service.NewBillingService(cfg, nil), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, service.NewUsageSettlementService(&unlimitedTPMReceiptStub{}, nil, nil))
					h.geminiCompatService = service.NewGeminiMessagesCompatService(nil, nil, nil, nil, nil, nil, upstream, nil, cfg)
					body := fmt.Sprintf(`{"model":%q,"input":"hello","reasoning":{"effort":%q},"text":{"format":{"type":"json_object"}}}`, model, effort)
					path := "/v1/responses"
					if protocol == "chat" {
						path = "/v1/chat/completions"
						body = fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hello"}],"reasoning_effort":%q}`, model, effort)
					}
					c, rec := requestPlanHTTPContext(path, body, group)
					if protocol == "chat" {
						h.ChatCompletions(c)
					} else {
						h.Responses(c)
					}
					if effort == "turbo" {
						require.Equal(t, 400, rec.Code, rec.Body.String())
						require.Contains(t, rec.Body.String(), "effort")
						require.Zero(t, upstream.calls)
						require.Empty(t, usage.logs)
						return
					}
					require.Equal(t, 200, rec.Code, rec.Body.String())
					require.Equal(t, 1, upstream.calls)
					require.Contains(t, upstream.path, "gemini-3-flash-preview")
					thinking := gjson.GetBytes(upstream.body, "generationConfig.thinkingConfig")
					require.True(t, thinking.Exists(), string(upstream.body))
					if effort == "none" {
						require.Equal(t, int64(0), thinking.Get("thinkingBudget").Int())
						require.True(t, thinking.Get("thinkingBudget").Exists())
					} else {
						require.Equal(t, strings.ToUpper(effort), thinking.Get("thinkingLevel").String())
					}
					if protocol == "responses" {
						require.Equal(t, "application/json", gjson.GetBytes(upstream.body, "generationConfig.responseMimeType").String())
					}
					require.NotContains(t, string(upstream.body), "thoughtSignature")
					require.Len(t, usage.logs, 1)
					require.True(t, upstream.plan.State().UsageClaimed)
				})
			}
		}
	}
}

func TestGeminiReviewCompositeConfiguredOwnership(t *testing.T) {
	for _, platform := range []string{service.PlatformMistral, service.PlatformQianfan, service.PlatformVolcengine} {
		t.Run(platform, func(t *testing.T) {
			cfg := &config.Config{RunMode: config.RunModeSimple}
			const model = "Arbitrary_Deployment-42"
			account := preparedGatewayFixtureAccount(service.Account{ID: 7, Platform: platform, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Credentials: map[string]any{"api_key": "fixture", "model_mapping": map[string]any{model: model}}, AccountGroups: []service.AccountGroup{{AccountID: 7, GroupID: 3}}})
			repo := &geminiReviewAccountRepo{openAIWSUsageHandlerAccountRepoStub: openAIWSUsageHandlerAccountRepoStub{account: account}}
			usage := &cursorRouteUsageRepo{}
			receipts := newHunyuanSTTReceiptRepository()
			upstream := &geminiReviewUpstream{requestPlanHTTPFixture: requestPlanHTTPFixture{t: t}, chat: platform == service.PlatformMistral}
			svc := service.ProvideOpenAIGatewayService(repo, usage, nil, nil, nil, nil, nil, cfg, nil, nil, service.NewBillingService(cfg, nil), nil, nil, upstream, nil, nil, nil, nil, nil, nil, nil, nil, service.NewUsageSettlementService(receipts, nil, nil))
			billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
			defer billing.Stop()
			h := NewOpenAIGatewayHandler(svc, service.NewConcurrencyService(&fakeConcurrencyCache{}), billing, service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, cfg)
			resolver := service.NewCompositeRouteResolver(nil)
			_ = service.NewGatewayService(repo, nil, nil, nil, nil, nil, nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, resolver, nil, nil)
			group := &service.Group{ID: 3, Platform: service.PlatformComposite, Status: service.StatusActive, Hydrated: true}
			for _, requested := range []string{model, "truly-unowned-model"} {
				c, rec := requestPlanHTTPContext("/v1beta/models/"+requested+":generateContent", `{"contents":[{"parts":[{"text":"hello"}]}]}`, group)
				router := gin.New()
				router.Use(func(route *gin.Context) {
					route.Keys = c.Keys
					route.Request = route.Request.WithContext(c.Request.Context())
				})
				dispatch := service.NewGatewayActionDispatch(service.GatewayActionHandlers{"gateway.generate_content_compat": GeminiProtocolBridge(h.Messages, h.CountTokens, h.Embeddings), "gateway.generate_content": func(c *gin.Context) { t.Error("unresolved model reached native Gemini"); c.Status(502) }})
				router.POST("/v1beta/models/*modelAction", service.CompositeGeminiTargetPlatformMiddleware(resolver), dispatch.Handler("generate_content"))
				router.ServeHTTP(rec, c.Request)
				if requested != model {
					require.Equal(t, 400, rec.Code, rec.Body.String())
					require.Contains(t, rec.Body.String(), "action_not_implemented")
					require.Equal(t, 1, upstream.calls)
					continue
				}
				require.Equal(t, 200, rec.Code, rec.Body.String())
				require.Equal(t, model, gjson.GetBytes(upstream.body, "model").String())
				require.Equal(t, "gemini", upstream.plan.Snapshot().Protocol)
				if platform == service.PlatformMistral {
					require.Equal(t, "/v1/chat/completions", upstream.path)
				} else {
					require.Contains(t, upstream.path, "/responses")
				}
				require.Equal(t, 1, upstream.calls)
				require.Equal(t, 1, len(usage.logs)+len(receipts.receipts))
			}
		})
	}
}

type geminiReviewAccountRepo struct {
	openAIWSUsageHandlerAccountRepoStub
}

func (repo *geminiReviewAccountRepo) ListModelAvailabilityCandidates(context.Context, *int64, []string, bool) ([]service.Account, error) {
	return []service.Account{repo.account}, nil
}
func (repo *geminiReviewAccountRepo) ListSchedulableByGroupID(context.Context, int64) ([]service.Account, error) {
	return []service.Account{repo.account}, nil
}
