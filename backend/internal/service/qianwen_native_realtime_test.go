//go:build unit

package service

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Realtime must not escape its gate through a native conversation model alias.
func TestQianwenNativeRealtimeMappedModelsNeverForward(t *testing.T) {
	for _, model := range []string{"qwen-omni-turbo-realtime", "qwen3-omni-flash-realtime", "qwen3.5-omni-plus-realtime", "qwen3-asr-flash-realtime", "qwen3-tts-flash-realtime"} {
		for _, operation := range []QianwenNativeOperation{QianwenNativeText, QianwenNativeMultimodal} {
			for _, stream := range []bool{false, true} {
				t.Run(model+string(operation)+map[bool]string{false: "/json", true: "/sse"}[stream], func(t *testing.T) {
					account := qianwenTestAccount()
					account.Credentials["model_mapping"] = map[string]any{"alias": model}
					body := []byte(`{"model":"alias","input":{"prompt":"hello"}}`)
					s := &OpenAIGatewayService{}
					req, err := s.BuildQianwenNativeRequest(context.Background(), account, operation, body, stream)
					// Guard order changed when the audio family landed: audio-mapped
					// models are now rejected by the audio gate first, which is the
					// more precise message. Either structured rejection is acceptable.
					if err != nil {
						require.ErrorContains(t, err, "unsupported DashScope")
					} else {
						t.Fatalf("expected a structured rejection for %s", model)
					}
					require.Nil(t, req)
					w := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(w)
					c.Request = httptest.NewRequest("POST", QianwenNativeMultimodalPath, nil)
					result, err := s.ForwardQianwenNative(context.Background(), c, account, operation, body, stream)
					require.ErrorContains(t, err, "unsupported DashScope model family")
					require.Nil(t, result)
					require.Equal(t, 400, w.Code)
					require.Contains(t, w.Body.String(), "invalid_request_error")
				})
			}
		}
	}
}
