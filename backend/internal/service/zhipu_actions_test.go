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

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

// zhipuActionUpstreamStub records requests and replays canned responses.
type zhipuActionUpstreamStub struct {
	mu        sync.Mutex
	requests  []*http.Request
	responses []*http.Response
	err       error
}

func (s *zhipuActionUpstreamStub) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	s.mu.Lock()
	s.requests = append(s.requests, req)
	s.mu.Unlock()
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

func (s *zhipuActionUpstreamStub) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return s.Do(req, proxyURL, accountID, accountConcurrency)
}

func (s *zhipuActionUpstreamStub) requestCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.requests)
}

func zhipuActionJSONResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func zhipuActionTestAccount() *Account {
	return &Account{
		ID:       11,
		Platform: PlatformZhipu,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "zhipu-upstream-key",
		},
	}
}

func zhipuActionCodingAccount() *Account {
	account := zhipuActionTestAccount()
	account.Credentials["account_mode"] = AccountModeCoding
	return account
}

func zhipuActionTestContext(t *testing.T, method, target string, headers map[string]string) (*gin.Context, *httptest.ResponseRecorder) {
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

// zhipuActionCacheStub is a stateful GatewayCache for binding/billing tests.
type zhipuActionCacheStub struct {
	mu              sync.Mutex
	sessionBindings map[string]int64
	pendingBilling  map[string][]byte
	claims          map[string]bool
}

func newZhipuActionCacheStub() *zhipuActionCacheStub {
	return &zhipuActionCacheStub{
		sessionBindings: map[string]int64{},
		pendingBilling:  map[string][]byte{},
		claims:          map[string]bool{},
	}
}

func (s *zhipuActionCacheStub) GetSessionAccountID(_ context.Context, _ int64, sessionHash string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id, ok := s.sessionBindings[sessionHash]; ok {
		return id, nil
	}
	return 0, ErrStickySessionNotFound
}

func (s *zhipuActionCacheStub) SetSessionAccountID(_ context.Context, _ int64, sessionHash string, accountID int64, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessionBindings[sessionHash] = accountID
	return nil
}

func (s *zhipuActionCacheStub) RefreshSessionTTL(_ context.Context, _ int64, _ string, _ time.Duration) error {
	return nil
}

func (s *zhipuActionCacheStub) DeleteSessionAccountID(_ context.Context, _ int64, sessionHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessionBindings, sessionHash)
	return nil
}

func (s *zhipuActionCacheStub) SetGrokVideoPendingBilling(_ context.Context, key string, payload []byte, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pendingBilling[key] = payload
	return nil
}

func (s *zhipuActionCacheStub) GetGrokVideoPendingBilling(_ context.Context, key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pendingBilling[key], nil
}

func (s *zhipuActionCacheStub) ClaimGrokVideoBilled(_ context.Context, key string, _ time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.claims[key] {
		return false, nil
	}
	s.claims[key] = true
	return true, nil
}

func (s *zhipuActionCacheStub) ReleaseGrokVideoBilled(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.claims, key)
	return nil
}

func (s *zhipuActionCacheStub) SetReasoningContent(_ context.Context, _ string, _ string, _ time.Duration) error {
	return nil
}

func (s *zhipuActionCacheStub) GetReasoningContent(_ context.Context, _ string) (string, error) {
	return "", ErrReasoningContentNotFound
}

