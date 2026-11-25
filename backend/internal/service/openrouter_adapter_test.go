//go:build unit

package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func openRouterFixtureAccount() *Account {
	return &Account{ID: 42, Platform: PlatformOpenRouter, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture-key"}}
}

func TestOpenRouterPublicTextForwarding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, model := range []string{"openai/gpt-4o", "openai/gpt-5-high", "anthropic/claude-sonnet-4", "google/gemini-2.5-pro", "openai/gpt-4o:free", "openrouter/auto", "vendor/future-model:thinking"} {
		for _, protocol := range []string{"chat", "messages", "responses"} {
			for _, stream := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/%t", model, protocol, stream), func(t *testing.T) {
					payload := fmt.Sprintf(`{"model":%q,"stream":%t,"messages":[{"role":"user","content":"hello"}],"max_tokens":32,"reasoning_effort":"high","tools":[{"type":"function","function":{"name":"weather","parameters":{"type":"object","properties":{"city":{"type":"string"}}}}}]}`, model, stream)
					if protocol == "messages" {
						payload = fmt.Sprintf(`{"model":%q,"stream":%t,"max_tokens":32,"messages":[{"role":"user","content":"hello"}],"thinking":{"type":"enabled","budget_tokens":2048},"tools":[{"name":"weather","input_schema":{"type":"object","properties":{"city":{"type":"string"}}}}]}`, model, stream)
					}
					if protocol == "responses" {
						payload = fmt.Sprintf(`{"model":%q,"stream":%t,"input":"hello","reasoning":{"effort":"high"},"tools":[{"type":"function","name":"weather","parameters":{"type":"object","properties":{"city":{"type":"string"}}}}]}`, model, stream)
					}
					output := `{"id":"fixture","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":17,"completion_tokens":8,"cost":99}}`
					contentType := "application/json"
					if stream {
						contentType = "text/event-stream"
						output = "data: " + `{"id":"fixture","choices":[{"index":0,"delta":{"role":"assistant","content":"hello"},"finish_reason":null}]}` + "\n\ndata: " + `{"id":"fixture","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":17,"completion_tokens":8,"cost":99}}` + "\n\ndata: [DONE]\n\n"
					}
					if protocol == "messages" || protocol == "responses" {
						output = openRouterNativeTextFixture(protocol, model, stream, 17, 8)
					}
					upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(output))}}
					gateway := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
					account := openRouterFixtureAccount()
					recorder := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(recorder)
					body := []byte(payload)
					c.Request = httptest.NewRequest("POST", "/v1/"+protocol, strings.NewReader(payload))
					var result *OpenAIForwardResult
					var err error
					switch protocol {
					case "chat":
						result, err = gateway.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")
					case "messages":
						result, err = gateway.ForwardAsAnthropic(context.Background(), c, account, body, "", "")
					case "responses":
						result, err = gateway.Forward(context.Background(), c, account, body)
					}
					require.NoError(t, err)
					require.NotNil(t, result)
					expectedURL := "https://openrouter.ai/api/v1/chat/completions"
					if protocol == "messages" || protocol == "responses" {
						expectedURL = "https://openrouter.ai/api/v1/" + protocol
					}
					require.Equal(t, expectedURL, upstream.lastReq.URL.String())
					require.Equal(t, "Bearer fixture-key", upstream.lastReq.Header.Get("Authorization"))
					require.Equal(t, model, gjson.GetBytes(upstream.lastBody, "model").String())
					namePath, schemaPath := "tools.0.function.name", "tools.0.function.parameters.type"
					if protocol == "messages" {
						namePath, schemaPath = "tools.0.name", "tools.0.input_schema.type"
					}
					if protocol == "responses" {
						namePath, schemaPath = "tools.0.name", "tools.0.parameters.type"
					}
					require.Equal(t, "weather", gjson.GetBytes(upstream.lastBody, namePath).String())
					require.Equal(t, "object", gjson.GetBytes(upstream.lastBody, schemaPath).String())
					require.Equal(t, 17, result.Usage.InputTokens)
					require.Equal(t, 8, result.Usage.OutputTokens)
					if stream && protocol == "chat" {
						require.True(t, gjson.GetBytes(upstream.lastBody, "stream_options.include_usage").Bool())
					}
					require.Contains(t, recorder.Body.String(), "hello")
				})
			}
		}
	}
}

