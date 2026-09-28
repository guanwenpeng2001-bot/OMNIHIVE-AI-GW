package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Volcengine Ark native media/embedding adapter.
//
// Evidence base (captured offline, see the actions-volcengine report):
//   - Ark official model list page, updated 2026.09.17, captured 2026-09-20.
//     It enumerates the official API references and per-model abilities:
//   - Video Generation API      docs/82379/1520757  (doubao-seedance-*:
//     文生视频 / 首帧 / 首尾帧 / 参考生视频 / 编辑视频 / 延长视频)
//   - Image Generation API      docs/82379/1541523  (doubao-seedream-*:
//     文生图 / 单张图生图 / 多参考图生图 / 组图生成 / 图层拆分)
//   - Embeddings Multimodal API docs/82379/1409290 (doubao-embedding-vision-*,
//     POST {root}/embeddings/multimodal) and the text vectorization API
//     docs/82379/1521765 (POST {root}/embeddings).
//
// Frozen runtime contract (service/volcengine_adapter.go): base
// https://ark.cn-beijing.volces.com/api/v3, Authorization: Bearer <ARK api_key>,
// account target is an Endpoint ID (ep-*) or a model ID. Region and product
// variants are expressed by the account base URL, never by a credential guess.
// Evidence captured 2026-09-21.

type VolcengineMediaEndpoint string

const (
	VolcengineMediaEndpointImagesGenerations VolcengineMediaEndpoint = "images_generations"
	VolcengineMediaEndpointImagesEdits       VolcengineMediaEndpoint = "images_edits"
	VolcengineMediaEndpointEmbeddings        VolcengineMediaEndpoint = "embeddings"
	VolcengineMediaEndpointVideosGenerations VolcengineMediaEndpoint = "videos_generations"
	VolcengineMediaEndpointVideoEdit         VolcengineMediaEndpoint = "video_edit"
	VolcengineMediaEndpointVideoExtend       VolcengineMediaEndpoint = "video_extend"
	VolcengineMediaEndpointVideoStatus       VolcengineMediaEndpoint = "video_status"
	VolcengineMediaEndpointVideoContent      VolcengineMediaEndpoint = "video_content"
	VolcengineMediaEndpointVideoCancel       VolcengineMediaEndpoint = "video_cancel"
)

func (e VolcengineMediaEndpoint) RequiresRequestBody() bool {
	switch e {
	case VolcengineMediaEndpointImagesGenerations, VolcengineMediaEndpointImagesEdits,
		VolcengineMediaEndpointVideosGenerations, VolcengineMediaEndpointVideoEdit,
		VolcengineMediaEndpointVideoExtend, VolcengineMediaEndpointEmbeddings:
		return true
	default:
		return false
	}
}

// IsVideoCreateRequest reports the three async creation cells that share the Ark
// /contents/generations/tasks endpoint.
func (e VolcengineMediaEndpoint) IsVideoCreateRequest() bool {
	return e == VolcengineMediaEndpointVideosGenerations || e == VolcengineMediaEndpointVideoEdit || e == VolcengineMediaEndpointVideoExtend
}

func (e VolcengineMediaEndpoint) IsVideoLookupRequest() bool {
	return e == VolcengineMediaEndpointVideoStatus || e == VolcengineMediaEndpointVideoContent || e == VolcengineMediaEndpointVideoCancel
}

func (e VolcengineMediaEndpoint) IsGenerationRequest() bool {
	switch e {
	case VolcengineMediaEndpointImagesGenerations, VolcengineMediaEndpointImagesEdits,
		VolcengineMediaEndpointVideosGenerations, VolcengineMediaEndpointVideoEdit,
		VolcengineMediaEndpointVideoExtend:
		return true
	default:
		return false
	}
}

// volcengineAPIRoot derives the Ark data-plane origin from the account chat base
// URL. Ark keeps its version segment inside the path (/api/v3), so unlike an
// OpenAI-style base the trailing version is preserved; only a full chat endpoint
// suffix is trimmed. Region and product (e.g. a coding-plan base) therefore flow
// through unchanged instead of being rewritten.
func volcengineAPIRoot(account *Account) string {
	base := volcengineRuntimeBaseURL
	if account != nil {
		if configured := strings.TrimSpace(account.GetOpenAIBaseURL()); configured != "" {
			base = configured
		}
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		parsed, _ = url.Parse(volcengineRuntimeBaseURL)
	}
	path := strings.TrimRight(parsed.Path, "/")
	path = strings.TrimSuffix(path, "/chat/completions")
	parsed.Path = strings.TrimRight(path, "/")
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return strings.TrimRight(parsed.String(), "/")
}

//nolint:unused // referenced only from //go:build unit tests, so the default lint pass cannot see its callers.
func buildVolcengineActionURL(account *Account, path string) string {
	return volcengineAPIRoot(account) + "/" + strings.TrimLeft(path, "/")
}

// volcengineValidatedActionURL validates the resolved Ark origin through the same
// URL allowlist the chat contract uses, so media/embedding requests cannot reach
// a disallowed host even though they share the account base URL.
func (s *OpenAIGatewayService) volcengineValidatedActionURL(account *Account, path string) (string, error) {
	root := volcengineAPIRoot(account)
	validated, err := s.validateUpstreamBaseURL(root)
	if err != nil {
		return "", fmt.Errorf("volcengine runtime base URL is invalid or disallowed")
	}
	return strings.TrimRight(validated, "/") + "/" + strings.TrimLeft(path, "/"), nil
}

