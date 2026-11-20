//go:build unit

package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type qianfanActionUpstreamStub struct {
	mu        sync.Mutex
	requests  []*http.Request
	responses []*http.Response
	err       error
}

func (s *qianfanActionUpstreamStub) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
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

func (s *qianfanActionUpstreamStub) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return s.Do(req, proxyURL, accountID, accountConcurrency)
}

func (s *qianfanActionUpstreamStub) requestCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.requests)
}

func (s *qianfanActionUpstreamStub) lastRequest(t *testing.T) *http.Request {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	require.NotEmpty(t, s.requests)
	return s.requests[len(s.requests)-1]
}

func qianfanActionJSONResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func qianfanActionBinaryResponse(contentType, body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{contentType}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func qianfanActionTestAccount() *Account {
	return &Account{
		ID:       21,
		Platform: PlatformQianfan,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "qianfan-upstream-key",
		},
	}
}

func qianfanActionTestContext(t *testing.T, method, target string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(method, target, nil)
	return c, recorder
}

type qianfanActionCacheStub struct {
	mu              sync.Mutex
	sessionBindings map[string]int64
	pendingBilling  map[string][]byte
	claims          map[string]bool
}

func newQianfanActionCacheStub() *qianfanActionCacheStub {
	return &qianfanActionCacheStub{
		sessionBindings: map[string]int64{},
		pendingBilling:  map[string][]byte{},
		claims:          map[string]bool{},
	}
}

func (s *qianfanActionCacheStub) GetSessionAccountID(_ context.Context, _ int64, sessionHash string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id, ok := s.sessionBindings[sessionHash]; ok {
		return id, nil
	}
	return 0, ErrStickySessionNotFound
}

func (s *qianfanActionCacheStub) SetSessionAccountID(_ context.Context, _ int64, sessionHash string, accountID int64, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessionBindings[sessionHash] = accountID
	return nil
}

func (s *qianfanActionCacheStub) RefreshSessionTTL(_ context.Context, _ int64, _ string, _ time.Duration) error {
	return nil
}

func (s *qianfanActionCacheStub) DeleteSessionAccountID(_ context.Context, _ int64, sessionHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessionBindings, sessionHash)
	return nil
}

func (s *qianfanActionCacheStub) SetGrokVideoPendingBilling(_ context.Context, key string, payload []byte, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pendingBilling[key] = payload
	return nil
}

func (s *qianfanActionCacheStub) GetGrokVideoPendingBilling(_ context.Context, key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pendingBilling[key], nil
}

func (s *qianfanActionCacheStub) ClaimGrokVideoBilled(_ context.Context, key string, _ time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.claims[key] {
		return false, nil
	}
	s.claims[key] = true
	return true, nil
}

func (s *qianfanActionCacheStub) ReleaseGrokVideoBilled(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.claims, key)
	return nil
}

func (s *qianfanActionCacheStub) SetReasoningContent(_ context.Context, _ string, _ string, _ time.Duration) error {
	return nil
}

func (s *qianfanActionCacheStub) GetReasoningContent(_ context.Context, _ string) (string, error) {
	return "", ErrReasoningContentNotFound
}

