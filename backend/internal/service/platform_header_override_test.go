//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPlatformHeaderOverridesReachChatUpstream(t *testing.T) {
	for _, platform := range []string{PlatformQianwen, PlatformCommandCode} {
		t.Run(platform, func(t *testing.T) {
			account := &Account{ID: 901, Platform: platform, Type: AccountTypeAPIKey, Credentials: map[string]any{
				"api_key": "sk-original", "base_url": "https://relay.example/v1", "api_protocol": APIProtocolChatCompletions,
				credKeyHeaderOverrideEnabled: true,
				credKeyHeaderOverrides:       map[string]any{"User-Agent": "relay-client", "Authorization": "bad", "x-api-key": "bad"},
			}}
			body := []byte(`{"model":"qwen-flash","messages":[{"role":"user","content":"hi"}]}`)
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"test","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`))}}
			svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
			c := adaptiveProtocolTestContext("/v1/chat/completions", body)
			_, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")
			require.NoError(t, err)
			require.Equal(t, "relay-client", upstream.lastReq.Header.Get("User-Agent"))
			if platform == PlatformCommandCode {
				require.Equal(t, "sk-original", upstream.lastReq.Header.Get("x-api-key"))
				require.Empty(t, upstream.lastReq.Header.Get("Authorization"))
			} else {
				require.Equal(t, "Bearer sk-original", upstream.lastReq.Header.Get("Authorization"))
				require.NotEqual(t, "bad", upstream.lastReq.Header.Get("x-api-key"))
			}
		})
	}
}
