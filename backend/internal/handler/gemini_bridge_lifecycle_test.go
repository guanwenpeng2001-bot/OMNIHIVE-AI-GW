//go:build unit

package handler

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type geminiBridgeLifecycleUpstream struct {
	requestPlanHTTPFixture
	mode     string
	accounts []int64
}

type geminiOpenCodeLifecycleUpstream struct {
	requestPlanHTTPFixture
	path string
}

type geminiControlsLifecycleUpstream struct {
	requestPlanHTTPFixture
	body []byte
	chat bool
}

func (u *geminiControlsLifecycleUpstream) Do(req *http.Request, proxy string, id int64, concurrency int) (*http.Response, error) {
	var err error
	u.body, err = io.ReadAll(req.Body)
	require.NoError(u.t, err)
	if u.chat {
		u.calls++
		u.plan = service.GatewayRequestPlanFromContext(req.Context())
		require.Equal(u.t, "/v1/chat/completions", req.URL.Path)
		require.Equal(u.t, "Bearer fixture", req.Header.Get("Authorization"))
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"chat-fixture","model":"mistral-small-latest","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`))}, nil
	}
	req.Body = io.NopCloser(bytes.NewReader(u.body))
	return u.requestPlanHTTPFixture.Do(req, proxy, id, concurrency)
}

func TestPublicMistralDefaultGroupHandlerLifecycle(t *testing.T) {
	for _, google := range []bool{false, true} {
		t.Run(fmt.Sprint(google), func(t *testing.T) {
			cfg := &config.Config{RunMode: config.RunModeSimple}
			account := preparedGatewayFixtureAccount(service.Account{ID: 7, Platform: service.PlatformMistral, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Credentials: map[string]any{"api_key": "fixture", "model_mapping": map[string]any{"claude-sonnet-4-5": "mistral-small-latest"}}, AccountGroups: []service.AccountGroup{{AccountID: 7, GroupID: 3}}})
			repo := &openAIWSUsageHandlerAccountRepoStub{account: account}
			usage := &cursorRouteUsageRepo{}
			receipts := newHunyuanSTTReceiptRepository()
			upstream := &geminiControlsLifecycleUpstream{requestPlanHTTPFixture: requestPlanHTTPFixture{t: t}, chat: true}
			svc := service.ProvideOpenAIGatewayService(repo, usage, nil, nil, nil, nil, nil, cfg, nil, nil, service.NewBillingService(cfg, nil), nil, nil, upstream, nil, nil, nil, nil, nil, nil, nil, nil, service.NewUsageSettlementService(receipts, nil, nil))
			billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
			defer billing.Stop()
			h := NewOpenAIGatewayHandler(svc, service.NewConcurrencyService(&fakeConcurrencyCache{}), billing, service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, cfg)
			group := &service.Group{ID: 3, Platform: service.PlatformMistral, Status: service.StatusActive, Hydrated: true}
			body := `{"model":"claude-sonnet-4-5","max_tokens":64,"system":"system text","messages":[{"role":"user","content":"hello"}],"stop_sequences":["END"],"tools":[{"name":"lookup","input_schema":{"type":"object"}}]}`
			path := "/v1/messages"
			if google {
				path = "/v1beta/models/claude-sonnet-4-5:generateContent"
				body = `{"systemInstruction":{"parts":[{"text":"system text"}]},"contents":[{"parts":[{"text":"hello"}]}],"generationConfig":{"stopSequences":["END"]},"tools":[{"functionDeclarations":[{"name":"lookup","parameters":{"type":"OBJECT"}}]}]}`
			}
			ctx, recorder := requestPlanHTTPContext(path, body, group)
			if google {
				ctx.Params = gin.Params{{Key: "modelAction", Value: "/claude-sonnet-4-5:generateContent"}}
				GeminiProtocolBridge(h.Messages, h.CountTokens, h.Embeddings)(ctx)
			} else {
				h.Messages(ctx)
			}
			require.Equal(t, 200, recorder.Code, recorder.Body.String())
			require.Contains(t, recorder.Body.String(), "hello")
			require.Contains(t, recorder.Body.String(), "claude-sonnet-4-5")
			require.Equal(t, 1, upstream.calls)
			require.Equal(t, "mistral-small-latest", gjson.GetBytes(upstream.body, "model").String())
			require.Equal(t, "END", gjson.GetBytes(upstream.body, "stop.0").String())
			require.Equal(t, "lookup", gjson.GetBytes(upstream.body, "tools.0.function.name").String())
			require.True(t, upstream.plan.State().Finalized)
			require.True(t, upstream.plan.State().UsageClaimed)
			require.Equal(t, 1, len(usage.logs)+len(receipts.receipts))
		})
	}
}

