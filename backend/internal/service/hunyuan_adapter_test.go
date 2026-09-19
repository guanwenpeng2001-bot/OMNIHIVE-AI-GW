package service

import (
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// These are synthetic fixtures for the frozen compatibility contract, not
// authenticated vendor captures. Unsupported stream semantics stay unverified.
func TestHunyuanAdapterContract(t *testing.T) {
	newAccount := func() *Account {
		return &Account{Platform: "hunyuan", Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "synthetic-compatibility-key"}}
	}
	const chatBody = `{"model":"client-alias","messages":[{"role":"user","content":"hello"}]}`
	t.Run("legacy_endpoint_and_bearer", func(t *testing.T) {
		for _, baseURL := range []string{"", "https://api.hunyuan.cloud.tencent.com", hunyuanCompatibilityBaseURL, hunyuanCompatibilityBaseURL + "/"} {
			account := newAccount()
			account.Credentials["base_url"] = baseURL
			request, err := buildHunyuanChatRequest(context.Background(), account, []byte(chatBody), "hunyuan-turbos-latest")
			require.NoError(t, err)
			require.Equal(t, "https://api.hunyuan.cloud.tencent.com/v1/chat/completions", request.URL.String())
			require.Equal(t, http.MethodPost, request.Method)
			require.Equal(t, "Bearer synthetic-compatibility-key", request.Header.Get("Authorization"))
			require.Equal(t, "application/json", request.Header.Get("Accept"))
			require.Empty(t, request.Header.Get("X-TC-Action"))
			payload, err := io.ReadAll(request.Body)
			require.NoError(t, err)
			require.JSONEq(t, `{"model":"hunyuan-turbos-latest","messages":[{"role":"user","content":"hello"}]}`, string(payload))
		}
	})
	t.Run("tools_reasoning_and_exact_target_preserved", func(t *testing.T) {
		const body = `{"model":"literal-model-ID","messages":[{"role":"assistant","content":null,"reasoning_content":"separate reasoning","tool_calls":[{"id":"call_weather","type":"function","function":{"name":"weather","arguments":"{\"city\":\"深圳\"}"}}]},{"role":"tool","tool_call_id":"call_weather","content":"sunny"}],"tools":[{"type":"function","function":{"name":"weather","parameters":{"type":"object"}}}],"extension":{"value":9007199254740993}}`
		request, err := buildHunyuanChatRequest(context.Background(), newAccount(), []byte(body), "literal-model-ID")
		require.NoError(t, err)
		payload, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		require.Equal(t, body, string(payload))
	})
	t.Run("unconfirmed_actions_fail_before_transport", func(t *testing.T) {
		request, err := buildHunyuanChatRequest(context.Background(), newAccount(), []byte(`{"model":"target","messages":[{"role":"user","content":"hello"}],"stream":true}`), "target")
		require.ErrorIs(t, err, errHunyuanStreamUnsupported)
		require.Nil(t, request)
		models, err := hunyuanDiscoveredModels()
		require.ErrorIs(t, err, errHunyuanDiscoveryUnsupported)
		require.Nil(t, models)
	})
	t.Run("credential_and_product_boundaries", func(t *testing.T) {
		for _, baseURL := range []string{"https://tokenhub.tencent.com/v1", "https://hunyuan.tencentcloudapi.com", hunyuanCompatibilityBaseURL + "/v1", hunyuanCompatibilityBaseURL + "?secret=value", "https://api.hunyuan.cloud.tencent.com.attacker.invalid/v1"} {
			account := newAccount()
			account.Credentials["base_url"] = baseURL
			request, err := buildHunyuanChatRequest(context.Background(), account, []byte(chatBody), "target")
			require.Error(t, err)
			require.Nil(t, request)
			require.NotContains(t, err.Error(), baseURL)
		}
		for _, account := range []*Account{nil, {Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, {Platform: "hunyuan", Type: AccountTypeOAuth}, {Platform: "hunyuan", Type: AccountTypeAPIKey, Credentials: map[string]any{"secret_id": "synthetic", "secret_key": "synthetic"}}} {
			request, err := buildHunyuanChatRequest(context.Background(), account, []byte(chatBody), "target")
			require.Error(t, err)
			require.Nil(t, request)
		}
	})
	t.Run("invalid_request_rejected", func(t *testing.T) {
		for _, body := range []string{`null`, `{`, `{"model":"target","messages":[]}`, `{"messages":[{}]}`, `{"model":"target","messages":[{}],"stream":"true"}`} {
			request, err := buildHunyuanChatRequest(context.Background(), newAccount(), []byte(body), "target")
			require.Error(t, err)
			require.Nil(t, request)
		}
	})
	t.Run("request_cancellation_retained", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		request, err := buildHunyuanChatRequest(ctx, newAccount(), []byte(chatBody), "target")
		require.NoError(t, err)
		cancel()
		require.ErrorIs(t, request.Context().Err(), context.Canceled)
	})
	t.Run("documented_usage_and_raw_response_preserved", func(t *testing.T) {
		body := []byte(`{"choices":[{"message":{"content":"answer","reasoning_content":"reason","tool_calls":[{"id":"call_tool","function":{"arguments":"{}"}}]}}],"usage":{"prompt_tokens":19,"completion_tokens":7,"total_tokens":26,"prompt_tokens_details":{"cached_tokens":12}}}`)
		original := string(body)
		usage, err := parseHunyuanChatUsage(body)
		require.NoError(t, err)
		require.Equal(t, &OpenAIUsage{InputTokens: 19, OutputTokens: 7}, usage)
		require.Equal(t, original, string(body))
		for _, body := range []string{`{}`, `{"usage":null}`, `{"usage":{}}`, `{"usage":{"prompt_tokens":2,"total_tokens":2}}`} {
			usage, err := parseHunyuanChatUsage([]byte(body))
			require.NoError(t, err)
			require.Nil(t, usage, "unknown usage must not become known zero")
		}
		usage, err = parseHunyuanChatUsage([]byte(`{"usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}}`))
		require.NoError(t, err)
		require.NotNil(t, usage)
		for _, body := range []string{`{`, `{"usage":{"prompt_tokens":-1,"completion_tokens":1,"total_tokens":0}}`, `{"usage":{"prompt_tokens":1.5,"completion_tokens":1,"total_tokens":2}}`} {
			usage, err := parseHunyuanChatUsage([]byte(body))
			require.Error(t, err)
			require.Nil(t, usage)
		}
	})
	t.Run("errors_do_not_invent_model_or_balance_evidence", func(t *testing.T) {
		require.Equal(t, "account_authentication", hunyuanHTTPErrorScope(http.StatusUnauthorized))
		require.Equal(t, "rate_limit", hunyuanHTTPErrorScope(http.StatusTooManyRequests))
		for _, status := range []int{http.StatusBadRequest, http.StatusForbidden, http.StatusNotFound, http.StatusInternalServerError} {
			require.Equal(t, "request", hunyuanHTTPErrorScope(status))
		}
	})
}
