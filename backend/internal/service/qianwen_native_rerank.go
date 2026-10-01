package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// Native text rerank on /api/v1/services/rerank/text-rerank/text-rerank.
// Models and shapes verified against the official Text Rerank API doc,
// 2026-09-22: https://www.alibabacloud.com/help/en/model-studio/text-rerank-api
// gte-rerank (v1) was discontinued 2026-05-30 and is not admitted.
// qwen3-rerank uses a different flat compatible-api shape whose legacy-host
// path is undocumented; it stays out of this adapter. qwen3-vl-rerank is
// admitted for text-only queries/documents; image/video documents have their
// own pricing dimensions and are rejected here.
const QianwenNativeRerankPath = "/api/v1/services/rerank/text-rerank/text-rerank"

func isQianwenNativeRerankModel(model string) bool {
	switch model {
	case "gte-rerank-v2", "qwen3-vl-rerank":
		return true
	}
	return false
}

func parseQianwenNativeRerankBody(model string, body []byte) (bool, error) {
	if !strings.Contains(strings.ToLower(model), "rerank") {
		return false, nil
	}
	return true, validateQianwenNativeRerankBody(model, body)
}

func validateQianwenNativeRerankBody(model string, body []byte) error {
	if !isQianwenNativeRerankModel(model) {
		return fmt.Errorf("unsupported DashScope rerank model: %s", model)
	}
	if !gjson.ValidBytes(body) {
		return fmt.Errorf("invalid DashScope rerank JSON")
	}
	query := gjson.GetBytes(body, "input.query")
	if query.Type != gjson.String || strings.TrimSpace(query.String()) == "" || len([]rune(query.String())) > 16000 {
		return fmt.Errorf("DashScope rerank requires input.query text")
	}
	documents := gjson.GetBytes(body, "input.documents")
	if !documents.IsArray() {
		return fmt.Errorf("DashScope rerank requires input.documents")
	}
	docs := documents.Array()
	maxDocs := 500
	if model == "qwen3-vl-rerank" {
		maxDocs = 100
	}
	if len(docs) == 0 || len(docs) > maxDocs {
		return fmt.Errorf("DashScope rerank %s supports 1-%d text documents", model, maxDocs)
	}
	for _, doc := range docs {
		if doc.Type != gjson.String || len([]rune(doc.String())) == 0 || len([]rune(doc.String())) > 16000 {
			return fmt.Errorf("DashScope rerank documents must be non-empty text; multimedia documents need their own adapter")
		}
	}
	if topN := gjson.GetBytes(body, "parameters.top_n"); topN.Exists() && (topN.Type != gjson.Number || topN.Float() != float64(topN.Int()) || topN.Int() < 1 || topN.Int() > int64(len(docs))) {
		return fmt.Errorf("DashScope rerank parameters.top_n must be an integer within the document count")
	}
	if returnDocuments := gjson.GetBytes(body, "parameters.return_documents"); returnDocuments.Exists() && returnDocuments.Type != gjson.True && returnDocuments.Type != gjson.False {
		return fmt.Errorf("DashScope rerank parameters.return_documents must be boolean")
	}
	if instruct := gjson.GetBytes(body, "parameters.instruct"); instruct.Exists() {
		if model != "qwen3-vl-rerank" || instruct.Type != gjson.String || strings.TrimSpace(instruct.String()) == "" {
			return fmt.Errorf("DashScope rerank parameters.instruct requires qwen3-vl-rerank and English text")
		}
	}
	unknown := false
	gjson.GetBytes(body, "input").ForEach(func(k, _ gjson.Result) bool {
		if k.String() != "query" && k.String() != "documents" {
			unknown = true
		}
		return true
	})
	gjson.GetBytes(body, "parameters").ForEach(func(k, _ gjson.Result) bool {
		switch k.String() {
		case "top_n", "return_documents", "instruct":
		default:
			unknown = true
		}
		return true
	})
	if unknown {
		return fmt.Errorf("unsupported DashScope rerank input or parameter field")
	}
	return nil
}

