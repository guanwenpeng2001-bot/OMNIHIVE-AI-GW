//go:build unit

package service

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestMessagesCodexSessionIndependentOfModel(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, model := range []string{"gpt-6.1-sol", "gpt-6-astra", "future-model", "claude-sonnet-4-5"} {
			t.Run(fmt.Sprintf("%s/stream=%v", model, stream), func(t *testing.T) {
				account := newOpenAIOAuthNamespaceTestAccount()
				upstream := &httpUpstreamRecorder{}
				svc := newOpenAIRejectedFieldTestService(upstream)
				for index, step := range []struct {
					tenant                       int64
					session, explicit, wantState string
				}{
					{7, "claude-session", "", ""}, {7, "claude-session", "", "state"},
					{8, "claude-session", "", ""}, {7, "other-session", "", ""},
					{7, "claude-session", "caller-one", ""}, {7, "other-session", "caller-one", "state"},
					{7, "claude-session", "caller-two", ""},
				} {
					resp := openAICompatSSECompletedResponse("response", model)
					resp.Header.Set("x-codex-turn-state", "state")
					upstream.responses = append(upstream.responses, resp)
					body := []byte(fmt.Sprintf(`{"model":%q,"max_tokens":16,"stream":%v,"messages":[{"role":"user","content":"hello"}]}`, model, stream))
					c := newOpenAIRejectedFieldTestContext(body)
					c.Set("api_key", &APIKey{ID: step.tenant})
					c.Request.Header.Set(claudeCodeSessionHeader, step.session)
					mappedModel := ""
					if model == "claude-sonnet-4-5" {
						mappedModel = "gpt-6.1-sol"
					}
					_, err := svc.ForwardAsAnthropic(context.Background(), c, account, body, step.explicit, mappedModel)
					require.NoError(t, err)
					seed := step.explicit
					if seed == "" {
						seed = step.session
					}
					require.Equal(t, generateSessionUUID(isolateOpenAIUpstreamSessionID(step.tenant, account, seed)), upstream.requests[index].Header.Get("session_id"))
					require.Equal(t, step.wantState, upstream.requests[index].Header.Get("x-codex-turn-state"))
					require.False(t, gjson.GetBytes(upstream.bodies[index], "prompt_cache_key").Exists())
					require.False(t, gjson.GetBytes(upstream.bodies[index], "previous_response_id").Exists())
				}
			})
		}
	}
}

func TestPublicResponsesPreservesCallerCacheOptions(t *testing.T) {
	body := []byte(`{"model":"gpt-6.1-sol","input":"hello","prompt_cache_options":{"mode":"implicit","ttl":"30m"}}`)
	account := newOpenAIRejectedFieldTestAccount()
	account.Credentials["base_url"] = "https://api.openai.com"
	upstream := &httpUpstreamRecorder{responses: []*http.Response{newOpenAIRejectedFieldTestResponse(200, `{"output":[],"usage":{"input_tokens":1,"output_tokens":1}}`)}}
	_, err := newOpenAIRejectedFieldTestService(upstream).Forward(context.Background(), newOpenAIRejectedFieldTestContext(body), account, body)
	require.NoError(t, err)
	require.JSONEq(t, gjson.GetBytes(body, "prompt_cache_options").Raw, gjson.GetBytes(upstream.bodies[0], "prompt_cache_options").Raw)
}

func TestRejectedContentBreakpointClearsEmptyExplicitMode(t *testing.T) {
	body := []byte(`{"input":[{"role":"user","content":[{"type":"input_text","text":"hello","prompt_cache_breakpoint":{"mode":"explicit"}}]}],"prompt_cache_options":{"mode":"explicit","ttl":"30m"}}`)
	retry, _, changed, err := normalizeOpenAIResponsesRejectedFieldRetryBody(400, body, []byte(`{"error":{"code":"unsupported_parameter","param":"input[0].content[0].prompt_cache_breakpoint"}}`))
	require.NoError(t, err)
	require.True(t, changed)
	require.False(t, gjson.GetBytes(retry, "input.0.content.0.prompt_cache_breakpoint").Exists())
	require.False(t, gjson.GetBytes(retry, "prompt_cache_options.mode").Exists())
	require.Equal(t, "30m", gjson.GetBytes(retry, "prompt_cache_options.ttl").String())
}

