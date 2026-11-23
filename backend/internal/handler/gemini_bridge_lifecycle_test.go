//go:build unit

package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// This transport returns fixed protocol fixtures; no provider or socket is used.
type geminiThoughtReplayUpstream struct {
	requestPlanHTTPFixture
	bodies          [][]byte
	omitMessageStop bool
}

func (upstream *geminiThoughtReplayUpstream) Do(request *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	upstream.calls++
	upstream.plan = service.GatewayRequestPlanFromContext(request.Context())
	require.NotNil(upstream.t, upstream.plan)
	require.False(upstream.t, upstream.plan.State().Finalized)
	require.Equal(upstream.t, int64(7), accountID)
	require.Equal(upstream.t, "thought-fixture.example", request.URL.Host)
	require.Equal(upstream.t, "/v1/messages", request.URL.Path)
	body, err := io.ReadAll(request.Body)
	require.NoError(upstream.t, err)
	upstream.bodies = append(upstream.bodies, body)
	model := gjson.GetBytes(body, "model").String()
	events := []string{fmt.Sprintf(`{"type":"message_start","message":{"id":"fixture-message-%d","type":"message","role":"assistant","model":%q,"content":[],"usage":{"input_tokens":5,"output_tokens":0}}}`, upstream.calls, model)}
	stopReason := "end_turn"
	if upstream.calls == 1 {
		stopReason = "tool_use"
		events = append(events,
			`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"fixed fixture thought"}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"opaque-upstream-secret"}}`,
			`{"type":"content_block_stop","index":0}`,
			`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"validation-echo-call","name":"validation_echo","input":{}}}`,
			`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"value\":\"echoed fixture\"}"}}`,
			`{"type":"content_block_stop","index":1}`)
	} else {
		events = append(events,
			`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"echoed fixture"}}`,
			`{"type":"content_block_stop","index":0}`)
	}
	events = append(events, fmt.Sprintf(`{"type":"message_delta","delta":{"stop_reason":%q},"usage":{"output_tokens":3}}`, stopReason))
	if !upstream.omitMessageStop {
		events = append(events, `{"type":"message_stop"}`)
	}
	var stream strings.Builder
	for _, event := range events {
		fmt.Fprintf(&stream, "event: %s\ndata: %s\n\n", gjson.Get(event, "type").String(), event)
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(stream.String()))}, nil
}