func TestBuildZhipuActionURL(t *testing.T) {
	svc := &OpenAIGatewayService{cfg: &config.Config{}}

	t.Run("payg default open platform domain", func(t *testing.T) {
		target, err := svc.buildZhipuActionURL(zhipuActionTestAccount(), ZhipuEndpointImagesGenerations, "")
		require.NoError(t, err)
		require.Equal(t, "https://open.bigmodel.cn/api/paas/v4/images/generations", target)
	})

	t.Run("coding plan account maps media to open platform domain", func(t *testing.T) {
		target, err := svc.buildZhipuActionURL(zhipuActionCodingAccount(), ZhipuEndpointVideosGenerations, "")
		require.NoError(t, err)
		require.Equal(t, "https://open.bigmodel.cn/api/paas/v4/videos/generations", target)
		require.NotContains(t, target, "/api/coding/")
	})

	t.Run("coding plan base_url credential maps to open platform domain", func(t *testing.T) {
		account := zhipuActionTestAccount()
		account.Credentials["base_url"] = "https://open.bigmodel.cn/api/coding/paas/v4"
		target, err := svc.buildZhipuActionURL(account, ZhipuEndpointAudioSpeech, "")
		require.NoError(t, err)
		require.Equal(t, "https://open.bigmodel.cn/api/paas/v4/audio/speech", target)
	})

	t.Run("z.ai international coding domain maps to paas", func(t *testing.T) {
		account := zhipuActionTestAccount()
		account.Credentials["base_url"] = "https://api.z.ai/api/coding/paas/v4"
		target, err := svc.buildZhipuActionURL(account, ZhipuEndpointRerank, "")
		require.NoError(t, err)
		require.Equal(t, "https://api.z.ai/api/paas/v4/rerank", target)
	})

	t.Run("custom relay base_url passthrough", func(t *testing.T) {
		account := zhipuActionTestAccount()
		account.Credentials["base_url"] = "https://relay.example/api/paas/v4/"
		target, err := svc.buildZhipuActionURL(account, ZhipuEndpointEmbeddings, "")
		require.NoError(t, err)
		require.Equal(t, "https://relay.example/api/paas/v4/embeddings", target)
	})

	t.Run("async result escapes task id", func(t *testing.T) {
		target, err := svc.buildZhipuActionURL(zhipuActionTestAccount(), ZhipuEndpointAsyncResult, "8868902201637896192")
		require.NoError(t, err)
		require.Equal(t, "https://open.bigmodel.cn/api/paas/v4/async-result/8868902201637896192", target)
	})

	t.Run("async result rejects invalid task id", func(t *testing.T) {
		for _, bad := range []string{"", "..", "a/b", "a?b", `a\b`} {
			_, err := svc.buildZhipuActionURL(zhipuActionTestAccount(), ZhipuEndpointAsyncResult, bad)
			require.Error(t, err, "task id %q must be rejected", bad)
		}
	})

	t.Run("non zhipu account rejected", func(t *testing.T) {
		_, err := svc.buildZhipuActionURL(&Account{Platform: PlatformGrok, Type: AccountTypeAPIKey}, ZhipuEndpointEmbeddings, "")
		require.Error(t, err)
	})
}

func TestParseZhipuActionRequest(t *testing.T) {
	t.Run("video create json", func(t *testing.T) {
		info := ParseZhipuActionRequest(ZhipuEndpointVideosGenerations, "application/json",
			[]byte(`{"model":"cogvideox-3","prompt":"cat","size":"1920x1080","duration":10,"fps":30,"image_url":["https://a/f.png","https://a/l.png"]}`))
		require.Equal(t, "cogvideox-3", info.Model)
		require.Equal(t, "1920x1080", info.Size)
		require.Equal(t, 10, info.DurationSeconds)
		require.Equal(t, "cat", info.InputText)
		require.True(t, info.HasInputImage)
		require.JSONEq(t, `{"prompt":"cat"}`, string(info.ModerationBody()))
	})

	t.Run("image generate json", func(t *testing.T) {
		info := ParseZhipuActionRequest(ZhipuEndpointImagesGenerations, "application/json",
			[]byte(`{"model":"glm-image","prompt":"一只小猫","size":"1280x1280"}`))
		require.Equal(t, "glm-image", info.Model)
		require.Equal(t, "一只小猫", info.InputText)
	})

	t.Run("stt multipart model field", func(t *testing.T) {
		body := strings.Join([]string{
			"--boundary123",
			`Content-Disposition: form-data; name="model"`,
			"",
			"glm-asr-2512",
			"--boundary123",
			`Content-Disposition: form-data; name="file"; filename="a.wav"`,
			"Content-Type: audio/wav",
			"",
			"RIFF....binary....",
			"--boundary123--",
			"",
		}, "\r\n")
		info := ParseZhipuActionRequest(ZhipuEndpointAudioTranscriptions, "multipart/form-data; boundary=boundary123", []byte(body))
		require.Equal(t, "glm-asr-2512", info.Model)
	})

	t.Run("web search fields", func(t *testing.T) {
		info := ParseZhipuActionRequest(ZhipuEndpointWebSearch, "application/json",
			[]byte(`{"search_query":"今日新闻","search_engine":"search_std"}`))
		require.Equal(t, "今日新闻", info.InputText)
		require.Equal(t, "search_std", info.SearchEngine)
	})

	t.Run("invalid json yields empty info", func(t *testing.T) {
		info := ParseZhipuActionRequest(ZhipuEndpointImagesGenerations, "application/json", []byte(`{`))
		require.Empty(t, info.Model)
	})
}