func TestMessagesPublicCacheBoundariesAtUpstream(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, transport := range []string{"public", "oauth", "third-party"} {
			t.Run(fmt.Sprintf("%s/stream=%v", transport, stream), func(t *testing.T) {
				account := newOpenAIRejectedFieldTestAccount()
				if transport == "public" {
					account.Credentials["base_url"] = "https://api.openai.com/v1"
				}
				if transport == "oauth" {
					account = newOpenAIOAuthNamespaceTestAccount()
				}
				body := []byte(fmt.Sprintf(`{"model":"gpt-6.1-sol","max_tokens":16,"stream":%v,"system":[{"type":"text","text":"system","cache_control":{"type":"ephemeral","ttl":"1h"}}],"messages":[{"role":"user","content":[{"type":"text","text":"hello","cache_control":{"type":"ephemeral","ttl":"5m"}}]}]}`, stream))
				upstream := &httpUpstreamRecorder{responses: []*http.Response{openAICompatSSECompletedResponse("response", "gpt-6.1-sol")}}
				_, err := newOpenAIRejectedFieldTestService(upstream).ForwardAsAnthropic(context.Background(), newOpenAIRejectedFieldTestContext(body), account, body, "", "")
				require.NoError(t, err)
				if transport == "public" {
					require.Equal(t, "explicit", gjson.GetBytes(upstream.bodies[0], "input.0.content.0.prompt_cache_breakpoint.mode").String())
					require.Equal(t, "explicit", gjson.GetBytes(upstream.bodies[0], "input.1.content.0.prompt_cache_breakpoint.mode").String())
					require.Equal(t, "explicit", gjson.GetBytes(upstream.bodies[0], "prompt_cache_options.mode").String())
				} else {
					require.NotContains(t, string(upstream.bodies[0]), "prompt_cache_breakpoint")
					require.False(t, gjson.GetBytes(upstream.bodies[0], "prompt_cache_options").Exists())
				}
				require.False(t, gjson.GetBytes(upstream.bodies[0], "prompt_cache_options.ttl").Exists())
				require.False(t, gjson.GetBytes(upstream.bodies[0], "prompt_cache_retention").Exists())
			})
		}
	}
}

func TestMessagesPublicCacheRetryAndContinuation(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			account := newOpenAIRejectedFieldTestAccount()
			account.Credentials["base_url"] = "https://api.openai.com"
			body := []byte(fmt.Sprintf(`{"model":"gpt-5.4","max_tokens":16,"stream":%v,"messages":[{"role":"user","content":[{"type":"text","text":"hello","cache_control":{"type":"ephemeral"}}]}]}`, stream))
			upstream := &httpUpstreamRecorder{responses: []*http.Response{
				newOpenAIRejectedFieldTestResponse(400, `{"error":{"code":"unsupported_parameter","param":"input[1].content[0].prompt_cache_breakpoint"}}`),
				openAICompatSSECompletedResponse("first", "gpt-5.4"),
				openAICompatSSECompletedResponse("second", "gpt-5.4"),
			}}
			svc := newOpenAIRejectedFieldTestService(upstream)
			_, err := svc.ForwardAsAnthropic(context.Background(), newOpenAIRejectedFieldTestContext(body), account, body, "explicit-key", "")
			require.NoError(t, err)
			require.Len(t, upstream.bodies, 2)
			require.Equal(t, "explicit", gjson.GetBytes(upstream.bodies[0], "prompt_cache_options.mode").String())
			require.False(t, gjson.GetBytes(upstream.bodies[1], "prompt_cache_options.mode").Exists())
			require.NotContains(t, string(upstream.bodies[1]), "prompt_cache_breakpoint")
			require.Equal(t, "hello", gjson.GetBytes(upstream.bodies[1], "input.1.content.0.text").String())
			callerOptions := ""
			if stream {
				callerOptions = `"prompt_cache_options":{"mode":"explicit","ttl":"30m"},`
			}
			next := []byte(fmt.Sprintf(`{%s"model":"gpt-5.4","max_tokens":16,"stream":%v,"system":[{"type":"text","text":"old-boundary","cache_control":{"type":"ephemeral"}}],"messages":[{"role":"user","content":"hello"},{"role":"assistant","content":"ok"},{"role":"user","content":"new-turn"}]}`, callerOptions, stream))
			_, err = svc.ForwardAsAnthropic(context.Background(), newOpenAIRejectedFieldTestContext(next), account, next, "explicit-key", "")
			require.NoError(t, err)
			require.Equal(t, "first", gjson.GetBytes(upstream.bodies[2], "previous_response_id").String())
			require.False(t, gjson.GetBytes(upstream.bodies[2], "prompt_cache_options.mode").Exists())
			require.NotContains(t, string(upstream.bodies[2]), "old-boundary")
		})
	}
}

func TestMessagesPublicCallerOptionsOverrideMapping(t *testing.T) {
	account := newOpenAIRejectedFieldTestAccount()
	account.Credentials["base_url"] = "https://api.openai.com"
	body := []byte(`{"model":"gpt-6.1-sol","max_tokens":16,"prompt_cache_options":{"mode":"implicit","ttl":"30m"},"messages":[{"role":"user","content":[{"type":"text","text":"hello","cache_control":{"type":"ephemeral"}}]}]}`)
	upstream := &httpUpstreamRecorder{responses: []*http.Response{openAICompatSSECompletedResponse("response", "gpt-6.1-sol")}}
	_, err := newOpenAIRejectedFieldTestService(upstream).ForwardAsAnthropic(context.Background(), newOpenAIRejectedFieldTestContext(body), account, body, "", "")
	require.NoError(t, err)
	require.JSONEq(t, gjson.GetBytes(body, "prompt_cache_options").Raw, gjson.GetBytes(upstream.bodies[0], "prompt_cache_options").Raw)
}

