package service

// Kimi 原生动作适配：联网搜索 REST（POST /v1/tools/search 与 /v1/tools/search_pro）。
//
// 证据（2026-09-21 实抓，第一方）：
//   - platform.kimi.ai/docs/api/tools-search：请求 {text_query 必填, limit 1-20 默认 5,
//     timeout_seconds 1-60, include_content 默认 false}；响应 search_results[]
//     {authority,date,icon,mime,site_name,snippet,text,title,url}；错误表
//     400 invalid_request / 401 / 403（无 body）/ 408 client_canceled /
//     429 rate_limited|rate_limit_unavailable（X-RateLimit-*）/ 500 internal_error /
//     502 upstream_failed / 504 timeout；响应头 X-Msh-Track-Id（客户端发送则回显）、
//     X-Msh-Chat-Id=toolgw-{track}。
//   - platform.kimi.com/docs/pricing/tools：Basic ¥0.01/次、Pro ¥0.015/次；
//     HTTP 200 且 search_results 非空才计费，失败或空结果不计费。
//
// 端点仅文档化于开放平台（payg）；Kimi For Coding 域名无 tools/search 发布记录，
// coding 模式账号在此显式拒绝（保持未核证，不臆造支持）。
// 路由绑定（gateway search 动作 → 本适配）由 multimodal-ingress 统一接线，见派发说明。

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/websearch"
)

const (
	// kimiSearchClientCeiling 是客户端总时限上限：上游 timeout_seconds 最大 60s，加传输余量。
	kimiSearchClientCeiling = 75 * time.Second
	kimiSearchUpstreamSlack = 5 * time.Second
	kimiSearchMaxBodyBytes  = 1 << 20
	kimiSearchTrackIDHeader = "X-Msh-Track-Id"
	kimiSearchChatIDHeader  = "X-Msh-Chat-Id"
	kimiSearchBasicPath     = "/tools/search"
	kimiSearchProPath       = "/tools/search_pro"
)

// KimiWebSearchRequest 是 Kimi 联网搜索的网关侧归一请求。
// TrackID 透传 X-Msh-Track-Id：重试沿用同一值，上游按同一请求关联排障（幂等提交锚点）。
type KimiWebSearchRequest struct {
	TextQuery      string   `json:"text_query"`
	Limit          int      `json:"limit"`
	TimeoutSeconds int      `json:"timeout_seconds"`
	IncludeContent bool     `json:"include_content"`
	Pro            bool     `json:"-"`
	Sites          []string `json:"sites"`
	TimeWindow     string   `json:"time_window"`
	TrackID        string   `json:"-"`
}

// KimiWebSearchResult 携带归一结果、上游原文与计费判据。
// BillableCall 为真时才计一次搜索调用费（官方口径：HTTP 200 且 search_results 非空）。
type KimiWebSearchResult struct {
	Results      []websearch.SearchResult
	RawBody      []byte
	BillableCall bool
	TrackID      string
	ChatID       string
}

// KimiActionService 适配 Kimi 原生动作（当前：联网搜索 REST）。
type KimiActionService struct {
	httpUpstream HTTPUpstream
	cfg          *config.Config
}

func NewKimiActionService(httpUpstream HTTPUpstream, cfg *config.Config) *KimiActionService {
	return &KimiActionService{httpUpstream: httpUpstream, cfg: cfg}
}

