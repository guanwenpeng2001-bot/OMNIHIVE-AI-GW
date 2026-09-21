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
	"net/textproto"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Mistral native actions beyond chat. Endpoint shapes, fields and event types are
// fixed against the official specification (mistralai/platform-docs-public
// openapi.yaml, retrieved 2026-09-21): /v1/embeddings, /v1/audio/transcriptions,
// /v1/audio/speech and /v1/audio/voices. Mistral has no /v1/images* endpoints;
// image generation exists only as a conversations/agent built-in tool, so nothing
// here bridges media generation onto chat-shaped endpoints.
const (
	mistralEmbeddingsEndpoint      = "/v1/embeddings"
	mistralTranscriptionsEndpoint  = "/v1/audio/transcriptions"
	mistralSpeechEndpoint          = "/v1/audio/speech"
	mistralVoicesEndpoint          = "/v1/audio/voices"
	mistralTranscriptionDoneEvent  = "transcription.done"
	mistralSpeechDoneEvent         = "speech.audio.done"
	mistralMultipartFieldSizeLimit = 1 << 20
)

// mistralActionAccount confines these forwards to Mistral API-key accounts, the
// only credential the official API documents for these endpoints.
func mistralActionAccount(account *Account) error {
	if account == nil || account.Platform != PlatformMistral {
		return fmt.Errorf("mistral action requires a mistral account")
	}
	if account.Type != AccountTypeAPIKey {
		return fmt.Errorf("mistral action requires an API key account")
	}
	return nil
}

// sendMistralActionRequest is the media-action twin of sendCCUpstreamRequest: a
// detached upstream context, the OpenAI HTTP profile, Bearer auth, the client
// header allowlist, account header overrides and proxy routing all stay identical
// to the Mistral chat trunk so error policy does not diverge between actions.
func (s *OpenAIGatewayService) sendMistralActionRequest(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	method string,
	targetURL string,
	body []byte,
	contentType string,
	accept string,
	token string,
	endpoint string,
) (*http.Response, error) {
	upstreamCtx, releaseUpstreamCtx := detachUpstreamContext(ctx)
	upstreamReq, err := http.NewRequestWithContext(upstreamCtx, method, targetURL, bytes.NewReader(body))
	releaseUpstreamCtx()
	if err != nil {
		return nil, fmt.Errorf("build upstream request: %w", err)
	}
	SetActualOpenAIUpstreamEndpoint(c, endpoint)
	upstreamReq = upstreamReq.WithContext(WithHTTPUpstreamProfile(upstreamReq.Context(), HTTPUpstreamProfileOpenAI))
	if contentType != "" {
		upstreamReq.Header.Set("Content-Type", contentType)
	}
	setModelProviderChatAuthHeader(upstreamReq.Header, account, token)
	if accept != "" {
		upstreamReq.Header.Set("Accept", accept)
	}
	if c != nil && c.Request != nil {
		for key, values := range c.Request.Header {
			if openaiCCRawAllowedHeaders[strings.ToLower(key)] {
				for _, value := range values {
					upstreamReq.Header.Add(key, value)
				}
			}
		}
	}
	if userAgent := account.GetOpenAIUserAgent(); userAgent != "" {
		upstreamReq.Header.Set("user-agent", userAgent)
	}
	account.ApplyHeaderOverrides(upstreamReq.Header)

	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	resp, err := s.doOpenAIUpstream(upstreamReq, proxyURL, account)
	if err != nil {
		return nil, s.handleOpenAIUpstreamTransportError(ctx, c, account, err, false)
	}
	return resp, nil
}

// handleMistralActionHTTPError applies the same failover classification and
// OpenAI-shaped client error contract as the Mistral chat path.
func (s *OpenAIGatewayService) handleMistralActionHTTPError(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	resp *http.Response,
	billingModel string,
) (*OpenAIForwardResult, error) {
	respBody, upstreamMsg := s.readOpenAIUpstreamError(resp)
	if failoverErr := s.failoverOpenAIUpstreamHTTPError(ctx, c, account, resp, respBody, upstreamMsg, billingModel); failoverErr != nil {
		return nil, failoverErr
	}
	return s.handleCompatErrorResponse(resp, c, account, writeOpenAIEmbeddingsError, billingModel)
}

