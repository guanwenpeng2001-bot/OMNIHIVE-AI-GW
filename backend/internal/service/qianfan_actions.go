package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Qianfan native actions (Baidu ModelBuilder / 百度智能云千帆).
//
// First-party wire shapes verified against the official Qianfan API reference
// (cloud.baidu.com/doc/qianfan-api, fetched 2026-09-21) and live unauthenticated
// endpoint probes on the same date; see the worker evidence log. Qianfan mixes
// three credential surfaces, so the action table never reuses one header for
// all paths:
//   - ModelBuilder data plane (qianfan.baidubce.com): `Authorization: Bearer <API Key>`
//   - Baidu AI Search (…/v2/ai_search/*): `X-Appbuilder-Authorization: Bearer <AppBuilder Key>`
//   - Baidu speech products (tsn.baidu.com TTS, aip.baidubce.com voice clone):
//     their own hosts and their own IAM token, never the ModelBuilder path.
type QianfanActionEndpoint string

const (
	QianfanEndpointEmbeddings        QianfanActionEndpoint = "embeddings"
	QianfanEndpointRerank            QianfanActionEndpoint = "rerank"
	QianfanEndpointImagesGenerations QianfanActionEndpoint = "images/generations"
	QianfanEndpointImagesEdits       QianfanActionEndpoint = "images/edits"
	QianfanEndpointResponses         QianfanActionEndpoint = "responses"
	QianfanEndpointSearchWebSearch   QianfanActionEndpoint = "ai_search/web_search"
	QianfanEndpointVideoGenerate     QianfanActionEndpoint = "video/generations"
	QianfanEndpointVideoStatus       QianfanActionEndpoint = "video/status"
	QianfanEndpointVideoCancel       QianfanActionEndpoint = "video/cancel"
	QianfanEndpointVideoContent      QianfanActionEndpoint = "video/content"
	QianfanEndpointAudioSpeech       QianfanActionEndpoint = "audio/speech"
	QianfanEndpointVoiceCloneCreate  QianfanActionEndpoint = "voice/clone/create"
	QianfanEndpointVoiceCloneList    QianfanActionEndpoint = "voice/clone/list"
)

// Fixed first-party product hosts. These Baidu speech endpoints are not served
// under the ModelBuilder base URL, so an account base_url override must not
// silently redirect them to a relay path that cannot serve the action.
const (
	qianfanSpeechTTSURL          = "https://tsn.baidu.com/text2audio"
	qianfanSpeechAIVoiceBaseURL  = "https://aip.baidubce.com/rest/2.0/speech/publiccloudspeech/v1"
	qianfanAppBuilderAuthHeader  = "X-Appbuilder-Authorization"
	qianfanVideoGenerationsPath  = "/video/generations"
	qianfanSpeechTTSDefaultVoice = "0"
)

// QianfanUpstreamHost groups endpoints that share one credential surface.
type QianfanUpstreamHost string

const (
	QianfanHostModelBuilder QianfanUpstreamHost = "modelbuilder"
	QianfanHostSpeechTTS    QianfanUpstreamHost = "speech-tts"
	QianfanHostSpeechAI     QianfanUpstreamHost = "speech-ai"
)

// UpstreamHost reports which first-party credential surface serves the action.
func (e QianfanActionEndpoint) UpstreamHost() QianfanUpstreamHost {
	switch e {
	case QianfanEndpointAudioSpeech:
		return QianfanHostSpeechTTS
	case QianfanEndpointVoiceCloneCreate, QianfanEndpointVoiceCloneList:
		return QianfanHostSpeechAI
	default:
		return QianfanHostModelBuilder
	}
}

// RequiresRequestBody reports whether the action carries a client body.
func (e QianfanActionEndpoint) RequiresRequestBody() bool {
	switch e {
	case QianfanEndpointVideoStatus, QianfanEndpointVideoContent, QianfanEndpointVideoCancel, QianfanEndpointVoiceCloneList:
		return false
	default:
		return true
	}
}

func (e QianfanActionEndpoint) httpMethod() string {
	switch e {
	case QianfanEndpointVideoStatus, QianfanEndpointVideoContent:
		return http.MethodGet
	case QianfanEndpointVideoCancel:
		return http.MethodDelete
	default:
		return http.MethodPost
	}
}

// UsesAppBuilderAuth reports actions served by the Baidu AI Search surface,
// which authenticates through X-Appbuilder-Authorization rather than the
// ModelBuilder Authorization header.
func (e QianfanActionEndpoint) UsesAppBuilderAuth() bool {
	return e == QianfanEndpointSearchWebSearch
}

// PlanProtocol labels the request-plan protocol dimension for the endpoint.
func (e QianfanActionEndpoint) PlanProtocol() string {
	switch e {
	case QianfanEndpointEmbeddings:
		return "embeddings"
	case QianfanEndpointRerank:
		return "rerank"
	case QianfanEndpointImagesGenerations, QianfanEndpointImagesEdits,
		QianfanEndpointVideoGenerate, QianfanEndpointVideoStatus,
		QianfanEndpointVideoContent, QianfanEndpointVideoCancel:
		return "media"
	case QianfanEndpointAudioSpeech, QianfanEndpointVoiceCloneCreate, QianfanEndpointVoiceCloneList:
		return "audio"
	case QianfanEndpointSearchWebSearch:
		return "search"
	default:
		return "media"
	}
}

// IsVideoCreateRequest reports async video task submission; billing is deferred
// until a status/content poll observes the succeeded task.
func (e QianfanActionEndpoint) IsVideoCreateRequest() bool {
	return e == QianfanEndpointVideoGenerate
}