// SearchWeb 执行一次 Kimi 联网搜索。同步请求：取消随 ctx 传播（上游 408 client_canceled
// 原样映射），无任务生命周期。返回错误均为 infraerrors.ApplicationError 或 ctx.Err()。
func (s *KimiActionService) SearchWeb(ctx context.Context, account *Account, request KimiWebSearchRequest) (*KimiWebSearchResult, error) {
	if s == nil || s.httpUpstream == nil {
		return nil, infraerrors.New(http.StatusInternalServerError, "KIMI_SEARCH_NOT_CONFIGURED", "kimi action service is not configured")
	}
	if account == nil || account.Platform != PlatformKimi {
		return nil, infraerrors.New(http.StatusBadRequest, "KIMI_SEARCH_INVALID_PLATFORM", "kimi web search requires a kimi account")
	}
	if account.GetAccountMode() == AccountModeCoding {
		return nil, infraerrors.New(http.StatusBadRequest, "KIMI_SEARCH_UNVERIFIED_ACCOUNT_MODE", "kimi for coding has no published tools/search endpoint; cell stays unverified")
	}
	apiKey := strings.TrimSpace(account.GetCNAPIKey())
	if apiKey == "" {
		return nil, infraerrors.New(http.StatusBadRequest, "KIMI_SEARCH_NO_APIKEY", "account api_key is empty")
	}
	query := strings.TrimSpace(request.TextQuery)
	if query == "" {
		return nil, infraerrors.New(http.StatusBadRequest, "KIMI_SEARCH_QUERY_REQUIRED", "text_query is required")
	}

	searchPath := kimiSearchBasicPath
	if request.Pro {
		searchPath = kimiSearchProPath
	}
	targetURL := strings.TrimRight(account.GetOpenAIFormatBaseURL(), "/") + searchPath
	validatedURL, err := cnValidateProbeURL(s.cfg, targetURL)
	if err != nil {
		return nil, infraerrors.New(http.StatusForbidden, "KIMI_SEARCH_URL_REJECTED", err.Error())
	}

	upstreamBody, err := marshalKimiSearchBody(query, request)
	if err != nil {
		return nil, infraerrors.New(http.StatusBadRequest, "KIMI_SEARCH_REQUEST_INVALID", err.Error())
	}

	callCtx := ctx
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		ceiling := kimiSearchClientCeiling
		if request.TimeoutSeconds > 0 && request.TimeoutSeconds <= 60 {
			ceiling = time.Duration(request.TimeoutSeconds)*time.Second + kimiSearchUpstreamSlack
		}
		var cancel context.CancelFunc
		callCtx, cancel = context.WithTimeout(ctx, ceiling)
		defer cancel()
	}

	req, err := http.NewRequestWithContext(callCtx, http.MethodPost, validatedURL, bytes.NewReader(upstreamBody))
	if err != nil {
		return nil, infraerrors.Newf(http.StatusInternalServerError, "KIMI_SEARCH_REQUEST_BUILD_FAILED", "build request: %v", err)
	}
	req = req.WithContext(WithHTTPUpstreamProfile(req.Context(), HTTPUpstreamProfileOpenAI))
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if trackID := strings.TrimSpace(request.TrackID); trackID != "" {
		req.Header.Set(kimiSearchTrackIDHeader, trackID)
	}
	account.ApplyHeaderOverrides(req.Header)

	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	resp, err := generationDo(s.httpUpstream, req, proxyURL, account.ID, maxInt(account.Concurrency, 1))
	if err != nil {
		if IsAttemptAdmissionError(err) {
			return nil, err
		}
		if ctxErr := callCtx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, infraerrors.Newf(http.StatusBadGateway, "KIMI_SEARCH_REQUEST_FAILED", "upstream request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, kimiSearchMaxBodyBytes))

	if resp.StatusCode != http.StatusOK {
		return nil, mapKimiSearchUpstreamError(resp, bodyBytes)
	}
	return parseKimiSearchResponse(resp, bodyBytes)
}

// marshalKimiSearchBody 按官方 schema 组装上游请求体；未设置的可选字段不出现在 body 中。
func marshalKimiSearchBody(query string, request KimiWebSearchRequest) ([]byte, error) {
	body := map[string]any{"text_query": query}
	if request.Limit > 0 {
		body["limit"] = request.Limit
	}
	if request.TimeoutSeconds > 0 {
		body["timeout_seconds"] = request.TimeoutSeconds
	}
	if request.IncludeContent {
		body["include_content"] = true
	}
	if request.Pro {
		if len(request.Sites) > 0 {
			body["sites"] = request.Sites
		}
		if strings.TrimSpace(request.TimeWindow) != "" {
			body["time_window"] = strings.TrimSpace(request.TimeWindow)
		}
	}
	return json.Marshal(body)
}

// parseKimiSearchResponse 解析 200 响应：归一结果到 websearch.SearchResult，
// 并按官方计费口径（search_results 非空）给出 BillableCall。
func parseKimiSearchResponse(resp *http.Response, body []byte) (*KimiWebSearchResult, error) {
	var payload struct {
		SearchResults []struct {
			URL     string `json:"url"`
			Title   string `json:"title"`
			Snippet string `json:"snippet"`
			Date    string `json:"date"`
		} `json:"search_results"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, infraerrors.New(http.StatusBadGateway, "KIMI_SEARCH_RESPONSE_INVALID", "upstream response is not valid JSON")
	}
	if payload.SearchResults == nil {
		return nil, infraerrors.New(http.StatusBadGateway, "KIMI_SEARCH_RESPONSE_INVALID", "upstream response missing search_results")
	}
	result := &KimiWebSearchResult{
		Results:      make([]websearch.SearchResult, 0, len(payload.SearchResults)),
		RawBody:      body,
		BillableCall: len(payload.SearchResults) > 0,
		TrackID:      resp.Header.Get(kimiSearchTrackIDHeader),
		ChatID:       resp.Header.Get(kimiSearchChatIDHeader),
	}
	for _, item := range payload.SearchResults {
		result.Results = append(result.Results, websearch.SearchResult{
			URL:     item.URL,
			Title:   item.Title,
			Snippet: item.Snippet,
			PageAge: item.Date,
		})
	}
	return result, nil
}

// mapKimiSearchUpstreamError 按官方错误表映射上游失败；401/403 无错误 body（官方注明）。
func mapKimiSearchUpstreamError(resp *http.Response, body []byte) error {
	upstreamMessage := strings.TrimSpace(extractUpstreamErrorMessage(body))
	withMessage := func(err *infraerrors.ApplicationError) *infraerrors.ApplicationError {
		if upstreamMessage != "" {
			err.Message = upstreamMessage
		}
		return err
	}
	switch resp.StatusCode {
	case http.StatusBadRequest:
		return withMessage(infraerrors.New(http.StatusBadRequest, "KIMI_SEARCH_UPSTREAM_INVALID", "upstream rejected the search request"))
	case http.StatusUnauthorized:
		return infraerrors.New(http.StatusUnauthorized, "KIMI_SEARCH_UPSTREAM_UNAUTHORIZED", "upstream rejected the account credential")
	case http.StatusForbidden:
		return infraerrors.New(http.StatusForbidden, "KIMI_SEARCH_UPSTREAM_FORBIDDEN", "upstream reports the account inactive or suspended")
	case http.StatusRequestTimeout:
		return withMessage(infraerrors.New(http.StatusRequestTimeout, "KIMI_SEARCH_CLIENT_CANCELED", "client canceled the request"))
	case http.StatusTooManyRequests:
		rateLimited := withMessage(infraerrors.New(http.StatusTooManyRequests, "KIMI_SEARCH_RATE_LIMITED", "upstream rate limit exceeded"))
		metadata := map[string]string{}
		for _, header := range []string{"X-RateLimit-Limit", "X-RateLimit-Remaining", "X-RateLimit-Reset", "Retry-After"} {
			if value := strings.TrimSpace(resp.Header.Get(header)); value != "" {
				metadata[header] = value
			}
		}
		if len(metadata) > 0 {
			rateLimited.Metadata = metadata
		}
		return rateLimited
	case http.StatusInternalServerError, http.StatusBadGateway, http.StatusGatewayTimeout:
		return withMessage(infraerrors.New(http.StatusBadGateway, "KIMI_SEARCH_UPSTREAM_FAILED", "upstream search service failed"))
	default:
		return infraerrors.Newf(http.StatusBadGateway, "KIMI_SEARCH_UPSTREAM_UNEXPECTED", "unexpected upstream status: %d", resp.StatusCode)
	}
}