func volcengineVideoTaskPath(taskID string) string {
	return "/contents/generations/tasks/" + url.PathEscape(strings.TrimSpace(taskID))
}

// ---------------------------------------------------------------------------
// Request parsing (OpenAI / grok-style inbound shapes)
// ---------------------------------------------------------------------------

type VolcengineMediaRequestInfo struct {
	Model           string
	Prompt          string
	Instruction     string
	N               int
	Size            string
	SizeTier        string
	AspectRatio     string
	Resolution      string
	DurationSeconds int
	Watermark       *bool
	Seed            *int64
	ResponseFormat  string
	InputImageURLs  []string
	InputVideoURLs  []string
	Uploads         []OpenAIImagesUpload
	MaskUpload      *OpenAIImagesUpload
}

func (r VolcengineMediaRequestInfo) HasInputImage() bool {
	return len(r.InputImageURLs) > 0 || len(r.Uploads) > 0
}

// ModerationBody renders the prompt/reference media for the shared security audit.
func (r VolcengineMediaRequestInfo) ModerationBody() []byte {
	payload := map[string]any{}
	if prompt := strings.TrimSpace(r.Prompt); prompt != "" {
		payload["prompt"] = prompt
	}
	images := make([]map[string]string, 0, len(r.InputImageURLs)+len(r.Uploads))
	for _, imageURL := range r.InputImageURLs {
		if imageURL = strings.TrimSpace(imageURL); imageURL != "" {
			images = append(images, map[string]string{"image_url": imageURL})
		}
	}
	for _, upload := range r.Uploads {
		if dataURL := upload.ModerationDataURL(); dataURL != "" {
			images = append(images, map[string]string{"image_url": dataURL})
		}
	}
	if len(images) > 0 {
		payload["images"] = images
	}
	if len(payload) == 0 {
		return nil
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil
	}
	return body
}

func ParseVolcengineMediaRequest(contentType string, body []byte) VolcengineMediaRequestInfo {
	info := VolcengineMediaRequestInfo{N: 1}
	if gjson.ValidBytes(body) {
		parseVolcengineMediaJSONRequest(body, &info)
	} else {
		parseVolcengineMediaMultipartRequest(contentType, body, &info)
	}
	info.Model = strings.TrimSpace(info.Model)
	info.Prompt = strings.TrimSpace(info.Prompt)
	info.Instruction = strings.TrimSpace(info.Instruction)
	info.Size = strings.TrimSpace(info.Size)
	info.SizeTier = NormalizeImageBillingTierOrDefault(info.Size)
	info.AspectRatio = strings.TrimSpace(info.AspectRatio)
	info.Resolution = strings.TrimSpace(info.Resolution)
	info.ResponseFormat = strings.TrimSpace(info.ResponseFormat)
	if info.N <= 0 {
		info.N = 1
	}
	return info
}

func parseVolcengineMediaJSONRequest(body []byte, info *VolcengineMediaRequestInfo) {
	if info == nil {
		return
	}
	info.Model = strings.TrimSpace(gjson.GetBytes(body, "model").String())
	info.Prompt = strings.TrimSpace(gjson.GetBytes(body, "prompt").String())
	info.Instruction = strings.TrimSpace(firstNonEmpty(
		gjson.GetBytes(body, "instruction").String(),
		gjson.GetBytes(body, "text").String(),
	))
	info.Size = strings.TrimSpace(gjson.GetBytes(body, "size").String())
	info.AspectRatio = strings.TrimSpace(firstNonEmpty(
		gjson.GetBytes(body, "aspect_ratio").String(),
		gjson.GetBytes(body, "ratio").String(),
	))
	info.Resolution = strings.TrimSpace(gjson.GetBytes(body, "resolution").String())
	info.ResponseFormat = strings.TrimSpace(gjson.GetBytes(body, "response_format").String())
	if duration := gjson.GetBytes(body, "duration"); duration.Exists() && duration.Type == gjson.Number {
		info.DurationSeconds = int(duration.Int())
	}
	if durationSeconds := gjson.GetBytes(body, "duration_seconds"); durationSeconds.Exists() && durationSeconds.Type == gjson.Number {
		info.DurationSeconds = int(durationSeconds.Int())
	}
	if n := gjson.GetBytes(body, "n"); n.Exists() && n.Type == gjson.Number {
		info.N = int(n.Int())
	}
	if seed := gjson.GetBytes(body, "seed"); seed.Exists() && seed.Type == gjson.Number {
		seedValue := seed.Int()
		info.Seed = &seedValue
	}
	if watermark := gjson.GetBytes(body, "watermark"); watermark.Exists() && (watermark.Type == gjson.True || watermark.Type == gjson.False) {
		value := watermark.Bool()
		info.Watermark = &value
	}
	appendVolcengineMediaRef(gjson.GetBytes(body, "image"), &info.InputImageURLs, false)
	appendVolcengineMediaRef(gjson.GetBytes(body, "images"), &info.InputImageURLs, false)
	appendVolcengineMediaRef(gjson.GetBytes(body, "video"), &info.InputVideoURLs, true)
	appendVolcengineMediaRef(gjson.GetBytes(body, "videos"), &info.InputVideoURLs, true)
}