func TestGeminiPlaintextThoughtReplayMessagesHandlerLifecycle(t *testing.T) {
	for _, scenario := range []struct{ name, platform, protocol, model string }{
		{"deepseek_adaptive", service.PlatformDeepseek, service.APIProtocolAdaptive, "deepseek-v4-flash"},
		{"unknown_anthropic", service.PlatformKimi, service.APIProtocolAnthropic, "private-custom-model"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			if scenario.name == "unknown_anthropic" {
				require.Equal(t, service.ThinkingProtocolUnknown, service.ResolveThinkingProtocol(scenario.model))
			}
			cfg := &config.Config{RunMode: config.RunModeSimple}
			account := preparedGatewayFixtureAccount(service.Account{ID: 7, Platform: scenario.platform, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Credentials: map[string]any{"api_key": "fixture", "api_protocol": scenario.protocol, "api_base_urls": map[string]any{"anthropic": "https://thought-fixture.example"}, "model_mapping": map[string]any{scenario.model: scenario.model}}, AccountGroups: []service.AccountGroup{{AccountID: 7, GroupID: 3}}})
			if scenario.protocol == service.APIProtocolAnthropic {
				account.Credentials["base_url"] = "https://thought-fixture.example"
			}
			repo := &openAIWSUsageHandlerAccountRepoStub{account: account}
			usage := &cursorRouteUsageRepo{}
			receipts := newHunyuanSTTReceiptRepository()
			upstream := &geminiThoughtReplayUpstream{requestPlanHTTPFixture: requestPlanHTTPFixture{t: t}}
			svc := service.ProvideOpenAIGatewayService(repo, usage, nil, nil, nil, nil, nil, cfg, nil, nil, service.NewBillingService(cfg, nil), nil, nil, upstream, nil, nil, nil, nil, nil, nil, nil, nil, service.NewUsageSettlementService(receipts, nil, nil))
			billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
			defer billing.Stop()
			handler := NewOpenAIGatewayHandler(svc, service.NewConcurrencyService(&fakeConcurrencyCache{}), billing, service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, cfg)
			group := &service.Group{ID: 3, Platform: scenario.platform, Status: service.StatusActive, Hydrated: true}
			action := "/" + scenario.model + ":streamGenerateContent"
			invoke := func(body string, wantStatus int) string {
				ctx, recorder := requestPlanHTTPContext("/v1beta/models"+action+"?alt=sse", body, group)
				ctx.Params = gin.Params{{Key: "modelAction", Value: action}}
				GeminiProtocolBridge(handler.Messages, handler.CountTokens, handler.Embeddings)(ctx)
				require.Equal(t, wantStatus, recorder.Code, recorder.Body.String())
				if wantStatus == http.StatusOK {
					require.Contains(t, recorder.Header().Get("Content-Type"), "text/event-stream")
				}
				return recorder.Body.String()
			}
			// Invalid replay must fail before the real handler acquires a usage owner.
			for _, invalid := range []string{
				`{"role":"model","parts":[{"thought":true,"text":" \n"}]}`,
				`{"role":"user","parts":[{"thought":true,"text":"fixture thought"}]}`,
				`{"role":"model","parts":[{"thoughtSignature":"opaque-only"}]}`,
			} {
				rejected := invoke(`{"contents":[`+invalid+`]}`, http.StatusBadRequest)
				require.Equal(t, "INVALID_ARGUMENT", gjson.Get(rejected, "error.status").String())
				require.Zero(t, upstream.calls)
				require.Zero(t, len(usage.logs)+len(receipts.receipts))
			}
			request := map[string]any{
				"contents":         []any{map[string]any{"role": "user", "parts": []any{map[string]any{"text": "Echo the fixture value"}}}},
				"tools":            []any{map[string]any{"functionDeclarations": []any{map[string]any{"name": "validation_echo", "parameters": map[string]any{"type": "OBJECT", "properties": map[string]any{"value": map[string]any{"type": "STRING"}}, "required": []string{"value"}}}}}},
				"generationConfig": map[string]any{"maxOutputTokens": 2048, "thinkingConfig": map[string]any{"includeThoughts": true}},
				"toolConfig":       map[string]any{"functionCallingConfig": map[string]any{"mode": "AUTO"}},
			}
			var firstPlan *service.GatewayRequestPlan
			for turn := 0; turn < 2; turn++ {
				body, err := json.Marshal(request)
				require.NoError(t, err)
				reply := invoke(string(body), http.StatusOK)
				require.NotContains(t, reply, `"error"`)
				require.NotContains(t, reply, "thoughtSignature")
				require.NotContains(t, reply, "opaque-upstream-secret")
				require.Equal(t, turn+1, upstream.calls)
				require.True(t, upstream.plan.State().Finalized)
				require.True(t, upstream.plan.State().UsageClaimed)
				require.Equal(t, turn+1, len(usage.logs)+len(receipts.receipts))
				require.False(t, gjson.GetBytes(upstream.bodies[turn], "thinking").Exists())
				var parts []any
				finishCount := 0
				for _, line := range strings.Split(reply, "\n") {
					if !strings.HasPrefix(line, "data: ") {
						continue
					}
					payload := strings.TrimPrefix(line, "data: ")
					require.True(t, gjson.Valid(payload), payload)
					if finish := gjson.Get(payload, "candidates.0.finishReason"); finish.Exists() {
						require.Equal(t, "STOP", finish.String())
						finishCount++
					}
					for _, part := range gjson.Get(payload, "candidates.0.content.parts").Array() {
						parts = append(parts, part.Value())
					}
				}
				require.Equal(t, 1, finishCount)
				if turn == 0 {
					firstPlan = upstream.plan
					require.Len(t, parts, 2)
					require.Equal(t, map[string]any{"thought": true, "text": "fixed fixture thought"}, parts[0])
					callPart, ok := parts[1].(map[string]any)
					require.True(t, ok)
					call, ok := callPart["functionCall"].(map[string]any)
					require.True(t, ok)
					require.Equal(t, "validation_echo", call["name"])
					require.Equal(t, "validation-echo-call", call["id"])
					// Execute validation_echo against the actual streamed arguments.
					arguments, ok := call["args"].(map[string]any)
					require.True(t, ok)
					result := map[string]any{"value": arguments["value"], "ok": true}
					require.Equal(t, "echoed fixture", result["value"])
					contents, ok := request["contents"].([]any)
					require.True(t, ok)
					request["contents"] = append(contents, map[string]any{"role": "model", "parts": parts}, map[string]any{"role": "user", "parts": []any{map[string]any{"functionResponse": map[string]any{"id": call["id"], "name": call["name"], "response": result}}}})
					request["toolConfig"] = map[string]any{"functionCallingConfig": map[string]any{"mode": "NONE"}}
					require.Equal(t, "auto", gjson.GetBytes(upstream.bodies[turn], "tool_choice.type").String())
				} else {
					require.NotSame(t, firstPlan, upstream.plan)
					require.Equal(t, []any{map[string]any{"text": "echoed fixture"}}, parts)
					sent := upstream.bodies[turn]
					require.JSONEq(t, `[{"type":"thinking","thinking":"fixed fixture thought"},{"type":"tool_use","id":"validation-echo-call","name":"validation_echo","input":{"value":"echoed fixture"}}]`, gjson.GetBytes(sent, "messages.1.content").Raw)
					require.Equal(t, "validation-echo-call", gjson.GetBytes(sent, "messages.2.content.0.tool_use_id").String())
					require.JSONEq(t, `{"value":"echoed fixture","ok":true}`, gjson.GetBytes(sent, "messages.2.content.0.content").String())
					require.Equal(t, "none", gjson.GetBytes(sent, "tool_choice.type").String())
					require.NotContains(t, string(sent), "signature")
					require.NotContains(t, string(sent), "thoughtSignature")
					require.NotContains(t, string(sent), "opaque-")
				}
			}
			// A stop reason without message_stop must not complete the Google stream.
			upstream.omitMessageStop = true
			body, err := json.Marshal(request)
			require.NoError(t, err)
			truncated := invoke(string(body), http.StatusOK)
			require.Contains(t, truncated, `"error"`)
			require.NotContains(t, truncated, `"finishReason":"STOP"`)
		})
	}
}

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
			billing := service.NewBillingService(cfg, nil)
			resolver := service.NewModelPricingResolver(nil, billing)
			if !native {
				input, output := 0.000001, 0.000002
				group.ModelPricing = []service.ChannelModelPricing{{
					Platform: service.PlatformOpenCodeGo, Models: []string{model},
					BillingMode: service.BillingModeToken, InputPrice: &input, OutputPrice: &output,
				}}
			}
			svc := service.ProvideOpenAIGatewayService(repo, usage, nil, nil, nil, nil, nil, cfg, nil, nil, billing,
				nil, nil, upstream, nil, nil, nil, resolver, nil, nil, nil, nil, service.NewUsageSettlementService(receipts, nil, nil))
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