func (s *OpenAIGatewayService) mistralActionBaseURL(account *Account) (string, error) {
	baseURL := account.GetOpenAIFormatBaseURL()
	if baseURL == "" {
		baseURL = mistralDefaultBaseURL
	}
	return s.validateUpstreamBaseURL(baseURL)
}

// relayMistralEventStream copies an upstream Mistral SSE stream (streaming
// transcription / streaming speech) to the client while capturing the terminal
// event's data payload for usage. Client disconnects keep draining upstream so
// billing evidence is not lost, matching the shared chat trunk lifecycle.
func (s *OpenAIGatewayService) relayMistralEventStream(
	c *gin.Context,
	resp *http.Response,
	terminalEvent string,
) (terminal gjson.Result, clientAborted bool, err error) {
	writeStreamHeaders := s.newStreamHeaderWriter(c, resp.Header)
	maxSize, idle := defaultMaxLineSize, 300*time.Second
	if s.cfg != nil {
		if s.cfg.Gateway.MaxLineSize > 0 {
			maxSize = s.cfg.Gateway.MaxLineSize
		}
		if s.cfg.Gateway.StreamDataIntervalTimeout > 0 {
			idle = time.Duration(s.cfg.Gateway.StreamDataIntervalTimeout) * time.Second
		}
	}
	scanner := newUpstreamFrameReader(resp.Body, maxSize, c.Request.Context(), idle)
	defer scanner.Close()

	clientDisconnected := false
	clientOutputStarted := false
	sawTerminal := false
	semanticMarked := false
	var frameErr error

	writeFrame := func(eventType, payload string) {
		if clientDisconnected {
			return
		}
		if !clientOutputStarted {
			writeStreamHeaders()
			clientOutputStarted = true
		}
		var builder strings.Builder
		if eventType != "" {
			builder.WriteString("event: " + eventType + "\n")
		}
		for _, line := range strings.Split(payload, "\n") {
			builder.WriteString("data: " + line + "\n")
		}
		builder.WriteString("\n")
		if _, writeErr := c.Writer.WriteString(builder.String()); writeErr != nil {
			clientDisconnected = true
			logger.L().Debug("mistral event stream: client disconnected, continuing to drain upstream for billing")
			return
		}
		c.Writer.Flush()
	}

	for scanner.Scan() {
		frame := scanner.frame
		payload := strings.TrimSpace(frame.Data)
		if payload == "" {
			continue
		}
		if !gjson.Valid(payload) {
			frameErr = fmt.Errorf("invalid Mistral stream event")
			break
		}
		if !semanticMarked {
			scanner.Semantic()
			semanticMarked = true
		}
		dataType := gjson.Get(payload, "type").String()
		if dataType == terminalEvent || frame.EventType == terminalEvent {
			terminal = gjson.Parse(payload)
			sawTerminal = true
		}
		writeFrame(frame.EventType, payload)
		if sawTerminal {
			if clientDisconnected {
				scanner.Disconnected()
			}
			break
		}
		if clientDisconnected {
			scanner.Disconnected()
		}
	}

	scanErr := scanner.Err()
	if frameErr != nil {
		scanErr = frameErr
	}
	clientAborted = clientDisconnected || c.Request.Context().Err() != nil
	if clientAborted {
		// 与主干一致：客户端取消后上游读失败不可区分，按已取得证据收尾。
		return terminal, true, context.Canceled
	}
	if scanErr != nil {
		return terminal, false, newOpenAIUpstreamStreamReadError(scanErr)
	}
	if !sawTerminal {
		return terminal, false, newOpenAIUpstreamStreamReadError(ErrOpenAIUpstreamStreamTruncated)
	}
	return terminal, false, nil
}