func appendVolcengineMediaRef(value gjson.Result, target *[]string, video bool) {
	if !value.Exists() || target == nil {
		return
	}
	if value.IsArray() {
		for _, item := range value.Array() {
			appendVolcengineMediaRef(item, target, video)
		}
		return
	}
	if ref := extractVolcengineMediaRefURL(value); ref != "" {
		*target = append(*target, ref)
	}
}

func extractVolcengineMediaRefURL(value gjson.Result) string {
	if !value.Exists() {
		return ""
	}
	if value.Type == gjson.String {
		return strings.TrimSpace(value.String())
	}
	if raw := strings.TrimSpace(value.Get("url").String()); raw != "" {
		return raw
	}
	for _, field := range []string{"image_url", "video_url"} {
		nested := value.Get(field)
		if !nested.Exists() {
			continue
		}
		if nested.Type == gjson.String {
			return strings.TrimSpace(nested.String())
		}
		return strings.TrimSpace(nested.Get("url").String())
	}
	return ""
}

func parseVolcengineMediaMultipartRequest(contentType string, body []byte, info *VolcengineMediaRequestInfo) {
	if info == nil {
		return
	}
	mediaType, params, err := mime.ParseMediaType(strings.TrimSpace(contentType))
	if err != nil || !strings.EqualFold(mediaType, "multipart/form-data") {
		return
	}
	boundary := strings.TrimSpace(params["boundary"])
	if boundary == "" {
		return
	}
	reader := multipart.NewReader(bytes.NewReader(body), boundary)
	for {
		part, err := reader.NextPart()
		if err != nil {
			return
		}
		name := strings.TrimSpace(part.FormName())
		if name == "" {
			_ = part.Close()
			continue
		}
		data, err := io.ReadAll(io.LimitReader(part, openAIImageMaxUploadPartSize))
		_ = part.Close()
		if err != nil {
			return
		}
		if fileName := strings.TrimSpace(part.FileName()); fileName != "" {
			upload := OpenAIImagesUpload{
				FieldName:   name,
				FileName:    fileName,
				ContentType: strings.TrimSpace(part.Header.Get("Content-Type")),
				Data:        data,
			}
			if name == "mask" {
				info.MaskUpload = &upload
				continue
			}
			if name == "image" || strings.HasPrefix(name, "image[") {
				info.Uploads = append(info.Uploads, upload)
			}
			continue
		}
		value := strings.TrimSpace(string(data))
		switch name {
		case "model":
			info.Model = value
		case "prompt":
			info.Prompt = value
		case "instruction", "text":
			info.Instruction = value
		case "size":
			info.Size = value
		case "aspect_ratio", "ratio":
			info.AspectRatio = value
		case "resolution":
			info.Resolution = value
		case "response_format":
			info.ResponseFormat = value
		case "duration", "duration_seconds":
			if duration, convErr := strconv.Atoi(value); convErr == nil {
				info.DurationSeconds = duration
			}
		case "n":
			if n, convErr := strconv.Atoi(value); convErr == nil {
				info.N = n
			}
		case "seed":
			if seed, convErr := strconv.ParseInt(value, 10, 64); convErr == nil {
				info.Seed = &seed
			}
		case "watermark":
			if watermark, convErr := strconv.ParseBool(value); convErr == nil {
				info.Watermark = &watermark
			}
		case "image", "image_url":
			if value != "" {
				info.InputImageURLs = append(info.InputImageURLs, value)
			}
		case "video", "video_url":
			if value != "" {
				info.InputVideoURLs = append(info.InputVideoURLs, value)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Ark error envelope and failover policy
// ---------------------------------------------------------------------------

// Ark returns OpenAI-shaped errors with a real HTTP status
// ({"error":{"code","message","param","type"}}); a small number of legacy
// endpoints answer HTTP 200 with a top-level code + message.
type VolcengineBusinessError struct {
	Code    string
	Message string
}

func (e *VolcengineBusinessError) Error() string {
	return fmt.Sprintf("volcengine upstream error %s: %s", e.Code, e.Message)
}

func extractVolcengineBusinessError(respBody []byte) *VolcengineBusinessError {
	if len(respBody) == 0 || !gjson.ValidBytes(respBody) {
		return nil
	}
	if errorNode := gjson.GetBytes(respBody, "error"); errorNode.Exists() {
		code := strings.TrimSpace(errorNode.Get("code").String())
		message := strings.TrimSpace(errorNode.Get("message").String())
		if code == "" && message == "" {
			return nil
		}
		return &VolcengineBusinessError{Code: code, Message: message}
	}
	return nil
}

// volcengineHTTPStatusFailoverable reports whether another account may
// legitimately succeed: account-scoped auth/quota failures and transient limits
// move to the next account; request-scoped validation failures do not.
func volcengineHTTPStatusFailoverable(statusCode int) bool {
	switch {
	case statusCode == http.StatusUnauthorized,
		statusCode == http.StatusPaymentRequired,
		statusCode == http.StatusTooManyRequests,
		statusCode == http.StatusRequestTimeout,
		statusCode >= 500:
		return true
	default:
		return false
	}
}

func volcengineActionErrorType(statusCode int) string {
	switch statusCode {
	case http.StatusBadRequest:
		return "invalid_request_error"
	case http.StatusNotFound:
		return "not_found_error"
	case http.StatusTooManyRequests:
		return "rate_limit_error"
	case http.StatusUnauthorized:
		return "authentication_error"
	case http.StatusForbidden:
		return "permission_error"
	case http.StatusPaymentRequired:
		return "billing_error"
	default:
		return "upstream_error"
	}
}

// handleVolcengineActionErrorResponse processes HTTP-level Ark failures: ops
// event, configurable passthrough, failover classification.
func (s *OpenAIGatewayService) handleVolcengineActionErrorResponse(
	ctx context.Context,
	resp *http.Response,
	c *gin.Context,
	account *Account,
	requestIDHeader string,
) (*OpenAIForwardResult, error) {
	body := s.readUpstreamErrorBody(resp)
	upstreamMsg := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(body)))
	if businessErr := extractVolcengineBusinessError(body); businessErr != nil && strings.TrimSpace(businessErr.Message) != "" {
		upstreamMsg = sanitizeUpstreamErrorMessage(businessErr.Message)
	}
	if upstreamMsg == "" {
		upstreamMsg = fmt.Sprintf("Volcengine upstream returned status %d", resp.StatusCode)
	}
	upstreamDetail := ""
	if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
		maxBytes := s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes
		if maxBytes <= 0 {
			maxBytes = 2048
		}
		upstreamDetail = truncateString(string(body), maxBytes)
	}
	setOpsUpstreamError(c, resp.StatusCode, upstreamMsg, upstreamDetail)

	if status, errType, errMsg, matched := applyErrorPassthroughRule(
		c, account.Platform, resp.StatusCode, body,
		http.StatusBadGateway, "upstream_error", "Upstream request failed",
	); matched {
		MarkResponseCommitted(c)
		writeVolcengineActionErrorResponse(c, status, errType, errMsg)
		return nil, fmt.Errorf("volcengine upstream error: %d (passthrough rule matched) message=%s", resp.StatusCode, upstreamMsg)
	}
	if !account.ShouldHandleErrorCode(resp.StatusCode) {
		appendVolcengineUpstreamErrorEvent(c, account, resp.StatusCode, requestIDHeader, "http_error", upstreamMsg, upstreamDetail)
		MarkResponseCommitted(c)
		writeVolcengineActionErrorResponse(c, http.StatusInternalServerError, "upstream_error", "Upstream gateway error")
		return nil, fmt.Errorf("volcengine upstream error: %d (not in custom error codes) message=%s", resp.StatusCode, upstreamMsg)
	}

	kind := "http_error"
	if volcengineHTTPStatusFailoverable(resp.StatusCode) {
		kind = "failover"
	}
	appendVolcengineUpstreamErrorEvent(c, account, resp.StatusCode, requestIDHeader, kind, upstreamMsg, upstreamDetail)
	if kind == "failover" {
		return nil, &UpstreamFailoverError{
			StatusCode:             resp.StatusCode,
			ResponseBody:           body,
			ResponseHeaders:        resp.Header.Clone(),
			RequestScopedTransient: resp.StatusCode == http.StatusTooManyRequests,
		}
	}
	MarkResponseCommitted(c)
	writeVolcengineActionErrorResponse(c, resp.StatusCode, volcengineActionErrorType(resp.StatusCode), upstreamMsg)
	return nil, fmt.Errorf("volcengine upstream error: %d %s", resp.StatusCode, upstreamMsg)
}

func appendVolcengineUpstreamErrorEvent(c *gin.Context, account *Account, statusCode int, requestIDHeader, kind, message, detail string) {
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
		ProxyID:            opsUpstreamProxyID(account),
		ProxyName:          opsUpstreamProxyName(account),
		Platform:           account.Platform,
		AccountID:          account.ID,
		AccountName:        account.Name,
		UpstreamStatusCode: statusCode,
		UpstreamRequestID:  requestIDHeader,
		Kind:               kind,
		Message:            message,
		Detail:             detail,
	})
}

