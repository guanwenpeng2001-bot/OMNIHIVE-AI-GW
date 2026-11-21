//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type compatibleLoopback struct {
	client      *http.Client
	proxy       string
	id          int64
	concurrency int
}

func (u *compatibleLoopback) Do(r *http.Request, p string, id int64, c int) (*http.Response, error) {
	u.proxy = p
	u.id = id
	u.concurrency = c
	return u.client.Do(r)
}
func (u *compatibleLoopback) DoWithTLS(r *http.Request, p string, id int64, c int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(r, p, id, c)
}
func compatibleFixture(protocol string, bridges []string) domain.CompatibleProvider {
	return domain.CompatibleProvider{ID: "fixture-cloud", Label: "Fixture", LabelZh: "Fixture", BaseSource: "preset", DefaultMode: "payg", EvidenceURL: "https://docs.example.com", EvidenceObserved: "2026-10-03", Modes: []domain.CompatibleMode{{Mode: "payg", DefaultProtocol: protocol, Endpoints: []domain.CompatibleEndpoint{{Protocol: protocol, BaseURL: "https://api.example.com/prefix/v1", Auth: "bearer"}}, Bridges: bridges, Discovery: "openai_models", DiscoveryProtocol: protocol, CapabilityProfile: "fixture-cloud-payg"}}}
}
func compatibleAccount(base string) *Account {
	return &Account{ID: 731, Status: StatusActive, Schedulable: true, Platform: "fixture-cloud", Type: AccountTypeAPIKey, Concurrency: 3, Credentials: map[string]any{"api_key": "private-key", "base_url": base, "model_mapping": map[string]any{"alias": "deployment-one"}}}
}

const compatibleChatJSON = `{"id":"c1","object":"chat.completion","model":"deployment-one","choices":[{"index":0,"message":{"role":"assistant","content":"hello","tool_calls":[{"id":"call1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":9,"completion_tokens":4,"total_tokens":13}}`
const compatibleMessageJSON = `{"id":"m1","type":"message","role":"assistant","model":"deployment-one","content":[{"type":"text","text":"hello"},{"type":"tool_use","id":"call1","name":"lookup","input":{}}],"stop_reason":"tool_use","usage":{"input_tokens":9,"output_tokens":4}}`
const compatibleResponseJSON = `{"id":"r1","object":"response","status":"completed","model":"deployment-one","output":[{"id":"msg1","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"hello","annotations":[]}]},{"id":"fc1","type":"function_call","call_id":"call1","name":"lookup","arguments":"{}","status":"completed"}],"usage":{"input_tokens":9,"output_tokens":4,"total_tokens":13}}`

