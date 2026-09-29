package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// ForwardVolcengineMedia implements the Ark media cells for one account:
// image generation/editing, async video generation/edit/extend, and the video
// task status/content/cancel lookups. Auth is `Authorization: Bearer <api_key>`
// on the account's own Ark origin (service/volcengine_actions.go).
func (s *OpenAIGatewayService) ForwardVolcengineMedia(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	endpoint VolcengineMediaEndpoint,
	taskID string,
	body []byte,
	contentType string,
) (*OpenAIForwardResult, error) {
	startTime := time.Now()
	if account == nil {
		return nil, fmt.Errorf("volcengine account is required")
	}
	if account.Platform != PlatformVolcengine {
		return nil, fmt.Errorf("account platform %s is not supported for volcengine media", account.Platform)
	}
	token, _, err := s.getRequestCredential(ctx, c, account)
	if err != nil {
		return nil, err
	}
	if endpoint == VolcengineMediaEndpointVideoContent {
		return s.forwardVolcengineVideoContent(ctx, c, account, token, taskID, startTime)
	}

	var method, targetPath string
	var upstreamBody []byte
	requestInfo := ParseVolcengineMediaRequest(contentType, body)
	upstreamModel := requestInfo.Model
	switch endpoint {
	case VolcengineMediaEndpointImagesGenerations, VolcengineMediaEndpointImagesEdits:
		method, targetPath = http.MethodPost, "/images/generations"
		if mapped := strings.TrimSpace(account.GetMappedModel(requestInfo.Model)); mapped != "" {
			upstreamModel = mapped
		}
		upstreamBody, err = buildVolcengineImageBody(endpoint, requestInfo, upstreamModel)
	case VolcengineMediaEndpointVideosGenerations, VolcengineMediaEndpointVideoEdit, VolcengineMediaEndpointVideoExtend:
		method, targetPath = http.MethodPost, "/contents/generations/tasks"
		if mapped := strings.TrimSpace(account.GetMappedModel(requestInfo.Model)); mapped != "" {
			upstreamModel = mapped
		}
		upstreamBody, err = buildVolcengineVideoCreateBody(endpoint, requestInfo, upstreamModel)
	case VolcengineMediaEndpointVideoStatus:
		method, targetPath = http.MethodGet, volcengineVideoTaskPath(taskID)
	case VolcengineMediaEndpointVideoCancel:
		method, targetPath = http.MethodDelete, volcengineVideoTaskPath(taskID)
	default:
		return nil, fmt.Errorf("unsupported volcengine media endpoint: %s", endpoint)
	}
	if err != nil {
		writeVolcengineActionErrorResponse(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return nil, err
	}
	targetURL, err := s.volcengineValidatedActionURL(account, targetPath)
	if err != nil {
		writeVolcengineActionErrorResponse(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return nil, err
	}

	var bodyReader io.Reader
	if upstreamBody != nil {
		bodyReader = bytes.NewReader(upstreamBody)
	}
	upstreamCtx, releaseUpstreamCtx := detachUpstreamContext(ctx)
	defer releaseUpstreamCtx()
	upstreamReq, err := http.NewRequestWithContext(upstreamCtx, method, targetURL, bodyReader)
	if err != nil {
		return nil, err
	}
	upstreamReq.Header.Set("Authorization", "Bearer "+token)
	upstreamReq.Header.Set("Accept", "application/json")
	if upstreamBody != nil {
		upstreamReq.Header.Set("Content-Type", "application/json")
	}
	account.ApplyHeaderOverrides(upstreamReq.Header)

	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	upstreamStart := time.Now()
	resp, err := generationDo(s.httpUpstream, upstreamReq, proxyURL, account.ID, account.Concurrency)
	SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(upstreamStart).Milliseconds())
	if err != nil {
		return nil, s.handleOpenAIUpstreamTransportError(ctx, c, account, err, false)
	}
	defer func() { _ = resp.Body.Close() }()

	requestIDHeader := firstNonEmpty(resp.Header.Get("x-request-id"), resp.Header.Get("x-tt-logid"))
	if resp.StatusCode >= 400 {
		return s.handleVolcengineActionErrorResponse(ctx, resp, c, account, requestIDHeader)
	}
	respBody, err := ReadUpstreamResponseBody(resp.Body, s.cfg, c, openAITooLargeError)
	if err != nil {
		return nil, err
	}

	switch endpoint {
	case VolcengineMediaEndpointImagesGenerations, VolcengineMediaEndpointImagesEdits:
		bridged, imageCount, bridgeErr := bridgeVolcengineImageResponse(respBody)
		if bridgeErr != nil {
			var businessErr *VolcengineBusinessError
			if errors.As(bridgeErr, &businessErr) {
				setOpsUpstreamError(c, http.StatusBadGateway, businessErr.Message, truncateString(string(respBody), 512))
			} else {
				setOpsUpstreamError(c, http.StatusBadGateway, "Volcengine upstream returned no usable image output", truncateString(string(respBody), 512))
			}
			return nil, &UpstreamFailoverError{StatusCode: http.StatusBadGateway, ResponseBody: respBody, ResponseHeaders: resp.Header.Clone()}
		}
		writeVolcengineActionJSONResponse(c, http.StatusOK, bridged)
		return &OpenAIForwardResult{
			RequestID:      requestIDHeader,
			Model:          requestInfo.Model,
			BillingModel:   requestInfo.Model,
			UpstreamModel:  upstreamModel,
			Duration:       time.Since(startTime),
			ImageCount:     imageCount,
			ImageSize:      requestInfo.SizeTier,
			ImageInputSize: requestInfo.Size,
		}, nil
	case VolcengineMediaEndpointVideosGenerations, VolcengineMediaEndpointVideoEdit, VolcengineMediaEndpointVideoExtend:
		createdTaskID := strings.TrimSpace(gjson.GetBytes(respBody, "id").String())
		if createdTaskID == "" {
			setOpsUpstreamError(c, http.StatusBadGateway, "Volcengine video create returned no task id", truncateString(string(respBody), 512))
			return nil, &UpstreamFailoverError{StatusCode: http.StatusBadGateway, ResponseBody: respBody, ResponseHeaders: resp.Header.Clone()}
		}
		writeVolcengineActionJSONResponse(c, http.StatusOK, respBody)
		createResolution, _ := volcengineVideoResolutionUpper(requestInfo.Resolution)
		return &OpenAIForwardResult{
			RequestID:            requestIDHeader,
			ResponseID:           createdTaskID,
			Model:                requestInfo.Model,
			BillingModel:         requestInfo.Model,
			UpstreamModel:        upstreamModel,
			Duration:             time.Since(startTime),
			VideoResolution:      volcengineVideoBillingResolution(createResolution),
			VideoDurationSeconds: requestInfo.DurationSeconds,
		}, nil
	case VolcengineMediaEndpointVideoStatus:
		respBody = rewriteVolcengineVideoContentURL(respBody, volcengineVideoContentProxyURL(c, taskID))
		writeVolcengineActionJSONResponse(c, http.StatusOK, respBody)
		result := &OpenAIForwardResult{
			RequestID: requestIDHeader,
			Duration:  time.Since(startTime),
		}
		if billed := ExtractVolcengineVideoBillingFromStatusBody(respBody, nil, taskID); billed != nil {
			result.ResponseID = billed.ResponseID
			result.Model = billed.Model
			result.BillingModel = billed.BillingModel
			result.UpstreamModel = billed.UpstreamModel
			result.VideoCount = billed.VideoCount
			result.VideoResolution = billed.VideoResolution
			result.VideoDurationSeconds = billed.VideoDurationSeconds
		}
		return result, nil
	case VolcengineMediaEndpointVideoCancel:
		writeVolcengineActionJSONResponse(c, http.StatusOK, respBody)
		return &OpenAIForwardResult{
			RequestID:  requestIDHeader,
			ResponseID: strings.TrimSpace(taskID),
			Duration:   time.Since(startTime),
		}, nil
	}
	return nil, fmt.Errorf("unsupported volcengine media endpoint: %s", endpoint)
}

// forwardVolcengineVideoContent streams the CDN video bytes for a succeeded
// task: status lookup -> content.video_url -> validated https download with
// Range passthrough.
func (s *OpenAIGatewayService) forwardVolcengineVideoContent(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	token, taskID string,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	statusURL, err := s.volcengineValidatedActionURL(account, volcengineVideoTaskPath(taskID))
	if err != nil {
		return nil, err
	}
	upstreamCtx, releaseUpstreamCtx := detachUpstreamContext(ctx)
	defer releaseUpstreamCtx()
	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}

	statusReq, err := http.NewRequestWithContext(WithHTTPUpstreamRedirectsDisabled(upstreamCtx), http.MethodGet, statusURL, nil)
	if err != nil {
		return nil, err
	}
	statusReq.Header.Set("Authorization", "Bearer "+token)
	statusReq.Header.Set("Accept", "application/json")
	account.ApplyHeaderOverrides(statusReq.Header)
	upstreamStart := time.Now()
	statusResp, err := generationDo(s.httpUpstream, statusReq, proxyURL, account.ID, account.Concurrency)
	if err != nil {
		SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(upstreamStart).Milliseconds())
		return nil, s.handleOpenAIUpstreamTransportError(ctx, c, account, err, false)
	}
	statusRequestID := statusResp.Header.Get("x-request-id")
	if statusResp.StatusCode >= 300 {
		defer func() { _ = statusResp.Body.Close() }()
		SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(upstreamStart).Milliseconds())
		if statusResp.StatusCode < 400 {
			return nil, fmt.Errorf("volcengine video status redirect is not allowed")
		}
		return s.handleVolcengineActionErrorResponse(ctx, statusResp, c, account, statusRequestID)
	}
	statusBody, err := ReadUpstreamResponseBody(statusResp.Body, s.cfg, c, openAITooLargeError)
	_ = statusResp.Body.Close()
	if err != nil {
		SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(upstreamStart).Milliseconds())
		return nil, err
	}

	contentURL, err := volcengineVideoSignedContentURL(statusBody)
	if err != nil {
		SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(upstreamStart).Milliseconds())
		return nil, err
	}
	if contentURL == "" {
		SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(upstreamStart).Milliseconds())
		MarkResponseCommitted(c)
		writeVolcengineActionErrorResponse(c, http.StatusConflict, "invalid_request_error", "Video content is not available yet")
		return nil, fmt.Errorf("volcengine video task %s has no content url", strings.TrimSpace(taskID))
	}

	contentReq, err := http.NewRequestWithContext(WithHTTPUpstreamRedirectsDisabled(upstreamCtx), http.MethodGet, contentURL, nil)
	if err != nil {
		SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(upstreamStart).Milliseconds())
		return nil, err
	}
	contentReq.Header.Set("Accept", "*/*")
	if c != nil {
		if rangeHeader := strings.TrimSpace(c.GetHeader("Range")); rangeHeader != "" {
			contentReq.Header.Set("Range", rangeHeader)
		}
	}
	contentResp, err := generationDo(s.httpUpstream, contentReq, proxyURL, account.ID, account.Concurrency)
	SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(upstreamStart).Milliseconds())
	if err != nil {
		return nil, s.handleOpenAIUpstreamTransportError(ctx, c, account, err, false)
	}
	defer func() { _ = contentResp.Body.Close() }()
	if contentResp.StatusCode >= 300 && contentResp.StatusCode < 400 {
		return nil, fmt.Errorf("volcengine video content redirect is not allowed")
	}
	if contentResp.StatusCode >= 400 && contentResp.StatusCode != http.StatusRequestedRangeNotSatisfiable {
		return s.handleVolcengineActionErrorResponse(ctx, contentResp, c, account, firstNonEmpty(contentResp.Header.Get("x-request-id"), statusRequestID))
	}
	if err := writeGrokMediaContentResponse(c, contentResp); err != nil {
		return nil, err
	}
	result := &OpenAIForwardResult{
		RequestID: firstNonEmpty(contentResp.Header.Get("x-request-id"), statusRequestID),
		Duration:  time.Since(startTime),
	}
	if billed := ExtractVolcengineVideoBillingFromStatusBody(statusBody, nil, taskID); billed != nil {
		result.ResponseID = billed.ResponseID
		result.Model = billed.Model
		result.BillingModel = billed.BillingModel
		result.UpstreamModel = billed.UpstreamModel
		result.VideoCount = billed.VideoCount
		result.VideoResolution = billed.VideoResolution
		result.VideoDurationSeconds = billed.VideoDurationSeconds
	}
	return result, nil
}