func validateQianwenNativeRerankOperation(operation QianwenNativeOperation, model string, body []byte, stream bool) (bool, error) {
	rerankModel := isQianwenNativeRerankModel(model)
	if operation == QianwenNativeRerank {
		if !rerankModel {
			return true, fmt.Errorf("DashScope rerank endpoint requires a rerank model, got %s", model)
		}
		if stream || gjson.GetBytes(body, "stream").Bool() {
			return true, fmt.Errorf("DashScope rerank does not support streaming")
		}
		return true, validateQianwenNativeRerankBody(model, body)
	}
	if rerankModel {
		return true, fmt.Errorf("DashScope rerank model %s requires the rerank endpoint", model)
	}
	return false, nil
}

// ForwardQianwenRerank bridges the standard flat rerank shape
// ({model, query, documents[], top_n?, return_documents?}) onto the native
// gte-rerank-v2/qwen3-vl-rerank endpoint and converts the response back to the
// flat shape ({model, results[], usage.total_tokens, id}). Document echo is
// always requested upstream so callers get faithful document content.
func (s *OpenAIGatewayService) ForwardQianwenRerank(ctx context.Context, c *gin.Context, account *Account, body []byte) (*OpenAIForwardResult, error) {
	started := time.Now()
	clientCtx := ctx
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	model, _, err := parseQianwenFlatRerankBody(body)
	if err != nil {
		writeChatCompletionsError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return nil, err
	}
	mapped := account.GetMappedModel(model)
	if !isQianwenNativeRerankModel(mapped) {
		err := fmt.Errorf("unsupported DashScope rerank model: %s", mapped)
		writeChatCompletionsError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return nil, err
	}
	nativeBody, err := buildQianwenNativeRerankBody(mapped, body)
	if err != nil {
		writeChatCompletionsError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return nil, err
	}
	req, err := s.BuildQianwenNativeRequest(ctx, account, QianwenNativeRerank, nativeBody, false)
	if err != nil {
		return nil, err
	}
	SetActualOpenAIUpstreamEndpoint(c, req.URL.Path)
	SetOpsUpstreamModel(c, mapped)
	accountProxyURL, egressErr := account.EgressProxyURL(ctx)
	if egressErr != nil {
		return nil, egressErr
	}
	resp, err := s.doOpenAIUpstream(req, accountProxyURL, account)
	if err != nil {
		return nil, s.handleOpenAIUpstreamTransportError(ctx, c, account, err, false)
	}
	defer func() { cancel(); _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return s.handleChatCompletionsErrorResponse(resp, c, account, model)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, qianwenNativeMaxFrame+1))
	if err != nil {
		return nil, err
	}
	if len(data) > qianwenNativeMaxFrame {
		return nil, fmt.Errorf("DashScope rerank response exceeds size limit")
	}
	result := &OpenAIForwardResult{Model: model, BillingModel: mapped, UpstreamModel: mapped, UpstreamEndpoint: req.URL.Path, UpstreamHeaders: resp.Header.Clone(), RequestID: resp.Header.Get("X-Request-Id")}
	if err := observeQianwenNativeRerank(data, result); err != nil {
		return nil, err
	}
	flat, err := buildQianwenFlatRerankResponse(data, model, result.Usage.InputTokens)
	if err != nil {
		return nil, err
	}
	c.Data(resp.StatusCode, "application/json", flat)
	result.Duration = time.Since(started)
	result.ClientDisconnect = clientCtx.Err() != nil
	return result, nil
}

// parseQianwenFlatRerankBody validates the standard flat rerank shape and
// returns the model and document count for top_n bounds.
func parseQianwenFlatRerankBody(body []byte) (string, int, error) {
	if !gjson.ValidBytes(body) {
		return "", 0, fmt.Errorf("invalid rerank JSON")
	}
	model := gjson.GetBytes(body, "model").String()
	query := gjson.GetBytes(body, "query")
	if query.Type != gjson.String || strings.TrimSpace(query.String()) == "" || len([]rune(query.String())) > 16000 {
		return "", 0, fmt.Errorf("rerank requires a non-empty query")
	}
	documents := gjson.GetBytes(body, "documents")
	if !documents.IsArray() || len(documents.Array()) == 0 || len(documents.Array()) > 500 {
		return "", 0, fmt.Errorf("rerank requires 1-500 documents")
	}
	for _, doc := range documents.Array() {
		if doc.Type != gjson.String || len([]rune(doc.String())) == 0 || len([]rune(doc.String())) > 16000 {
			return "", 0, fmt.Errorf("rerank documents must be non-empty text")
		}
	}
	docCount := len(documents.Array())
	if topN := gjson.GetBytes(body, "top_n"); topN.Exists() && (topN.Type != gjson.Number || topN.Float() != float64(topN.Int()) || topN.Int() < 1 || topN.Int() > int64(docCount)) {
		return "", 0, fmt.Errorf("rerank top_n must be an integer within the document count")
	}
	if returnDocuments := gjson.GetBytes(body, "return_documents"); returnDocuments.Exists() && returnDocuments.Type != gjson.True && returnDocuments.Type != gjson.False {
		return "", 0, fmt.Errorf("rerank return_documents must be boolean")
	}
	unknown := false
	gjson.ParseBytes(body).ForEach(func(k, _ gjson.Result) bool {
		switch k.String() {
		case "model", "query", "documents", "top_n", "return_documents":
		default:
			unknown = true
		}
		return true
	})
	if unknown {
		return "", 0, fmt.Errorf("unsupported rerank request field")
	}
	return model, docCount, nil
}

