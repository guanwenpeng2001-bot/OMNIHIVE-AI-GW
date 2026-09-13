//go:build unit

package service

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestQianwenNativeVideoNeverBuildsUnownedTask(t *testing.T) {
	s := &OpenAIGatewayService{}
	account := qianwenTestAccount()
	for _, model := range []string{"wan2.1-t2v-turbo", "wan2.6-t2v", "wan2.7-videoedit"} {
		account.Credentials["model_mapping"] = map[string]any{"video-alias": model}
		for _, operation := range []QianwenNativeOperation{QianwenNativeText, QianwenNativeMultimodal, "video_generation"} {
			req, err := s.BuildQianwenNativeRequest(context.Background(), account, operation, []byte(`{"model":"video-alias","input":{"prompt":"A cat"}}`), false)
			require.Error(t, err)
			require.Nil(t, req)
		}
	}
	_, supported := QianwenNativeOperationForPath(QianwenNativeVideoPath)
	require.False(t, supported)
}

func TestQianwenNativeVideoAsyncResponseRejected(t *testing.T) {
	for _, state := range []string{"PENDING", "RUNNING", "SUCCEEDED", "FAILED", "CANCELED", "UNKNOWN"} {
		t.Run(state, func(t *testing.T) {
			body := []byte(`{"output":{"task_id":"job-1","task_status":"` + state + `"},"usage":{"video_duration":5,"video_count":1}}`)
			for repeat := 0; repeat < 2; repeat++ {
				result := &OpenAIForwardResult{}
				require.ErrorContains(t, observeQianwenNative(body, result), "unsupported DashScope asynchronous task")
				require.Zero(t, result.VideoCount)
				require.Zero(t, result.Usage.OutputTokens)
			}
		})
	}
	for _, body := range []string{`{"output":{"task_id":null}}`, `{"output":{"task_id":""}}`, `{"output":{"task_status":"PENDING"}}`} {
		require.Error(t, observeQianwenNative([]byte(body), &OpenAIForwardResult{}))
	}
	require.NoError(t, observeQianwenNative([]byte(`{"output":{"text":"task_id is just text"},"usage":{"output_tokens":3}}`), &OpenAIForwardResult{}))
}

func TestQianwenNativeVideoAsyncSuccessNotForwardedOrBilled(t *testing.T) {
	upstream := &httpUpstreamRecorder{resp: qianwenModelPage(`{"output":{"task_id":"unowned","task_status":"PENDING"}}`, 200)}
	s := &OpenAIGatewayService{httpUpstream: upstream}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", QianwenNativeTextPath, nil)
	result, err := s.ForwardQianwenNative(context.Background(), c, qianwenTestAccount(), QianwenNativeText, []byte(`{"model":"qwen-plus","input":{"prompt":"hello"}}`), false)
	require.ErrorContains(t, err, "unsupported DashScope asynchronous task")
	require.Nil(t, result)
	require.Empty(t, w.Body.String())
}

func TestQianwenNativeVideoSSETaskFrameRejectedPreservesPriorUsage(t *testing.T) {
	first := "data: {\"usage\":{\"input_tokens\":4,\"output_tokens\":2}}\n\n"
	upstream := &httpUpstreamRecorder{resp: qianwenModelPage(first+"data: {\"output\":{\"task_id\":\"unowned\",\"task_status\":\"PENDING\"}}\n\n", 200)}
	upstream.resp.Header.Set("Content-Type", "text/event-stream")
	s := &OpenAIGatewayService{httpUpstream: upstream}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", QianwenNativeTextPath, nil)
	result, err := s.ForwardQianwenNative(context.Background(), c, qianwenTestAccount(), QianwenNativeText, []byte(`{"model":"qwen-plus","input":{"prompt":"hello"}}`), true)
	require.ErrorContains(t, err, "unsupported DashScope asynchronous task")
	require.NotNil(t, result)
	require.Equal(t, 4, result.Usage.InputTokens)
	require.Equal(t, 2, result.Usage.OutputTokens)
	require.Zero(t, result.VideoCount)
	require.Equal(t, first, w.Body.String())
}