// prepareMistralEmbeddingsRequest enforces the native EmbeddingRequest contract:
// model and a string/string-array input are required, and OpenAI's `dimensions`
// is bridged onto the semantically identical native `output_dimension` (the
// native schema sets additionalProperties: false, so forwarding `dimensions`
// would only produce an upstream 422). All other fields pass through untouched;
// upstream validation remains the honest error path.
func prepareMistralEmbeddingsRequest(body []byte) ([]byte, error) {
	var request struct {
		Model string          `json:"model"`
		Input json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(body, &request); err != nil || request.Model == "" || len(request.Input) == 0 || bytes.Equal(bytes.TrimSpace(request.Input), []byte("null")) {
		return nil, fmt.Errorf("invalid Mistral embeddings request")
	}
	var single string
	var batch []string
	if err := json.Unmarshal(request.Input, &single); err != nil {
		if err := json.Unmarshal(request.Input, &batch); err != nil {
			return nil, fmt.Errorf("mistral embeddings input must be a string or an array of strings")
		}
	}
	if dimensions := gjson.GetBytes(body, "dimensions"); dimensions.Exists() {
		if !gjson.GetBytes(body, "output_dimension").Exists() {
			if dimensions.Type != gjson.Number || dimensions.Float() <= 0 || dimensions.Float() != float64(dimensions.Int()) {
				return nil, fmt.Errorf("mistral embeddings dimensions must be a positive integer")
			}
			bridged, err := sjson.SetBytes(body, "output_dimension", dimensions.Int())
			if err != nil {
				return nil, fmt.Errorf("bridge mistral embeddings dimensions: %w", err)
			}
			body = bridged
		}
		without, err := sjson.DeleteBytes(body, "dimensions")
		if err != nil {
			return nil, fmt.Errorf("bridge mistral embeddings dimensions: %w", err)
		}
		body = without
	}
	return body, nil
}

// mistralEmbeddingsUsageKnown mirrors mistralChatUsageKnown for the embeddings
// shape: both token counters must be present non-negative integers before the
// response may settle as billable.
func mistralEmbeddingsUsageKnown(body []byte) bool {
	usage := gjson.GetBytes(body, "usage")
	if !usage.Exists() || !usage.IsObject() {
		return false
	}
	for _, field := range []string{"prompt_tokens", "completion_tokens"} {
		value := usage.Get(field)
		if value.Type != gjson.Number || value.Float() < 0 || value.Float() != float64(value.Int()) {
			return false
		}
	}
	return true
}

// ForwardMistralEmbeddings forwards OpenAI-shaped /v1/embeddings to the native
// Mistral endpoint. Usage is extracted from the response; unknown usage never
// settles as a trusted zero.
func (s *OpenAIGatewayService) ForwardMistralEmbeddings(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	defaultMappedModel string,
) (*OpenAIForwardResult, error) {
	if err := mistralActionAccount(account); err != nil {
		return nil, err
	}
	startTime := time.Now()

	originalModel := strings.TrimSpace(gjson.GetBytes(body, "model").String())
	if originalModel == "" {
		writeOpenAIEmbeddingsError(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return nil, fmt.Errorf("missing model in request")
	}
	billingModel := resolveOpenAIForwardModel(account, originalModel, defaultMappedModel)
	upstreamModel := normalizeOpenAIModelForUpstream(account, billingModel)
	SetOpsUpstreamModel(c, upstreamModel)
	upstreamBody := body
	if upstreamModel != originalModel {
		upstreamBody = ReplaceModelInBody(body, upstreamModel)
	}
	prepared, err := prepareMistralEmbeddingsRequest(upstreamBody)
	if err != nil {
		writeOpenAIEmbeddingsError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return nil, err
	}
	token, tokenKind, err := s.getRequestCredential(ctx, c, account)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("account %d missing %s credential", account.ID, tokenKind)
	}
	baseURL, err := s.mistralActionBaseURL(account)
	if err != nil {
		return nil, fmt.Errorf("invalid base_url: %w", err)
	}
	targetURL := buildOpenAIEmbeddingsURL(baseURL)

	resp, err := s.sendMistralActionRequest(ctx, c, account, http.MethodPost, targetURL, prepared, "application/json", "application/json", token, mistralEmbeddingsEndpoint)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return s.handleMistralActionHTTPError(ctx, c, account, resp, billingModel)
	}
	respBody, err := ReadUpstreamResponseBody(resp.Body, s.cfg, c, openAITooLargeError)
	if err != nil {
		return nil, fmt.Errorf("read upstream body: %w", err)
	}
	if !mistralEmbeddingsUsageKnown(respBody) {
		writeOpenAIEmbeddingsError(c, http.StatusBadGateway, "upstream_error", "Mistral usage is unknown")
		return nil, fmt.Errorf("mistral usage is unknown")
	}
	writeOpenAIEmbeddingsUpstreamResponse(c, resp, respBody, s.responseHeaderFilter)
	return &OpenAIForwardResult{
		RequestID:       firstNonEmptyString(resp.Header.Get("x-request-id"), resp.Header.Get("request-id")),
		UpstreamHeaders: resp.Header,
		Usage:           extractOpenAIEmbeddingsUsage(respBody),
		Model:           originalModel,
		BillingModel:    billingModel,
		UpstreamModel:   upstreamModel,
		Stream:          false,
		Duration:        time.Since(startTime),
	}, nil
}

