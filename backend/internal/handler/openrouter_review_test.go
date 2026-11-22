//go:build unit

package handler

import (
	"fmt"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestCompositeTextPreflightMatchesOpenAIChatDescriptor(t *testing.T) {
	for _, descriptor := range domain.PlatformDescriptors() {
		cell := domain.PlatformAction(domain.ActionIdentity{Platform: descriptor.ID, HostedAccountType: service.AccountTypeAPIKey}, "chat")
		if cell.Adapter != "openai.chat" {
			continue
		}
		group := &service.Group{ID: 3, Platform: service.PlatformComposite}
		request, _ := requestPlanHTTPContext("/v1/chat/completions", `{}`, group)
		request.Request = request.Request.WithContext(service.WithCompositeRouteDecision(request.Request.Context(), service.CompositeRouteDecision{Matched: true, Source: service.CompositeRouteSourceExplicit, PublicModel: "vendor/future", UpstreamModel: "vendor/future", TargetPlatform: descriptor.ID}))
		require.True(t, openAICompatibleTextTargetAllowed(request, &service.APIKey{Group: group}, "vendor/future"), descriptor.ID)
	}
}

func TestCompositeWorkBuddyKiloTextPreflight(t *testing.T) {
	for _, platform := range []string{service.PlatformWorkBuddy, service.PlatformKilo} {
		model := "kilo-auto/free"
		paths := []string{"/v1/chat/completions", "/v1/responses", "/v1/messages"}
		if platform == service.PlatformWorkBuddy {
			model = "auto"
			paths = append(paths, "/v1beta/models/auto:generateContent")
		}
		for _, path := range paths {
			t.Run(platform+path, func(t *testing.T) {
				cfg := &config.Config{RunMode: config.RunModeSimple}
				account := preparedGatewayFixtureAccount(service.Account{ID: 7, Platform: platform, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Credentials: map[string]any{"model_mapping": map[string]any{model: model}}, AccountGroups: []service.AccountGroup{{AccountID: 7, GroupID: 3}}})
				if platform == service.PlatformWorkBuddy {
					account.Credentials["api_key"] = "fixture"
				} else {
					// A published catalog supersedes the bootstrap inventory, even
					// for kilo-auto/free. Supply current native free evidence.
					account.Credentials["account_mode"] = service.KiloAccountModeFree
					account.Extra[service.UpstreamModelCatalogExtraKey] = service.AccountModelCatalogSnapshot{
						Complete: true, SourceKind: "live", DiscoveryScope: "account",
						DiscoveredModels: []string{model},
						EligibleModels:   []service.AccountCatalogModel{{ID: model, Endpoints: []string{"chat_completions"}, EvidenceKind: "native_account_catalog"}},
					}
					free := true
					account.SetUpstreamModelMetadataSnapshot(service.UpstreamModelMetadataSnapshot{
						Source: "live", Models: map[string]service.UpstreamModelMetadata{model: {ID: model, Free: &free}},
					})
				}
				repo := &geminiReviewAccountRepo{openAIWSUsageHandlerAccountRepoStub: openAIWSUsageHandlerAccountRepoStub{account: account}}
				upstream := &openRouterHandlerUpstream{geminiReviewUpstream: geminiReviewUpstream{requestPlanHTTPFixture: requestPlanHTTPFixture{t: t}, chat: true}}
				svc := service.ProvideOpenAIGatewayService(repo, &cursorRouteUsageRepo{}, nil, nil, nil, nil, nil, cfg, nil, nil, service.NewBillingService(cfg, nil), nil, nil, upstream, nil, nil, nil, nil, nil, nil, nil, nil, service.NewUsageSettlementService(newHunyuanSTTReceiptRepository(), nil, nil))
				billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
				t.Cleanup(billing.Stop)
				h := NewOpenAIGatewayHandler(svc, service.NewConcurrencyService(&fakeConcurrencyCache{}), billing, service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, cfg)
				group := &service.Group{ID: 3, Platform: service.PlatformComposite, Status: service.StatusActive, Hydrated: true}
				payload := fmt.Sprintf(`{"model":%q,"max_tokens":32,"messages":[{"role":"user","content":"hello"}]}`, model)
				next := h.ChatCompletions
				switch path {
				case "/v1/responses":
					next = h.Responses
					payload = fmt.Sprintf(`{"model":%q,"input":"hello"}`, model)
				case "/v1/messages":
					next = h.Messages
				case "/v1beta/models/auto:generateContent":
					next = GeminiProtocolBridge(h.Messages, h.CountTokens, h.Embeddings)
					payload = `{"contents":[{"parts":[{"text":"hello"}]}]}`
				}
				c, rec := requestPlanHTTPContext(path, payload, group)
				c.Params = gin.Params{{Key: "modelAction", Value: "/" + model + ":generateContent"}}
				c.Request = c.Request.WithContext(service.WithCompositeRouteDecision(c.Request.Context(), service.CompositeRouteDecision{Matched: true, Source: service.CompositeRouteSourceExplicit, PublicModel: model, UpstreamModel: model, TargetPlatform: platform}))
				next(c)
				require.Equal(t, 200, rec.Code, rec.Body.String())
				require.Equal(t, 1, upstream.calls)
				require.Equal(t, model, gjson.GetBytes(upstream.body, "model").String())
				require.Contains(t, rec.Body.String(), "hello")
			})
		}
	}
}
