//go:build unit

package handler

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type cacheCompatibilityUpstream struct {
	protocolInputUpstream
	headers []http.Header
}

func (upstream *cacheCompatibilityUpstream) Do(request *http.Request, proxy string, accountID int64, concurrency int) (*http.Response, error) {
	upstream.headers = append(upstream.headers, request.Header.Clone())
	response, err := upstream.protocolInputUpstream.Do(request, proxy, accountID, concurrency)
	if err == nil {
		response.Header.Set("x-codex-turn-state", "handler-turn-state")
	}
	return response, err
}

func TestOpenAICacheCompatibilityHandlers(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, route := range []string{"oauth-messages", "public-messages", "public-responses"} {
			t.Run(fmt.Sprintf("%s/stream=%v", route, stream), func(t *testing.T) {
				cfg := &config.Config{RunMode: config.RunModeSimple}
				account := preparedGatewayFixtureAccount(service.Account{ID: 7, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Credentials: map[string]any{"api_key": "fixture", "base_url": "https://api.openai.com", "model_mapping": map[string]any{"gpt-6.1-sol": "gpt-6.1-sol"}}, AccountGroups: []service.AccountGroup{{AccountID: 7, GroupID: 3}}})
				account.Extra["openai_responses_supported"] = true
				if route == "oauth-messages" {
					account.Type = service.AccountTypeOAuth
					account.Credentials["access_token"] = "fixture"
				}
				upstream := &cacheCompatibilityUpstream{}
				repository := &openAIWSUsageHandlerAccountRepoStub{account: account}
				gateway := service.ProvideOpenAIGatewayService(repository, &cursorRouteUsageRepo{}, nil, nil, nil, nil, nil, cfg, nil, nil, service.NewBillingService(cfg, nil), nil, nil, upstream, nil, nil, nil, nil, nil, nil, nil, nil, service.NewUsageSettlementService(&unlimitedTPMReceiptStub{}, nil, nil))
				billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
				defer billing.Stop()
				handler := NewOpenAIGatewayHandler(gateway, service.NewConcurrencyService(&fakeConcurrencyCache{}), billing, service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, cfg)
				group := &service.Group{ID: 3, Platform: service.PlatformOpenAI, Status: service.StatusActive, Hydrated: true, AllowMessagesDispatch: true}
				identities := []string{""}
				if route == "oauth-messages" {
					identities = []string{"", "", "caller-one", "caller-two"}
				}
				for index, explicit := range identities {
					body := fmt.Sprintf(`{"model":"gpt-6.1-sol","max_tokens":32,"stream":%v,"prompt_cache_key":%q,"messages":[{"role":"user","content":[{"type":"text","text":"hello","cache_control":{"type":"ephemeral","ttl":"5m"}}]}]}`, stream, explicit)
					path := "/v1/messages"
					if route == "public-responses" {
						path = "/v1/responses"
						body = fmt.Sprintf(`{"model":"gpt-6.1-sol","stream":%v,"input":"hello","prompt_cache_options":{"mode":"implicit","ttl":"30m"}}`, stream)
					}
					client, recorder := requestPlanHTTPContext(path, body, group)
					client.Request.Header.Set("X-Claude-Code-Session-Id", "real-claude-session")
					client.Request.Header.Set("User-Agent", "curl/8.0")
					if route == "public-responses" {
						handler.Responses(client)
					} else {
						handler.Messages(client)
					}
					require.Equal(t, 200, recorder.Code, recorder.Body.String())
					require.Len(t, upstream.headers, index+1)
					if route == "oauth-messages" {
						require.NotEmpty(t, upstream.headers[index].Get("session_id"))
						require.NotEqual(t, "real-claude-session", upstream.headers[index].Get("session_id"))
						require.False(t, gjson.GetBytes(upstream.body, "prompt_cache_key").Exists())
						require.False(t, gjson.GetBytes(upstream.body, "previous_response_id").Exists())
						require.False(t, gjson.GetBytes(upstream.body, "prompt_cache_options").Exists())
						if index == 1 {
							require.Equal(t, upstream.headers[0].Get("session_id"), upstream.headers[1].Get("session_id"))
							require.Equal(t, "handler-turn-state", upstream.headers[1].Get("x-codex-turn-state"))
						}
						if index > 1 {
							require.NotEqual(t, upstream.headers[index-1].Get("session_id"), upstream.headers[index].Get("session_id"))
							require.Empty(t, upstream.headers[index].Get("x-codex-turn-state"))
						}
					} else if route == "public-messages" {
						require.Equal(t, "explicit", gjson.GetBytes(upstream.body, "prompt_cache_options.mode").String())
						require.Equal(t, "explicit", gjson.GetBytes(upstream.body, "input.0.content.0.prompt_cache_breakpoint.mode").String())
					} else {
						require.JSONEq(t, `{"mode":"implicit","ttl":"30m"}`, gjson.GetBytes(upstream.body, "prompt_cache_options").Raw)
					}
				}
			})
		}
	}
}
