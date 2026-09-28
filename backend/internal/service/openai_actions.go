package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	ws "github.com/coder/websocket"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// These are provider protocol adapters, not admission grants. Ingress must use
// the shared request plan, model authorization, pricing and resource ownership.
// Protocol evidence: https://developers.openai.com/api/reference/resources/audio
// and /videos, /images, /files, /batches (checked 2026-09-21).
var ErrOpenAIActionCredential = errors.New("OpenAI native action requires an OpenAI API key account")

func openAIActionPath(action string) (string, bool) {
	switch action {
	case "audio.tts":
		return "/audio/speech", true
	case "audio.stt":
		return "/audio/transcriptions", true
	case "audio.translate":
		return "/audio/translations", true
	case "image.variation":
		return "/images/variations", true
	case "video.generate":
		return "/videos", true
	case "video.edit":
		return "/videos/edits", true
	case "video.extend":
		return "/videos/extensions", true
	case "realtime.session":
		return "/realtime/client_secrets", true
	case "voice":
		return "/audio/voices", true
	default:
		return "", false
	}
}

// openAIActionCatalogEndpoint maps one native action onto the catalogue endpoint
// family its model evidence is recorded under. The video lifecycle shares the
// media (images) family the catalogue publishes for video tasks, so the send
// boundary must ask for that family instead of the action name; actions whose
// family the catalogue never publishes keep their own token and stay refused.
func openAIActionCatalogEndpoint(action string) string {
	switch action {
	case "video.generate", "video.edit", "video.extend":
		return CompositeRouteEndpointImages
	default:
		return action
	}
}

func openAIActionCredential(account *Account) (string, error) {
	if account == nil || account.Platform != PlatformOpenAI || account.Type != AccountTypeAPIKey {
		return "", ErrOpenAIActionCredential
	}
	credential := strings.TrimSpace(account.GetOpenAIProtocolAPIKey())
	if credential == "" {
		return "", ErrOpenAIActionCredential
	}
	return credential, nil
}

func (s *OpenAIGatewayService) openAIActionURL(account *Account, endpoint string) (string, error) {
	base := strings.TrimRight(account.GetOpenAIFormatBaseURL(), "/")
	if base == "" {
		base = "https://api.openai.com"
	}
	validated, err := s.validateUpstreamBaseURL(base)
	if err != nil {
		return "", err
	}
	target, err := url.Parse(validated)
	if err != nil || target.Host == "" || target.User != nil || target.RawQuery != "" || target.Fragment != "" {
		return "", errors.New("invalid OpenAI action base URL")
	}
	target.Path = strings.TrimRight(target.Path, "/")
	if target.Path == "" {
		target.Path = "/v1"
	}
	target.Path += endpoint
	target.RawPath = ""
	return target.String(), nil
}

