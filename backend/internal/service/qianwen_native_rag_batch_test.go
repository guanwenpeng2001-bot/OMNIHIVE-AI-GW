package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestQianwenNativeResourceGateBoundaries(t *testing.T) {
	for _, tc := range []struct{ prefix, family string }{
		{"/api/v1/apps", "rag"}, {"/api/v1/indices", "rag"},
		{"/api/v1/services/aigc/rag", "rag"},
		{"/api/v1/files", "files"}, {"/compatible-mode/v1/files", "files"},
		{"/api/v1/services/aigc/files", "files"},
		{"/api/v1/batches", "batch"}, {"/compatible-mode/v1/batches", "batch"},
		{"/api/v1/services/aigc/batch", "batch"},
	} {
		for _, suffix := range []string{"", "/", "/foreign-id", "/foreign-id/content", "/foreign-id/cancel", "/unknown-action"} {
			t.Run(tc.prefix+suffix, func(t *testing.T) {
				gate, ok := QianwenNativeResourceGateForPath(tc.prefix + suffix)
				require.True(t, ok)
				require.Equal(t, tc.family, gate.Family)
				require.Equal(t, "qianwen_"+tc.family+"_unsupported", gate.Code)
				require.NotEmpty(t, gate.Reason)
			})
		}
		_, ok := QianwenNativeResourceGateForPath(tc.prefix + "-unrelated")
		require.False(t, ok)
	}
	for _, path := range []string{QianwenNativeTextPath, QianwenNativeMultimodalPath, "/api/v1/tasks/job", "/v1/images/batches", "/v1/chat/completions"} {
		_, ok := QianwenNativeResourceGateForPath(path)
		require.False(t, ok, path)
	}
}

func TestQianwenNativeResourceCannotBuildConversationRequest(t *testing.T) {
	s := &OpenAIGatewayService{}
	a := &Account{Platform: PlatformQianwen, Type: AccountTypeAPIKey}
	for _, op := range []QianwenNativeOperation{"rag", "files", "batch", "batches"} {
		req, err := s.BuildQianwenNativeRequest(context.Background(), a, op, []byte(`{"model":"qwen-plus","input":{"prompt":"hello"}}`), false)
		require.Nil(t, req)
		require.ErrorContains(t, err, "unsupported DashScope operation")
	}
}