func buildQianwenNativeRerankBody(mapped string, flat []byte) ([]byte, error) {
	var payload map[string]any
	if err := json.Unmarshal(flat, &payload); err != nil {
		return nil, fmt.Errorf("invalid rerank JSON")
	}
	native := map[string]any{
		"model": mapped,
		"input": map[string]any{
			"query":     payload["query"],
			"documents": payload["documents"],
		},
		"parameters": map[string]any{"return_documents": true},
	}
	parameters, _ := native["parameters"].(map[string]any)
	if topN, ok := payload["top_n"]; ok {
		parameters["top_n"] = topN
	}
	if instruct, ok := payload["instruct"].(string); ok && instruct != "" && mapped == "qwen3-vl-rerank" {
		parameters["instruct"] = instruct
	}
	return json.Marshal(native)
}

// buildQianwenFlatRerankResponse converts the native output.results wire back
// to the flat rerank shape, keeping index/relevance_score ordering and any
// returned document content.
func buildQianwenFlatRerankResponse(native []byte, model string, totalTokens int) ([]byte, error) {
	results := gjson.GetBytes(native, "output.results")
	if !results.IsArray() {
		return nil, fmt.Errorf("DashScope rerank response requires output.results")
	}
	flat := map[string]any{
		"model":   model,
		"results": json.RawMessage(results.Raw),
		"usage":   map[string]any{"total_tokens": totalTokens},
	}
	if id := gjson.GetBytes(native, "request_id").String(); id != "" {
		flat["id"] = id
	}
	return json.Marshal(flat)
}

// observeQianwenNativeRerank preserves the provider wire shape and maps the
// documented usage.total_tokens onto input tokens for the shared token billing
// pipeline (Beijing list prices: gte-rerank-v2 0.8, qwen3-vl-rerank 0.5
// CNY/1M tokens, checked 2026-09-22).
func observeQianwenNativeRerank(body []byte, result *OpenAIForwardResult) error {
	if !gjson.ValidBytes(body) {
		return fmt.Errorf("invalid DashScope rerank response JSON")
	}
	if err := rejectQianwenNativeAsyncResponse(body); err != nil {
		return err
	}
	if id := gjson.GetBytes(body, "request_id").String(); id != "" {
		result.RequestID = id
	}
	if code := gjson.GetBytes(body, "code").String(); code != "" {
		return fmt.Errorf("DashScope rerank response error: %s", code)
	}
	results := gjson.GetBytes(body, "output.results")
	if !results.IsArray() {
		return fmt.Errorf("DashScope rerank response requires output.results")
	}
	for _, item := range results.Array() {
		if item.Get("index").Type != gjson.Number || item.Get("relevance_score").Type != gjson.Number {
			return fmt.Errorf("DashScope rerank result lacks index or relevance_score")
		}
	}
	totalTokens := gjson.GetBytes(body, "usage.total_tokens")
	if totalTokens.Type != gjson.Number || totalTokens.Float() != float64(totalTokens.Int()) || totalTokens.Int() < 0 || totalTokens.Int() > 1<<53 {
		return fmt.Errorf("DashScope rerank response requires integral usage.total_tokens")
	}
	result.Usage = OpenAIUsage{InputTokens: int(totalTokens.Int())}
	return nil
}