// IsVideoLookupRequest covers owner-bound task queries and cancellation.
func (e QianfanActionEndpoint) IsVideoLookupRequest() bool {
	switch e {
	case QianfanEndpointVideoStatus, QianfanEndpointVideoCancel:
		return true
	default:
		return false
	}
}

// IsGenerationRequest covers client-billable generation submissions gated by
// the group image-generation permission (image + video create).
func (e QianfanActionEndpoint) IsGenerationRequest() bool {
	switch e {
	case QianfanEndpointImagesGenerations, QianfanEndpointImagesEdits, QianfanEndpointVideoGenerate:
		return true
	default:
		return false
	}
}

// QianfanActionBaseURL resolves the ModelBuilder base URL for an account. The
// existing qianfan adapter already owns URL-policy validation; this only adds
// the documentation-required v2 default.
func qianfanActionBaseURL(account *Account) string {
	if account == nil {
		return ""
	}
	base := strings.TrimSpace(account.GetOpenAIFormatBaseURL())
	if base == "" {
		base = qianfanDefaultBaseURL
	}
	return base
}

// buildQianfanActionURL resolves the concrete upstream URL for one action. The
// video endpoints live at the host root (/video/generations) rather than under
// /v2, so the origin is derived from the validated base URL.
func (s *OpenAIGatewayService) buildQianfanActionURL(account *Account, endpoint QianfanActionEndpoint, taskID string) (string, error) {
	if account == nil || account.Platform != PlatformQianfan {
		return "", fmt.Errorf("qianfan account is required")
	}
	switch endpoint.UpstreamHost() {
	case QianfanHostSpeechTTS:
		return qianfanSpeechTTSURL, nil
	case QianfanHostSpeechAI:
		path := strings.TrimPrefix(string(endpoint), "voice/clone/")
		if path == "" || path == string(endpoint) {
			return "", fmt.Errorf("qianfan voice endpoint is invalid")
		}
		return qianfanSpeechAIVoiceBaseURL + "/voice/clone/" + path, nil
	}
	base, err := s.validateUpstreamBaseURL(qianfanActionBaseURL(account))
	if err != nil {
		return "", err
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Host == "" {
		return "", fmt.Errorf("invalid qianfan base URL")
	}
	origin := parsed.Scheme + "://" + parsed.Host
	if endpoint == QianfanEndpointVideoGenerate || endpoint.IsVideoLookupRequest() {
		target := origin + qianfanVideoGenerationsPath
		if endpoint == QianfanEndpointVideoGenerate {
			return target, nil
		}
		taskID = strings.TrimSpace(taskID)
		if taskID == "" || strings.ContainsAny(taskID, "&?#") {
			return "", fmt.Errorf("qianfan video task id is invalid")
		}
		return target + "?task_id=" + url.QueryEscape(taskID), nil
	}
	return strings.TrimRight(base, "/") + "/" + string(endpoint), nil
}

// QianfanActionRequestInfo carries parsed inbound request facts used for
// routing, moderation, plan and billing snapshots.
type QianfanActionRequestInfo struct {
	Model           string
	Stream          bool
	Size            string
	InputText       string
	Input           []string
	HasInputImage   bool
	DurationSeconds int
	Voice           string
	ResponseFormat  string
	Speed           float64
}

// ModerationBody normalizes generation prompts for the content moderation
// extractor, mirroring the OpenAI images moderation shape.
func (r QianfanActionRequestInfo) ModerationBody() []byte {
	prompt := strings.TrimSpace(r.InputText)
	if prompt == "" {
		return nil
	}
	body, err := json.Marshal(map[string]any{"prompt": prompt})
	if err != nil {
		return nil
	}
	return body
}

// ParseQianfanActionRequest extracts request facts. Multipart bodies (image
// edits) only surface the small model form field; the raw body is forwarded
// untouched so file parts survive.
func ParseQianfanActionRequest(endpoint QianfanActionEndpoint, contentType string, body []byte) QianfanActionRequestInfo {
	info := QianfanActionRequestInfo{}
	if len(body) == 0 {
		return info
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(contentType)), "multipart/form-data") {
		if model := extractMultipartFormField(body, contentType, "model"); model != "" {
			info.Model = model
		}
		if prompt := extractMultipartFormField(body, contentType, "prompt"); prompt != "" {
			info.InputText = prompt
		}
		return info
	}
	if !gjson.ValidBytes(body) {
		return info
	}
	root := gjson.ParseBytes(body)
	info.Model = strings.TrimSpace(root.Get("model").String())
	info.Stream = root.Get("stream").Bool()
	info.Size = strings.TrimSpace(root.Get("size").String())
	switch endpoint {
	case QianfanEndpointImagesGenerations, QianfanEndpointImagesEdits:
		info.InputText = strings.TrimSpace(root.Get("prompt").String())
		if image := root.Get("image"); image.Exists() {
			info.HasInputImage = strings.TrimSpace(image.String()) != "" || image.IsArray() || image.IsObject()
		}
	case QianfanEndpointAudioSpeech:
		info.InputText = strings.TrimSpace(firstNonEmptyString(root.Get("input").String(), root.Get("tex").String()))
		info.Voice = strings.TrimSpace(firstNonEmptyString(root.Get("voice").String(), root.Get("per").String()))
		info.ResponseFormat = strings.TrimSpace(firstNonEmptyString(root.Get("response_format").String(), root.Get("format").String()))
		info.Speed = root.Get("speed").Float()
	case QianfanEndpointSearchWebSearch:
		if message := root.Get("messages.0.content"); message.Exists() {
			info.InputText = strings.TrimSpace(message.String())
		}
	case QianfanEndpointVideoGenerate:
		info.DurationSeconds = int(root.Get("duration").Int())
		root.Get("content").ForEach(func(_, part gjson.Result) bool {
			switch strings.TrimSpace(part.Get("type").String()) {
			case "text":
				if text := strings.TrimSpace(part.Get("text").String()); text != "" {
					info.InputText = text
				}
			case "image_url":
				info.HasInputImage = true
			}
			return true
		})
	case QianfanEndpointEmbeddings:
		input := root.Get("input")
		if input.Type == gjson.String {
			info.Input = []string{input.String()}
		} else {
			input.ForEach(func(_, item gjson.Result) bool {
				if item.Type == gjson.String {
					info.Input = append(info.Input, item.String())
				}
				return true
			})
		}
	case QianfanEndpointRerank:
		info.InputText = strings.TrimSpace(root.Get("query").String())
	}
	return info
}

