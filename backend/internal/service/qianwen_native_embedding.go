package service

import (
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
)

// Native synchronous text embedding on
// /api/v1/services/embeddings/text-embedding/text-embedding.
// Shapes and limits verified against the official synchronous text-embedding
// doc, 2026-09-22: https://www.alibabacloud.com/help/en/model-studio/text-embedding-synchronous-api
// Beijing list prices (CNY/1M tokens): qwen3.7-text-embedding 0.5,
// qwen3.7-text-embedding-flash 0.125, text-embedding-v4 0.5, text-embedding-v3
// 0.5 (free-quota-gated upstream after exhaustion). Multimodal embedding uses
// a different endpoint with contents[] and is a separate adapter.
const QianwenNativeEmbeddingPath = "/api/v1/services/embeddings/text-embedding/text-embedding"

func isQianwenNativeEmbeddingModel(model string) bool {
	switch model {
	case "text-embedding-v3", "text-embedding-v4", "qwen3.7-text-embedding", "qwen3.7-text-embedding-flash":
		return true
	}
	return false
}

// IsQianwenCompatibleEmbeddingModel bounds the compatible-mode /embeddings
// bridge to the models the official OpenAI-compatibility page documents
// (2026-09-22); other embedding families use the native text-embedding path.
func IsQianwenCompatibleEmbeddingModel(model string) bool {
	return model == "text-embedding-v3" || model == "text-embedding-v4"
}

func qianwenNativeEmbeddingMaxTexts(model string) int {
	if strings.HasPrefix(model, "qwen3.7-text-embedding") {
		return 20
	}
	return 10
}

func parseQianwenNativeEmbeddingBody(model string, body []byte) (bool, error) {
	if !isQianwenNativeEmbeddingModel(model) && !strings.Contains(strings.ToLower(model), "text-embedding") {
		return false, nil
	}
	return true, validateQianwenNativeEmbeddingBody(model, body)
}

func validateQianwenNativeEmbeddingBody(model string, body []byte) error {
	if !isQianwenNativeEmbeddingModel(model) {
		return fmt.Errorf("unsupported DashScope embedding model: %s", model)
	}
	if !gjson.ValidBytes(body) {
		return fmt.Errorf("invalid DashScope embedding JSON")
	}
	texts := gjson.GetBytes(body, "input.texts")
	if !texts.IsArray() {
		return fmt.Errorf("DashScope embedding requires input.texts")
	}
	items := texts.Array()
	if len(items) == 0 || len(items) > qianwenNativeEmbeddingMaxTexts(model) {
		return fmt.Errorf("DashScope embedding %s supports 1-%d texts per request", model, qianwenNativeEmbeddingMaxTexts(model))
	}
	for _, item := range items {
		if item.Type != gjson.String || len(item.String()) == 0 || len([]rune(item.String())) > 64000 {
			return fmt.Errorf("DashScope embedding texts must be non-empty strings")
		}
	}
	if textType := gjson.GetBytes(body, "parameters.text_type"); textType.Exists() && (textType.Type != gjson.String || (textType.String() != "query" && textType.String() != "document")) {
		return fmt.Errorf("DashScope embedding parameters.text_type must be query or document")
	}
	if dimension := gjson.GetBytes(body, "parameters.dimension"); dimension.Exists() {
		if dimension.Type != gjson.Number || dimension.Float() != float64(dimension.Int()) {
			return fmt.Errorf("DashScope embedding parameters.dimension must be an integer")
		}
		switch dimension.Int() {
		case 64, 128, 256, 512, 768, 1024, 1536, 2048, 2560:
		default:
			return fmt.Errorf("unsupported DashScope embedding dimension")
		}
		if dimension.Int() == 2560 && model != "qwen3.7-text-embedding" {
			return fmt.Errorf("DashScope embedding dimension 2560 requires qwen3.7-text-embedding")
		}
		if (dimension.Int() == 1536 || dimension.Int() == 2048) && model != "text-embedding-v4" {
			return fmt.Errorf("DashScope embedding dimension %d requires text-embedding-v4", dimension.Int())
		}
	}
	if outputType := gjson.GetBytes(body, "parameters.output_type"); outputType.Exists() && (outputType.Type != gjson.String || (outputType.String() != "dense" && outputType.String() != "sparse" && outputType.String() != "dense&sparse")) {
		return fmt.Errorf("DashScope embedding parameters.output_type must be dense, sparse or dense&sparse")
	}
	if instruct := gjson.GetBytes(body, "parameters.instruct"); instruct.Exists() {
		if instruct.Type != gjson.String || strings.TrimSpace(instruct.String()) == "" {
			return fmt.Errorf("DashScope embedding parameters.instruct must be non-empty text")
		}
		if gjson.GetBytes(body, "parameters.text_type").String() != "query" {
			return fmt.Errorf("DashScope embedding parameters.instruct requires text_type=query")
		}
	}
	unknown := false
	gjson.GetBytes(body, "input").ForEach(func(k, _ gjson.Result) bool {
		if k.String() != "texts" {
			unknown = true
		}
		return true
	})
	gjson.GetBytes(body, "parameters").ForEach(func(k, _ gjson.Result) bool {
		switch k.String() {
		case "text_type", "dimension", "output_type", "instruct":
		default:
			unknown = true
		}
		return true
	})
	if unknown {
		return fmt.Errorf("unsupported DashScope embedding input or parameter field")
	}
	return nil
}

