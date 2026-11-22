package service

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// These W02 compatibility checks also run without the unit build tag.
func TestRawChatBufferedReceiptCompatibility(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		valid      bool
	}{
		{"wrapped zero", `{"data":{"usage":{"prompt_tokens":0,"completion_tokens":0}}}`, true},
		{"empty choices with usage", `{"object":"chat.completion","choices":[],"usage":{"prompt_tokens":7,"completion_tokens":2}}`, true},
		{"missing usage", `{"choices":[]}`, false},
		{"partial usage", `{"data":{"usage":{"prompt_tokens":0}}}`, false},
		{"invalid usage", `{"data":{"usage":{"prompt_tokens":-1,"completion_tokens":0}}}`, false},
		{"invalid choice", `{"choices":[{}],"usage":{"prompt_tokens":0,"completion_tokens":0}}`, false},
		{"wrapped error", `{"data":{"error":{"message":"private diagnostic"},"usage":{"prompt_tokens":0,"completion_tokens":0}}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			resp := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(tc.body))}
			result, err := (&OpenAIGatewayService{}).bufferRawChatCompletions(c, resp, &Account{Platform: PlatformOpenAI}, "m", "m", "m", nil, nil, time.Now())
			if tc.valid {
				require.NoError(t, err)
				require.True(t, result.TokenUsageObservation.known())
				require.Equal(t, tc.body, rec.Body.String())
			} else {
				require.Error(t, err)
				require.Nil(t, result)
				require.Equal(t, http.StatusBadGateway, rec.Code)
				require.NotContains(t, rec.Body.String(), "private diagnostic")
			}
		})
	}
}

func TestLegacyOAuthEmptyTerminatorCompatibility(t *testing.T) {
	const done = "data: [DONE]\n\n"
	for _, tc := range []struct {
		name, body  string
		accountType string
		passthrough bool
		valid       bool
	}{
		{"legacy empty", done, AccountTypeOAuth, false, true},
		{"legacy comments and CRLF", ": keepalive\r\n\r\ndata:[DONE]\r\n\r\n", AccountTypeOAuth, false, true},
		{"empty SSE", "", AccountTypeOAuth, false, false},
		{"comments only", ": keepalive\n\n", AccountTypeOAuth, false, false},
		{"error event", "event: error\n" + done, AccountTypeOAuth, false, false},
		{"partial then done", "data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n" + done, AccountTypeOAuth, false, false},
		{"usage then done", "data: {\"type\":\"response.in_progress\",\"usage\":{\"input_tokens\":7,\"output_tokens\":2}}\n\n" + done, AccountTypeOAuth, false, false},
		{"missing response then done", "data: {\"type\":\"response.completed\"}\n\n" + done, AccountTypeOAuth, false, false},
		{"malformed then done", "data: {invalid}\n\n" + done, AccountTypeOAuth, false, false},
		{"API key", done, AccountTypeAPIKey, false, false},
		{"explicit passthrough", done, AccountTypeOAuth, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}}
			account := &Account{Platform: PlatformOpenAI, Type: tc.accountType}
			result, err := (&OpenAIGatewayService{}).handleBufferedResponsesSSE(resp, c, account, []byte(tc.body), "m", "m", tc.passthrough)
			require.NotNil(t, result)
			if tc.valid {
				require.NoError(t, err)
				require.Equal(t, http.StatusOK, rec.Code)
				require.Equal(t, tc.body, rec.Body.String())
				require.False(t, result.observation.known(), "an empty terminator must never become trusted-zero usage")
			} else {
				require.Error(t, err)
				require.Equal(t, http.StatusBadGateway, rec.Code)
				require.NotContains(t, rec.Body.String(), "data:")
			}
		})
	}
}