func writeVolcengineActionErrorResponse(c *gin.Context, statusCode int, errType, message string) {
	if c == nil || c.Writer == nil || c.Writer.Written() {
		return
	}
	c.JSON(statusCode, gin.H{
		"error": gin.H{
			"type":    strings.TrimSpace(errType),
			"message": strings.TrimSpace(message),
		},
	})
}

func writeVolcengineActionJSONResponse(c *gin.Context, status int, body []byte) {
	if c == nil {
		return
	}
	c.Data(status, "application/json", body)
}

// ---------------------------------------------------------------------------
// Image generation / editing (doubao-seedream-*)
// ---------------------------------------------------------------------------

func volcengineImageResponseFormat(requested string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(requested)) {
	case "", "url":
		return "url", nil
	case "b64_json", "base64":
		return "b64_json", nil
	default:
		return "", fmt.Errorf("response_format %q is not supported by Volcengine image generation", requested)
	}
}

// volcengineImageSize accepts the documented Seedream size forms: a resolution
// tier ("1K"/"2K"/"4K") or "WxH".
func volcengineImageSize(size string) (string, error) {
	trimmed := strings.TrimSpace(size)
	if trimmed == "" {
		return "", nil
	}
	lower := strings.ToLower(trimmed)
	switch lower {
	case "1k", "2k", "4k":
		return strings.ToUpper(lower), nil
	}
	parts := strings.Split(lower, "x")
	if len(parts) != 2 {
		return "", fmt.Errorf("size %q is not supported by Volcengine image generation", size)
	}
	width, errW := strconv.Atoi(strings.TrimSpace(parts[0]))
	height, errH := strconv.Atoi(strings.TrimSpace(parts[1]))
	if errW != nil || errH != nil || width <= 0 || height <= 0 {
		return "", fmt.Errorf("size %q is not supported by Volcengine image generation", size)
	}
	return fmt.Sprintf("%dx%d", width, height), nil
}