func TestBuildQianfanActionURL(t *testing.T) {
	svc := &OpenAIGatewayService{cfg: &config.Config{}}
	account := qianfanActionTestAccount()

	t.Run("modelbuilder actions stay under v2", func(t *testing.T) {
		for endpoint, want := range map[QianfanActionEndpoint]string{
			QianfanEndpointEmbeddings:        "https://qianfan.baidubce.com/v2/embeddings",
			QianfanEndpointRerank:            "https://qianfan.baidubce.com/v2/rerank",
			QianfanEndpointImagesGenerations: "https://qianfan.baidubce.com/v2/images/generations",
			QianfanEndpointImagesEdits:       "https://qianfan.baidubce.com/v2/images/edits",
			QianfanEndpointSearchWebSearch:   "https://qianfan.baidubce.com/v2/ai_search/web_search",
		} {
			target, err := svc.buildQianfanActionURL(account, endpoint, "")
			require.NoError(t, err)
			require.Equal(t, want, target)
		}
	})

	t.Run("video actions live at the host root", func(t *testing.T) {
		target, err := svc.buildQianfanActionURL(account, QianfanEndpointVideoGenerate, "")
		require.NoError(t, err)
		require.Equal(t, "https://qianfan.baidubce.com/video/generations", target)
		require.NotContains(t, target, "/v2/video")

		status, err := svc.buildQianfanActionURL(account, QianfanEndpointVideoStatus, "task-42")
		require.NoError(t, err)
		require.Equal(t, "https://qianfan.baidubce.com/video/generations?task_id=task-42", status)

		cancel, err := svc.buildQianfanActionURL(account, QianfanEndpointVideoCancel, "task-42")
		require.NoError(t, err)
		require.Equal(t, "https://qianfan.baidubce.com/video/generations?task_id=task-42", cancel)
	})

	t.Run("video status rejects a malformed task id", func(t *testing.T) {
		_, err := svc.buildQianfanActionURL(account, QianfanEndpointVideoStatus, "bad&id")
		require.Error(t, err)
	})

	t.Run("custom relay base url is honoured for v2 paths", func(t *testing.T) {
		relay := qianfanActionTestAccount()
		relay.Credentials["base_url"] = "https://relay.example.com/v2"
		target, err := svc.buildQianfanActionURL(relay, QianfanEndpointEmbeddings, "")
		require.NoError(t, err)
		require.Equal(t, "https://relay.example.com/v2/embeddings", target)

		video, err := svc.buildQianfanActionURL(relay, QianfanEndpointVideoGenerate, "")
		require.NoError(t, err)
		require.Equal(t, "https://relay.example.com/video/generations", video)
	})

	t.Run("speech products keep their first-party hosts", func(t *testing.T) {
		tts, err := svc.buildQianfanActionURL(account, QianfanEndpointAudioSpeech, "")
		require.NoError(t, err)
		require.Equal(t, "https://tsn.baidu.com/text2audio", tts)

		voice, err := svc.buildQianfanActionURL(account, QianfanEndpointVoiceCloneCreate, "")
		require.NoError(t, err)
		require.Equal(t, "https://aip.baidubce.com/rest/2.0/speech/publiccloudspeech/v1/voice/clone/create", voice)

		relay := qianfanActionTestAccount()
		relay.Credentials["base_url"] = "https://relay.example.com/v2"
		tts, err = svc.buildQianfanActionURL(relay, QianfanEndpointAudioSpeech, "")
		require.NoError(t, err)
		require.Equal(t, "https://tsn.baidu.com/text2audio", tts)
	})

	t.Run("non qianfan account is rejected", func(t *testing.T) {
		other := qianfanActionTestAccount()
		other.Platform = PlatformZhipu
		_, err := svc.buildQianfanActionURL(other, QianfanEndpointEmbeddings, "")
		require.Error(t, err)
	})
}

func TestQianfanActionAuthSurface(t *testing.T) {
	svc := &OpenAIGatewayService{cfg: &config.Config{}}

	searchHeader := http.Header{}
	svc.applyQianfanActionAuth(QianfanEndpointSearchWebSearch, searchHeader, "builder-key")
	require.Equal(t, "Bearer builder-key", searchHeader.Get("X-Appbuilder-Authorization"))
	require.Empty(t, searchHeader.Get("Authorization"))

	modelHeader := http.Header{}
	modelHeader.Set("X-Appbuilder-Authorization", "stale")
	svc.applyQianfanActionAuth(QianfanEndpointEmbeddings, modelHeader, "model-key")
	require.Equal(t, "Bearer model-key", modelHeader.Get("Authorization"))
	require.Empty(t, modelHeader.Get("X-Appbuilder-Authorization"))

	speechHeader := http.Header{}
	svc.applyQianfanActionAuth(QianfanEndpointAudioSpeech, speechHeader, "speech-key")
	require.Equal(t, "Bearer speech-key", speechHeader.Get("Authorization"))
	require.Empty(t, speechHeader.Get("X-Appbuilder-Authorization"))
}

