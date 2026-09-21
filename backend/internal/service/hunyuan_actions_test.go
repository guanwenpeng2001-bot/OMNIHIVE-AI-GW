//go:build unit

package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// hunyuanActionUpstreamStub records requests and replays canned responses. It
// honours the request context so client cancellation stays observable.
type hunyuanActionUpstreamStub struct {
	mu        sync.Mutex
	requests  []*http.Request
	bodies    [][]byte
	responses []*http.Response
	err       error
}

func (s *hunyuanActionUpstreamStub) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	s.mu.Lock()
	s.requests = append(s.requests, req)
	if req.Body != nil {
		body, _ := io.ReadAll(req.Body)
		s.bodies = append(s.bodies, body)
	} else {
		s.bodies = append(s.bodies, nil)
	}
	s.mu.Unlock()
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	if s.err != nil {
		return nil, s.err
	}
	if len(s.responses) == 0 {
		return nil, errors.New("no canned response")
	}
	resp := s.responses[0]
	s.responses = s.responses[1:]
	return resp, nil
}

func (s *hunyuanActionUpstreamStub) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return s.Do(req, proxyURL, accountID, accountConcurrency)
}

func (s *hunyuanActionUpstreamStub) requestCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.requests)
}

func (s *hunyuanActionUpstreamStub) recordedRequest(index int) (*http.Request, []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests[index], s.bodies[index]
}

func hunyuanActionJSONResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func hunyuanActionBinaryResponse(status int, body string, contentType string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{contentType}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func hunyuanActionTestAccount() *Account {
	return &Account{
		ID:       21,
		Platform: PlatformHunyuan,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "hunyuan-upstream-key",
		},
	}
}

func hunyuanActionTestContext(t *testing.T, method, target string, headers map[string]string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(method, target, nil)
	for name, value := range headers {
		c.Request.Header.Set(name, value)
	}
	return c, recorder
}

func hunyuanActionTestService(upstream HTTPUpstream) *OpenAIGatewayService {
	return &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
}

// TestHunyuanActionEndpointPaths pins every native path and authentication
// header against the saved first-party TokenHub documentation (2026-09-21).
func TestHunyuanActionEndpointPaths(t *testing.T) {
	cases := []struct {
		endpoint    HunyuanActionEndpoint
		taskID      string
		expectedURL string
		expectedMth string
		authHeader  string
		authValue   string
		response    string
	}{
		{HunyuanEndpointImageGeneration, "", "https://tokenhub.tencentmaas.com/v1/wand/hunyuan-image/v3-generation", http.MethodPost, "Authorization", "Bearer hunyuan-upstream-key", `{"id":"x","created":1,"data":[{"url":"https://cos.example.com/a.png"}]}`},
		{HunyuanEndpointImagePanorama, "", "https://tokenhub.tencentmaas.com/v1/wand/hunyuan-image/world2-panorama", http.MethodPost, "Authorization", "Bearer hunyuan-upstream-key", `{"id":"x","created":1,"status":"succeeded","data":[{"super_resolution_image_url":"https://cos.example.com/p.png"}]}`},
		{HunyuanEndpointVideoGeneration, "", "https://tokenhub.tencentmaas.com/v1/wand/hunyuan-video/generation", http.MethodPost, "Authorization", "Bearer hunyuan-upstream-key", `{"id":"accepted","status":"queued"}`},
		{HunyuanEndpointVideoScene, "", "https://tokenhub.tencentmaas.com/v1/wand/hunyuan-video/world2-scene", http.MethodPost, "Authorization", "Bearer hunyuan-upstream-key", `{"id":"accepted","status":"queued"}`},
		{HunyuanEndpointVideoTask, "task-42", "https://tokenhub.tencentmaas.com/v1/wand/hunyuan-video/tasks/task-42", http.MethodGet, "Authorization", "Bearer hunyuan-upstream-key", `{"id":"accepted","status":"queued"}`},
		{HunyuanEndpointTranscribeSync, "", "https://tokenhub.tencentmaas.com/v1/wand/asrproxy/sync_transcribe", http.MethodPost, "Authorization", "Bearer hunyuan-upstream-key", `{"id":"accepted","status":"queued"}`},
		{HunyuanEndpointTranscribeAsync, "", "https://tokenhub.tencentmaas.com/v1/wand/asrproxy/async_create", http.MethodPost, "Authorization", "Bearer hunyuan-upstream-key", `{"id":"accepted","status":"queued"}`},
		{HunyuanEndpointTranscribeTask, "asr-7", "https://tokenhub.tencentmaas.com/v1/wand/asrproxy/tasks/asr-7", http.MethodGet, "Authorization", "Bearer hunyuan-upstream-key", `{"id":"accepted","status":"queued"}`},
		{HunyuanEndpointSpeechSync, "", "https://tokenhub.tencentmaas.com/v1/wand/minimax-tts/sync_tts", http.MethodPost, "Authorization", "Bearer hunyuan-upstream-key", `{"id":"accepted","status":"queued"}`},
		{HunyuanEndpointVoiceClone, "", "https://tokenhub.tencentmaas.com/v1/wand/minimax-tts/sync_clone", http.MethodPost, "Authorization", "Bearer hunyuan-upstream-key", `{"voice_id":"minimax_55e81114"}`},
		{HunyuanEndpointTextEmbeddings, "", "https://tokenhub.tencentmaas.com/v1/embeddings", http.MethodPost, "Authorization", "Bearer hunyuan-upstream-key", `{"id":"accepted","status":"queued"}`},
		{HunyuanEndpointMultimodalEmbeddings, "", "https://tokenhub.tencentmaas.com/v1/embeddings/multimodal", http.MethodPost, "Authorization", "Bearer hunyuan-upstream-key", `{"id":"accepted","status":"queued"}`},
		{HunyuanEndpoint3DSubmit, "", "https://tokenhub.tencentmaas.com/v1/api/3d/submit", http.MethodPost, "Authorization", "Bearer hunyuan-upstream-key", `{"id":"accepted","status":"queued"}`},
		{HunyuanEndpoint3DQuery, "", "https://tokenhub.tencentmaas.com/v1/api/3d/query", http.MethodPost, "Authorization", "Bearer hunyuan-upstream-key", `{"id":"accepted","status":"queued"}`},
		{HunyuanEndpointMessages, "", "https://tokenhub.tencentmaas.com/v1/messages", http.MethodPost, "x-api-key", "hunyuan-upstream-key", `{"id":"accepted","status":"queued"}`},
		{HunyuanEndpointResponses, "", "https://tokenhub.tencentmaas.com/v1/responses", http.MethodPost, "Authorization", "Bearer hunyuan-upstream-key", `{"id":"accepted","status":"queued"}`},
	}
	for _, testCase := range cases {
		t.Run(string(testCase.endpoint), func(t *testing.T) {
			upstream := &hunyuanActionUpstreamStub{responses: []*http.Response{
				hunyuanActionJSONResponse(http.StatusOK, testCase.response),
			}}
			svc := hunyuanActionTestService(upstream)
			c, _ := hunyuanActionTestContext(t, http.MethodPost, "/v1/hunyuan/action", nil)

			_, err := svc.ForwardHunyuanAction(context.Background(), c, hunyuanActionTestAccount(),
				testCase.endpoint, testCase.taskID, []byte(`{"model":"hy3"}`), "application/json", "")
			require.NoError(t, err)
			require.Equal(t, 1, upstream.requestCount())
			request, _ := upstream.recordedRequest(0)
			require.Equal(t, testCase.expectedURL, request.URL.String())
			require.Equal(t, testCase.expectedMth, request.Method)
			require.Equal(t, testCase.authValue, request.Header.Get(testCase.authHeader))
			require.Empty(t, request.Header.Get("X-TC-Action"), "TC3 signed requests are retired")
		})
	}
}

