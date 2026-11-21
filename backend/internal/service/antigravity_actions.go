package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/tidwall/gjson"
)

// Antigravity standard-entry image actions (image.generate / image.edit).
//
// The verified CloudCode v1internal surface has no dedicated image endpoint:
// image output is an ordinary generateContent response that carries inline image
// parts (docs/gateway-platform-protocol-matrix.md; wire shape pinned by
// internal/pkg/antigravity/client_test.go). This bridge therefore builds the same
// v1internal generateContent request the Antigravity OAuth path already sends,
// runs it through ForwardGemini, and converts the Gemini response into the
// standard OpenAI images payload. It never invents an upstream /v1/images
// endpoint and never borrows a Gemini API key: the account's own OAuth token and
// its CloudCode project identifier are the identity.
//
// Ingress owns route registration and the dispatch-map binding for
// "antigravity.images"; this adapter owns admission, protocol conversion,
// upstream forwarding and usage settlement for the action. Only the Antigravity
// OAuth account type reaches the CloudCode path; an Antigravity API-key
// deployment keeps using the Gemini-compatible bridge.

const (
	AntigravityImageActionGenerate = "image.generate"
	AntigravityImageActionEdit     = "image.edit"

	// The CloudCode generateContent response carries a single candidate image.
	antigravityImageMaxOutputCount = 1
)

// AntigravityImagesRequest is a parsed standard-entry images request together with
// the native CloudCode generateContent body it converts to. The standard OpenAI
// shapes (JSON images[].image_url, multipart image file) are normalized here so
// the upstream request only ever carries inline base64 image parts.
type AntigravityImagesRequest struct {
	Action         string
	Model          string
	Prompt         string
	ResponseFormat string
	SizeTier       string
	HasInputImage  bool
	Multipart      bool
	NativeBody     []byte
	// CanonicalBody is the normalized JSON body every bridge entry accepts, so a
	// multipart upload or an images[].image_url edit can also reach the
	// Gemini-compatible API-key path without a second normalization.
	CanonicalBody []byte
	// ModerationBody is the JSON prompt/image payload the security audit reads;
	// a multipart body is not readable as JSON, so the entry audits this instead.
	ModerationBody []byte
}

// AntigravityUnsupportedActionError reports an action cell the CloudCode protocol
// cannot serve, carrying the dated evidence recorded in the domain authority so
// the caller returns an explicit protocol error instead of a silent 404.
func AntigravityUnsupportedActionError(action string) error {
	if evidence, ok := domain.AntigravityHardConflictEvidence(action); ok {
		return fmt.Errorf("antigravity does not implement %q: %s", action, evidence)
	}
	return fmt.Errorf("antigravity does not implement %q", action)
}

// ParseAntigravityImagesRequest reads a standard-entry images request and
// converts it to the native Gemini generateContent body. The OpenAI JSON edit
// shape (images[].image_url), the canonical single-image field and multipart
// image uploads all normalize to the one shape CloudCode accepts: an inline
// base64 image part next to the edit instruction.
func ParseAntigravityImagesRequest(endpointPath, contentType string, body []byte) (*AntigravityImagesRequest, error) {
	endpoint := ""
	switch {
	case strings.Contains(endpointPath, "/images/edits"):
		endpoint = openAIImagesEditsEndpoint
	case strings.Contains(endpointPath, "/images/generations"):
		endpoint = openAIImagesGenerationsEndpoint
	default:
		return nil, errors.New("unsupported images endpoint")
	}
	action := AntigravityImageActionGenerate
	if endpoint == openAIImagesEditsEndpoint {
		action = AntigravityImageActionEdit
	}

	// Reuse the shared OpenAI images parsing for the transport-level shapes so
	// multipart boundaries, uploads and field decoding stay in one place; the
	// provider-specific model gate is deliberately not applied here because
	// Antigravity serves gemini-*-image models, not gpt-image-*.
	standard := &OpenAIImagesRequest{Endpoint: endpoint, ContentType: contentType, N: 1, Body: body}
	mediaType, _, mediaErr := mime.ParseMediaType(strings.TrimSpace(contentType))
	if mediaErr == nil && strings.EqualFold(mediaType, "multipart/form-data") {
		standard.Multipart = true
		if err := parseOpenAIImagesMultipartRequest(body, contentType, standard); err != nil {
			return nil, err
		}
		if standard.MaskUpload != nil || standard.HasMask {
			return nil, errors.New("antigravity image edit does not support a mask")
		}
	} else {
		if len(body) == 0 {
			return nil, errors.New("request body is empty")
		}
		if !gjson.ValidBytes(body) {
			return nil, errors.New("failed to parse request body")
		}
		if err := parseAntigravityImagesJSONRequest(endpoint, body, standard); err != nil {
			return nil, err
		}
	}

	canonical, err := antigravityImagesCanonicalBody(action, standard)
	if err != nil {
		return nil, err
	}
	request, err := buildAntigravityImageActionRequest(action, canonical)
	if err != nil {
		return nil, err
	}
	request.Multipart = standard.Multipart
	request.CanonicalBody = canonical
	request.ModerationBody = antigravityImagesModerationBody(canonical)
	return request, nil
}

