package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// IsQianwenImageEditModel describes the native edit wire family, not account
// authorization. Selection and mapped-model admission remain in the gateway.
func IsQianwenImageEditModel(model string) bool {
	return model == "qwen-image-edit" || strings.HasPrefix(model, "qwen-image-edit-")
}

// BuildQianwenImageGenerationBody bridges the standard shape to native images.
// The compatible-mode 3.0 branch remains owned by ForwardQianwenImages.
func BuildQianwenImageGenerationBody(body []byte) ([]byte, error) {
	var request struct {
		Model          string `json:"model"`
		Prompt         string `json:"prompt"`
		Size           string `json:"size"`
		Count          *int   `json:"n"`
		ResponseFormat string `json:"response_format"`
		Stream         bool   `json:"stream"`
	}
	if err := json.Unmarshal(body, &request); err != nil {
		return nil, fmt.Errorf("invalid image generation JSON")
	}
	if request.Stream || (request.ResponseFormat != "" && request.ResponseFormat != "b64_json" && request.ResponseFormat != "url") {
		return nil, fmt.Errorf("unsupported image generation response format")
	}
	count := 1
	if request.Count != nil {
		count = *request.Count
	}
	return buildQianwenStandardImageBody(request.Model, request.Prompt, request.Size, count, nil)
}

func (s *OpenAIGatewayService) ForwardQianwenNativeImages(ctx context.Context, c *gin.Context, account *Account, body []byte, responseFormat string) (*OpenAIForwardResult, error) {
	model := account.GetMappedModel(gjson.GetBytes(body, "model").String())
	if !IsQianwenNativeImageModel(model) || IsQianwenImageEditModel(model) {
		err := fmt.Errorf("mapped model does not support the native image generation protocol")
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": gin.H{"type": "invalid_request_error", "code": "unsupported_image_generation_model", "message": err.Error()}})
		return nil, err
	}
	return s.forwardQianwenStandardNativeImage(ctx, c, account, body, responseFormat)
}

// BuildQianwenImageEditBody translates uploaded image data URIs into native
// multimodal content, retaining the public model until normal account mapping.
func BuildQianwenImageEditBody(model, prompt, size string, count int, images []string) ([]byte, error) {
	if len(images) < 1 || len(images) > 3 {
		return nil, fmt.Errorf("image editing requires one to three uploaded images")
	}
	return buildQianwenStandardImageBody(model, prompt, size, count, images)
}

func buildQianwenStandardImageBody(model, prompt, size string, count int, images []string) ([]byte, error) {
	if strings.TrimSpace(model) == "" || strings.TrimSpace(prompt) == "" {
		return nil, fmt.Errorf("model and prompt are required")
	}
	if count < 1 || count > 6 || (model == "qwen-image-edit" && count != 1) {
		return nil, fmt.Errorf("invalid number of output images")
	}
	parameters := map[string]any{"n": count}
	if size != "" && size != "auto" {
		parts := strings.Split(size, "x")
		if len(parts) != 2 {
			return nil, fmt.Errorf("size must be WxH")
		}
		for _, part := range parts {
			dimension, err := strconv.Atoi(part)
			if err != nil || dimension <= 0 {
				return nil, fmt.Errorf("size dimensions must be positive integers")
			}
		}
		parameters["size"] = strings.Join(parts, "*")
	}
	content := make([]map[string]string, 0, len(images)+1)
	for _, image := range images {
		content = append(content, map[string]string{"image": image})
	}
	content = append(content, map[string]string{"text": prompt})
	return json.Marshal(map[string]any{
		"model":      model,
		"input":      map[string]any{"messages": []any{map[string]any{"role": "user", "content": content}}},
		"parameters": parameters,
	})
}