// mistralMultipartFieldValue reads a single form field from a captured multipart
// body without disturbing the bytes that will be forwarded upstream.
func mistralMultipartFieldValue(body []byte, contentType string, field string) (string, error) {
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil || params["boundary"] == "" {
		return "", fmt.Errorf("invalid multipart content type")
	}
	reader := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			return "", nil
		}
		if err != nil {
			return "", fmt.Errorf("invalid multipart body")
		}
		if part.FormName() != field || part.FileName() != "" {
			continue
		}
		value, err := io.ReadAll(io.LimitReader(part, mistralMultipartFieldSizeLimit))
		if err != nil {
			return "", fmt.Errorf("read multipart field: %w", err)
		}
		return string(value), nil
	}
}

// replaceMistralMultipartModel rewrites only the model form field so upstream
// model mapping keeps working for transcriptions; file bytes and every other
// field are copied verbatim under a fresh boundary.
func replaceMistralMultipartModel(body []byte, contentType string, model string) ([]byte, string, error) {
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil || params["boundary"] == "" {
		return nil, "", fmt.Errorf("invalid multipart content type")
	}
	reader := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	var rewritten bytes.Buffer
	writer := multipart.NewWriter(&rewritten)
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, "", fmt.Errorf("invalid multipart body")
		}
		header := make(textproto.MIMEHeader, len(part.Header))
		for key, values := range part.Header {
			header[key] = append([]string(nil), values...)
		}
		target, err := writer.CreatePart(header)
		if err != nil {
			return nil, "", fmt.Errorf("rebuild multipart body: %w", err)
		}
		if part.FormName() == "model" && part.FileName() == "" {
			if _, err := io.WriteString(target, model); err != nil {
				return nil, "", fmt.Errorf("rebuild multipart body: %w", err)
			}
			_, _ = io.Copy(io.Discard, part)
			continue
		}
		if _, err := io.Copy(target, part); err != nil {
			return nil, "", fmt.Errorf("rebuild multipart body: %w", err)
		}
	}
	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("rebuild multipart body: %w", err)
	}
	return rewritten.Bytes(), writer.FormDataContentType(), nil
}

// mistralTranscriptionUsage extracts the official TranscriptionResponse usage:
// prompt_audio_seconds drives the native per-audio-time price, token counters are
// kept for observability. Usage is known only when the audio seconds are a
// present non-negative number.
func mistralTranscriptionUsage(body []byte) (audioSeconds float64, usage OpenAIUsage, known bool) {
	usageJSON := gjson.GetBytes(body, "usage")
	if !usageJSON.Exists() || !usageJSON.IsObject() {
		return 0, OpenAIUsage{}, false
	}
	seconds := usageJSON.Get("prompt_audio_seconds")
	if seconds.Type != gjson.Number || seconds.Float() < 0 {
		return 0, OpenAIUsage{}, false
	}
	usage = OpenAIUsage{
		InputTokens:  int(usageJSON.Get("prompt_tokens").Int()),
		OutputTokens: int(usageJSON.Get("completion_tokens").Int()),
	}
	return seconds.Float(), usage, true
}

