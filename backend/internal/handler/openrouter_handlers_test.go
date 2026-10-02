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
	return response, err
}
func (u *openRouterHandlerUpstream) DoWithTLS(request *http.Request, proxy string, id int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(request, proxy, id, concurrency)
}

func TestOpenRouterActualPublicHandlers(t *testing.T) {
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
					if protocol == "google" {
						c.Params = gin.Params{{Key: "modelAction", Value: "/" + model + ":" + action}}
					}
					next(c)
					require.Equal(t, 200, rec.Code, rec.Body.String())
					require.Equal(t, 1, upstream.calls)
					require.Equal(t, "/api/v1/chat/completions", upstream.path)
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
					require.Equal(t, "weather", gjson.GetBytes(upstream.body, "tools.0.function.name").String())
					require.Equal(t, "object", gjson.GetBytes(upstream.body, "tools.0.function.parameters.type").String())
					require.Equal(t, "json_schema", gjson.GetBytes(upstream.body, "response_format.type").String())
					require.Equal(t, "object", gjson.GetBytes(upstream.body, "response_format.json_schema.schema.type").String())
					require.Equal(t, "high", gjson.GetBytes(upstream.body, "reasoning_effort").String())
					if stream {
						require.True(t, gjson.GetBytes(upstream.body, "stream_options.include_usage").Bool())
					}
				})
			}
		}
	}
}