// ForwardQianwenImageEdit uses the native synchronous lifecycle and its image
// usage observer. After confirmed generation, a failed media conversion still
// returns the observed billable result while the client receives an explicit 502.
// Failed native requests and unverified image usage remain unbillable.
func (s *OpenAIGatewayService) ForwardQianwenImageEdit(ctx context.Context, c *gin.Context, account *Account, body []byte) (*OpenAIForwardResult, error) {
	model := gjson.GetBytes(body, "model").String()
	mapped := account.GetMappedModel(model)
	if !IsQianwenImageEditModel(mapped) {
		err := fmt.Errorf("mapped model does not support the native image edit protocol")
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": gin.H{"type": "invalid_request_error", "code": "unsupported_image_edit_model", "message": err.Error()}})
		return nil, err
	}
	if mapped == "qwen-image-edit" && gjson.GetBytes(body, "parameters.n").Int() != 1 {
		err := fmt.Errorf("qwen-image-edit supports n=1")
		writeChatCompletionsError(c, http.StatusBadRequest, "invalid_image_edit_request", err.Error())
		return nil, err
	}
	return s.forwardQianwenStandardNativeImage(ctx, c, account, body, "b64_json")
}

func (s *OpenAIGatewayService) forwardQianwenStandardNativeImage(ctx context.Context, c *gin.Context, account *Account, body []byte, responseFormat string) (*OpenAIForwardResult, error) {
	started := time.Now()
	ctx, cancel := context.WithTimeout(ctx, qianwenNativeImageTimeout)
	defer cancel()
	// This existing synchronous writer also keeps native error responses from
	// committing until the bridge has decided whether conversion is possible.
	original := c.Writer
	buffered := &geminiActionWriter{ResponseWriter: original, status: http.StatusOK}
	c.Writer = buffered
	defer func() { c.Writer = original }()
	result, err := s.ForwardQianwenNative(ctx, c, account, QianwenNativeMultimodal, body, false)
	c.Writer = original
	if err != nil || result == nil || buffered.status >= http.StatusBadRequest {
		if buffered.written || buffered.status != http.StatusOK {
			original.WriteHeader(buffered.status)
			_, _ = original.Write(buffered.body.Bytes())
		} else {
			writeChatCompletionsError(c, http.StatusBadGateway, "upstream_error", "DashScope image request failed")
		}
		if err == nil {
			err = fmt.Errorf("DashScope image request failed")
		}
		return nil, err
	}
	if result.ImageCount < 1 || result.ImageCount > 6 {
		writeChatCompletionsError(c, http.StatusBadGateway, "upstream_error", "Unexpected number of DashScope image outputs")
		return nil, fmt.Errorf("unexpected DashScope image count")
	}
	items := make([]map[string]string, 0, result.ImageCount)
	for _, choice := range gjson.GetBytes(buffered.body.Bytes(), "output.choices").Array() {
		for _, part := range choice.Get("message.content").Array() {
			image := part.Get("image").String()
			if strings.TrimSpace(image) == "" {
				continue
			}
			if responseFormat == "url" {
				items = append(items, map[string]string{"url": image})
				continue
			}
			encoded, err := s.fetchOpenAIImageURLBase64(withQianwenResultLookup(ctx, image), account, image)
			if err == nil && strings.HasPrefix(strings.ToLower(image), "data:") {
				decoded, decodeErr := base64.StdEncoding.DecodeString(encoded)
				if decodeErr != nil || !isBackfillImageContent(decoded) {
					err = fmt.Errorf("invalid inline image output")
				}
			}
			if err != nil {
				writeChatCompletionsError(c, http.StatusBadGateway, "upstream_error", "Failed to retrieve DashScope image output")
				result.Duration = time.Since(started)
				return result, nil
			}
			items = append(items, map[string]string{"b64_json": encoded})
		}
	}
	original.Header().Del("Content-Length")
	original.Header().Del("Content-Type")
	c.JSON(http.StatusOK, gin.H{"created": time.Now().Unix(), "data": items})
	result.Duration = time.Since(started)
	return result, nil
}
