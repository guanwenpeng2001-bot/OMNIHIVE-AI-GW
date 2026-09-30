//go:build unit

package service

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestQianwenImageEditForwardAndBilling(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString(b64BackfillPNGBytes)
	upstream := &httpUpstreamRecorder{resp: qianwenModelPage(`{"request_id":"edit-output","output":{"choices":[{"message":{"content":[{"image":"data:image/png;base64,`+encoded+`"}]}}]},"usage":{"image_count":1,"width":1328,"height":1328}}`, 200)}
	s := &OpenAIGatewayService{httpUpstream: upstream}
	account := qianwenTestAccount()
	account.Credentials["model_mapping"] = map[string]any{"public-edit": "qwen-image-edit-plus"}
	body, err := BuildQianwenImageEditBody("public-edit", "make it blue", "1024x1024", 2, []string{"data:image/png;base64," + encoded})
	require.NoError(t, err)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/v1/images/edits", nil)
	result, err := s.ForwardQianwenImageEdit(context.Background(), c, account, body)
	require.NoError(t, err)
	require.Equal(t, 200, w.Code)
	require.Equal(t, encoded, gjson.GetBytes(w.Body.Bytes(), "data.0.b64_json").String())
	require.True(t, gjson.GetBytes(w.Body.Bytes(), "created").Int() > 0)
	require.Equal(t, 1, result.ImageCount) // Actual output, not requested n=2.
	require.Equal(t, "public-edit", result.Model)
	require.Equal(t, "qwen-image-edit-plus", result.UpstreamModel)
	require.Equal(t, QianwenNativeMultimodalPath, upstream.requests[0].URL.Path)
	require.Equal(t, "qwen-image-edit-plus", gjson.GetBytes(upstream.bodies[0], "model").String())
	require.Empty(t, upstream.requests[0].Header.Get("X-DashScope-Async"))
	require.Empty(t, upstream.requests[0].Header.Get("X-DashScope-SSE"))
	repo := &openAIRecordUsageLogRepoStub{inserted: true}
	billing := newOpenAIRecordUsageServiceForTest(repo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
	price := 0.05
	err = billing.RecordUsage(context.Background(), &OpenAIRecordUsageInput{Result: result, Account: account, User: &User{ID: 20123}, APIKey: &APIKey{ID: 10123, GroupID: i64p(123), Group: &Group{ID: 123, Platform: PlatformQianwen, RateMultiplier: 1, ImagePrice2K: &price}}})
	require.NoError(t, err)
	require.Equal(t, 1, repo.lastLog.ImageCount)
	require.InDelta(t, price, repo.lastLog.TotalCost, 1e-12)
}

func TestQianwenImageEditFailureHasNoBillableResult(t *testing.T) {
	for _, test := range []struct {
		name, body string
		status     int
	}{
		{"upstream", `{"code":"InvalidParameter","message":"bad edit"}`, 400},
		{"empty", `{"output":{"choices":[]}}`, 200},
		{"usage mismatch", `{"output":{"choices":[{"message":{"content":[{"image":"data:image/png;base64,aGVsbG8="}]}}]},"usage":{"image_count":2}}`, 200},
		{"private download", `{"output":{"choices":[{"message":{"content":[{"image":"http://127.0.0.1/private"}]}}]}}`, 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			upstream := &httpUpstreamRecorder{resp: qianwenModelPage(test.body, test.status)}
			s := &OpenAIGatewayService{httpUpstream: upstream}
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("POST", "/v1/images/edits", nil)
			body, err := BuildQianwenImageEditBody("qwen-image-edit-plus", "edit", "", 1, []string{"data:image/png;base64,aGVsbG8="})
			require.NoError(t, err)
			result, err := s.ForwardQianwenImageEdit(context.Background(), c, qianwenTestAccount(), body)
			require.Error(t, err)
			require.Nil(t, result)
			require.GreaterOrEqual(t, w.Code, 400, w.Body.String())
			require.Len(t, upstream.requests, 1)
		})
	}
}

func TestQianwenStandardNativeGeneration(t *testing.T) {
	for _, model := range []string{"qwen-image-2.0", "qwen-image-2.0-private"} {
		body, err := BuildQianwenImageGenerationBody([]byte(`{"model":"` + model + `","prompt":"draw a cat","n":1,"size":"3072x512"}`))
		require.NoError(t, err)
		require.Equal(t, "3072*512", gjson.GetBytes(body, "parameters.size").String())
		require.Len(t, gjson.GetBytes(body, "input.messages.0.content").Array(), 1)
		require.False(t, gjson.GetBytes(body, "input.messages.0.content.0.image").Exists())
		upstream := &httpUpstreamRecorder{resp: qianwenModelPage(`{"output":{"choices":[{"message":{"content":[{"image":"https://example.com/result.png"}]}}]}}`, 200)}
		s := &OpenAIGatewayService{httpUpstream: upstream}
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/v1/images/generations", nil)
		result, err := s.ForwardQianwenNativeImages(context.Background(), c, qianwenTestAccount(), body, "url")
		require.NoError(t, err)
		require.Equal(t, 1, result.ImageCount)
		require.Equal(t, "https://example.com/result.png", gjson.GetBytes(w.Body.Bytes(), "data.0.url").String())
		require.Equal(t, model, gjson.GetBytes(upstream.bodies[0], "model").String())
	}
}

func TestQianwenImageEditDownloadsOutputWithoutCredentials(t *testing.T) {
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		qianwenModelPage(`{"output":{"choices":[{"message":{"content":[{"image":"https://cdn.example.com/result.png"}]}}]},"usage":{"image_count":1}}`, 200),
		b64BackfillImageResponse(200, "image/png", b64BackfillPNGBytes),
	}}
	s := &OpenAIGatewayService{httpUpstream: upstream}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/v1/images/edits", nil)
	body, err := BuildQianwenImageEditBody("qwen-image-edit-plus", "edit", "", 1, []string{"data:image/png;base64,iVBORw0KGgoAAAAA"})
	require.NoError(t, err)
	result, err := s.ForwardQianwenImageEdit(context.Background(), c, qianwenTestAccount(), body)
	require.NoError(t, err)
	require.Equal(t, 1, result.ImageCount)
	require.Len(t, upstream.requests, 2)
	require.Equal(t, http.MethodGet, upstream.requests[1].Method)
	require.Empty(t, upstream.requests[1].Header.Get("Authorization"))
	require.Equal(t, base64.StdEncoding.EncodeToString(b64BackfillPNGBytes), gjson.GetBytes(w.Body.Bytes(), "data.0.b64_json").String())
}
