//go:build unit

package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestResponsesChatLeadingRolesTargetProfile(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tt := range []struct {
		name, platform, body, want string
		chat, reject               bool
	}{
		{"qianwen merge", PlatformQianwen, `{"model":"qwen-turbo","instructions":"root","input":[{"role":"developer","content":"dev"},{"role":"user","content":"hi"}]}`, `[{"role":"system","content":"root\n\ndev"},{"role":"user","content":"hi"}]`, false, false},
		{"openai preserve", PlatformOpenAI, `{"model":"qwen-turbo","instructions":"root","input":[{"role":"developer","content":"dev"},{"role":"user","content":"hi"}]}`, `[{"role":"system","content":"root"},{"role":"developer","content":"dev"},{"role":"user","content":"hi"}]`, false, false},
		{"qianwen reject later system", PlatformQianwen, `{"model":"qwen-turbo","input":[{"role":"user","content":"hi"},{"role":"system","content":"later"}]}`, "", false, true},
		{"openai preserve later developer", PlatformOpenAI, `{"model":"qwen-turbo","input":[{"role":"user","content":"hi"},{"role":"developer","content":"later"}]}`, `[{"role":"user","content":"hi"},{"role":"developer","content":"later"}]`, false, false},
		{"native chat passthrough", PlatformQianwen, `{"model":"qwen-turbo","messages":[{"role":"system","content":"root"},{"role":"developer","content":"dev"},{"role":"user","content":"hi"}]}`, `[{"role":"system","content":"root"},{"role":"developer","content":"dev"},{"role":"user","content":"hi"}]`, true, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			body := []byte(tt.body)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}},
				Body: io.NopCloser(strings.NewReader(`{"id":"chatcmpl_roles","object":"chat.completion","model":"qwen-turbo","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`)),
			}}
			svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
			account := rawChatCompletionsTestAccount()
			account.Platform = tt.platform
			account.Credentials["api_protocol"] = APIProtocolChatCompletions
			var err error
			if tt.chat {
				_, err = svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")
			} else {
				_, err = svc.forwardResponsesViaRawChatCompletions(context.Background(), c, account, body)
			}
			if tt.reject {
				require.ErrorContains(t, err, "mid-conversation")
				require.Equal(t, http.StatusBadRequest, rec.Code)
				require.Nil(t, upstream.lastReq, "unsupported input must fail before upstream consumption")
				return
			}
			require.NoError(t, err, rec.Body.String())
			require.NotNil(t, upstream.lastReq)
			require.JSONEq(t, tt.want, gjson.GetBytes(upstream.lastBody, "messages").Raw)
		})
	}
}