// prepareOpenAIActionBody preserves all multipart parts, including repeated
// fields, filenames, MIME types and binary data, while replacing only model.
func prepareOpenAIActionBody(action, contentType string, body []byte, mappedModel string) ([]byte, string, string, error) {
	mediaType, parameters, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, "", "", err
	}
	modelPath := "model"
	if action == "realtime.session" {
		modelPath = "session.model"
	}
	// Voice creation is account-scoped and has no model parameter; every other
	// action keeps the model binding below.
	modelRequired := action != "voice"
	if mediaType == "application/json" {
		if !gjson.ValidBytes(body) || !gjson.ParseBytes(body).IsObject() {
			return nil, "", "", errors.New("action body must be a JSON object")
		}
		if action == "audio.stt" || action == "audio.translate" || action == "image.variation" || action == "voice" {
			return nil, "", "", errors.New("action requires multipart/form-data")
		}
		model := gjson.GetBytes(body, modelPath).String()
		if action == "video.edit" || action == "video.extend" {
			// These endpoints inherit the source model; do not invent a model
			// body parameter. Ingress supplies the owner-checked source model.
			if mappedModel == "" || !openAIResourceID(gjson.GetBytes(body, "video.id").String()) {
				return nil, "", "", errors.New("bound source video and model are required")
			}
			return body, contentType, mappedModel, nil
		}
		if mappedModel != "" {
			model = mappedModel
			body, err = sjson.SetBytes(body, modelPath, model)
		}
		if model == "" {
			return nil, "", "", errors.New("model is required")
		}
		return body, contentType, model, err
	}
	if mediaType != "multipart/form-data" || parameters["boundary"] == "" || action == "audio.tts" || action == "realtime.session" {
		return nil, "", "", errors.New("invalid action content type")
	}
	reader := multipart.NewReader(bytes.NewReader(body), parameters["boundary"])
	var output bytes.Buffer
	writer := multipart.NewWriter(&output)
	model, modelCount, fileCount := "", 0, 0
	for {
		part, readErr := reader.NextPart()
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, "", "", readErr
		}
		if part.FormName() == "model" && modelRequired {
			modelCount++
			if modelCount > 1 || part.FileName() != "" {
				return nil, "", "", errors.New("ambiguous multipart model")
			}
			value, valueErr := io.ReadAll(part)
			if valueErr != nil {
				return nil, "", "", valueErr
			}
			model = string(value)
			if mappedModel != "" {
				model = mappedModel
			}
			if err = writer.WriteField("model", model); err != nil {
				return nil, "", "", err
			}
		} else {
			expectedFile := "file"
			if action == "image.variation" {
				expectedFile = "image"
			}
			if action == "voice" {
				expectedFile = "audio_sample"
			}
			if part.FormName() == expectedFile && part.FileName() != "" {
				fileCount++
			}
			destination, partErr := writer.CreatePart(part.Header)
			if partErr != nil {
				return nil, "", "", partErr
			}
			if _, err = io.Copy(destination, part); err != nil {
				return nil, "", "", err
			}
		}
		_ = part.Close()
	}
	if modelRequired && modelCount == 0 && mappedModel != "" {
		model = mappedModel
		if err = writer.WriteField("model", model); err != nil {
			return nil, "", "", err
		}
	}
	if modelRequired && model == "" {
		return nil, "", "", errors.New("model is required")
	}
	if (action == "audio.stt" || action == "audio.translate" || action == "image.variation" || action == "voice") && fileCount == 0 {
		return nil, "", "", errors.New("uploaded file is required")
	}
	if err = writer.Close(); err != nil {
		return nil, "", "", err
	}
	return output.Bytes(), writer.FormDataContentType(), model, nil
}

// ForwardOpenAIAction executes one attempt through the OpenAI trunk. It returns
// the raw response (audio, text, JSON or SSE); the caller owns Body and records
// observed usage even on interruption. No automatic retry or local billing is
// introduced here. Native jobs must be submitted through UpstreamJobService.
func (s *OpenAIGatewayService) ForwardOpenAIAction(ctx context.Context, account *Account, action, contentType string, body []byte, mappedModel string) (*http.Response, error) {
	credential, err := openAIActionCredential(account)
	if err != nil {
		return nil, err
	}
	endpoint, exists := openAIActionPath(action)
	if !exists {
		return nil, fmt.Errorf("unverified OpenAI action %q", action)
	}
	body, contentType, model, err := prepareOpenAIActionBody(action, contentType, body, mappedModel)
	if err != nil {
		return nil, err
	}
	if model != "" {
		if err = validateAccountForwardModel(ctx, s.accountRepo, account, model, openAIActionCatalogEndpoint(action)); err != nil {
			return nil, err
		}
	}
	target, err := s.openAIActionURL(account, endpoint)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	account.ApplyHeaderOverrides(request.Header)
	request.Header.Set("Authorization", "Bearer "+credential)
	request.Header.Set("Content-Type", contentType)
	request = request.WithContext(WithHTTPUpstreamProfile(request.Context(), HTTPUpstreamProfileOpenAI))
	proxy := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxy = account.Proxy.URL()
	}
	return s.doOpenAIUpstream(request, proxy, account)
}

// ParseOpenAIActionUsage returns only explicitly observed usage. Absence is not
// zero usage or permission to bill an audio request as text tokens.
func ParseOpenAIActionUsage(body []byte) (json.RawMessage, bool) {
	if !gjson.ValidBytes(body) {
		return nil, false
	}
	usage := gjson.GetBytes(body, "usage")
	if !usage.IsObject() {
		usage = gjson.GetBytes(body, "response.usage")
	}
	if !usage.IsObject() {
		return nil, false
	}
	return json.RawMessage(usage.Raw), true
}