func TestParseQianfanActionRequest(t *testing.T) {
	t.Run("image generation prompt and size", func(t *testing.T) {
		info := ParseQianfanActionRequest(QianfanEndpointImagesGenerations, "application/json",
			[]byte(`{"model":"ernie-image","prompt":"海边日落","size":"1024x1024","n":2}`))
		require.Equal(t, "ernie-image", info.Model)
		require.Equal(t, "海边日落", info.InputText)
		require.Equal(t, "1024x1024", info.Size)
	})

	t.Run("rerank query", func(t *testing.T) {
		info := ParseQianfanActionRequest(QianfanEndpointRerank, "application/json",
			[]byte(`{"model":"bce-reranker-base","query":"上海天气","documents":["上海气候","北京美食"]}`))
		require.Equal(t, "bce-reranker-base", info.Model)
		require.Equal(t, "上海天气", info.InputText)
	})

	t.Run("embeddings string and list input", func(t *testing.T) {
		info := ParseQianfanActionRequest(QianfanEndpointEmbeddings, "application/json",
			[]byte(`{"model":"embedding-v1","input":["a","b"]}`))
		require.Equal(t, []string{"a", "b"}, info.Input)
	})

	t.Run("video create collects text and image parts", func(t *testing.T) {
		info := ParseQianfanActionRequest(QianfanEndpointVideoGenerate, "application/json",
			[]byte(`{"model":"musesteamer-air-i2v","content":[{"type":"text","text":"嫦娥奔月"},{"type":"image_url","image_url":{"url":"https://x/y.png"}}],"duration":5}`))
		require.Equal(t, "嫦娥奔月", info.InputText)
		require.True(t, info.HasInputImage)
		require.Equal(t, 5, info.DurationSeconds)
	})

	t.Run("search reads the first message", func(t *testing.T) {
		info := ParseQianfanActionRequest(QianfanEndpointSearchWebSearch, "application/json",
			[]byte(`{"messages":[{"role":"user","content":"北京有哪些景区"}]}`))
		require.Equal(t, "北京有哪些景区", info.InputText)
	})

	t.Run("multipart surfaces only the model field", func(t *testing.T) {
		contentType := "multipart/form-data; boundary=BOUND"
		body := strings.Join([]string{
			"--BOUND", `Content-Disposition: form-data; name="model"`, "", "ernie-image", "--BOUND--", "",
		}, "\r\n")
		info := ParseQianfanActionRequest(QianfanEndpointImagesEdits, contentType, []byte(body))
		require.Equal(t, "ernie-image", info.Model)
	})

	t.Run("speech reads input and voice", func(t *testing.T) {
		info := ParseQianfanActionRequest(QianfanEndpointAudioSpeech, "application/json",
			[]byte(`{"input":"你好世界","voice":"5003","response_format":"wav","speed":1.0}`))
		require.Equal(t, "你好世界", info.InputText)
		require.Equal(t, "5003", info.Voice)
		require.Equal(t, "wav", info.ResponseFormat)
	})
}

func TestBuildQianfanSpeechBody(t *testing.T) {
	info := QianfanActionRequestInfo{InputText: "今天天气不错", Voice: "5003", ResponseFormat: "wav", Speed: 1.0}
	body, contentType, err := buildQianfanSpeechBody(info, "req-1")
	require.NoError(t, err)
	require.Equal(t, "application/x-www-form-urlencoded", contentType)
	values, err := url.ParseQuery(string(body))
	require.NoError(t, err)
	require.Equal(t, "今天天气不错", values.Get("tex"))
	require.Equal(t, "5003", values.Get("per"))
	require.Equal(t, "6", values.Get("aue"))
	require.Equal(t, "5", values.Get("spd"))
	require.Equal(t, "1", values.Get("ctp"))
	require.Equal(t, "zh", values.Get("lan"))
	require.Equal(t, "req-1", values.Get("cuid"))

	_, _, err = buildQianfanSpeechBody(QianfanActionRequestInfo{}, "req-2")
	require.Error(t, err)
}

