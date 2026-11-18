//go:build unit

package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/tlsfingerprint"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/repository"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func specialAuthHandler(t *testing.T, account service.Account, transport service.HTTPUpstream) (*OpenAIGatewayHandler, *service.Group, *hunyuanSTTReceiptRepository, *cursorRouteUsageRepo) {
	t.Helper()
	cfg := compatibleReviewConfig()
	accounts := &geminiReviewAccountRepo{openAIWSUsageHandlerAccountRepoStub: openAIWSUsageHandlerAccountRepoStub{account: account}}
	receipts := newHunyuanSTTReceiptRepository()
	usage := &cursorRouteUsageRepo{}
	billingService := service.NewBillingService(cfg, nil)
	gateway := service.ProvideOpenAIGatewayService(accounts, usage, nil, nil, nil, nil, nil, cfg, nil, nil, billingService, nil, nil, transport, nil, nil, nil, service.NewModelPricingResolver(nil, billingService), nil, nil, nil, nil, service.NewUsageSettlementService(receipts, nil, nil))
	billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, &config.Config{RunMode: config.RunModeSimple}, nil)
	t.Cleanup(billing.Stop)
	handler := NewOpenAIGatewayHandler(gateway, service.NewConcurrencyService(&fakeConcurrencyCache{}), billing, service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, cfg)
	inputPrice, outputPrice := 0.5, 1.0
	group := &service.Group{ID: 3, Platform: account.Platform, Status: service.StatusActive, Hydrated: true, RateMultiplier: 1, ModelPricing: []service.ChannelModelPricing{{Models: []string{"fixture-model"}, BillingMode: service.BillingModeToken, InputPrice: &inputPrice, OutputPrice: &outputPrice}}}
	return handler, group, receipts, usage
}

// The Copilot test keeps the actual destination through all gateway builders and
// auth checks; only the test transport maps the documented host to loopback.
// Nous official_proxy uses the real shared repository transport without rewriting.
type specialAuthLoopbackTransport struct {
	test   *testing.T
	client *http.Client
	base   string
	host   string
	bearer string
}

