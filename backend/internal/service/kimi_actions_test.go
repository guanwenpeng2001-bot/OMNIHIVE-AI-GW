package service

// Kimi 联网搜索动作（/v1/tools/search[/search_pro]）相邻行为测试。
// 证据：platform.kimi.ai/docs/api/tools-search、platform.kimi.com/docs/pricing/tools
// （2026-09-21 实抓，第一方）。覆盖：正例（URL/body/header 达真实上游形状）、
// 反例（官方错误表逐码映射）、取消传播、幂等提交（TrackID 复用）、价格解析。

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	infraerrors "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/errors"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type kimiActionsUpstreamStub struct {
	do       func(req *http.Request) (*http.Response, error)
	requests []*http.Request
	bodies   []string
}

func (u *kimiActionsUpstreamStub) Do(req *http.Request, proxyURL string, accountID int64, accountConcurrency int) (*http.Response, error) {
	u.requests = append(u.requests, req)
	if req.Body != nil {
		body, _ := io.ReadAll(req.Body)
		u.bodies = append(u.bodies, string(body))
	}
	return u.do(req)
}

func (u *kimiActionsUpstreamStub) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, accountConcurrency)
}

func kimiSearchHTTPResponse(status int, body string, headers map[string]string) *http.Response {
	resp := &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{},
	}
	for key, value := range headers {
		resp.Header.Set(key, value)
	}
	return resp
}

func kimiSearchPayGAccount() *Account {
	return &Account{
		ID:       11,
		Platform: PlatformKimi,
		Type:     AccountTypeAPIKey,
		Status:   StatusActive,
		Credentials: map[string]any{
			"account_mode": "payg",
			"api_key":      "sk-kimi-test",
		},
	}
}

func TestKimiActionServiceSearchWebReachesUpstreamShape(t *testing.T) {
	upstream := &kimiActionsUpstreamStub{do: func(req *http.Request) (*http.Response, error) {
		return kimiSearchHTTPResponse(http.StatusOK, `{"search_results":[{"authority":"S","date":"2026-06-01","icon":"https://platform.kimi.ai/favicon.ico","mime":"text/html","site_name":"Kimi API Open Platform","snippet":"Documentation entry.","text":"","title":"API Overview","url":"https://platform.kimi.ai/docs/api/overview"}]}`, map[string]string{
			"X-Msh-Track-Id": "track-search-1",
			"X-Msh-Chat-Id":  "toolgw-track-search-1",
		}), nil
	}}
	service := NewKimiActionService(upstream, &config.Config{})

	result, err := service.SearchWeb(context.Background(), kimiSearchPayGAccount(), KimiWebSearchRequest{
		TextQuery:      "Kimi K2 model release",
		Limit:          5,
		TimeoutSeconds: 10,
		IncludeContent: true,
		TrackID:        "track-search-1",
	})
	require.NoError(t, err)
	require.Len(t, upstream.requests, 1)

	req := upstream.requests[0]
	require.Equal(t, http.MethodPost, req.Method)
	require.Equal(t, "https://api.moonshot.cn/v1/tools/search", req.URL.String())
	require.Equal(t, "Bearer sk-kimi-test", req.Header.Get("Authorization"))
	require.Equal(t, "application/json", req.Header.Get("Content-Type"))
	require.Equal(t, "track-search-1", req.Header.Get("X-Msh-Track-Id"), "客户端 TrackID 必须透传以便上游关联同一请求")
	require.JSONEq(t, `{"text_query":"Kimi K2 model release","limit":5,"timeout_seconds":10,"include_content":true}`, upstream.bodies[0])
	require.Equal(t, HTTPUpstreamProfileOpenAI, HTTPUpstreamProfileFromContext(req.Context()))

	require.True(t, result.BillableCall, "HTTP 200 且 search_results 非空必须计一次调用费")
	require.Equal(t, "track-search-1", result.TrackID)
	require.Equal(t, "toolgw-track-search-1", result.ChatID)
	require.Len(t, result.Results, 1)
	require.Equal(t, "https://platform.kimi.ai/docs/api/overview", result.Results[0].URL)
	require.Equal(t, "API Overview", result.Results[0].Title)
	require.Equal(t, "Documentation entry.", result.Results[0].Snippet)
	require.Equal(t, "2026-06-01", result.Results[0].PageAge)
	require.Contains(t, string(result.RawBody), "search_results")
}

