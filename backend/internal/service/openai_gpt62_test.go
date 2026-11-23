package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/openai_compat"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestGPT62AliasNormalizationRemainsUnavailable(t *testing.T) {
	for _, base := range []string{"gpt-6.2-sol", "gpt-6.2-astra"} {
		for _, effort := range []string{"low", "medium", "high", "xhigh", "max", "none", "minimal"} {
			alias := base + "-" + effort
			require.Equal(t, alias, normalizeCodexModel(alias))
			require.Error(t, rejectOpenAIGPT62Alias(&Account{Platform: PlatformOpenAI}, alias))
			require.Nil(t, rejectOpenAIGPT62Alias(&Account{Platform: PlatformOpenRouter}, alias))
		}
		require.Nil(t, rejectOpenAIGPT62Alias(&Account{Platform: PlatformOpenAI}, base))
		require.Equal(t, "max", normalizeOpenAIReasoningEffortForModel("max", base))
	}
	require.Equal(t, "gpt-6-astra", normalizeCodexModel("gpt-6"))
	require.Equal(t, "xhigh", normalizeOpenAIReasoningEffortForModel("max", "gpt-5.5"))
}

func TestGPT62ProviderPrefixedPublishedIDsAreAccepted(t *testing.T) {
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	for _, model := range []string{
		"codex/gpt-6.2-astra",
		"auto/gpt-6.2-sol",
		"openai/gpt-6.2-astra",
	} {
		t.Run(model, func(t *testing.T) {
			for _, body := range []string{`{}`, `{"reasoning_effort":"medium"}`, `{"reasoning":{"effort":"high"}}`} {
				require.Nil(t, validateOpenAIGPT62RequestBody(account, model, []byte(body)))
			}
			for _, body := range []string{`{"reasoning_effort":"none"}`, `{"reasoning":{"effort":"minimal"}}`} {
				require.Error(t, validateOpenAIGPT62RequestBody(account, model, []byte(body)))
			}
		})
	}
	require.Error(t, validateOpenAIGPT62RequestBody(account, "gpt-6.2-astra-max",
		[]byte(`{"reasoning_effort":"medium"}`)))
}

func TestOpenAICompatibleCatalogChatOnlyForwarding(t *testing.T) {
	for _, tc := range []struct {
		model, effort, clientUA, overrideUA, wantUA string
	}{
		{model: "deepseek/deepseek-v4-pro", wantUA: openAIAPIKeyDefaultUserAgent},
		{model: "codex/gpt-6.2-astra", clientUA: "Python-urllib/3.12", wantUA: openAIAPIKeyDefaultUserAgent},
		{model: "codex/gpt-6.2-astra", effort: "high", clientUA: "codex_cli_rs/0.144.1", wantUA: "codex_cli_rs/0.144.1"},
		{model: "auto/kimi-k3", clientUA: "python-requests/2.32", overrideUA: "configured-relay/1", wantUA: "configured-relay/1"},
		{model: "auto/gemini-3.8-flash", clientUA: "Go-http-client/1.1", wantUA: openAIAPIKeyDefaultUserAgent},
		{model: "agy-cli/gemini-3.8-flash", clientUA: "curl/8.10.1", wantUA: "curl/8.10.1"},
	} {
		t.Run(tc.model+"/"+tc.clientUA, func(t *testing.T) {
			account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{
				"api_key": "fixture", "base_url": "https://relay.example",
			}}
			account.Extra = map[string]any{
				UpstreamModelCatalogExtraKey: AccountModelCatalogSnapshot{
					Complete:       true,
					EligibleModels: []AccountCatalogModel{{ID: tc.model, Endpoints: []string{CompositeRouteEndpointChatCompletions}}},
				},
			}
			require.True(t, shouldForwardOpenAIResponsesViaRawChatCompletions(account))
			account.Extra[openai_compat.ExtraKeyResponsesMode] = string(openai_compat.ResponsesSupportModeForceResponses)
			require.False(t, shouldForwardOpenAIResponsesViaRawChatCompletions(account))
			delete(account.Extra, openai_compat.ExtraKeyResponsesMode)
			if tc.overrideUA != "" {
				account.Extra["header_overrides"] = map[string]any{
					"User-Agent": tc.overrideUA, "X-Upstream-Route": "test-route", "Authorization": "ignored",
				}
			}
			body := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hello"}]}`, tc.model)
			if tc.effort != "" {
				body = fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hello"}],"reasoning_effort":%q}`, tc.model, tc.effort)
			}
			writer := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(writer)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
			c.Request.Header.Set("User-Agent", tc.clientUA)
			response := fmt.Sprintf(`{"id":"fixture","object":"chat.completion","model":%q,"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`, tc.model)
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(response)),
			}}
			svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
			_, err := svc.ForwardAsChatCompletions(t.Context(), c, account, []byte(body), "", "")
			require.NoError(t, err)
			require.Len(t, upstream.requests, 1)
			require.Equal(t, "/v1/chat/completions", upstream.lastReq.URL.Path)
			require.Equal(t, tc.wantUA, upstream.lastReq.UserAgent())
			require.Equal(t, tc.model, gjson.GetBytes(upstream.lastBody, "model").String())
			require.Equal(t, tc.effort, gjson.GetBytes(upstream.lastBody, "reasoning_effort").String())
			require.Equal(t, tc.effort != "", gjson.GetBytes(upstream.lastBody, "reasoning_effort").Exists())
			require.Equal(t, "Bearer "+account.GetOpenAIApiKey(), upstream.lastReq.Header.Get("Authorization"))
			if tc.overrideUA != "" {
				require.Equal(t, "test-route", getHeaderRaw(upstream.lastReq.Header, "x-upstream-route"))
			}
		})
	}
}