// TestHunyuanActionBaseURLBoundary keeps the accepted official sites working and
// rejects anything that would send the account credential to an unverified host.
func TestHunyuanActionBaseURLBoundary(t *testing.T) {
	for _, baseURL := range []string{
		"", hunyuanCompatibilityBaseURL, hunyuanCompatibilityBaseURL + "/",
		"https://tokenhub.tencentmaas.cn/v1",
		"https://tokenhub-intl.tencentmaas.com/v1",
		"https://tokenhub-intl.tencentmaas.cn/v1",
		hunyuanLegacyBaseURL,
	} {
		account := hunyuanActionTestAccount()
		account.Credentials["base_url"] = baseURL
		_, err := hunyuanActionBaseURL(account)
		require.NoErrorf(t, err, "base url %q must resolve", baseURL)
	}
	for _, baseURL := range []string{
		"https://tokenhub.tencentmaas.com.evil.example/v1",
		"https://api.openai.com/v1",
		"http://tokenhub.tencentmaas.com/v1",
	} {
		account := hunyuanActionTestAccount()
		account.Credentials["base_url"] = baseURL
		_, err := hunyuanActionBaseURL(account)
		require.Errorf(t, err, "base url %q must be rejected", baseURL)
	}

	wrongPlatform := hunyuanActionTestAccount()
	wrongPlatform.Platform = PlatformZhipu
	_, err := hunyuanActionBaseURL(wrongPlatform)
	require.Error(t, err)

	wrongType := hunyuanActionTestAccount()
	wrongType.Type = AccountTypeOAuth
	_, err = hunyuanActionBaseURL(wrongType)
	require.Error(t, err)
}

// TestHunyuanTaskIDRejectsPathEscape proves a caller cannot break out of the
// documented task path with a crafted id.
func TestHunyuanTaskIDRejectsPathEscape(t *testing.T) {
	svc := hunyuanActionTestService(&hunyuanActionUpstreamStub{})
	for _, taskID := range []string{"", ".", "..", "../../v1/models", "a/b", "a?b", `a\b`, "a#b"} {
		_, err := svc.buildHunyuanActionURL(hunyuanActionTestAccount(), HunyuanEndpointVideoTask, taskID)
		require.Errorf(t, err, "task id %q must be rejected", taskID)
	}
	target, err := svc.buildHunyuanActionURL(hunyuanActionTestAccount(), HunyuanEndpointVideoTask, "251435731-WandVideo-05c42d57")
	require.NoError(t, err)
	require.Equal(t, "https://tokenhub.tencentmaas.com/v1/wand/hunyuan-video/tasks/251435731-WandVideo-05c42d57", target)
}

func TestHunyuanActionParseRequest(t *testing.T) {
	t.Run("image generation reads prompt size and count", func(t *testing.T) {
		info := ParseHunyuanActionRequest(HunyuanEndpointImageGeneration, "application/json",
			[]byte(`{"model":"hy-image-v3","prompt":"一只橙色小猫","size":"1024x1024","seed":7}`))
		require.Equal(t, "hy-image-v3", info.Model)
		require.Equal(t, "一只橙色小猫", info.InputText)
		require.Equal(t, "1024x1024", info.Size)
		require.Equal(t, 1, info.Count)
		require.False(t, info.HasInputImage)
	})
	t.Run("reference image generation reports the source images", func(t *testing.T) {
		info := ParseHunyuanActionRequest(HunyuanEndpointImageGeneration, "application/json",
			[]byte(`{"model":"hy-image-v3","prompt":"改成夜晚","images":["https://cos.example.com/a.png"]}`))
		require.True(t, info.HasInputImage)
	})
	t.Run("video generation reports image to video and duration", func(t *testing.T) {
		info := ParseHunyuanActionRequest(HunyuanEndpointVideoGeneration, "application/json",
			[]byte(`{"model":"hy-video-v1.5","prompt":"让主体转头","image_url":"https://example.com/start.jpg","duration":5,"aspect_ratio":"16:9"}`))
		require.True(t, info.HasInputImage)
		require.Equal(t, 5, info.DurationSeconds)
		require.Equal(t, "16:9", info.AspectRatio)
	})
	t.Run("speech synthesis reads the spoken text", func(t *testing.T) {
		info := ParseHunyuanActionRequest(HunyuanEndpointSpeechSync, "application/json",
			[]byte(`{"model":"minimax-speech-2.8-hd","text":"刚吹过晚风的巷口"}`))
		require.Equal(t, "刚吹过晚风的巷口", info.InputText)
	})
	t.Run("multipart edit keeps the model field", func(t *testing.T) {
		contentType := "multipart/form-data; boundary=----boundary"
		body := []byte("------boundary\r\nContent-Disposition: form-data; name=\"model\"\r\n\r\nhy-image-v3\r\n------boundary\r\nContent-Disposition: form-data; name=\"prompt\"\r\n\r\n夜景\r\n------boundary--\r\n")
		info := ParseHunyuanActionRequest(HunyuanEndpointImageGeneration, contentType, body)
		require.Equal(t, "hy-image-v3", info.Model)
		require.Equal(t, "夜景", info.InputText)
		require.True(t, info.HasInputImage)
	})
	t.Run("voice clone reads the reference audio and demo model", func(t *testing.T) {
		info := ParseHunyuanActionRequest(HunyuanEndpointVoiceClone, "application/json",
			[]byte(`{"model":"minimax-voice-clone","audio_url":"https://cos.example.com/a.wav","tts_model":"minimax-speech-2.8-hd","text":"试听文本"}`))
		require.Equal(t, "minimax-voice-clone", info.Model)
		require.Equal(t, "试听文本", info.InputText)
		require.Equal(t, "minimax-speech-2.8-hd", info.DemoSpeechModel)
	})
	t.Run("3D query reads the task id from the body", func(t *testing.T) {
		info := ParseHunyuanActionRequest(HunyuanEndpoint3DQuery, "application/json",
			[]byte(`{"model":"hy-3d-3.1","id":"143128-3d-7f2c"}`))
		require.Equal(t, "hy-3d-3.1", info.Model)
		require.Equal(t, "143128-3d-7f2c", info.TaskID)
		empty := ParseHunyuanActionRequest(HunyuanEndpoint3DQuery, "application/json", []byte(`{"model":"hy-3d-3.1"}`))
		require.Empty(t, empty.TaskID, "a query without an id stays unbound")
	})
}

