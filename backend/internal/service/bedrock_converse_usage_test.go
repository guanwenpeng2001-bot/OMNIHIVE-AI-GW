//go:build unit

package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAuditC05BedrockRejectsUnknownUsage(test *testing.T) {
	for name, usage := range map[string]string{
		"absent": "", "empty": `,"usage":{}`, "missing_input": `,"usage":{"outputTokens":4}`,
		"missing_output": `,"usage":{"inputTokens":2}`, "negative_input": `,"usage":{"inputTokens":-2,"outputTokens":4}`,
		"negative_output":     `,"usage":{"inputTokens":2,"outputTokens":-4}`,
		"fraction":            `,"usage":{"inputTokens":2.5,"outputTokens":4}`,
		"string":              `,"usage":{"inputTokens":"2","outputTokens":4}`,
		"null":                `,"usage":{"inputTokens":null,"outputTokens":4}`,
		"boolean":             `,"usage":{"inputTokens":true,"outputTokens":4}`,
		"overflow":            `,"usage":{"inputTokens":9223372036854775808,"outputTokens":4}`,
		"invalid_cache":       `,"usage":{"inputTokens":2,"outputTokens":4,"cacheReadInputTokens":-1}`,
		"invalid_write_cache": `,"usage":{"inputTokens":2,"outputTokens":4,"cacheWriteInputTokens":1.5}`,
		"invalid_total":       `,"usage":{"inputTokens":2,"outputTokens":4,"totalTokens":-1}`,
	} {
		for _, stream := range []bool{false, true} {
			test.Run(fmt.Sprintf("%s/stream=%t", name, stream), func(test *testing.T) {
				target := "amazon.nova-fixture"
				account := catalogEvidenceAccount(901, PlatformBedrock, target)
				account.Type = AccountTypeBedrock
				snapshot := account.GetUpstreamModelCatalog()
				snapshot.EligibleModels[0].Endpoints = []string{CompositeRouteEndpointMessages}
				account.Extra[UpstreamModelCatalogExtraKey] = snapshot
				account.Credentials = map[string]any{"api_key": "audit-fixture", "auth_mode": "apikey", "aws_region": "eu-west-1"}
				body := []byte(fmt.Sprintf(`{"model":"amazon.nova-fixture","max_tokens":100,"messages":[{"role":"user","content":"hello"}],"stream":%t}`, stream))
				response := `{"output":{"message":{"role":"assistant","content":[{"text":"hello"}]}},"stopReason":"end_turn"` + usage + `}`
				if stream {
					frames := bedrockContractFrame("messageStart", "event", `{"role":"assistant"}`)
					frames = append(frames, bedrockContractFrame("messageStop", "event", `{"stopReason":"end_turn"}`)...)
					frames = append(frames, bedrockContractFrame("metadata", "event", `{`+strings.TrimPrefix(usage, ",")+`}`)...)
					response = string(frames)
				}
				transport := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(response))}}
				gateway := &GatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: transport}
				recorder := httptest.NewRecorder()
				requestContext, _ := gin.CreateTestContext(recorder)
				requestContext.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
				parsed, err := ParseGatewayRequest(NewRequestBodyRef(body), "anthropic")
				require.NoError(test, err)
				accepted := 0
				parsed.OnUpstreamAccepted = func() { accepted++ }
				result, err := gateway.Forward(context.Background(), requestContext, &account, parsed)
				test.Logf("status=%d result=%+v error=%v", recorder.Code, result, err)
				require.Error(test, err, "unknown or invalid usage must not become a trusted successful settlement result")
				require.Nil(test, result)
				require.Equal(test, 1, accepted)
				require.Len(test, transport.requests, 1)
				var retry *UpstreamFailoverError
				require.False(test, errors.As(err, &retry), "accepted unknown usage must not invite automatic retry")
				require.NotContains(test, recorder.Body.String(), "event: message_stop")
			})
		}
	}
}

func TestAuditC05AcceptedStreamFailuresAreNotRetryable(t *testing.T) {
	start := bedrockContractFrame("messageStart", "event", `{"role":"assistant"}`)
	stopped := append(append([]byte{}, start...), bedrockContractFrame("messageStop", "event", `{"stopReason":"end_turn"}`)...)
	metadata := append(append([]byte{}, stopped...), bedrockContractFrame("metadata", "event", `{"usage":{"inputTokens":2,"outputTokens":4}}`)...)
	for name, frames := range map[string][]byte{
		"before_output":                   []byte("invalid frame"),
		"missing_metadata":                stopped,
		"corruption_after_valid_metadata": append(append([]byte{}, metadata...), []byte("invalid frame")...),
		"duplicate_metadata":              append(append([]byte{}, metadata...), bedrockContractFrame("metadata", "event", `{"usage":{"inputTokens":2,"outputTokens":4}}`)...),
	} {
		t.Run(name, func(t *testing.T) {
			account := catalogEvidenceAccount(902, PlatformBedrock, "amazon.nova-fixture")
			account.Type = AccountTypeBedrock
			snapshot := account.GetUpstreamModelCatalog()
			snapshot.EligibleModels[0].Endpoints = []string{CompositeRouteEndpointMessages}
			account.Extra[UpstreamModelCatalogExtraKey] = snapshot
			account.Credentials = map[string]any{"api_key": "fixture", "auth_mode": "apikey", "aws_region": "eu-west-1"}
			body := []byte(`{"model":"amazon.nova-fixture","max_tokens":100,"messages":[{"role":"user","content":"hello"}],"stream":true}`)
			transport := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(frames))}}
			gateway := &GatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: transport}
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
			parsed, err := ParseGatewayRequest(NewRequestBodyRef(body), "anthropic")
			require.NoError(t, err)
			result, err := gateway.Forward(context.Background(), c, &account, parsed)
			require.Error(t, err)
			require.Nil(t, result)
			var retry *UpstreamFailoverError
			require.False(t, errors.As(err, &retry))
			require.Len(t, transport.requests, 1)
			require.NotContains(t, recorder.Body.String(), "event: message_stop")
		})
	}
}

func TestAuditC05KnownZeroUsageIsValid(t *testing.T) {
	response := []byte(`{"output":{"message":{"role":"assistant","content":[]}},"stopReason":"end_turn","usage":{"inputTokens":0,"outputTokens":0,"cacheReadInputTokens":0,"cacheWriteInputTokens":0}}`)
	converted, err := ConvertBedrockConverseResponse(response, "id", "model")
	require.NoError(t, err)
	usage := parseClaudeUsageFromResponseBody(converted)
	require.Equal(t, 0, usage.InputTokens)
	require.Equal(t, 0, usage.OutputTokens)
	frames := bedrockContractFrame("messageStart", "event", `{"role":"assistant"}`)
	frames = append(frames, bedrockContractFrame("messageStop", "event", `{"stopReason":"end_turn"}`)...)
	frames = append(frames, bedrockContractFrame("metadata", "event", `{"usage":{"inputTokens":0,"outputTokens":0}}`)...)
	streamUsage, err := ConvertBedrockConverseStream(context.Background(), io.NopCloser(bytes.NewReader(frames)), io.Discard, "id", "model")
	require.NoError(t, err)
	require.Equal(t, int64(0), streamUsage["input_tokens"])
	require.Equal(t, int64(0), streamUsage["output_tokens"])
}