// buildQianfanSpeechBody converts the gateway-facing speech synthesis request
// (OpenAI shape: input/voice/response_format/speed) into the Baidu TTS
// x-www-form-urlencoded body documented for tsn.baidu.com/text2audio.
func buildQianfanSpeechBody(info QianfanActionRequestInfo, requestID string) ([]byte, string, error) {
	text := strings.TrimSpace(info.InputText)
	if text == "" {
		return nil, "", fmt.Errorf("qianfan speech synthesis requires input text")
	}
	form := url.Values{}
	form.Set("tex", text)
	form.Set("cuid", firstNonEmptyString(strings.TrimSpace(requestID), "omnihive"))
	form.Set("ctp", "1")
	form.Set("lan", "zh")
	form.Set("per", firstNonEmptyString(strings.TrimSpace(info.Voice), qianfanSpeechTTSDefaultVoice))
	if audioFormat := qianfanSpeechAudioFormat(info.ResponseFormat); audioFormat != "" {
		form.Set("aue", audioFormat)
	}
	if info.Speed > 0 {
		form.Set("spd", qianfanSpeechSpeed(info.Speed))
	}
	return []byte(form.Encode()), "application/x-www-form-urlencoded", nil
}

// qianfanSpeechAudioFormat maps the client response_format onto Baidu's aue
// code documented for the short-text synthesis endpoint.
func qianfanSpeechAudioFormat(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "mp3":
		return "3"
	case "pcm":
		return "4"
	case "wav":
		return "6"
	default:
		return ""
	}
}

// qianfanSpeechSpeed maps the OpenAI speed multiplier onto Baidu's 0-15 range
// (5 = normal). The official default is 5, so out-of-range input stays normal.
func qianfanSpeechSpeed(speed float64) string {
	index := int(speed*5 + 0.5)
	if index < 0 {
		index = 0
	}
	if index > 15 {
		index = 15
	}
	return fmt.Sprintf("%d", index)
}

// errQianfanImageEditImageRequired marks a client fault: the OpenAI multipart
// image-edit shape was sent without any image part.
var errQianfanImageEditImageRequired = errors.New("qianfan image edit requires an image part")

// PrepareQianfanImageEditBody converts an OpenAI-style multipart image edit into
// the JSON body documented for POST /v2/images/edits (model, image, mask,
// prompt, n, size). Qianfan's edit endpoint is JSON-only: the documented image
// field takes a URL or a base64/data-URL string (official 通用图像编辑 doc,
// captured 2026-09-21), so a multipart client is bridged instead of being
// forwarded as a body the upstream cannot parse. JSON bodies and non-multipart
// content types are returned untouched.
func PrepareQianfanImageEditBody(body []byte, contentType string) ([]byte, string, error) {
	mediaType, params, err := mime.ParseMediaType(strings.TrimSpace(contentType))
	if err != nil || !strings.EqualFold(mediaType, "multipart/form-data") || strings.TrimSpace(params["boundary"]) == "" {
		return body, contentType, nil
	}
	info := ParseGrokMediaRequest(contentType, body)
	images := make([]string, 0, len(info.InputImageURLs)+len(info.Uploads))
	for _, imageURL := range info.InputImageURLs {
		if trimmed := strings.TrimSpace(imageURL); trimmed != "" {
			images = append(images, trimmed)
		}
	}
	for _, upload := range info.Uploads {
		dataURL, uploadErr := openAIImageUploadToDataURL(upload)
		if uploadErr != nil {
			return nil, "", uploadErr
		}
		images = append(images, dataURL)
	}
	if len(images) == 0 {
		return nil, "", errQianfanImageEditImageRequired
	}
	payload := map[string]any{}
	if model := strings.TrimSpace(info.Model); model != "" {
		payload["model"] = model
	}
	if prompt := strings.TrimSpace(info.Prompt); prompt != "" {
		payload["prompt"] = prompt
	}
	if size := strings.TrimSpace(info.Size); size != "" {
		payload["size"] = size
	}
	if info.N > 1 {
		payload["n"] = info.N
	}
	if len(images) == 1 {
		payload["image"] = images[0]
	} else {
		payload["image"] = images
	}
	if maskURL := strings.TrimSpace(info.MaskImageURL); maskURL != "" {
		payload["mask"] = maskURL
	}
	if info.MaskUpload != nil {
		dataURL, uploadErr := openAIImageUploadToDataURL(*info.MaskUpload)
		if uploadErr != nil {
			return nil, "", uploadErr
		}
		payload["mask"] = dataURL
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, "", err
	}
	return encoded, "application/json", nil
}