// TestHunyuanForwardImageGeneration walks the documented Hy-Image-3.0 contract:
// native vendor path, unchanged body, data[].url passthrough, ImageCount.
func TestHunyuanForwardImageGeneration(t *testing.T) {
	upstream := &hunyuanActionUpstreamStub{responses: []*http.Response{
		hunyuanActionJSONResponse(http.StatusOK, `{"id":"4-WandImage-a786","created":1785125529,"data":[{"url":"https://aigc-image.cos.myqcloud.com/xxx/result.png","revised_prompt":"一只橙色小猫坐在窗台上"}],"request_id":"3aec3299","tokenhub_usage":{"total_tokens":1024}}`),
	}}
	svc := hunyuanActionTestService(upstream)
	account := hunyuanActionTestAccount()
	account.Credentials["model_mapping"] = map[string]any{"hy-image-public": "hy-image-v3"}
	c, recorder := hunyuanActionTestContext(t, http.MethodPost, "/v1/images/generations", nil)

	requestBody := `{"model":"hy-image-public","prompt":"一只橙色小猫在窗台上看向镜头","size":"1024x1024"}`
	result, err := svc.ForwardHunyuanAction(context.Background(), c, account, HunyuanEndpointImageGeneration, "",
		[]byte(requestBody), "application/json", "")

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 1, result.ImageCount)
	require.Equal(t, "1024x1024", result.ImageOutputSize)
	require.Equal(t, "hy-image-public", result.Model)
	require.Equal(t, "hy-image-v3", result.UpstreamModel)
	require.Equal(t, 1024, hunyuanResponseTokenTotal([]byte(`{"tokenhub_usage":{"total_tokens":1024}}`)))
	require.Contains(t, recorder.Body.String(), "aigc-image.cos.myqcloud.com")

	request, sentBody := upstream.recordedRequest(0)
	require.Equal(t, "https://tokenhub.tencentmaas.com/v1/wand/hunyuan-image/v3-generation", request.URL.String())
	require.JSONEq(t, `{"model":"hy-image-v3","prompt":"一只橙色小猫在窗台上看向镜头","size":"1024x1024"}`, string(sentBody))
}

func TestHunyuanForwardImageGenerationWithoutOutputFails(t *testing.T) {
	upstream := &hunyuanActionUpstreamStub{responses: []*http.Response{
		hunyuanActionJSONResponse(http.StatusOK, `{"id":"4-WandImage-a786","created":1785125529,"data":[]}`),
	}}
	svc := hunyuanActionTestService(upstream)
	c, _ := hunyuanActionTestContext(t, http.MethodPost, "/v1/images/generations", nil)

	_, err := svc.ForwardHunyuanAction(context.Background(), c, hunyuanActionTestAccount(), HunyuanEndpointImageGeneration, "",
		[]byte(`{"model":"hy-image-v3","prompt":"x"}`), "application/json", "")

	require.Error(t, err)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
}

// TestHunyuanForwardImageEditUsesReferenceImages proves the reference-image
// ("参考生图") form of the same vendor endpoint carries the edit source through
// untouched, which is the documented shape behind image.edit.
func TestHunyuanForwardImageEditUsesReferenceImages(t *testing.T) {
	upstream := &hunyuanActionUpstreamStub{responses: []*http.Response{
		hunyuanActionJSONResponse(http.StatusOK, `{"id":"4-WandImage-edit","created":1785125529,"data":[{"url":"https://aigc-image.cos.myqcloud.com/edit.png"}]}`),
	}}
	svc := hunyuanActionTestService(upstream)
	c, _ := hunyuanActionTestContext(t, http.MethodPost, "/v1/images/edits", nil)
	editBody := `{"model":"hy-image-v3","prompt":"把背景换成夜晚","images":["https://cos.example.com/source.png"]}`

	result, err := svc.ForwardHunyuanAction(context.Background(), c, hunyuanActionTestAccount(), HunyuanEndpointImageGeneration, "",
		[]byte(editBody), "application/json", "")

	require.NoError(t, err)
	require.Equal(t, 1, result.ImageCount)
	request, sentBody := upstream.recordedRequest(0)
	require.Equal(t, "https://tokenhub.tencentmaas.com/v1/wand/hunyuan-image/v3-generation", request.URL.String())
	require.JSONEq(t, editBody, string(sentBody))
}

func TestHunyuanForwardPanoramaUsesVendorEnvelope(t *testing.T) {
	upstream := &hunyuanActionUpstreamStub{responses: []*http.Response{
		hunyuanActionJSONResponse(http.StatusOK, `{"id":"4-WandImage-pano","created":1785125529,"status":"succeeded","data":[{"cover_image_url":"https://cos.example.com/cover.png","super_resolution_image_url":"https://cos.example.com/pano.png"}]}`),
	}}
	svc := hunyuanActionTestService(upstream)
	c, _ := hunyuanActionTestContext(t, http.MethodPost, "/v1/hunyuan/panorama", nil)

	result, err := svc.ForwardHunyuanAction(context.Background(), c, hunyuanActionTestAccount(), HunyuanEndpointImagePanorama, "",
		[]byte(`{"model":"hy-world2-panorama","prompt":"现代美术馆室内全景"}`), "application/json", "")

	require.NoError(t, err)
	require.Equal(t, 1, result.ImageCount)
}

// TestHunyuanForwardVideoGeneration covers submit acceptance and the documented
// 200-with-failed-envelope rejection that must not be treated as a task.
func TestHunyuanForwardVideoGeneration(t *testing.T) {
	t.Run("accepted submit returns the vendor task id", func(t *testing.T) {
		upstream := &hunyuanActionUpstreamStub{responses: []*http.Response{
			hunyuanActionJSONResponse(http.StatusOK, `{"task_id":"251435731-WandVideo-05c42d57d3a74c1fa39d56ff59b31e20","created":1787108411,"request_id":"835a268e-af02-4615-a30b-7e4b815e8839"}`),
		}}
		svc := hunyuanActionTestService(upstream)
		c, _ := hunyuanActionTestContext(t, http.MethodPost, "/v1/videos", nil)

		result, err := svc.ForwardHunyuanAction(context.Background(), c, hunyuanActionTestAccount(), HunyuanEndpointVideoGeneration, "",
			[]byte(`{"model":"hy-video-v1.5","prompt":"一只小狗在草地上奔跑","aspect_ratio":"16:9"}`), "application/json", "")

		require.NoError(t, err)
		require.Equal(t, "251435731-WandVideo-05c42d57d3a74c1fa39d56ff59b31e20", result.ResponseID)
		request, _ := upstream.recordedRequest(0)
		require.Equal(t, "https://tokenhub.tencentmaas.com/v1/wand/hunyuan-video/generation", request.URL.String())
	})
	t.Run("platform rejection envelope fails over instead of accepting", func(t *testing.T) {
		upstream := &hunyuanActionUpstreamStub{responses: []*http.Response{
			hunyuanActionJSONResponse(http.StatusOK, `{"task_id":"","status":"failed","error":{"code":"1300","message":"Trigger the platform strategy"}}`),
		}}
		svc := hunyuanActionTestService(upstream)
		c, _ := hunyuanActionTestContext(t, http.MethodPost, "/v1/videos", nil)

		_, err := svc.ForwardHunyuanAction(context.Background(), c, hunyuanActionTestAccount(), HunyuanEndpointVideoGeneration, "",
			[]byte(`{"model":"hy-video-v1.5","prompt":"blocked"}`), "application/json", "")

		require.Error(t, err)
		var failoverErr *UpstreamFailoverError
		require.ErrorAs(t, err, &failoverErr)
	})
}