// ForwardMistralAudioTranscription forwards multipart /v1/audio/transcriptions
// to Mistral's Voxtral transcription endpoint, streaming included (stream=true is
// the official SSE variant whose terminal transcription.done event carries usage).
func (s *OpenAIGatewayService) ForwardMistralAudioTranscription(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	contentType string,
	defaultMappedModel string,
) (*OpenAIForwardResult, error) {
	if err := mistralActionAccount(account); err != nil {
		return nil, err
	}
	startTime := time.Now()

	mediaType, _, mediaErr := mime.ParseMediaType(contentType)
	if mediaErr != nil || !strings.HasPrefix(strings.ToLower(mediaType), "multipart/") {
		writeOpenAIEmbeddingsError(c, http.StatusBadRequest, "invalid_request_error", "multipart/form-data is required")
		return nil, fmt.Errorf("mistral transcription requires multipart form data")
	}
	originalModel, err := mistralMultipartFieldValue(body, contentType, "model")
	if err != nil {
		writeOpenAIEmbeddingsError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return nil, err
	}
	originalModel = strings.TrimSpace(originalModel)
	if originalModel == "" {
		writeOpenAIEmbeddingsError(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return nil, fmt.Errorf("missing model in request")
	}
	streamField, err := mistralMultipartFieldValue(body, contentType, "stream")
	if err != nil {
		writeOpenAIEmbeddingsError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return nil, err
	}
	clientStream := strings.EqualFold(strings.TrimSpace(streamField), "true")

	billingModel := resolveOpenAIForwardModel(account, originalModel, defaultMappedModel)
	upstreamModel := normalizeOpenAIModelForUpstream(account, billingModel)
	SetOpsUpstreamModel(c, upstreamModel)
	upstreamBody, upstreamContentType := body, contentType
	if upstreamModel != originalModel {
		upstreamBody, upstreamContentType, err = replaceMistralMultipartModel(body, contentType, upstreamModel)
		if err != nil {
			writeOpenAIEmbeddingsError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
			return nil, err
		}
	}
	token, tokenKind, err := s.getRequestCredential(ctx, c, account)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("account %d missing %s credential", account.ID, tokenKind)
	}
	baseURL, err := s.mistralActionBaseURL(account)
	if err != nil {
		return nil, fmt.Errorf("invalid base_url: %w", err)
	}
	targetURL := buildOpenAIEndpointURL(baseURL, mistralTranscriptionsEndpoint)

	accept := "application/json"
	if clientStream {
		accept = "text/event-stream"
	}
	resp, err := s.sendMistralActionRequest(ctx, c, account, http.MethodPost, targetURL, upstreamBody, upstreamContentType, accept, token, mistralTranscriptionsEndpoint)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return s.handleMistralActionHTTPError(ctx, c, account, resp, billingModel)
	}

	if clientStream {
		terminal, clientAborted, relayErr := s.relayMistralEventStream(c, resp, mistralTranscriptionDoneEvent)
		audioSeconds, usage, known := mistralTranscriptionUsage([]byte(terminal.Raw))
		result := func() *OpenAIForwardResult {
			if !known {
				return nil
			}
			return &OpenAIForwardResult{
				RequestID:        firstNonEmptyString(resp.Header.Get("x-request-id"), resp.Header.Get("request-id")),
				UpstreamHeaders:  resp.Header,
				Usage:            usage,
				AudioUsage:       &AudioUsage{Mode: "stt", DurationOrUnits: audioSeconds / 3600.0},
				Model:            originalModel,
				BillingModel:     billingModel,
				UpstreamModel:    upstreamModel,
				Stream:           true,
				ClientDisconnect: clientAborted,
				Duration:         time.Since(startTime),
			}
		}
		if !known {
			if clientAborted {
				return nil, context.Canceled
			}
			return nil, newOpenAIUpstreamStreamReadError(fmt.Errorf("mistral usage is unknown"))
		}
		return result(), relayErr
	}

	respBody, err := ReadUpstreamResponseBody(resp.Body, s.cfg, c, openAITooLargeError)
	if err != nil {
		return nil, fmt.Errorf("read upstream body: %w", err)
	}
	audioSeconds, usage, known := mistralTranscriptionUsage(respBody)
	if !known {
		writeOpenAIEmbeddingsError(c, http.StatusBadGateway, "upstream_error", "Mistral usage is unknown")
		return nil, fmt.Errorf("mistral usage is unknown")
	}
	writeOpenAIEmbeddingsUpstreamResponse(c, resp, respBody, s.responseHeaderFilter)
	return &OpenAIForwardResult{
		RequestID:       firstNonEmptyString(resp.Header.Get("x-request-id"), resp.Header.Get("request-id")),
		UpstreamHeaders: resp.Header,
		Usage:           usage,
		AudioUsage:      &AudioUsage{Mode: "stt", DurationOrUnits: audioSeconds / 3600.0},
		Model:           originalModel,
		BillingModel:    billingModel,
		UpstreamModel:   upstreamModel,
		Stream:          false,
		Duration:        time.Since(startTime),
	}, nil
}

