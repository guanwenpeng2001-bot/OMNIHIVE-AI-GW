package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func nativeProfileAccount(platform, mode, base string) *Account {
	return &Account{Platform: platform, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture-key", "account_mode": mode, "api_protocol": APIProtocolAdaptive, "base_url": base}}
}

func TestNativeProfileExactResolution(t *testing.T) {
	for _, p := range nativeProviderProfiles {
		t.Run(p.Label, func(t *testing.T) {
			base := p.Chat
			if base == "" {
				base = p.Responses
			}
			a := nativeProfileAccount(p.Platform, p.Mode, base)
			require.Equal(t, p.Chat, a.GetCNProtocolBaseURL(APIProtocolChatCompletions))
			require.Equal(t, p.Messages, a.GetCNProtocolBaseURL(APIProtocolAnthropic))
			require.Equal(t, p.Responses, a.GetCNProtocolBaseURL(APIProtocolResponses))
			require.Equal(t, p.Chat, a.GetOpenAIFormatBaseURL())
			require.Equal(t, p.Responses != "", a.SupportsNativeCNResponses())
			if p.Capability != "" {
				require.Equal(t, p.Capability, resolveCapabilityProfile(a))
			} else {
				require.True(t, strings.HasPrefix(resolveCapabilityProfile(a), "custom:"))
			}
			// Explicit per-protocol roots win in fixed as well as adaptive mode.
			a.Credentials["api_protocol"] = APIProtocolAnthropic
			a.Credentials["api_base_urls"] = map[string]any{APIProtocolAnthropic: "https://relay.example/kept"}
			require.Equal(t, "https://relay.example/kept", a.GetAnthropicProtocolBaseURL())
			require.True(t, strings.HasPrefix(resolveCapabilityProfile(a), "custom:"))
		})
	}
}

func TestNativeProfileBoundaryIsolation(t *testing.T) {
	for _, base := range []string{"https://api.kimi.ai/coding/v1", "https://api.kimi.com.evil.test/coding/v1", "https://api.kimi.com/coding/v1?other=1", "https://api.kimi.com/other", "https://api.kimi.com:444/coding/v1", "http://api.kimi.com/coding/v1", "https://relay.example/v1"} {
		a := nativeProfileAccount(PlatformKimi, AccountModeCoding, base)
		require.Empty(t, nativeProfileProbeURL(a, true), base)
		require.True(t, strings.HasPrefix(resolveCapabilityProfile(a), "custom:"), base)
	}
	a := nativeProfileAccount(PlatformKimi, AccountModePayG, "https://api.moonshot.ai/anthropic")
	require.Equal(t, "https://api.moonshot.ai/v1/users/me/balance", nativeProfileProbeURL(a, false))
	a.Credentials["account_mode"] = AccountModeCoding
	require.Empty(t, nativeProfileProbeURL(a, false))
	require.True(t, strings.HasPrefix(resolveCapabilityProfile(a), "custom:"))
	a = nativeProfileAccount(PlatformZhipu, AccountModeCoding, "https://api.z.ai/api/coding/paas/v4")
	require.Equal(t, "https://api.z.ai/api/v1", a.GetCNProtocolBaseURL(APIProtocolResponses))
	a.Credentials["api_base_urls"] = map[string]any{APIProtocolAnthropic: "https://relay.example/messages"}
	require.Empty(t, nativeProfileProbeURL(a, true))
}

func TestNativeProfileRequestAuthAndDiscovery(t *testing.T) {
	service := &AccountTestService{cfg: upstreamModelSyncTestConfig()}
	gateway := &OpenAIGatewayService{cfg: upstreamModelSyncTestConfig()}
	for _, p := range nativeProviderProfiles {
		t.Run(p.Label, func(t *testing.T) {
			base := p.Chat
			if base == "" {
				base = p.Responses
			}
			a := nativeProfileAccount(p.Platform, p.Mode, base)
			if p.Messages != "" {
				target, err := gateway.nativeAnthropicTargetURL(a)
				require.NoError(t, err)
				require.Equal(t, NativeAnthropicProfileTargetURL(p.Messages), target)
				req, _, err := gateway.buildNativeAnthropicUpstreamRequest(context.Background(), nil, a, []byte(`{"model":"fixture","messages":[]}`), "fixture-key", target)
				require.NoError(t, err)
				if p.Platform == PlatformDeepseek || (p.Platform == PlatformHunyuan && p.Mode == AccountModePayG) {
					require.Equal(t, "fixture-key", req.Header.Get("x-api-key"))
				} else {
					require.Equal(t, "Bearer fixture-key", req.Header.Get("Authorization"))
				}
			}
			if p.Responses != "" {
				req, err := gateway.buildUpstreamRequestOpenAIPassthrough(context.Background(), nil, a, []byte(`{"model":"fixture","input":"local"}`), "fixture-key")
				require.NoError(t, err, p.Label)
				require.Equal(t, strings.TrimRight(p.Responses, "/")+"/responses", req.URL.String(), p.Label)
				require.Equal(t, "Bearer fixture-key", req.Header.Get("Authorization"), p.Label)
			}
			req, err := service.buildUpstreamModelsRequest(context.Background(), a)
			if !p.Discovery {
				require.Error(t, err)
				require.Nil(t, req)
				return
			}
			require.NoError(t, err)
			require.Equal(t, "Bearer fixture-key", req.Header.Get("Authorization"))
			require.Equal(t, http.MethodGet, req.Method)
			require.True(t, strings.HasPrefix(req.URL.String(), strings.Split(p.Chat, "/")[0]+"//"+strings.Split(p.Chat, "/")[2]))
		})
	}
	a := nativeProfileAccount(PlatformKimi, AccountModeCoding, "https://api.kimi.ai/coding/v1")
	a.Credentials["api_protocol"] = APIProtocolAnthropic
	target, err := gateway.nativeAnthropicTargetURL(a)
	require.NoError(t, err)
	require.Equal(t, "https://api.kimi.ai/coding/v1/messages", target)
	a = nativeProfileAccount(PlatformKimi, AccountModePayG, "https://relay.example/anthropic")
	a.Credentials["api_protocol"] = APIProtocolAnthropic
	_, err = service.buildUpstreamModelsRequest(context.Background(), a)
	require.Error(t, err)
	require.Empty(t, nativeProfileProbeURL(a, false))
}

func TestNativeProfileUnknownProbesDoNotSend(t *testing.T) {
	a := nativeProfileAccount(PlatformMiniMax, AccountModeCoding, "https://api.minimax.cn/v1")
	quota := &CNProviderQuotaService{}
	_, err := quota.queryUsageForAccount(context.Background(), a)
	require.ErrorContains(t, err, "no supported quota endpoint")
	balance := &CNProviderBalanceService{}
	a = nativeProfileAccount(PlatformKimi, AccountModePayG, "https://relay.example/v1")
	_, err = balance.queryBalanceForAccount(context.Background(), a)
	require.ErrorContains(t, err, "no supported balance endpoint")
}

// Exercise the real native request builder over loopback. Only the connection
// address changes; the exact request path, Host and auth are observed on wire.
func TestNativeProfileLoopbackRequests(t *testing.T) {
	type observed struct{ host, path, bearer, key string }
	got := make(chan observed, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got <- observed{r.Host, r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("x-api-key")}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"fixture"}]}`))
	}))
	defer server.Close()
	local, err := url.Parse(server.URL)
	require.NoError(t, err)
	gateway := &OpenAIGatewayService{cfg: upstreamModelSyncTestConfig()}
	for _, p := range nativeProviderProfiles {
		if p.Messages == "" {
			continue
		}
		a := nativeProfileAccount(p.Platform, p.Mode, p.Messages)
		a.Credentials["api_protocol"] = APIProtocolAnthropic
		target, err := gateway.nativeAnthropicTargetURL(a)
		require.NoError(t, err, p.Label)
		req, _, err := gateway.buildNativeAnthropicUpstreamRequest(context.Background(), nil, a, []byte(`{"model":"fixture","messages":[{"role":"user","content":"local"}]}`), "fixture-key", target)
		require.NoError(t, err, p.Label)
		expectedHost, expectedPath := req.URL.Host, req.URL.Path
		req.Host = expectedHost
		req.URL.Scheme, req.URL.Host = local.Scheme, local.Host
		response, err := server.Client().Do(req)
		require.NoError(t, err, p.Label)
		response.Body.Close()
		actual := <-got
		require.Equal(t, expectedHost, actual.host, p.Label)
		require.Equal(t, expectedPath, actual.path, p.Label)
		if p.Platform == PlatformDeepseek || (p.Platform == PlatformHunyuan && p.Mode == AccountModePayG) {
			require.Equal(t, "fixture-key", actual.key, p.Label)
			require.Empty(t, actual.bearer, p.Label)
		} else {
			require.Equal(t, "Bearer fixture-key", actual.bearer, p.Label)
			require.Empty(t, actual.key, p.Label)
		}
	}
}

func TestNativeProfilePlanChatBuilders(t *testing.T) {
	gateway := &OpenAIGatewayService{cfg: upstreamModelSyncTestConfig()}
	body := []byte(`{"model":"fixture","messages":[{"role":"user","content":"local"}]}`)
	for _, p := range nativeProviderProfiles {
		if p.Chat == "" {
			continue
		}
		a := nativeProfileAccount(p.Platform, p.Mode, p.Chat)
		switch p.Platform {
		case PlatformQianfan:
			req, err := buildQianfanRequest(context.Background(), a, "chat/completions", body, gateway.validateUpstreamBaseURL)
			require.NoError(t, err, p.Label)
			require.Equal(t, p.Chat+"/chat/completions", req.URL.String(), p.Label)
			require.Equal(t, "Bearer fixture-key", req.Header.Get("Authorization"))
		case PlatformHunyuan:
			req, err := buildHunyuanChatRequest(context.Background(), a, body, "fixture")
			require.NoError(t, err, p.Label)
			require.Equal(t, p.Chat+"/chat/completions", req.URL.String(), p.Label)
			require.Equal(t, "Bearer fixture-key", req.Header.Get("Authorization"))
		case PlatformVolcengine:
			req, err := gateway.prepareVolcengineChatRequest(a, body, "fixture")
			require.NoError(t, err, p.Label)
			require.Equal(t, p.Chat+"/chat/completions", req.TargetURL, p.Label)
		}
	}
}

func TestNativeProfileMissingRootsNeverUseOpenAI(t *testing.T) {
	gateway := &OpenAIGatewayService{cfg: upstreamModelSyncTestConfig()}
	a := nativeProfileAccount(PlatformKimi, AccountModePayG, "")
	a.Credentials["api_base_urls"] = map[string]any{APIProtocolAnthropic: "https://private.example/messages"}
	_, err := gateway.buildUpstreamRequestOpenAIPassthrough(context.Background(), nil, a, []byte(`{}`), "fixture-key")
	require.ErrorContains(t, err, "no configured Responses endpoint")
	_, err = gateway.buildUpstreamRequest(context.Background(), nil, a, []byte(`{}`), "fixture-key", false, "", false)
	require.ErrorContains(t, err, "no configured Responses endpoint")
	_, err = gateway.openAIChatCompletionsTargetURL(a)
	require.ErrorContains(t, err, "no configured Chat endpoint")
	_, err = gateway.buildInputTokensUpstreamRequest(context.Background(), nil, a, []byte(`{}`), "fixture-key")
	require.ErrorContains(t, err, "no configured Responses endpoint")
	a.Credentials["api_base_urls"] = map[string]any{APIProtocolChatCompletions: "https://api.moonshot.ai/v1", APIProtocolAnthropic: "https://api.moonshot.cn/anthropic"}
	require.Empty(t, a.GetCNProtocolBaseURL(APIProtocolResponses))
}

// The international personal and enterprise products share inference roots.
// Only an explicit Responses selection can distinguish enterprise configuration.
func TestNativeProfileHunyuanIntlResponsesRequiresExplicitSelection(t *testing.T) {
	gateway := &OpenAIGatewayService{cfg: upstreamModelSyncTestConfig()}
	const intlChat = "https://tokenhub-intl.tencentcloudmaas.com/plan/v3"
	const intlMessages = "https://tokenhub-intl.tencentcloudmaas.com/plan/anthropic"
	body := []byte(`{"model":"fixture","input":"local"}`)
	for _, base := range []string{intlChat, ""} {
		t.Run("personal_"+base, func(t *testing.T) {
			account := nativeProfileAccount(PlatformHunyuan, AccountModeCoding, base)
			account.Credentials["api_base_urls"] = map[string]any{APIProtocolChatCompletions: intlChat, APIProtocolAnthropic: intlMessages}
			require.Empty(t, account.GetCNProtocolBaseURL(APIProtocolResponses))
			require.False(t, account.SupportsNativeCNResponses())
			require.False(t, account.UsesNativeCNResponses())
			request, err := gateway.buildUpstreamRequestOpenAIPassthrough(context.Background(), nil, account, body, "fixture-key")
			require.ErrorContains(t, err, "no configured Responses endpoint")
			require.Nil(t, request)
			_, err = gateway.buildUpstreamRequest(context.Background(), nil, account, body, "fixture-key", false, "", false)
			require.ErrorContains(t, err, "no configured Responses endpoint")
			chat, err := buildHunyuanChatRequest(context.Background(), account, []byte(`{"model":"fixture","messages":[{"role":"user","content":"local"}]}`), "fixture")
			require.NoError(t, err)
			require.Equal(t, intlChat+"/chat/completions", chat.URL.String())
			require.Equal(t, intlMessages, account.GetAnthropicProtocolBaseURL())
		})
		t.Run("explicit_enterprise_"+base, func(t *testing.T) {
			account := nativeProfileAccount(PlatformHunyuan, AccountModeCoding, base)
			account.Credentials["api_base_urls"] = map[string]any{APIProtocolChatCompletions: intlChat, APIProtocolAnthropic: intlMessages, APIProtocolResponses: intlChat}
			require.True(t, account.SupportsNativeCNResponses())
			require.True(t, account.UsesNativeCNResponses())
			request, err := gateway.buildUpstreamRequestOpenAIPassthrough(context.Background(), nil, account, body, "fixture-key")
			require.NoError(t, err)
			require.Equal(t, intlChat+"/responses", request.URL.String())
			require.Equal(t, "Bearer fixture-key", request.Header.Get("Authorization"))
		})
	}
	for _, fixture := range []struct{ name, base, messages, responses string }{
		{"CN personal", "https://api.lkeap.cloud.tencent.com/plan/v3", "https://api.lkeap.cloud.tencent.com/plan/anthropic", ""},
		{"CN enterprise", "https://tokenhub.tencentmaas.com/plan/v3", "https://tokenhub.tencentmaas.com/plan/anthropic", "https://tokenhub.tencentmaas.com/plan/v3"},
		{"mixed", intlChat, "https://tokenhub.tencentmaas.com/plan/anthropic", ""},
		{"custom messages only", "", "https://relay.example/messages", ""},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			account := nativeProfileAccount(PlatformHunyuan, AccountModeCoding, fixture.base)
			account.Credentials["api_base_urls"] = map[string]any{APIProtocolAnthropic: fixture.messages}
			require.Equal(t, fixture.responses, account.GetCNProtocolBaseURL(APIProtocolResponses))
			require.Equal(t, fixture.responses != "", account.SupportsNativeCNResponses())
		})
	}
}
