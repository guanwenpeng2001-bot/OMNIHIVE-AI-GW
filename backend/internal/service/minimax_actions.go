package service

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// MiniMax native media/voice adapter. Every endpoint below is backed by official
// MiniMax first-party documentation (platform.minimax.io / platform.minimaxi.com
// OpenAPI references, evidence captured 2026-09-26):
//   - image-01            POST /v1/image_generation (subject_reference for image-to-image)
//   - MiniMax-H3/H3-Max   POST /v2/video_generation + GET /v2/query/video_generation/{task_id}
//                         + DELETE /v2/video_generation/{task_id} (queued-task cancel)
//   - speech-*            POST /v1/t2a_v2 (hex audio, extra_info.usage_characters billing)
//   - asr-1.0             POST /v1/speech_to_text (multipart, OpenAI-shaped response)
//   - voice_clone         POST /v1/files/upload (purpose=voice_clone) + POST /v1/voice_clone
//                         + POST /v1/get_voice + POST /v1/delete_voice
// Auth is `Authorization: Bearer <api_key>` on the same host as the account chat
// base URL; the legacy GroupId query parameter no longer exists in current docs.

type MiniMaxMediaEndpoint string

const (
	MiniMaxMediaEndpointImagesGenerations MiniMaxMediaEndpoint = "images_generations"
	MiniMaxMediaEndpointImagesEdits       MiniMaxMediaEndpoint = "images_edits"
	MiniMaxMediaEndpointVideosGenerations MiniMaxMediaEndpoint = "videos_generations"
	MiniMaxMediaEndpointVideoStatus       MiniMaxMediaEndpoint = "video_status"
	MiniMaxMediaEndpointVideoContent      MiniMaxMediaEndpoint = "video_content"
	MiniMaxMediaEndpointVideoCancel       MiniMaxMediaEndpoint = "video_cancel"
)

func (e MiniMaxMediaEndpoint) RequiresRequestBody() bool {
	switch e {
	case MiniMaxMediaEndpointImagesGenerations, MiniMaxMediaEndpointImagesEdits, MiniMaxMediaEndpointVideosGenerations:
		return true
	default:
		return false
	}
}

func (e MiniMaxMediaEndpoint) IsVideoLookupRequest() bool {
	return e == MiniMaxMediaEndpointVideoStatus || e == MiniMaxMediaEndpointVideoContent || e == MiniMaxMediaEndpointVideoCancel
}

func (e MiniMaxMediaEndpoint) IsGenerationRequest() bool {
	return e == MiniMaxMediaEndpointImagesGenerations || e == MiniMaxMediaEndpointImagesEdits || e == MiniMaxMediaEndpointVideosGenerations
}

// MiniMax voice endpoint names accepted by ForwardMiniMaxVoice.
const (
	MiniMaxVoiceEndpointTTS          = "tts"
	MiniMaxVoiceEndpointSTT          = "stt"
	MiniMaxVoiceEndpointCustomVoices = "custom-voices"
)

// minimaxAPIRoot derives the media API origin from the account chat base URL
// (e.g. https://api.minimaxi.com/v1 -> https://api.minimaxi.com), preserving any
// relay path prefix (https://relay.example/mm/v1 -> https://relay.example/mm).
func minimaxAPIRoot(account *Account) string {
	base := DefaultMiniMaxBaseURL
	if account != nil {
		if configured := strings.TrimSpace(account.GetOpenAIBaseURL()); configured != "" {
			base = configured
		}
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		parsed, _ = url.Parse(DefaultMiniMaxBaseURL)
	}
	path := strings.TrimRight(parsed.Path, "/")
	path = strings.TrimSuffix(path, "/v1")
	parsed.Path = strings.TrimRight(path, "/")
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return strings.TrimRight(parsed.String(), "/")
}

func buildMiniMaxActionURL(account *Account, path string) string {
	return minimaxAPIRoot(account) + "/" + strings.TrimLeft(path, "/")
}

func minimaxVideoStatusPath(taskID string) string {
	return "/v2/query/video_generation/" + url.PathEscape(strings.TrimSpace(taskID))
}

// ---------------------------------------------------------------------------
// Request parsing (OpenAI/grok-dialect inbound shapes)
// ---------------------------------------------------------------------------

type MiniMaxMediaRequestInfo struct {
	Model           string
	Prompt          string
	N               int
	Size            string
	SizeTier        string
	AspectRatio     string
	Resolution      string
	DurationSeconds int
	Seed            *int64
	ResponseFormat  string
	InputImageURLs  []string
	Uploads         []OpenAIImagesUpload
	MaskUpload      *OpenAIImagesUpload
}

func (r MiniMaxMediaRequestInfo) HasInputImage() bool {
	return len(r.InputImageURLs) > 0 || len(r.Uploads) > 0
}