func TestForwardQianfanEmbeddings(t *testing.T) {
	upstream := &qianfanActionUpstreamStub{responses: []*http.Response{qianfanActionJSONResponse(http.StatusOK,
		`{"id":"as-1","object":"list","created":1737030777,"data":[{"object":"embedding","embedding":[0.1],"index":0}],"model":"embedding-v1","usage":{"prompt_tokens":3,"total_tokens":3}}`)}}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	account := qianfanActionTestAccount()
	account.Credentials["model_mapping"] = map[string]any{"embedding-public": "embedding-v1"}
	c, _ := qianfanActionTestContext(t, http.MethodPost, "/v1/embeddings")

	result, err := svc.ForwardQianfanAction(context.Background(), c, account,
		QianfanEndpointEmbeddings, "", []byte(`{"model":"embedding-public","input":["White T-shirt"]}`), "application/json", "")

	require.NoError(t, err)
	require.Equal(t, 3, result.Usage.InputTokens)
	require.Equal(t, "embedding-public", result.Model)
	require.Equal(t, "embedding-v1", result.UpstreamModel)
	req := upstream.lastRequest(t)
	require.Equal(t, "https://qianfan.baidubce.com/v2/embeddings", req.URL.String())
	require.Equal(t, "Bearer qianfan-upstream-key", req.Header.Get("Authorization"))
	require.Empty(t, req.Header.Get("X-Appbuilder-Authorization"))
	body, _ := io.ReadAll(req.Body)
	require.Contains(t, string(body), `"model":"embedding-v1"`)
}

func TestForwardQianfanRerank(t *testing.T) {
	upstream := &qianfanActionUpstreamStub{responses: []*http.Response{qianfanActionJSONResponse(http.StatusOK,
		`{"id":"as-2","object":"reranker_list","model":"bce-reranker-base","results":[{"document":"上海气候","relevance_score":0.68,"index":0}],"usage":{"prompt_tokens":11,"total_tokens":11}}`)}}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	c, _ := qianfanActionTestContext(t, http.MethodPost, "/v1/rerank")

	result, err := svc.ForwardQianfanAction(context.Background(), c, qianfanActionTestAccount(),
		QianfanEndpointRerank, "", []byte(`{"model":"bce-reranker-base","query":"上海天气","documents":["上海气候"]}`), "application/json", "")

	require.NoError(t, err)
	require.Equal(t, 11, result.Usage.InputTokens)
	require.Equal(t, "https://qianfan.baidubce.com/v2/rerank", upstream.lastRequest(t).URL.String())
}

