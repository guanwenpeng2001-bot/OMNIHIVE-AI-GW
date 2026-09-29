package handler

import (
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDuplicateModelHTTPIngressRejectedBeforeScheduling(t *testing.T) {
	h := newOpenAIImageChatRejectionHandler(t)
	for _, tc := range []struct {
		name, path string
		call       func(*gin.Context)
	}{
		{"raw_chat", "/v1/chat/completions", h.ChatCompletions},
		{"responses", "/v1/responses", h.Responses},
		{"anthropic", "/v1/messages", (&GatewayHandler{}).Messages},
		{"openai_messages", "/v1/messages", h.Messages},
	} {
		for _, tail := range []string{`"model":"revoked"`, `"MODEL":"revoked"`, `"mo\u0064el":null`} {
			t.Run(tc.name+tail, func(t *testing.T) {
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(`{"model":"allowed","input":"hello","messages":[],`+tail+`}`))
				setImageChatTestAuth(c)
				tc.call(c)
				require.Equal(t, http.StatusBadRequest, rec.Code)
				require.Equal(t, "invalid_request_error", gjson.Get(rec.Body.String(), "error.type").String())
				require.NotContains(t, rec.Body.String(), "revoked")
				require.NotContains(t, rec.Body.String(), "duplicate")
				_, selected := c.Get(opsAccountIDKey)
				require.False(t, selected)
			})
		}
	}
}