// ModerationBody renders the prompt/reference image for the shared security audit.
func (r MiniMaxMediaRequestInfo) ModerationBody() []byte {
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

func ParseMiniMaxMediaRequest(contentType string, body []byte) MiniMaxMediaRequestInfo {
	info := MiniMaxMediaRequestInfo{N: 1}
	if gjson.ValidBytes(body) {
		parseMiniMaxMediaJSONRequest(body, &info)
	} else {
		parseMiniMaxMediaMultipartRequest(contentType, body, &info)
	}
	info.Model = strings.TrimSpace(info.Model)
	info.Prompt = strings.TrimSpace(info.Prompt)
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

func parseMiniMaxMediaJSONRequest(body []byte, info *MiniMaxMediaRequestInfo) {
	if info == nil {
		return
	}
	info.Model = strings.TrimSpace(gjson.GetBytes(body, "model").String())
	info.Prompt = strings.TrimSpace(gjson.GetBytes(body, "prompt").String())
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
	if n := gjson.GetBytes(body, "n"); n.Exists() && n.Type == gjson.Number {
		info.N = int(n.Int())
	}
	if seed := gjson.GetBytes(body, "seed"); seed.Exists() && seed.Type == gjson.Number {
		seedValue := seed.Int()
		info.Seed = &seedValue
	}
	appendMiniMaxImageRef(gjson.GetBytes(body, "image"), info)
	appendMiniMaxImageRef(gjson.GetBytes(body, "images"), info)
}

func appendMiniMaxImageRef(value gjson.Result, info *MiniMaxMediaRequestInfo) {
	if !value.Exists() || info == nil {
		return
	}
	if value.IsArray() {
		for _, item := range value.Array() {
			appendMiniMaxImageRef(item, info)
		}
		return
	}
	if imageURL := extractMiniMaxImageRefURL(value); imageURL != "" {
		info.InputImageURLs = append(info.InputImageURLs, imageURL)
	}
}

func extractMiniMaxImageRefURL(value gjson.Result) string {
	if !value.Exists() {
		return ""
	}
	if value.Type == gjson.String {
		return strings.TrimSpace(value.String())
	}
	if imageURL := strings.TrimSpace(value.Get("url").String()); imageURL != "" {
		return imageURL
	}
	if nested := value.Get("image_url"); nested.Exists() {
		if nested.Type == gjson.String {
			return strings.TrimSpace(nested.String())
		}
		return strings.TrimSpace(nested.Get("url").String())
	}
	return ""
}

func parseMiniMaxMediaMultipartRequest(contentType string, body []byte, info *MiniMaxMediaRequestInfo) {
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
		case "size":
			info.Size = value
		case "aspect_ratio", "ratio":
			info.AspectRatio = value
		case "resolution":
			info.Resolution = value
		case "response_format":
			info.ResponseFormat = value
		case "duration":
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
		case "image", "image_url":
			if value != "" {
				info.InputImageURLs = append(info.InputImageURLs, value)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// MiniMax business error envelope (base_resp) and error mapping
// ---------------------------------------------------------------------------

// MiniMaxBusinessError is the base_resp{status_code,status_msg} failure returned
// with HTTP 200 by the image/speech/voice APIs. Video V2 and STT instead use
// OpenAI-style error bodies with real HTTP status codes.
type MiniMaxBusinessError struct {
	Code    int
	Message string
}

func (e *MiniMaxBusinessError) Error() string {
	return fmt.Sprintf("minimax upstream error %d: %s", e.Code, e.Message)
}

func extractMiniMaxBusinessError(body []byte) *MiniMaxBusinessError {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return nil
	}
	baseResp := gjson.GetBytes(body, "base_resp")
	if !baseResp.Exists() {
		return nil
	}
	code := int(baseResp.Get("status_code").Int())
	if code == 0 {
		return nil
	}
	message := strings.TrimSpace(baseResp.Get("status_msg").String())
	if message == "" {
		message = fmt.Sprintf("status_code %d", code)
	}
	return &MiniMaxBusinessError{Code: code, Message: message}
}

// httpStatus maps official base_resp codes to gateway client statuses.
func (e *MiniMaxBusinessError) httpStatus() int {
	switch e.Code {
	case 1004, 2049: // authentication failed / invalid API key
		return http.StatusUnauthorized
	case 1008: // insufficient balance
		return http.StatusPaymentRequired
	case 1002, 1039: // rate limit / TPM limit
		return http.StatusTooManyRequests
	case 1026, 1042, 1043: // sensitive content / invalid characters / ASR similarity check
		return http.StatusUnprocessableEntity
	case 2013: // invalid input parameters
		return http.StatusBadRequest
	case 2038: // no cloning permission
		return http.StatusForbidden
	default:
		return http.StatusBadGateway
	}
}

// failoverable reports whether another account may legitimately succeed:
// account-scoped auth/balance failures and transient limits/server errors move
// to the next account; request-scoped validation/moderation failures do not.
func (e *MiniMaxBusinessError) failoverable() bool {
	switch e.Code {
	case 1004, 2049, 1008, 1002, 1039, 1000, 1001, 1013:
		return true
	default:
		return false
	}
}

// minimaxHTTPStatusFailoverable mirrors the failover policy for the
// OpenAI-style error envelopes used by video V2 and STT.
func minimaxHTTPStatusFailoverable(statusCode int) bool {
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

func minimaxActionErrorType(statusCode int) string {
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

// handleMiniMaxActionErrorResponse processes HTTP-level failures from MiniMax
// media/voice endpoints: ops event, configurable passthrough, failover classification.
func (s *OpenAIGatewayService) handleMiniMaxActionErrorResponse(
	ctx context.Context,
	resp *http.Response,
	c *gin.Context,
	account *Account,
	requestIDHeader string,
) (*OpenAIForwardResult, error) {
	body := s.readUpstreamErrorBody(resp)
	upstreamMsg := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(body)))
	if upstreamMsg == "" {
		upstreamMsg = fmt.Sprintf("MiniMax upstream returned status %d", resp.StatusCode)
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
		writeMiniMaxActionErrorResponse(c, status, errType, errMsg)
		return nil, fmt.Errorf("minimax upstream error: %d (passthrough rule matched) message=%s", resp.StatusCode, upstreamMsg)
	}
	if !account.ShouldHandleErrorCode(resp.StatusCode) {
		appendMiniMaxUpstreamErrorEvent(c, account, resp.StatusCode, requestIDHeader, "http_error", upstreamMsg, upstreamDetail)
		MarkResponseCommitted(c)
		writeMiniMaxActionErrorResponse(c, http.StatusInternalServerError, "upstream_error", "Upstream gateway error")
		return nil, fmt.Errorf("minimax upstream error: %d (not in custom error codes) message=%s", resp.StatusCode, upstreamMsg)
	}

	kind := "http_error"
	if minimaxHTTPStatusFailoverable(resp.StatusCode) {
		kind = "failover"
	}
	appendMiniMaxUpstreamErrorEvent(c, account, resp.StatusCode, requestIDHeader, kind, upstreamMsg, upstreamDetail)
	if kind == "failover" {
		return nil, &UpstreamFailoverError{
			StatusCode:             resp.StatusCode,
			ResponseBody:           body,
			ResponseHeaders:        resp.Header.Clone(),
			RequestScopedTransient: resp.StatusCode == http.StatusTooManyRequests,
		}
	}
	MarkResponseCommitted(c)
	writeMiniMaxActionErrorResponse(c, resp.StatusCode, minimaxActionErrorType(resp.StatusCode), upstreamMsg)
	return nil, fmt.Errorf("minimax upstream error: %d %s", resp.StatusCode, upstreamMsg)
}

func appendMiniMaxUpstreamErrorEvent(c *gin.Context, account *Account, statusCode int, requestIDHeader, kind, message, detail string) {
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

// writeMiniMaxActionBusinessError renders a base_resp business failure to the client.
func writeMiniMaxActionBusinessError(c *gin.Context, businessErr *MiniMaxBusinessError) {
	if c == nil || businessErr == nil {
		return
	}
	MarkResponseCommitted(c)
	writeMiniMaxActionErrorResponse(c, businessErr.httpStatus(), minimaxActionErrorType(businessErr.httpStatus()), businessErr.Message)
}

func writeMiniMaxActionErrorResponse(c *gin.Context, statusCode int, errType, message string) {
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

// ---------------------------------------------------------------------------
// Image generation (image-01) bridging
// ---------------------------------------------------------------------------

// minimaxImageResponseFormat maps OpenAI response_format values to MiniMax.
func minimaxImageResponseFormat(requested string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(requested)) {
	case "", "url":
		return "url", nil
	case "b64_json", "base64":
		return "base64", nil
	default:
		return "", fmt.Errorf("response_format %q is not supported by MiniMax image generation", requested)
	}
}

// buildMiniMaxImageBody converts the inbound images request to the official
// POST /v1/image_generation JSON body.
func buildMiniMaxImageBody(endpoint MiniMaxMediaEndpoint, info MiniMaxMediaRequestInfo, model string) ([]byte, error) {
	if strings.TrimSpace(info.Prompt) == "" {
		return nil, fmt.Errorf("prompt is required")
	}
	if strings.TrimSpace(model) == "" {
		model = "image-01"
	}
	payload := map[string]any{
		"model":  model,
		"prompt": info.Prompt,
	}
	if info.N > 1 {
		payload["n"] = info.N
	}
	if info.AspectRatio != "" {
		payload["aspect_ratio"] = info.AspectRatio
	} else if size := strings.TrimSpace(info.Size); size != "" {
		width, height, ok := parseMiniMaxImageSize(size)
		if !ok {
			return nil, fmt.Errorf("size %q is not supported by MiniMax image generation", info.Size)
		}
		payload["width"] = width
		payload["height"] = height
	}
	responseFormat, err := minimaxImageResponseFormat(info.ResponseFormat)
	if err != nil {
		return nil, err
	}
	if responseFormat != "url" {
		payload["response_format"] = responseFormat
	}
	if info.Seed != nil {
		payload["seed"] = *info.Seed
	}
	if endpoint == MiniMaxMediaEndpointImagesEdits {
		if info.MaskUpload != nil {
			return nil, fmt.Errorf("mask is not supported by MiniMax image edits (subject_reference only)")
		}
		reference := ""
		if len(info.InputImageURLs) > 0 {
			reference = strings.TrimSpace(info.InputImageURLs[0])
		} else if len(info.Uploads) > 0 {
			dataURL, convErr := openAIImageUploadToDataURL(info.Uploads[0])
			if convErr != nil {
				return nil, convErr
			}
			reference = dataURL
		}
		if reference == "" {
			return nil, fmt.Errorf("image is required for MiniMax image edits")
		}
		if len(info.InputImageURLs)+len(info.Uploads) > 1 {
			return nil, fmt.Errorf("MiniMax image edits accept exactly one subject reference image")
		}
		payload["subject_reference"] = []map[string]string{
			{"type": "character", "image_file": reference},
		}
	}
	return marshalOpenAIUpstreamJSON(payload)
}

func parseMiniMaxImageSize(size string) (int, int, bool) {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(size)), "x")
	if len(parts) != 2 {
		return 0, 0, false
	}
	width, errW := strconv.Atoi(parts[0])
	height, errH := strconv.Atoi(parts[1])
	if errW != nil || errH != nil {
		return 0, 0, false
	}
	// Official bounds: [512, 2048], divisible by 8.
	if width < 512 || width > 2048 || height < 512 || height > 2048 || width%8 != 0 || height%8 != 0 {
		return 0, 0, false
	}
	return width, height, true
}

// bridgeMiniMaxImageResponse converts the official response into the OpenAI
// images response shape ({created, data:[{url}|{b64_json}]}).
func bridgeMiniMaxImageResponse(respBody []byte) ([]byte, int, error) {
	if businessErr := extractMiniMaxBusinessError(respBody); businessErr != nil {
		return nil, 0, businessErr
	}
	data := gjson.GetBytes(respBody, "data")
	if !data.Exists() {
		return nil, 0, fmt.Errorf("minimax image response is missing data")
	}
	items := make([]map[string]string, 0, 4)
	for _, imageURL := range data.Get("image_urls").Array() {
		if value := strings.TrimSpace(imageURL.String()); value != "" {
			items = append(items, map[string]string{"url": value})
		}
	}
	for _, encoded := range data.Get("image_base64").Array() {
		if value := strings.TrimSpace(encoded.String()); value != "" {
			items = append(items, map[string]string{"b64_json": value})
		}
	}
	if len(items) == 0 {
		failedCount := gjson.GetBytes(respBody, "metadata.failed_count").Int()
		if failedCount > 0 {
			return nil, 0, &MiniMaxBusinessError{Code: 1026, Message: "all images were blocked by upstream content safety"}
		}
		return nil, 0, fmt.Errorf("minimax image response contains no image output")
	}
	out, err := marshalOpenAIUpstreamJSON(map[string]any{
		"created": time.Now().Unix(),
		"data":    items,
	})
	if err != nil {
		return nil, 0, err
	}
	return out, len(items), nil
}

// ---------------------------------------------------------------------------
// Video generation (MiniMax-H3 V2) bridging, ownership, deferred billing
// ---------------------------------------------------------------------------

// minimaxVideoUpstreamResolution maps client/OpenAI-style resolution values to
// the official V2 enum (480P / 768P / 2K). Empty passes through (upstream default).
func minimaxVideoUpstreamResolution(requested string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(requested)) {
	case "":
		return "", nil
	case "480p":
		return "480P", nil
	case "720p", "768p":
		return "768P", nil
	case "1080p", "2k":
		return "2K", nil
	default:
		return "", fmt.Errorf("resolution %q is not supported by MiniMax video generation (480P/768P/2K)", requested)
	}
}

// minimaxVideoBillingResolution folds the official enum onto the existing
// 480p/720p/1080p price tiers: 768P prices at 720p, 2K at the highest (1080p) tier.
func minimaxVideoBillingResolution(upstreamResolution string) string {
	switch strings.ToUpper(strings.TrimSpace(upstreamResolution)) {
	case "480P":
		return VideoBillingResolution480P
	case "768P":
		return VideoBillingResolution720P
	case "2K":
		return VideoBillingResolution1080P
	default:
		return ""
	}
}

// buildMiniMaxVideoCreateBody converts the inbound videos request to the official
// POST /v2/video_generation JSON body (multimodal content array).
func buildMiniMaxVideoCreateBody(info MiniMaxMediaRequestInfo, model string) ([]byte, error) {
	if strings.TrimSpace(info.Prompt) == "" {
		return nil, fmt.Errorf("prompt is required")
	}
	if strings.TrimSpace(model) == "" {
		model = "MiniMax-H3"
	}
	content := make([]map[string]any, 0, 3)
	content = append(content, map[string]any{"type": "text", "text": info.Prompt})
	imageRefs := make([]string, 0, 2)
	imageRefs = append(imageRefs, info.InputImageURLs...)
	for _, upload := range info.Uploads {
		dataURL, err := openAIImageUploadToDataURL(upload)
		if err != nil {
			return nil, err
		}
		imageRefs = append(imageRefs, dataURL)
	}
	if len(imageRefs) > 2 {
		return nil, fmt.Errorf("MiniMax video generation accepts at most a first-frame and a last-frame image")
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
	payload := map[string]any{
		"model":   model,
		"content": content,
	}
	resolution, err := minimaxVideoUpstreamResolution(info.Resolution)
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
	return marshalOpenAIUpstreamJSON(payload)
}

// MiniMaxVideoPendingBilling is the create-time snapshot used when status polling
// first observes a succeeded task with content.url.
type MiniMaxVideoPendingBilling struct {
	Model                string `json:"model"`
	BillingModel         string `json:"billing_model,omitempty"`
	UpstreamModel        string `json:"upstream_model,omitempty"`
	VideoResolution      string `json:"video_resolution,omitempty"`
	VideoDurationSeconds int    `json:"video_duration_seconds,omitempty"`
	OriginalModel        string `json:"original_model,omitempty"`
	CreatedAt            string `json:"created_at,omitempty"`
}

func MiniMaxVideoPendingCreatedAtNow() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func minimaxVideoPendingBillingKey(taskID string, userID, apiKeyID int64) string {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" || userID <= 0 || apiKeyID <= 0 {
		return ""
	}
	return fmt.Sprintf("minimax:%d:%d:%s", userID, apiKeyID, taskID)
}

func minimaxVideoPendingBillingTTL() time.Duration { return 24 * time.Hour }
func minimaxVideoBilledClaimTTL() time.Duration    { return 48 * time.Hour }

// MiniMaxVideoRequestSessionHash scopes a video task to its owner key.
func MiniMaxVideoRequestSessionHash(taskID string, userID, apiKeyID int64) string {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" || userID <= 0 || apiKeyID <= 0 {
		return ""
	}
	ownerSeed := fmt.Sprintf("%d:%d:%s", userID, apiKeyID, taskID)
	return "minimax-video:" + DeriveSessionHashFromSeed(ownerSeed)
}