func (u *geminiOpenCodeLifecycleUpstream) Do(req *http.Request, proxy string, id int64, concurrency int) (*http.Response, error) {
	u.path = req.URL.Path
	return u.requestPlanHTTPFixture.Do(req, proxy, id, concurrency)
}

func (u *geminiBridgeLifecycleUpstream) Do(req *http.Request, proxy string, id int64, concurrency int) (*http.Response, error) {
	u.accounts = append(u.accounts, id)
	u.plan = service.GatewayRequestPlanFromContext(req.Context())
	require.Equal(u.t, fmt.Sprintf("vendor-%d", id), req.Header.Get("x-goog-api-key"))
	require.Empty(u.t, req.Header.Get("Authorization"))
	if u.mode == "before" && len(u.accounts) == 1 {
		return nil, &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}
	}
	if u.mode == "policy" {
		return &http.Response{StatusCode: 400, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"code":400,"status":"INVALID_ARGUMENT","message":"invalid tool schema"}}`))}, nil
	}
	if u.mode == "after" {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"partial\"}]}}],\"usageMetadata\":{\"promptTokenCount\":2,\"candidatesTokenCount\":1}}\n\n"))}, nil
	}
	return u.requestPlanHTTPFixture.Do(req, proxy, id, concurrency)
}

func TestGeminiResponsesHandlerFailoverBoundaries(t *testing.T) {
	for _, mode := range []string{"before", "policy", "after"} {
		t.Run(mode, func(t *testing.T) {
			cfg := &config.Config{RunMode: config.RunModeSimple}
			cfg.Gateway.MaxAccountSwitches = 1
			group := &service.Group{ID: 3, Platform: service.PlatformGemini, Status: service.StatusActive, Hydrated: true}
			var accounts []*service.Account
			for _, id := range []int64{7, 8} {
				account := preparedGatewayFixtureAccount(service.Account{ID: id, Platform: service.PlatformGemini, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Credentials: map[string]any{"api_key": fmt.Sprintf("vendor-%d", id)}, AccountGroups: []service.AccountGroup{{AccountID: id, GroupID: 3}}})
				account.Extra[service.UpstreamModelCatalogExtraKey] = service.AccountModelCatalogSnapshot{Complete: true, DiscoveredModels: []string{"gemini-2.5-flash"}, EligibleModels: []service.AccountCatalogModel{{ID: "gemini-2.5-flash", Endpoints: []string{"generate_content"}, EvidenceKind: "fixture"}}}
				accounts = append(accounts, &account)
			}
			h, cleanup := newTestGatewayHandler(t, group, accounts)
			defer cleanup()
			h.cfg = cfg
			usage := &cursorRouteUsageRepo{}
			upstream := &geminiBridgeLifecycleUpstream{requestPlanHTTPFixture: requestPlanHTTPFixture{t: t, gemini: true, stream: mode == "after"}, mode: mode}
			snapshot := service.NewSchedulerSnapshotService(&fakeSchedulerCache{accounts: accounts}, nil, nil, nil, nil)
			h.gatewayService = service.ProvideGatewayService(nil, &fakeGroupRepo{group: group}, usage, nil, nil, nil, nil, nil, cfg, snapshot, nil, service.NewBillingService(cfg, nil), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, service.NewUsageSettlementService(&unlimitedTPMReceiptStub{}, nil, nil))
			h.geminiCompatService = service.NewGeminiMessagesCompatService(nil, nil, nil, nil, nil, nil, upstream, nil, cfg)
			c, rec := requestPlanHTTPContext("/v1/responses", fmt.Sprintf(`{"model":"gemini-2.5-flash","input":"hello","stream":%t}`, mode == "after"), group)
			c.Request.Header.Set("Authorization", "Bearer caller-secret")
			h.Responses(c)
			switch mode {
			case "before":
				require.Equal(t, 200, rec.Code, rec.Body.String())
				require.Len(t, upstream.accounts, 2)
				require.NotEqual(t, upstream.accounts[0], upstream.accounts[1])
				require.Len(t, usage.logs, 1)
			case "policy":
				require.Equal(t, 400, rec.Code, rec.Body.String())
				require.Len(t, upstream.accounts, 1)
				require.Empty(t, usage.logs)
			case "after":
				require.Equal(t, 200, rec.Code, rec.Body.String())
				require.Len(t, upstream.accounts, 1)
				require.Contains(t, rec.Body.String(), "response.failed")
				require.NotContains(t, rec.Body.String(), "response.completed")
				require.Len(t, usage.logs, 1)
			}
		})
	}
}

// This runs the real handler, account scheduler, converter, mock transport and
// single usage owner. The public router/descriptor binding is a sibling change.
func TestGeminiResponsesHandlerLifecycleIntegration(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			cfg := &config.Config{RunMode: config.RunModeSimple}
			group := &service.Group{ID: 3, Platform: service.PlatformGemini, Status: service.StatusActive, Hydrated: true}
			account := preparedGatewayFixtureAccount(service.Account{ID: 7, Platform: service.PlatformGemini, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Credentials: map[string]any{"api_key": "fixture"}, AccountGroups: []service.AccountGroup{{AccountID: 7, GroupID: 3}}})
			account.Extra[service.UpstreamModelCatalogExtraKey] = service.AccountModelCatalogSnapshot{Complete: true, DiscoveredModels: []string{"gemini-2.5-flash"}, EligibleModels: []service.AccountCatalogModel{{ID: "gemini-2.5-flash", Endpoints: []string{"generate_content"}, EvidenceKind: "fixture"}}}
			accounts := []*service.Account{&account}
			h, cleanup := newTestGatewayHandler(t, group, accounts)
			defer cleanup()
			h.cfg = cfg
			usage := &cursorRouteUsageRepo{}
			upstream := &requestPlanHTTPFixture{t: t, gemini: true, stream: stream}
			snapshot := service.NewSchedulerSnapshotService(&fakeSchedulerCache{accounts: accounts}, nil, nil, nil, nil)
			h.gatewayService = service.ProvideGatewayService(nil, &fakeGroupRepo{group: group}, usage, nil, nil, nil, nil, nil, cfg, snapshot, nil, service.NewBillingService(cfg, nil), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, service.NewUsageSettlementService(&unlimitedTPMReceiptStub{}, nil, nil))
			h.geminiCompatService = service.NewGeminiMessagesCompatService(nil, nil, nil, nil, nil, nil, upstream, nil, cfg)
			c, rec := requestPlanHTTPContext("/v1/responses", fmt.Sprintf(`{"model":"gemini-2.5-flash","input":"hello","stream":%t}`, stream), group)
			h.Responses(c)
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			require.Equal(t, 1, upstream.calls)
			require.NotNil(t, upstream.plan)
			require.True(t, upstream.plan.State().Finalized)
			require.True(t, upstream.plan.State().UsageClaimed)
			require.Len(t, usage.logs, 1)
			if stream {
				require.Contains(t, rec.Body.String(), "response.completed")
			} else {
				require.Contains(t, rec.Body.String(), `"object":"response"`)
			}
		})
	}
}

func TestGeminiBridgeExistingOpenAIHandlerLifecycle(t *testing.T) {
	for _, scenario := range []struct{ stream, allowMessages bool }{{false, false}, {true, false}, {false, true}, {true, true}} {
		t.Run(fmt.Sprintf("stream=%t/messages=%t", scenario.stream, scenario.allowMessages), func(t *testing.T) {
			stream := scenario.stream
			cfg := &config.Config{RunMode: config.RunModeSimple}
			account := preparedGatewayFixtureAccount(service.Account{ID: 7, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true, Credentials: map[string]any{"access_token": "fixture"}, AccountGroups: []service.AccountGroup{{AccountID: 7, GroupID: 3}}})
			repo := &openAIWSUsageHandlerAccountRepoStub{account: account}
			usage := &cursorRouteUsageRepo{}
			upstream := &geminiControlsLifecycleUpstream{requestPlanHTTPFixture: requestPlanHTTPFixture{t: t}}
			svc := service.ProvideOpenAIGatewayService(repo, usage, nil, nil, nil, nil, nil, cfg, nil, nil, service.NewBillingService(cfg, nil), nil, nil, upstream, nil, nil, nil, nil, nil, nil, nil, nil, service.NewUsageSettlementService(&unlimitedTPMReceiptStub{}, nil, nil))
			billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
			defer billing.Stop()
			h := NewOpenAIGatewayHandler(svc, service.NewConcurrencyService(&fakeConcurrencyCache{}), billing, service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, cfg)
			group := &service.Group{ID: 3, Platform: service.PlatformOpenAI, Status: service.StatusActive, Hydrated: true}
			if scenario.allowMessages {
				group.AllowMessagesDispatch = true
				group.MessagesDispatchModelConfig = service.OpenAIMessagesDispatchModelConfig{ExactModelMappings: map[string]string{"gpt-5.1": "gpt-5.4-mini"}}
			}
			action := "generateContent"
			if stream {
				action = "streamGenerateContent"
			}
			path := "/v1beta/models/gpt-5.1:" + action + "?alt=sse"
			c, rec := requestPlanHTTPContext(path, `{"systemInstruction":{"parts":[{"text":"Return JSON"}]},"contents":[{"role":"user","parts":[{"text":"hello"}]}],"tools":[{"functionDeclarations":[{"name":"lookup","parameters":{"type":"OBJECT","properties":{"query":{"type":"STRING"}}}}]}],"generationConfig":{"maxOutputTokens":32,"thinkingConfig":{"thinkingLevel":"HIGH"},"responseMimeType":"application/json","responseJsonSchema":{"type":"object","properties":{"answer":{"type":"string"}}}}}`, group)
			c.Params = gin.Params{{Key: "modelAction", Value: "/gpt-5.1:" + action}}
			GeminiProtocolBridge(h.Messages, h.CountTokens, h.Embeddings)(c)
			require.Equal(t, 200, rec.Code, rec.Body.String())
			require.Contains(t, rec.Body.String(), "hello")
			require.Contains(t, rec.Body.String(), "STOP")
			require.Equal(t, 1, upstream.calls)
			// The existing OAuth alias normalization maps gpt-5.1 to gpt-5.4;
			// the Messages-only gpt-5.4-mini override must not replace it.
			require.Equal(t, "gpt-5.4", gjson.GetBytes(upstream.body, "model").String())
			require.Equal(t, "high", gjson.GetBytes(upstream.body, "reasoning.effort").String())
			require.Equal(t, "string", gjson.GetBytes(upstream.body, "text.format.schema.properties.answer.type").String())
			require.Equal(t, "lookup", gjson.GetBytes(upstream.body, "tools.0.name").String())
			require.Contains(t, string(upstream.body), "Return JSON")
			require.NotNil(t, upstream.plan)
			require.Equal(t, "gemini", upstream.plan.Snapshot().Protocol)
			require.True(t, upstream.plan.State().Finalized)
			require.True(t, upstream.plan.State().UsageClaimed)
			require.Len(t, usage.logs, 1)
			for _, control := range []string{`"stopSequences":["END"]`, `"topK":7`} {
				controlled, controlledRec := requestPlanHTTPContext(path, `{"contents":[{"role":"user","parts":[{"text":"hello"}]}],"generationConfig":{`+control+`}}`, group)
				controlled.Params = gin.Params{{Key: "modelAction", Value: "/gpt-5.1:" + action}}
				GeminiProtocolBridge(h.Messages, h.CountTokens, h.Embeddings)(controlled)
				require.Equal(t, 400, controlledRec.Code, controlledRec.Body.String())
				require.Contains(t, controlledRec.Body.String(), "Responses upstream protocol")
				require.Equal(t, 1, upstream.calls)
				require.Len(t, usage.logs, 1)
			}
		})
	}
}

func TestGeminiOpenCodeNativeAndTextHandlerLifecycle(t *testing.T) {
	for _, native := range []bool{true, false} {
		t.Run(fmt.Sprint(native), func(t *testing.T) {
			cfg := &config.Config{RunMode: config.RunModeSimple}
			group := &service.Group{ID: 3, Platform: service.PlatformOpenCodeGo, Status: service.StatusActive, Hydrated: true}
			mode, protocol, model, endpoint := service.AccountModeGo, service.APIProtocolResponses, "gpt-5.1", "responses"
			if native {
				mode, protocol, model, endpoint = service.AccountModeZen, service.APIProtocolAdaptive, "private-google-model", "generate_content"
			}
			account := preparedGatewayFixtureAccount(service.Account{ID: 7, Platform: service.PlatformOpenCodeGo, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Credentials: map[string]any{"api_key": "fixture", "account_mode": mode, "api_protocol": protocol, "model_mapping": map[string]any{"public": model}}, AccountGroups: []service.AccountGroup{{AccountID: 7, GroupID: 3}}})
			account.Extra[service.UpstreamModelCatalogExtraKey] = service.AccountModelCatalogSnapshot{Complete: true, DiscoveredModels: []string{model}, EligibleModels: []service.AccountCatalogModel{{ID: model, Endpoints: []string{endpoint}, EvidenceKind: "fixture"}}}
			repo := &openAIWSUsageHandlerAccountRepoStub{account: account}
			usage := &cursorRouteUsageRepo{}
			upstream := &geminiOpenCodeLifecycleUpstream{requestPlanHTTPFixture: requestPlanHTTPFixture{t: t, gemini: native}}
			receipts := newHunyuanSTTReceiptRepository()
			svc := service.ProvideOpenAIGatewayService(repo, usage, nil, nil, nil, nil, nil, cfg, nil, nil, service.NewBillingService(cfg, nil), nil, nil, upstream, nil, nil, nil, nil, nil, nil, nil, nil, service.NewUsageSettlementService(receipts, nil, nil))
			h, cleanup := newTestGatewayHandler(t, group, []*service.Account{&account})
			defer cleanup()
			h.cfg = cfg
			h.openAIGatewayService = svc
			c, rec := requestPlanHTTPContext("/v1beta/models/public:generateContent", `{"contents":[{"parts":[{"text":"hello"}]}]}`, group)
			c.Params = gin.Params{{Key: "modelAction", Value: "/public:generateContent"}}
			h.OpenCodeGenerateContent(c)
			require.Equal(t, 200, rec.Code, rec.Body.String())
			require.Contains(t, rec.Body.String(), "hello")
			require.Equal(t, 1, upstream.calls)
			if native {
				require.Contains(t, upstream.path, "/models/private-google-model:generateContent")
				require.Len(t, receipts.receipts, 1)
			} else {
				require.Len(t, usage.logs, 1)
			}
		})
	}
}