// TestHunyuanForwardHy3DTaskPair covers the HY-3D submit/query pair (1823/137181),
// including the POST query whose body carries {model, id} and the rejected
// submission envelope that must not be accepted as a task.
func TestHunyuanForwardHy3DTaskPair(t *testing.T) {
	t.Run("submit returns the vendor task id", func(t *testing.T) {
		upstream := &hunyuanActionUpstreamStub{responses: []*http.Response{
			hunyuanActionJSONResponse(http.StatusOK, `{"id":"143128-3d-7f2c","status":"queued","request_id":"9a1c"}`),
		}}
		svc := hunyuanActionTestService(upstream)
		c, _ := hunyuanActionTestContext(t, http.MethodPost, "/v1/hunyuan/3d/submit", nil)
		submitBody := `{"model":"hy-3d-3.1","prompt":"一只低多边形的狐狸"}`

		result, err := svc.ForwardHunyuanAction(context.Background(), c, hunyuanActionTestAccount(), HunyuanEndpoint3DSubmit, "",
			[]byte(submitBody), "application/json", "")

		require.NoError(t, err)
		require.Equal(t, "143128-3d-7f2c", result.ResponseID)
		request, sentBody := upstream.recordedRequest(0)
		require.Equal(t, "https://tokenhub.tencentmaas.com/v1/api/3d/submit", request.URL.String())
		require.JSONEq(t, submitBody, string(sentBody))
	})
	t.Run("a rejected submission fails over instead of accepting", func(t *testing.T) {
		upstream := &hunyuanActionUpstreamStub{responses: []*http.Response{
			hunyuanActionJSONResponse(http.StatusOK, `{"id":"","status":"failed","error":{"code":"1300","message":"Trigger the platform strategy"}}`),
		}}
		svc := hunyuanActionTestService(upstream)
		c, _ := hunyuanActionTestContext(t, http.MethodPost, "/v1/hunyuan/3d/submit", nil)

		_, err := svc.ForwardHunyuanAction(context.Background(), c, hunyuanActionTestAccount(), HunyuanEndpoint3DSubmit, "",
			[]byte(`{"model":"hy-3d-3.1","prompt":"blocked"}`), "application/json", "")

		require.Error(t, err)
		var failoverErr *UpstreamFailoverError
		require.ErrorAs(t, err, &failoverErr)
	})
	t.Run("query posts the model and id body untouched", func(t *testing.T) {
		upstream := &hunyuanActionUpstreamStub{responses: []*http.Response{
			hunyuanActionJSONResponse(http.StatusOK, `{"id":"143128-3d-7f2c","status":"completed","model":"hy-3d-3.1","data":[{"url":"https://cos.example.com/model.glb"}]}`),
		}}
		svc := hunyuanActionTestService(upstream)
		c, recorder := hunyuanActionTestContext(t, http.MethodPost, "/v1/hunyuan/3d/query", nil)
		queryBody := `{"model":"hy-3d-3.1","id":"143128-3d-7f2c"}`

		result, err := svc.ForwardHunyuanAction(context.Background(), c, hunyuanActionTestAccount(), HunyuanEndpoint3DQuery, "",
			[]byte(queryBody), "application/json", "")

		require.NoError(t, err)
		require.Equal(t, "143128-3d-7f2c", result.ResponseID)
		require.Contains(t, recorder.Body.String(), "model.glb")
		request, sentBody := upstream.recordedRequest(0)
		require.Equal(t, "https://tokenhub.tencentmaas.com/v1/api/3d/query", request.URL.String())
		require.Equal(t, http.MethodPost, request.Method)
		require.JSONEq(t, queryBody, string(sentBody))
	})
}

// TestHunyuanForwardVideoStatusAndContent covers the owner-scoped task read, the
// content-URL rewrite onto the gateway proxy, and the media download.
func TestHunyuanForwardVideoStatusAndContent(t *testing.T) {
	const taskID = "251435731-WandVideo-05c42d57d3a74c1fa39d56ff59b31e20"
	statusBody := `{"created":1787108411,"error":null,"task_id":"251435731-WandVideo-05c42d57d3a74c1fa39d56ff59b31e20","status":"succeeded","videos":[{"audio_url":"","generate_info":"","url":"https://aigc-output-video.cos.ap-guangzhou.myqcloud.com/out.mp4"}],"tokenhub_usage":{"total_tokens":150000},"request_id":"252e94a8"}`

	t.Run("status rewrites the media url to the gateway proxy", func(t *testing.T) {
		upstream := &hunyuanActionUpstreamStub{responses: []*http.Response{
			hunyuanActionJSONResponse(http.StatusOK, statusBody),
		}}
		svc := hunyuanActionTestService(upstream)
		c, recorder := hunyuanActionTestContext(t, http.MethodGet, "/v1/videos/"+taskID, nil)

		result, err := svc.ForwardHunyuanAction(context.Background(), c, hunyuanActionTestAccount(), HunyuanEndpointVideoTask, taskID, nil, "", "")

		require.NoError(t, err)
		require.Equal(t, 1, result.VideoCount)
		require.Equal(t, taskID, result.ResponseID)
		require.Contains(t, recorder.Body.String(), "/v1/videos/"+taskID+"/content")
		require.NotContains(t, recorder.Body.String(), "myqcloud.com")
	})
	t.Run("running status reports no completion", func(t *testing.T) {
		upstream := &hunyuanActionUpstreamStub{responses: []*http.Response{
			hunyuanActionJSONResponse(http.StatusOK, `{"created":1787108411,"error":null,"task_id":"t","status":"running","request_id":"r"}`),
		}}
		svc := hunyuanActionTestService(upstream)
		c, _ := hunyuanActionTestContext(t, http.MethodGet, "/v1/videos/t", nil)

		result, err := svc.ForwardHunyuanAction(context.Background(), c, hunyuanActionTestAccount(), HunyuanEndpointVideoTask, "t", nil, "", "")
		require.NoError(t, err)
		require.Zero(t, result.VideoCount)
		require.False(t, IsHunyuanVideoTaskTerminal([]byte(`{"status":"running"}`)))
		require.True(t, IsHunyuanVideoTaskTerminal([]byte(`{"status":"failed"}`)))
	})
	t.Run("content streams the settled media", func(t *testing.T) {
		upstream := &hunyuanActionUpstreamStub{responses: []*http.Response{
			hunyuanActionJSONResponse(http.StatusOK, statusBody),
			hunyuanActionBinaryResponse(http.StatusOK, "MP4BYTES", "video/mp4"),
		}}
		svc := hunyuanActionTestService(upstream)
		c, recorder := hunyuanActionTestContext(t, http.MethodGet, "/v1/videos/"+taskID+"/content", map[string]string{"Range": "bytes=0-1023"})

		result, err := svc.ForwardHunyuanVideoContent(context.Background(), c, hunyuanActionTestAccount(), taskID, time.Now())

		require.NoError(t, err)
		require.Equal(t, 1, result.VideoCount)
		require.Equal(t, "MP4BYTES", recorder.Body.String())
		require.Equal(t, 2, upstream.requestCount())
		contentRequest, _ := upstream.recordedRequest(1)
		require.Equal(t, "https://aigc-output-video.cos.ap-guangzhou.myqcloud.com/out.mp4", contentRequest.URL.String())
		require.Equal(t, "bytes=0-1023", contentRequest.Header.Get("Range"))
		require.Empty(t, contentRequest.Header.Get("Authorization"), "vendor credentials never reach the media host")
	})
	t.Run("content for an unfinished task returns the status body", func(t *testing.T) {
		upstream := &hunyuanActionUpstreamStub{responses: []*http.Response{
			hunyuanActionJSONResponse(http.StatusOK, `{"task_id":"t","status":"queued","request_id":"r"}`),
		}}
		svc := hunyuanActionTestService(upstream)
		c, recorder := hunyuanActionTestContext(t, http.MethodGet, "/v1/videos/t/content", nil)

		result, err := svc.ForwardHunyuanVideoContent(context.Background(), c, hunyuanActionTestAccount(), "t", time.Now())

		require.NoError(t, err)
		require.Zero(t, result.VideoCount)
		require.Contains(t, recorder.Body.String(), "queued")
		require.Equal(t, 1, upstream.requestCount())
	})
}