// prepareMistralSpeechRequest enforces the native SpeechRequest minimum: a
// non-empty input string and a model (the gateway prices per model). voice_id /
// ref_audio selection and response_format are validated upstream.
func prepareMistralSpeechRequest(body []byte) error {
	var request struct {
		Model string `json:"model"`
		Input string `json:"input"`
	}
	if err := json.Unmarshal(body, &request); err != nil || strings.TrimSpace(request.Model) == "" {
		return fmt.Errorf("invalid Mistral speech request")
	}
	if strings.TrimSpace(request.Input) == "" {
		return fmt.Errorf("mistral speech input must be a non-empty string")
	}
	return nil
}

// mistralSpeechBillableCharacters counts input runes: the official TTS price is
// per character, and rune counting keeps multibyte text honest.
func mistralSpeechBillableCharacters(body []byte) int {
	return len([]rune(gjson.GetBytes(body, "input").String()))
}

// ForwardMistralAudioSpeech forwards OpenAI-shaped /v1/audio/speech to the
// native Mistral TTS endpoint. Non-streaming returns the official
// {"audio_data": base64} envelope; streaming relays speech.audio.delta events
// and settles on speech.audio.done. Billing uses input characters, the official
// pricing unit.
func (s *OpenAIGatewayService) ForwardMistralAudioSpeech(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	defaultMappedModel string,
) (*OpenAIForwardResult, error) {
	if err := mistralActionAccount(account); err != nil {
		return nil, err
	}
	startTime := time.Now()

	if err := prepareMistralSpeechRequest(body); err != nil {
		writeOpenAIEmbeddingsError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return nil, err
	}
	originalModel := strings.TrimSpace(gjson.GetBytes(body, "model").String())
	clientStream := gjson.GetBytes(body, "stream").Bool()
	billingModel := resolveOpenAIForwardModel(account, originalModel, defaultMappedModel)
	upstreamModel := normalizeOpenAIModelForUpstream(account, billingModel)
	SetOpsUpstreamModel(c, upstreamModel)
	upstreamBody := body
	if upstreamModel != originalModel {
		upstreamBody = ReplaceModelInBody(body, upstreamModel)
	}
	token, tokenKind, err := s.getRequestCredential(ctx, c, account)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("account %d missing %s credential", account.ID, tokenKind)
	}
	baseURL, err := s.mistralActionBaseURL(account)
	if err != nil {
		return nil, fmt.Errorf("invalid base_url: %w", err)
	}
	targetURL := buildOpenAIEndpointURL(baseURL, mistralSpeechEndpoint)

	accept := "application/json"
	if clientStream {
		accept = "text/event-stream"
	}
	resp, err := s.sendMistralActionRequest(ctx, c, account, http.MethodPost, targetURL, upstreamBody, "application/json", accept, token, mistralSpeechEndpoint)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return s.handleMistralActionHTTPError(ctx, c, account, resp, billingModel)
	}

	audioUsage := &AudioUsage{Mode: "tts", DurationOrUnits: float64(mistralSpeechBillableCharacters(body)) / 1_000_000.0}
	if clientStream {
		terminal, clientAborted, relayErr := s.relayMistralEventStream(c, resp, mistralSpeechDoneEvent)
		var usage OpenAIUsage
		if terminalUsage := terminal.Get("usage"); terminalUsage.Exists() {
			usage = OpenAIUsage{
				InputTokens:  int(terminalUsage.Get("prompt_tokens").Int()),
				OutputTokens: int(terminalUsage.Get("completion_tokens").Int()),
			}
		}
		return &OpenAIForwardResult{
			RequestID:        firstNonEmptyString(resp.Header.Get("x-request-id"), resp.Header.Get("request-id")),
			UpstreamHeaders:  resp.Header,
			Usage:            usage,
			AudioUsage:       audioUsage,
			Model:            originalModel,
			BillingModel:     billingModel,
			UpstreamModel:    upstreamModel,
			Stream:           true,
			ClientDisconnect: clientAborted,
			Duration:         time.Since(startTime),
		}, relayErr
	}

	respBody, err := ReadUpstreamResponseBody(resp.Body, s.cfg, c, openAITooLargeError)
	if err != nil {
		return nil, fmt.Errorf("read upstream body: %w", err)
	}
	writeOpenAIEmbeddingsUpstreamResponse(c, resp, respBody, s.responseHeaderFilter)
	return &OpenAIForwardResult{
		RequestID:       firstNonEmptyString(resp.Header.Get("x-request-id"), resp.Header.Get("request-id")),
		UpstreamHeaders: resp.Header,
		AudioUsage:      audioUsage,
		Model:           originalModel,
		BillingModel:    billingModel,
		UpstreamModel:   upstreamModel,
		Stream:          false,
		Duration:        time.Since(startTime),
	}, nil
}

