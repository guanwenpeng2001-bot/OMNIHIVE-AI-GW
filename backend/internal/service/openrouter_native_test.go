//go:build unit

package service

import (
	"bytes"
	"context"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenRouterNativeProtocolKeepsOpaqueFields(t *testing.T) {
	for _, protocol := range []string{"messages", "responses"} {
		t.Run(protocol, func(t *testing.T) {
			body := []byte(`{"model":"vendor/future:thinking","messages":[{"role":"assistant","content":[{"type":"redacted_thinking","data":"FAKE_OPAQUE_DATA"},{"type":"thinking","thinking":"fixture","signature":"FAKE_SIGNATURE"}]}],"max_tokens":64}`)
			reply := `{"id":"msg_provider","type":"message","role":"assistant","model":"vendor/future:thinking","content":[{"type":"redacted_thinking","data":"FAKE_OPAQUE_DATA"}],"stop_reason":"end_turn","usage":{"input_tokens":2,"output_tokens":1}}`
			if protocol == "responses" {
				body = []byte(`{"model":"vendor/future:thinking","input":[{"type":"reasoning","id":"rs_provider","encrypted_content":"FAKE_CIPHERTEXT","summary":[]}],"store":false}`)
				reply = `{"id":"resp_provider","object":"response","status":"completed","model":"vendor/future:thinking","output":[{"type":"reasoning","id":"rs_provider","encrypted_content":"FAKE_CIPHERTEXT","summary":[]}],"usage":{"input_tokens":2,"output_tokens":1}}`
			}
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(reply))}}
			gateway := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
			account := openRouterFixtureAccount()
			request := adaptiveProtocolTestContext("/v1/"+protocol, body)
			var result *OpenAIForwardResult
			var err error
			if protocol == "messages" {
				result, err = gateway.ForwardAsAnthropic(context.Background(), request, account, body, "", "")
			} else {
				result, err = gateway.Forward(context.Background(), request, account, body)
			}
			require.NoError(t, err)
			require.Equal(t, "https://openrouter.ai/api/v1/"+protocol, upstream.lastReq.URL.String())
			require.JSONEq(t, string(body), string(upstream.lastBody))
			require.Equal(t, 2, result.Usage.InputTokens)
			require.Equal(t, 1, result.Usage.OutputTokens)
		})
	}
}

func TestOpenRouterNativeAliasesHeadersAndFixedProtocols(t *testing.T) {
	for _, protocol := range []string{APIProtocolAnthropic, APIProtocolResponses} {
		t.Run(protocol, func(t *testing.T) {
			endpoint := "responses"
			reply := `{"id":"r","object":"response","status":"completed","model":"vendor/private:thinking","output":[],"usage":{"input_tokens":2,"output_tokens":1}}`
			body := []byte(`{"model":"public-alias","input":"hello"}`)
			if protocol == APIProtocolAnthropic {
				endpoint = "messages"
				reply = `{"id":"m","type":"message","role":"assistant","model":"vendor/private:thinking","content":[],"stop_reason":"end_turn","usage":{"input_tokens":2,"output_tokens":1}}`
				body = []byte(`{"model":"public-alias","max_tokens":64,"messages":[{"role":"user","content":"hello"}]}`)
			}
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(reply))}}
			gateway := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
			account := openRouterFixtureAccount()
			account.Credentials["api_protocol"] = protocol
			account.Credentials["base_url"] = "https://relay.example/prefix/v1"
			account.Credentials["model_mapping"] = map[string]any{"public-alias": "vendor/private:thinking"}
			proxyID := int64(9)
			account.ProxyID = &proxyID
			account.Proxy = &Proxy{Protocol: "http", Host: "proxy.example", Port: 8081}
			recorder := httptest.NewRecorder()
			request, _ := gin.CreateTestContext(recorder)
			request.Request = httptest.NewRequest("POST", "/v1/"+endpoint, bytes.NewReader(body))
			request.Request.Header.Set("anthropic-version", "2023-06-01")
			request.Request.Header.Set("x-api-key", "client-key-must-not-forward")
			result, err := gateway.forwardOpenRouterNative(context.Background(), request, account, body, protocol, "")
			require.NoError(t, err)
			require.Equal(t, "https://relay.example/prefix/v1/"+endpoint, upstream.lastReq.URL.String())
			require.Equal(t, "Bearer fixture-key", upstream.lastReq.Header.Get("Authorization"))
			require.Empty(t, upstream.lastReq.Header.Get("x-api-key"))
			require.Equal(t, "http://proxy.example:8081", upstream.lastProxyURL)
			if protocol == APIProtocolAnthropic {
				require.Equal(t, "2023-06-01", upstream.lastReq.Header.Get("anthropic-version"))
			}
			require.Equal(t, "vendor/private:thinking", gjson.GetBytes(upstream.lastBody, "model").String())
			require.Equal(t, "public-alias", gjson.Get(recorder.Body.String(), "model").String())
			require.Equal(t, "public-alias", result.Model)
			require.Equal(t, "vendor/private:thinking", result.BillingModel)
			require.Equal(t, "vendor/private:thinking", result.UpstreamResponseModel)
			account.Credentials["api_protocol"] = APIProtocolChatCompletions
			upstream.lastReq = nil
			_, err = gateway.forwardOpenRouterNative(context.Background(), request, account, body, protocol, "")
			require.Error(t, err)
			require.Nil(t, upstream.lastReq)
		})
	}
}