// TestHunyuanForwardSpeechRecognition covers the synchronous and asynchronous
// speech recognition contracts, including the documented billing units.
func TestHunyuanForwardSpeechRecognition(t *testing.T) {
	t.Run("synchronous transcription returns text and bills by duration", func(t *testing.T) {
		upstream := &hunyuanActionUpstreamStub{responses: []*http.Response{
			hunyuanActionJSONResponse(http.StatusOK, `{"id":"th_251416641","object":"asr.task","created_at":1721808000,"status":"completed","request_id":"fd8a1c2f","output":{"source":"zh","duration_ms":15230,"text":"大家好","sentences":[{"begin_ms":500,"end_ms":3200,"text":"大家好"}]},"usage":{"total_token":761}}`),
		}}
		svc := hunyuanActionTestService(upstream)
		c, _ := hunyuanActionTestContext(t, http.MethodPost, "/v1/audio/transcriptions", nil)

		result, err := svc.ForwardHunyuanAction(context.Background(), c, hunyuanActionTestAccount(), HunyuanEndpointTranscribeSync, "",
			[]byte(`{"model":"hy-asr-3.0-preview","input_url":"https://cos.example.com/xxx.mp4"}`), "application/json", "")

		require.NoError(t, err)
		require.NotNil(t, result.AudioUsage)
		require.Equal(t, "stt", result.AudioUsage.Mode)
		require.InDelta(t, 15230.0/1000.0/3600.0, result.AudioUsage.DurationOrUnits, 1e-9)
	})
	t.Run("asynchronous submit then completed task", func(t *testing.T) {
		upstream := &hunyuanActionUpstreamStub{responses: []*http.Response{
			hunyuanActionJSONResponse(http.StatusOK, `{"id":"th_251416641_700001910665_asrproxy_781c68c1","created_at":1721808000,"status":"queued","request_id":"64094fda"}`),
			hunyuanActionJSONResponse(http.StatusOK, `{"id":"th_251416641_700001910665_asrproxy_781c68c1","object":"asr.task","created_at":1721808000,"status":"completed","request_id":"b1504f51","output":{"source":"zh","duration_ms":6025,"text":"大家好","subtitle_url":"https://xxx.cos.ap-guangzhou.myqcloud.com/subtitle/xxx.srt"},"usage":{"total_token":6025}}`),
		}}
		svc := hunyuanActionTestService(upstream)
		submitCtx, _ := hunyuanActionTestContext(t, http.MethodPost, "/v1/audio/transcriptions", nil)

		submitResult, err := svc.ForwardHunyuanAction(context.Background(), submitCtx, hunyuanActionTestAccount(), HunyuanEndpointTranscribeAsync, "",
			[]byte(`{"model":"hy-asr-3.0-preview","input_url":"https://cos.example.com/xxx.mp4"}`), "application/json", "")
		require.NoError(t, err)
		require.Equal(t, "th_251416641_700001910665_asrproxy_781c68c1", submitResult.ResponseID)

		taskCtx, _ := hunyuanActionTestContext(t, http.MethodGet, "/v1/hunyuan/transcribe/tasks/th_251416641", nil)
		taskResult, err := svc.ForwardHunyuanAction(context.Background(), taskCtx, hunyuanActionTestAccount(), HunyuanEndpointTranscribeTask,
			"th_251416641_700001910665_asrproxy_781c68c1", nil, "", "")
		require.NoError(t, err)
		require.NotNil(t, taskResult.AudioUsage)
		require.InDelta(t, 6025.0/1000.0/3600.0, taskResult.AudioUsage.DurationOrUnits, 1e-9)
		require.True(t, IsHunyuanTranscribeTaskSuccess([]byte(`{"status":"completed"}`)))
		require.False(t, IsHunyuanTranscribeTaskSuccess([]byte(`{"status":"in_progress"}`)))
	})
}

func TestHunyuanForwardSpeechSynthesis(t *testing.T) {
	upstream := &hunyuanActionUpstreamStub{responses: []*http.Response{
		hunyuanActionJSONResponse(http.StatusOK, `{"data":{"audio":"deadbeef","status":2},"extra_info":{"audio_length":9900,"usage_characters":26},"trace_id":"01b8bf9b","base_resp":{"status_code":0,"status_msg":"success"},"usage":{"total_token":991785}}`),
	}}
	svc := hunyuanActionTestService(upstream)
	c, _ := hunyuanActionTestContext(t, http.MethodPost, "/v1/audio/speech", nil)

	result, err := svc.ForwardHunyuanAction(context.Background(), c, hunyuanActionTestAccount(), HunyuanEndpointSpeechSync, "",
		[]byte(`{"model":"minimax-speech-2.8-hd","text":"刚吹过晚风的巷口飘着糖炒栗子的香","voice_setting":{"voice_id":"minimax_55e81114-ce7e-4ca1"}}`), "application/json", "")

	require.NoError(t, err)
	require.NotNil(t, result.AudioUsage)
	require.Equal(t, "tts", result.AudioUsage.Mode)
	require.InDelta(t, 26.0/1_000_000.0, result.AudioUsage.DurationOrUnits, 1e-12)
	require.Equal(t, "minimax-speech-2.8-hd", result.Model)
}