func TestOpenRouterMetadataAndKeyUsage(t *testing.T) {
	models, metadata, err := extractUpstreamModelCatalog([]byte(`{"data":[{"id":"vendor/future:free","name":"Future","context_length":65536,"architecture":{"input_modalities":["text","image"],"output_modalities":["text"]},"top_provider":{"max_completion_tokens":8192},"supported_parameters":["tools","reasoning"],"reasoning":{"mandatory":true},"pricing":{"prompt":"0"}}]}`), false, PlatformOpenRouter)
	require.NoError(t, err)
	require.Equal(t, []string{"vendor/future:free"}, models)
	m := metadata[models[0]]
	require.EqualValues(t, 65536, m.ContextWindow)
	require.EqualValues(t, 8192, m.MaxOutputTokens)
	require.Equal(t, []string{"text", "image"}, m.InputModalities)
	require.Equal(t, []string{"tools", "reasoning"}, m.SupportedParameters)
	require.NotNil(t, m.Free)
	require.True(t, *m.Free)
	require.Nil(t, m.SupportedReasoningLevels)
	require.Nil(t, m.SupportsFunctionCalling)
	account := openRouterFixtureAccount()
	request, err := (&AccountTestService{cfg: rawChatCompletionsTestConfig()}).buildOpenRouterModelsRequest(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "https://openrouter.ai/api/v1/models/user?output_modalities=all", request.URL.String())
	for _, fixture := range []string{`{"data":{"limit":null,"limit_remaining":null,"usage":1,"usage_daily":0,"usage_weekly":0.5,"usage_monthly":1}}`, `{"data":{"limit":10,"limit_remaining":9,"usage":1,"limit_reset":"monthly"}}`} {
		usage, err := parseOpenRouterKeyUsage([]byte(fixture))
		require.NoError(t, err)
		require.Equal(t, float64(1), *usage.Usage)
		require.Equal(t, usage.Limit == nil, usage.Unlimited)
	}
	for _, fixture := range []string{`{}`, `{"data":null}`, `{"data":{"usage":-1}}`} {
		_, err := parseOpenRouterKeyUsage([]byte(fixture))
		require.Error(t, err)
	}
}

func TestOpenRouterDiscoveryComputesFreeMetadata(t *testing.T) {
	_, metadata, err := extractUpstreamModelCatalog([]byte(`{"data":[{"id":"vendor/free:free","pricing":{"prompt":"0","completion":"0","request":"0"}},{"id":"vendor/paid-alias:free","pricing":{"prompt":"0.000001","completion":"0"}},{"id":"vendor/tiny-paid:free","pricing":{"prompt":"1e-999"}},{"id":"vendor/zero-priced","pricing":{"prompt":0,"completion":0}},{"id":"vendor/paid","pricing":{"prompt":"0","completion":"0.01"}}]}`), false, PlatformOpenRouter)
	require.NoError(t, err)
	for _, model := range []string{"vendor/free:free", "vendor/zero-priced"} {
		require.NotNil(t, metadata[model].Free, model)
		require.True(t, *metadata[model].Free, model)
	}
	for _, model := range []string{"vendor/paid-alias:free", "vendor/tiny-paid:free", "vendor/paid"} {
		require.NotNil(t, metadata[model].Free, model)
		require.False(t, *metadata[model].Free, model)
	}

	_, metadata, err = extractUpstreamModelCatalog([]byte(`{"data":[{"id":"vendor/missing:free"},{"id":"vendor/null:free","pricing":null},{"id":"vendor/empty:free","pricing":{}},{"id":"vendor/null-fields:free","pricing":{"prompt":null,"completion":null}},{"id":"vendor/nested-empty:free","pricing":{"prompt":{}}},{"id":"vendor/malformed:free","pricing":"free"},{"id":"vendor/bool:free","pricing":{"prompt":false}}]}`), false, PlatformOpenRouter)
	require.NoError(t, err)
	for _, model := range []string{"vendor/missing:free", "vendor/null:free", "vendor/empty:free", "vendor/null-fields:free", "vendor/nested-empty:free", "vendor/malformed:free", "vendor/bool:free"} {
		if metadata[model].Free != nil {
			require.False(t, *metadata[model].Free, model)
		}
	}
}