func (s *OpenAIGatewayService) BindMiniMaxVideoRequestAccount(
	ctx context.Context,
	groupID *int64,
	taskID string,
	userID, apiKeyID, accountID int64,
) error {
	if s == nil || s.cache == nil {
		return fmt.Errorf("minimax video request binding cache is unavailable")
	}
	cacheKey := s.openAISessionCacheKey(MiniMaxVideoRequestSessionHash(taskID, userID, apiKeyID))
	if cacheKey == "" || accountID <= 0 {
		return fmt.Errorf("minimax video request binding is invalid")
	}
	ttl := minimaxVideoPendingBillingTTL()
	if s.cfg != nil && s.cfg.Gateway.OpenAIWS.StickySessionTTLSeconds > 0 {
		if sticky := time.Duration(s.cfg.Gateway.OpenAIWS.StickySessionTTLSeconds) * time.Second; sticky > ttl {
			ttl = sticky
		}
	}
	return s.cache.SetSessionAccountID(ctx, derefGroupID(groupID), cacheKey, accountID, ttl)
}

func (s *OpenAIGatewayService) ResolveMiniMaxVideoRequestAccount(
	ctx context.Context,
	groupID *int64,
	taskID string,
	userID, apiKeyID int64,
) (int64, error) {
	if s == nil || s.cache == nil {
		return 0, fmt.Errorf("minimax video request binding cache is unavailable")
	}
	cacheKey := s.openAISessionCacheKey(MiniMaxVideoRequestSessionHash(taskID, userID, apiKeyID))
	if cacheKey == "" {
		return 0, fmt.Errorf("minimax video request binding is invalid")
	}
	return s.cache.GetSessionAccountID(ctx, derefGroupID(groupID), cacheKey)
}