// ForwardQianfanAction forwards one Qianfan native action through the shared
// OpenAI-trunk account, error and billing policies. Responses are passed
// through: JSON for most actions, binary audio for speech synthesis and the
// video bytes for a completed video content download.
func (s *OpenAIGatewayService) ForwardQianfanAction(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	endpoint QianfanActionEndpoint,
	taskID string,
	body []byte,
	contentType string,
	rawQuery string,
) (*OpenAIForwardResult, error) {
	startTime := time.Now()
	if account == nil || account.Platform != PlatformQianfan {
		return nil, fmt.Errorf("qianfan account is required")
	}
	if endpoint == QianfanEndpointVideoContent {
		return s.forwardQianfanVideoContent(ctx, c, account, taskID, startTime)
	}
	token, _, err := s.getRequestCredential(ctx, c, account)
	if err != nil {
		return nil, err
	}
	targetURL, err := s.buildQianfanActionURL(account, endpoint, taskID)
	if err != nil {
		return nil, err
	}
	if !endpoint.RequiresRequestBody() && strings.TrimSpace(rawQuery) != "" {
		targetURL += "&" + strings.TrimPrefix(rawQuery, "?")
	}

	requestInfo := ParseQianfanActionRequest(endpoint, contentType, body)
	upstreamModel := requestInfo.Model
	var bodyReader io.Reader
	upstreamContentType := contentType
	if endpoint == QianfanEndpointAudioSpeech {
		formBody, formType, formErr := buildQianfanSpeechBody(requestInfo, generateRequestID())
		if formErr != nil {
			return nil, formErr
		}
		body, upstreamContentType = formBody, formType
		bodyReader = bytes.NewReader(body)
	} else {
		if endpoint == QianfanEndpointImagesEdits {
			convertedBody, convertedType, convertErr := PrepareQianfanImageEditBody(body, contentType)
			if convertErr != nil {
				writeChatCompletionsError(c, http.StatusBadRequest, "invalid_request_error", convertErr.Error())
				return nil, convertErr
			}
			body, upstreamContentType = convertedBody, convertedType
		}
		if endpoint.RequiresRequestBody() {
			if gjson.ValidBytes(body) && strings.TrimSpace(requestInfo.Model) != "" {
				if mapped := strings.TrimSpace(account.GetMappedModel(requestInfo.Model)); mapped != "" && mapped != requestInfo.Model {
					if rewritten, rewriteErr := sjson.SetBytes(body, "model", mapped); rewriteErr == nil {
						body = rewritten
						upstreamModel = mapped
					}
				}
			}
			bodyReader = bytes.NewReader(body)
		}
	}

	upstreamCtx, releaseUpstreamCtx := detachUpstreamContext(ctx)
	defer releaseUpstreamCtx()
	upstreamReq, err := http.NewRequestWithContext(upstreamCtx, endpoint.httpMethod(), targetURL, bodyReader)
	if err != nil {
		return nil, err
	}
	s.applyQianfanActionAuth(endpoint, upstreamReq.Header, token)
	upstreamReq.Header.Set("Accept", "application/json")
	if endpoint == QianfanEndpointAudioSpeech {
		upstreamReq.Header.Set("Accept", "audio/*")
	}
	if endpoint.RequiresRequestBody() {
		upstreamContentType = strings.TrimSpace(upstreamContentType)
		if upstreamContentType == "" {
			upstreamContentType = "application/json"
		}
		upstreamReq.Header.Set("Content-Type", upstreamContentType)
	}
	account.ApplyHeaderOverrides(upstreamReq.Header)
	// Freeze the action credential surface after custom headers so a header
	// override cannot silently swap the ModelBuilder and AppBuilder surfaces.
	s.applyQianfanActionAuth(endpoint, upstreamReq.Header, token)

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

	requestIDHeader := strings.TrimSpace(resp.Header.Get("x-request-id"))
	if resp.StatusCode >= 400 {
		return s.handleErrorResponse(ctx, resp, c, account, body, requestInfo.Model)
	}
	if endpoint == QianfanEndpointAudioSpeech {
		return s.forwardQianfanSpeechResponse(ctx, c, resp, requestInfo, startTime)
	}

	respBody, err := ReadUpstreamResponseBody(resp.Body, s.cfg, c, openAITooLargeError)
	if err != nil {
		return nil, err
	}
	if endpoint == QianfanEndpointImagesGenerations || endpoint == QianfanEndpointImagesEdits {
		if countOpenAIResponseImageOutputsFromJSONBytes(respBody) <= 0 {
			setOpsUpstreamError(c, http.StatusBadGateway, "qianfan upstream returned no image output", truncateString(string(respBody), 512))
			return nil, &UpstreamFailoverError{StatusCode: http.StatusBadGateway, ResponseBody: respBody, ResponseHeaders: resp.Header.Clone()}
		}
	}
	writeGrokMediaResponse(c, resp, respBody, s.responseHeaderFilter)

	result := &OpenAIForwardResult{
		RequestID:       requestIDHeader,
		UpstreamHeaders: resp.Header,
		Usage:           qianfanActionUsageFromResponse(endpoint, respBody),
		Model:           requestInfo.Model,
		BillingModel:    requestInfo.Model,
		UpstreamModel:   upstreamModel,
		ResponseHeaders: resp.Header.Clone(),
		Duration:        time.Since(startTime),
	}
	switch endpoint {
	case QianfanEndpointImagesGenerations, QianfanEndpointImagesEdits:
		result.ImageCount = countOpenAIResponseImageOutputsFromJSONBytes(respBody)
		result.ImageOutputSizes = collectOpenAIResponseImageOutputSizesFromJSONBytes(respBody)
	case QianfanEndpointVideoGenerate:
		result.ResponseID = qianfanVideoTaskID(respBody)
	case QianfanEndpointVideoStatus:
		result.ResponseID = strings.TrimSpace(taskID)
		if statusModel := strings.TrimSpace(gjson.GetBytes(respBody, "model").String()); statusModel != "" {
			result.Model = statusModel
			result.BillingModel = statusModel
		}
		if IsQianfanVideoTaskSuccess(respBody) {
			result.VideoCount = 1
			result.VideoResolution = qianfanVideoResolution(respBody)
			result.VideoDurationSeconds = int(gjson.GetBytes(respBody, "duration").Int())
		}
	case QianfanEndpointSearchWebSearch:
		if len(gjson.GetBytes(respBody, "references").Array()) > 0 || len(gjson.GetBytes(respBody, "choices").Array()) > 0 {
			result.WebSearchCalls = 1
		}
		if result.Model == "" {
			result.Model = "ai_search"
			result.BillingModel = result.Model
		}
	case QianfanEndpointVoiceCloneCreate:
		result.ResponseID = strings.TrimSpace(gjson.GetBytes(respBody, "voice_id").String())
	}
	return result, nil
}