func TestForwardQianfanImages(t *testing.T) {
	t.Run("positive image output is counted", func(t *testing.T) {
		upstream := &qianfanActionUpstreamStub{responses: []*http.Response{qianfanActionJSONResponse(http.StatusOK,
			`{"id":"as-3","created":1735264326,"data":[{"url":"https://qianfan-img-gen.bj.bcebos.com/x.png"}]}`)}}
		svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
		c, recorder := qianfanActionTestContext(t, http.MethodPost, "/v1/images/generations")

		result, err := svc.ForwardQianfanAction(context.Background(), c, qianfanActionTestAccount(),
			QianfanEndpointImagesGenerations, "", []byte(`{"model":"ernie-image","prompt":"海边日落"}`), "application/json", "")

		require.NoError(t, err)
		require.Equal(t, 1, result.ImageCount)
		require.Equal(t, http.StatusOK, recorder.Code)
		require.Contains(t, recorder.Body.String(), "qianfan-img-gen")
	})

	t.Run("empty image output fails over instead of billing", func(t *testing.T) {
		upstream := &qianfanActionUpstreamStub{responses: []*http.Response{qianfanActionJSONResponse(http.StatusOK,
			`{"id":"as-4","created":1,"data":[]}`)}}
		svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
		c, _ := qianfanActionTestContext(t, http.MethodPost, "/v1/images/generations")

		_, err := svc.ForwardQianfanAction(context.Background(), c, qianfanActionTestAccount(),
			QianfanEndpointImagesGenerations, "", []byte(`{"model":"ernie-image","prompt":"x"}`), "application/json", "")

		var failover *UpstreamFailoverError
		require.ErrorAs(t, err, &failover)
		require.Equal(t, http.StatusBadGateway, failover.StatusCode)
	})

	t.Run("image edit bridges multipart to the documented json body", func(t *testing.T) {
		upstream := &qianfanActionUpstreamStub{responses: []*http.Response{qianfanActionJSONResponse(http.StatusOK,
			`{"id":"as-5","created":1,"data":[{"url":"https://x/y.png"}]}`)}}
		svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
		c, _ := qianfanActionTestContext(t, http.MethodPost, "/v1/images/edits")

		var multipartBody strings.Builder
		multipartBody.WriteString("--eboundary\r\nContent-Disposition: form-data; name=\"model\"\r\n\r\nernie-irag-edit\r\n")
		multipartBody.WriteString("--eboundary\r\nContent-Disposition: form-data; name=\"prompt\"\r\n\r\n替换背景\r\n")
		multipartBody.WriteString("--eboundary\r\nContent-Disposition: form-data; name=\"n\"\r\n\r\n2\r\n")
		multipartBody.WriteString("--eboundary\r\nContent-Disposition: form-data; name=\"image\"; filename=\"source.png\"\r\nContent-Type: image/png\r\n\r\nPNGDATA\r\n")
		multipartBody.WriteString("--eboundary\r\nContent-Disposition: form-data; name=\"mask\"; filename=\"mask.png\"\r\nContent-Type: image/png\r\n\r\nMASKDATA\r\n")
		multipartBody.WriteString("--eboundary--\r\n")
		contentType := "multipart/form-data; boundary=eboundary"

		result, err := svc.ForwardQianfanAction(context.Background(), c, qianfanActionTestAccount(),
			QianfanEndpointImagesEdits, "", []byte(multipartBody.String()), contentType, "")

		require.NoError(t, err)
		require.Equal(t, 1, result.ImageCount)
		req := upstream.lastRequest(t)
		require.Equal(t, "https://qianfan.baidubce.com/v2/images/edits", req.URL.String())
		require.Equal(t, "application/json", req.Header.Get("Content-Type"), "multipart edits bridge to the official JSON body")
		upstreamBody, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		require.Equal(t, "ernie-irag-edit", gjson.GetBytes(upstreamBody, "model").String())
		require.Equal(t, "替换背景", gjson.GetBytes(upstreamBody, "prompt").String())
		require.Equal(t, int64(2), gjson.GetBytes(upstreamBody, "n").Int())
		require.True(t, strings.HasPrefix(gjson.GetBytes(upstreamBody, "image").String(), "data:image/png;base64,"),
			"the uploaded source image becomes a documented data-URL image field")
		require.True(t, strings.HasPrefix(gjson.GetBytes(upstreamBody, "mask").String(), "data:image/png;base64,"))
	})

	t.Run("image edit without an image part is a client fault", func(t *testing.T) {
		upstream := &qianfanActionUpstreamStub{responses: []*http.Response{qianfanActionJSONResponse(http.StatusOK, `{}`)}}
		svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
		c, recorder := qianfanActionTestContext(t, http.MethodPost, "/v1/images/edits")

		_, err := svc.ForwardQianfanAction(context.Background(), c, qianfanActionTestAccount(),
			QianfanEndpointImagesEdits, "",
			[]byte("--BOUND\r\nContent-Disposition: form-data; name=\"model\"\r\n\r\nernie-image\r\n--BOUND--\r\n"),
			"multipart/form-data; boundary=BOUND", "")

		require.ErrorIs(t, err, errQianfanImageEditImageRequired)
		require.Equal(t, http.StatusBadRequest, recorder.Code, "a malformed client body must not become a gateway 502")
		require.Contains(t, recorder.Body.String(), "image part")
		require.Zero(t, upstream.requestCount(), "the client fault is rejected before any upstream call")
	})
}

