//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestDeepSeekContextSuffixScope(t *testing.T) {
	for _, tc := range []struct {
		name, base string
		normalize  bool
	}{
		{"default", "", true},
		{"official", "https://api.deepseek.com/v1", true},
		{"anthropic", "https://api.deepseek.com/anthropic/", true},
		{"custom", "https://private.example/v1", false},
		{"lookalike", "https://api.deepseek.com.example/v1", false},
		{"private path", "https://api.deepseek.com/private", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &Account{Platform: PlatformDeepseek, Type: AccountTypeAPIKey, Credentials: map[string]any{"base_url": tc.base}}
			for _, model := range []string{"deepseek-flash[1m]", "deepseek-flash[1M][1m]", "deepseek-flash"} {
				want := model
				if tc.normalize {
					want = "deepseek-flash"
				}
				require.Equal(t, want, normalizeOpenAIModelForUpstream(a, model))
			}
			require.True(t, a.IsModelSupported("private-unknown"), "empty mapping must remain unrestricted")
		})
	}
	a := &Account{Platform: PlatformDeepseek, Type: AccountTypeAPIKey, Credentials: map[string]any{
		"model_mapping": map[string]any{"alias": "private[1m]"},
	}}
	require.True(t, a.IsModelSupported("alias"))
	require.False(t, a.IsModelSupported("other"))
	require.Equal(t, "private[1m]", normalizeOpenAIModelForUpstream(a, a.GetMappedModel("alias")))
	a.Platform = PlatformOpenAI
	a.Extra = map[string]any{"openai_passthrough": true}
	require.True(t, a.IsModelSupported("other"), "passthrough outranks stale mapping")
	require.Equal(t, "deepseek-flash[1m]", normalizeOpenAIModelForUpstream(a, "deepseek-flash[1m]"))
	a.Credentials = map[string]any{}
	require.Equal(t, "deepseek-flash[1m]", normalizeOpenAIModelForUpstream(a, "deepseek-flash[1m]"))
	a.Extra = nil
	a.Platform = PlatformDeepseek
	a.Credentials["api_base_urls"] = map[string]any{APIProtocolAnthropic: "https://private.example/anthropic"}
	require.Equal(t, "deepseek-flash[1m]", normalizeOpenAIModelForUpstream(a, "deepseek-flash[1m]"))
	for _, platform := range []string{PlatformOpenAI, PlatformKimi, PlatformQianwen} {
		a.Platform = platform
		require.Equal(t, "deepseek-flash[1m]", normalizeOpenAIModelForUpstream(a, "deepseek-flash[1m]"))
	}
}

func TestDeepSeekCompatibilityPreservesQianwenRetirement(t *testing.T) {
	for _, mapping := range []map[string]any{
		nil,
		{"alias": "qwen1.5-72b-chat", "qwen1.5-72b-chat": "current-model"},
	} {
		a := &Account{Platform: PlatformQianwen, Type: AccountTypeAPIKey, Credentials: map[string]any{"model_mapping": mapping}}
		require.False(t, a.IsModelSupported("qwen1.5-72b-chat"), "retired input must stay blocked even when mapped to a current model")
		if mapping != nil {
			require.False(t, a.IsModelSupported("alias"), "mapping to a retired model must stay blocked")
		}
		require.Equal(t, "qwen1.5-72b-chat[1m]", normalizeOpenAIModelForUpstream(a, "qwen1.5-72b-chat[1m]"))
	}
}

const deepSeekMediaRequest = `{"model":"deepseek-flash[1m]","store":true,"previous_response_id":"resp_old","metadata":{"integer":9007199254740993,"decimal":0.1234567890123456789},"input":[{"type":"function_call","call_id":"call_A","name":"view_image","arguments":"{}"},{"type":"custom_tool_call","call_id":"call_B","name":"view_image","input":"image"},{"type":"function_call_output","call_id":"call_A","output":[{"type":"input_text","text":"image A","number":9007199254740993},{"type":"input_image","image_url":"data:image/png;base64,QQ=="}]},{"type":"message","role":"system","content":"notice"},{"type":"custom_tool_call_output","call_id":"call_B","output":[{"type":"input_image","image_url":"data:image/png;base64,Qg=="}]}]}`