// parseAntigravityImagesJSONRequest reads the JSON field set the bridge accepts.
// Editing additionally requires exactly one source image, either as the canonical
// top-level image field or as the OpenAI images[].image_url entry.
func parseAntigravityImagesJSONRequest(endpoint string, body []byte, standard *OpenAIImagesRequest) error {
	standard.Model = strings.TrimSpace(gjson.GetBytes(body, "model").String())
	standard.Prompt = strings.TrimSpace(gjson.GetBytes(body, "prompt").String())
	standard.Size = strings.TrimSpace(gjson.GetBytes(body, "size").String())
	standard.ResponseFormat = strings.ToLower(strings.TrimSpace(gjson.GetBytes(body, "response_format").String()))
	if n := gjson.GetBytes(body, "n"); n.Exists() {
		if n.Type != gjson.Number {
			return errors.New("invalid n field type")
		}
		standard.N = int(n.Int())
	}
	if stream := gjson.GetBytes(body, "stream"); stream.Exists() && stream.Bool() {
		return errors.New("antigravity images do not support streaming")
	}
	if endpoint != openAIImagesEditsEndpoint {
		return nil
	}
	if gjson.GetBytes(body, "mask").Exists() {
		return errors.New("antigravity image edit does not support a mask")
	}
	imageURL := strings.TrimSpace(gjson.GetBytes(body, "image").String())
	if imageURL == "" {
		images := gjson.GetBytes(body, "images")
		if images.Exists() {
			if !images.IsArray() {
				return errors.New("invalid images field type")
			}
			if len(images.Array()) != 1 {
				return errors.New("antigravity image edit accepts a single source image")
			}
			entry := images.Array()[0]
			imageURL = strings.TrimSpace(entry.Get("image_url").String())
			if imageURL == "" && entry.Get("file_id").Exists() {
				return errors.New("images[].file_id is not supported (use images[].image_url with a base64 data URL)")
			}
		}
	}
	if imageURL == "" {
		return errors.New("antigravity image edit requires a source image")
	}
	standard.InputImageURLs = []string{imageURL}
	return nil
}

// antigravityImagesCanonicalBody reduces a parsed standard-entry request to the
// single JSON shape buildAntigravityImageActionRequest validates.
func antigravityImagesCanonicalBody(action string, standard *OpenAIImagesRequest) ([]byte, error) {
	if standard == nil {
		return nil, errors.New("images request is required")
	}
	payload := map[string]any{
		"model":  standard.Model,
		"prompt": standard.Prompt,
		"n":      standard.N,
	}
	if standard.Size != "" {
		payload["size"] = standard.Size
	}
	if standard.ResponseFormat != "" {
		payload["response_format"] = standard.ResponseFormat
	}
	if action != AntigravityImageActionEdit {
		return json.Marshal(payload)
	}
	image := ""
	switch {
	case len(standard.Uploads) > 1, len(standard.InputImageURLs) > 1:
		return nil, errors.New("antigravity image edit accepts a single source image")
	case len(standard.Uploads) == 1:
		image = standard.Uploads[0].ModerationDataURL()
		if image == "" {
			return nil, errors.New("antigravity image edit requires an image upload")
		}
	case len(standard.InputImageURLs) == 1:
		image = strings.TrimSpace(standard.InputImageURLs[0])
	}
	if image == "" {
		return nil, errors.New("antigravity image edit requires a source image")
	}
	payload["image"] = image
	return json.Marshal(payload)
}

