//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func qianwenTestAccount() *Account {
	return &Account{ID: 7, Platform: PlatformQianwen, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "sk-sp-test"}}
}

func TestQianwenProtocolBases(t *testing.T) {
	a := qianwenTestAccount()
	require.Equal(t, APIProtocolAdaptive, a.GetAPIProtocol())
	require.True(t, a.SupportsNativeCNResponses())
	require.Equal(t, DefaultQianwenBaseURL, a.GetOpenAIBaseURL())
	require.Equal(t, DefaultQianwenAnthropicBaseURL, a.GetAnthropicProtocolBaseURL())
	a.Credentials["api_base_urls"] = map[string]any{APIProtocolChatCompletions: "https://relay.example/v1"}
	require.Equal(t, "https://relay.example/v1", a.GetCNProtocolBaseURL(APIProtocolResponses))
	a.Credentials["api_protocol"] = APIProtocolAnthropic
	a.Credentials["base_url"] = "https://relay.example/anthropic"
	require.Equal(t, "https://relay.example/anthropic", a.GetAnthropicProtocolBaseURL())
	require.Equal(t, "https://relay.example/v1", a.GetOpenAIFormatBaseURL())
	delete(a.Credentials, "api_base_urls")
	require.Equal(t, DefaultQianwenBaseURL, a.GetOpenAIFormatBaseURL())
	require.Equal(t, "sk-sp-test", a.GetCNAPIKey())
}

func qianwenModelPage(body string, status int) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestQianwenModelPaginationPreservesCanonicalIDs(t *testing.T) {
	u := &httpUpstreamRecorder{responses: []*http.Response{
		qianwenModelPage(`{"data":[{"id":"MiniMax-M2.5","object":"model"}],"last_id":"model-id-0","has_more":true}`, 200),
		qianwenModelPage(`{"data":[{"id":"Vendor/Model","object":"model"}],"last_id":"model-id-1","has_more":false}`, 200),
	}}
	s := &AccountTestService{httpUpstream: u, cfg: upstreamModelSyncTestConfig()}
	a := qianwenTestAccount()
	a.Credentials["api_protocol"] = APIProtocolAnthropic
	a.Credentials["base_url"] = DefaultQianwenAnthropicBaseURL
	ids, body, err := s.fetchUpstreamModelList(context.Background(), a)
	require.NoError(t, err)
	require.Equal(t, []string{"MiniMax-M2.5", "Vendor/Model"}, ids)
	require.Equal(t, DefaultQianwenDashScopeBaseURL+"/models?page_no=1&page_size=100&service_site=asia-pacific-china", u.requests[0].URL.String())
	require.Equal(t, "model-id-0", u.requests[1].URL.Query().Get("after"))
	require.Equal(t, "Bearer sk-sp-test", u.requests[1].Header.Get("Authorization"))
	require.Equal(t, "Vendor/Model", gjson.GetBytes(body, "data.1.id").String())
	require.False(t, gjson.GetBytes(body, "data.0.context_length").Exists())
}

func TestQianwenModelPickerCachesAllPages(t *testing.T) {
	u := &httpUpstreamRecorder{responses: []*http.Response{
		qianwenModelPage(`{"data":[{"id":"qwen-plus"}],"last_id":"cursor","has_more":true}`, 200),
		qianwenModelPage(`{"data":[{"id":"ZHIPU/GLM-5.3-Flash","created":123,"owned_by":"system"}],"has_more":false}`, 200),
	}}
	s := &OpenAIGatewayService{httpUpstream: u}
	first, err := s.FetchOpenAIModelsList(context.Background(), qianwenTestAccount())
	require.NoError(t, err)
	require.Contains(t, string(first.Body), `"id":"ZHIPU/GLM-5.3-Flash"`)
	require.Contains(t, string(first.Body), `"created":123`)
	second, err := s.FetchOpenAIModelsList(context.Background(), qianwenTestAccount())
	require.NoError(t, err)
	require.Equal(t, first.ETag, second.ETag)
	require.Len(t, u.requests, 2)
}

func TestQianwenPaginationRejectsIncompleteCatalog(t *testing.T) {
	for _, tc := range []struct {
		name, second string
		status       int
	}{
		{"repeated cursor", `{"data":[{"id":"b"}],"last_id":"cursor","has_more":true}`, 200},
		{"missing cursor", `{"data":[{"id":"b"}],"has_more":true}`, 200},
		{"empty page", `{"data":[],"last_id":"next","has_more":true}`, 200},
		{"bad JSON", `{`, 200},
		{"later error", `{"error":"unauthorized"}`, 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u := &httpUpstreamRecorder{responses: []*http.Response{
				qianwenModelPage(`{"data":[{"id":"a"}],"last_id":"cursor","has_more":true}`, 200), qianwenModelPage(tc.second, tc.status),
			}}
			s := &AccountTestService{httpUpstream: u, cfg: upstreamModelSyncTestConfig()}
			ids, err := s.FetchUpstreamSupportedModels(context.Background(), qianwenTestAccount())
			require.Error(t, err)
			require.Nil(t, ids)
			require.Len(t, u.requests, 2)
		})
	}
}

