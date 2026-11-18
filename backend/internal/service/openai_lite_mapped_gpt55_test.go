package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestMappedGPT55LiteCompatibility(t *testing.T) {
	for _, tc := range []struct {
		name     string
		id       int64
		model    string
		mapped   bool
		wantLite bool
	}{
		{"mapped", 12, "gpt-5.5", true, false},
		{"alternateMappedAccount", 13, "gpt-5.5", true, false},
		{"anotherMappedAccount", 14, "gpt-5.5", true, false},
		{"alternateNativeAccount", 13, "gpt-6-astra", false, true},
		{"anotherNativeAccount", 14, "gpt-6-astra", false, true},
		{"nativeSol", 12, "gpt-5.6-sol", true, true},
		{"noMapping", 12, "gpt-5.5", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := &Account{ID: tc.id, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{}}
			if tc.mapped {
				account.Credentials["model_mapping"] = map[string]any{"gpt-5.6-sol": "gpt-5.5"}
			}
			body := []byte(`{"model":"` + tc.model + `","input":[{"type":"additional_tools","role":"developer","tools":[]},{"type":"function_call_output","call_id":"call1","output":"12345678901234567890"}],"reasoning":{"context":"all_turns"},"client_metadata":{"ws_request_header_x_openai_internal_codex_responses_lite":"true","keep":"yes"}}`)
			request, err := http.NewRequest("POST", "http://localhost", bytes.NewReader(body))
			require.NoError(t, err)
			request.Header.Set(responsesLiteHeader, "true")
			require.NoError(t, applyMappedGPT55LiteCompatibility(request, account, body))
			require.Equal(t, tc.wantLite, isOpenAIResponsesLiteHeader(request.Header.Get(responsesLiteHeader)))
			got, err := io.ReadAll(request.Body)
			require.NoError(t, err)
			if tc.wantLite {
				require.Equal(t, body, got)
			} else {
				require.False(t, isOpenAIResponsesLiteWebSocketPayload(got))
				require.Equal(t, gjson.GetBytes(body, "input").Raw, gjson.GetBytes(got, "input").Raw)
				require.Equal(t, "all_turns", gjson.GetBytes(got, "reasoning.context").String())
				require.Equal(t, "yes", gjson.GetBytes(got, "client_metadata.keep").String())
				require.Equal(t, int64(len(got)), request.ContentLength)
				replay, err := request.GetBody()
				require.NoError(t, err)
				again, err := io.ReadAll(replay)
				require.NoError(t, err)
				require.Equal(t, got, again)
			}
			require.True(t, isOpenAIResponsesLiteWebSocketPayload(body))
		})
	}
}

func TestMappedGPT55LiteBuildersPreserveIngressForFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, passthrough := range []bool{false, true} {
		clientContext, _ := gin.CreateTestContext(httptest.NewRecorder())
		clientContext.Request = httptest.NewRequest("POST", "/v1/responses", nil)
		clientContext.Request.Header.Set(responsesLiteHeader, "true")
		gateway := &OpenAIGatewayService{cfg: &config.Config{}}
		account := &Account{ID: 12, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"model_mapping": map[string]any{"gpt-5.6-sol": "gpt-5.5"}}}
		body := []byte(`{"model":"gpt-5.5","stream":true,"input":[]}`)
		var request *http.Request
		var err error
		if passthrough {
			request, err = gateway.buildUpstreamRequestOpenAIPassthrough(context.Background(), clientContext, account, body, "test-token")
		} else {
			request, err = gateway.buildUpstreamRequest(context.Background(), clientContext, account, body, "test-token", true, "", true)
		}
		require.NoError(t, err)
		require.Empty(t, request.Header.Get(responsesLiteHeader))
		require.Equal(t, "true", clientContext.GetHeader(responsesLiteHeader))
		account.ID = 14
		body = []byte(`{"model":"gpt-6-astra","stream":true,"input":[]}`)
		if passthrough {
			request, err = gateway.buildUpstreamRequestOpenAIPassthrough(context.Background(), clientContext, account, body, "test-token")
		} else {
			request, err = gateway.buildUpstreamRequest(context.Background(), clientContext, account, body, "test-token", true, "", true)
		}
		require.NoError(t, err)
		require.Equal(t, "true", request.Header.Get(responsesLiteHeader))
	}
}