// applyQianfanActionAuth writes the credential surface that belongs to the
// action. AppBuilder search must never receive the ModelBuilder Bearer header
// and vice versa, which is exactly the independent-auth constraint on Qianfan.
func (s *OpenAIGatewayService) applyQianfanActionAuth(endpoint QianfanActionEndpoint, header http.Header, token string) {
	if endpoint.UsesAppBuilderAuth() {
		header.Del("Authorization")
		header.Set(qianfanAppBuilderAuthHeader, "Bearer "+token)
		return
	}
	header.Del(qianfanAppBuilderAuthHeader)
	header.Set("Authorization", "Bearer "+token)
}

// qianfanActionUsageFromResponse builds token usage for the token-billed
// actions (embeddings, rerank); media actions price per unit instead.
func qianfanActionUsageFromResponse(endpoint QianfanActionEndpoint, responseBody []byte) OpenAIUsage {
	switch endpoint {
	case QianfanEndpointEmbeddings, QianfanEndpointRerank:
		return extractOpenAIEmbeddingsUsage(responseBody)
	default:
		return OpenAIUsage{}
	}
}

// --- Speech synthesis (tsn.baidu.com) ---

// qianfanSpeechReadCloser keeps the peeked bytes buffered while preserving the
// upstream body closer so the audio stream can be relayed unmodified.
type qianfanSpeechReadCloser struct {
	reader *bufio.Reader
	closer io.Closer
}

func (b *qianfanSpeechReadCloser) Read(p []byte) (int, error) { return b.reader.Read(p) }
func (b *qianfanSpeechReadCloser) Close() error               { return b.closer.Close() }

// forwardQianfanSpeechResponse relays the tsn.baidu.com synthesis result. The
// endpoint answers HTTP 200 with a JSON error envelope for IAM/quota failures
// and with a binary audio stream on success, so the body is peeked instead of
// trusting the status code.
func (s *OpenAIGatewayService) forwardQianfanSpeechResponse(
	ctx context.Context, c *gin.Context, resp *http.Response, info QianfanActionRequestInfo, startTime time.Time,
) (*OpenAIForwardResult, error) {
	buffered := &qianfanSpeechReadCloser{reader: bufio.NewReader(resp.Body), closer: resp.Body}
	resp.Body = buffered
	head, peekErr := buffered.reader.Peek(1)
	if peekErr == nil && len(head) == 1 && head[0] == '{' {
		body, err := ReadUpstreamResponseBody(buffered.reader, s.cfg, c, openAITooLargeError)
		if err != nil {
			return nil, err
		}
		setOpsUpstreamError(c, http.StatusBadGateway, "qianfan speech synthesis failed", truncateString(string(body), 512))
		_ = ctx
		return nil, &UpstreamFailoverError{StatusCode: http.StatusBadGateway, ResponseBody: body, ResponseHeaders: resp.Header.Clone()}
	}
	if err := writeGrokMediaContentResponse(c, resp); err != nil {
		return nil, err
	}
	result := &OpenAIForwardResult{
		Model:        firstNonEmptyString(info.Model, "qianfan-tts"),
		BillingModel: firstNonEmptyString(info.Model, "qianfan-tts"),
		AudioUsage:   &AudioUsage{Mode: "tts", DurationOrUnits: float64(len([]rune(strings.TrimSpace(info.InputText)))) / 1_000_000.0},
		Duration:     time.Since(startTime),
	}
	if result.AudioUsage.DurationOrUnits <= 0 {
		result.AudioUsage = nil
	}
	return result, nil
}

// --- Video task lifecycle ---

// qianfanVideoTaskID extracts task_id from the video create response.
func qianfanVideoTaskID(body []byte) string {
	for _, path := range []string{"task_id", "id"} {
		if value := strings.TrimSpace(gjson.GetBytes(body, path).String()); value != "" {
			return value
		}
	}
	return ""
}

// IsQianfanVideoTaskSuccess matches the documented terminal success shape:
// status == "succeeded" plus a downloadable content.video_url.
func IsQianfanVideoTaskSuccess(statusBody []byte) bool {
	if len(statusBody) == 0 || !gjson.ValidBytes(statusBody) {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(gjson.GetBytes(statusBody, "status").String()), "succeeded") {
		return false
	}
	return strings.TrimSpace(gjson.GetBytes(statusBody, "content.video_url").String()) != ""
}