func TestQianwenConnectionTestsInferenceInAllProtocols(t *testing.T) {
	a := adaptiveCNAccountTestAccount(7, PlatformQianwen)
	s, upstream := adaptiveCNAccountTestService(a, adaptiveCNChatTestResponse(), adaptiveCNAnthropicTestResponse(), adaptiveCNResponsesTestResponse())
	c, recorder := newTestContext()
	require.NoError(t, s.TestAccountConnection(c, 7, "", "hello", AccountTestModeDefault))
	require.Len(t, upstream.requests, 3)
	for i, req := range upstream.requests {
		require.Equal(t, http.MethodPost, req.Method)
		require.Equal(t, DefaultQianwenTestModel, gjson.GetBytes(upstream.bodies[i], "model").String())
		require.NotContains(t, req.URL.Path, "/models")
	}
	require.Equal(t, "sk-adaptive-test", upstream.requests[1].Header.Get("x-api-key"))
	require.Equal(t, 1, strings.Count(recorder.Body.String(), `"type":"test_complete"`))
}

func TestQianwenNativeConversationRequest(t *testing.T) {
	s := &OpenAIGatewayService{}
	a := qianwenTestAccount()
	body := []byte(`{"model":"Vendor/Model","input":{"messages":[{"role":"user","content":[{"image":"https://example.com/dog.jpg"},{"text":"Describe"}]}]}}`)
	req, err := s.BuildQianwenNativeRequest(context.Background(), a, QianwenNativeMultimodal, body, true)
	require.NoError(t, err)
	require.Equal(t, DefaultQianwenDashScopeBaseURL+"/services/aigc/multimodal-generation/generation", req.URL.String())
	require.Equal(t, "Bearer sk-sp-test", req.Header.Get("Authorization"))
	require.Equal(t, "enable", req.Header.Get("X-DashScope-SSE"))
	got, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	require.JSONEq(t, string(body), string(got))
	_, err = s.BuildQianwenNativeRequest(context.Background(), a, "video", body, false)
	require.ErrorContains(t, err, "unsupported")
	_, err = s.BuildQianwenNativeRequest(context.Background(), a, QianwenNativeText, []byte(`{}`), false)
	require.ErrorContains(t, err, "model")
	a.Credentials["api_base_urls"] = map[string]any{"dashscope": "https://relay.example/api/v1"}
	req, err = s.BuildQianwenNativeRequest(context.Background(), a, QianwenNativeText, body, false)
	require.NoError(t, err)
	require.Equal(t, "https://relay.example/api/v1/services/aigc/text-generation/generation", req.URL.String())
}

func TestQianwenNativeTransportPreservesUsageAndErrors(t *testing.T) {
	for _, status := range []int{200, 429} {
		body := `{"usage":{"input_tokens":20,"output_tokens":2},"code":"test-code"}`
		u := &httpUpstreamRecorder{resp: qianwenModelPage(body, status)}
		s := &OpenAIGatewayService{httpUpstream: u}
		resp, err := s.DoQianwenNativeRequest(context.Background(), qianwenTestAccount(), QianwenNativeMultimodal, []byte(`{"model":"qwen-vl-plus","input":{"messages":[]}}`), false)
		require.NoError(t, err)
		got, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
		require.Equal(t, status, resp.StatusCode)
		require.JSONEq(t, body, string(got))
	}
}

func TestQianwenSharedModelsRequireExplicitRoutes(t *testing.T) {
	for _, model := range []string{"deepseek-v3", "glm-5", "kimi-k3", "qwen-plus", "MiniMax-M2.5"} {
		r := NewCompositeRouteResolver(nil)
		r.SetModelOwnershipResolver(func(context.Context, int64, string) (CompositeModelOwnership, error) {
			return CompositeModelOwnership{Matched: true, TargetPlatform: PlatformQianwen}, nil
		})
		d, err := r.Resolve(context.Background(), 7, model, CompositeRouteEndpointChatCompletions)
		require.NoError(t, err)
		require.False(t, d.Matched)
		r.repo = compositeRouteRepoStub{routes: []CompositeModelRoute{{ID: 1, GroupID: 7, Enabled: true, PublicModel: model, MatchType: CompositeRouteMatchExact, TargetPlatform: PlatformQianwen, UpstreamModel: model, Endpoint: CompositeRouteEndpointAny}}}
		d, err = r.Resolve(context.Background(), 7, model, CompositeRouteEndpointChatCompletions)
		require.NoError(t, err)
		require.True(t, d.Matched)
		require.Equal(t, PlatformQianwen, d.TargetPlatform)
		require.Equal(t, model, d.UpstreamModel)
	}
	platform, ok := DetectModelPlatform("qianwen/qwen-plus")
	require.True(t, ok)
	require.Equal(t, PlatformQianwen, platform)
}