func TestOpenAICompatibleConnectionProbeHTTP(t *testing.T) {
	var sends atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		if r.URL.Path != "/v1/models" || r.UserAgent() != "test-relay/1" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Header.Get("Authorization") != "Bearer fixture" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, `{"data":[{"id":"provider/chat-model"}]}`)
	}))
	defer upstream.Close()
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{
		"api_key": "fixture", "base_url": upstream.URL + "/v1",
	}, Extra: map[string]any{"header_overrides": map[string]string{"User-Agent": "test-relay/1"}}}

	cfg := &config.Config{}
	cfg.Security.URLAllowlist.Enabled = true
	cfg.Security.URLAllowlist.UpstreamHosts = []string{"127.0.0.1"}
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	cfg.Security.URLAllowlist.AllowPrivateHosts = true
	service := &AccountTestService{cfg: cfg}
	probe := service.ProbeConnection(t.Context(), account, "")
	require.Equal(t, "passed", probe.Network)
	require.Equal(t, "passed", probe.Authorization)
	require.Equal(t, []string{"provider/chat-model"}, probe.Models)

	cfg.Security.URLAllowlist.AllowPrivateHosts = false
	probe = service.ProbeConnection(t.Context(), account, "")
	require.NotEqual(t, "passed", probe.Network)
	cfg.Security.URLAllowlist.AllowPrivateHosts = true
	cfg.Security.URLAllowlist.AllowInsecureHTTP = false
	probe = service.ProbeConnection(t.Context(), account, "")
	require.NotEqual(t, "passed", probe.Network)
	require.EqualValues(t, 2, sends.Load())
}