// qianfanVideoResolution derives the shared billing resolution tier from the
// documented width/height fields.
func qianfanVideoResolution(statusBody []byte) string {
	width := int(gjson.GetBytes(statusBody, "width").Int())
	height := int(gjson.GetBytes(statusBody, "height").Int())
	if width <= 0 || height <= 0 {
		return ""
	}
	shortSide := width
	if height < shortSide {
		shortSide = height
	}
	switch {
	case shortSide <= 480:
		return VideoBillingResolution480P
	case shortSide <= 720:
		return VideoBillingResolution720P
	default:
		return VideoBillingResolution1080P
	}
}

// QianfanVideoPendingBilling is the create-time snapshot for deferred video
// billing: the status body carries no model, so the accepted create request is
// the billing evidence.
type QianfanVideoPendingBilling struct {
	Model                string `json:"model"`
	BillingModel         string `json:"billing_model,omitempty"`
	UpstreamModel        string `json:"upstream_model,omitempty"`
	VideoResolution      string `json:"video_resolution,omitempty"`
	VideoDurationSeconds int    `json:"video_duration_seconds,omitempty"`
	CreatedAt            string `json:"created_at,omitempty"`
}

func QianfanVideoPendingCreatedAtNow() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

// QianfanVideoTaskSessionHash scopes a video task binding to owner + key so a
// task id can never be resolved across tenants.
func QianfanVideoTaskSessionHash(taskID string, userID, apiKeyID int64) string {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" || userID <= 0 || apiKeyID <= 0 {
		return ""
	}
	return "qianfan-video:" + DeriveSessionHashFromSeed(fmt.Sprintf("%d:%d:%s", userID, apiKeyID, taskID))
}

func qianfanVideoPendingBillingKey(taskID string, userID, apiKeyID int64) string {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" || userID <= 0 || apiKeyID <= 0 {
		return ""
	}
	return fmt.Sprintf("%d:%d:%s", userID, apiKeyID, taskID)
}

func qianfanVideoPendingBillingTTL(cfg *config.Config) time.Duration {
	_ = cfg
	return 24 * time.Hour
}

func qianfanVideoBilledClaimTTL(cfg *config.Config) time.Duration {
	_ = cfg
	return 48 * time.Hour
}

// BindQianfanVideoTaskAccount records which upstream account accepted a video
// task; status/content polls resolve through this binding only.
func (s *OpenAIGatewayService) BindQianfanVideoTaskAccount(ctx context.Context, groupID *int64, taskID string, userID, apiKeyID, accountID int64) error {
	if s == nil || s.cache == nil {
		return fmt.Errorf("qianfan video task binding cache is unavailable")
	}
	cacheKey := s.openAISessionCacheKey(QianfanVideoTaskSessionHash(taskID, userID, apiKeyID))
	if cacheKey == "" || accountID <= 0 {
		return fmt.Errorf("qianfan video task binding is invalid")
	}
	ttl := qianfanVideoPendingBillingTTL(s.cfg)
	if s.cfg != nil && s.cfg.Gateway.OpenAIWS.StickySessionTTLSeconds > 0 {
		if sticky := time.Duration(s.cfg.Gateway.OpenAIWS.StickySessionTTLSeconds) * time.Second; sticky > ttl {
			ttl = sticky
		}
	}
	return s.cache.SetSessionAccountID(ctx, derefGroupID(groupID), cacheKey, accountID, ttl)
}

// ResolveQianfanVideoTaskAccount returns the bound account id for an owner task.
func (s *OpenAIGatewayService) ResolveQianfanVideoTaskAccount(ctx context.Context, groupID *int64, taskID string, userID, apiKeyID int64) (int64, error) {
	if s == nil || s.cache == nil {
		return 0, fmt.Errorf("qianfan video task binding cache is unavailable")
	}
	cacheKey := s.openAISessionCacheKey(QianfanVideoTaskSessionHash(taskID, userID, apiKeyID))
	if cacheKey == "" {
		return 0, fmt.Errorf("qianfan video task binding is invalid")
	}
	return s.cache.GetSessionAccountID(ctx, derefGroupID(groupID), cacheKey)
}