// volcengineEmbeddingsPath selects the Ark vectorization endpoint by input
// shape: a text input list uses /embeddings (文本向量化 API, docs/82379/1521765),
// while text/image_url/video_url input objects use /embeddings/multimodal
// (多模态向量化 API, docs/82379/1409290). Both share the account's Bearer key.
func volcengineEmbeddingsPath(body []byte) string {
	for _, item := range gjson.GetBytes(body, "input").Array() {
		if item.IsObject() {
			return "/embeddings/multimodal"
		}
	}
	return "/embeddings"
}

// ForwardVolcengineEmbeddings forwards the inbound OpenAI-shaped embeddings
// request to Ark (model doubao-embedding-* / doubao-embedding-vision-*) and
// returns the upstream body with token usage for billing. The model is rewritten
// to the resolved account target so Endpoint IDs and aliases are honored like
// the chat path.
func (s *OpenAIGatewayService) ForwardVolcengineEmbeddings(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	contentType string,
) (*OpenAIForwardResult, error) {
	startTime := time.Now()
	if account == nil {
		return nil, fmt.Errorf("volcengine account is required")
	}
	if account.Platform != PlatformVolcengine {
		return nil, fmt.Errorf("account platform %s is not supported for volcengine embeddings", account.Platform)
	}
	if !gjson.ValidBytes(body) || !gjson.ParseBytes(body).IsObject() {
		err := fmt.Errorf("volcengine embeddings body must be a JSON object")
		writeVolcengineActionErrorResponse(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return nil, err
	}
	requestModel := strings.TrimSpace(gjson.GetBytes(body, "model").String())
	upstreamModel := requestModel
	if mapped := strings.TrimSpace(account.GetMappedModel(requestModel)); mapped != "" {
		upstreamModel = mapped
	}
	upstreamBody := body
	if upstreamModel != requestModel {
		rewritten, err := sjson.SetBytes(body, "model", upstreamModel)
		if err != nil {
			writeVolcengineActionErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "failed to resolve embeddings model target")
			return nil, err
		}
		upstreamBody = rewritten
	}
	token, _, err := s.getRequestCredential(ctx, c, account)
	if err != nil {
		return nil, err
	}
	targetURL, err := s.volcengineValidatedActionURL(account, volcengineEmbeddingsPath(body))
	if err != nil {
		writeVolcengineActionErrorResponse(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return nil, err
	}
	upstreamCtx, releaseUpstreamCtx := detachUpstreamContext(ctx)
	defer releaseUpstreamCtx()
	upstreamReq, err := http.NewRequestWithContext(upstreamCtx, http.MethodPost, targetURL, bytes.NewReader(upstreamBody))
	if err != nil {
		return nil, err
	}
	upstreamReq.Header.Set("Authorization", "Bearer "+token)
	upstreamReq.Header.Set("Accept", "application/json")
	upstreamReq.Header.Set("Content-Type", "application/json")
	account.ApplyHeaderOverrides(upstreamReq.Header)

	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	upstreamStart := time.Now()
	resp, err := generationDo(s.httpUpstream, upstreamReq, proxyURL, account.ID, account.Concurrency)
	SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(upstreamStart).Milliseconds())
	if err != nil {
		return nil, s.handleOpenAIUpstreamTransportError(ctx, c, account, err, false)
	}
	defer func() { _ = resp.Body.Close() }()
	requestIDHeader := firstNonEmpty(resp.Header.Get("x-request-id"), resp.Header.Get("x-tt-logid"))
	if resp.StatusCode >= 400 {
		return s.handleVolcengineActionErrorResponse(ctx, resp, c, account, requestIDHeader)
	}
	respBody, err := ReadUpstreamResponseBody(resp.Body, s.cfg, c, openAITooLargeError)
	if err != nil {
		return nil, err
	}
	data := gjson.GetBytes(respBody, "data")
	if !data.Exists() || !data.IsArray() || len(data.Array()) == 0 {
		setOpsUpstreamError(c, http.StatusBadGateway, "Volcengine embeddings returned no vectors", truncateString(string(respBody), 512))
		return nil, &UpstreamFailoverError{StatusCode: http.StatusBadGateway, ResponseBody: respBody, ResponseHeaders: resp.Header.Clone()}
	}
	writeVolcengineActionJSONResponse(c, http.StatusOK, respBody)
	usage := OpenAIUsage{
		InputTokens:  int(gjson.GetBytes(respBody, "usage.prompt_tokens").Int()),
		OutputTokens: int(gjson.GetBytes(respBody, "usage.total_tokens").Int()),
	}
	return &OpenAIForwardResult{
		RequestID:     requestIDHeader,
		Model:         firstNonEmpty(requestModel, upstreamModel),
		BillingModel:  firstNonEmpty(requestModel, upstreamModel),
		UpstreamModel: upstreamModel,
		Duration:      time.Since(startTime),
		Usage:         usage,
	}, nil
}