// TestHunyuanForwardVoiceClone covers the documented 音色复刻 contract
// (1823/135798): the reference audio and demo line are forwarded untouched, the
// returned voice_id is the action result, and only the demo audio is billable
// because the clone fee is charged upstream on the first synthesis that uses it.
func TestHunyuanForwardVoiceClone(t *testing.T) {
	t.Run("clone with demo audio bills the demo characters against tts_model", func(t *testing.T) {
		upstream := &hunyuanActionUpstreamStub{responses: []*http.Response{
			// Documented response fields only: voice_id, demo_audio, extra_info,
			// base_resp and usage.total_token.
			hunyuanActionJSONResponse(http.StatusOK, `{"voice_id":"minimax_55e81114-ce7e-4ca1","demo_audio":"https://cos.example.com/demo.wav","extra_info":{"audio_length":9900,"usage_characters":26},"base_resp":{"status_code":0,"status_msg":"success"},"usage":{"total_token":991785}}`),
		}}
		svc := hunyuanActionTestService(upstream)
		c, recorder := hunyuanActionTestContext(t, http.MethodPost, "/v1/custom-voices", nil)
		cloneBody := `{"model":"minimax-voice-clone","audio_url":"https://cos.example.com/source.wav","tts_model":"minimax-speech-2.8-hd","text":"刚吹过晚风的巷口飘着糖炒栗子的香","output_format":"url"}`

		result, err := svc.ForwardHunyuanAction(context.Background(), c, hunyuanActionTestAccount(), HunyuanEndpointVoiceClone, "",
			[]byte(cloneBody), "application/json", "")

		require.NoError(t, err)
		require.Equal(t, "minimax_55e81114-ce7e-4ca1", result.ResponseID)
		require.NotNil(t, result.AudioUsage)
		require.Equal(t, "tts", result.AudioUsage.Mode)
		require.InDelta(t, 26.0/1_000_000.0, result.AudioUsage.DurationOrUnits, 1e-12)
		require.Equal(t, "minimax-speech-2.8-hd", result.Model, "demo characters are priced against the synthesizing model")
		require.Contains(t, recorder.Body.String(), "minimax_55e81114-ce7e-4ca1")

		request, sentBody := upstream.recordedRequest(0)
		require.Equal(t, "https://tokenhub.tencentmaas.com/v1/wand/minimax-tts/sync_clone", request.URL.String())
		require.Equal(t, "Bearer hunyuan-upstream-key", request.Header.Get("Authorization"))
		require.JSONEq(t, cloneBody, string(sentBody))
	})
	t.Run("clone without a demo line carries no gateway units", func(t *testing.T) {
		upstream := &hunyuanActionUpstreamStub{responses: []*http.Response{
			hunyuanActionJSONResponse(http.StatusOK, `{"voice_id":"minimax_55e81114-ce7e-4ca1","base_resp":{"status_code":0}}`),
		}}
		svc := hunyuanActionTestService(upstream)
		c, _ := hunyuanActionTestContext(t, http.MethodPost, "/v1/custom-voices", nil)

		result, err := svc.ForwardHunyuanAction(context.Background(), c, hunyuanActionTestAccount(), HunyuanEndpointVoiceClone, "",
			[]byte(`{"model":"minimax-voice-clone","audio_data":"UklGRg=="}`), "application/json", "")

		require.NoError(t, err)
		require.Equal(t, "minimax_55e81114-ce7e-4ca1", result.ResponseID)
		require.Nil(t, result.AudioUsage, "voice creation is billed upstream on first synthesis, not here")
	})
	t.Run("content rejection passes the vendor body through without billing", func(t *testing.T) {
		upstream := &hunyuanActionUpstreamStub{responses: []*http.Response{
			hunyuanActionJSONResponse(http.StatusOK, `{"voice_id":"","input_sensitive":true,"base_resp":{"status_code":1004,"status_msg":"input sensitive"}}`),
		}}
		svc := hunyuanActionTestService(upstream)
		c, recorder := hunyuanActionTestContext(t, http.MethodPost, "/v1/custom-voices", nil)

		result, err := svc.ForwardHunyuanAction(context.Background(), c, hunyuanActionTestAccount(), HunyuanEndpointVoiceClone, "",
			[]byte(`{"model":"minimax-voice-clone","audio_url":"https://cos.example.com/blocked.wav"}`), "application/json", "")

		require.NoError(t, err, "a client-content rejection must not fail over to another account")
		require.Nil(t, result.AudioUsage)
		require.Empty(t, result.ResponseID)
		require.Contains(t, recorder.Body.String(), "input_sensitive", "the vendor envelope stays readable")
	})
	t.Run("a success envelope without a voice id fails over", func(t *testing.T) {
		upstream := &hunyuanActionUpstreamStub{responses: []*http.Response{
			hunyuanActionJSONResponse(http.StatusOK, `{"base_resp":{"status_code":0}}`),
		}}
		svc := hunyuanActionTestService(upstream)
		c, _ := hunyuanActionTestContext(t, http.MethodPost, "/v1/custom-voices", nil)

		_, err := svc.ForwardHunyuanAction(context.Background(), c, hunyuanActionTestAccount(), HunyuanEndpointVoiceClone, "",
			[]byte(`{"model":"minimax-voice-clone","audio_url":"https://cos.example.com/source.wav"}`), "application/json", "")

		require.Error(t, err)
		var failoverErr *UpstreamFailoverError
		require.ErrorAs(t, err, &failoverErr)
		require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
	})
}

func TestHunyuanForwardEmbeddings(t *testing.T) {
	t.Run("text embeddings are OpenAI compatible", func(t *testing.T) {
		upstream := &hunyuanActionUpstreamStub{responses: []*http.Response{
			hunyuanActionJSONResponse(http.StatusOK, `{"object":"list","model":"kinfra-text-embedding-0.6b","data":[{"object":"embedding","index":0,"embedding":[0.1,0.2]}],"usage":{"prompt_tokens":12,"total_tokens":12}}`),
		}}
		svc := hunyuanActionTestService(upstream)
		c, _ := hunyuanActionTestContext(t, http.MethodPost, "/v1/embeddings", nil)

		result, err := svc.ForwardHunyuanAction(context.Background(), c, hunyuanActionTestAccount(), HunyuanEndpointTextEmbeddings, "",
			[]byte(`{"model":"kinfra-text-embedding-0.6b","input":"腾讯云 TokenHub 是一站式 AI 大模型服务平台。"}`), "application/json", "")

		require.NoError(t, err)
		require.Equal(t, 12, result.Usage.InputTokens)
		request, _ := upstream.recordedRequest(0)
		require.Equal(t, "https://tokenhub.tencentmaas.com/v1/embeddings", request.URL.String())
	})
	t.Run("multimodal embeddings use the native fused shape", func(t *testing.T) {
		upstream := &hunyuanActionUpstreamStub{responses: []*http.Response{
			hunyuanActionJSONResponse(http.StatusOK, `{"model":"kinfra-vl-embedding-2b","data":[{"index":0,"embedding":[0.1]}],"usage":{"prompt_tokens":20,"total_tokens":20,"prompt_tokens_details":{"text_tokens":5,"image_tokens":15}}}`),
		}}
		svc := hunyuanActionTestService(upstream)
		c, _ := hunyuanActionTestContext(t, http.MethodPost, "/v1/hunyuan/embeddings/multimodal", nil)
		body := `{"model":"kinfra-vl-embedding-2b","input":[{"type":"text","text":"检索词"},{"type":"image_url","image_url":{"url":"https://cos.example.com/a.png"}}],"instructions":"生成适合图文检索的向量"}`

		result, err := svc.ForwardHunyuanAction(context.Background(), c, hunyuanActionTestAccount(), HunyuanEndpointMultimodalEmbeddings, "",
			[]byte(body), "application/json", "")

		require.NoError(t, err)
		require.Equal(t, 20, result.Usage.InputTokens)
		request, sentBody := upstream.recordedRequest(0)
		require.Equal(t, "https://tokenhub.tencentmaas.com/v1/embeddings/multimodal", request.URL.String())
		require.JSONEq(t, body, string(sentBody))
	})
}