func TestGPT62UltrafastRemainsBlockedForEveryPlan(t *testing.T) {
	svc := &OpenAIGatewayService{} // also fail closed without a settings service
	for _, model := range []string{"gpt-6.2-sol", "gpt-6.2-astra"} {
		for _, plan := range []string{"", "plus", "prolite", "pro", "chatgpt_pro", "pro_max", "promax", "enterprise", "edu"} {
			for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
				account := &Account{Platform: PlatformOpenAI, Type: kind, Credentials: map[string]any{"plan_type": plan}}
				body := []byte(`{"model":"` + model + `","service_tier":" ULTRAFAST "}`)
				_, err := svc.applyOpenAIFastPolicyToBody(context.Background(), account, model, body)
				var blocked *OpenAIFastBlockedError
				require.ErrorAs(t, err, &blocked)
				frame := []byte(`{"type":"response.create","model":"` + model + `","service_tier":"ultrafast"}`)
				_, denied, err := svc.applyOpenAIFastPolicyToWSResponseCreate(context.Background(), account, model, frame)
				require.NoError(t, err)
				require.NotNil(t, denied)
				require.NotContains(t, accountCodexToolCapabilities(account, model), "service_tiers", "a plan label cannot publish an unsupported tier")
			}
		}
	}
	for _, platform := range []string{PlatformGrok, PlatformOpenRouter, PlatformDeepseek} {
		action, _ := svc.evaluateOpenAIFastPolicy(context.Background(), &Account{Platform: platform}, "gpt-6.2-sol", "ultrafast")
		require.Equal(t, BetaPolicyActionPass, action, "other provider policies remain independent")
	}
}

func TestGPT62MappedUltrafastNeverReachesHTTPUpstream(t *testing.T) {
	for _, model := range []string{"gpt-6.2-sol", "gpt-6.2-astra"} {
		upstream := &httpUpstreamRecorder{}
		svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
		account := rawGPT56ResponsesAPIKeyAccount("public-model", model)
		body := []byte(`{"model":"public-model","service_tier":"ultrafast","input":"hello"}`)
		writer := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(writer)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(body)))
		SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
		result, err := svc.Forward(context.Background(), c, account, body)
		require.Error(t, err)
		require.Nil(t, result)
		require.Equal(t, http.StatusForbidden, writer.Code)
		require.Empty(t, upstream.lastBody)
	}
}

// These fixtures are exact API Standard rates verified on 2026-11-18, not
// subscription prices. They are deliberately not installed as runtime fallbacks.
const gpt62VerifiedStandardPriceFixture = `{
 "gpt-6.2-sol":{"litellm_provider":"openai","mode":"chat","input_cost_per_token":0.000002,"output_cost_per_token":0.00001,"cache_read_input_token_cost":0.0000001},
 "gpt-6.2-astra":{"litellm_provider":"openai","mode":"chat","input_cost_per_token":0.000008,"output_cost_per_token":0.00004,"cache_read_input_token_cost":0.0000004}
}`

func TestGPT62BillingFailsClosedWithoutIntegratedTariff(t *testing.T) {
	bare := NewBillingService(&config.Config{}, nil)
	priced := NewBillingService(&config.Config{}, newStubPricingServiceFromJSON(t, gpt62VerifiedStandardPriceFixture))
	for _, model := range []string{"gpt-6.2-sol", "gpt-6.2-astra"} {
		_, err := bare.GetModelPricing(model)
		require.ErrorIs(t, err, ErrModelPricingUnavailable)
		require.False(t, bare.HasIdentifiedTokenPricing(model))
		before, err := priced.GetModelPricing(model)
		require.NoError(t, err)
		beforeJSON, err := json.Marshal(before)
		require.NoError(t, err)
		cost, err := priced.CalculateCostWithServiceTier(model, UsageTokens{InputTokens: 1000, OutputTokens: 1000}, 1, "ultrafast")
		require.ErrorIs(t, err, ErrModelPricingUnavailable)
		require.Equal(t, PricingStateUnpriced, cost.PricingState)
		require.Zero(t, cost.ActualCost)
		for _, at := range []time.Time{time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC), time.Date(2027, 3, 1, 8, 0, 0, 0, time.UTC)} {
			cost, err = priced.CalculateCostUnified(CostInput{Model: model, Tokens: UsageTokens{InputTokens: 1000}, RateMultiplier: 1, ServiceTier: "ultrafast", PricingAt: at, Resolver: NewModelPricingResolver(nil, priced)})
			require.ErrorIs(t, err, ErrModelPricingUnavailable)
			require.Equal(t, PricingStateUnpriced, cost.PricingState)
			require.Zero(t, cost.ActualCost)
		}
		after, err := priced.GetModelPricing(model)
		require.NoError(t, err)
		afterJSON, err := json.Marshal(after)
		require.NoError(t, err)
		require.JSONEq(t, string(beforeJSON), string(afterJSON), "failed quotes cannot mutate shared rates")
	}
	require.NoError(t, validateOpenAIGPT62BillingTier("other-provider/gpt-6.2-sol", "ultrafast"))
	other := NewBillingService(&config.Config{}, newStubPricingServiceFromJSON(t, `{
		"anthropic/gpt-6.2-sol":{"litellm_provider":"anthropic","input_cost_per_token":0.000007}
	}`))
	for _, resolver := range []*ModelPricingResolver{nil, NewModelPricingResolver(nil, other)} {
		input := CostInput{Provider: "other-provider", Model: "gpt-6.2-sol", Tokens: UsageTokens{InputTokens: 1000}, RateMultiplier: 1, ServiceTier: "ultrafast", Resolver: resolver}
		cost, err := other.CalculateCostUnified(input)
		require.ErrorIs(t, err, ErrModelPricingUnavailable)
		require.Zero(t, cost.ActualCost)

		input.Provider = PlatformAnthropic
		cost, err = other.CalculateCostUnified(input)
		require.NoError(t, err)
		require.InDelta(t, 0.014, cost.ActualCost, 1e-12)
	}
}

