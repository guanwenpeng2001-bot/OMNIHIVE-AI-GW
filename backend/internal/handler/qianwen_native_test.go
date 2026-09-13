//go:build unit

package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestQianwenNativeAuditInput(t *testing.T) {
	for _, tc := range []struct {
		body, text string
		images     []string
	}{
		{`{"input":{"prompt":"hello"}}`, "hello", nil},
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
