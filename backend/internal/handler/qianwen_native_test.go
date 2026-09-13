//go:build unit

package handler

import (
	"net/http/httptest"
	"strings"
	"testing"

	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestQianwenNativeAuditInput(t *testing.T) {
	for _, tc := range []struct {
		body, text string
		images     []string
	}{
		{`{"input":{"prompt":"hello"}}`, "hello", nil},
		{`{"input":{"prompt":"safe"},"input":{"prompt":"actual"}}`, "actual", nil},
		{`{"input":{"messages":[{"role":"user","content":"safe","content":"actual"}]}}`, "actual", nil},
		{`{"input":{"messages":[{"role":"user","content":"hello"}]}}`, "hello", nil},
		{`{"input":{"messages":[{"role":"user","content":[{"text":"describe"},{"image":"https://example.com/dog.jpg"}]}]}}`, "describe", []string{"https://example.com/dog.jpg"}},
	} {
		input := service.ExtractContentModerationInput(service.ContentModerationProtocolOpenAIChat, qianwenNativeAuditBody([]byte(tc.body)))
		require.Equal(t, tc.text, input.Text)
		require.Equal(t, len(tc.images), len(input.Images))
		if len(tc.images) > 0 {
			require.Equal(t, tc.images, input.Images)
		}
	}
}

func TestQianwenNativeConflictingModelsRejectedBeforeAdmission(t *testing.T) {
	h := newOpenAIImageChatRejectionHandler(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", service.QianwenNativeTextPath, strings.NewReader(`{"model":"qwen-plus","model":"another-model","input":{"prompt":"hello"}}`))
	setImageChatTestAuth(c)
	key, _ := middleware.GetAPIKeyFromContext(c)
	key.Group = &service.Group{Platform: service.PlatformQianwen}
	h.QianwenNative(c)
	require.Equal(t, 400, w.Code)
	require.Contains(t, w.Body.String(), "Conflicting DashScope model fields")
	_, selected := c.Get(opsAccountIDKey)
	require.False(t, selected)
}