func TestForwardQianfanVideoLifecycle(t *testing.T) {
	upstream := &qianfanActionUpstreamStub{responses: []*http.Response{
		qianfanActionJSONResponse(http.StatusOK, `{"id":"as-6","task_id":"67c999d8e8c05a4"}`),
		qianfanActionJSONResponse(http.StatusOK, `{"id":"as-7","task_id":"67c999d8e8c05a4","model":"musesteamer-air-i2v","status":"running"}`),
		qianfanActionJSONResponse(http.StatusOK, `{"id":"as-8","task_id":"67c999d8e8c05a4","model":"musesteamer-air-i2v","status":"succeeded","content":{"video_url":"https://cdn.example.com/v.mp4"},"duration":5,"width":1280,"height":704}`),
		qianfanActionJSONResponse(http.StatusOK, `{"id":"as-9","task_id":"67c999d8e8c05a4","status":"Terminated"}`),
	}}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}

	createCtx, _ := qianfanActionTestContext(t, http.MethodPost, "/v1/videos")
	created, err := svc.ForwardQianfanAction(context.Background(), createCtx, qianfanActionTestAccount(),
		QianfanEndpointVideoGenerate, "",
		[]byte(`{"model":"musesteamer-air-i2v","content":[{"type":"image_url","image_url":{"url":"https://x/y.png"}}],"duration":5}`),
		"application/json", "")
	require.NoError(t, err)
	require.Equal(t, "67c999d8e8c05a4", created.ResponseID)
	createReq := upstream.lastRequest(t)
	require.Equal(t, "https://qianfan.baidubce.com/video/generations", createReq.URL.String())

	statusCtx, _ := qianfanActionTestContext(t, http.MethodGet, "/v1/videos/67c999d8e8c05a4")
	running, err := svc.ForwardQianfanAction(context.Background(), statusCtx, qianfanActionTestAccount(),
		QianfanEndpointVideoStatus, "67c999d8e8c05a4", nil, "", "")
	require.NoError(t, err)
	require.Zero(t, running.VideoCount)

	succeeded, err := svc.ForwardQianfanAction(context.Background(), statusCtx, qianfanActionTestAccount(),
		QianfanEndpointVideoStatus, "67c999d8e8c05a4", nil, "", "")
	require.NoError(t, err)
	require.Equal(t, 1, succeeded.VideoCount)
	require.Equal(t, VideoBillingResolution720P, succeeded.VideoResolution)
	require.True(t, IsQianfanVideoTaskSuccess([]byte(`{"status":"succeeded","content":{"video_url":"https://cdn/x.mp4"}}`)))
	require.False(t, IsQianfanVideoTaskSuccess([]byte(`{"status":"running","content":{"video_url":"https://cdn/x.mp4"}}`)))

	cancelCtx, _ := qianfanActionTestContext(t, http.MethodDelete, "/v1/videos/67c999d8e8c05a4")
	cancelled, err := svc.ForwardQianfanAction(context.Background(), cancelCtx, qianfanActionTestAccount(),
		QianfanEndpointVideoCancel, "67c999d8e8c05a4", nil, "", "")
	require.NoError(t, err)
	require.NotNil(t, cancelled)
	cancelReq := upstream.lastRequest(t)
	require.Equal(t, http.MethodDelete, cancelReq.Method)
	require.Contains(t, cancelReq.URL.String(), "task_id=67c999d8e8c05a4")
}

func TestForwardQianfanVideoContentDownload(t *testing.T) {
	upstream := &qianfanActionUpstreamStub{responses: []*http.Response{
		qianfanActionJSONResponse(http.StatusOK, `{"id":"as-10","task_id":"t1","status":"succeeded","content":{"video_url":"https://cdn.example.com/v.mp4"}}`),
		qianfanActionBinaryResponse("video/mp4", "MP4BYTES"),
	}}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	c, recorder := qianfanActionTestContext(t, http.MethodGet, "/v1/videos/t1/content")

	result, err := svc.ForwardQianfanAction(context.Background(), c, qianfanActionTestAccount(),
		QianfanEndpointVideoContent, "t1", nil, "", "")

	require.NoError(t, err)
	require.Equal(t, 1, result.VideoCount)
	require.Equal(t, "MP4BYTES", recorder.Body.String())
	require.Equal(t, 2, upstream.requestCount())
}