func TestGPT62NamesDoNotGrantCatalogEligibility(t *testing.T) {
	for _, plan := range []string{"pro", "promax", "enterprise"} {
		account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Credentials: map[string]any{"plan_type": plan}}
		for _, model := range []string{"gpt-6.2-sol", "gpt-6.2-astra"} {
			require.False(t, AccountSupportsResolvedModel(context.Background(), account, model, AccountModelBindingOptions{}))
			require.NotContains(t, NewModelRegistry().Snapshot().CandidateIDs(PlatformOpenAI, "openai"), model, "release bundle remains unchanged until reviewed together")
		}
	}
}

func TestGPT62UltrafastUsageRetainsUnpricedReviewReceipt(t *testing.T) {
	for _, model := range []string{"gpt-6.2-sol", "gpt-6.2-astra"} {
		t.Run(model, func(t *testing.T) {
			usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
			billingRepo := &openAIRecordUsageBillingRepoStub{result: &UsageBillingApplyResult{Applied: true}}
			userRepo := &openAIRecordUsageUserRepoStub{}
			subRepo := &openAIRecordUsageSubRepoStub{}
			quotaSvc := &openAIRecordUsageAPIKeyQuotaStub{}
			svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(usageRepo, billingRepo, userRepo, subRepo, nil)
			svc.billingService = NewBillingService(&config.Config{}, newStubPricingServiceFromJSON(t, gpt62VerifiedStandardPriceFixture))
			tier := "ultrafast"
			err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
				Result: &OpenAIForwardResult{RequestID: "gpt62-review", Model: model, ServiceTier: &tier, Usage: OpenAIUsage{InputTokens: 1000, OutputTokens: 1000}, Duration: time.Second},
				APIKey: &APIKey{ID: 10, Quota: 100, Group: &Group{RateMultiplier: 1}}, User: &User{ID: 20},
				Account: &Account{ID: 30, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, APIKeyService: quotaSvc,
			})
			require.ErrorIs(t, err, ErrUsageSettlementNeedsReview)
			receipt := receiptFixture(t, svc).receipt
			require.Equal(t, UsageSettlementNeedsReview, receipt.State)
			require.Equal(t, PricingStateUnpriced, receipt.Usage.PricingState)
			require.Equal(t, model, receipt.Usage.Model)
			require.Equal(t, 1000, receipt.Usage.OutputTokens)
			require.Zero(t, billingRepo.calls)
			require.Zero(t, usageRepo.calls)
			require.Zero(t, userRepo.deductCalls)
			require.Zero(t, subRepo.incrementCalls)
			require.Zero(t, quotaSvc.quotaCalls)
		})
	}
}
