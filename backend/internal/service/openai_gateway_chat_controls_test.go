//go:build unit

package service

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestChatFallbacksForwardOutputAndSamplingControls(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name    string
		path    string
		body    string
		forward func(*OpenAIGatewayService, *gin.Context, *Account, []byte) error
	}{
		{
			name: "responses", path: "/v1/responses",
			body: `{"model":"gpt-5.4","input":"hello","max_output_tokens":2,"temperature":0.25,"top_p":0.75}`,
			forward: func(svc *OpenAIGatewayService, c *gin.Context, account *Account, body []byte) error {
				_, err := svc.Forward(context.Background(), c, account, body)
				return err
			},
		},
		{
			name: "messages", path: "/v1/messages",
			body: `{"model":"claude-sonnet-4","messages":[{"role":"user","content":"hello"}],"max_tokens":2,"stop_sequences":["STOP"],"temperature":0.25,"top_p":0.75}`,
			forward: func(svc *OpenAIGatewayService, c *gin.Context, account *Account, body []byte) error {
				_, err := svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "")
				return err
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(tc.body)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, tc.path, bytes.NewReader(body))
			upstream := &httpUpstreamRecorder{err: errors.New("stop after capture")}
			svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
			account := forceChatResponsesFallbackAccount()
			if tc.path == "/v1/messages" {
				account = forceChatMessagesFallbackAccount()
			}
			require.Error(t, tc.forward(svc, c, account, body))
			require.Equal(t, int64(2), gjson.GetBytes(upstream.lastBody, "max_tokens").Int())
			require.Equal(t, 0.25, gjson.GetBytes(upstream.lastBody, "temperature").Float())
			require.Equal(t, 0.75, gjson.GetBytes(upstream.lastBody, "top_p").Float())
			if tc.path == "/v1/messages" {
				require.Equal(t, "STOP", gjson.GetBytes(upstream.lastBody, "stop.0").String())
			}
		})
	}
}