// ForwardMistralVoices forwards the official voice-management API
// (/v1/audio/voices CRUD and the {voice_id}/sample subresource). The subpath is
// everything after /v1/audio/voices; method and content type follow the client.
// Voice management is not a billable generation action, so no usage is attached.
func (s *OpenAIGatewayService) ForwardMistralVoices(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	subpath string,
	body []byte,
	contentType string,
	method string,
) (*OpenAIForwardResult, error) {
	if err := mistralActionAccount(account); err != nil {
		return nil, err
	}
	startTime := time.Now()

	subpath = strings.Trim(strings.TrimSpace(subpath), "/")
	if subpath != "" {
		parts := strings.Split(subpath, "/")
		if len(parts) > 2 || (len(parts) == 2 && parts[1] != "sample") {
			return nil, fmt.Errorf("unsupported mistral voices endpoint: %s", subpath)
		}
		for _, part := range parts {
			if part == "" || part == "." || part == ".." || strings.ContainsAny(part, "?#\\") {
				return nil, fmt.Errorf("invalid mistral voices endpoint path")
			}
		}
	}
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodDelete:
	default:
		return nil, fmt.Errorf("unsupported mistral voices method: %s", method)
	}
	token, tokenKind, err := s.getRequestCredential(ctx, c, account)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("account %d missing %s credential", account.ID, tokenKind)
	}
	baseURL, err := s.mistralActionBaseURL(account)
	if err != nil {
		return nil, fmt.Errorf("invalid base_url: %w", err)
	}
	targetURL := buildOpenAIEndpointURL(baseURL, mistralVoicesEndpoint)
	if subpath != "" {
		targetURL += "/" + subpath
	}

	resp, err := s.sendMistralActionRequest(ctx, c, account, method, targetURL, body, contentType, "application/json, audio/*", token, mistralVoicesEndpoint)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return s.handleMistralActionHTTPError(ctx, c, account, resp, "voices")
	}
	respBody, err := ReadUpstreamResponseBody(resp.Body, s.cfg, c, openAITooLargeError)
	if err != nil {
		return nil, fmt.Errorf("read upstream body: %w", err)
	}
	writeOpenAIEmbeddingsUpstreamResponse(c, resp, respBody, s.responseHeaderFilter)
	return &OpenAIForwardResult{
		RequestID:       firstNonEmptyString(resp.Header.Get("x-request-id"), resp.Header.Get("request-id")),
		UpstreamHeaders: resp.Header,
		Model:           "voices",
		BillingModel:    "voices",
		UpstreamModel:   "voices",
		Stream:          false,
		Duration:        time.Since(startTime),
	}, nil
}