func TestForwardZhipuActionImagesPositive(t *testing.T) {
	upstream := &zhipuActionUpstreamStub{
		responses: []*http.Response{
			zhipuActionJSONResponse(http.StatusOK, `{"created":1732083164,"data":[{"url":"https://cdn.bigmodel.cn/img.png"}],"content_filter":[{"role":"assistant","level":3}]}`),
		},
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	account := zhipuActionTestAccount()
	account.Credentials["model_mapping"] = map[string]any{"glm-image-public": "glm-image"}
	c, recorder := zhipuActionTestContext(t, http.MethodPost, "/v1/images/generations", nil)

	result, err := svc.ForwardZhipuAction(context.Background(), c, account,
		ZhipuEndpointImagesGenerations, "",
		[]byte(`{"model":"glm-image-public","prompt":"一只小猫","size":"1280x1280"}`), "application/json", "")

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 1, result.ImageCount)
	require.Equal(t, "glm-image-public", result.Model)
	require.Equal(t, "glm-image", result.UpstreamModel)
	require.Equal(t, 1, upstream.requestCount())
	req := upstream.requests[0]
	require.Equal(t, "https://open.bigmodel.cn/api/paas/v4/images/generations", req.URL.String())
	require.Equal(t, http.MethodPost, req.Method)
	require.Equal(t, "Bearer zhipu-upstream-key", req.Header.Get("Authorization"))
	sentBody, _ := io.ReadAll(req.Body)
	require.JSONEq(t, `{"model":"glm-image","prompt":"一只小猫","size":"1280x1280"}`, string(sentBody))
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), "https://cdn.bigmodel.cn/img.png")
}