// antigravityImagesModerationBody reduces the canonical body to the prompt and
// input image the security audit inspects, independent of the client transport.
func antigravityImagesModerationBody(canonical []byte) []byte {
	payload := map[string]any{}
	if prompt := strings.TrimSpace(gjson.GetBytes(canonical, "prompt").String()); prompt != "" {
		payload["prompt"] = prompt
	}
	if image := strings.TrimSpace(gjson.GetBytes(canonical, "image").String()); image != "" {
		payload["images"] = []any{map[string]any{"image_url": image}}
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

// buildAntigravityImageActionRequest validates the canonical JSON image request
// and converts it to the native Gemini generateContent body. Editing passes the
// input image as an inline image part alongside the edit instruction, which is
// the only image-editing shape the CloudCode surface supports.
func buildAntigravityImageActionRequest(action string, body []byte) (*AntigravityImagesRequest, error) {
	if action != AntigravityImageActionGenerate && action != AntigravityImageActionEdit {
		return nil, AntigravityUnsupportedActionError(action)
	}
	var request struct {
		Model          string `json:"model"`
		Prompt         string `json:"prompt"`
		ResponseFormat string `json:"response_format"`
		Count          int    `json:"n"`
		Size           string `json:"size"`
		Image          string `json:"image"`
	}
	if json.Unmarshal(body, &request) != nil || strings.TrimSpace(request.Model) == "" {
		return nil, errors.New("antigravity image request requires model and valid JSON")
	}
	prompt := strings.TrimSpace(request.Prompt)
	if prompt == "" {
		return nil, errors.New("antigravity image request requires prompt")
	}
	if request.Count < 0 || request.Count > antigravityImageMaxOutputCount {
		return nil, fmt.Errorf("antigravity image request supports n<=%d", antigravityImageMaxOutputCount)
	}
	if request.ResponseFormat != "" && request.ResponseFormat != "b64_json" {
		return nil, errors.New("antigravity image request supports response_format=b64_json")
	}

	parts := []any{map[string]any{"text": prompt}}
	hasInputImage := false
	if action == AntigravityImageActionEdit {
		mimeType, encoded, err := parseAntigravityImageDataURL(request.Image)
		if err != nil {
			return nil, err
		}
		parts = append(parts, map[string]any{
			"inlineData": map[string]any{"mimeType": mimeType, "data": encoded},
		})
		hasInputImage = true
	}

	generationConfig := map[string]any{"responseModalities": []string{"TEXT", "IMAGE"}}
	sizeTier := NormalizeImageBillingTierOrDefault(request.Size)
	if strings.TrimSpace(request.Size) != "" {
		generationConfig["imageConfig"] = map[string]any{"imageSize": sizeTier}
	}

	nativeBody, err := json.Marshal(map[string]any{
		"contents":         []any{map[string]any{"role": "user", "parts": parts}},
		"generationConfig": generationConfig,
	})
	if err != nil {
		return nil, err
	}
	return &AntigravityImagesRequest{
		Action:         action,
		Model:          request.Model,
		Prompt:         prompt,
		ResponseFormat: request.ResponseFormat,
		SizeTier:       sizeTier,
		HasInputImage:  hasInputImage,
		NativeBody:     nativeBody,
	}, nil
}

func parseAntigravityImageDataURL(value string) (string, string, error) {
	prefix, encoded, found := strings.Cut(value, ",")
	if !found || !strings.HasPrefix(prefix, "data:image/") || !strings.HasSuffix(prefix, ";base64") {
		return "", "", errors.New("antigravity image edit requires a base64 image data URL")
	}
	if _, err := base64.StdEncoding.DecodeString(encoded); err != nil {
		return "", "", errors.New("invalid image base64")
	}
	return strings.TrimSuffix(strings.TrimPrefix(prefix, "data:"), ";base64"), encoded, nil
}

// ForwardImages serves a standard-entry Antigravity image action on the account's
// own OAuth/project identity. It buffers the native generateContent response,
// converts it to the standard OpenAI images payload, and returns the forwarding
// result so ingress can price the generated image tier. The account must already
// be selected by ingress and the request must come from
// ParseAntigravityImagesRequest.
func (s *AntigravityGatewayService) ForwardImages(ctx context.Context, c *gin.Context, account *Account, request *AntigravityImagesRequest, options ...ForwardGeminiOption) (*ForwardResult, error) {
	if request == nil {
		_ = s.writeGoogleError(c, http.StatusBadRequest, "Antigravity image request is required")
		return nil, errors.New("antigravity image request is required")
	}
	mappedModel := s.getMappedModel(account, request.Model)
	if mappedModel == "" {
		MarkOpsClientBusinessLimited(c, OpsClientBusinessLimitedReasonLocalFeatureGate)
		_ = s.writeGoogleError(c, http.StatusForbidden, fmt.Sprintf("model %s not in whitelist", request.Model))
		return nil, fmt.Errorf("model %s not in whitelist", request.Model)
	}
	if !isImageGenerationModel(mappedModel) {
		_ = s.writeGoogleError(c, http.StatusBadRequest, fmt.Sprintf("model %s does not produce images", request.Model))
		return nil, fmt.Errorf("model %s does not produce images", request.Model)
	}

	originalWriter := c.Writer
	buffered := &geminiActionWriter{ResponseWriter: originalWriter, status: http.StatusOK}
	c.Writer = buffered
	defer func() { c.Writer = originalWriter }()

	result, forwardErr := s.ForwardGemini(ctx, c, account, request.Model, "generateContent", false, request.NativeBody, false, options...)
	c.Writer = originalWriter
	if forwardErr != nil || buffered.status >= http.StatusBadRequest {
		// Replay only a response the upstream path actually committed; a bare
		// error must not surface as an empty 200.
		if buffered.written || buffered.status != http.StatusOK {
			originalWriter.WriteHeader(buffered.status)
			if buffered.body.Len() > 0 {
				_, _ = originalWriter.Write(buffered.body.Bytes())
			}
		}
		return result, forwardErr
	}

	payload, contentType, err := convertGeminiActionResponse(request.Action, request.ResponseFormat, buffered.body.Bytes())
	if err != nil {
		// The upstream 200 carried no usable image part; the caller treats a
		// returned error as "response already committed", so write it explicitly.
		c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"type": "upstream_error", "message": "Antigravity upstream returned no usable image"}})
		return result, err
	}
	originalWriter.Header().Del("Content-Length")
	originalWriter.Header().Del("Content-Type")
	c.Data(http.StatusOK, contentType, payload)
	return result, nil
}