func TestOpenRouterNativeResponsesRelaysStateParameterRejection(t *testing.T) {
	body := []byte(`{"model":"vendor/future","input":"hello","store":true,"previous_response_id":"provider-prior"}`)
	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 400, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"store and previous_response_id unsupported"}}`))}}
	gateway := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	request := adaptiveProtocolTestContext("/v1/responses", body)
	_, err := gateway.Forward(context.Background(), request, openRouterFixtureAccount(), body)
	require.Error(t, err)
	require.JSONEq(t, string(body), string(upstream.lastBody))
	require.Equal(t, 400, request.Writer.Status())
}

func TestOpenRouterNativeTerminalStatesAndUnknownUsage(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, status := range []string{"completed", "incomplete", "failed"} {
			t.Run(fmt.Sprintf("%s/%t", status, stream), func(t *testing.T) {
				reply := fmt.Sprintf(`{"id":"resp_terminal","object":"response","model":"vendor/future","status":%q,"output":[],"usage":{"input_tokens":2,"output_tokens":1}}`, status)
				contentType := "application/json"
				if stream {
					contentType = "text/event-stream"
					reply = "event: response." + status + "\ndata: " + fmt.Sprintf(`{"type":"response.%s","response":%s}`, status, reply) + "\n\n"
				}
				upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(reply))}}
				gateway := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
				body := []byte(fmt.Sprintf(`{"model":"vendor/future","stream":%t,"input":"hi"}`, stream))
				recorder := httptest.NewRecorder()
				request, _ := gin.CreateTestContext(recorder)
				request.Request = httptest.NewRequest("POST", "/v1/responses", bytes.NewReader(body))
				result, err := gateway.Forward(context.Background(), request, openRouterFixtureAccount(), body)
				require.NotNil(t, result)
				require.Equal(t, 2, result.Usage.InputTokens)
				if status == "failed" {
					require.Error(t, err)
					_, _, transport := OpenAIUpstreamStreamReadErrorDetails(err)
					require.False(t, transport)
				} else {
					require.NoError(t, err)
				}
				require.Contains(t, recorder.Body.String(), `"status":"`+status+`"`)
				if status != "completed" {
					require.NotContains(t, recorder.Body.String(), `"status":"completed"`)
				}
			})
		}
	}
	for _, fixture := range []struct {
		protocol, contentType, reply string
		stream                       bool
	}{
		{APIProtocolResponses, "text/html", "<html>failure</html>", false},
		{APIProtocolAnthropic, "text/html", "<html>failure</html>", false},
		{APIProtocolResponses, "text/event-stream", "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"r\",\"status\":\"in_progress\"}}\n\n", true},
		{APIProtocolAnthropic, "text/event-stream", "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"m\",\"usage\":{\"input_tokens\":2}}}\n\n", true},
		{APIProtocolResponses, "application/json", `{"object":"response","id":"r","status":"completed","output":[]}`, false},
		{APIProtocolAnthropic, "application/json", `{"type":"message","id":"m","content":[],"stop_reason":"end_turn"}`, false},
	} {
		t.Run(fixture.protocol+fixture.reply, func(t *testing.T) {
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {fixture.contentType}}, Body: io.NopCloser(strings.NewReader(fixture.reply))}}
			gateway := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
			body := []byte(fmt.Sprintf(`{"model":"vendor/future","stream":%t,"input":"hello","max_tokens":64,"messages":[{"role":"user","content":"hello"}]}`, fixture.stream))
			recorder := httptest.NewRecorder()
			request, _ := gin.CreateTestContext(recorder)
			request.Request = httptest.NewRequest("POST", "/fixture", bytes.NewReader(body))
			result, err := gateway.forwardOpenRouterNative(context.Background(), request, openRouterFixtureAccount(), body, fixture.protocol, "")
			require.Error(t, err)
			require.Nil(t, result)
			require.NotContains(t, recorder.Body.String(), "response.completed")
			require.NotContains(t, recorder.Body.String(), "message_stop")
		})
	}
}

func TestOpenRouterNativeHTTPErrorClassification(t *testing.T) {
	for _, status := range []int{401, 402, 403, 429} {
		body := []byte(fmt.Sprintf(`{"error":{"code":%d,"message":"fixture","metadata":{"limit_source":"openrouter_in_flight_budget"}}}`, status))
		upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}, "Retry-After": {"3"}}, Body: io.NopCloser(bytes.NewReader(body))}}
		gateway := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
		request := adaptiveProtocolTestContext("/v1/responses", []byte(`{"model":"vendor/future","input":"hello"}`))
		result, err := gateway.Forward(context.Background(), request, openRouterFixtureAccount(), []byte(`{"model":"vendor/future","input":"hello"}`))
		require.Error(t, err)
		require.Nil(t, result)
		if status == 403 {
			require.Equal(t, 403, request.Writer.Status())
		} else {
			var failure *UpstreamFailoverError
			require.ErrorAs(t, err, &failure)
			require.Equal(t, status, failure.StatusCode)
			require.Equal(t, status == 402, failure.RequestScopedTransient)
		}
	}
}

func TestOpenRouterNativeMessagesCacheUsageAndPartialMetering(t *testing.T) {
	for _, terminal := range []bool{true, false} {
		reply := openRouterNativeTextFixture("messages", "vendor/future", true, 2, 1)
		reply = strings.Replace(reply, `"input_tokens":2,"output_tokens":0`, `"input_tokens":2,"output_tokens":0,"cache_read_input_tokens":3,"cache_creation_input_tokens":4`, 1)
		if !terminal {
			reply = strings.Replace(reply, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n", "", 1)
		}
		upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(reply))}}
		gateway := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
		body := []byte(`{"model":"vendor/future","stream":true,"max_tokens":64,"messages":[{"role":"user","content":"hi"}]}`)
		request := adaptiveProtocolTestContext("/v1/messages", body)
		result, err := gateway.ForwardAsAnthropic(context.Background(), request, openRouterFixtureAccount(), body, "", "")
		if terminal {
			require.NoError(t, err)
		} else {
			require.Error(t, err)
		}
		require.NotNil(t, result)
		require.Equal(t, 9, result.Usage.InputTokens)
		require.Equal(t, 3, result.Usage.CacheReadInputTokens)
		require.Equal(t, 4, result.Usage.CacheCreationInputTokens)
		require.Equal(t, 1, result.Usage.OutputTokens)
	}
}