func TestKimiActionServiceSearchWebProEndpointAndParams(t *testing.T) {
	upstream := &kimiActionsUpstreamStub{do: func(req *http.Request) (*http.Response, error) {
		return kimiSearchHTTPResponse(http.StatusOK, `{"search_results":[{"url":"https://example.com/a","title":"A","snippet":"sa","date":"2026-09-01"}]}`, nil), nil
	}}
	service := NewKimiActionService(upstream, &config.Config{})

	account := kimiSearchPayGAccount()
	account.Credentials["base_url"] = "https://relay.example.com/v1"
	result, err := service.SearchWeb(context.Background(), account, KimiWebSearchRequest{
		TextQuery:  "site restricted query",
		Pro:        true,
		Sites:      []string{"example.com"},
		TimeWindow: "week",
	})
	require.NoError(t, err)
	require.True(t, result.BillableCall)
	require.Equal(t, "https://relay.example.com/v1/tools/search_pro", upstream.requests[0].URL.String(),
		"Pro 走 /v1/tools/search_pro，自定义 base_url 与网关转发同口径")
	require.JSONEq(t, `{"text_query":"site restricted query","sites":["example.com"],"time_window":"week"}`, upstream.bodies[0])
}

func TestKimiActionServiceSearchWebEmptyResultsNotBillable(t *testing.T) {
	upstream := &kimiActionsUpstreamStub{do: func(req *http.Request) (*http.Response, error) {
		return kimiSearchHTTPResponse(http.StatusOK, `{"search_results":[]}`, nil), nil
	}}
	service := NewKimiActionService(upstream, &config.Config{})

	result, err := service.SearchWeb(context.Background(), kimiSearchPayGAccount(), KimiWebSearchRequest{TextQuery: "nothing matches this"})
	require.NoError(t, err)
	require.False(t, result.BillableCall, "官方口径：空结果不计费")
	require.Empty(t, result.Results)
}