// buildVolcengineImageBody converts the inbound images request to the official
// Ark POST {root}/images/generations body. Generation and editing share the one
// endpoint; editing adds the reference `image` (Seedream 图生图 / 多参考图生图).
// Seedream "组图生成" is expressed through sequential_image_generation.
func buildVolcengineImageBody(endpoint VolcengineMediaEndpoint, info VolcengineMediaRequestInfo, model string) ([]byte, error) {
	if strings.TrimSpace(info.Prompt) == "" {
		return nil, fmt.Errorf("prompt is required")
	}
	payload := map[string]any{"prompt": info.Prompt}
	if strings.TrimSpace(model) != "" {
		payload["model"] = model
	}
	responseFormat, err := volcengineImageResponseFormat(info.ResponseFormat)
	if err != nil {
		return nil, err
	}
	payload["response_format"] = responseFormat
	if size, err := volcengineImageSize(info.Size); err != nil {
		return nil, err
	} else if size != "" {
		payload["size"] = size
	}
	if info.Seed != nil {
		payload["seed"] = *info.Seed
	}
	if info.Watermark != nil {
		payload["watermark"] = *info.Watermark
	}
	if info.N > 1 {
		payload["sequential_image_generation"] = "auto"
		payload["sequential_image_generation_options"] = map[string]any{"max_images": info.N}
	}
	if endpoint == VolcengineMediaEndpointImagesEdits {
		if info.MaskUpload != nil {
			return nil, fmt.Errorf("mask is not supported by Volcengine image edits (reference image input only)")
		}
		references := make([]string, 0, len(info.InputImageURLs)+len(info.Uploads))
		for _, imageURL := range info.InputImageURLs {
			if imageURL = strings.TrimSpace(imageURL); imageURL != "" {
				references = append(references, imageURL)
			}
		}
		for _, upload := range info.Uploads {
			dataURL, convErr := openAIImageUploadToDataURL(upload)
			if convErr != nil {
				return nil, convErr
			}
			references = append(references, dataURL)
		}
		if len(references) == 0 {
			return nil, fmt.Errorf("image is required for Volcengine image edits")
		}
		if len(references) == 1 {
			payload["image"] = references[0]
		} else {
			payload["image"] = references
		}
	}
	return marshalOpenAIUpstreamJSON(payload)
}

// bridgeVolcengineImageResponse passes through the Ark images response, which is
// already OpenAI-shaped ({model,created,data:[{url}|{b64_json}],usage}), and
// returns the billable image count.
func bridgeVolcengineImageResponse(respBody []byte) ([]byte, int, error) {
	if businessErr := extractVolcengineBusinessError(respBody); businessErr != nil {
		return nil, 0, businessErr
	}
	data := gjson.GetBytes(respBody, "data")
	if !data.Exists() || !data.IsArray() {
		return nil, 0, fmt.Errorf("volcengine image response is missing data")
	}
	count := 0
	for _, item := range data.Array() {
		if strings.TrimSpace(item.Get("url").String()) != "" || strings.TrimSpace(item.Get("b64_json").String()) != "" {
			count++
		}
	}
	if count == 0 {
		return nil, 0, fmt.Errorf("volcengine image response contains no image output")
	}
	return respBody, count, nil
}

// ---------------------------------------------------------------------------
// Video generation / edit / extend / status / content (doubao-seedance-*)
// ---------------------------------------------------------------------------

// volcengineVideoResolutionUpper normalizes a client/OpenAI-style resolution to
// the Ark enum (480p / 720p / 1080p / 4k). Empty passes through (upstream default).
func volcengineVideoResolutionUpper(requested string) (string, error) {
	value := strings.ToLower(strings.TrimSpace(requested))
	if value == "" {
		return "", nil
	}
	if strings.HasSuffix(value, "p") || value == "4k" {
		return value, nil
	}
	return "", fmt.Errorf("resolution %q is not supported by Volcengine video generation (480p/720p/1080p/4k)", requested)
}

// volcengineVideoBillingResolution folds the Ark enum onto the existing
// 480p/720p/1080p price tiers; 4k prices at the highest (1080p) tier.
func volcengineVideoBillingResolution(upstreamResolution string) string {
	switch strings.ToLower(strings.TrimSpace(upstreamResolution)) {
	case "480p":
		return VideoBillingResolution480P
	case "720p":
		return VideoBillingResolution720P
	case "1080p", "4k":
		return VideoBillingResolution1080P
	default:
		return ""
	}
}