func TestDeepSeekToolMediaNormalization(t *testing.T) {
	for _, protocol := range []string{APIProtocolResponses, APIProtocolAdaptive} {
		a := &Account{Platform: PlatformDeepseek, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_protocol": protocol}}
		body := []byte(deepSeekMediaRequest)
		out := normalizeDeepSeekResponsesRequestBody(a, body)
		require.Equal(t, deepSeekMediaRequest, string(body))
		require.False(t, gjson.GetBytes(out, "store").Bool())
		require.False(t, gjson.GetBytes(out, "previous_response_id").Exists())
		require.Equal(t, "9007199254740993", gjson.GetBytes(out, "metadata.integer").Raw)
		require.Equal(t, "0.1234567890123456789", gjson.GetBytes(out, "metadata.decimal").Raw)
		require.Contains(t, gjson.GetBytes(out, "input.2.output").String(), "image A")
		require.Contains(t, gjson.GetBytes(out, "input.2.output").String(), "9007199254740993")
		require.Equal(t, "call_A", gjson.GetBytes(out, "input.2.call_id").String())
		require.Equal(t, "call_B", gjson.GetBytes(out, "input.3.call_id").String())
		require.Equal(t, gjson.String, gjson.GetBytes(out, "input.3.output").Type)
		require.Equal(t, "user", gjson.GetBytes(out, "input.4.role").String())
		require.Equal(t, "[Tool output media for call call_A]", gjson.GetBytes(out, "input.4.content.0.text").String())
		require.Equal(t, "[Tool output media for call call_B]", gjson.GetBytes(out, "input.4.content.2.text").String())
		require.Equal(t, "system", gjson.GetBytes(out, "input.5.role").String())
		require.Equal(t, out, normalizeDeepSeekResponsesRequestBody(a, out), "retry must not duplicate lifted media")
	}
	for _, platform := range []string{PlatformOpenAI, PlatformKimi, PlatformQianwen} {
		a := &Account{Platform: platform, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_protocol": APIProtocolResponses}}
		out := normalizeDeepSeekResponsesRequestBody(a, []byte(deepSeekMediaRequest))
		require.Equal(t, gjson.Get(deepSeekMediaRequest, "input").Raw, gjson.GetBytes(out, "input").Raw)
	}
	a := &Account{Platform: PlatformDeepseek, Type: AccountTypeAPIKey}
	require.Equal(t, deepSeekMediaRequest, string(normalizeDeepSeekResponsesRequestBody(a, []byte(deepSeekMediaRequest))))
	a.Credentials = map[string]any{"api_protocol": APIProtocolResponses}
	plain := []byte(`{ "store":false, "input":[{"type":"function_call_output","call_id":"a","output":"plain"},{"role":"developer","content":"notice"},{"type":"function_call_output","call_id":"b","output":"plain"}] }`)
	require.Equal(t, plain, normalizeDeepSeekResponsesRequestBody(a, plain))
}

func TestDeepSeekToolMediaFailedForwardRetry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(deepSeekMediaRequest)
	upstream := &httpUpstreamRecorder{err: errors.New("injected upstream failure")}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	a := officialDeepSeekTestAccount(9401)
	a.Credentials["api_protocol"] = APIProtocolResponses
	_, err := svc.Forward(context.Background(), adaptiveProtocolTestContext("/v1/responses", body), a, body)
	require.Error(t, err)
	require.Equal(t, "user", gjson.GetBytes(upstream.lastBody, "input.4.role").String())
	require.Equal(t, "deepseek-flash", gjson.GetBytes(upstream.lastBody, "model").String())
	require.Equal(t, deepSeekMediaRequest, string(body))
	first := append([]byte(nil), upstream.lastBody...)
	_, err = svc.Forward(context.Background(), adaptiveProtocolTestContext("/v1/responses", body), a, body)
	require.Error(t, err)
	require.JSONEq(t, string(first), string(upstream.lastBody))
	a.Platform = PlatformOpenAI
	a.Credentials["base_url"] = "https://api.openai.com"
	a.Extra = map[string]any{"openai_passthrough": true}
	_, err = svc.Forward(context.Background(), adaptiveProtocolTestContext("/v1/responses", body), a, body)
	require.Error(t, err)
	require.Equal(t, gjson.Get(deepSeekMediaRequest, "input").Raw, gjson.GetBytes(upstream.lastBody, "input").Raw)
	require.Equal(t, "deepseek-flash[1m]", gjson.GetBytes(upstream.lastBody, "model").String())
}
