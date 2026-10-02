//go:build unit

package service

import (
	"context"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestOpenRouterResponsesInputTokensNeverCallsNativeResponses(t *testing.T) {
	for _, base := range []string{"", "https://relay.example/v1"} {
		upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 401, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"must not be called"}}`))}}
		gateway := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
		account := openRouterFixtureAccount()
		account.Credentials["base_url"] = base
		body := []byte(`{"model":"openai/gpt-5-high:free","input":"hello world"}`)
		request := adaptiveProtocolTestContext("/v1/responses/input_tokens", body)
		require.NoError(t, gateway.ForwardResponsesInputTokens(context.Background(), request, account, body))
		require.Nil(t, upstream.lastReq)
	}
}