// buildVolcengineVideoCreateBody converts an inbound videos request to the
// official POST {root}/contents/generations/tasks body. The multimodal content
// array carries the text prompt plus, per cell: reference images for
// generation, or the source video for edit/extend (文档 82379/1520757).
func buildVolcengineVideoCreateBody(endpoint VolcengineMediaEndpoint, info VolcengineMediaRequestInfo, model string) ([]byte, error) {
	prompt := strings.TrimSpace(firstNonEmpty(info.Prompt, info.Instruction))
	if prompt == "" {
		return nil, fmt.Errorf("prompt is required")
	}
	content := make([]map[string]any, 0, 4)
	content = append(content, map[string]any{"type": "text", "text": prompt})

	imageRefs := make([]string, 0, 2)
	imageRefs = append(imageRefs, info.InputImageURLs...)
	for _, upload := range info.Uploads {
		dataURL, err := openAIImageUploadToDataURL(upload)
		if err != nil {
			return nil, err
		}
		imageRefs = append(imageRefs, dataURL)
	}

	switch endpoint {
	case VolcengineMediaEndpointVideoEdit, VolcengineMediaEndpointVideoExtend:
		source := ""
		if len(info.InputVideoURLs) > 0 {
			source = strings.TrimSpace(info.InputVideoURLs[0])
		}
		if source == "" {
			return nil, fmt.Errorf("a source video is required for Volcengine video %s", map[VolcengineMediaEndpoint]string{VolcengineMediaEndpointVideoEdit: "edit", VolcengineMediaEndpointVideoExtend: "extend"}[endpoint])
		}
		content = append(content, map[string]any{
			"type":      "video_url",
			"video_url": map[string]string{"url": source},
		})
		if len(imageRefs) > 2 {
			return nil, fmt.Errorf("Volcengine video %s accepts at most a first-frame and a last-frame image", endpoint)
		}
	default:
		if len(imageRefs) > 2 {
			return nil, fmt.Errorf("Volcengine video generation accepts at most a first-frame and a last-frame image")
		}
	}
	roles := []string{"first_frame", "last_frame"}
	for index, ref := range imageRefs {
		if ref = strings.TrimSpace(ref); ref == "" {
			continue
		}
		content = append(content, map[string]any{
			"type":      "image_url",
			"image_url": map[string]string{"url": ref},
			"role":      roles[index],
		})
	}

	payload := map[string]any{"content": content}
	if strings.TrimSpace(model) != "" {
		payload["model"] = model
	}
	resolution, err := volcengineVideoResolutionUpper(info.Resolution)
	if err != nil {
		return nil, err
	}
	if resolution != "" {
		payload["resolution"] = resolution
	}
	if info.DurationSeconds > 0 {
		payload["duration"] = info.DurationSeconds
	}
	if info.AspectRatio != "" {
		payload["ratio"] = info.AspectRatio
	}
	if info.Watermark != nil {
		payload["watermark"] = *info.Watermark
	}
	if info.Seed != nil {
		payload["seed"] = *info.Seed
	}
	return marshalOpenAIUpstreamJSON(payload)
}

// VolcengineVideoPendingBilling is the create-time snapshot used when status
// polling first observes a succeeded task with content.video_url.
type VolcengineVideoPendingBilling struct {
	Model                string `json:"model"`
	BillingModel         string `json:"billing_model,omitempty"`
	UpstreamModel        string `json:"upstream_model,omitempty"`
	VideoResolution      string `json:"video_resolution,omitempty"`
	VideoDurationSeconds int    `json:"video_duration_seconds,omitempty"`
	OriginalModel        string `json:"original_model,omitempty"`
	CreatedAt            string `json:"created_at,omitempty"`
}

func VolcengineVideoPendingCreatedAtNow() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func volcengineVideoPendingBillingKey(taskID string, userID, apiKeyID int64) string {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" || userID <= 0 || apiKeyID <= 0 {
		return ""
	}
	return fmt.Sprintf("volcengine:%d:%d:%s", userID, apiKeyID, taskID)
}

func volcengineVideoPendingBillingTTL() time.Duration { return 24 * time.Hour }
func volcengineVideoBilledClaimTTL() time.Duration    { return 48 * time.Hour }

// VolcengineVideoRequestSessionHash scopes a video task to its owner key.
func VolcengineVideoRequestSessionHash(taskID string, userID, apiKeyID int64) string {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" || userID <= 0 || apiKeyID <= 0 {
		return ""
	}
	ownerSeed := fmt.Sprintf("%d:%d:%s", userID, apiKeyID, taskID)
	return "volcengine-video:" + DeriveSessionHashFromSeed(ownerSeed)
}

func (s *OpenAIGatewayService) BindVolcengineVideoRequestAccount(
	ctx context.Context,
	groupID *int64,
	taskID string,
	userID, apiKeyID, accountID int64,
) error {
	if s == nil || s.cache == nil {
		return fmt.Errorf("volcengine video request binding cache is unavailable")
	}
	cacheKey := s.openAISessionCacheKey(VolcengineVideoRequestSessionHash(taskID, userID, apiKeyID))
	if cacheKey == "" || accountID <= 0 {
		return fmt.Errorf("volcengine video request binding is invalid")
	}
	ttl := volcengineVideoPendingBillingTTL()
	if s.cfg != nil && s.cfg.Gateway.OpenAIWS.StickySessionTTLSeconds > 0 {
		if sticky := time.Duration(s.cfg.Gateway.OpenAIWS.StickySessionTTLSeconds) * time.Second; sticky > ttl {
			ttl = sticky
		}
	}
	return s.cache.SetSessionAccountID(ctx, derefGroupID(groupID), cacheKey, accountID, ttl)
}