func (transport *specialAuthLoopbackTransport) Do(request *http.Request, proxy string, id int64, concurrency int) (*http.Response, error) {
	host := transport.host
	if host == "" {
		host = "api.githubcopilot.com"
	}
	require.Equal(transport.test, host, request.URL.Host)
	require.True(transport.test, service.HTTPUpstreamRedirectsDisabled(request.Context()))
	if host == "api.githubcopilot.com" {
		require.Equal(transport.test, "omnihive", request.Header.Get("Copilot-Integration-Id"))
	} else {
		require.Empty(transport.test, request.Header.Get("Copilot-Integration-Id"))
	}
	bearer := transport.bearer
	if bearer == "" {
		bearer = "github_pat_fixture"
	}
	require.Equal(transport.test, "Bearer "+bearer, request.Header.Get("Authorization"))
	if request.Method == http.MethodGet && host == "api.githubcopilot.com" {
		require.Equal(transport.test, "/models", request.URL.Path)
		require.Equal(transport.test, service.CopilotModelsAPIVersion, request.Header.Get("X-GitHub-Api-Version"))
	} else {
		require.Empty(transport.test, request.Header.Get("X-GitHub-Api-Version"))
	}
	require.Empty(transport.test, request.Header.Get("X-Api-Key"))
	copied := request.Clone(request.Context())
	destination, _ := url.Parse(transport.base)
	copied.URL.Scheme = destination.Scheme
	copied.URL.Host = destination.Host
	client := *transport.client
	client.CheckRedirect = service.CopilotNousCheckRedirect
	return client.Do(copied)
}
func (transport *specialAuthLoopbackTransport) DoWithTLS(request *http.Request, proxy string, id int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return transport.Do(request, proxy, id, concurrency)
}
func specialHandlerAccount(platform, base, key string) service.Account {
	account := compatibleReviewAccount(base)
	account.Platform = platform
	account.Credentials["account_mode"] = "payg"
	account.Credentials["api_key"] = key
	if platform == "nous_research" {
		account.Credentials["auth_mode"] = "official_proxy"
	}
	account.Credentials["header_override_enabled"] = true
	account.Credentials["header_overrides"] = map[string]any{"Copilot-Integration-Id": "agentic-workflows"}
	return preparedGatewayFixtureAccount(account)
}
func TestCopilotNousPublicHandlers(t *testing.T) {
	for _, scenario := range []string{"github_copilot", "nous_research", "nous_invocation"} {
		platform := scenario
		if scenario == "nous_invocation" {
			platform = "nous_research"
		}
		for _, path := range []string{"/v1/chat/completions", "/v1/messages", "/v1/responses", "/v1beta/models/fixture-model:generateContent"} {
			for _, stream := range []bool{false, true} {
				for _, presence := range []string{"missing", "positive"} {
					t.Run(fmt.Sprintf("%s/%s/%t/%s", scenario, path, stream, presence), func(t *testing.T) {
						var calls atomic.Int64
						upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
							calls.Add(1)
							require.Equal(t, "POST", request.Method)
							require.Equal(t, "/v1/chat/completions", request.URL.Path)
							if scenario == "nous_research" {
								require.Empty(t, request.Header.Get("Authorization"))
							}
							var body map[string]any
							require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
							require.Equal(t, "fixture-model", body["model"])
							require.Contains(t, fmt.Sprint(body["tools"]), "lookup")
							upstreamStream, _ := body["stream"].(bool)
							if upstreamStream {
								writer.Header().Set("Content-Type", "text/event-stream")
							} else {
								writer.Header().Set("Content-Type", "application/json")
							}
							reply := compatibleReviewReply(domain.APIProtocolChatCompletions, presence, upstreamStream)
							reply = strings.ReplaceAll(reply, `"content":"delivered"`, `"content":"delivered","tool_calls":[{"index":0,"id":"lookup-call","type":"function","function":{"name":"lookup","arguments":"{}"}}]`)
							reply = strings.ReplaceAll(reply, `"finish_reason":"stop"`, `"finish_reason":"tool_calls"`)
							fmt.Fprint(writer, reply)
						}))
						defer upstream.Close()
						account := specialHandlerAccount(platform, upstream.URL+"/v1", "")
						var transport service.HTTPUpstream = repository.NewHTTPUpstream(compatibleReviewConfig())
						if platform == "github_copilot" {
							account = specialHandlerAccount(platform, "", "github_pat_fixture")
							transport = &specialAuthLoopbackTransport{test: t, client: upstream.Client(), base: upstream.URL}
						}
						if scenario == "nous_invocation" {
							token := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256"}`)) + "." + base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"exp":%d,"scope":"inference:invoke"}`, time.Now().Add(time.Hour).Unix()))) + "." + base64.RawURLEncoding.EncodeToString([]byte("synthetic-signature"))
							account = specialHandlerAccount(platform, "", token)
							account.Credentials["auth_mode"] = service.NousAuthInvocationToken
							transport = &specialAuthLoopbackTransport{test: t, client: upstream.Client(), base: upstream.URL, host: "inference-api.nousresearch.com", bearer: token}
						}
						handler, group, receipts, usage := specialAuthHandler(t, account, transport)
						body := fmt.Sprintf(`{"model":"fixture-model","stream":%t,"input":"hi","messages":[{"role":"user","content":"hi"}],"max_tokens":32}`, stream)
						var payload map[string]any
						require.NoError(t, json.Unmarshal([]byte(body), &payload))
						switch path {
						case "/v1/messages":
							payload["tools"] = []any{map[string]any{"name": "lookup", "input_schema": map[string]any{"type": "object"}}}
						case "/v1/responses":
							payload["tools"] = []any{map[string]any{"type": "function", "name": "lookup", "parameters": map[string]any{"type": "object"}}}
						default:
							payload["tools"] = []any{map[string]any{"type": "function", "function": map[string]any{"name": "lookup", "parameters": map[string]any{"type": "object"}}}}
						}
						encoded, err := json.Marshal(payload)
						require.NoError(t, err)
						body = string(encoded)
						actualPath := path
						if strings.Contains(path, "v1beta") {
							body = `{"contents":[{"role":"user","parts":[{"text":"hi"}]}],"tools":[{"functionDeclarations":[{"name":"lookup","parameters":{"type":"OBJECT"}}]}]}`
							if stream {
								actualPath = strings.Replace(path, ":generateContent", ":streamGenerateContent?alt=sse", 1)
							}
						}
						client, rec := requestPlanHTTPContext(actualPath, body, group)
						switch path {
						case "/v1/messages":
							handler.Messages(client)
						case "/v1/chat/completions":
							handler.ChatCompletions(client)
						case "/v1/responses":
							handler.Responses(client)
						default:
							action := "generateContent"
							if stream {
								action = "streamGenerateContent"
							}
							client.Params = gin.Params{{Key: "modelAction", Value: "/fixture-model:" + action}}
							GeminiProtocolBridge(handler.Messages, nil, nil)(client)
						}
						require.Equal(t, 200, rec.Code, rec.Body.String())
						require.Contains(t, rec.Body.String(), "delivered")
						require.Contains(t, rec.Body.String(), "lookup")
						require.EqualValues(t, 1, calls.Load())
						require.Len(t, receipts.receipts, 1)
						require.Empty(t, usage.logs)
						for _, receipt := range receipts.receipts {
							if presence == "missing" {
								require.Equal(t, service.UsageSettlementNeedsReview, receipt.State)
								require.Equal(t, service.PricingStateUnpriced, receipt.Usage.PricingState)
								require.Zero(t, receipts.debit)
							} else {
								require.Equal(t, service.UsageSettlementSettled, receipt.State)
							}
						}
					})
				}
			}
		}
	}
}
func TestCopilotNousSharedTransportRedirectAndEgressPolicy(t *testing.T) {
	var targetCalls atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { targetCalls.Add(1) }))
	defer target.Close()
	for _, status := range []int{301, 302, 307, 308, 401, 403, 429} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			source := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				require.Equal(t, "Bearer frontdoor", request.Header.Get("Authorization"))
				writer.Header().Set("Location", target.URL)
				writer.WriteHeader(status)
				fmt.Fprint(writer, `{"error":{"message":"fixture denial"}}`)
			}))
			defer source.Close()
			account := specialHandlerAccount("nous_research", source.URL+"/v1", "frontdoor")
			handler, group, _, _ := specialAuthHandler(t, account, repository.NewHTTPUpstream(compatibleReviewConfig()))
			client, rec := requestPlanHTTPContext("/v1/chat/completions", `{"model":"fixture-model","messages":[{"role":"user","content":"hi"}]}`, group)
			handler.ChatCompletions(client)
			require.GreaterOrEqual(t, rec.Code, 400, rec.Body.String())
			require.Zero(t, targetCalls.Load())
		})
	}
	cfg := compatibleReviewConfig()
	cfg.Security.URLAllowlist.Enabled = true
	cfg.Security.URLAllowlist.AllowPrivateHosts = false
	cfg.Security.URLAllowlist.UpstreamHosts = []string{"allowed.invalid"}
	account := specialHandlerAccount("nous_research", target.URL+"/v1", "frontdoor")
	handler, group, _, _ := specialAuthHandler(t, account, repository.NewHTTPUpstream(cfg))
	client, rec := requestPlanHTTPContext("/v1/chat/completions", `{"model":"fixture-model","messages":[{"role":"user","content":"hi"}]}`, group)
	handler.ChatCompletions(client)
	require.GreaterOrEqual(t, rec.Code, 400)
	require.Zero(t, targetCalls.Load())
}
func TestCopilotNousSavedProbeAndCatalog(t *testing.T) {
	for _, platform := range []string{"github_copilot", "nous_research"} {
		var paths []string
		upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			paths = append(paths, request.Method+" "+request.URL.Path)
			if platform == "nous_research" {
				require.Empty(t, request.Header.Get("Authorization"))
			}
			if request.Method == "GET" {
				writer.Header().Set("Content-Type", "application/json")
				fmt.Fprint(writer, `{"data":[{"id":"fixture-model"}]}`)
			} else {
				writer.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(writer, compatibleReviewReply(domain.APIProtocolChatCompletions, "positive", true))
			}
		}))
		defer upstream.Close()
		cfg := compatibleReviewConfig()
		account := specialHandlerAccount(platform, upstream.URL+"/v1", "")
		var transport service.HTTPUpstream = repository.NewHTTPUpstream(cfg)
		catalogPath := "GET /v1/models"
		if platform == "github_copilot" {
			account = specialHandlerAccount(platform, "", "github_pat_fixture")
			transport = &specialAuthLoopbackTransport{test: t, client: upstream.Client(), base: upstream.URL}
			catalogPath = "GET /models"
		}
		accounts := &openAIWSUsageHandlerAccountRepoStub{account: account}
		probe := service.NewAccountTestService(accounts, nil, nil, nil, nil, transport, cfg, nil)
		result := probe.ProbeAccount(context.Background(), service.AccountProbeInput{AccountID: account.ID, ModelID: "fixture-model"}, func(service.TestEvent) {})
		require.True(t, result.Success, result.Error)
		catalog, err := probe.SyncUpstreamModelCatalog(context.Background(), &account)
		require.NoError(t, err)
		require.Contains(t, catalog.DiscoveredModels, "fixture-model")
		require.Equal(t, []string{"POST /v1/chat/completions", catalogPath}, paths)
	}
}