// SelectMiniMaxVideoRequestAccount re-selects only the bound owner account for
// status/content/cancel lookups so task IDs cannot cross tenants.
func (s *OpenAIGatewayService) SelectMiniMaxVideoRequestAccount(
	ctx context.Context, groupID *int64, sessionHash string, accountID int64, requestedModel string,
) (*AccountSelectionResult, OpenAIAccountScheduleDecision, error) {
	decision := OpenAIAccountScheduleDecision{Layer: openAIAccountScheduleLayerSessionSticky}
	if accountID <= 0 || strings.TrimSpace(sessionHash) == "" {
		return nil, decision, ErrNoAvailableAccounts
	}
	ctx = s.withOpenAIGroupPrivacyRequirement(WithOpenAIProfitControlSuppressed(ctx), groupID)
	scheduler := &defaultOpenAIAccountScheduler{service: s}
	selection, _, err := scheduler.selectBySessionHash(ctx, OpenAIAccountScheduleRequest{
		GroupID: groupID, Platform: PlatformMiniMax, SessionHash: sessionHash,
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

func (s *OpenAIGatewayService) StoreMiniMaxVideoPendingBilling(
	ctx context.Context,
	taskID string,
	userID, apiKeyID int64,
	pending MiniMaxVideoPendingBilling,
) error {
	if s == nil || s.cache == nil {
		return fmt.Errorf("minimax video pending billing cache is unavailable")
	}
	key := minimaxVideoPendingBillingKey(taskID, userID, apiKeyID)
	if key == "" {
		return fmt.Errorf("minimax video pending billing key is invalid")
	}
	pending.Model = strings.TrimSpace(pending.Model)
	pending.BillingModel = strings.TrimSpace(pending.BillingModel)
	pending.UpstreamModel = strings.TrimSpace(pending.UpstreamModel)
	pending.OriginalModel = strings.TrimSpace(pending.OriginalModel)
	if pending.VideoResolution != "" {
		pending.VideoResolution = minimaxVideoBillingResolution(pending.VideoResolution)
	}
	if pending.VideoDurationSeconds > 0 {
		pending.VideoDurationSeconds = NormalizeVideoBillingDurationSecondsOrDefault(pending.VideoDurationSeconds)
	}
	if strings.TrimSpace(pending.CreatedAt) == "" {
		pending.CreatedAt = MiniMaxVideoPendingCreatedAtNow()
	} else {
		pending.CreatedAt = strings.TrimSpace(pending.CreatedAt)
	}
	payload, err := json.Marshal(pending)
	if err != nil {
		return err
	}
	return s.cache.SetGrokVideoPendingBilling(ctx, key, payload, minimaxVideoPendingBillingTTL())
}

func (s *OpenAIGatewayService) LoadMiniMaxVideoPendingBilling(
	ctx context.Context,
	taskID string,
	userID, apiKeyID int64,
) (*MiniMaxVideoPendingBilling, error) {
	if s == nil || s.cache == nil {
		return nil, fmt.Errorf("minimax video pending billing cache is unavailable")
	}
	key := minimaxVideoPendingBillingKey(taskID, userID, apiKeyID)
	if key == "" {
		return nil, fmt.Errorf("minimax video pending billing key is invalid")
	}
	payload, err := s.cache.GetGrokVideoPendingBilling(ctx, key)
	if err != nil || len(payload) == 0 {
		return nil, err
	}
	var pending MiniMaxVideoPendingBilling
	if err := json.Unmarshal(payload, &pending); err != nil {
		return nil, err
	}
	return &pending, nil
}

// ClaimMiniMaxVideoBilling returns true once per task so repeated status/content
// polls never double-bill. Fail-closed on cache errors.
func (s *OpenAIGatewayService) ClaimMiniMaxVideoBilling(
	ctx context.Context,
	taskID string,
	userID, apiKeyID int64,
) (bool, error) {
	if s == nil || s.cache == nil {
		return false, fmt.Errorf("minimax video billing claim cache is unavailable")
	}
	key := minimaxVideoPendingBillingKey(taskID, userID, apiKeyID)
	if key == "" {
		return false, fmt.Errorf("minimax video billing claim key is invalid")
	}
	return s.cache.ClaimGrokVideoBilled(ctx, key, minimaxVideoBilledClaimTTL())
}

// ReleaseMiniMaxVideoBilling clears the claim after a failed durable RecordUsage.
func (s *OpenAIGatewayService) ReleaseMiniMaxVideoBilling(
	ctx context.Context,
	taskID string,
	userID, apiKeyID int64,
) error {
	if s == nil || s.cache == nil {
		return fmt.Errorf("minimax video billing claim cache is unavailable")
	}
	key := minimaxVideoPendingBillingKey(taskID, userID, apiKeyID)
	if key == "" {
		return fmt.Errorf("minimax video billing claim key is invalid")
	}
	return s.cache.ReleaseGrokVideoBilled(ctx, key)
}

// StableMiniMaxVideoBillingRequestID is the durable usage/dedup key for one async
// video task across status/content polls (forced prefix registered in
// isForcedUsageBillingRequestID).
func StableMiniMaxVideoBillingRequestID(taskID string) string {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return ""
	}
	if strings.HasPrefix(taskID, "minimax_video:") {
		return taskID
	}
	return "minimax_video:" + taskID
}

// StableMiniMaxAudioBillingRequestID is the durable usage/dedup key for one TTS/STT call.
func StableMiniMaxAudioBillingRequestID(upstreamRequestID string) string {
	upstreamRequestID = strings.TrimSpace(upstreamRequestID)
	if strings.HasPrefix(upstreamRequestID, "minimax_audio:") {
		return upstreamRequestID
	}
	if upstreamRequestID == "" {
		return "minimax_audio:" + generateRequestID()
	}
	return "minimax_audio:" + upstreamRequestID
}

// IsMiniMaxVideoStatusBillable matches the official success shape:
// task.status == "succeeded" AND non-empty task.content.url.
func IsMiniMaxVideoStatusBillable(statusBody []byte) bool {
	if len(statusBody) == 0 || !gjson.ValidBytes(statusBody) {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(gjson.GetBytes(statusBody, "task.status").String()), "succeeded") {
		return false
	}
	return strings.TrimSpace(gjson.GetBytes(statusBody, "task.content.url").String()) != ""
}

// ExtractMiniMaxVideoBillingFromStatusBody builds usage units from an official
// succeeded task. Duration prefers usage.output_seconds, then task.duration, then
// the create-time snapshot; resolution folds 768P→720p / 2K→1080p price tiers.
func ExtractMiniMaxVideoBillingFromStatusBody(statusBody []byte, pending *MiniMaxVideoPendingBilling, taskID string) *OpenAIForwardResult {
	if !IsMiniMaxVideoStatusBillable(statusBody) {
		return nil
	}
	task := gjson.GetBytes(statusBody, "task")
	model := strings.TrimSpace(task.Get("model").String())
	billingModel := ""
	upstreamModel := ""
	resolution := minimaxVideoBillingResolution(task.Get("resolution").String())
	durationSeconds := 0
	if usage := task.Get("usage"); usage.Exists() {
		if seconds := usage.Get("output_seconds"); seconds.Exists() && seconds.Type == gjson.Number && seconds.Int() > 0 {
			durationSeconds = int(seconds.Int())
		}
	}
	if durationSeconds <= 0 {
		if duration := task.Get("duration"); duration.Exists() && duration.Type == gjson.Number {
			durationSeconds = int(duration.Int())
		}
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
	if model == "" {
		model = "MiniMax-H3"
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
	responseID := strings.TrimSpace(task.Get("id").String())
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

// ---------------------------------------------------------------------------
// Media forwarder (image generate/edit, video create/status/content/cancel)
// ---------------------------------------------------------------------------

func (s *OpenAIGatewayService) ForwardMiniMaxMedia(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	endpoint MiniMaxMediaEndpoint,
	taskID string,
	body []byte,
	contentType string,
) (*OpenAIForwardResult, error) {
	startTime := time.Now()
	if account == nil {
		return nil, fmt.Errorf("minimax account is required")
	}
	if account.Platform != PlatformMiniMax {
		return nil, fmt.Errorf("account platform %s is not supported for minimax media", account.Platform)
	}
	token, _, err := s.getRequestCredential(ctx, c, account)
	if err != nil {
		return nil, err
	}
	if endpoint == MiniMaxMediaEndpointVideoContent {
		return s.forwardMiniMaxVideoContent(ctx, c, account, token, taskID, startTime)
	}

	var method, targetURL string
	var upstreamBody []byte
	requestInfo := ParseMiniMaxMediaRequest(contentType, body)
	upstreamModel := requestInfo.Model
	switch endpoint {
	case MiniMaxMediaEndpointImagesGenerations, MiniMaxMediaEndpointImagesEdits:
		method, targetURL = http.MethodPost, buildMiniMaxActionURL(account, "/v1/image_generation")
		if mapped := strings.TrimSpace(account.GetMappedModel(requestInfo.Model)); mapped != "" {
			upstreamModel = mapped
		}
		upstreamBody, err = buildMiniMaxImageBody(endpoint, requestInfo, upstreamModel)
	case MiniMaxMediaEndpointVideosGenerations:
		method, targetURL = http.MethodPost, buildMiniMaxActionURL(account, "/v2/video_generation")
		if mapped := strings.TrimSpace(account.GetMappedModel(requestInfo.Model)); mapped != "" {
			upstreamModel = mapped
		}
		upstreamBody, err = buildMiniMaxVideoCreateBody(requestInfo, upstreamModel)
	case MiniMaxMediaEndpointVideoStatus:
		method, targetURL = http.MethodGet, buildMiniMaxActionURL(account, minimaxVideoStatusPath(taskID))
	case MiniMaxMediaEndpointVideoCancel:
		method, targetURL = http.MethodDelete, buildMiniMaxActionURL(account, "/v2/video_generation/"+url.PathEscape(strings.TrimSpace(taskID)))
	default:
		return nil, fmt.Errorf("unsupported minimax media endpoint: %s", endpoint)
	}
	if err != nil {
		writeMiniMaxActionErrorResponse(c, http.StatusBadRequest, "invalid_request_error", err.Error())
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

	proxyURL, egressErr := account.EgressProxyURL(ctx)
	if egressErr != nil {
		return nil, egressErr
	}
	upstreamStart := time.Now()
	resp, err := generationDo(accountHTTPUpstream(s.httpUpstream, account), upstreamReq, proxyURL, account.ID, account.Concurrency)
	SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(upstreamStart).Milliseconds())
	if err != nil {
		return nil, s.handleOpenAIUpstreamTransportError(ctx, c, account, err, false)
	}
	defer func() { _ = resp.Body.Close() }()

	requestIDHeader := firstNonEmpty(resp.Header.Get("x-request-id"), resp.Header.Get("x-tt-logid"))
	if resp.StatusCode >= 400 {
		return s.handleMiniMaxActionErrorResponse(ctx, resp, c, account, requestIDHeader)
	}
	respBody, err := ReadUpstreamResponseBody(resp.Body, s.cfg, c, openAITooLargeError)
	if err != nil {
		return nil, err
	}

	switch endpoint {
	case MiniMaxMediaEndpointImagesGenerations, MiniMaxMediaEndpointImagesEdits:
		bridged, imageCount, bridgeErr := bridgeMiniMaxImageResponse(respBody)
		if bridgeErr != nil {
			var businessErr *MiniMaxBusinessError
			if errors.As(bridgeErr, &businessErr) {
				if businessErr.failoverable() {
					return nil, &UpstreamFailoverError{StatusCode: businessErr.httpStatus(), ResponseBody: respBody, ResponseHeaders: resp.Header.Clone()}
				}
				writeMiniMaxActionBusinessError(c, businessErr)
				return nil, businessErr
			}
			setOpsUpstreamError(c, http.StatusBadGateway, "MiniMax upstream returned no usable image output", truncateString(string(respBody), 512))
			return nil, &UpstreamFailoverError{StatusCode: http.StatusBadGateway, ResponseBody: respBody, ResponseHeaders: resp.Header.Clone()}
		}
		writeMiniMaxActionJSONResponse(c, http.StatusOK, bridged)
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
	case MiniMaxMediaEndpointVideosGenerations:
		createdTaskID := strings.TrimSpace(gjson.GetBytes(respBody, "task_id").String())
		if createdTaskID == "" {
			setOpsUpstreamError(c, http.StatusBadGateway, "MiniMax video create returned no task_id", truncateString(string(respBody), 512))
			return nil, &UpstreamFailoverError{StatusCode: http.StatusBadGateway, ResponseBody: respBody, ResponseHeaders: resp.Header.Clone()}
		}
		writeMiniMaxActionJSONResponse(c, http.StatusOK, respBody)
		// Fold the official resolution enum (validated during body build) onto the
		// existing 480p/720p/1080p price tiers; upstream default is 768P.
		createResolution, _ := minimaxVideoUpstreamResolution(requestInfo.Resolution)
		if createResolution == "" {
			createResolution = "768P"
		}
		return &OpenAIForwardResult{
			RequestID:            requestIDHeader,
			ResponseID:           createdTaskID,
			Model:                requestInfo.Model,
			BillingModel:         requestInfo.Model,
			UpstreamModel:        upstreamModel,
			Duration:             time.Since(startTime),
			VideoResolution:      minimaxVideoBillingResolution(createResolution),
			VideoDurationSeconds: requestInfo.DurationSeconds,
		}, nil
	case MiniMaxMediaEndpointVideoStatus:
		respBody = rewriteMiniMaxVideoContentURL(respBody, minimaxVideoContentProxyURL(c, taskID))
		writeMiniMaxActionJSONResponse(c, http.StatusOK, respBody)
		result := &OpenAIForwardResult{
			RequestID: requestIDHeader,
			Duration:  time.Since(startTime),
		}
		if billed := ExtractMiniMaxVideoBillingFromStatusBody(respBody, nil, taskID); billed != nil {
			result.ResponseID = billed.ResponseID
			result.Model = billed.Model
			result.BillingModel = billed.BillingModel
			result.UpstreamModel = billed.UpstreamModel
			result.VideoCount = billed.VideoCount
			result.VideoResolution = billed.VideoResolution
			result.VideoDurationSeconds = billed.VideoDurationSeconds
		}
		return result, nil
	case MiniMaxMediaEndpointVideoCancel:
		writeMiniMaxActionJSONResponse(c, http.StatusOK, respBody)
		return &OpenAIForwardResult{
			RequestID:  requestIDHeader,
			ResponseID: strings.TrimSpace(taskID),
			Duration:   time.Since(startTime),
		}, nil
	}
	return nil, fmt.Errorf("unsupported minimax media endpoint: %s", endpoint)
}

// forwardMiniMaxVideoContent streams the CDN video bytes for a succeeded task:
// status lookup -> content.url -> validated https download with Range passthrough.
func (s *OpenAIGatewayService) forwardMiniMaxVideoContent(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	token, taskID string,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	statusURL := buildMiniMaxActionURL(account, minimaxVideoStatusPath(taskID))
	upstreamCtx, releaseUpstreamCtx := detachUpstreamContext(ctx)
	defer releaseUpstreamCtx()
	proxyURL, egressErr := account.EgressProxyURL(ctx)
	if egressErr != nil {
		return nil, egressErr
	}

	statusReq, err := http.NewRequestWithContext(WithHTTPUpstreamRedirectsDisabled(upstreamCtx), http.MethodGet, statusURL, nil)
	if err != nil {
		return nil, err
	}
	statusReq.Header.Set("Authorization", "Bearer "+token)
	statusReq.Header.Set("Accept", "application/json")
	account.ApplyHeaderOverrides(statusReq.Header)
	upstreamStart := time.Now()
	statusResp, err := generationDo(accountHTTPUpstream(s.httpUpstream, account), statusReq, proxyURL, account.ID, account.Concurrency)
	if err != nil {
		SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(upstreamStart).Milliseconds())
		return nil, s.handleOpenAIUpstreamTransportError(ctx, c, account, err, false)
	}
	statusRequestID := statusResp.Header.Get("x-request-id")
	if statusResp.StatusCode >= 300 {
		defer func() { _ = statusResp.Body.Close() }()
		SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(upstreamStart).Milliseconds())
		if statusResp.StatusCode < 400 {
			return nil, fmt.Errorf("minimax video status redirect is not allowed")
		}
		return s.handleMiniMaxActionErrorResponse(ctx, statusResp, c, account, statusRequestID)
	}
	statusBody, err := ReadUpstreamResponseBody(statusResp.Body, s.cfg, c, openAITooLargeError)
	_ = statusResp.Body.Close()
	if err != nil {
		SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(upstreamStart).Milliseconds())
		return nil, err
	}

	contentURL, err := minimaxVideoSignedContentURL(statusBody)
	if err != nil {
		SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(upstreamStart).Milliseconds())
		return nil, err
	}
	if contentURL == "" {
		SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(upstreamStart).Milliseconds())
		MarkResponseCommitted(c)
		writeMiniMaxActionErrorResponse(c, http.StatusConflict, "invalid_request_error", "Video content is not available yet")
		return nil, fmt.Errorf("minimax video task %s has no content url", strings.TrimSpace(taskID))
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
	contentResp, err := generationDo(accountHTTPUpstream(s.httpUpstream, account), contentReq, proxyURL, account.ID, account.Concurrency)
	SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(upstreamStart).Milliseconds())
	if err != nil {
		return nil, s.handleOpenAIUpstreamTransportError(ctx, c, account, err, false)
	}
	defer func() { _ = contentResp.Body.Close() }()
	if contentResp.StatusCode >= 300 && contentResp.StatusCode < 400 {
		return nil, fmt.Errorf("minimax video content redirect is not allowed")
	}
	if contentResp.StatusCode >= 400 && contentResp.StatusCode != http.StatusRequestedRangeNotSatisfiable {
		return s.handleMiniMaxActionErrorResponse(ctx, contentResp, c, account, firstNonEmpty(contentResp.Header.Get("x-request-id"), statusRequestID))
	}
	if err := writeGrokMediaContentResponse(c, contentResp); err != nil {
		return nil, err
	}
	result := &OpenAIForwardResult{
		RequestID: firstNonEmpty(contentResp.Header.Get("x-request-id"), statusRequestID),
		Duration:  time.Since(startTime),
	}
	if billed := ExtractMiniMaxVideoBillingFromStatusBody(statusBody, nil, taskID); billed != nil {
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

// minimaxVideoSignedContentURL validates the upstream-issued download URL:
// https only, no embedded credentials, no non-standard port.
func minimaxVideoSignedContentURL(statusBody []byte) (string, error) {
	rawURL := strings.TrimSpace(gjson.GetBytes(statusBody, "task.content.url").String())
	if rawURL == "" {
		return "", nil
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || !strings.EqualFold(parsed.Scheme, "https") || parsed.Hostname() == "" ||
		(parsed.Port() != "" && parsed.Port() != "443") || parsed.User != nil {
		return "", fmt.Errorf("minimax video status returned an unsupported content URL")
	}
	return parsed.String(), nil
}

// rewriteMiniMaxVideoContentURL swaps the time-limited CDN URL for the
// owner-checked gateway proxy path.
func rewriteMiniMaxVideoContentURL(body []byte, proxyURL string) []byte {
	if len(body) == 0 || strings.TrimSpace(proxyURL) == "" || !gjson.ValidBytes(body) {
		return body
	}
	if strings.TrimSpace(gjson.GetBytes(body, "task.content.url").String()) == "" {
		return body
	}
	rewritten, err := sjson.SetBytes(body, "task.content.url", proxyURL)
	if err != nil {
		return body
	}
	return rewritten
}

func minimaxVideoContentProxyURL(c *gin.Context, taskID string) string {
	if c == nil || c.Request == nil || c.Request.URL == nil || strings.TrimSpace(taskID) == "" {
		return ""
	}
	pathPrefix := ""
	if strings.HasPrefix(c.Request.URL.Path, "/v1/") {
		pathPrefix = "/v1"
	}
	return pathPrefix + "/videos/" + url.PathEscape(strings.Trim(taskID, "/")) + "/content"
}

func writeMiniMaxActionJSONResponse(c *gin.Context, status int, body []byte) {
	if c == nil {
		return
	}
	c.Data(status, "application/json", body)
}

// ---------------------------------------------------------------------------
// TTS (t2a_v2) / STT (speech_to_text) / voice cloning bridging
// ---------------------------------------------------------------------------

var minimaxTTSSupportedFormats = map[string]string{
	"mp3":  "audio/mpeg",
	"wav":  "audio/wav",
	"pcm":  "audio/L16",
	"flac": "audio/flac",
	"opus": "audio/opus",
}

// buildMiniMaxTTSBody converts an OpenAI /audio/speech request to t2a_v2 JSON.
func buildMiniMaxTTSBody(body []byte, model string) ([]byte, error) {
	if !gjson.ValidBytes(body) {
		return nil, fmt.Errorf("request body must be JSON")
	}
	text := strings.TrimSpace(firstNonEmpty(
		gjson.GetBytes(body, "input").String(),
		gjson.GetBytes(body, "text").String(),
	))
	if text == "" {
		return nil, fmt.Errorf("input is required")
	}
	if strings.TrimSpace(model) == "" {
		model = "speech-2.8-hd"
	}
	voiceID := strings.TrimSpace(firstNonEmpty(
		gjson.GetBytes(body, "voice").String(),
		gjson.GetBytes(body, "voice_id").String(),
	))
	if voiceID == "" {
		return nil, fmt.Errorf("voice is required")
	}
	format := strings.ToLower(strings.TrimSpace(gjson.GetBytes(body, "response_format").String()))
	if format == "" {
		format = "mp3"
	}
	if _, ok := minimaxTTSSupportedFormats[format]; !ok {
		return nil, fmt.Errorf("response_format %q is not supported by MiniMax speech (mp3/wav/pcm/flac/opus)", format)
	}
	voiceSetting := map[string]any{"voice_id": voiceID}
	if speed := gjson.GetBytes(body, "speed"); speed.Exists() && speed.Type == gjson.Number && speed.Float() > 0 {
		voiceSetting["speed"] = speed.Float()
	}
	payload := map[string]any{
		"model":         model,
		"text":          text,
		"stream":        false,
		"output_format": "hex",
		"voice_setting": voiceSetting,
		"audio_setting": map[string]any{"format": format},
	}
	return marshalOpenAIUpstreamJSON(payload)
}

// MiniMaxTTSResult carries the decoded audio and official billing units.
type MiniMaxTTSResult struct {
	Audio           []byte
	Format          string
	ContentType     string
	UsageCharacters int64
	TraceID         string
}

// decodeMiniMaxTTSResponse validates base_resp and hex-decodes the audio payload.
func decodeMiniMaxTTSResponse(respBody []byte) (*MiniMaxTTSResult, error) {
	if businessErr := extractMiniMaxBusinessError(respBody); businessErr != nil {
		return nil, businessErr
	}
	hexAudio := strings.TrimSpace(gjson.GetBytes(respBody, "data.audio").String())
	if hexAudio == "" {
		return nil, fmt.Errorf("minimax tts response contains no audio data")
	}
	audio, err := hex.DecodeString(hexAudio)
	if err != nil {
		return nil, fmt.Errorf("minimax tts response audio is not valid hex: %w", err)
	}
	format := strings.ToLower(strings.TrimSpace(gjson.GetBytes(respBody, "extra_info.audio_format").String()))
	contentType, ok := minimaxTTSSupportedFormats[format]
	if !ok {
		format = "mp3"
		contentType = minimaxTTSSupportedFormats["mp3"]
	}
	return &MiniMaxTTSResult{
		Audio:           audio,
		Format:          format,
		ContentType:     contentType,
		UsageCharacters: gjson.GetBytes(respBody, "extra_info.usage_characters").Int(),
		TraceID:         strings.TrimSpace(gjson.GetBytes(respBody, "trace_id").String()),
	}, nil
}

// minimaxSTTBillingSeconds extracts the billed audio duration from an STT
// response: json body `duration`, or the terminating SSE event `duration`.
func minimaxSTTBillingSeconds(contentType string, respBody []byte, reqBodyLen int, elapsed time.Duration) float64 {
	secs := 0.0
	if gjson.ValidBytes(respBody) {
		if value := gjson.GetBytes(respBody, "duration"); value.Exists() && value.Type == gjson.Number && value.Float() > 0 {
			secs = value.Float()
		}
	}
	if secs <= 0 && strings.Contains(contentType, "text/event-stream") {
		for _, line := range strings.Split(string(respBody), "\n") {
			line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "data:"))
			if !gjson.Valid(line) {
				continue
			}
			if gjson.Get(line, "finish").Bool() {
				if value := gjson.Get(line, "duration"); value.Exists() && value.Type == gjson.Number && value.Float() > 0 {
					secs = value.Float()
				}
			}
		}
	}
	if secs <= 0 && reqBodyLen > 0 {
		// Compressed-speech lower bound (~16KB/s), same heuristic as the grok path.
		secs = float64(reqBodyLen) / 16000.0
	}
	if secs <= 0 {
		secs = elapsed.Seconds()
	}
	return secs
}

// ForwardMiniMaxVoice handles TTS, STT, and custom-voices endpoints for MiniMax.
func (s *OpenAIGatewayService) ForwardMiniMaxVoice(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	endpoint string,
	body []byte,
	contentType string,
) (*OpenAIForwardResult, error) {
	startTime := time.Now()
	if s == nil || account == nil {
		return nil, fmt.Errorf("minimax voice service/account is required")
	}
	if account.Platform != PlatformMiniMax {
		return nil, fmt.Errorf("account platform %s is not supported for minimax voice", account.Platform)
	}
	endpoint = strings.Trim(strings.TrimSpace(endpoint), "/")
	parts := strings.Split(endpoint, "/")
	baseEndpoint := parts[0]
	switch baseEndpoint {
	case MiniMaxVoiceEndpointTTS, MiniMaxVoiceEndpointSTT:
		if len(parts) > 1 {
			return nil, fmt.Errorf("unsupported minimax voice endpoint: %s", endpoint)
		}
	case MiniMaxVoiceEndpointCustomVoices:
		if len(parts) > 3 || (len(parts) == 3 && parts[2] != "audio") {
			return nil, fmt.Errorf("unsupported minimax voice endpoint: %s", endpoint)
		}
		for _, part := range parts[1:] {
			if part == "" || part == "." || part == ".." || strings.ContainsAny(part, "?#\\") {
				return nil, fmt.Errorf("invalid minimax voice endpoint path")
			}
		}
	default:
		return nil, fmt.Errorf("unsupported minimax voice endpoint: %s", endpoint)
	}
	token, _, err := s.getRequestCredential(ctx, c, account)
	if err != nil {
		return nil, err
	}

	switch baseEndpoint {
	case MiniMaxVoiceEndpointTTS:
		return s.forwardMiniMaxTTS(ctx, c, account, token, body, startTime)
	case MiniMaxVoiceEndpointSTT:
		return s.forwardMiniMaxSTT(ctx, c, account, token, body, contentType, startTime)
	default:
		return s.forwardMiniMaxCustomVoices(ctx, c, account, token, parts, body, contentType, startTime)
	}
}

// doMiniMaxVoiceRequest issues one upstream call with the shared transport.
func (s *OpenAIGatewayService) doMiniMaxVoiceRequest(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	token, method, targetURL string,
	bodyReader io.Reader,
	contentType string,
	extraHeaders map[string]string,
) (*http.Response, error) {
	upstreamCtx, release := detachUpstreamContext(ctx)
	defer release()
	req, err := http.NewRequestWithContext(upstreamCtx, method, targetURL, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for key, value := range extraHeaders {
		req.Header.Set(key, value)
	}
	account.ApplyHeaderOverrides(req.Header)
	proxyURL, egressErr := account.EgressProxyURL(ctx)
	if egressErr != nil {
		return nil, egressErr
	}
	upstreamStart := time.Now()
	resp, err := generationDo(accountHTTPUpstream(s.httpUpstream, account), req, proxyURL, account.ID, account.Concurrency)
	SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(upstreamStart).Milliseconds())
	if err != nil {
		return nil, s.handleOpenAIUpstreamTransportError(ctx, c, account, err, false)
	}
	return resp, nil
}

func (s *OpenAIGatewayService) forwardMiniMaxTTS(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	token string,
	body []byte,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	requestModel := strings.TrimSpace(gjson.GetBytes(body, "model").String())
	upstreamModel := requestModel
	if mapped := strings.TrimSpace(account.GetMappedModel(requestModel)); mapped != "" {
		upstreamModel = mapped
	}
	upstreamBody, err := buildMiniMaxTTSBody(body, upstreamModel)
	if err != nil {
		writeMiniMaxActionErrorResponse(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return nil, err
	}
	targetURL := buildMiniMaxActionURL(account, "/v1/t2a_v2")
	resp, err := s.doMiniMaxVoiceRequest(ctx, c, account, token, http.MethodPost, targetURL, bytes.NewReader(upstreamBody), "application/json", map[string]string{"Accept": "application/json"})
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	requestIDHeader := resp.Header.Get("x-request-id")
	if resp.StatusCode >= 400 {
		return s.handleMiniMaxActionErrorResponse(ctx, resp, c, account, requestIDHeader)
	}
	respBody, err := ReadUpstreamResponseBody(resp.Body, s.cfg, c, openAITooLargeError)
	if err != nil {
		return nil, err
	}
	decoded, err := decodeMiniMaxTTSResponse(respBody)
	if err != nil {
		var businessErr *MiniMaxBusinessError
		if errors.As(err, &businessErr) {
			if businessErr.failoverable() {
				return nil, &UpstreamFailoverError{StatusCode: businessErr.httpStatus(), ResponseBody: respBody, ResponseHeaders: resp.Header.Clone()}
			}
			writeMiniMaxActionBusinessError(c, businessErr)
			return nil, businessErr
		}
		setOpsUpstreamError(c, http.StatusBadGateway, "MiniMax tts returned no usable audio", truncateString(string(respBody), 512))
		return nil, &UpstreamFailoverError{StatusCode: http.StatusBadGateway, ResponseBody: respBody, ResponseHeaders: resp.Header.Clone()}
	}
	if c != nil && c.Writer != nil && !c.Writer.Written() {
		c.Data(http.StatusOK, decoded.ContentType, decoded.Audio)
	}
	var audioUsage *AudioUsage
	if decoded.UsageCharacters > 0 {
		audioUsage = &AudioUsage{Mode: "tts", DurationOrUnits: float64(decoded.UsageCharacters) / 1_000_000.0}
	}
	return &OpenAIForwardResult{
		RequestID:     StableMiniMaxAudioBillingRequestID(firstNonEmpty(requestIDHeader, decoded.TraceID)),
		Model:         firstNonEmpty(requestModel, upstreamModel),
		BillingModel:  firstNonEmpty(requestModel, upstreamModel),
		UpstreamModel: upstreamModel,
		Duration:      time.Since(startTime),
		AudioUsage:    audioUsage,
	}, nil
}

// buildMiniMaxSTTMultipart rewrites the inbound OpenAI transcriptions multipart
// body for /v1/speech_to_text: file + model/response_format/stream/timestamp_level
// fields pass through; `language` moves to a header (returned separately).
func buildMiniMaxSTTMultipart(contentType string, body []byte, model string) ([]byte, string, string, error) {
	mediaType, params, err := mime.ParseMediaType(strings.TrimSpace(contentType))
	if err != nil || !strings.EqualFold(mediaType, "multipart/form-data") {
		return nil, "", "", fmt.Errorf("request body must be multipart/form-data")
	}
	boundary := strings.TrimSpace(params["boundary"])
	if boundary == "" {
		return nil, "", "", fmt.Errorf("multipart boundary is missing")
	}
	var fileName, fileContentType string
	var fileData []byte
	fields := map[string]string{}
	reader := multipart.NewReader(bytes.NewReader(body), boundary)
	for {
		part, err := reader.NextPart()
		if err != nil {
			break
		}
		name := strings.TrimSpace(part.FormName())
		if name == "" {
			_ = part.Close()
			continue
		}
		data, err := io.ReadAll(io.LimitReader(part, openAIImageMaxUploadPartSize))
		_ = part.Close()
		if err != nil {
			return nil, "", "", err
		}
		if partFileName := strings.TrimSpace(part.FileName()); partFileName != "" {
			if name == "file" && fileData == nil {
				fileName = partFileName
				fileContentType = strings.TrimSpace(part.Header.Get("Content-Type"))
				fileData = data
			}
			continue
		}
		fields[name] = strings.TrimSpace(string(data))
	}
	if len(fileData) == 0 {
		return nil, "", "", fmt.Errorf("file is required")
	}
	var out bytes.Buffer
	writer := multipart.NewWriter(&out)
	if model == "" {
		model = "asr-1.0"
	}
	_ = writer.WriteField("model", model)
	responseFormat := fields["response_format"]
	if responseFormat != "" {
		_ = writer.WriteField("response_format", responseFormat)
	}
	if stream := fields["stream"]; stream != "" {
		_ = writer.WriteField("stream", stream)
	}
	if timestampLevel := fields["timestamp_level"]; timestampLevel != "" {
		_ = writer.WriteField("timestamp_level", timestampLevel)
	}
	partHeader := minimaxMultipartFileHeader(fileName, fileContentType)
	fileWriter, err := writer.CreatePart(partHeader)
	if err != nil {
		return nil, "", "", err
	}
	if _, err := fileWriter.Write(fileData); err != nil {
		return nil, "", "", err
	}
	if err := writer.Close(); err != nil {
		return nil, "", "", err
	}
	return out.Bytes(), writer.FormDataContentType(), fields["language"], nil
}

func minimaxMultipartFileHeader(fileName, contentType string) textproto.MIMEHeader {
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, strings.ReplaceAll(fileName, `"`, `\"`)))
	header.Set("Content-Type", contentType)
	return header
}

