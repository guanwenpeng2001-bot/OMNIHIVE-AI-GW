//go:build unit

package handler

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

type openRouterHandlerUpstream struct {
	geminiReviewUpstream
	request *http.Request
}

func (u *openRouterHandlerUpstream) Do(request *http.Request, proxy string, id int64, concurrency int) (*http.Response, error) {
	u.request = request
	response, err := u.geminiReviewUpstream.Do(request, proxy, id, concurrency)
	if err == nil && gjson.GetBytes(u.body, "stream").Bool() {
		response.Header.Set("Content-Type", "text/event-stream")
		response.Body = io.NopCloser(strings.NewReader("data: " + `{"id":"fixture","choices":[{"index":0,"delta":{"role":"assistant","content":"hello","reasoning_content":"considering"},"finish_reason":null}]}` + "\n\ndata: " + `{"id":"fixture","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"cost":77}}` + "\n\ndata: [DONE]\n\n"))
	}
	if err == nil && (strings.HasSuffix(request.URL.Path, "/messages") || strings.HasSuffix(request.URL.Path, "/responses")) {
		protocol := "responses"
		if strings.HasSuffix(request.URL.Path, "/messages") {
			protocol = "messages"
		}
		response.Body = io.NopCloser(strings.NewReader(openRouterNativeTextFixture(protocol, gjson.GetBytes(u.body, "model").String(), gjson.GetBytes(u.body, "stream").Bool(), 2, 1)))
	}
	return response, err
}
func (u *openRouterHandlerUpstream) DoWithTLS(request *http.Request, proxy string, id int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(request, proxy, id, concurrency)
}