func validateQianwenNativeEmbeddingOperation(operation QianwenNativeOperation, model string, body []byte, stream bool) (bool, error) {
	embeddingModel := isQianwenNativeEmbeddingModel(model)
	if operation == QianwenNativeEmbedding {
		if !embeddingModel {
			return true, fmt.Errorf("DashScope embedding endpoint requires a text embedding model, got %s", model)
		}
		if stream || gjson.GetBytes(body, "stream").Bool() {
			return true, fmt.Errorf("DashScope embedding does not support streaming")
		}
		return true, validateQianwenNativeEmbeddingBody(model, body)
	}
	if embeddingModel {
		return true, fmt.Errorf("DashScope embedding model %s requires the text-embedding endpoint", model)
	}
	return false, nil
}

// observeQianwenNativeEmbedding maps the documented usage.total_tokens onto
// input tokens so the shared token pipeline prices the request exactly like an
// embedding card import (Beijing list: 0.5/0.125 CNY per 1M tokens, checked
// 2026-09-22).
func observeQianwenNativeEmbedding(body []byte, result *OpenAIForwardResult) error {
	if !gjson.ValidBytes(body) {
		return fmt.Errorf("invalid DashScope embedding response JSON")
	}
	if err := rejectQianwenNativeAsyncResponse(body); err != nil {
		return err
	}
	if id := gjson.GetBytes(body, "request_id").String(); id != "" {
		result.RequestID = id
	}
	if code := gjson.GetBytes(body, "code").String(); code != "" {
		return fmt.Errorf("DashScope embedding response error: %s", code)
	}
	if status := gjson.GetBytes(body, "status_code"); status.Exists() && (status.Type != gjson.Number || status.Float() != 200) {
		return fmt.Errorf("DashScope embedding response reports an unsuccessful status")
	}
	embeddings := gjson.GetBytes(body, "output.embeddings")
	if !embeddings.IsArray() || len(embeddings.Array()) == 0 {
		return fmt.Errorf("DashScope embedding response requires output.embeddings")
	}
	for _, item := range embeddings.Array() {
		if item.Get("text_index").Type != gjson.Number || (!item.Get("embedding").IsArray() && !item.Get("sparse_embedding").IsArray()) {
			return fmt.Errorf("DashScope embedding output lacks text_index or embedding vector")
		}
	}
	totalTokens := gjson.GetBytes(body, "usage.total_tokens")
	if totalTokens.Type != gjson.Number || totalTokens.Float() != float64(totalTokens.Int()) || totalTokens.Int() < 0 || totalTokens.Int() > 1<<53 {
		return fmt.Errorf("DashScope embedding response requires integral usage.total_tokens")
	}
	result.Usage = OpenAIUsage{InputTokens: int(totalTokens.Int())}
	return nil
}