// OpenOpenAIRealtime opens the native API WebSocket, never the Codex sideband.
// The caller relays events using its request context and settles the shared
// receipt from response.done usage. Cancelling ctx closes the upstream socket.
func (s *OpenAIGatewayService) OpenOpenAIRealtime(ctx context.Context, account *Account, model string) (*ws.Conn, error) {
	if account != nil {
		if err := checkUnboundedTPMSession(ctx, account.ID); err != nil {
			return nil, err
		}
	}
	credential, err := openAIActionCredential(account)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(model) == "" {
		return nil, errors.New("model is required")
	}
	if err = validateAccountForwardModel(ctx, s.accountRepo, account, model, "realtime"); err != nil {
		return nil, err
	}
	target, err := s.openAIActionURL(account, "/realtime")
	if err != nil {
		return nil, err
	}
	parsed, _ := url.Parse(target)
	query := parsed.Query()
	query.Set("model", model)
	parsed.RawQuery = query.Encode()
	baseTransport, _ := http.DefaultTransport.(*http.Transport)
	transport := baseTransport.Clone()
	defer transport.CloseIdleConnections()
	if account.ProxyID != nil && account.Proxy != nil {
		proxy, parseErr := url.Parse(account.Proxy.URL())
		if parseErr != nil {
			return nil, parseErr
		}
		transport.Proxy = http.ProxyURL(proxy)
	}
	headers := http.Header{}
	account.ApplyHeaderOverrides(headers)
	headers.Set("Authorization", "Bearer "+credential)
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	connection, response, err := ws.Dial(ctx, parsed.String(), &ws.DialOptions{HTTPClient: client, HTTPHeader: headers, CompressionMode: ws.CompressionDisabled})
	if err != nil {
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		return nil, errors.New("OpenAI realtime handshake failed")
	}
	connection.SetReadLimit(16 << 20)
	// The callback is detached by socket cancellation only when the request
	// completes; ingress must cancel its session context when the client closes.
	context.AfterFunc(ctx, func() { _ = connection.CloseNow() })
	return connection, nil
}

// ForwardOpenAIResource resolves a public resource under the requesting user
// and key before using the bound account. There is no raw task-ID lookup path.
// Workspace/revision validation remains in the shared repository admission.
func (s *OpenAIGatewayService) ForwardOpenAIResource(ctx context.Context, repository UpstreamJobRepository, publicID string, userID, keyID int64, account *Account, operation string) (*http.Response, error) {
	credential, err := openAIActionCredential(account)
	if err != nil {
		return nil, err
	}
	if repository == nil || userID <= 0 || keyID <= 0 {
		return nil, ErrUpstreamJobNotFound
	}
	job, err := repository.Get(ctx, publicID, userID, keyID)
	if err != nil {
		return nil, err
	}
	if job == nil || job.Platform != PlatformOpenAI || job.UserID != userID || job.APIKeyID != keyID || job.AccountID != account.ID || !openAIResourceID(job.UpstreamTaskID) {
		return nil, ErrUpstreamJobNotFound
	}
	method, endpoint := http.MethodGet, ""
	switch job.Family {
	case "video":
		endpoint = "/videos/" + job.UpstreamTaskID
		if operation == "content" {
			endpoint += "/content"
		} else if operation == "delete" {
			method = http.MethodDelete
		} else if operation != "status" {
			return nil, errors.New("unverified video operation")
		}
	case "file":
		endpoint = "/files/" + job.UpstreamTaskID
		if operation == "content" {
			endpoint += "/content"
		} else if operation == "delete" {
			method = http.MethodDelete
		} else if operation != "status" {
			return nil, errors.New("unverified file operation")
		}
	case "batch":
		endpoint = "/batches/" + job.UpstreamTaskID
		if operation == "cancel" {
			method = http.MethodPost
			endpoint += "/cancel"
		} else if operation != "status" {
			return nil, errors.New("unverified batch operation")
		}
	default:
		return nil, errors.New("unverified OpenAI resource family")
	}
	target, err := s.openAIActionURL(account, endpoint)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, method, target, nil)
	if err != nil {
		return nil, err
	}
	account.ApplyHeaderOverrides(request.Header)
	request.Header.Set("Authorization", "Bearer "+credential)
	request = request.WithContext(WithHTTPUpstreamProfile(ctx, HTTPUpstreamProfileOpenAI))
	proxy := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxy = account.Proxy.URL()
	}
	return s.doOpenAIUpstream(request, proxy, account)
}

func openAIResourceID(id string) bool {
	if id == "" {
		return false
	}
	for _, character := range id {
		if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') && (character < '0' || character > '9') && character != '_' && character != '-' {
			return false
		}
	}
	return true
}