func TestOpenRouterActualPublicHandlers(t *testing.T)    { testOpenRouterPublicHandlers(t, false) }
func TestOpenRouterActualCompositeHandlers(t *testing.T) { testOpenRouterPublicHandlers(t, true) }
func testOpenRouterPublicHandlers(t *testing.T, composite bool) {
	for _, model := range []string{"openai/gpt-5-high", "openai/gpt-4o:free", "anthropic/claude-sonnet-4", "google/gemini-2.5-pro", "vendor/future:thinking"} {
		for _, protocol := range []string{"chat", "messages", "responses", "google"} {
			for _, stream := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/%t", protocol, model, stream), func(t *testing.T) {
					cfg := &config.Config{RunMode: config.RunModeSimple}
					account := preparedGatewayFixtureAccount(service.Account{ID: 7, Platform: service.PlatformOpenRouter, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Credentials: map[string]any{"api_key": "fixture", "model_mapping": map[string]any{model: model}}, AccountGroups: []service.AccountGroup{{AccountID: 7, GroupID: 3}}})
					repo := &geminiReviewAccountRepo{openAIWSUsageHandlerAccountRepoStub: openAIWSUsageHandlerAccountRepoStub{account: account}}
					usage := &cursorRouteUsageRepo{}
					receipts := newHunyuanSTTReceiptRepository()
					upstream := &openRouterHandlerUpstream{geminiReviewUpstream: geminiReviewUpstream{requestPlanHTTPFixture: requestPlanHTTPFixture{t: t}, chat: true}}
					svc := service.ProvideOpenAIGatewayService(repo, usage, nil, nil, nil, nil, nil, cfg, nil, nil, service.NewBillingService(cfg, nil), nil, nil, upstream, nil, nil, nil, nil, nil, nil, nil, nil, service.NewUsageSettlementService(receipts, nil, nil))
					billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
					defer billing.Stop()
					h := NewOpenAIGatewayHandler(svc, service.NewConcurrencyService(&fakeConcurrencyCache{}), billing, service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, cfg)
					group := &service.Group{ID: 3, Platform: service.PlatformOpenRouter, Status: service.StatusActive, Hydrated: true}
					if composite {
						group.Platform = service.PlatformComposite
					}
					path := "/v1/chat/completions"
					payload := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hello"}],"max_tokens":32}`, model)
					next := h.ChatCompletions
					switch protocol {
					case "messages":
						path = "/v1/messages"
						next = h.Messages
					case "responses":
						path = "/v1/responses"
						payload = fmt.Sprintf(`{"model":%q,"input":"hello"}`, model)
						next = h.Responses
					case "google":
						path = "/v1beta/models/" + model + ":generateContent"
						payload = `{"contents":[{"parts":[{"text":"hello"}]}]}`
						next = GeminiProtocolBridge(h.Messages, h.CountTokens, h.Embeddings)
					}
					tool := `{"type":"function","function":{"name":"weather","parameters":{"type":"object","properties":{"city":{"type":"string"}}}}}`
					if protocol == "messages" {
						tool = `{"name":"weather","input_schema":{"type":"object","properties":{"city":{"type":"string"}}}}`
					}
					if protocol == "responses" {
						tool = `{"type":"function","name":"weather","parameters":{"type":"object","properties":{"city":{"type":"string"}}}}`
					}
					if protocol == "google" {
						tool = `{"functionDeclarations":[{"name":"weather","parameters":{"type":"object","properties":{"city":{"type":"string"}}}}]}`
					}
					payload, _ = sjson.SetRaw(payload, "tools", "["+tool+"]")
					schema := `{"type":"object","properties":{"city":{"type":"string"}},"required":["city"],"additionalProperties":false}`
					switch protocol {
					case "chat":
						payload, _ = sjson.SetRaw(payload, "response_format", `{"type":"json_schema","json_schema":{"name":"result","schema":`+schema+`}}`)
						payload, _ = sjson.Set(payload, "reasoning_effort", "high")
					case "messages":
						payload, _ = sjson.SetRaw(payload, "output_config", `{"effort":"high","format":{"type":"json_schema","schema":`+schema+`}}`)
					case "responses":
						payload, _ = sjson.SetRaw(payload, "text.format", `{"type":"json_schema","name":"result","schema":`+schema+`}`)
						payload, _ = sjson.Set(payload, "reasoning.effort", "high")
					case "google":
						payload, _ = sjson.Set(payload, "generationConfig.responseMimeType", "application/json")
						payload, _ = sjson.SetRaw(payload, "generationConfig.responseSchema", schema)
						payload, _ = sjson.Set(payload, "generationConfig.thinkingConfig.thinkingLevel", "HIGH")
					}

					if protocol != "google" {
						payload, _ = sjson.Set(payload, "stream", stream)
					}
					action := "generateContent"
					if protocol == "google" && stream {
						action = "streamGenerateContent"
						path = "/v1beta/models/" + model + ":" + action + "?alt=sse"
					}
					c, rec := requestPlanHTTPContext(path, payload, group)
					if composite {
						c.Request = c.Request.WithContext(service.WithCompositeRouteDecision(c.Request.Context(), service.CompositeRouteDecision{Matched: true, Source: service.CompositeRouteSourceExplicit, PublicModel: model, UpstreamModel: model, TargetPlatform: service.PlatformOpenRouter}))
					}
					if protocol == "google" {
						c.Params = gin.Params{{Key: "modelAction", Value: "/" + model + ":" + action}}
					}
					next(c)
					require.Equal(t, 200, rec.Code, rec.Body.String())
					require.Equal(t, 1, upstream.calls)
					expectedPath := "/api/v1/chat/completions"
					if protocol == "messages" || protocol == "responses" {
						expectedPath = "/api/v1/" + protocol
					}
					require.Equal(t, expectedPath, upstream.path)
					require.Equal(t, model, gjson.GetBytes(upstream.body, "model").String())
					require.Contains(t, rec.Body.String(), "hello")
					if strings.HasPrefix(model, "vendor/future") {
						require.NotEmpty(t, receipts.receipts)
						require.Empty(t, usage.logs)
						for _, receipt := range receipts.receipts {
							require.Equal(t, service.UsageSettlementNeedsReview, receipt.State)
							require.Equal(t, service.PricingStateUnpriced, receipt.Usage.PricingState)
						}
						require.Zero(t, receipts.debit)
					}
					require.Equal(t, "Bearer fixture", upstream.request.Header.Get("Authorization"))
					switch protocol {
					case "messages":
						require.Equal(t, "weather", gjson.GetBytes(upstream.body, "tools.0.name").String())
						require.Equal(t, "object", gjson.GetBytes(upstream.body, "tools.0.input_schema.type").String())
						require.Equal(t, "json_schema", gjson.GetBytes(upstream.body, "output_config.format.type").String())
						require.Equal(t, "high", gjson.GetBytes(upstream.body, "output_config.effort").String())
					case "responses":
						require.Equal(t, "weather", gjson.GetBytes(upstream.body, "tools.0.name").String())
						require.Equal(t, "object", gjson.GetBytes(upstream.body, "tools.0.parameters.type").String())
						require.Equal(t, "json_schema", gjson.GetBytes(upstream.body, "text.format.type").String())
						require.Equal(t, "high", gjson.GetBytes(upstream.body, "reasoning.effort").String())
					default:
						require.Equal(t, "weather", gjson.GetBytes(upstream.body, "tools.0.function.name").String())
						require.Equal(t, "object", gjson.GetBytes(upstream.body, "tools.0.function.parameters.type").String())
						require.Equal(t, "json_schema", gjson.GetBytes(upstream.body, "response_format.type").String())
						require.Equal(t, "high", gjson.GetBytes(upstream.body, "reasoning_effort").String())
					}
					if stream && (protocol == "chat" || protocol == "google") {
						require.True(t, gjson.GetBytes(upstream.body, "stream_options.include_usage").Bool())
					}
					if protocol == "messages" || protocol == "responses" {
						require.False(t, gjson.GetBytes(upstream.body, "stream_options").Exists())
					}

				})
			}
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