// SelectQianfanVideoTaskAccount admits only the already bound owner account.
func (s *OpenAIGatewayService) SelectQianfanVideoTaskAccount(
	ctx context.Context, groupID *int64, sessionHash string, accountID int64, requestedModel string,
) (*AccountSelectionResult, OpenAIAccountScheduleDecision, error) {
	decision := OpenAIAccountScheduleDecision{Layer: openAIAccountScheduleLayerSessionSticky}
	if accountID <= 0 || strings.TrimSpace(sessionHash) == "" {
		return nil, decision, ErrNoAvailableAccounts
	}
	ctx = s.withOpenAIGroupPrivacyRequirement(WithOpenAIProfitControlSuppressed(ctx), groupID)
	scheduler := &defaultOpenAIAccountScheduler{service: s}
	selection, _, err := scheduler.selectBySessionHash(ctx, OpenAIAccountScheduleRequest{
		GroupID: groupID, Platform: PlatformQianfan, SessionHash: sessionHash,
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

// StoreQianfanVideoPendingBilling persists create-time billing params so the
// deferred status poll can price a completed task.
func (s *OpenAIGatewayService) StoreQianfanVideoPendingBilling(ctx context.Context, taskID string, userID, apiKeyID int64, pending QianfanVideoPendingBilling) error {
	if s == nil || s.cache == nil {
		return fmt.Errorf("qianfan video pending billing cache is unavailable")
	}
	key := qianfanVideoPendingBillingKey(taskID, userID, apiKeyID)
	if key == "" {
		return fmt.Errorf("qianfan video pending billing key is invalid")
	}
	pending.Model = strings.TrimSpace(pending.Model)
	pending.BillingModel = strings.TrimSpace(pending.BillingModel)
	pending.UpstreamModel = strings.TrimSpace(pending.UpstreamModel)
	if pending.VideoResolution != "" {
		pending.VideoResolution = NormalizeVideoBillingResolutionOrDefault(pending.VideoResolution)
	}
	if pending.VideoDurationSeconds > 0 {
		pending.VideoDurationSeconds = NormalizeVideoBillingDurationSecondsOrDefault(pending.VideoDurationSeconds)
	}
	if strings.TrimSpace(pending.CreatedAt) == "" {
		pending.CreatedAt = QianfanVideoPendingCreatedAtNow()
	}
	payload, err := json.Marshal(pending)
	if err != nil {
		return err
	}
	return s.cache.SetGrokVideoPendingBilling(ctx, key, payload, qianfanVideoPendingBillingTTL(s.cfg))
}

// LoadQianfanVideoPendingBilling returns the create-time snapshot (nil on miss).
func (s *OpenAIGatewayService) LoadQianfanVideoPendingBilling(ctx context.Context, taskID string, userID, apiKeyID int64) (*QianfanVideoPendingBilling, error) {
	if s == nil || s.cache == nil {
		return nil, fmt.Errorf("qianfan video pending billing cache is unavailable")
	}
	key := qianfanVideoPendingBillingKey(taskID, userID, apiKeyID)
	if key == "" {
		return nil, fmt.Errorf("qianfan video pending billing key is invalid")
	}
	payload, err := s.cache.GetGrokVideoPendingBilling(ctx, key)
	if err != nil || len(payload) == 0 {
		return nil, err
	}
	var pending QianfanVideoPendingBilling
	if err := json.Unmarshal(payload, &pending); err != nil {
		return nil, err
	}
	return &pending, nil
}

// ClaimQianfanVideoBilling returns true once per completed task so concurrent
// status/content polls cannot double-bill. Fail-closed on cache errors.
func (s *OpenAIGatewayService) ClaimQianfanVideoBilling(ctx context.Context, taskID string, userID, apiKeyID int64) (bool, error) {
	if s == nil || s.cache == nil {
		return false, fmt.Errorf("qianfan video billing claim cache is unavailable")
	}
	key := qianfanVideoPendingBillingKey(taskID, userID, apiKeyID)
	if key == "" {
		return false, fmt.Errorf("qianfan video billing claim key is invalid")
	}
	return s.cache.ClaimGrokVideoBilled(ctx, key, qianfanVideoBilledClaimTTL(s.cfg))
}

// ReleaseQianfanVideoBilling clears a claim after a failed durable RecordUsage
// so a later poll can retry billing.
func (s *OpenAIGatewayService) ReleaseQianfanVideoBilling(ctx context.Context, taskID string, userID, apiKeyID int64) error {
	if s == nil || s.cache == nil {
		return fmt.Errorf("qianfan video billing claim cache is unavailable")
	}
	key := qianfanVideoPendingBillingKey(taskID, userID, apiKeyID)
	if key == "" {
		return fmt.Errorf("qianfan video billing claim key is invalid")
	}
	return s.cache.ReleaseGrokVideoBilled(ctx, key)
}

// StableQianfanVideoBillingRequestID is the durable usage/dedup key for one
// async video task (not the per-poll gateway request id).
func StableQianfanVideoBillingRequestID(taskID string) string {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return ""
	}
	if strings.HasPrefix(taskID, "qianfan-video:") {
		return taskID
	}
	return "qianfan-video:" + taskID
}

// BuildQianfanVideoPendingBilling snapshots create-time billing evidence for a
// freshly accepted video task.
func BuildQianfanVideoPendingBilling(requestInfo QianfanActionRequestInfo, billingModel, upstreamModel, createdAt string) QianfanVideoPendingBilling {
	pending := QianfanVideoPendingBilling{
		Model:                strings.TrimSpace(requestInfo.Model),
		BillingModel:         strings.TrimSpace(billingModel),
		UpstreamModel:        strings.TrimSpace(upstreamModel),
		VideoDurationSeconds: NormalizeVideoBillingDurationSecondsOrDefault(qianfanVideoDurationOrDefault(requestInfo.DurationSeconds)),
		CreatedAt:            strings.TrimSpace(createdAt),
	}
	if pending.CreatedAt == "" {
		pending.CreatedAt = QianfanVideoPendingCreatedAtNow()
	}
	return pending
}

// qianfanVideoDurationOrDefault applies the documented 5s default when the
// create request omitted duration.
func qianfanVideoDurationOrDefault(durationSeconds int) int {
	if durationSeconds <= 0 {
		return 5
	}
	return durationSeconds
}

// MergeQianfanVideoPendingBilling overlays the create-time snapshot onto the
// completed-task billing record.
func MergeQianfanVideoPendingBilling(result *OpenAIForwardResult, pending *QianfanVideoPendingBilling, taskID string) *OpenAIForwardResult {
	if result == nil || pending == nil {
		return result
	}
	merged := *result
	merged.Model = firstNonEmptyString(pending.BillingModel, pending.Model, merged.Model)
	merged.BillingModel = firstNonEmptyString(pending.BillingModel, pending.Model, merged.BillingModel, merged.Model)
	merged.UpstreamModel = firstNonEmptyString(pending.UpstreamModel, merged.UpstreamModel)
	if strings.TrimSpace(pending.VideoResolution) != "" {
		merged.VideoResolution = pending.VideoResolution
	}
	if pending.VideoDurationSeconds > 0 {
		merged.VideoDurationSeconds = pending.VideoDurationSeconds
	}
	merged.VideoResolution = NormalizeVideoBillingResolutionOrDefault(merged.VideoResolution)
	merged.VideoDurationSeconds = NormalizeVideoBillingDurationSecondsOrDefault(qianfanVideoDurationOrDefault(merged.VideoDurationSeconds))
	if e2e := qianfanVideoE2EDuration(pending.CreatedAt, time.Now()); e2e > 0 {
		merged.Duration = e2e
	}
	merged.RequestID = StableQianfanVideoBillingRequestID(firstNonEmptyString(merged.ResponseID, taskID))
	return &merged
}

func qianfanVideoE2EDuration(createdAt string, discoveredAt time.Time) time.Duration {
	createdAt = strings.TrimSpace(createdAt)
	if createdAt == "" {
		return 0
	}
	created, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		if created, err = time.Parse(time.RFC3339, createdAt); err != nil {
			return 0
		}
	}
	if discoveredAt.IsZero() {
		discoveredAt = time.Now()
	}
	if d := discoveredAt.Sub(created); d > 0 {
		return d
	}
	return 0
}

// forwardQianfanVideoContent proxies the CDN download of a completed task. The
// task status is re-queried through the owner-bound account so the CDN URL
// never leaves the gateway; unready tasks see their status body instead.
func (s *OpenAIGatewayService) forwardQianfanVideoContent(
	ctx context.Context, c *gin.Context, account *Account, taskID string, startTime time.Time,
) (*OpenAIForwardResult, error) {
	token, _, err := s.getRequestCredential(ctx, c, account)
	if err != nil {
		return nil, err
	}
	statusURL, err := s.buildQianfanActionURL(account, QianfanEndpointVideoStatus, taskID)
	if err != nil {
		return nil, err
	}
	proxyURL, egressErr := account.EgressProxyURL(ctx)
	if egressErr != nil {
		return nil, egressErr
	}

	upstreamCtx, releaseUpstreamCtx := detachUpstreamContext(ctx)
	defer releaseUpstreamCtx()
	statusReq, err := http.NewRequestWithContext(WithHTTPUpstreamRedirectsDisabled(upstreamCtx), http.MethodGet, statusURL, nil)
	if err != nil {
		return nil, err
	}
	statusReq.Header.Set("Authorization", "Bearer "+token)
	statusReq.Header.Set("Accept", "application/json")
	account.ApplyHeaderOverrides(statusReq.Header)
	statusReq.Header.Set("Authorization", "Bearer "+token)

	upstreamStart := time.Now()
	statusResp, err := generationDo(accountHTTPUpstream(s.httpUpstream, account), statusReq, proxyURL, account.ID, account.Concurrency)
	SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(upstreamStart).Milliseconds())
	if err != nil {
		return nil, s.handleOpenAIUpstreamTransportError(ctx, c, account, err, false)
	}
	defer func() { _ = statusResp.Body.Close() }()
	if statusResp.StatusCode >= 400 {
		return s.handleErrorResponse(ctx, statusResp, c, account, nil, "")
	}
	statusBody, err := ReadUpstreamResponseBody(statusResp.Body, s.cfg, c, openAITooLargeError)
	if err != nil {
		return nil, err
	}
	videoURL := strings.TrimSpace(gjson.GetBytes(statusBody, "content.video_url").String())
	if !IsQianfanVideoTaskSuccess(statusBody) || videoURL == "" {
		writeGrokMediaResponse(c, statusResp, statusBody, s.responseHeaderFilter)
		return &OpenAIForwardResult{ResponseID: strings.TrimSpace(taskID), Duration: time.Since(startTime)}, nil
	}
	parsed, err := url.Parse(videoURL)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
		setOpsUpstreamError(c, http.StatusBadGateway, "qianfan upstream returned an invalid video url", truncateString(videoURL, 256))
		return nil, &UpstreamFailoverError{StatusCode: http.StatusBadGateway, ResponseBody: statusBody}
	}

	contentReq, err := http.NewRequestWithContext(upstreamCtx, http.MethodGet, videoURL, nil)
	if err != nil {
		return nil, err
	}
	if rangeHeader := strings.TrimSpace(c.GetHeader("Range")); rangeHeader != "" {
		contentReq.Header.Set("Range", rangeHeader)
	}
	contentReq.Header.Set("Accept", "*/*")
	contentResp, err := generationDo(accountHTTPUpstream(s.httpUpstream, account), contentReq, proxyURL, account.ID, account.Concurrency)
	if err != nil {
		return nil, s.handleOpenAIUpstreamTransportError(ctx, c, account, err, false)
	}
	defer func() { _ = contentResp.Body.Close() }()
	if contentResp.StatusCode >= 400 {
		return s.handleErrorResponse(ctx, contentResp, c, account, nil, "")
	}
	if err := writeGrokMediaContentResponse(c, contentResp); err != nil {
		return nil, err
	}
	return &OpenAIForwardResult{
		ResponseID: strings.TrimSpace(taskID),
		Duration:   time.Since(startTime),
		VideoCount: 1,
		RequestID:  StableQianfanVideoBillingRequestID(taskID),
	}, nil
}