func TestMessagesFutureModelsDoNotGainLegacyDigestFallback(t *testing.T) {
	for _, model := range []string{"gpt-6.1-sol", "future-model"} {
		for _, oauth := range []bool{false, true} {
			account := newOpenAIRejectedFieldTestAccount()
			if oauth {
				account = newOpenAIOAuthNamespaceTestAccount()
			}
			body := []byte(fmt.Sprintf(`{"model":%q,"max_tokens":16,"messages":[{"role":"user","content":"hello"}]}`, model))
			upstream := &httpUpstreamRecorder{responses: []*http.Response{openAICompatSSECompletedResponse("response", model)}}
			_, err := newOpenAIRejectedFieldTestService(upstream).ForwardAsAnthropic(context.Background(), newOpenAIRejectedFieldTestContext(body), account, body, "", "")
			require.NoError(t, err)
			require.Empty(t, upstream.requests[0].Header.Get("session_id"))
			require.False(t, gjson.GetBytes(upstream.bodies[0], "prompt_cache_key").Exists())
			require.False(t, gjson.GetBytes(upstream.bodies[0], "previous_response_id").Exists())
		}
	}
}

func TestRejectedCacheBoundaryPreservesOtherMarkersAndOptions(t *testing.T) {
	body := []byte(`{"input":[{"role":"user","content":[{"type":"input_text","text":"first","prompt_cache_breakpoint":{"mode":"explicit"}},{"type":"input_text","text":"second","prompt_cache_breakpoint":{"mode":"explicit"}}]}],"prompt_cache_options":{"mode":"explicit","ttl":"30m"}}`)
	for _, response := range []string{
		`{"error":{"code":"unsupported_parameter","param":"input[0].content[0].prompt_cache_breakpoint"}}`,
		`{"error":{"message":"Unsupported parameter: 'input[0].content[0].prompt_cache_breakpoint'."}}`,
		`{"error":{"message":"input[0].content[0].prompt_cache_breakpoint is not supported on this model"}}`,
	} {
		retry, _, changed, err := normalizeOpenAIResponsesRejectedFieldRetryBody(400, body, []byte(response))
		require.NoError(t, err)
		require.True(t, changed)
		require.False(t, gjson.GetBytes(retry, "input.0.content.0.prompt_cache_breakpoint").Exists())
		require.Equal(t, "explicit", gjson.GetBytes(retry, "input.0.content.1.prompt_cache_breakpoint.mode").String())
		require.JSONEq(t, `{"mode":"explicit","ttl":"30m"}`, gjson.GetBytes(retry, "prompt_cache_options").Raw)
	}
}

func TestPublicResponsesCacheBreakpointRetry(t *testing.T) {
	for _, stream := range []bool{false, true} {
		body := []byte(fmt.Sprintf(`{"model":"gpt-6.1-sol","stream":%v,"input":[{"role":"user","content":[{"type":"input_text","text":"hello","prompt_cache_breakpoint":{"mode":"explicit"}}]}],"prompt_cache_options":{"mode":"explicit","ttl":"30m"}}`, stream))
		account := newOpenAIRejectedFieldTestAccount()
		account.Credentials["base_url"] = "https://api.openai.com"
		success := newOpenAIRejectedFieldTestResponse(200, `{"output":[],"usage":{"input_tokens":1,"output_tokens":1}}`)
		if stream {
			success = openAICompatSSECompletedResponse("response", "gpt-6.1-sol")
		}
		upstream := &httpUpstreamRecorder{responses: []*http.Response{newOpenAIRejectedFieldTestResponse(400, `{"error":{"code":"unsupported_parameter","param":"input[0].content[0].prompt_cache_breakpoint"}}`), success}}
		_, err := newOpenAIRejectedFieldTestService(upstream).Forward(context.Background(), newOpenAIRejectedFieldTestContext(body), account, body)
		require.NoError(t, err)
		require.Len(t, upstream.bodies, 2)
		require.Equal(t, "explicit", gjson.GetBytes(upstream.bodies[0], "prompt_cache_options.mode").String())
		require.False(t, gjson.GetBytes(upstream.bodies[1], "prompt_cache_options.mode").Exists())
		require.False(t, gjson.GetBytes(upstream.bodies[1], "input.0.content.0.prompt_cache_breakpoint").Exists())
		require.Equal(t, "30m", gjson.GetBytes(upstream.bodies[1], "prompt_cache_options.ttl").String())
	}
}