func TestOpenRouterPaginationAndUsageTransport(t *testing.T) {
	response := func(body string) *http.Response {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
	}
	for _, next := range []string{"?page=2", "https://attacker.example/models"} {
		upstream := &httpUpstreamRecorder{responses: []*http.Response{response(`{"data":[{"id":"vendor/one"}],"links":{"next":"` + next + `"}}`), response(`{"data":[{"id":"vendor/two:free"}],"links":{"next":null}}`)}}
		account := openRouterFixtureAccount()
		svc := &AccountTestService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
		request, err := svc.buildOpenRouterModelsRequest(context.Background(), account)
		require.NoError(t, err)
		models, _, err := svc.fetchCompleteModelPages(request, account)
		if strings.Contains(next, "attacker") {
			require.Error(t, err)
			require.Len(t, upstream.requests, 1)
		} else {
			require.NoError(t, err)
			require.Equal(t, []string{"vendor/one", "vendor/two:free"}, models)
			require.Len(t, upstream.requests, 2)
			require.Equal(t, "Bearer fixture-key", upstream.requests[1].Header.Get("Authorization"))
		}
	}
	upstream := &httpUpstreamRecorder{resp: response(`{"data":{"limit":null,"limit_remaining":null,"usage":12,"usage_daily":1,"usage_weekly":2,"usage_monthly":3}}`)}
	account := openRouterFixtureAccount()
	account.Credentials["base_url"] = "https://relay.example/prefix/v1"
	proxyID := int64(5)
	account.ProxyID = &proxyID
	account.Proxy = &Proxy{Protocol: "http", Host: "proxy.example", Port: 8080}
	usageSvc := &AccountUsageService{openRouterGateway: &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}}
	usage, err := usageSvc.getOpenRouterUsage(context.Background(), account, true)
	require.NoError(t, err)
	require.True(t, usage.OpenRouterKey.Unlimited)
	require.Equal(t, "https://relay.example/prefix/v1/key", upstream.lastReq.URL.String())
	require.Equal(t, "http://proxy.example:8080", upstream.lastProxyURL)
	require.Equal(t, "Bearer fixture-key", upstream.lastReq.Header.Get("Authorization"))
}

func TestOpenRouterUnknownUsageAndErrors(t *testing.T) {
	for _, protocol := range []string{"chat", "messages", "responses"} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%t", protocol, stream), func(t *testing.T) {
				output := `{"id":"fixture","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}]}`
				ct := "application/json"
				if stream {
					ct = "text/event-stream"
					output = "data: " + `{"id":"fixture","choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":"stop"}]}` + "\n\ndata: [DONE]\n\n"
				}
				upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {ct}}, Body: io.NopCloser(strings.NewReader(output))}}
				gateway := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
				account := openRouterFixtureAccount()
				payload := fmt.Sprintf(`{"model":"vendor/future","stream":%t,"max_tokens":32,"messages":[{"role":"user","content":"hello"}]}`, stream)
				if protocol == "responses" {
					payload = fmt.Sprintf(`{"model":"vendor/future","stream":%t,"input":"hello"}`, stream)
				}
				c := adaptiveProtocolTestContext("/v1/"+protocol, []byte(payload))
				var result *OpenAIForwardResult
				var err error
				switch protocol {
				case "chat":
					result, err = gateway.ForwardAsChatCompletions(context.Background(), c, account, []byte(payload), "", "")
				case "messages":
					result, err = gateway.ForwardAsAnthropic(context.Background(), c, account, []byte(payload), "", "")
				case "responses":
					result, err = gateway.Forward(context.Background(), c, account, []byte(payload))
				}
				require.Error(t, err)
				require.Nil(t, result, "unknown usage must not yield a billable zero result")
			})
		}
	}
	for _, test := range []struct {
		status    int
		source    string
		transient bool
	}{{401, "", false}, {402, "openrouter_credits", false}, {402, "openrouter_key_limit", false}, {402, "openrouter_in_flight_budget", true}, {403, "", false}, {429, "", false}} {
		t.Run(fmt.Sprintf("%d/%s", test.status, test.source), func(t *testing.T) {
			body := fmt.Sprintf(`{"error":{"code":%d,"message":"fixture","metadata":{"limit_source":%q}}}`, test.status, test.source)
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: test.status, Header: http.Header{"Retry-After": {"23"}}, Body: io.NopCloser(strings.NewReader(body))}}
			gateway := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
			account := openRouterFixtureAccount()
			payload := []byte(`{"model":"vendor/future","messages":[{"role":"user","content":"hello"}]}`)
			c := adaptiveProtocolTestContext("/v1/chat/completions", payload)
			_, err := gateway.ForwardAsChatCompletions(context.Background(), c, account, payload, "", "")
			require.Error(t, err)
			if test.status == 403 {
				require.False(t, gateway.handleOpenAIAccountUpstreamError(context.Background(), account, 403, nil, []byte(body), "vendor/future"))
				return
			}
			var failure *UpstreamFailoverError
			require.ErrorAs(t, err, &failure)
			require.Equal(t, test.transient, failure.RequestScopedTransient)
			require.Equal(t, "23", failure.ResponseHeaders.Get("Retry-After"))
			if test.transient {
				require.Greater(t, failure.SameAccountRetryDelay.Milliseconds(), int64(22000))
			}
		})
	}
}