// TestHunyuanForwardClientCancellationPropagates pins the plan contract that a
// client cancellation reaches the upstream instead of being silently retried.
// Video tasks document no cancel endpoint, so cancellation must stop the
// gateway from starting new work rather than calling a vendor cancel route.
func TestHunyuanForwardClientCancellationPropagates(t *testing.T) {
	t.Run("image generation", func(t *testing.T) {
		upstream := &hunyuanActionUpstreamStub{responses: []*http.Response{
			hunyuanActionJSONResponse(http.StatusOK, `{"id":"4-WandImage-x","data":[{"url":"https://cos.example.com/a.png"}]}`),
		}}
		svc := hunyuanActionTestService(upstream)
		c, recorder := hunyuanActionTestContext(t, http.MethodPost, "/v1/images/generations", nil)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := svc.ForwardHunyuanAction(ctx, c, hunyuanActionTestAccount(), HunyuanEndpointImageGeneration, "",
			[]byte(`{"model":"hy-image-v3","prompt":"x"}`), "application/json", "")

		require.Error(t, err)
		require.Zero(t, recorder.Body.Len(), "a cancelled request must not commit a body")
	})
	t.Run("owner-bound task read", func(t *testing.T) {
		upstream := &hunyuanActionUpstreamStub{responses: []*http.Response{
			hunyuanActionJSONResponse(http.StatusOK, `{"task_id":"t","status":"running"}`),
		}}
		svc := hunyuanActionTestService(upstream)
		c, recorder := hunyuanActionTestContext(t, http.MethodGet, "/v1/videos/t", nil)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := svc.ForwardHunyuanAction(ctx, c, hunyuanActionTestAccount(), HunyuanEndpointVideoTask, "t", nil, "", "")

		require.Error(t, err)
		require.Zero(t, recorder.Body.Len())
		// Cancellation reaches the upstream call itself (the stub observes the
		// cancelled request context and fails), and the failure is never retried.
		require.Equal(t, 1, upstream.requestCount())
	})
}

// TestHunyuanActionErrorClassification covers the documented business codes and
// the conservative fallback when a code is not recognised.
func TestHunyuanActionErrorClassification(t *testing.T) {
	cases := []struct {
		status   int
		body     string
		expected string
	}{
		{http.StatusUnauthorized, `{"error":{"code":"1002","message":"Authorization is invalid"}}`, "account_authentication"},
		{http.StatusForbidden, `{"error":{"code":"1103","message":"Access denied for the requested resource"}}`, "account_permission"},
		{http.StatusTooManyRequests, `{"error":{"code":"1101","message":"Account in arrears"}}`, "account_quota"},
		{http.StatusTooManyRequests, `{"error":{"code":"1302","message":"Too frequent API calls"}}`, "rate_limit"},
		{http.StatusBadRequest, `{"task_id":"","status":"failed","error":{"code":"1300","message":"Trigger the platform strategy"}}`, "content_policy"},
		{http.StatusInternalServerError, `{"error":{"code":"5000","message":"Internal server error"}}`, "upstream_unavailable"},
		{http.StatusBadRequest, `{"error":{"code":"1201","message":"Invalid parameters"}}`, "request"},
		{http.StatusTeapot, `{"error":{"code":"9999","message":"unknown"}}`, "request"},
		{http.StatusUnauthorized, `not json`, "account_authentication"},
		{http.StatusTooManyRequests, ``, "rate_limit"},
	}
	for _, testCase := range cases {
		require.Equalf(t, testCase.expected, HunyuanActionErrorScope(testCase.status, []byte(testCase.body)),
			"status %d body %s", testCase.status, testCase.body)
	}
	require.Equal(t, "account_quota", HunyuanActionErrorScope(http.StatusOK, []byte(`{"base_resp":{"status_code":1102,"status_msg":"Resource pack depleted"}}`)))
}

// TestHunyuanTaskOwnershipBinding proves a task id can never be resolved across
// tenants and that reads never refresh the binding.
func TestHunyuanTaskOwnershipBinding(t *testing.T) {
	cache := newMinimaxActionFakeCache()
	svc := &OpenAIGatewayService{cfg: &config.Config{}, cache: cache}
	const taskID = "251435731-WandVideo-05c42d57d3a74c1fa39d56ff59b31e20"
	groupID := int64(7)

	ownerHash := HunyuanTaskSessionHash(taskID, 101, 201)
	require.NotEmpty(t, ownerHash)
	require.NotEqual(t, ownerHash, HunyuanTaskSessionHash(taskID, 101, 202))
	require.NotEqual(t, ownerHash, HunyuanTaskSessionHash(taskID, 102, 201))
	require.Empty(t, HunyuanTaskSessionHash("", 101, 201))

	require.NoError(t, svc.BindHunyuanTaskAccount(context.Background(), &groupID, taskID, 101, 201, 21))
	bound, err := svc.ResolveHunyuanTaskAccount(context.Background(), &groupID, taskID, 101, 201)
	require.NoError(t, err)
	require.Equal(t, int64(21), bound)

	// A different tenant key must not observe the binding.
	_, err = svc.ResolveHunyuanTaskAccount(context.Background(), &groupID, taskID, 101, 202)
	require.ErrorIs(t, err, ErrStickySessionNotFound)
	_, err = svc.ResolveHunyuanTaskAccount(context.Background(), &groupID, taskID, 999, 201)
	require.ErrorIs(t, err, ErrStickySessionNotFound)

	// Binding is rejected without a usable owner identity.
	require.Error(t, svc.BindHunyuanTaskAccount(context.Background(), &groupID, taskID, 0, 201, 21))
	require.Error(t, svc.BindHunyuanTaskAccount(context.Background(), &groupID, taskID, 101, 201, 0))
}