func compatibleSSE(protocol string) string {
	switch protocol {
	case APIProtocolChatCompletions:
		return "data: " + `{"id":"c1","object":"chat.completion.chunk","model":"deployment-one","choices":[{"index":0,"delta":{"role":"assistant","content":"hello","tool_calls":[{"index":0,"id":"call1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}` + "\n\ndata: " + `{"id":"c1","object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":9,"completion_tokens":4,"total_tokens":13}}` + "\n\ndata: [DONE]\n\n"
	case APIProtocolAnthropic:
		return "event: message_start\ndata: " + `{"type":"message_start","message":{"id":"m1","type":"message","role":"assistant","model":"deployment-one","content":[],"usage":{"input_tokens":9,"output_tokens":0}}}` + "\n\nevent: content_block_start\ndata: " + `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}` + "\n\nevent: content_block_delta\ndata: " + `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}` + "\n\nevent: content_block_stop\ndata: " + `{"type":"content_block_stop","index":0}` + "\n\nevent: content_block_start\ndata: " + `{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"call1","name":"lookup","input":{}}}` + "\n\nevent: content_block_delta\ndata: " + `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{}"}}` + "\n\nevent: content_block_stop\ndata: " + `{"type":"content_block_stop","index":1}` + "\n\nevent: message_delta\ndata: " + `{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":4}}` + "\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	default:
		return "event: response.output_text.delta\ndata: " + `{"type":"response.output_text.delta","output_index":0,"content_index":0,"delta":"hello"}` + "\n\nevent: response.completed\ndata: {\"type\":\"response.completed\",\"response\":" + compatibleResponseJSON + "}\n\n"
	}
}
func TestCompatibleRuntimeNativeAndBridges(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, protocol := range []string{APIProtocolChatCompletions, APIProtocolAnthropic, APIProtocolResponses} {
		target := map[string]string{APIProtocolChatCompletions: "chat", APIProtocolAnthropic: "messages", APIProtocolResponses: "responses"}[protocol]
		bridges := []string{}
		for _, source := range []string{"chat", "messages", "responses", "gemini"} {
			if source != target {
				bridges = append(bridges, source+"_to_"+target)
			}
		}
		t.Run(protocol, func(t *testing.T) {
			restore := domain.SetCompatibleProvidersForTest([]domain.CompatibleProvider{compatibleFixture(protocol, bridges)})
			defer restore()
			for _, ingress := range []string{"chat", "messages", "responses", "gemini"} {
				for _, stream := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/stream=%v", ingress, stream), func(t *testing.T) {
						var sent []byte
						var path string
						var auth http.Header
						server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
							sent, _ = io.ReadAll(r.Body)
							path = r.URL.Path
							auth = r.Header.Clone()
							if gjson.GetBytes(sent, "stream").Bool() {
								w.Header().Set("Content-Type", "text/event-stream")
								fmt.Fprint(w, compatibleSSE(protocol))
							} else {
								w.Header().Set("Content-Type", "application/json")
								fmt.Fprint(w, map[string]string{APIProtocolChatCompletions: compatibleChatJSON, APIProtocolAnthropic: compatibleMessageJSON, APIProtocolResponses: compatibleResponseJSON}[protocol])
							}
						}))
						defer server.Close()
						account := compatibleAccount(server.URL + "/prefix/v1")
						account.Credentials["model_mapping"] = map[string]any{"private/gpt-5.4-high": "deployment-one"}
						transport := &compatibleLoopback{client: server.Client()}
						gateway := &OpenAIGatewayService{cfg: loopbackHTTPTestConfig(), httpUpstream: transport}
						body := []byte(fmt.Sprintf(`{"model":"private/gpt-5.4-high","stream":%v,"messages":[{"role":"assistant","content":null,"tool_calls":[{"id":"previous","type":"function","function":{"name":"lookup","arguments":"{}"}}]},{"role":"tool","tool_call_id":"previous","content":"prior result"},{"role":"user","content":"hello"}],"reasoning_effort":"high","tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}]}`, stream))
						if ingress == "messages" || ingress == "gemini" {
							body = []byte(fmt.Sprintf(`{"model":"private/gpt-5.4-high","stream":%v,"max_tokens":32,"messages":[{"role":"assistant","content":[{"type":"tool_use","id":"previous","name":"lookup","input":{}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"previous","content":"prior result"},{"type":"text","text":"hello"}]}],"output_config":{"effort":"high"},"tools":[{"name":"lookup","input_schema":{"type":"object"}}]}`, stream))
						}
						if ingress == "responses" {
							body = []byte(fmt.Sprintf(`{"model":"private/gpt-5.4-high","stream":%v,"reasoning":{"effort":"high"},"input":[{"type":"function_call","call_id":"previous","name":"lookup","arguments":"{}"},{"type":"function_call_output","call_id":"previous","output":"prior result"},{"role":"user","content":"hello"}],"tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}]}`, stream))
						}
						ctx := WithGatewayRequestPlan(context.Background(), NewGatewayRequestPlan(GatewayRequestSpec{Model: "private/gpt-5.4-high", Protocol: map[string]string{"chat": "chat_completions", "messages": "anthropic", "responses": "responses", "gemini": "gemini"}[ingress], Action: map[string]string{"gemini": "generate_content", "chat": "chat", "messages": "messages", "responses": "responses"}[ingress]}))
						installUnlimitedTPMFixture(GatewayRequestPlanFromContext(ctx))
						if ingress == "gemini" {
							ctx = context.WithValue(ctx, gatewayActionRequirementKey{}, "generate_content")
							var err error
							body, err = GeminiToMessages([]byte(`{"contents":[{"role":"model","parts":[{"functionCall":{"id":"previous","name":"lookup","args":{}}}]},{"role":"user","parts":[{"functionResponse":{"id":"previous","name":"lookup","response":{"result":"prior result"}}},{"text":"hello"}]}],"tools":[{"functionDeclarations":[{"name":"lookup","parameters":{"type":"OBJECT"}}]}]}`), "private/gpt-5.4-high", stream)
							require.NoError(t, err)
						}
						rec := httptest.NewRecorder()
						c, _ := gin.CreateTestContext(rec)
						c.Request = httptest.NewRequest("POST", "/v1/"+ingress, bytes.NewReader(body)).WithContext(ctx)
						var geminiWriter *GeminiProtocolWriter
						if ingress == "gemini" {
							geminiWriter = NewGeminiProtocolWriter(c.Writer, "private/gpt-5.4-high", "generate_content", stream, 0)
							c.Writer = geminiWriter
						}
						var result *OpenAIForwardResult
						var err error
						switch ingress {
						case "chat":
							result, err = gateway.ForwardAsChatCompletions(ctx, c, account, body, "", "")
						case "responses":
							result, err = gateway.Forward(ctx, c, account, body)
						default:
							result, err = gateway.ForwardAsAnthropic(ctx, c, account, body, "", "")
						}
						require.NoError(t, err, rec.Body.String())
						if geminiWriter != nil {
							require.NoError(t, geminiWriter.Finish())
						}
						require.NotNil(t, result)
						require.Equal(t, "/prefix/v1/"+map[string]string{APIProtocolChatCompletions: "chat/completions", APIProtocolAnthropic: "messages", APIProtocolResponses: "responses"}[protocol], path)
						require.Equal(t, "Bearer private-key", auth.Get("Authorization"))
						require.Equal(t, "deployment-one", gjson.GetBytes(sent, "model").String())
						require.Contains(t, string(sent), "lookup")
						require.Contains(t, string(sent), "prior result")
						require.Contains(t, rec.Body.String(), "hello")
						require.Equal(t, 9, result.Usage.InputTokens)
						require.Equal(t, 4, result.Usage.OutputTokens)
						require.Equal(t, int64(731), transport.id)
						require.Equal(t, 3, transport.concurrency)
						require.Equal(t, "fixture-cloud", account.Platform)
					})
				}
			}
		})
	}
}
func TestCompatibleModeBaseDiscoveryAndValidation(t *testing.T) {
	row := compatibleFixture(APIProtocolChatCompletions, []string{"messages_to_chat"})
	row.Modes[0].Endpoints = append(row.Modes[0].Endpoints, domain.CompatibleEndpoint{Protocol: APIProtocolAnthropic, BaseURL: "https://messages.example.com/anthropic/v1", Auth: "x_api_key"})
	row.Modes = append(row.Modes, domain.CompatibleMode{Mode: "coding", DefaultProtocol: APIProtocolChatCompletions, Endpoints: []domain.CompatibleEndpoint{{Protocol: APIProtocolChatCompletions, BaseURL: "https://coding.example.com/v1", Auth: "api_key_header"}}, Discovery: "none", CapabilityProfile: "fixture-cloud-coding"})
	restore := domain.SetCompatibleProvidersForTest([]domain.CompatibleProvider{row})
	defer restore()
	a := compatibleAccount("")
	a.Credentials["api_base_urls"] = map[string]any{APIProtocolChatCompletions: "https://custom.example.com/route"}
	require.Equal(t, "https://custom.example.com/route", a.compatibleProtocolBase(APIProtocolChatCompletions))
	require.Equal(t, "https://messages.example.com/anthropic/v1", a.compatibleProtocolBase(APIProtocolAnthropic))
	require.Equal(t, "custom:fixture-cloud", resolveCapabilityProfile(a))
	req, err := buildCompatibleModelsRequest(context.Background(), a, func(s string) (string, error) { return s, nil })
	require.NoError(t, err)
	require.Equal(t, "https://custom.example.com/route/models", req.URL.String())
	require.Equal(t, "Bearer private-key", req.Header.Get("Authorization"))
	a.Credentials["account_mode"] = "coding"
	_, err = buildCompatibleModelsRequest(context.Background(), a, func(s string) (string, error) { t.Fatal("must not resolve discovery"); return s, nil })
	require.ErrorContains(t, err, "declare models manually")
	_, err = bindCompatibleRequest(context.Background(), a, "messages", "deployment-one")
	require.Error(t, err)
	a.Credentials["account_mode"] = "typo"
	require.Error(t, validateExpandedPlatformAccount(a))
}
func TestCompatibleAzureResource(t *testing.T) {
	row := compatibleFixture(APIProtocolChatCompletions, nil)
	row.BaseSource = "account"
	row.Modes[0].Endpoints[0] = domain.CompatibleEndpoint{Protocol: APIProtocolChatCompletions, Path: "/openai/v1", Auth: "api_key_header"}
	row.Modes[0].Discovery = "none"
	row.Modes[0].DiscoveryProtocol = ""
	restore := domain.SetCompatibleProvidersForTest([]domain.CompatibleProvider{row})
	defer restore()
	a := compatibleAccount("")
	require.Error(t, validateCompatibleAccount(a))
	a.Credentials["base_url"] = "https://resource.example.com"
	require.NoError(t, validateCompatibleAccount(a))
	require.Equal(t, "https://resource.example.com/openai/v1", a.compatibleProtocolBase(APIProtocolChatCompletions))
	a.Credentials["base_url"] = "https://resource.example.com/openai/v1"
	require.Equal(t, "https://resource.example.com/openai/v1", a.compatibleProtocolBase(APIProtocolChatCompletions))
	req, _ := http.NewRequest("POST", "https://resource.example.com/openai/v1/chat/completions", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer wrong")
	require.NoError(t, applyCompatibleRequestAuth(a, req))
	require.Equal(t, "private-key", req.Header.Get("api-key"))
	require.Empty(t, req.Header.Get("Authorization"))
}

func TestCompatibleFailureUsageAndCancellation(t *testing.T) {
	restore := domain.SetCompatibleProvidersForTest([]domain.CompatibleProvider{compatibleFixture(APIProtocolChatCompletions, []string{"messages_to_chat", "responses_to_chat", "gemini_to_chat"})})
	defer restore()
	for _, scenario := range []struct {
		name     string
		status   int
		response string
		stream   bool
	}{
		{"malformed JSON", 200, `{"id":`, false},
		{"upstream error", 400, `{"error":{"message":"fixture invalid request","type":"invalid_request_error"}}`, false},
		{"broken stream", 200, "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n", true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if scenario.stream {
					w.Header().Set("Content-Type", "text/event-stream")
				} else {
					w.Header().Set("Content-Type", "application/json")
				}
				w.WriteHeader(scenario.status)
				fmt.Fprint(w, scenario.response)
			}))
			defer server.Close()
			gateway := &OpenAIGatewayService{cfg: loopbackHTTPTestConfig(), httpUpstream: &compatibleLoopback{client: server.Client()}}
			body := []byte(fmt.Sprintf(`{"model":"alias","stream":%v,"messages":[{"role":"user","content":"hi"}]}`, scenario.stream))
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
			_, err := gateway.ForwardAsChatCompletions(context.Background(), c, compatibleAccount(server.URL+"/v1"), body, "", "")
			require.Error(t, err)
			require.NotContains(t, rec.Body.String(), "[DONE]")
		})
	}
	t.Run("bounded request cancellation", func(t *testing.T) {
		arrived := make(chan struct{})
		cancelled := make(chan struct{})
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			close(arrived)
			select {
			case <-r.Context().Done():
				close(cancelled)
			case <-time.After(4 * time.Second):
			}
		}))
		defer server.Close()
		gateway := &OpenAIGatewayService{cfg: loopbackHTTPTestConfig(), httpUpstream: &compatibleLoopback{client: server.Client()}}
		ctx, cancel := context.WithCancel(context.WithValue(context.Background(), boundedStreamContextKey{}, true))
		defer cancel()
		body := []byte(`{"model":"alias","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body)).WithContext(ctx)
		done := make(chan error, 1)
		go func() {
			_, err := gateway.ForwardAsChatCompletions(ctx, c, compatibleAccount(server.URL+"/v1"), body, "", "")
			done <- err
		}()
		select {
		case <-arrived:
		case <-time.After(2 * time.Second):
			t.Fatal("request did not reach loopback")
		}
		cancel()
		select {
		case err := <-done:
			require.Error(t, err)
		case <-time.After(2 * time.Second):
			t.Fatal("cancelled request did not return")
		}
		select {
		case <-cancelled:
		case <-time.After(2 * time.Second):
			t.Fatal("upstream context did not cancel")
		}
	})
}
func TestCompatibleModelRulesAndModeScheduling(t *testing.T) {
	row := compatibleFixture(APIProtocolChatCompletions, []string{"chat_to_messages"})
	row.Modes[0].Endpoints = append(row.Modes[0].Endpoints, domain.CompatibleEndpoint{Protocol: APIProtocolAnthropic, BaseURL: "https://api.example.com/messages/v1", Auth: "x_api_key"})
	row.Modes = append(row.Modes, domain.CompatibleMode{Mode: "coding", DefaultProtocol: APIProtocolChatCompletions, Endpoints: []domain.CompatibleEndpoint{{Protocol: APIProtocolChatCompletions, BaseURL: "https://coding.example.com/v1", Auth: "bearer"}}, Discovery: "none", CapabilityProfile: "fixture-coding"})
	restore := domain.SetCompatibleProvidersForTest([]domain.CompatibleProvider{row})
	defer restore()
	a := compatibleAccount("")
	a.Credentials["protocol_rules"] = []any{map[string]any{"pattern": "deployment-*", "protocol": APIProtocolAnthropic}}
	bound, err := bindCompatibleRequest(context.Background(), a, "chat", "deployment-one")
	require.NoError(t, err)
	require.Equal(t, APIProtocolAnthropic, bound.GetAPIProtocol())
	require.Equal(t, APIProtocolAdaptive, a.GetAPIProtocol())
	ctx := WithGatewayRequestPlan(context.Background(), NewGatewayRequestPlan(GatewayRequestSpec{Protocol: "chat_completions", Action: "chat", Model: "alias"}))
	require.True(t, (&OpenAIGatewayService{}).supportsOpenAIAccountModel(ctx, a, "alias", OpenAIEndpointCapabilityChatCompletions))
	require.NoError(t, validateAccountForwardModel(ctx, nil, bound, "deployment-one", "messages"))
	require.True(t, errors.Is(validateAccountForwardModel(ctx, nil, bound, "not-selected", "messages"), ErrAccountModelUnsupported))
	a.Credentials["account_mode"] = "coding"
	require.Error(t, validateCompatibleAccount(a))
	require.False(t, AccountSupportsResolvedModel(ctx, a, "alias", AccountModelBindingOptions{Endpoint: "chat_completions"}))
	a.Credentials["protocol_rules"] = []any{}
	ctx = context.WithValue(ctx, gatewayActionRequirementKey{}, "messages")
	require.False(t, accountImplementsGatewayAction(ctx, a))
	a.Credentials["api_protocol"] = "invalid"
	require.Error(t, validateCompatibleAccount(a))
}
func TestCompatibleNativeOpaqueResponsesAndDiscoveryHTTP(t *testing.T) {
	row := compatibleFixture(APIProtocolResponses, nil)
	row.Modes[0].Endpoints[0].Auth = "api_key_header"
	restore := domain.SetCompatibleProvidersForTest([]domain.CompatibleProvider{row})
	defer restore()
	var paths []string
	var sent []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		require.Equal(t, "private-key", r.Header.Get("api-key"))
		require.Empty(t, r.Header.Get("Authorization"))
		sent, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "GET" {
			fmt.Fprint(w, `{"data":[{"id":"tenant-deployment"}]}`)
		} else {
			fmt.Fprint(w, compatibleResponseJSON)
		}
	}))
	defer server.Close()
	a := compatibleAccount(server.URL + "/openai/v1")
	gateway := &OpenAIGatewayService{cfg: loopbackHTTPTestConfig(), httpUpstream: &compatibleLoopback{client: server.Client()}}
	body := []byte(`{"model":"alias","input":[{"type":"reasoning","encrypted_content":"opaque-history"}],"previous_response_id":"tenant-owned-response","store":true,"future_vendor_field":{"preserve":true}}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest("POST", "/v1/responses", bytes.NewReader(body))
	_, err := gateway.Forward(context.Background(), c, a, body)
	require.NoError(t, err)
	require.Equal(t, "tenant-owned-response", gjson.GetBytes(sent, "previous_response_id").String())
	require.True(t, gjson.GetBytes(sent, "store").Bool())
	require.True(t, gjson.GetBytes(sent, "future_vendor_field.preserve").Bool())
	require.Contains(t, string(sent), "opaque-history")
	tester := &AccountTestService{cfg: loopbackHTTPTestConfig(), httpUpstream: &compatibleLoopback{client: server.Client()}}
	req, err := tester.buildUpstreamModelsRequest(context.Background(), a)
	require.NoError(t, err)
	resp, err := server.Client().Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, []string{"/openai/v1/responses", "/openai/v1/models"}, paths)
}