func TestOpenRouterStreamFailureAfterUsageIsNotSuccess(t *testing.T) {
	for _, protocol := range []string{"chat", "messages", "responses"} {
		upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: " + `{"id":"fixture","choices":[{"index":0,"delta":{"content":"partial"},"finish_reason":null}],"usage":{"prompt_tokens":3,"completion_tokens":1}}` + "\n\ndata: " + `{"choices":[{"index":0,"delta":{},"finish_reason":"error"}],"error":{"code":429,"message":"provider limit"}}` + "\n\ndata: [DONE]\n\n"))}}
		gateway := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
		account := openRouterFixtureAccount()
		payload := []byte(`{"model":"vendor/future","stream":true,"max_tokens":32,"messages":[{"role":"user","content":"hello"}]}`)
		if protocol == "responses" {
			payload = []byte(`{"model":"vendor/future","stream":true,"input":"hello"}`)
		}
		c := adaptiveProtocolTestContext("/v1/"+protocol, payload)
		var err error
		switch protocol {
		case "chat":
			_, err = gateway.ForwardAsChatCompletions(context.Background(), c, account, payload, "", "")
		case "messages":
			_, err = gateway.ForwardAsAnthropic(context.Background(), c, account, payload, "", "")
		case "responses":
			_, err = gateway.Forward(context.Background(), c, account, payload)
		}
		require.Error(t, err, protocol)
	}
}

func TestOpenRouterErrorStateIsNotModelRevocation(t *testing.T) {
	for _, fixture := range []struct {
		status               int
		source               string
		permanent, temporary bool
	}{{401, "", true, false}, {402, "openrouter_credits", false, true}, {402, "openrouter_key_limit", false, true}, {402, "openrouter_in_flight_budget", false, false}, {403, "", false, false}, {429, "", false, false}} {
		repo := &commandCodeRecoveryRepo{}
		gateway := &OpenAIGatewayService{rateLimitService: NewRateLimitService(repo, nil, rawChatCompletionsTestConfig(), nil, nil)}
		account := openRouterFixtureAccount()
		body := []byte(fmt.Sprintf(`{"error":{"code":%d,"message":"fixture","metadata":{"limit_source":%q}}}`, fixture.status, fixture.source))
		gateway.handleOpenAIAccountUpstreamError(context.Background(), account, fixture.status, http.Header{"Retry-After": {"23"}}, body, "vendor/future")
		require.Equal(t, fixture.permanent, repo.permanent == 1)
		require.Equal(t, fixture.temporary, !repo.temporaryUntil.IsZero())
		if fixture.status == 429 {
			require.False(t, repo.rateUntil.IsZero())
		}
	}
}

func openRouterNativeTextFixture(protocol, model string, stream bool, input, output int) string {
	if protocol == "messages" {
		body := fmt.Sprintf(`{"id":"native-message","type":"message","role":"assistant","model":%q,"content":[{"type":"text","text":"hello"}],"stop_reason":"end_turn","usage":{"input_tokens":%d,"output_tokens":%d}}`, model, input, output)
		if !stream {
			return body
		}
		return fmt.Sprintf("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"native-message\",\"type\":\"message\",\"role\":\"assistant\",\"model\":%q,\"content\":[],\"usage\":{\"input_tokens\":%d,\"output_tokens\":0}}}\n\nevent: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"hello\"}}\n\nevent: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":%d}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n", model, input, output)
	}
	body := fmt.Sprintf(`{"id":"native-response","object":"response","status":"completed","model":%q,"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":%d,"output_tokens":%d}}`, model, input, output)
	if !stream {
		return body
	}
	return "event: response.completed\ndata: " + `{"type":"response.completed","response":` + body + `}` + "\n\n"
}