func TestForwardQianfanSearchUsesAppBuilderAuth(t *testing.T) {
	upstream := &qianfanActionUpstreamStub{responses: []*http.Response{qianfanActionJSONResponse(http.StatusOK,
		`{"request_id":"req-1","references":[{"id":1,"title":"t","url":"https://x","type":"web"}]}`)}}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	c, _ := qianfanActionTestContext(t, http.MethodPost, "/v1/search")

	result, err := svc.ForwardQianfanAction(context.Background(), c, qianfanActionTestAccount(),
		QianfanEndpointSearchWebSearch, "", []byte(`{"messages":[{"role":"user","content":"北京景区"}],"search_source":"baidu_search_v2"}`), "application/json", "")

	require.NoError(t, err)
	require.Equal(t, 1, result.WebSearchCalls)
	req := upstream.lastRequest(t)
	require.Equal(t, "https://qianfan.baidubce.com/v2/ai_search/web_search", req.URL.String())
	require.Equal(t, "Bearer qianfan-upstream-key", req.Header.Get("X-Appbuilder-Authorization"))
	require.Empty(t, req.Header.Get("Authorization"))
}

func TestForwardQianfanAudioSpeech(t *testing.T) {
	t.Run("success relays binary audio and records tts units", func(t *testing.T) {
		upstream := &qianfanActionUpstreamStub{responses: []*http.Response{qianfanActionBinaryResponse("audio/mp3", "MP3DATA")}}
		svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
		c, recorder := qianfanActionTestContext(t, http.MethodPost, "/v1/audio/speech")

		result, err := svc.ForwardQianfanAction(context.Background(), c, qianfanActionTestAccount(),
			QianfanEndpointAudioSpeech, "", []byte(`{"model":"tts","input":"今天天气不错","voice":"5003"}`), "application/json", "")

		require.NoError(t, err)
		require.Equal(t, "MP3DATA", recorder.Body.String())
		require.NotNil(t, result.AudioUsage)
		require.Equal(t, "tts", result.AudioUsage.Mode)
		req := upstream.lastRequest(t)
		require.Equal(t, "https://tsn.baidu.com/text2audio", req.URL.String())
		require.Equal(t, "Bearer qianfan-upstream-key", req.Header.Get("Authorization"))
		require.Equal(t, "application/x-www-form-urlencoded", req.Header.Get("Content-Type"))
		form, err := url.ParseQuery(string(readAll(t, req)))
		require.NoError(t, err)
		require.Equal(t, "今天天气不错", form.Get("tex"))
		require.Equal(t, "5003", form.Get("per"))
	})

	t.Run("json error envelope becomes a failover error", func(t *testing.T) {
		upstream := &qianfanActionUpstreamStub{responses: []*http.Response{qianfanActionJSONResponse(http.StatusOK,
			`{"err_no":502,"err_msg":"217: Iam auth error: inner error","err_subcode":217}`)}}
		svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
		c, _ := qianfanActionTestContext(t, http.MethodPost, "/v1/audio/speech")

		_, err := svc.ForwardQianfanAction(context.Background(), c, qianfanActionTestAccount(),
			QianfanEndpointAudioSpeech, "", []byte(`{"input":"hi"}`), "application/json", "")

		var failover *UpstreamFailoverError
		require.ErrorAs(t, err, &failover)
	})
}

func TestForwardQianfanVoiceCloneUsesSpeechHost(t *testing.T) {
	upstream := &qianfanActionUpstreamStub{responses: []*http.Response{qianfanActionJSONResponse(http.StatusOK,
		`{"voice_id":"voice-123"}`)}}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	c, _ := qianfanActionTestContext(t, http.MethodPost, "/v1/voices/clone")

	result, err := svc.ForwardQianfanAction(context.Background(), c, qianfanActionTestAccount(),
		QianfanEndpointVoiceCloneCreate, "", []byte(`{"voice_name":"demo","lang":"zh"}`), "application/json", "")

	require.NoError(t, err)
	require.Equal(t, "voice-123", result.ResponseID)
	req := upstream.lastRequest(t)
	require.Equal(t, "https://aip.baidubce.com/rest/2.0/speech/publiccloudspeech/v1/voice/clone/create", req.URL.String())
	require.Equal(t, "Bearer qianfan-upstream-key", req.Header.Get("Authorization"))
}