// minimaxSTTRequestModel extracts the optional model form field from an inbound
// OpenAI transcriptions multipart body (gjson cannot see multipart fields).
func minimaxSTTRequestModel(contentType string, body []byte) string {
	mediaType, params, err := mime.ParseMediaType(strings.TrimSpace(contentType))
	if err != nil || !strings.EqualFold(mediaType, "multipart/form-data") {
		return ""
	}
	boundary := strings.TrimSpace(params["boundary"])
	if boundary == "" {
		return ""
	}
	reader := multipart.NewReader(bytes.NewReader(body), boundary)
	for {
		part, err := reader.NextPart()
		if err != nil {
			return ""
		}
		if strings.TrimSpace(part.FormName()) != "model" {
			_ = part.Close()
			continue
		}
		data, _ := io.ReadAll(io.LimitReader(part, 1<<10))
		_ = part.Close()
		return strings.TrimSpace(string(data))
	}
}

func (s *OpenAIGatewayService) forwardMiniMaxSTT(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	token string,
	body []byte,
	contentType string,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	requestModel := minimaxSTTRequestModel(contentType, body)
	upstreamModel := requestModel
	if mapped := strings.TrimSpace(account.GetMappedModel(requestModel)); mapped != "" {
		upstreamModel = mapped
	}
	upstreamBody, upstreamContentType, language, err := buildMiniMaxSTTMultipart(contentType, body, upstreamModel)
	if err != nil {
		writeMiniMaxActionErrorResponse(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return nil, err
	}
	extraHeaders := map[string]string{"Accept": "application/json, text/event-stream, text/plain, text/vtt"}
	if language != "" {
		extraHeaders["language"] = language
	}
	targetURL := buildMiniMaxActionURL(account, "/v1/speech_to_text")
	resp, err := s.doMiniMaxVoiceRequest(ctx, c, account, token, http.MethodPost, targetURL, bytes.NewReader(upstreamBody), upstreamContentType, extraHeaders)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	requestIDHeader := resp.Header.Get("x-request-id")
	if resp.StatusCode >= 400 {
		return s.handleMiniMaxActionErrorResponse(ctx, resp, c, account, requestIDHeader)
	}
	respBody, err := ReadUpstreamResponseBody(resp.Body, s.cfg, c, openAITooLargeError)
	if err != nil {
		return nil, err
	}
	elapsed := time.Since(startTime)
	respContentType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if respContentType == "" {
		respContentType = "application/json"
	}
	if c != nil && c.Writer != nil && !c.Writer.Written() {
		c.Data(http.StatusOK, respContentType, respBody)
	}
	seconds := minimaxSTTBillingSeconds(respContentType, respBody, len(upstreamBody), elapsed)
	var audioUsage *AudioUsage
	if seconds > 0 {
		audioUsage = &AudioUsage{Mode: "stt", DurationOrUnits: seconds / 3600.0}
	}
	return &OpenAIForwardResult{
		RequestID:     StableMiniMaxAudioBillingRequestID(requestIDHeader),
		Model:         firstNonEmpty(requestModel, upstreamModel),
		BillingModel:  firstNonEmpty(requestModel, upstreamModel),
		UpstreamModel: upstreamModel,
		Duration:      elapsed,
		AudioUsage:    audioUsage,
	}, nil
}

// ---------------------------------------------------------------------------
// Custom voices: files/upload + voice_clone + get_voice + delete_voice
// ---------------------------------------------------------------------------

// minimaxGeneratedVoiceID follows the official voice_id rule: [8,256] chars,
// letter-first, letters/digits/-/_ allowed, no trailing -/_.
func minimaxGeneratedVoiceID() string {
	var randomBytes [10]byte
	if _, err := cryptorand.Read(randomBytes[:]); err != nil {
		return fmt.Sprintf("mmcv%d", time.Now().UnixNano())
	}
	return "mmcv" + hex.EncodeToString(randomBytes[:])
}

// forwardMiniMaxCustomVoices bridges the grok-dialect custom-voices routes onto
// the official voice-clone management APIs.
func (s *OpenAIGatewayService) forwardMiniMaxCustomVoices(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	token string,
	parts []string,
	body []byte,
	contentType string,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	method := http.MethodPost
	if c != nil && c.Request != nil && strings.TrimSpace(c.Request.Method) != "" {
		method = c.Request.Method
	}
	voiceID := ""
	if len(parts) == 2 {
		voiceID = strings.TrimSpace(parts[1])
	}
	if len(parts) == 3 && parts[2] == "audio" {
		// Official docs expose no voice-sample download endpoint.
		err := fmt.Errorf("voice audio download is not available on MiniMax")
		writeMiniMaxActionErrorResponse(c, http.StatusBadRequest, "action_not_implemented", err.Error())
		return nil, err
	}

	switch {
	case method == http.MethodPost && len(parts) == 1:
		return s.forwardMiniMaxVoiceCloneCreate(ctx, c, account, token, body, contentType, startTime)
	case method == http.MethodGet && len(parts) == 1:
		return s.forwardMiniMaxVoiceList(ctx, c, account, token, "", startTime)
	case method == http.MethodGet && len(parts) == 2:
		return s.forwardMiniMaxVoiceList(ctx, c, account, token, voiceID, startTime)
	case method == http.MethodDelete && len(parts) == 2:
		return s.forwardMiniMaxVoiceDelete(ctx, c, account, token, voiceID, startTime)
	default:
		err := fmt.Errorf("unsupported minimax custom-voices operation")
		writeMiniMaxActionErrorResponse(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return nil, err
	}
}

// minimaxUploadVoiceFile performs POST /v1/files/upload and returns the file_id.
func (s *OpenAIGatewayService) minimaxUploadVoiceFile(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	token, purpose, fileName, fileContentType string,
	fileData []byte,
) (int64, error) {
	var out bytes.Buffer
	writer := multipart.NewWriter(&out)
	if err := writer.WriteField("purpose", purpose); err != nil {
		return 0, err
	}
	partHeader := minimaxMultipartFileHeader(fileName, fileContentType)
	fileWriter, err := writer.CreatePart(partHeader)
	if err != nil {
		return 0, err
	}
	if _, err := fileWriter.Write(fileData); err != nil {
		return 0, err
	}
	if err := writer.Close(); err != nil {
		return 0, err
	}
	targetURL := buildMiniMaxActionURL(account, "/v1/files/upload")
	resp, err := s.doMiniMaxVoiceRequest(ctx, c, account, token, http.MethodPost, targetURL, bytes.NewReader(out.Bytes()), writer.FormDataContentType(), map[string]string{"Accept": "application/json"})
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		// handleMiniMaxActionErrorResponse always reports a non-nil error, so it is
		// propagated directly here like every other caller of this helper.
		_, forwardErr := s.handleMiniMaxActionErrorResponse(ctx, resp, c, account, resp.Header.Get("x-request-id"))
		return 0, forwardErr
	}
	respBody, err := ReadUpstreamResponseBody(resp.Body, s.cfg, c, openAITooLargeError)
	if err != nil {
		return 0, err
	}
	if businessErr := extractMiniMaxBusinessError(respBody); businessErr != nil {
		if businessErr.failoverable() {
			return 0, &UpstreamFailoverError{StatusCode: businessErr.httpStatus(), ResponseBody: respBody, ResponseHeaders: resp.Header.Clone()}
		}
		writeMiniMaxActionBusinessError(c, businessErr)
		return 0, businessErr
	}
	fileID := gjson.GetBytes(respBody, "file.file_id").Int()
	if fileID <= 0 {
		return 0, &UpstreamFailoverError{StatusCode: http.StatusBadGateway, ResponseBody: respBody, ResponseHeaders: resp.Header.Clone()}
	}
	return fileID, nil
}

// forwardMiniMaxVoiceCloneCreate: multipart audio in -> upload -> voice_clone -> voice_id out.
func (s *OpenAIGatewayService) forwardMiniMaxVoiceCloneCreate(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	token string,
	body []byte,
	contentType string,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	fileName, fileContentType, fileData, fields, err := parseMiniMaxVoiceCloneUpload(contentType, body)
	if err != nil {
		writeMiniMaxActionErrorResponse(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return nil, err
	}
	voiceID := strings.TrimSpace(firstNonEmpty(fields["voice_id"], fields["name"]))
	if voiceID == "" {
		voiceID = minimaxGeneratedVoiceID()
	}
	fileID, err := s.minimaxUploadVoiceFile(ctx, c, account, token, "voice_clone", fileName, fileContentType, fileData)
	if err != nil {
		return nil, err
	}
	cloneBody, err := marshalOpenAIUpstreamJSON(map[string]any{
		"file_id":  fileID,
		"voice_id": voiceID,
	})
	if err != nil {
		return nil, err
	}
	targetURL := buildMiniMaxActionURL(account, "/v1/voice_clone")
	resp, err := s.doMiniMaxVoiceRequest(ctx, c, account, token, http.MethodPost, targetURL, bytes.NewReader(cloneBody), "application/json", map[string]string{"Accept": "application/json"})
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	requestIDHeader := resp.Header.Get("x-request-id")
	if resp.StatusCode >= 400 {
		return s.handleMiniMaxActionErrorResponse(ctx, resp, c, account, requestIDHeader)
	}
	respBody, err := ReadUpstreamResponseBody(resp.Body, s.cfg, c, openAITooLargeError)
	if err != nil {
		return nil, err
	}
	if businessErr := extractMiniMaxBusinessError(respBody); businessErr != nil {
		if businessErr.failoverable() {
			return nil, &UpstreamFailoverError{StatusCode: businessErr.httpStatus(), ResponseBody: respBody, ResponseHeaders: resp.Header.Clone()}
		}
		writeMiniMaxActionBusinessError(c, businessErr)
		return nil, businessErr
	}
	out, err := marshalOpenAIUpstreamJSON(map[string]any{
		"voice_id": voiceID,
		"file_id":  fileID,
	})
	if err != nil {
		return nil, err
	}
	writeMiniMaxActionJSONResponse(c, http.StatusOK, out)
	return &OpenAIForwardResult{
		RequestID: StableMiniMaxAudioBillingRequestID(requestIDHeader),
		Model:     "voice_clone",
		Duration:  time.Since(startTime),
		// Voice clone creation carries no gateway audio units (group pricing has
		// no voice-clone rate; matches the grok custom-voices precedent).
	}, nil
}

// forwardMiniMaxVoiceList implements list (voiceID == "") and single-voice lookup
// through POST /v1/get_voice with voice_type=voice_cloning.
func (s *OpenAIGatewayService) forwardMiniMaxVoiceList(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	token, voiceID string,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	reqBody, err := marshalOpenAIUpstreamJSON(map[string]any{"voice_type": "voice_cloning"})
	if err != nil {
		return nil, err
	}
	targetURL := buildMiniMaxActionURL(account, "/v1/get_voice")
	resp, err := s.doMiniMaxVoiceRequest(ctx, c, account, token, http.MethodPost, targetURL, bytes.NewReader(reqBody), "application/json", map[string]string{"Accept": "application/json"})
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	requestIDHeader := resp.Header.Get("x-request-id")
	if resp.StatusCode >= 400 {
		return s.handleMiniMaxActionErrorResponse(ctx, resp, c, account, requestIDHeader)
	}
	respBody, err := ReadUpstreamResponseBody(resp.Body, s.cfg, c, openAITooLargeError)
	if err != nil {
		return nil, err
	}
	if businessErr := extractMiniMaxBusinessError(respBody); businessErr != nil {
		if businessErr.failoverable() {
			return nil, &UpstreamFailoverError{StatusCode: businessErr.httpStatus(), ResponseBody: respBody, ResponseHeaders: resp.Header.Clone()}
		}
		writeMiniMaxActionBusinessError(c, businessErr)
		return nil, businessErr
	}
	voices := make([]map[string]any, 0, 8)
	matched := false
	for _, entry := range gjson.GetBytes(respBody, "voice_cloning").Array() {
		entryVoiceID := strings.TrimSpace(entry.Get("voice_id").String())
		if entryVoiceID == "" {
			continue
		}
		if voiceID != "" && entryVoiceID != voiceID {
			continue
		}
		matched = true
		voices = append(voices, map[string]any{
			"voice_id":     entryVoiceID,
			"created_time": strings.TrimSpace(entry.Get("created_time").String()),
		})
	}
	if voiceID != "" && !matched {
		err := fmt.Errorf("voice %s not found", voiceID)
		writeMiniMaxActionErrorResponse(c, http.StatusNotFound, "not_found_error", err.Error())
		return nil, err
	}
	var out []byte
	if voiceID != "" && len(voices) == 1 {
		out, err = marshalOpenAIUpstreamJSON(voices[0])
	} else {
		out, err = marshalOpenAIUpstreamJSON(map[string]any{"data": voices})
	}
	if err != nil {
		return nil, err
	}
	writeMiniMaxActionJSONResponse(c, http.StatusOK, out)
	return &OpenAIForwardResult{
		RequestID: StableMiniMaxAudioBillingRequestID(requestIDHeader),
		Model:     "get_voice",
		Duration:  time.Since(startTime),
	}, nil
}

func (s *OpenAIGatewayService) forwardMiniMaxVoiceDelete(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	token, voiceID string,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	if strings.TrimSpace(voiceID) == "" {
		err := fmt.Errorf("voice_id is required")
		writeMiniMaxActionErrorResponse(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return nil, err
	}
	reqBody, err := marshalOpenAIUpstreamJSON(map[string]any{
		"voice_type": "voice_cloning",
		"voice_id":   voiceID,
	})
	if err != nil {
		return nil, err
	}
	targetURL := buildMiniMaxActionURL(account, "/v1/delete_voice")
	resp, err := s.doMiniMaxVoiceRequest(ctx, c, account, token, http.MethodPost, targetURL, bytes.NewReader(reqBody), "application/json", map[string]string{"Accept": "application/json"})
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	requestIDHeader := resp.Header.Get("x-request-id")
	if resp.StatusCode >= 400 {
		return s.handleMiniMaxActionErrorResponse(ctx, resp, c, account, requestIDHeader)
	}
	respBody, err := ReadUpstreamResponseBody(resp.Body, s.cfg, c, openAITooLargeError)
	if err != nil {
		return nil, err
	}
	if businessErr := extractMiniMaxBusinessError(respBody); businessErr != nil {
		if businessErr.failoverable() {
			return nil, &UpstreamFailoverError{StatusCode: businessErr.httpStatus(), ResponseBody: respBody, ResponseHeaders: resp.Header.Clone()}
		}
		writeMiniMaxActionBusinessError(c, businessErr)
		return nil, businessErr
	}
	out, err := marshalOpenAIUpstreamJSON(map[string]any{"voice_id": voiceID, "deleted": true})
	if err != nil {
		return nil, err
	}
	writeMiniMaxActionJSONResponse(c, http.StatusOK, out)
	return &OpenAIForwardResult{
		RequestID: StableMiniMaxAudioBillingRequestID(requestIDHeader),
		Model:     "delete_voice",
		Duration:  time.Since(startTime),
	}, nil
}

// parseMiniMaxVoiceCloneUpload extracts the first audio file and scalar fields
// from the inbound custom-voices multipart body.
func parseMiniMaxVoiceCloneUpload(contentType string, body []byte) (string, string, []byte, map[string]string, error) {
	fields := map[string]string{}
	mediaType, params, err := mime.ParseMediaType(strings.TrimSpace(contentType))
	if err != nil || !strings.EqualFold(mediaType, "multipart/form-data") {
		return "", "", nil, fields, fmt.Errorf("request body must be multipart/form-data")
	}
	boundary := strings.TrimSpace(params["boundary"])
	if boundary == "" {
		return "", "", nil, fields, fmt.Errorf("multipart boundary is missing")
	}
	var fileName, fileContentType string
	var fileData []byte
	reader := multipart.NewReader(bytes.NewReader(body), boundary)
	for {
		part, err := reader.NextPart()
		if err != nil {
			break
		}
		name := strings.TrimSpace(part.FormName())
		if name == "" {
			_ = part.Close()
			continue
		}
		data, err := io.ReadAll(io.LimitReader(part, openAIImageMaxUploadPartSize))
		_ = part.Close()
		if err != nil {
			return "", "", nil, fields, err
		}
		if partFileName := strings.TrimSpace(part.FileName()); partFileName != "" {
			if fileData == nil {
				fileName = partFileName
				fileContentType = strings.TrimSpace(part.Header.Get("Content-Type"))
				fileData = data
			}
			continue
		}
		fields[name] = strings.TrimSpace(string(data))
	}
	if len(fileData) == 0 {
		return "", "", nil, fields, fmt.Errorf("audio file is required")
	}
	return fileName, fileContentType, fileData, fields, nil
}