// ParseOpenAIJobUpdate translates provider lifecycle names without treating a
// deleting video as a cancellable generation or a pending batch as completed.
func ParseOpenAIJobUpdate(body []byte) (UpstreamJobUpdate, error) {
	if !gjson.ValidBytes(body) {
		return UpstreamJobUpdate{}, errors.New("invalid OpenAI job JSON")
	}
	identifier := gjson.GetBytes(body, "id").String()
	status := gjson.GetBytes(body, "status").String()
	if !openAIResourceID(identifier) {
		return UpstreamJobUpdate{}, errors.New("invalid OpenAI job ID")
	}
	update := UpstreamJobUpdate{TaskID: identifier, ProviderStatus: status}
	switch status {
	case "queued", "validating":
		update.Status = "submitted"
	case "in_progress", "finalizing", "cancelling":
		update.Status = "running"
	case "completed":
		update.Status = "succeeded"
	case "failed":
		update.Status = "failed"
		update.ErrorCode = gjson.GetBytes(body, "error.code").String()
		update.ErrorMessage = sanitizeUpstreamErrorMessage(gjson.GetBytes(body, "error.message").String())
	case "expired":
		update.Status = "expired"
	case "cancelled":
		// Shared baseline has no cancelled terminal. Retain provider truth and
		// avoid marking a cancelled batch (possibly partial) as successful.
		update.Status = "failed"
		update.ErrorCode = "upstream_cancelled"
	default:
		return UpstreamJobUpdate{}, errors.New("unverified OpenAI job status")
	}
	update.Usage, _ = ParseOpenAIActionUsage(body)
	return update, nil
}

// openAIResourceRequest executes one bounded request for the native task and
// resource lifecycle (files, batches, videos). Responses are capped well above
// any documented metadata payload; 429/5xx stay retryable through the shared
// job service instead of being folded into terminal evidence here.
func (s *OpenAIGatewayService) openAIResourceRequest(ctx context.Context, account *Account, method, endpoint, contentType string, body []byte) ([]byte, int, error) {
	credential, err := openAIActionCredential(account)
	if err != nil {
		return nil, 0, err
	}
	target, err := s.openAIActionURL(account, endpoint)
	if err != nil {
		return nil, 0, err
	}
	request, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	account.ApplyHeaderOverrides(request.Header)
	request.Header.Set("Authorization", "Bearer "+credential)
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	request.Header.Set("Accept", "application/json")
	request = request.WithContext(WithHTTPUpstreamProfile(request.Context(), HTTPUpstreamProfileOpenAI))
	proxy := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxy = account.Proxy.URL()
	}
	response, err := s.doOpenAIUpstream(request, proxy, account)
	if err != nil {
		return nil, 0, err
	}
	defer func() {
		_ = response.Body.Close()
	}()
	const resourceResponseLimit = 4 << 20
	raw, err := io.ReadAll(io.LimitReader(response.Body, resourceResponseLimit+1))
	if err != nil {
		return nil, response.StatusCode, err
	}
	if len(raw) > resourceResponseLimit {
		return nil, response.StatusCode, errors.New("OpenAI resource response exceeds limit")
	}
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
		return nil, response.StatusCode, fmt.Errorf("OpenAI upstream temporarily unavailable: %d", response.StatusCode)
	}
	return raw, response.StatusCode, nil
}

// openAIResourceFailure keeps provider rejection truth without echoing a body
// that could carry sensitive fragments back to the client.
func openAIResourceFailure(status int, raw []byte) UpstreamJobUpdate {
	message := ""
	if gjson.ValidBytes(raw) {
		message = strings.TrimSpace(gjson.GetBytes(raw, "error.message").String())
	}
	reason := fmt.Sprintf("OpenAI rejected the request (HTTP %d)", status)
	if message != "" {
		reason += ": " + message
	}
	return UpstreamJobUpdate{Status: "failed", ErrorCode: "upstream_rejected", ErrorMessage: sanitizeUpstreamErrorMessage(reason)}
}

// openAIResourceAccount revalidates the bound account at poll time: a disabled
// or re-typed account must not keep serving a recorded job.
func (s *OpenAIGatewayService) openAIResourceAccount(ctx context.Context, j *UpstreamJob) (*Account, error) {
	if j == nil || j.Platform != PlatformOpenAI {
		return nil, ErrUpstreamJobNotFound
	}
	account, err := s.accountRepo.GetByID(ctx, j.AccountID)
	if err != nil {
		return nil, err
	}
	if account == nil || account.Platform != PlatformOpenAI || account.Type != AccountTypeAPIKey || !account.IsActive() {
		return nil, errors.New("OpenAI resource account unavailable")
	}
	return account, nil
}