// TestHunyuanTaskBillingClaimIsIdempotent proves concurrent status and content
// reads cannot double-bill and that a failed write can retry.
func TestHunyuanTaskBillingClaimIsIdempotent(t *testing.T) {
	cache := newMinimaxActionFakeCache()
	svc := &OpenAIGatewayService{cfg: &config.Config{}, cache: cache}
	const taskID = "251435731-WandVideo-05c42d57d3a74c1fa39d56ff59b31e20"

	claimed, err := svc.ClaimHunyuanTaskBilling(context.Background(), taskID, 101, 201)
	require.NoError(t, err)
	require.True(t, claimed)
	claimed, err = svc.ClaimHunyuanTaskBilling(context.Background(), taskID, 101, 201)
	require.NoError(t, err)
	require.False(t, claimed, "a second poll must not bill again")

	require.NoError(t, svc.ReleaseHunyuanTaskBilling(context.Background(), taskID, 101, 201))
	claimed, err = svc.ClaimHunyuanTaskBilling(context.Background(), taskID, 101, 201)
	require.NoError(t, err)
	require.True(t, claimed, "a released claim must be retryable")

	// A different tenant key owns its own claim.
	claimed, err = svc.ClaimHunyuanTaskBilling(context.Background(), taskID, 101, 202)
	require.NoError(t, err)
	require.True(t, claimed)

	_, err = svc.ClaimHunyuanTaskBilling(context.Background(), "", 101, 201)
	require.Error(t, err)
}

// TestHunyuanTaskPendingBillingSnapshot covers the submit-time pricing evidence,
// including the documented 5s/720p default and the speech-recognition variant
// that must not inherit video tiers.
func TestHunyuanTaskPendingBillingSnapshot(t *testing.T) {
	t.Run("video defaults follow the documented fixed output", func(t *testing.T) {
		info := ParseHunyuanActionRequest(HunyuanEndpointVideoGeneration, "application/json",
			[]byte(`{"model":"hy-video-v1.5","prompt":"run"}`))
		pending := BuildHunyuanTaskPendingBilling(info, "hy-video-v1.5", "hy-video-v1.5", "hy-video-v1.5", "")
		require.Equal(t, hunyuanVideoDefaultDurationSeconds, pending.VideoDurationSeconds)
		require.Equal(t, VideoBillingResolution720P, pending.VideoResolution)
		require.NotEmpty(t, pending.CreatedAt)
	})
	t.Run("explicit request evidence wins", func(t *testing.T) {
		info := ParseHunyuanActionRequest(HunyuanEndpointVideoGeneration, "application/json",
			[]byte(`{"model":"hy-video-v1.5","prompt":"run","duration":5,"resolution":"1080p"}`))
		pending := BuildHunyuanTaskPendingBilling(info, "hy-video-v1.5", "hy-video-v1.5", "hy-video-v1.5", "")
		require.Equal(t, VideoBillingResolution1080P, pending.VideoResolution)
		require.Equal(t, 5, pending.VideoDurationSeconds)
	})
	t.Run("speech recognition snapshot carries no video tiers", func(t *testing.T) {
		info := ParseHunyuanActionRequest(HunyuanEndpointTranscribeAsync, "application/json",
			[]byte(`{"model":"hy-asr-3.0-preview","input_url":"https://cos.example.com/a.mp4"}`))
		pending := BuildHunyuanTaskPendingBilling(info, "hy-asr-3.0-preview", "hy-asr-3.0-preview", "hy-asr-3.0-preview", "")
		require.Zero(t, pending.VideoDurationSeconds)
		require.Empty(t, pending.VideoResolution)
	})
	t.Run("merge overlays the snapshot and pins the durable billing id", func(t *testing.T) {
		statusResult := &OpenAIForwardResult{ResponseID: "task-1", VideoCount: 1, Model: "hy-video-v1.5"}
		pending := &HunyuanTaskPendingBilling{
			Model:                "hy-video-v1.5",
			BillingModel:         "hy-video-v1.5",
			VideoResolution:      VideoBillingResolution1080P,
			VideoDurationSeconds: 5,
			CreatedAt:            HunyuanTaskPendingCreatedAtNow(),
		}
		merged := MergeHunyuanTaskPendingBilling(statusResult, pending, "task-1")
		require.Equal(t, VideoBillingResolution1080P, merged.VideoResolution)
		require.Equal(t, 5, merged.VideoDurationSeconds)
		require.Equal(t, StableHunyuanVideoBillingRequestID("task-1"), merged.RequestID)
		require.Equal(t, "hunyuan-video:task-1", merged.RequestID)
	})
	t.Run("pending snapshot round trips through the cache", func(t *testing.T) {
		cache := newMinimaxActionFakeCache()
		svc := &OpenAIGatewayService{cfg: &config.Config{}, cache: cache}
		pending := HunyuanTaskPendingBilling{Model: "hy-video-v1.5", BillingModel: "hy-video-v1.5", VideoDurationSeconds: 5, CreatedAt: HunyuanTaskPendingCreatedAtNow()}
		require.NoError(t, svc.StoreHunyuanTaskPendingBilling(context.Background(), "task-1", 101, 201, pending))

		loaded, err := svc.LoadHunyuanTaskPendingBilling(context.Background(), "task-1", 101, 201)
		require.NoError(t, err)
		require.NotNil(t, loaded)
		require.Equal(t, 5, loaded.VideoDurationSeconds)

		missing, err := svc.LoadHunyuanTaskPendingBilling(context.Background(), "task-1", 999, 201)
		require.NoError(t, err)
		require.Nil(t, missing, "another tenant must not read this snapshot")
	})
}

// TestHunyuanBillingRequestIDStability keeps retries on one durable money event.
func TestHunyuanBillingRequestIDStability(t *testing.T) {
	require.Equal(t, "hunyuan-video:task-1", StableHunyuanVideoBillingRequestID("task-1"))
	require.Equal(t, "hunyuan-video:task-1", StableHunyuanVideoBillingRequestID("hunyuan-video:task-1"))
	require.Empty(t, StableHunyuanVideoBillingRequestID("  "))

	require.Equal(t, "hunyuan-transcribe:asr-1", HunyuanTranscribeTaskBillingRequestID("asr-1"))
	require.Equal(t, "hunyuan-transcribe:asr-1", HunyuanTranscribeTaskBillingRequestID("hunyuan-transcribe:asr-1"))

	stable := StableHunyuanAudioBillingRequestID("upstream-1")
	require.Equal(t, "hunyuan_audio:upstream-1", stable)
	require.Equal(t, stable, StableHunyuanAudioBillingRequestID(stable))
	require.NotEmpty(t, StableHunyuanAudioBillingRequestID(""))
}

// TestHunyuanVideoResolutionFromSize maps documented sizes onto billing tiers.
func TestHunyuanVideoResolutionFromSize(t *testing.T) {
	require.Equal(t, VideoBillingResolution720P, hunyuanVideoResolutionFromSize("1280x720"))
	require.Equal(t, VideoBillingResolution480P, hunyuanVideoResolutionFromSize("640x480"))
	require.Equal(t, VideoBillingResolution1080P, hunyuanVideoResolutionFromSize("1920x1080"))
	require.Equal(t, VideoBillingResolution720P, hunyuanVideoResolutionFromSize("720x1280"))
	require.Empty(t, hunyuanVideoResolutionFromSize(""))
	require.Empty(t, hunyuanVideoResolutionFromSize("not-a-size"))
}