func TestQianwenMilestoneProfitCapabilities(t *testing.T) {
	for _, platform := range []string{PlatformCursor, PlatformKimi, PlatformZhipu, PlatformDeepseek, PlatformMiniMax, PlatformOpenCodeGo, PlatformQianwen, PlatformCommandCode} {
		require.NoError(t, ValidateProfitControlConfig(platform, true, .2, .1))
		enabled, margin, buffer := NormalizeProfitControlConfig(platform, true, .2, .1)
		require.True(t, enabled)
		require.Equal(t, .2, margin)
		require.Equal(t, .1, buffer)
		g := profitControlTestGroup(7, .2, .1)
		g.Platform = platform
		s := &OpenAIGatewayService{}
		require.NotNil(t, s.resolveOpenAIProfitControlGate(profitControlTestCtx(g), &g.ID))
		ctx := s.withOpenAIProfitControlGate(profitControlTestCtx(g), &g.ID)
		a := &Account{Platform: platform}
		rate := .9
		a.RateMultiplier = &rate
		veto, _ := openAIProfitControlVetoReason(ctx, a)
		require.True(t, veto, platform)
		rate = .5
		veto, _ = openAIProfitControlVetoReason(ctx, a)
		require.False(t, veto, platform)
	}
	require.Error(t, ValidateProfitControlConfig(PlatformComposite, true, .2, .1))
	require.True(t, groupSupportsOAuthOnlyFilter(PlatformCursor))
}

func TestQianwenNativeModelPages(t *testing.T) {
	u := &httpUpstreamRecorder{responses: []*http.Response{
		qianwenModelPage(`{"success":true,"output":{"total":4,"page_no":1,"models":[{"model":"ZHIPU/GLM-5.3-Flash","prices":[{"range_name":"Default"}]},{"model":"wanx-3d"}]}}`, 200),
		qianwenModelPage(`{"success":true,"output":{"total":4,"page_no":2,"models":[{"model":"wanx-3d"},{"model":"qwen-realtime"}]}}`, 200),
	}}
	s := &AccountTestService{httpUpstream: u, cfg: upstreamModelSyncTestConfig()}
	ids, body, err := s.fetchUpstreamModelList(context.Background(), qianwenTestAccount())
	require.NoError(t, err)
	require.Equal(t, []string{"ZHIPU/GLM-5.3-Flash", "qwen-realtime", "wanx-3d"}, ids)
	require.Equal(t, "2", u.requests[1].URL.Query().Get("page_no"))
	require.Equal(t, "100", u.requests[1].URL.Query().Get("page_size"))
	require.Equal(t, "asia-pacific-china", u.requests[1].URL.Query().Get("service_site"))
	require.Empty(t, u.requests[1].URL.Query().Get("after"))
	require.Len(t, gjson.GetBytes(body, "data").Array(), 3)
	require.Equal(t, "Default", gjson.GetBytes(body, "data.0.prices.0.range_name").String())
}

func TestQianwenNativeModelPagesRejectPartial(t *testing.T) {
	for _, body := range []string{
		`{"success":true,"output":{"total":3,"page_no":2,"models":[{"model":"b"}]}}`,
		`{"success":true,"output":{"total":2,"page_no":1,"models":[{"model":"b"}]}}`,
		`{"success":false,"output":{"total":2,"page_no":2,"models":[{"model":"b"}]}}`,
		`{"success":true,"output":{"total":2,"page_no":2,"models":[]}}`,
		`{"success":true,"output":{"total":2,"page_no":2,"models":[{"model":""}]}}`,
	} {
		u := &httpUpstreamRecorder{responses: []*http.Response{
			qianwenModelPage(`{"success":true,"output":{"total":2,"page_no":1,"models":[{"model":"a"}]}}`, 200),
			qianwenModelPage(body, 200),
		}}
		s := &AccountTestService{httpUpstream: u, cfg: upstreamModelSyncTestConfig()}
		ids, _, err := s.fetchUpstreamModelList(context.Background(), qianwenTestAccount())
		require.Error(t, err)
		require.Nil(t, ids)
	}
}