func TestForwardZhipuActionImagesNoOutputIsFailover(t *testing.T) {
	upstream := &zhipuActionUpstreamStub{
		responses: []*http.Response{
			zhipuActionJSONResponse(http.StatusOK, `{"created":1732083164,"data":[],"content_filter":[{"role":"user","level":0}]}`),
		},
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	c, _ := zhipuActionTestContext(t, http.MethodPost, "/v1/images/generations", nil)

	_, err := svc.ForwardZhipuAction(context.Background(), c, zhipuActionTestAccount(),
		ZhipuEndpointImagesGenerations, "",
		[]byte(`{"model":"cogview-4","prompt":"blocked"}`), "application/json", "")

	require.Error(t, err)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
}

func TestForwardZhipuActionUpstreamErrorPassthrough(t *testing.T) {
	upstream := &zhipuActionUpstreamStub{
		responses: []*http.Response{
			zhipuActionJSONResponse(http.StatusBadRequest, `{"error":{"code":"1214","message":"prompt 长度超限"}}`),
		},
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	c, recorder := zhipuActionTestContext(t, http.MethodPost, "/v1/images/generations", nil)

	_, err := svc.ForwardZhipuAction(context.Background(), c, zhipuActionTestAccount(),
		ZhipuEndpointImagesGenerations, "",
		[]byte(`{"model":"cogview-4","prompt":"x"}`), "application/json", "")

	require.Error(t, err)
	require.NotZero(t, recorder.Code)
	require.NotEqual(t, http.StatusOK, recorder.Code)
}

func TestForwardZhipuActionVideoCreate(t *testing.T) {
	upstream := &zhipuActionUpstreamStub{
		responses: []*http.Response{
			zhipuActionJSONResponse(http.StatusOK, `{"id":"8868902201637896192","request_id":"req-1","model":"cogvideox-3","task_status":"PROCESSING"}`),
		},
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	c, recorder := zhipuActionTestContext(t, http.MethodPost, "/v1/videos/generations", nil)

	result, err := svc.ForwardZhipuAction(context.Background(), c, zhipuActionTestAccount(),
		ZhipuEndpointVideosGenerations, "",
		[]byte(`{"model":"cogvideox-3","prompt":"A cat is playing with a ball.","size":"1920x1080","fps":30,"duration":5}`), "application/json", "")

	require.NoError(t, err)
	require.Equal(t, "8868902201637896192", result.ResponseID)
	require.Zero(t, result.VideoCount, "async create must not bill immediately")
	require.Equal(t, "https://open.bigmodel.cn/api/paas/v4/videos/generations", upstream.requests[0].URL.String())
	require.Contains(t, recorder.Body.String(), "PROCESSING")
}

func TestForwardZhipuActionVideoStatusSuccessRewritesContentURL(t *testing.T) {
	upstream := &zhipuActionUpstreamStub{
		responses: []*http.Response{
			zhipuActionJSONResponse(http.StatusOK, `{"model":"cogvideox-3","task_status":"SUCCESS","request_id":"req-1","video_result":[{"url":"https://cdn.bigmodel.cn/video.mp4","cover_image_url":"https://cdn.bigmodel.cn/cover.png"}]}`),
		},
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	c, recorder := zhipuActionTestContext(t, http.MethodGet, "/v1/videos/task-1", nil)

	result, err := svc.ForwardZhipuAction(context.Background(), c, zhipuActionTestAccount(),
		ZhipuEndpointAsyncResult, "task-1", nil, "", "")

	require.NoError(t, err)
	require.Equal(t, 1, result.VideoCount)
	require.Equal(t, "cogvideox-3", result.Model)
	require.Equal(t, "task-1", result.ResponseID)
	require.Equal(t, "https://open.bigmodel.cn/api/paas/v4/async-result/task-1", upstream.requests[0].URL.String())
	require.Equal(t, http.MethodGet, upstream.requests[0].Method)
	require.Empty(t, upstream.requests[0].Header.Get("Content-Type"))
	require.Equal(t, "Bearer zhipu-upstream-key", upstream.requests[0].Header.Get("Authorization"))
	body := recorder.Body.String()
	require.Contains(t, body, "/v1/videos/task-1/content", "video_result.url must be rewritten to the gateway proxy")
	require.NotContains(t, body, "https://cdn.bigmodel.cn/video.mp4")
}

func TestForwardZhipuActionVideoStatusProcessingNotBillable(t *testing.T) {
	upstream := &zhipuActionUpstreamStub{
		responses: []*http.Response{
			zhipuActionJSONResponse(http.StatusOK, `{"model":"cogvideox-3","task_status":"PROCESSING","request_id":"req-1"}`),
		},
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	c, _ := zhipuActionTestContext(t, http.MethodGet, "/v1/videos/task-1", nil)

	result, err := svc.ForwardZhipuAction(context.Background(), c, zhipuActionTestAccount(),
		ZhipuEndpointAsyncResult, "task-1", nil, "", "")

	require.NoError(t, err)
	require.Zero(t, result.VideoCount)
}

func TestForwardZhipuVideoContentStreamsDownload(t *testing.T) {
	upstream := &zhipuActionUpstreamStub{
		responses: []*http.Response{
			zhipuActionJSONResponse(http.StatusOK, `{"model":"cogvideox-3","task_status":"SUCCESS","video_result":[{"url":"https://cdn.bigmodel.cn/video.mp4"}]}`),
			{
				StatusCode: http.StatusPartialContent,
				Header: http.Header{
					"Content-Type":   []string{"video/mp4"},
					"Content-Length": []string{"13"},
					"Content-Range":  []string{"bytes 0-12/100"},
				},
				Body: io.NopCloser(strings.NewReader("video-payload")),
			},
		},
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	c, recorder := zhipuActionTestContext(t, http.MethodGet, "/v1/videos/task-1/content", map[string]string{"Range": "bytes=0-12"})

	result, err := svc.ForwardZhipuAction(context.Background(), c, zhipuActionTestAccount(),
		ZhipuEndpointVideoContent, "task-1", nil, "", "")

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 1, result.VideoCount)
	require.Equal(t, 2, upstream.requestCount())
	contentReq := upstream.requests[1]
	require.Equal(t, "https://cdn.bigmodel.cn/video.mp4", contentReq.URL.String())
	require.Empty(t, contentReq.Header.Get("Authorization"), "CDN download must not leak the upstream credential")
	require.Equal(t, "bytes=0-12", contentReq.Header.Get("Range"))
	require.Equal(t, http.StatusPartialContent, recorder.Code)
	require.Equal(t, "video-payload", recorder.Body.String())
	require.Equal(t, "video/mp4", recorder.Header().Get("Content-Type"))
}

func TestForwardZhipuVideoContentUnreadyTaskReturnsStatus(t *testing.T) {
	upstream := &zhipuActionUpstreamStub{
		responses: []*http.Response{
			zhipuActionJSONResponse(http.StatusOK, `{"model":"cogvideox-3","task_status":"PROCESSING"}`),
		},
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	c, recorder := zhipuActionTestContext(t, http.MethodGet, "/v1/videos/task-1/content", nil)

	result, err := svc.ForwardZhipuAction(context.Background(), c, zhipuActionTestAccount(),
		ZhipuEndpointVideoContent, "task-1", nil, "", "")

	require.NoError(t, err)
	require.Zero(t, result.VideoCount)
	require.Equal(t, 1, upstream.requestCount(), "unready task must not reach the CDN")
	require.Contains(t, recorder.Body.String(), "PROCESSING")
}

func TestForwardZhipuActionEmbeddingsUsage(t *testing.T) {
	upstream := &zhipuActionUpstreamStub{
		responses: []*http.Response{
			zhipuActionJSONResponse(http.StatusOK, `{"model":"embedding-3","object":"list","data":[{"index":0,"object":"embedding","embedding":[0.1,0.2]}],"usage":{"prompt_tokens":7,"completion_tokens":0,"total_tokens":7}}`),
		},
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	c, recorder := zhipuActionTestContext(t, http.MethodPost, "/v1/embeddings", nil)

	result, err := svc.ForwardZhipuAction(context.Background(), c, zhipuActionTestAccount(),
		ZhipuEndpointEmbeddings, "",
		[]byte(`{"model":"embedding-3","input":"你好，今天天气怎么样.","dimensions":2048}`), "application/json", "")

	require.NoError(t, err)
	require.Equal(t, 7, result.Usage.InputTokens)
	require.Equal(t, "embedding-3", result.Model)
	require.Equal(t, "https://open.bigmodel.cn/api/paas/v4/embeddings", upstream.requests[0].URL.String())
	require.Contains(t, recorder.Body.String(), `"object":"list"`)
}

func TestForwardZhipuActionRerankUsage(t *testing.T) {
	upstream := &zhipuActionUpstreamStub{
		responses: []*http.Response{
			zhipuActionJSONResponse(http.StatusOK, `{"id":"20241120141244890ab4ee4af84acf","results":[{"document":"doc B","index":1,"relevance_score":0.99}],"usage":{"prompt_tokens":72,"total_tokens":72},"created":1732083164,"request_id":"r-1"}`),
		},
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	c, _ := zhipuActionTestContext(t, http.MethodPost, "/v1/rerank", nil)

	result, err := svc.ForwardZhipuAction(context.Background(), c, zhipuActionTestAccount(),
		ZhipuEndpointRerank, "",
		[]byte(`{"model":"rerank","query":"查询候选文本A","documents":["需要打分的候选文本A","需要打分的候选文本B"],"top_n":4}`), "application/json", "")

	require.NoError(t, err)
	require.Equal(t, 72, result.Usage.InputTokens)
	require.Equal(t, "rerank", result.Model)
	sentBody, _ := io.ReadAll(upstream.requests[0].Body)
	require.JSONEq(t, `{"model":"rerank","query":"查询候选文本A","documents":["需要打分的候选文本A","需要打分的候选文本B"],"top_n":4}`, string(sentBody))
}

func TestForwardZhipuActionAudioSpeech(t *testing.T) {
	upstream := &zhipuActionUpstreamStub{
		responses: []*http.Response{
			{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"audio/wav"}},
				Body:       io.NopCloser(strings.NewReader("RIFF-wav-bytes")),
			},
		},
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	c, recorder := zhipuActionTestContext(t, http.MethodPost, "/v1/audio/speech", nil)

	result, err := svc.ForwardZhipuAction(context.Background(), c, zhipuActionTestAccount(),
		ZhipuEndpointAudioSpeech, "",
		[]byte(`{"model":"glm-tts","input":"你好，今天天气怎么样.","voice":"tongtong","response_format":"wav"}`), "application/json", "")

	require.NoError(t, err)
	require.NotNil(t, result.AudioUsage)
	require.Equal(t, "tts", result.AudioUsage.Mode)
	require.InDelta(t, 11.0/1_000_000.0, result.AudioUsage.DurationOrUnits, 1e-9)
	require.Equal(t, "audio/wav", recorder.Header().Get("Content-Type"))
	require.Equal(t, "RIFF-wav-bytes", recorder.Body.String())
}

func TestForwardZhipuActionAudioTranscriptionsMultipart(t *testing.T) {
	upstream := &zhipuActionUpstreamStub{
		responses: []*http.Response{
			zhipuActionJSONResponse(http.StatusOK, `{"id":"asr-1","created":1732083164,"request_id":"r-1","model":"glm-asr-2512","text":"转录文本"}`),
		},
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	c, recorder := zhipuActionTestContext(t, http.MethodPost, "/v1/audio/transcriptions", nil)

	multipartBody := strings.Join([]string{
		"--boundary123",
		`Content-Disposition: form-data; name="model"`,
		"",
		"glm-asr-2512",
		"--boundary123",
		`Content-Disposition: form-data; name="file"; filename="a.wav"`,
		"Content-Type: audio/wav",
		"",
		strings.Repeat("x", 48000),
		"--boundary123--",
		"",
	}, "\r\n")
	contentType := "multipart/form-data; boundary=boundary123"

	result, err := svc.ForwardZhipuAction(context.Background(), c, zhipuActionTestAccount(),
		ZhipuEndpointAudioTranscriptions, "",
		[]byte(multipartBody), contentType, "")

	require.NoError(t, err)
	require.Equal(t, "glm-asr-2512", result.Model)
	require.NotNil(t, result.AudioUsage)
	require.Equal(t, "stt", result.AudioUsage.Mode)
	require.Greater(t, result.AudioUsage.DurationOrUnits, 0.0)
	req := upstream.requests[0]
	require.Equal(t, contentType, req.Header.Get("Content-Type"), "multipart boundary must survive")
	sentBody, _ := io.ReadAll(req.Body)
	require.Equal(t, multipartBody, string(sentBody), "multipart body must pass through untouched")
	require.Contains(t, recorder.Body.String(), "转录文本")
}

func TestForwardZhipuActionWebSearch(t *testing.T) {
	upstream := &zhipuActionUpstreamStub{
		responses: []*http.Response{
			zhipuActionJSONResponse(http.StatusOK, `{"id":"search-1","created":1732083164,"request_id":"r-1","search_result":[{"title":"t","link":"https://example.com","content":"c"}]}`),
		},
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	c, _ := zhipuActionTestContext(t, http.MethodPost, "/v1/web_search", nil)

	result, err := svc.ForwardZhipuAction(context.Background(), c, zhipuActionTestAccount(),
		ZhipuEndpointWebSearch, "",
		[]byte(`{"search_query":"今日新闻","search_engine":"search_std"}`), "application/json", "")

	require.NoError(t, err)
	require.Equal(t, 1, result.WebSearchCalls)
	require.Equal(t, "search_std", result.Model)
}

func TestForwardZhipuActionVoiceListQueryPassthrough(t *testing.T) {
	upstream := &zhipuActionUpstreamStub{
		responses: []*http.Response{
			zhipuActionJSONResponse(http.StatusOK, `{"voices":[{"voice":"tongtong","voice_type":"system"}],"total":1}`),
		},
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	c, recorder := zhipuActionTestContext(t, http.MethodGet, "/v1/voice/list?voice_name=tong&page=1", nil)

	result, err := svc.ForwardZhipuAction(context.Background(), c, zhipuActionTestAccount(),
		ZhipuEndpointVoiceList, "", nil, "", "voice_name=tong&page=1")

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "https://open.bigmodel.cn/api/paas/v4/voice/list?voice_name=tong&page=1", upstream.requests[0].URL.String())
	require.Equal(t, http.MethodGet, upstream.requests[0].Method)
	require.Contains(t, recorder.Body.String(), "tongtong")
}

func TestForwardZhipuActionClientCancelReachesUpstream(t *testing.T) {
	cancelingUpstream := &zhipuActionCancelStub{}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: cancelingUpstream}
	c, _ := zhipuActionTestContext(t, http.MethodPost, "/v1/images/generations", nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	_, err := svc.ForwardZhipuAction(ctx, c, zhipuActionTestAccount(),
		ZhipuEndpointImagesGenerations, "",
		[]byte(`{"model":"glm-image","prompt":"x"}`), "application/json", "")

	require.Error(t, err)
	require.True(t, cancelingUpstream.observedCancel, "client cancellation must propagate to the in-flight upstream request")
}

// zhipuActionCancelStub blocks like a slow upstream until the request context
// is done, recording whether client cancellation reached the wire.
type zhipuActionCancelStub struct {
	observedCancel bool
}

func (s *zhipuActionCancelStub) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	select {
	case <-req.Context().Done():
		s.observedCancel = true
		return nil, req.Context().Err()
	case <-time.After(5 * time.Second):
		return zhipuActionJSONResponse(http.StatusOK, `{"created":1,"data":[{"url":"https://cdn/x.png"}]}`), nil
	}
}

func (s *zhipuActionCancelStub) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return s.Do(req, proxyURL, accountID, accountConcurrency)
}

func TestZhipuVideoTaskSessionHashOwnerScoped(t *testing.T) {
	ownerHash := ZhipuVideoTaskSessionHash("task-1", 100, 200)
	require.NotEmpty(t, ownerHash)
	require.NotEqual(t, ownerHash, ZhipuVideoTaskSessionHash("task-1", 101, 200), "another user must not resolve the task")
	require.NotEqual(t, ownerHash, ZhipuVideoTaskSessionHash("task-1", 100, 201), "another key must not resolve the task")
	require.Empty(t, ZhipuVideoTaskSessionHash("", 100, 200))
	require.Empty(t, ZhipuVideoTaskSessionHash("task-1", 0, 200))
}

func TestZhipuVideoTaskBindingRoundTrip(t *testing.T) {
	cache := newZhipuActionCacheStub()
	svc := &OpenAIGatewayService{cfg: &config.Config{}, cache: cache}
	groupID := int64(55)
	ctx := context.Background()

	require.NoError(t, svc.BindZhipuVideoTaskAccount(ctx, &groupID, "task-1", 100, 200, 42))

	accountID, err := svc.ResolveZhipuVideoTaskAccount(ctx, &groupID, "task-1", 100, 200)
	require.NoError(t, err)
	require.Equal(t, int64(42), accountID)

	_, err = svc.ResolveZhipuVideoTaskAccount(ctx, &groupID, "task-1", 101, 200)
	require.Error(t, err, "cross-tenant task lookup must miss the binding")
}

func TestZhipuVideoPendingBillingStoreLoadClaimRelease(t *testing.T) {
	cache := newZhipuActionCacheStub()
	svc := &OpenAIGatewayService{cfg: &config.Config{}, cache: cache}
	ctx := context.Background()

	pending := BuildZhipuVideoPendingBilling(
		ZhipuActionRequestInfo{Model: "cogvideox-3", Size: "1920x1080", DurationSeconds: 10},
		"cogvideox-3", "", "public-video", ZhipuVideoPendingCreatedAtNow(),
	)
	require.NoError(t, svc.StoreZhipuVideoPendingBilling(ctx, "task-1", 100, 200, pending))

	loaded, err := svc.LoadZhipuVideoPendingBilling(ctx, "task-1", 100, 200)
	require.NoError(t, err)
	require.NotNil(t, loaded)
	require.Equal(t, "cogvideox-3", loaded.Model)
	require.Equal(t, 10, loaded.VideoDurationSeconds)
	require.Equal(t, VideoBillingResolution1080P, loaded.VideoResolution)

	otherUser, err := svc.LoadZhipuVideoPendingBilling(ctx, "task-1", 101, 200)
	require.NoError(t, err)
	require.Nil(t, otherUser, "pending snapshot must stay owner-scoped")

	claimed, err := svc.ClaimZhipuVideoBilling(ctx, "task-1", 100, 200)
	require.NoError(t, err)
	require.True(t, claimed)
	again, err := svc.ClaimZhipuVideoBilling(ctx, "task-1", 100, 200)
	require.NoError(t, err)
	require.False(t, again, "billing claim must be one-shot (idempotent completion)")

	require.NoError(t, svc.ReleaseZhipuVideoBilling(ctx, "task-1", 100, 200))
	afterRelease, err := svc.ClaimZhipuVideoBilling(ctx, "task-1", 100, 200)
	require.NoError(t, err)
	require.True(t, afterRelease, "release must let a failed RecordUsage retry billing")
}

func TestBuildZhipuVideoPendingBillingDefaults(t *testing.T) {
	defaulted := BuildZhipuVideoPendingBilling(
		ZhipuActionRequestInfo{Model: "cogvideox-3"},
		"", "", "", "",
	)
	require.Equal(t, 5, defaulted.VideoDurationSeconds, "documented cogvideox default duration is 5s")
	require.NotEmpty(t, defaulted.CreatedAt)

	tests := []struct {
		size string
		want string
	}{
		{"720x480", VideoBillingResolution480P},
		{"1280x720", VideoBillingResolution720P},
		{"720x1280", VideoBillingResolution720P},
		{"1920x1080", VideoBillingResolution1080P},
		{"1080x1920", VideoBillingResolution1080P},
		{"3840x2160", VideoBillingResolution1080P},
		{"2048x1080", VideoBillingResolution1080P},
	}
	for _, tt := range tests {
		t.Run(tt.size, func(t *testing.T) {
			pending := BuildZhipuVideoPendingBilling(
				ZhipuActionRequestInfo{Model: "cogvideox-3", Size: tt.size},
				"", "", "", "",
			)
			require.Equal(t, tt.want, pending.VideoResolution)
		})
	}
}

func TestMergeZhipuVideoPendingBilling(t *testing.T) {
	pending := &ZhipuVideoPendingBilling{
		Model:                "cogvideox-3",
		BillingModel:         "cogvideox-3",
		UpstreamModel:        "cogvideox-3",
		VideoResolution:      VideoBillingResolution720P,
		VideoDurationSeconds: 5,
		CreatedAt:            time.Now().Add(-2 * time.Minute).UTC().Format(time.RFC3339Nano),
	}
	statusResult := &OpenAIForwardResult{ResponseID: "task-1", VideoCount: 1}

	merged := MergeZhipuVideoPendingBilling(statusResult, pending, "task-1")

	require.NotNil(t, merged)
	require.Equal(t, "cogvideox-3", merged.Model)
	require.Equal(t, VideoBillingResolution720P, merged.VideoResolution)
	require.Equal(t, 5, merged.VideoDurationSeconds)
	require.Equal(t, "zhipu-video:task-1", merged.RequestID)
	require.GreaterOrEqual(t, merged.Duration, 2*time.Minute, "duration must be E2E create-accept to discovery")
}

func TestIsZhipuVideoTaskSuccess(t *testing.T) {
	require.True(t, IsZhipuVideoTaskSuccess([]byte(`{"task_status":"SUCCESS","video_result":[{"url":"https://cdn/v.mp4"}]}`)))
	require.False(t, IsZhipuVideoTaskSuccess([]byte(`{"task_status":"PROCESSING"}`)))
	require.False(t, IsZhipuVideoTaskSuccess([]byte(`{"task_status":"FAIL"}`)))
	require.False(t, IsZhipuVideoTaskSuccess([]byte(`{"task_status":"SUCCESS","video_result":[]}`)))
	require.False(t, IsZhipuVideoTaskSuccess([]byte(`{"task_status":"SUCCESS","video_result":[{"url":" "}]}`)))
	require.False(t, IsZhipuVideoTaskSuccess(nil))
}

func TestEstimateZhipuAudioUsage(t *testing.T) {
	tts := estimateZhipuAudioUsage(ZhipuEndpointAudioSpeech,
		[]byte(`{"model":"glm-tts","input":"你好世界"}`), nil, time.Second)
	require.NotNil(t, tts)
	require.Equal(t, "tts", tts.Mode)
	require.InDelta(t, 4.0/1_000_000.0, tts.DurationOrUnits, 1e-9)

	require.Nil(t, estimateZhipuAudioUsage(ZhipuEndpointAudioSpeech, []byte(`{"model":"glm-tts"}`), nil, time.Second))

	stt := estimateZhipuAudioUsage(ZhipuEndpointAudioTranscriptions,
		[]byte(strings.Repeat("x", 16000*30)), nil, 2*time.Second)
	require.NotNil(t, stt)
	require.Equal(t, "stt", stt.Mode)
	require.InDelta(t, 30.0/3600.0, stt.DurationOrUnits, 1e-6, "body-size floor must bill the real audio scale")

	require.Nil(t, estimateZhipuAudioUsage(ZhipuEndpointVoiceClone, []byte(`{}`), nil, time.Second))
}

func TestStableZhipuBillingRequestIDs(t *testing.T) {
	require.Equal(t, "zhipu-video:task-1", StableZhipuVideoBillingRequestID("task-1"))
	require.Equal(t, "zhipu-video:task-1", StableZhipuVideoBillingRequestID("zhipu-video:task-1"))
	require.Empty(t, StableZhipuVideoBillingRequestID(""))

	require.Equal(t, "zhipu_audio:r-1", StableZhipuAudioBillingRequestID("r-1"))
	require.Equal(t, "zhipu_audio:r-1", StableZhipuAudioBillingRequestID("zhipu_audio:r-1"))
	require.True(t, strings.HasPrefix(StableZhipuAudioBillingRequestID(""), "zhipu_audio:"))

	require.Equal(t, "zhipu_realtime:r-1", StableZhipuRealtimeBillingRequestID("r-1"))
	require.True(t, strings.HasPrefix(StableZhipuRealtimeBillingRequestID(""), "zhipu_realtime:"))
}

func TestZhipuRealtimeEventHasAudio(t *testing.T) {
	require.True(t, zhipuRealtimeEventHasAudio([]byte(`{"type":"response.audio.delta","delta":"QUJD"}`)))
	require.True(t, zhipuRealtimeEventHasAudio([]byte(`{"type":"input_audio_buffer.append","audio":"QUJD"}`)))
	require.False(t, zhipuRealtimeEventHasAudio([]byte(`{"type":"response.audio_transcript.delta","delta":"hello"}`)))
	require.False(t, zhipuRealtimeEventHasAudio([]byte(`{"type":"session.updated","session":{}}`)))
	require.False(t, zhipuRealtimeEventHasAudio([]byte(`not-json`)))
}

func TestZhipuVideoContentProxyURL(t *testing.T) {
	c, _ := zhipuActionTestContext(t, http.MethodGet, "/v1/videos/task-1", nil)
	require.Equal(t, "/v1/videos/task-1/content", zhipuVideoContentProxyURL(c, "task-1"))

	c2, _ := zhipuActionTestContext(t, http.MethodGet, "/videos/task-1", nil)
	require.Equal(t, "/videos/task-1/content", zhipuVideoContentProxyURL(c2, "task-1"))
}