func (s *OpenAIGatewayService) ResolveVolcengineVideoRequestAccount(
	ctx context.Context,
	groupID *int64,
	taskID string,
	userID, apiKeyID int64,
) (int64, error) {
	if s == nil || s.cache == nil {
		return 0, fmt.Errorf("volcengine video request binding cache is unavailable")
	}
	cacheKey := s.openAISessionCacheKey(VolcengineVideoRequestSessionHash(taskID, userID, apiKeyID))
	if cacheKey == "" {
		return 0, fmt.Errorf("volcengine video request binding is invalid")
	}
	return s.cache.GetSessionAccountID(ctx, derefGroupID(groupID), cacheKey)
}

// SelectVolcengineVideoRequestAccount re-selects only the bound owner account for
// status/content/cancel lookups so task IDs cannot cross tenants.
func (s *OpenAIGatewayService) SelectVolcengineVideoRequestAccount(
	ctx context.Context, groupID *int64, sessionHash string, accountID int64, requestedModel string,
) (*AccountSelectionResult, OpenAIAccountScheduleDecision, error) {
	decision := OpenAIAccountScheduleDecision{Layer: openAIAccountScheduleLayerSessionSticky}
	if accountID <= 0 || strings.TrimSpace(sessionHash) == "" {
		return nil, decision, ErrNoAvailableAccounts
	}
	ctx = s.withOpenAIGroupPrivacyRequirement(WithOpenAIProfitControlSuppressed(ctx), groupID)
	scheduler := &defaultOpenAIAccountScheduler{service: s}
	selection, _, err := scheduler.selectBySessionHash(ctx, OpenAIAccountScheduleRequest{
		GroupID: groupID, Platform: PlatformVolcengine, SessionHash: sessionHash,
		StickyAccountID: accountID, PreserveStickyBinding: true, DisableStickyEscape: true,
		RequestedModel: requestedModel, RequiredTransport: OpenAIUpstreamTransportHTTPSSE,
		RequirePrivacySet: s.openAIGroupRequiresPrivacySet(ctx, groupID),
	})
	if err != nil {
		return nil, decision, err
	}
	if selection == nil || selection.Account == nil {
		return nil, decision, ErrNoAvailableAccounts
	}
	decision.StickySessionHit = true
	decision.SelectedAccountID = selection.Account.ID
	decision.SelectedAccountType = selection.Account.Type
	return selection, decision, nil
}

func (s *OpenAIGatewayService) StoreVolcengineVideoPendingBilling(
	ctx context.Context,
	taskID string,
	userID, apiKeyID int64,
	pending VolcengineVideoPendingBilling,
) error {
	if s == nil || s.cache == nil {
		return fmt.Errorf("volcengine video pending billing cache is unavailable")
	}
	key := volcengineVideoPendingBillingKey(taskID, userID, apiKeyID)
	if key == "" {
		return fmt.Errorf("volcengine video pending billing key is invalid")
	}
	pending.Model = strings.TrimSpace(pending.Model)
	pending.BillingModel = strings.TrimSpace(pending.BillingModel)
	pending.UpstreamModel = strings.TrimSpace(pending.UpstreamModel)
	pending.OriginalModel = strings.TrimSpace(pending.OriginalModel)
	if pending.VideoResolution != "" {
		pending.VideoResolution = volcengineVideoBillingResolution(pending.VideoResolution)
	}
	if pending.VideoDurationSeconds > 0 {
		pending.VideoDurationSeconds = NormalizeVideoBillingDurationSecondsOrDefault(pending.VideoDurationSeconds)
	}
	if strings.TrimSpace(pending.CreatedAt) == "" {
		pending.CreatedAt = VolcengineVideoPendingCreatedAtNow()
	} else {
		pending.CreatedAt = strings.TrimSpace(pending.CreatedAt)
	}
	payload, err := json.Marshal(pending)
	if err != nil {
		return err
	}
	return s.cache.SetGrokVideoPendingBilling(ctx, key, payload, volcengineVideoPendingBillingTTL())
}

func (s *OpenAIGatewayService) LoadVolcengineVideoPendingBilling(
	ctx context.Context,
	taskID string,
	userID, apiKeyID int64,
) (*VolcengineVideoPendingBilling, error) {
	if s == nil || s.cache == nil {
		return nil, fmt.Errorf("volcengine video pending billing cache is unavailable")
	}
	key := volcengineVideoPendingBillingKey(taskID, userID, apiKeyID)
	if key == "" {
		return nil, fmt.Errorf("volcengine video pending billing key is invalid")
	}
	payload, err := s.cache.GetGrokVideoPendingBilling(ctx, key)
	if err != nil || len(payload) == 0 {
		return nil, err
	}
	var pending VolcengineVideoPendingBilling
	if err := json.Unmarshal(payload, &pending); err != nil {
		return nil, err
	}
	return &pending, nil
}

// ClaimVolcengineVideoBilling returns true once per task so repeated
// status/content polls never double-bill. Fail-closed on cache errors.
func (s *OpenAIGatewayService) ClaimVolcengineVideoBilling(
	ctx context.Context,
	taskID string,
	userID, apiKeyID int64,
) (bool, error) {
	if s == nil || s.cache == nil {
		return false, fmt.Errorf("volcengine video billing claim cache is unavailable")
	}
	key := volcengineVideoPendingBillingKey(taskID, userID, apiKeyID)
	if key == "" {
		return false, fmt.Errorf("volcengine video billing claim key is invalid")
	}
	return s.cache.ClaimGrokVideoBilled(ctx, key, volcengineVideoBilledClaimTTL())
}