func TestKimiActionServiceSearchWebRejectsUnverifiedAccountShapes(t *testing.T) {
	upstream := &kimiActionsUpstreamStub{do: func(req *http.Request) (*http.Response, error) {
		return kimiSearchHTTPResponse(http.StatusOK, `{"search_results":[]}`, nil), nil
	}}
	service := NewKimiActionService(upstream, &config.Config{})

	coding := kimiSearchPayGAccount()
	coding.Credentials["account_mode"] = "coding"
	_, err := service.SearchWeb(context.Background(), coding, KimiWebSearchRequest{TextQuery: "q"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "KIMI_SEARCH_UNVERIFIED_ACCOUNT_MODE")

	zhipu := &Account{ID: 12, Platform: PlatformZhipu, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "sk-z"}}
	_, err = service.SearchWeb(context.Background(), zhipu, KimiWebSearchRequest{TextQuery: "q"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "KIMI_SEARCH_INVALID_PLATFORM")

	noKey := kimiSearchPayGAccount()
	delete(noKey.Credentials, "api_key")
	_, err = service.SearchWeb(context.Background(), noKey, KimiWebSearchRequest{TextQuery: "q"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "KIMI_SEARCH_NO_APIKEY")

	_, err = service.SearchWeb(context.Background(), kimiSearchPayGAccount(), KimiWebSearchRequest{TextQuery: "  "})
	require.Error(t, err)
	require.Contains(t, err.Error(), "KIMI_SEARCH_QUERY_REQUIRED")

	require.Empty(t, upstream.requests, "资格/参数拒绝不得发起任何上游请求")
}

func TestKimiActionServiceSearchWebURLPolicyRejects(t *testing.T) {
	upstream := &kimiActionsUpstreamStub{do: func(req *http.Request) (*http.Response, error) {
		return kimiSearchHTTPResponse(http.StatusOK, `{"search_results":[]}`, nil), nil
	}}
	service := NewKimiActionService(upstream, cnProbeAllowlistConfig("api.deepseek.com"))

	_, err := service.SearchWeb(context.Background(), kimiSearchPayGAccount(), KimiWebSearchRequest{TextQuery: "q"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "KIMI_SEARCH_URL_REJECTED")
	require.Empty(t, upstream.requests, "URL 策略拒绝时 API key 不得出站")
}

func TestKimiActionServiceSearchWebMapsUpstreamErrors(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		wantStatus int
		wantReason string
	}{
		{"400 invalid_request", http.StatusBadRequest, `{"error":{"type":"invalid_request","message":"text_query is required"}}`, http.StatusBadRequest, "KIMI_SEARCH_UPSTREAM_INVALID"},
		{"401 no body", http.StatusUnauthorized, ``, http.StatusUnauthorized, "KIMI_SEARCH_UPSTREAM_UNAUTHORIZED"},
		{"403 no body", http.StatusForbidden, ``, http.StatusForbidden, "KIMI_SEARCH_UPSTREAM_FORBIDDEN"},
		{"408 client_canceled", http.StatusRequestTimeout, `{"error":{"type":"client_canceled","message":"client canceled the request"}}`, http.StatusRequestTimeout, "KIMI_SEARCH_CLIENT_CANCELED"},
		{"429 rate_limited", http.StatusTooManyRequests, `{"error":{"type":"rate_limited","message":"project qps limit exceeded"}}`, http.StatusTooManyRequests, "KIMI_SEARCH_RATE_LIMITED"},
		{"500 internal_error", http.StatusInternalServerError, `{"error":{"type":"internal_error","message":"boom"}}`, http.StatusBadGateway, "KIMI_SEARCH_UPSTREAM_FAILED"},
		{"502 upstream_failed", http.StatusBadGateway, `{"error":{"type":"upstream_failed","message":"upstream service failed"}}`, http.StatusBadGateway, "KIMI_SEARCH_UPSTREAM_FAILED"},
		{"504 timeout", http.StatusGatewayTimeout, `{"error":{"type":"timeout","message":"request timeout"}}`, http.StatusBadGateway, "KIMI_SEARCH_UPSTREAM_FAILED"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstream := &kimiActionsUpstreamStub{do: func(req *http.Request) (*http.Response, error) {
				return kimiSearchHTTPResponse(tt.status, tt.body, map[string]string{"X-RateLimit-Remaining": "0"}), nil
			}}
			service := NewKimiActionService(upstream, &config.Config{})

			_, err := service.SearchWeb(context.Background(), kimiSearchPayGAccount(), KimiWebSearchRequest{TextQuery: "q"})
			require.Error(t, err)
			var appErr *infraerrors.ApplicationError
			require.True(t, errors.As(err, &appErr), "upstream failures must surface as ApplicationError, got %T", err)
			require.Equal(t, int32(tt.wantStatus), appErr.Code)
			require.Equal(t, tt.wantReason, appErr.Reason)
			if tt.status == http.StatusTooManyRequests {
				require.Equal(t, "0", appErr.Metadata["X-RateLimit-Remaining"], "429 必须携带限流头元数据")
			}
			if tt.status == http.StatusBadRequest {
				require.Equal(t, "text_query is required", appErr.Message, "上游错误消息必须透传")
			}
		})
	}
}

func TestKimiActionServiceSearchWebRejectsMalformedSuccessBody(t *testing.T) {
	for _, body := range []string{`not json`, `{"unexpected":true}`} {
		upstream := &kimiActionsUpstreamStub{do: func(req *http.Request) (*http.Response, error) {
			return kimiSearchHTTPResponse(http.StatusOK, body, nil), nil
		}}
		service := NewKimiActionService(upstream, &config.Config{})

		_, err := service.SearchWeb(context.Background(), kimiSearchPayGAccount(), KimiWebSearchRequest{TextQuery: "q"})
		require.Error(t, err, "body=%q", body)
		require.Contains(t, err.Error(), "KIMI_SEARCH_RESPONSE_INVALID")
	}
}

func TestKimiActionServiceSearchWebPropagatesCancellation(t *testing.T) {
	upstream := &kimiActionsUpstreamStub{do: func(req *http.Request) (*http.Response, error) {
		return nil, context.Canceled
	}}
	service := NewKimiActionService(upstream, &config.Config{})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := service.SearchWeb(ctx, kimiSearchPayGAccount(), KimiWebSearchRequest{TextQuery: "q"})
	require.ErrorIs(t, err, context.Canceled, "客户端取消必须原样传播，不伪装成上游错误")
}

func TestKimiActionServiceSearchWebIdempotentRetryReusesTrackID(t *testing.T) {
	upstream := &kimiActionsUpstreamStub{do: func(req *http.Request) (*http.Response, error) {
		return kimiSearchHTTPResponse(http.StatusOK, `{"search_results":[{"url":"https://example.com","title":"t","snippet":"s","date":"2026-09-01"}]}`, nil), nil
	}}
	service := NewKimiActionService(upstream, &config.Config{})
	account := kimiSearchPayGAccount()
	request := KimiWebSearchRequest{TextQuery: "retryable", TrackID: "track-retry-42"}

	first, err := service.SearchWeb(context.Background(), account, request)
	require.NoError(t, err)
	second, err := service.SearchWeb(context.Background(), account, request)
	require.NoError(t, err)

	require.Len(t, upstream.requests, 2)
	require.Equal(t, "track-retry-42", upstream.requests[0].Header.Get("X-Msh-Track-Id"))
	require.Equal(t, upstream.requests[0].Header.Get("X-Msh-Track-Id"), upstream.requests[1].Header.Get("X-Msh-Track-Id"),
		"重试必须复用同一 TrackID（幂等提交锚点）")
	require.Equal(t, upstream.bodies[0], upstream.bodies[1], "重试请求体必须逐字节一致")
	require.True(t, first.BillableCall)
	require.True(t, second.BillableCall)
}

func TestKimiSearchBillableCallDrivesWebSearchCost(t *testing.T) {
	billing := NewBillingService(&config.Config{}, nil)

	billable := CalculateWebSearchCostGuard(billing, true, nil, 1)
	require.InDelta(t, 0.01, billable.TotalCost, 1e-12, "默认可计费搜索调用按 defaultWebSearchPricePerCall 计价")

	free := CalculateWebSearchCostGuard(billing, true, float64Ptr(0), 1)
	require.Zero(t, free.TotalCost, "分组显式 0 价免费")

	groupPriced := CalculateWebSearchCostGuard(billing, true, float64Ptr(0.005), 2)
	require.InDelta(t, 0.005, groupPriced.TotalCost, 1e-12)
	require.InDelta(t, 0.01, groupPriced.ActualCost, 1e-12, "费率倍数作用于实际扣费")

	notBillable := CalculateWebSearchCostGuard(billing, false, nil, 1)
	require.Zero(t, notBillable.TotalCost, "空结果/失败不计费（官方口径）")
}

// CalculateWebSearchCostGuard 把适配器 BillableCall 折成调用次数后走既有按次计费，
// 空结果/失败请求折算 0 次（与官方「非空才计费」口径一致）。
func CalculateWebSearchCostGuard(billing *BillingService, billableCall bool, groupPrice *float64, rateMultiplier float64) *CostBreakdown {
	calls := 0
	if billableCall {
		calls = 1
	}
	return billing.CalculateWebSearchCost(calls, groupPrice, rateMultiplier)
}

func TestKimiActionServiceSearchWebHonorsTimeoutSecondsCeiling(t *testing.T) {
	upstream := &kimiActionsUpstreamStub{do: func(req *http.Request) (*http.Response, error) {
		deadline, ok := req.Context().Deadline()
		require.True(t, ok, "无调用方 deadline 时适配器必须自设上限")
		remaining := time.Until(deadline)
		require.Greater(t, remaining, 10*time.Second)
		require.Less(t, remaining, 20*time.Second, "timeout_seconds=10 时客户端上限应≈15s")
		return kimiSearchHTTPResponse(http.StatusOK, `{"search_results":[]}`, nil), nil
	}}
	service := NewKimiActionService(upstream, &config.Config{})

	_, err := service.SearchWeb(context.Background(), kimiSearchPayGAccount(), KimiWebSearchRequest{TextQuery: "q", TimeoutSeconds: 10})
	require.NoError(t, err)
}

// --- Kimi count_tokens 动作（openai.count_tokens 适配，CN 本地估算分支）---
// 既有既定行为：Kimi 的 Anthropic 兼容层无 /v1/messages/count_tokens，一律本地
// tiktoken 估算（openai_gateway_count_tokens.go:272-293）。此处固定该行为：
// 正例返回 200 input_tokens 且零上游请求；反例非法 body 返回 400。

func TestKimiCountTokensEstimatesLocallyWithoutUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte(`{"model":"kimi-k2.6","system":"You are helpful.","messages":[{"role":"user","content":"hello"}]}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	upstream := &httpUpstreamRecorder{}
	svc := &OpenAIGatewayService{httpUpstream: upstream}

	err := svc.ForwardCountTokensAsAnthropic(context.Background(), c, kimiSearchPayGAccount(), body, "")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Empty(t, upstream.requests, "Kimi count_tokens 必须本地估算，任何协议下都不发上游请求")
	require.Positive(t, gjson.GetBytes(rec.Body.Bytes(), "input_tokens").Int())
}

func TestKimiCountTokensRejectsInvalidBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte(`{"messages":[{"role":"user","content":"hello"}]}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	upstream := &httpUpstreamRecorder{}
	svc := &OpenAIGatewayService{httpUpstream: upstream}

	err := svc.ForwardCountTokensAsAnthropic(context.Background(), c, kimiSearchPayGAccount(), body, "")
	require.Error(t, err, "缺 model 的 count_tokens 请求必须 400")
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Empty(t, upstream.requests)
}
