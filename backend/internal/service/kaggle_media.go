package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// Kaggle notebook image API (02_qwen-image_api.ipynb): POST /v1/images/generations,
// OpenAI-shaped JSON with response_format=b64_json. The upstream may prefix the
// JSON with keepalive whitespace so a Cloudflare tunnel does not hit its
// time-to-first-byte limit.
func prepareKaggleImage(body []byte) ([]byte, error) {
	if !gjson.ValidBytes(body) || !gjson.ParseBytes(body).IsObject() {
		return nil, fmt.Errorf("a JSON object is required")
	}
	if model := gjson.GetBytes(body, "model"); model.Type != gjson.String || strings.TrimSpace(model.String()) == "" {
		return nil, fmt.Errorf("model must be a nonempty string")
	}
	if prompt := gjson.GetBytes(body, "prompt"); prompt.Type != gjson.String || strings.TrimSpace(prompt.String()) == "" {
		return nil, fmt.Errorf("prompt is required")
	}
	if format := gjson.GetBytes(body, "response_format"); format.Exists() && format.String() != "b64_json" {
		return nil, fmt.Errorf("only response_format='b64_json' is supported")
	}
	if count := gjson.GetBytes(body, "n"); count.Exists() && (count.Type != gjson.Number || count.Int() < 1 || count.Int() > 4 || count.Float() != float64(count.Int())) {
		return nil, fmt.Errorf("n must be an integer from 1 to 4")
	}
	if size := gjson.GetBytes(body, "size"); size.Exists() && size.Type != gjson.String {
		return nil, fmt.Errorf("size must look like '1024x1024'")
	}
	return body, nil
}

func trimJSONPadding(raw []byte) []byte {
	return bytes.TrimSpace(raw)
}

func (gateway *OpenAIGatewayService) ForwardKaggleMedia(ctx context.Context, request *gin.Context, account *Account, action string, body []byte, contentType string) (*OpenAIForwardResult, error) {
	started := time.Now()
	if action != "image.generate" || account == nil || account.Platform != PlatformKaggle || account.Type != AccountTypeAPIKey {
		return nil, fmt.Errorf("kaggle API-key image account required")
	}
	original := strings.TrimSpace(gjson.GetBytes(body, "model").String())
	prepared, err := prepareKaggleImage(body)
	if err != nil || original == "" {
		if err == nil {
			err = fmt.Errorf("model is required")
		}
		writeOpenAIEmbeddingsError(request, 400, "invalid_request_error", err.Error())
		return nil, err
	}
	model := resolveOpenAIForwardModel(account, original, "")
	SetOpsUpstreamModel(request, model)
	outbound := ReplaceModelInBody(prepared, model)
	base, err := gateway.validateUpstreamBaseURL(account.GetOpenAIBaseURL())
	if err != nil {
		return nil, err
	}
	token, _, err := gateway.getRequestCredential(ctx, request, account)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("kaggle key is missing")
	}
	path := "/v1/images/generations"
	response, err := gateway.sendMistralActionRequest(WithHTTPUpstreamRedirectsDisabled(ctx), request, account, http.MethodPost, account.buildOpenAIEndpointURL(base, path), outbound, "application/json", "application/json", token, path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := response.Body.Close(); err != nil {
			slog.Debug("provider response cleanup failed")
		}
	}()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return gateway.handleMistralActionHTTPError(ctx, request, account, response, model)
	}
	raw, err := ReadUpstreamResponseBody(response.Body, gateway.cfg, request, openAITooLargeError)
	if err != nil {
		return nil, err
	}
	raw = trimJSONPadding(raw)
	result := &OpenAIForwardResult{RequestID: response.Header.Get("x-request-id"), UpstreamHeaders: response.Header, Model: original, BillingModel: model, UpstreamModel: model, Duration: time.Since(started)}
	invalid := func(reason string) (*OpenAIForwardResult, error) {
		writeOpenAIEmbeddingsError(request, 502, "upstream_error", reason)
		return nil, fmt.Errorf("%s", reason)
	}
	if !gjson.ValidBytes(raw) || gjson.GetBytes(raw, "error").Exists() {
		return invalid("Invalid Kaggle image response")
	}
	data := gjson.GetBytes(raw, "data")
	if !data.IsArray() || len(data.Array()) == 0 {
		return invalid("Image result is empty")
	}
	outputSizes := []string{}
	for _, entry := range data.Array() {
		decoded, decodeErr := base64.StdEncoding.DecodeString(entry.Get("b64_json").String())
		if decodeErr != nil || len(decoded) == 0 {
			return invalid("Image result has invalid base64")
		}
		dimensions, _, err := image.DecodeConfig(bytes.NewReader(decoded))
		if err != nil {
			return invalid("Image result is not a supported raster image")
		}
		outputSizes = append(outputSizes, fmt.Sprintf("%dx%d", dimensions.Width, dimensions.Height))
	}
	result.ImageCount = len(data.Array())
	requestedSize := gjson.GetBytes(body, "size").String()
	if requestedSize == "" {
		if w, h := gjson.GetBytes(raw, "usage.width").Int(), gjson.GetBytes(raw, "usage.height").Int(); w > 0 && h > 0 {
			requestedSize = fmt.Sprintf("%dx%d", w, h)
		}
	}
	resolution := ResolveImageBillingSize(requestedSize, outputSizes)
	result.ImageSize, result.ImageSizeSource = resolution.BillingSize, resolution.Source
	result.ImageInputSize, result.ImageOutputSize = resolution.InputSize, resolution.OutputSize
	result.ImageOutputSizes, result.ImageSizeBreakdown = outputSizes, resolution.Breakdown
	writeOpenAIEmbeddingsUpstreamResponse(request, response, raw, gateway.responseHeaderFilter)
	return result, nil
}