func TestQianfanVideoPendingBillingLifecycle(t *testing.T) {
	cache := newQianfanActionCacheStub()
	svc := &OpenAIGatewayService{cfg: &config.Config{}, cache: cache}
	ctx := context.Background()

	require.NoError(t, svc.BindQianfanVideoTaskAccount(ctx, nil, "t1", 7, 9, 21))
	bound, err := svc.ResolveQianfanVideoTaskAccount(ctx, nil, "t1", 7, 9)
	require.NoError(t, err)
	require.EqualValues(t, 21, bound)

	// A different owner/key must not resolve the same task.
	if _, err := svc.ResolveQianfanVideoTaskAccount(ctx, nil, "t1", 8, 9); err == nil {
		require.EqualValues(t, 0, mustResolve(t, svc, ctx, "t1", 8, 9))
	}

	pending := BuildQianfanVideoPendingBilling(QianfanActionRequestInfo{Model: "musesteamer-air-i2v", DurationSeconds: 5}, "musesteamer-air-i2v", "", "")
	require.NoError(t, svc.StoreQianfanVideoPendingBilling(ctx, "t1", 7, 9, pending))
	loaded, err := svc.LoadQianfanVideoPendingBilling(ctx, "t1", 7, 9)
	require.NoError(t, err)
	require.NotNil(t, loaded)
	require.Equal(t, 5, loaded.VideoDurationSeconds)

	claimed, err := svc.ClaimQianfanVideoBilling(ctx, "t1", 7, 9)
	require.NoError(t, err)
	require.True(t, claimed)
	again, err := svc.ClaimQianfanVideoBilling(ctx, "t1", 7, 9)
	require.NoError(t, err)
	require.False(t, again, "a second poll must not double-bill")
	require.NoError(t, svc.ReleaseQianfanVideoBilling(ctx, "t1", 7, 9))
	reclaimed, err := svc.ClaimQianfanVideoBilling(ctx, "t1", 7, 9)
	require.NoError(t, err)
	require.True(t, reclaimed, "a released claim can be retried after a failed record")
}

func TestQianfanVideoTaskSessionHashScoping(t *testing.T) {
	require.Empty(t, QianfanVideoTaskSessionHash("", 1, 2))
	require.Empty(t, QianfanVideoTaskSessionHash("t", 0, 2))
	require.Empty(t, QianfanVideoTaskSessionHash("t", 1, 0))
	require.NotEqual(t, QianfanVideoTaskSessionHash("t", 1, 2), QianfanVideoTaskSessionHash("t", 1, 3))
	require.NotEqual(t, QianfanVideoTaskSessionHash("t", 1, 2), QianfanVideoTaskSessionHash("t", 2, 2))
	require.Equal(t, QianfanVideoTaskSessionHash("t", 1, 2), QianfanVideoTaskSessionHash("t", 1, 2))
	require.Equal(t, "qianfan-video:t", StableQianfanVideoBillingRequestID("t"))
}

func TestMergeQianfanVideoPendingBilling(t *testing.T) {
	pending := &QianfanVideoPendingBilling{
		Model:                "musesteamer-air-i2v",
		BillingModel:         "musesteamer-air-i2v",
		VideoResolution:      VideoBillingResolution720P,
		VideoDurationSeconds: 5,
		CreatedAt:            time.Now().Add(-2 * time.Second).UTC().Format(time.RFC3339Nano),
	}
	status := &OpenAIForwardResult{ResponseID: "t1", VideoCount: 1, Duration: time.Millisecond}
	merged := MergeQianfanVideoPendingBilling(status, pending, "t1")
	require.Equal(t, "musesteamer-air-i2v", merged.Model)
	require.Equal(t, VideoBillingResolution720P, merged.VideoResolution)
	require.Equal(t, 5, merged.VideoDurationSeconds)
	require.Equal(t, "qianfan-video:t1", merged.RequestID)
	require.Greater(t, merged.Duration, time.Second)
}

func mustResolve(t *testing.T, svc *OpenAIGatewayService, ctx context.Context, taskID string, userID, keyID int64) int64 {
	t.Helper()
	id, err := svc.ResolveQianfanVideoTaskAccount(ctx, nil, taskID, userID, keyID)
	require.NoError(t, err)
	return id
}

func readAll(t *testing.T, req *http.Request) []byte {
	t.Helper()
	body, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	return body
}