// SubmitOpenAIFile uploads one file through POST /v1/files (multipart purpose +
// file). Validation, size limits and tenant ownership stay in the shared
// resource layer; this adapter only guarantees the upstream wire shape and a
// parsed file object. Evidence: /files in the official OpenAPI specification,
// checked 2026-09-21.
func (s *OpenAIGatewayService) SubmitOpenAIFile(ctx context.Context, account *Account, contentType string, body []byte) (UpstreamJobUpdate, error) {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "multipart/form-data" {
		return UpstreamJobUpdate{}, errors.New("file upload requires multipart/form-data")
	}
	raw, status, err := s.openAIResourceRequest(ctx, account, http.MethodPost, "/files", contentType, body)
	if err != nil {
		return UpstreamJobUpdate{}, err
	}
	if status < 200 || status >= 300 {
		return openAIResourceFailure(status, raw), nil
	}
	if !gjson.ValidBytes(raw) || gjson.GetBytes(raw, "object").String() != "file" || !openAIResourceID(gjson.GetBytes(raw, "id").String()) {
		return UpstreamJobUpdate{}, errors.New("invalid OpenAI file upload response")
	}
	identifier := gjson.GetBytes(raw, "id").String()
	return UpstreamJobUpdate{TaskID: identifier, Status: "succeeded", ResultRef: identifier}, nil
}

// SubmitOpenAIBatch creates one batch through POST /v1/batches. The documented
// endpoint and window values are pinned here so the shared layer never forwards
// an unbillable or unverified batch target. Even an immediately completed batch
// stays running: settlement requires output-file validation in the shared job
// layer. Evidence: CreateBatchRequest/Batch schemas checked 2026-09-21.
func (s *OpenAIGatewayService) SubmitOpenAIBatch(ctx context.Context, account *Account, body []byte) (UpstreamJobUpdate, error) {
	if !gjson.ValidBytes(body) || !gjson.ParseBytes(body).IsObject() {
		return UpstreamJobUpdate{}, errors.New("batch body must be a JSON object")
	}
	if !openAIResourceID(gjson.GetBytes(body, "input_file_id").String()) {
		return UpstreamJobUpdate{}, errors.New("batch requires a valid input_file_id")
	}
	switch gjson.GetBytes(body, "endpoint").String() {
	case "/v1/responses", "/v1/chat/completions", "/v1/embeddings", "/v1/completions", "/v1/moderations", "/v1/images/generations", "/v1/images/edits", "/v1/videos":
	default:
		return UpstreamJobUpdate{}, errors.New("unverified OpenAI batch endpoint")
	}
	if gjson.GetBytes(body, "completion_window").String() != "24h" {
		return UpstreamJobUpdate{}, errors.New("unverified OpenAI batch completion window")
	}
	raw, status, err := s.openAIResourceRequest(ctx, account, http.MethodPost, "/batches", "application/json", body)
	if err != nil {
		return UpstreamJobUpdate{}, err
	}
	if status < 200 || status >= 300 {
		return openAIResourceFailure(status, raw), nil
	}
	update, err := parseOpenAIBatchResource(raw)
	if err != nil {
		return UpstreamJobUpdate{}, err
	}
	if update.Status == "succeeded" {
		update.Status = "running"
	}
	return update, nil
}

// PollOpenAIBatch reads one owner-bound batch. Usage and output validation that
// gate settlement belong to the shared job layer; this adapter reports provider
// truth plus the output file reference.
func (s *OpenAIGatewayService) PollOpenAIBatch(ctx context.Context, j *UpstreamJob) (UpstreamJobUpdate, error) {
	if j == nil || j.Family != "batch" {
		return UpstreamJobUpdate{}, ErrUpstreamJobUnsupportedUsage
	}
	account, err := s.openAIResourceAccount(ctx, j)
	if err != nil {
		return UpstreamJobUpdate{}, err
	}
	raw, status, err := s.openAIResourceRequest(ctx, account, http.MethodGet, "/batches/"+url.PathEscape(j.UpstreamTaskID), "", nil)
	if err != nil {
		return UpstreamJobUpdate{}, err
	}
	if status < 200 || status >= 300 {
		return openAIResourceFailure(status, raw), nil
	}
	return parseOpenAIBatchResource(raw)
}

func parseOpenAIBatchResource(raw []byte) (UpstreamJobUpdate, error) {
	update, err := ParseOpenAIJobUpdate(raw)
	if err != nil {
		return UpstreamJobUpdate{}, err
	}
	if gjson.GetBytes(raw, "object").String() != "batch" {
		return UpstreamJobUpdate{}, errors.New("invalid OpenAI batch object")
	}
	if output := gjson.GetBytes(raw, "output_file_id").String(); openAIResourceID(output) {
		update.ResultRef = output
	}
	return update, nil
}
