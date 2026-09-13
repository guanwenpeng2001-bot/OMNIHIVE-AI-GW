//go:build unit

package service

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestQianwenNativeImageForwardAndCharge(t *testing.T) {
	for _, model := range []string{"qwen-image-2.0", "qwen-image-edit-plus"} {
		t.Run(model, func(t *testing.T) {
			response := `{"request_id":"image-native","output":{"choices":[{"message":{"content":[{"image":"https://example.com/1.png"},{"image":"https://example.com/2.png"}]},"finish_reason":"stop"}]},"usage":{"image_count":2,"width":1328,"height":1328}}`
			upstream := &httpUpstreamRecorder{resp: qianwenModelPage(response, 200)}
			s := &OpenAIGatewayService{httpUpstream: upstream}
			account := qianwenTestAccount()
			account.Credentials["model_mapping"] = map[string]any{"image-alias": model}
			body := []byte(`{"model":"image-alias","input":{"messages":[{"role":"user","content":[{"text":"Draw a cat"},{"image":"https://example.com/input.png"}]}]},"parameters":{"n":2,"size":"1024*1024","watermark":false}}`)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("POST", QianwenNativeMultimodalPath, nil)
			result, err := s.ForwardQianwenNative(context.Background(), c, account, QianwenNativeMultimodal, body, false)
			require.NoError(t, err)
			require.Equal(t, response, w.Body.String())
			require.Equal(t, 2, result.ImageCount)
			require.Equal(t, ImageBillingSize2K, result.ImageSize)
			require.Equal(t, ImageSizeSourceOutput, result.ImageSizeSource)
			require.Nil(t, result.ThinkingEnabled)
			require.False(t, gjson.GetBytes(upstream.bodies[0], "parameters.enable_thinking").Exists())
			require.Equal(t, model, gjson.GetBytes(upstream.bodies[0], "model").String())
			require.Equal(t, "https://example.com/input.png", gjson.GetBytes(upstream.bodies[0], "input.messages.0.content.1.image").String())
			require.Empty(t, upstream.requests[0].Header.Get("X-DashScope-Async"))
			require.Empty(t, upstream.requests[0].Header.Get("X-DashScope-SSE"))
			deadline, bounded := upstream.requests[0].Context().Deadline()
			require.True(t, bounded)
			require.LessOrEqual(t, time.Until(deadline), qianwenNativeImageTimeout)

			repo := &openAIRecordUsageLogRepoStub{inserted: true}
			billing := newOpenAIRecordUsageServiceForTest(repo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
			price := 0.05 // Fixture tariff, not a claim about Alibaba's live price.
			err = billing.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
				Result: result, Account: account, User: &User{ID: 20123},
				APIKey: &APIKey{ID: 10123, GroupID: i64p(123), Group: &Group{ID: 123, Platform: PlatformQianwen, RateMultiplier: 1, ImagePrice2K: &price}},
			})
			require.NoError(t, err)
			require.Equal(t, 2, repo.lastLog.ImageCount)
			require.InDelta(t, 0.10, repo.lastLog.TotalCost, 1e-12)
			require.InDelta(t, 0.10, repo.lastLog.ActualCost, 1e-12)
		})
	}
}

func TestQianwenNativeImageSuccessfulCountOnly(t *testing.T) {
	for _, tc := range []struct {
		name, response string
		want           int
	}{
		{"missing usage", `{"output":{"choices":[{"message":{"content":[{"image":"https://example.com/1.png"}]}}]}}`, 1},
		{"zero output", `{"usage":{"image_count":1},"output":{"choices":[]}}`, 0},
		{"mismatch", `{"usage":{"image_count":2},"output":{"choices":[{"message":{"content":[{"image":"https://example.com/1.png"}]}}]}}`, 0},
		{"fractional count", `{"usage":{"image_count":1.5},"output":{"choices":[{"message":{"content":[{"image":"https://example.com/1.png"}]}}]}}`, 0},
		{"job", `{"output":{"task_id":"unowned","task_status":"PENDING"}}`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := &OpenAIForwardResult{}
			err := observeQianwenNativeImage([]byte(tc.response), []byte(`{"parameters":{"n":6,"size":"1024*1024"}}`), result)
			if tc.want > 0 {
				require.NoError(t, err)
				require.Equal(t, ImageSizeSourceInput, result.ImageSizeSource)
			} else {
				require.Error(t, err)
			}
			require.Equal(t, tc.want, result.ImageCount)
		})
	}
}

func TestQianwenNativeImageMappedGuards(t *testing.T) {
	for _, tc := range []struct {
		model     string
		operation QianwenNativeOperation
		stream    bool
	}{
		{"qwen-image-plus", QianwenNativeText, false},
		{"qwen-image-edit", QianwenNativeMultimodal, true},
		{"qwen-image-3.0", QianwenNativeMultimodal, false},
		{"qwen-tts", QianwenNativeMultimodal, false},
		{"wan2.6-t2v", QianwenNativeMultimodal, false},
	} {
		account := qianwenTestAccount()
		account.Credentials["model_mapping"] = map[string]any{"alias": tc.model}
		s := &OpenAIGatewayService{}
		_, err := s.BuildQianwenNativeRequest(context.Background(), account, tc.operation, []byte(`{"model":"alias","input":{"messages":[{"content":[{"text":"hello"}]}]}}`), tc.stream)
		require.Error(t, err)
	}
}
