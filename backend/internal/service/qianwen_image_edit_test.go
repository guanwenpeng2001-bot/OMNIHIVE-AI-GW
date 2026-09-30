//go:build unit

package service

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

func TestQianwenStandardImageFailureHasNoBillableResult(t *testing.T) {
	for _, model := range []string{"qwen-image-edit-plus", "qwen-image-2.0"} {
		for _, test := range []struct {
			name, body string
			status     int
		}{
			{"upstream", `{"code":"InvalidParameter","message":"bad edit"}`, 400},
			{"empty", `{"output":{"choices":[]}}`, 200},
			{"usage mismatch", `{"output":{"choices":[{"message":{"content":[{"image":"data:image/png;base64,aGVsbG8="}]}}]},"usage":{"image_count":2}}`, 200},
			{"malformed", `{"output":{"choices":[{"message":{"content":[{"image":123}]}}]}}`, 200},
			{"unverified usage", `{"output":{"choices":[{"message":{"content":[{"image":"https://cdn.example.com/result.png"}]}}]},"usage":{"image_count":"1"}}`, 200},
		} {
			t.Run(model+"/"+test.name, func(t *testing.T) {
				upstream := &httpUpstreamRecorder{resp: qianwenModelPage(test.body, test.status)}
				s := &OpenAIGatewayService{httpUpstream: upstream}
				w := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(w)
				c.Request = httptest.NewRequest("POST", "/v1/images/edits", nil)
				body, err := buildQianwenStandardImageBody(model, "edit", "", 1, []string{"data:image/png;base64,aGVsbG8="})
				require.NoError(t, err)
				var result *OpenAIForwardResult
				if IsQianwenImageEditModel(model) {
					result, err = s.ForwardQianwenImageEdit(context.Background(), c, qianwenTestAccount(), body)
				} else {
					result, err = s.ForwardQianwenNativeImages(context.Background(), c, qianwenTestAccount(), body, "b64_json")
				}
				require.Error(t, err)
				require.Nil(t, result)
				require.GreaterOrEqual(t, w.Code, 400, w.Body.String())
				require.Len(t, upstream.requests, 1)
			})
		}
	}
}

type qianwenImageDownloadFailureTransport struct {
	*httpUpstreamRecorder
	downloadError error
}

func (transport *qianwenImageDownloadFailureTransport) Do(request *http.Request, proxyURL string, accountID int64, concurrency int) (*http.Response, error) {
	// Make elapsed-time assertions reliable on Windows' coarse timer resolution.
	time.Sleep(20 * time.Millisecond)
	if request.Method == http.MethodGet && transport.downloadError != nil {
		transport.requests = append(transport.requests, request)
		return nil, transport.downloadError
	}
	return transport.httpUpstreamRecorder.Do(request, proxyURL, accountID, concurrency)
}

func TestQianwenStandardImageConversionFailurePreservesObservedUsage(t *testing.T) {
	for _, model := range []string{"qwen-image-edit-plus", "qwen-image-2.0"} {
		for _, test := range []struct {
			name, image   string
			status        int
			downloadError error
			requests      int
		}{
			{name: "private", image: "https://127.0.0.1/private", requests: 1},
			{name: "expired", image: "https://cdn.example.com/result.png", status: 403, requests: 2},
			{name: "unreachable", image: "https://cdn.example.com/result.png", downloadError: errors.New("connection refused"), requests: 2},
			{name: "nonimage", image: "https://cdn.example.com/result.png", status: 200, requests: 2},
		} {
			t.Run(model+"/"+test.name, func(t *testing.T) {
				upstream := &qianwenImageDownloadFailureTransport{httpUpstreamRecorder: &httpUpstreamRecorder{responses: []*http.Response{
					qianwenModelPage(`{"request_id":"observed-image","output":{"choices":[{"message":{"content":[{"image":"`+test.image+`"}]}}]},"usage":{"image_count":1,"width":1328,"height":1328}}`, 200),
					b64BackfillImageResponse(test.status, "image/png", []byte("not an image")),
				}}, downloadError: test.downloadError}
				gateway := &OpenAIGatewayService{httpUpstream: upstream}
				account := qianwenTestAccount()
				body, err := buildQianwenStandardImageBody(model, "a blue cat", "1024x1024", 2, []string{"data:image/png;base64,iVBORw0KGgoAAAAA"})
				require.NoError(t, err)
				response := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(response)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
				var result *OpenAIForwardResult
				if IsQianwenImageEditModel(model) {
					result, err = gateway.ForwardQianwenImageEdit(context.Background(), c, account, body)
				} else {
					result, err = gateway.ForwardQianwenNativeImages(context.Background(), c, account, body, "b64_json")
				}
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, http.StatusBadGateway, response.Code)
				require.Equal(t, "Failed to retrieve DashScope image output", gjson.GetBytes(response.Body.Bytes(), "error.message").String())
				require.Equal(t, 1, result.ImageCount) // Requested two; only one was observed.
				require.Positive(t, result.Duration)
				require.Len(t, upstream.requests, test.requests)
				require.Equal(t, http.MethodPost, upstream.requests[0].Method)
				if test.requests == 2 {
					require.Equal(t, http.MethodGet, upstream.requests[1].Method)
					require.Empty(t, upstream.requests[1].Header.Get("Authorization"))
				}
				repo := &openAIRecordUsageLogRepoStub{inserted: true}
				billing := newOpenAIRecordUsageServiceForTest(repo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
				price := 0.05
				err = billing.RecordUsage(context.Background(), &OpenAIRecordUsageInput{Result: result, Account: account, User: &User{ID: 20123}, APIKey: &APIKey{ID: 10123, GroupID: i64p(123), Group: &Group{ID: 123, Platform: PlatformQianwen, RateMultiplier: 1, ImagePrice2K: &price}}})
				require.NoError(t, err)
				require.Equal(t, 1, repo.calls)
				require.Equal(t, 1, repo.lastLog.ImageCount)
				require.InDelta(t, price, repo.lastLog.TotalCost, 1e-12)
			})
		}
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