func TestCompatibleAccountProbeUsesSelectedNativeEndpoint(t *testing.T) {
	for _, protocol := range []string{APIProtocolChatCompletions, APIProtocolAnthropic, APIProtocolResponses} {
		t.Run(protocol, func(t *testing.T) {
			row := compatibleFixture(protocol, nil)
			row.Modes[0].Endpoints[0].Auth = map[string]string{APIProtocolChatCompletions: "api_key_header", APIProtocolAnthropic: "x_api_key", APIProtocolResponses: "bearer"}[protocol]
			restore := domain.SetCompatibleProvidersForTest([]domain.CompatibleProvider{row})
			defer restore()
			var path string
			var auth http.Header
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				path = r.URL.Path
				auth = r.Header.Clone()
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, compatibleSSE(protocol))
			}))
			defer server.Close()
			a := compatibleAccount(server.URL + "/prefix/v1")
			proxyID := int64(81)
			a.ProxyID = &proxyID
			a.Proxy = &Proxy{Protocol: "http", Host: "proxy.example", Port: 8080}
			transport := &compatibleLoopback{client: server.Client()}
			svc := &AccountTestService{cfg: loopbackHTTPTestConfig(), httpUpstream: transport}
			var complete bool
			probe := &accountProbeContext{ctx: context.Background(), emit: func(event TestEvent) {
				if event.Type == "test_complete" {
					complete = event.Success
				}
			}}
			require.NoError(t, svc.testCompatibleAccountConnection(probe, a, "alias", "hello"))
			require.True(t, complete)
			require.Equal(t, "/prefix/v1/"+map[string]string{APIProtocolChatCompletions: "chat/completions", APIProtocolAnthropic: "messages", APIProtocolResponses: "responses"}[protocol], path)
			require.Equal(t, "http://proxy.example:8080", transport.proxy)
			require.Equal(t, map[string]string{APIProtocolChatCompletions: "private-key", APIProtocolAnthropic: "private-key", APIProtocolResponses: "Bearer private-key"}[protocol], auth.Get(map[string]string{APIProtocolChatCompletions: "api-key", APIProtocolAnthropic: "x-api-key", APIProtocolResponses: "Authorization"}[protocol]))
			require.False(t, a.IsCNProvider())
			descriptor, _ := domain.LookupPlatform(a.Platform)
			require.False(t, descriptor.CNProvider)
			require.False(t, descriptor.SchedulingThreshold)
		})
	}
}