// ReleaseVolcengineVideoBilling clears the claim after a failed durable RecordUsage.
func (s *OpenAIGatewayService) ReleaseVolcengineVideoBilling(
	ctx context.Context,
	taskID string,
	userID, apiKeyID int64,
) error {
	if s == nil || s.cache == nil {
		return fmt.Errorf("volcengine video billing claim cache is unavailable")
	}
	key := volcengineVideoPendingBillingKey(taskID, userID, apiKeyID)
	if key == "" {
		return fmt.Errorf("volcengine video billing claim key is invalid")
	}
	return s.cache.ReleaseGrokVideoBilled(ctx, key)
}

// StableVolcengineVideoBillingRequestID is the durable usage/dedup key for one
// async video task across status/content polls (forced prefix registered in
// isForcedUsageBillingRequestID).
func StableVolcengineVideoBillingRequestID(taskID string) string {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return ""
	}
	if strings.HasPrefix(taskID, "volcengine_video:") {
		return taskID
	}
	return "volcengine_video:" + taskID
}

// IsVolcengineVideoStatusBillable matches the Ark success shape:
// status == "succeeded" AND non-empty content.video_url.
func IsVolcengineVideoStatusBillable(statusBody []byte) bool {
	if len(statusBody) == 0 || !gjson.ValidBytes(statusBody) {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(gjson.GetBytes(statusBody, "status").String()), "succeeded") {
		return false
	}
	return strings.TrimSpace(gjson.GetBytes(statusBody, "content.video_url").String()) != ""
}

// ExtractVolcengineVideoBillingFromStatusBody builds usage units from an Ark
// succeeded task. Duration prefers the task duration, then the create-time
// snapshot; the resolution enum folds onto the existing price tiers.
func ExtractVolcengineVideoBillingFromStatusBody(statusBody []byte, pending *VolcengineVideoPendingBilling, taskID string) *OpenAIForwardResult {
	if !IsVolcengineVideoStatusBillable(statusBody) {
		return nil
	}
	model := strings.TrimSpace(gjson.GetBytes(statusBody, "model").String())
	billingModel := ""
	upstreamModel := ""
	resolution := volcengineVideoBillingResolution(gjson.GetBytes(statusBody, "resolution").String())
	durationSeconds := 0
	if duration := gjson.GetBytes(statusBody, "duration"); duration.Exists() && duration.Type == gjson.Number && duration.Int() > 0 {
		durationSeconds = int(duration.Int())
	}
	if pending != nil {
		if model == "" {
			model = firstNonEmpty(pending.BillingModel, pending.Model, pending.OriginalModel)
		}
		billingModel = firstNonEmpty(pending.BillingModel, pending.Model)
		upstreamModel = pending.UpstreamModel
		if resolution == "" {
			resolution = pending.VideoResolution
		}
		if durationSeconds <= 0 {
			durationSeconds = pending.VideoDurationSeconds
		}
	}
	if billingModel == "" {
		billingModel = model
	}
	if resolution == "" {
		resolution = VideoBillingResolution480P
	}
	if durationSeconds > 0 {
		durationSeconds = NormalizeVideoBillingDurationSecondsOrDefault(durationSeconds)
	}
	responseID := strings.TrimSpace(gjson.GetBytes(statusBody, "id").String())
	if responseID == "" {
		responseID = strings.TrimSpace(taskID)
	}
	return &OpenAIForwardResult{
		ResponseID:           responseID,
		Model:                model,
		BillingModel:         billingModel,
		UpstreamModel:        upstreamModel,
		VideoCount:           1,
		VideoResolution:      resolution,
		VideoDurationSeconds: durationSeconds,
	}
}

// volcengineVideoSignedContentURL validates the upstream-issued download URL:
// https only, no embedded credentials, no non-standard port.
func volcengineVideoSignedContentURL(statusBody []byte) (string, error) {
	rawURL := strings.TrimSpace(gjson.GetBytes(statusBody, "content.video_url").String())
	if rawURL == "" {
		return "", nil
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || !strings.EqualFold(parsed.Scheme, "https") || parsed.Hostname() == "" ||
		(parsed.Port() != "" && parsed.Port() != "443") || parsed.User != nil {
		return "", fmt.Errorf("volcengine video status returned an unsupported content URL")
	}
	return parsed.String(), nil
}

// rewriteVolcengineVideoContentURL swaps the time-limited CDN URL for the
// owner-checked gateway proxy path.
func rewriteVolcengineVideoContentURL(body []byte, proxyURL string) []byte {
	if len(body) == 0 || strings.TrimSpace(proxyURL) == "" || !gjson.ValidBytes(body) {
		return body
	}
	if strings.TrimSpace(gjson.GetBytes(body, "content.video_url").String()) == "" {
		return body
	}
	rewritten, err := sjson.SetBytes(body, "content.video_url", proxyURL)
	if err != nil {
		return body
	}
	return rewritten
}

func volcengineVideoContentProxyURL(c *gin.Context, taskID string) string {
	if c == nil || c.Request == nil || c.Request.URL == nil || strings.TrimSpace(taskID) == "" {
		return ""
	}
	pathPrefix := ""
	if strings.HasPrefix(c.Request.URL.Path, "/v1/") {
		pathPrefix = "/v1"
	}
	return pathPrefix + "/videos/" + url.PathEscape(strings.Trim(taskID, "/")) + "/content"
}