func TestCompatibleBulkCredentialPatchValidation(t *testing.T) {
	restore := domain.SetCompatibleProvidersForTest([]domain.CompatibleProvider{compatibleFixture(APIProtocolChatCompletions, nil)})
	defer restore()
	a := compatibleAccount("https://custom.example.com/v1")
	repo := &accountRepoStubForBulkUpdate{getByIDsAccounts: []*Account{a}}
	svc := &adminServiceImpl{accountRepo: repo}
	_, err := svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{AccountIDs: []int64{a.ID}, Credentials: map[string]any{"account_mode": "coding"}})
	require.ErrorContains(t, err, "mode")
	require.Zero(t, repo.bulkUpdateCalls)
	require.Empty(t, a.GetCredential("account_mode"))
	_, err = svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{AccountIDs: []int64{a.ID}, Credentials: map[string]any{"api_protocol": APIProtocolChatCompletions}})
	require.NoError(t, err)
	require.Equal(t, 1, repo.bulkUpdateCalls)
	require.Equal(t, "https://custom.example.com/v1", a.GetCredential("base_url"))
	require.Equal(t, "private-key", a.GetCredential("api_key"))
}
func TestCompatibleTwoRootsActualForwarding(t *testing.T) {
	var messagePath, chatPath string
	var messageAuth, chatAuth http.Header
	messages := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		messagePath = r.URL.Path
		messageAuth = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, compatibleMessageJSON)
	}))
	defer messages.Close()
	chat := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chatPath = r.URL.Path
		chatAuth = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, compatibleChatJSON)
	}))
	defer chat.Close()
	row := compatibleFixture(APIProtocolChatCompletions, []string{"messages_to_chat"})
	row.Modes[0].Endpoints = append(row.Modes[0].Endpoints, domain.CompatibleEndpoint{Protocol: APIProtocolAnthropic, BaseURL: messages.URL + "/native/v1", Auth: "x_api_key"})
	restore := domain.SetCompatibleProvidersForTest([]domain.CompatibleProvider{row})
	defer restore()
	a := compatibleAccount("")
	a.Credentials["api_base_urls"] = map[string]any{APIProtocolChatCompletions: chat.URL + "/custom"}
	transport := &compatibleLoopback{client: messages.Client()}
	svc := &OpenAIGatewayService{cfg: loopbackHTTPTestConfig(), httpUpstream: transport}
	body := []byte(`{"model":"alias","messages":[{"role":"user","content":"hello"}],"max_tokens":12}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	_, err := svc.ForwardAsAnthropic(context.Background(), c, a, body, "", "")
	require.NoError(t, err)
	require.Equal(t, "/native/v1/messages", messagePath)
	require.Equal(t, "private-key", messageAuth.Get("x-api-key"))
	require.Empty(t, messageAuth.Get("Authorization"))
	a.Credentials["api_protocol"] = APIProtocolChatCompletions
	rec = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	_, err = svc.ForwardAsAnthropic(context.Background(), c, a, body, "", "")
	require.NoError(t, err)
	require.Equal(t, "/custom/chat/completions", chatPath)
	require.Equal(t, "Bearer private-key", chatAuth.Get("Authorization"))
	require.Empty(t, chatAuth.Get("x-api-key"))
}

func TestCompatibleUsagePresenceAcrossNativeProtocols(t *testing.T) {
	stripUsage := func(raw []byte) []byte {
		var data map[string]any
		if json.Unmarshal(raw, &data) != nil {
			return raw
		}
		delete(data, "usage")
		for _, key := range []string{"message", "response"} {
			if obj, ok := data[key].(map[string]any); ok {
				delete(obj, "usage")
			}
		}
		out, _ := json.Marshal(data)
		return out
	}
	for _, protocol := range []string{APIProtocolChatCompletions, APIProtocolAnthropic, APIProtocolResponses} {
		t.Run(protocol, func(t *testing.T) {
			restore := domain.SetCompatibleProvidersForTest([]domain.CompatibleProvider{compatibleFixture(protocol, nil)})
			defer restore()
			require.Empty(t, (&Group{Platform: "fixture-cloud"}).ResolveMessagesDispatchModel("claude-opus-4-6"))
			for _, stream := range []bool{false, true} {
				t.Run(fmt.Sprint(stream), func(t *testing.T) {
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if stream {
							w.Header().Set("Content-Type", "text/event-stream")
							for _, line := range strings.Split(compatibleSSE(protocol), "\n") {
								if strings.HasPrefix(line, "data:") {
									line = "data: " + string(stripUsage([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:")))))
								}
								fmt.Fprintln(w, line)
							}
						} else {
							w.Header().Set("Content-Type", "application/json")
							raw := map[string]string{APIProtocolChatCompletions: compatibleChatJSON, APIProtocolAnthropic: compatibleMessageJSON, APIProtocolResponses: compatibleResponseJSON}[protocol]
							w.Write(stripUsage([]byte(raw)))
						}
					}))
					defer server.Close()
					a := compatibleAccount(server.URL + "/v1")
					svc := &OpenAIGatewayService{cfg: loopbackHTTPTestConfig(), httpUpstream: &compatibleLoopback{client: server.Client()}}
					body := []byte(fmt.Sprintf(`{"model":"alias","stream":%v,"messages":[{"role":"user","content":"hello"}],"input":"hello","max_tokens":32}`, stream))
					rec := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(rec)
					c.Request = httptest.NewRequest("POST", "/v1/responses", bytes.NewReader(body))
					var err error
					var result *OpenAIForwardResult
					switch protocol {
					case APIProtocolChatCompletions:
						result, err = svc.ForwardAsChatCompletions(context.Background(), c, a, body, "", "")
					case APIProtocolAnthropic:
						result, err = svc.ForwardAsAnthropic(context.Background(), c, a, body, "", "")
					case APIProtocolResponses:
						result, err = svc.Forward(context.Background(), c, a, body)
					}
					require.NoError(t, err)
					require.NotNil(t, result.TokenUsageObservation)
					require.False(t, result.TokenUsageObservation.InputTokensKnown)
					require.False(t, result.TokenUsageObservation.OutputTokensKnown)
					require.Contains(t, rec.Body.String(), "hello")
				})
			}
		})
	}
}
func TestCompatibleKnownZeroUsage(t *testing.T) {
	restore := domain.SetCompatibleProvidersForTest([]domain.CompatibleProvider{compatibleFixture(APIProtocolChatCompletions, nil)})
	defer restore()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"c","object":"chat.completion","choices":[{"message":{"role":"assistant","content":""},"finish_reason":"stop"}],"usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}}`)
	}))
	defer server.Close()
	a := compatibleAccount(server.URL + "/v1")
	svc := &OpenAIGatewayService{cfg: loopbackHTTPTestConfig(), httpUpstream: &compatibleLoopback{client: server.Client()}}
	body := []byte(`{"model":"alias","messages":[{"role":"user","content":"hi"}]}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
	result, err := svc.ForwardAsChatCompletions(context.Background(), c, a, body, "", "")
	require.NoError(t, err)
	require.Zero(t, result.Usage.InputTokens)
	require.Zero(t, result.Usage.OutputTokens)
}

func TestCompatibleNoForeignDiscoveryOrMetadata(t *testing.T) {
	row := compatibleFixture(APIProtocolChatCompletions, nil)
	row.Modes[0].Endpoints = append(row.Modes[0].Endpoints, domain.CompatibleEndpoint{Protocol: APIProtocolAnthropic, BaseURL: "https://messages.example.com/v1", Auth: "x_api_key"})
	restore := domain.SetCompatibleProvidersForTest([]domain.CompatibleProvider{row})
	defer restore()
	a := compatibleAccount("")
	a.Credentials["api_base_urls"] = map[string]any{APIProtocolAnthropic: "https://private.example.com/v1"}
	_, err := buildCompatibleModelsRequest(context.Background(), a, func(s string) (string, error) {
		t.Fatal("must not try official discovery with custom key")
		return s, nil
	})
	require.ErrorContains(t, err, "declare models manually")
	tester := &AccountTestService{httpUpstream: &compatibleLoopback{}}
	_, err = tester.fetchModelsDevMetadata(context.Background(), a, []string{"gpt-5.4"})
	require.ErrorContains(t, err, "own reviewed capability profile")
	req, _ := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", nil)
	require.ErrorContains(t, applyCompatibleRequestAuth(a, req), "does not match")
	require.Empty(t, req.Header.Get("Authorization"))
}
