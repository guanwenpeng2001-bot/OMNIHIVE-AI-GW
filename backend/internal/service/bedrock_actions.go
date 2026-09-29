package service

// Bedrock native media and vector actions beyond the text trunks.
//
// Protocol evidence: first-party Amazon Bedrock documentation captured
// 2026-09-21 (API references for Amazon Bedrock Runtime, Agents for Amazon
// Bedrock Runtime and the Amazon Bedrock control plane, the botocore service-2
// signing metadata, and the per-family model parameter pages).
//   - Amazon Bedrock Runtime operation list
//     (API_Operations_Amazon_Bedrock_Runtime): ApplyGuardrail, Converse,
//     ConverseStream, CountTokens, GetAsyncInvoke, InvokeGuardrailChecks,
//     InvokeModel, InvokeModelWithBidirectionalStream,
//     InvokeModelWithResponseStream, ListAsyncInvokes, StartAsyncInvoke.
//     There is no text-to-speech, speech-to-text, translation, voice or
//     standalone search operation, so those cells cannot be served here.
//   - CountTokens: POST /model/{modelId}/count-tokens with
//     {"input":{"invokeModel":{"body":<base64>}}} or {"input":{"converse":{...}}},
//     answering {"inputTokens":<integer>}.
//   - Rerank (Agents for Amazon Bedrock Runtime, POST /rerank): queries fixed to
//     one entry with type TEXT, sources[N<=1000] with type INLINE and an inline
//     text document, rerankingConfiguration type BEDROCK_RERANKING_MODEL with a
//     model ARN and 1..1000 results; response results[].{index,relevanceScore,
//     document}. Reranking is priced per query and the response carries no token
//     counters, so no usage is invented here.
//   - Embeddings: Amazon Titan Text Embeddings V2
//     ({"inputText","dimensions":1024|512|256,"normalize","embeddingTypes"}) and
//     G1 ({"inputText"}) answer
//     {"embedding":[...],"inputTextTokenCount":N}. Cohere Embed families are
//     deliberately unsupported: AWS publishes no token usage for their
//     responses, so the gateway could not settle a price honestly.
//   - Images: InvokeModel with a per-family task body. Amazon Nova Canvas,
//     Amazon Titan Image Generator G1 and Stability AI answer
//     {"images":[base64,...]} plus their own filtering signal
//     (Nova/Titan: "error"; Stability: non-null "finish_reasons").
//   - Video: Nova Reel is asynchronous only (StartAsyncInvoke/GetAsyncInvoke,
//     text-to-video and image-to-video, 1280x720 in 6 second increments up to
//     120s) and requires outputDataConfig.s3OutputDataConfig.s3Uri; the async
//     job adapter and its account credential are an open integration item
//     (domain/bedrock_actions.go records the cell as unverified), not implemented
//     here.
//
// Every function here dispatches on the serving family and keeps a non-Claude
// body away from the Claude parser.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

const (
	bedrockRuntimeServiceName      = "bedrock-runtime"
	bedrockAgentRuntimeServiceName = "bedrock-agent-runtime"
	// Bedrock Runtime and Agents for Amazon Bedrock Runtime both sign with the
	// SigV4 service name "bedrock" (botocore service-2 metadata signingName,
	// checked 2026-09-21), so one signer serves both endpoints.
	bedrockSigV4ServiceName = "bedrock"

	bedrockCountTokensOperation = "count-tokens"
	bedrockInvokeOperation      = "invoke"
	bedrockRerankPath           = "/rerank"

	bedrockResponseReadLimit = 64 << 20
	// Titan text embeddings take one inputText per InvokeModel call; the bound
	// keeps a single client request from fanning out unbounded upstream calls.
	bedrockMaxEmbeddingInputs = 128
	// Documented RerankSource and numberOfResults maxima.
	bedrockMaxRerankSources = 1000
	bedrockMaxRerankResults = 1000
	// Documented per text cap shared by the rerank query and its documents.
	bedrockMaxRerankTextRune = 16000
)

// bedrockServiceEndpoint builds the regional endpoint for one Bedrock service.
// It mirrors bedrockEndpoint (bedrock_catalog.go), which covers the control
// plane and the runtime; the agents runtime follows the same region rules.
func bedrockServiceEndpoint(region, service string) (string, error) {
	if !bedrockRegionPattern.MatchString(region) {
		return "", fmt.Errorf("invalid Bedrock region")
	}
	suffix := "amazonaws.com"
	if strings.HasPrefix(region, "cn-") {
		suffix += ".cn"
	}
	return "https://" + service + "." + region + "." + suffix, nil
}

func bedrockModelTargetURL(region, target, operation string) (string, error) {
	endpoint, err := bedrockServiceEndpoint(region, bedrockRuntimeServiceName)
	if err != nil {
		return "", err
	}
	return endpoint + "/model/" + bedrockEscapeTarget(target) + "/" + operation, nil
}

// bedrockActionAccount confines the media and vector adapters to Bedrock accounts.
func bedrockActionAccount(account *Account) error {
	if account == nil || !account.IsBedrock() {
		return fmt.Errorf("bedrock action requires a bedrock account")
	}
	return nil
}

// applyBedrockActionAuth mirrors the existing Bedrock trunk: an account that
// carries a runtime API key uses Bearer, every other account is SigV4 signed
// with the given service name.
func applyBedrockActionAuth(ctx context.Context, account *Account, request *http.Request, payload []byte, service string) error {
	if apiKey := account.GetCredential("api_key"); apiKey != "" && (!account.UsesBedrockCatalog() || account.IsBedrockAPIKey()) {
		request.Header.Set("Authorization", "Bearer "+apiKey)
		return nil
	}
	signer, err := NewBedrockSignerFromAccount(account)
	if err != nil {
		return err
	}
	return signer.SignRequestForService(ctx, request, payload, service)
}

// sendBedrockActionRequest forwards one signed Bedrock action request on the
// shared transport. Client cancellation and the original deadline are retained
// (detachUpstreamContext is a pass-through), matching every other action
// adapter; no adapter-side retry is performed.
func (s *OpenAIGatewayService) sendBedrockActionRequest(ctx context.Context, account *Account, method, targetURL string, payload []byte, contentType, accept, service string) (*http.Response, error) {
	upstreamCtx, releaseUpstreamCtx := detachUpstreamContext(ctx)
	request, err := http.NewRequestWithContext(upstreamCtx, method, targetURL, bytes.NewReader(payload))
	releaseUpstreamCtx()
	if err != nil {
		return nil, fmt.Errorf("build bedrock upstream request: %w", err)
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	if accept != "" {
		request.Header.Set("Accept", accept)
	}
	if err := applyBedrockActionAuth(request.Context(), account, request, payload, service); err != nil {
		return nil, err
	}
	return generationDo(s.httpUpstream, request, upstreamModelsProxyURL(account), account.ID, account.Concurrency)
}

// handleBedrockActionHTTPError reuses the OpenAI-shaped compat error chain so a
// Bedrock media or vector failure keeps the same failover classification and
// client error contract as every other OpenAI-shaped action adapter.
func (s *OpenAIGatewayService) handleBedrockActionHTTPError(ctx context.Context, c *gin.Context, account *Account, resp *http.Response, billingModel string) (*OpenAIForwardResult, error) {
	respBody, upstreamMsg := s.readOpenAIUpstreamError(resp)
	if failoverErr := s.failoverOpenAIUpstreamHTTPError(ctx, c, account, resp, respBody, upstreamMsg, billingModel); failoverErr != nil {
		return nil, failoverErr
	}
	return s.handleCompatErrorResponse(resp, c, account, writeOpenAIEmbeddingsError, billingModel)
}

func readBedrockActionBody(resp *http.Response) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(resp.Body, bedrockResponseReadLimit+1))
	if err != nil {
		return nil, fmt.Errorf("read bedrock response: %w", err)
	}
	if len(body) > bedrockResponseReadLimit {
		return nil, fmt.Errorf("bedrock response exceeds limit")
	}
	return body, nil
}

// bedrockUpstreamMessage extracts the AWS error message from a JSON error body.
func bedrockUpstreamMessage(body []byte) string {
	if message := strings.TrimSpace(gjson.GetBytes(body, "message").String()); message != "" {
		return sanitizeUpstreamErrorMessage(message)
	}
	return sanitizeUpstreamErrorMessage(strings.TrimSpace(string(body)))
}

func bedrockRequestID(resp *http.Response) string {
	return firstNonEmptyString(resp.Header.Get("x-amzn-requestid"), resp.Header.Get("x-request-id"))
}

// ---------------------------------------------------------------------------
// count_tokens
// ---------------------------------------------------------------------------

// ForwardBedrockCountTokens counts an Anthropic Messages request through the
// documented bedrock-runtime CountTokens operation. Claude targets reuse the
// Invoke preparation (anthropic_version and beta tokens) and travel in the
// "invokeModel" union member; every other serving family is translated by the
// existing Converse bridge and travels in the "converse" member, so a non-Claude
// body never reaches the Claude parser.
func (s *GatewayService) ForwardBedrockCountTokens(ctx context.Context, c *gin.Context, account *Account, parsed *ParsedRequest) error {
	if err := bedrockActionAccount(account); err != nil {
		s.countTokensError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return err
	}
	body := parsed.Body.Bytes()
	if len(body) == 0 {
		s.countTokensError(c, http.StatusBadRequest, "invalid_request_error", "Request body is empty")
		return fmt.Errorf("empty count_tokens body")
	}
	target, valid := ResolveBedrockModelID(account, parsed.Model)
	if !valid {
		s.countTokensError(c, http.StatusBadRequest, "invalid_request_error", "bedrock target is required")
		return fmt.Errorf("bedrock target is required")
	}
	if err := validateAccountForwardModel(ctx, s.accountRepo, account, target, CompositeRouteEndpointCountTokens); err != nil {
		s.countTokensError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return err
	}
	betaHeader := ""
	if c != nil && c.Request != nil {
		betaHeader = c.GetHeader("anthropic-beta")
	}
	input, err := s.buildBedrockCountTokensInput(ctx, account, target, body, betaHeader)
	if err != nil {
		s.countTokensError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return err
	}
	payload, err := json.Marshal(map[string]any{"input": input})
	if err != nil {
		s.countTokensError(c, http.StatusInternalServerError, "api_error", "Failed to build request")
		return err
	}
	targetURL, err := bedrockModelTargetURL(bedrockRuntimeRegion(account), target, bedrockCountTokensOperation)
	if err != nil {
		s.countTokensError(c, http.StatusInternalServerError, "api_error", "Failed to build request")
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(payload))
	if err != nil {
		s.countTokensError(c, http.StatusInternalServerError, "api_error", "Failed to build request")
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	if err := applyBedrockActionAuth(ctx, account, request, payload, bedrockSigV4ServiceName); err != nil {
		s.countTokensError(c, http.StatusBadGateway, "upstream_error", "Failed to sign upstream request")
		return err
	}
	resp, err := generationDo(s.httpUpstream, request, upstreamModelsProxyURL(account), account.ID, account.Concurrency)
	if err != nil {
		s.countTokensError(c, http.StatusBadGateway, "upstream_error", "Request failed")
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := readBedrockActionBody(resp)
	if err != nil {
		s.countTokensError(c, http.StatusBadGateway, "upstream_error", "Failed to read response")
		return err
	}
	if resp.StatusCode >= http.StatusBadRequest {
		s.countTokensError(c, http.StatusBadGateway, "upstream_error", bedrockUpstreamMessage(respBody))
		return fmt.Errorf("bedrock count_tokens upstream error: %d", resp.StatusCode)
	}
	count := gjson.GetBytes(respBody, "inputTokens")
	if count.Type != gjson.Number || count.Float() < 0 || count.Float() != float64(count.Int()) {
		s.countTokensError(c, http.StatusBadGateway, "upstream_error", "Bedrock token count is unknown")
		return fmt.Errorf("bedrock token count is unknown")
	}
	c.JSON(http.StatusOK, gin.H{"input_tokens": int(count.Int())})
	return nil
}

// buildBedrockCountTokensInput selects the documented CountTokensInput union
// member for the resolved target's serving family.
func (s *GatewayService) buildBedrockCountTokensInput(ctx context.Context, account *Account, target string, body []byte, betaHeader string) (map[string]any, error) {
	if strings.Contains(target, "anthropic.") {
		tokens, err := s.resolveBedrockBetaTokensForRequest(ctx, account, betaHeader, body, target)
		if err != nil {
			return nil, err
		}
		prepared, err := PrepareBedrockRequestBodyWithTokens(body, target, tokens, false)
		if err != nil {
			return nil, err
		}
		return map[string]any{"invokeModel": map[string]any{"body": base64.StdEncoding.EncodeToString(prepared)}}, nil
	}
	if betaHeader != "" {
		return nil, fmt.Errorf("anthropic beta unsupported for Converse token counting")
	}
	converted, err := PrepareBedrockConverseRequest(body)
	if err != nil {
		return nil, err
	}
	var converse map[string]any
	if err := json.Unmarshal(converted, &converse); err != nil {
		return nil, fmt.Errorf("invalid Converse token input")
	}
	// ConverseTokensRequest mirrors only the conversational inputs; sampling and
	// stop configuration are not part of the documented token-count input.
	delete(converse, "inferenceConfig")
	if len(converse) == 0 {
		return nil, fmt.Errorf("converse token input is empty")
	}
	return map[string]any{"converse": converse}, nil
}

// ---------------------------------------------------------------------------
// embeddings
// ---------------------------------------------------------------------------

type bedrockEmbeddingRequest struct {
	Model      string
	Inputs     []string
	Dimensions int
}

// parseBedrockEmbeddingRequest enforces the documented mapping onto the Titan
// text families. Upstream keeps the final word on the accepted dimensions.
func parseBedrockEmbeddingRequest(body []byte) (*bedrockEmbeddingRequest, error) {
	if !gjson.ValidBytes(body) {
		return nil, fmt.Errorf("invalid embeddings JSON")
	}
	root := gjson.ParseBytes(body)
	request := &bedrockEmbeddingRequest{Model: strings.TrimSpace(root.Get("model").String())}
	if request.Model == "" {
		return nil, fmt.Errorf("model is required")
	}
	input := root.Get("input")
	switch {
	case input.Type == gjson.String:
		request.Inputs = []string{input.String()}
	case input.IsArray():
		for _, item := range input.Array() {
			if item.Type != gjson.String {
				return nil, fmt.Errorf("bedrock embeddings input must be a string or an array of strings")
			}
			request.Inputs = append(request.Inputs, item.String())
		}
	default:
		return nil, fmt.Errorf("bedrock embeddings input must be a string or an array of strings")
	}
	if len(request.Inputs) == 0 {
		return nil, fmt.Errorf("bedrock embeddings input must not be empty")
	}
	if len(request.Inputs) > bedrockMaxEmbeddingInputs {
		return nil, fmt.Errorf("bedrock embeddings accept at most %d inputs per request", bedrockMaxEmbeddingInputs)
	}
	if dimensions := root.Get("dimensions"); dimensions.Exists() {
		if dimensions.Type != gjson.Number || dimensions.Float() <= 0 || dimensions.Float() != float64(dimensions.Int()) {
			return nil, fmt.Errorf("bedrock embeddings dimensions must be a positive integer")
		}
		request.Dimensions = int(dimensions.Int())
	}
	if format := strings.TrimSpace(root.Get("encoding_format").String()); format != "" && format != "float" {
		return nil, fmt.Errorf("bedrock embeddings return float vectors only")
	}
	return request, nil
}

// bedrockEmbeddingFamily resolves the serving family adapter for a target.
func bedrockEmbeddingFamily(target string) string {
	switch {
	case strings.HasPrefix(target, "amazon.titan-embed-text-v2"):
		return "amazon.titan-embed-text-v2"
	case strings.HasPrefix(target, "amazon.titan-embed-text-v1"):
		return "amazon.titan-embed-text-v1"
	}
	return ""
}

func buildBedrockTitanEmbeddingPayload(family, input string, dimensions int) ([]byte, error) {
	payload := map[string]any{"inputText": input}
	if family == "amazon.titan-embed-text-v2" && dimensions > 0 {
		// Documented accepted values are 1024, 512 and 256; upstream rejects the rest.
		payload["dimensions"] = dimensions
	}
	return json.Marshal(payload)
}

// ForwardBedrockEmbeddings forwards the OpenAI-shaped embeddings contract to the
// Titan text serving family. Titan takes one inputText per InvokeModel call, so a
// multi-input request fans out and the per-call inputTextTokenCount is summed for
// billing; an uninterpretable answer never settles as a trusted zero.
func (s *OpenAIGatewayService) ForwardBedrockEmbeddings(ctx context.Context, c *gin.Context, account *Account, body []byte, defaultMappedModel string) (*OpenAIForwardResult, error) {
	if err := bedrockActionAccount(account); err != nil {
		return nil, err
	}
	startTime := time.Now()
	request, err := parseBedrockEmbeddingRequest(body)
	if err != nil {
		writeOpenAIEmbeddingsError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return nil, err
	}
	target, valid := ResolveBedrockModelID(account, request.Model)
	if !valid {
		writeOpenAIEmbeddingsError(c, http.StatusBadRequest, "invalid_request_error", "bedrock target is required")
		return nil, fmt.Errorf("bedrock target is required")
	}
	family := bedrockEmbeddingFamily(target)
	if family == "" {
		writeOpenAIEmbeddingsError(c, http.StatusBadRequest, "invalid_request_error", "unsupported Bedrock embedding model")
		return nil, fmt.Errorf("unsupported Bedrock embedding model")
	}
	billingModel := resolveOpenAIForwardModel(account, request.Model, defaultMappedModel)
	if err := validateAccountForwardModel(ctx, s.accountRepo, account, target, CompositeRouteEndpointEmbeddings); err != nil {
		writeOpenAIEmbeddingsError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return nil, err
	}
	targetURL, err := bedrockModelTargetURL(bedrockRuntimeRegion(account), target, bedrockInvokeOperation)
	if err != nil {
		return nil, err
	}
	requestID := ""
	vectors := make([]json.RawMessage, 0, len(request.Inputs))
	totalTokens := 0
	for _, input := range request.Inputs {
		payload, err := buildBedrockTitanEmbeddingPayload(family, input, request.Dimensions)
		if err != nil {
			return nil, err
		}
		resp, err := s.sendBedrockActionRequest(ctx, account, http.MethodPost, targetURL, payload, "application/json", "application/json", bedrockSigV4ServiceName)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode >= http.StatusBadRequest {
			return s.handleBedrockActionHTTPError(ctx, c, account, resp, billingModel)
		}
		respBody, err := readBedrockActionBody(resp)
		if requestID == "" {
			requestID = bedrockRequestID(resp)
		}
		_ = resp.Body.Close()
		if err != nil {
			return nil, err
		}
		vector := gjson.GetBytes(respBody, "embedding")
		count := gjson.GetBytes(respBody, "inputTextTokenCount")
		if !vector.IsArray() || len(vector.Array()) == 0 || count.Type != gjson.Number || count.Float() < 0 || count.Float() != float64(count.Int()) {
			writeOpenAIEmbeddingsError(c, http.StatusBadGateway, "upstream_error", "Bedrock embedding usage is unknown")
			return nil, fmt.Errorf("bedrock embedding usage is unknown")
		}
		vectors = append(vectors, json.RawMessage(vector.Raw))
		totalTokens += int(count.Int())
	}
	data := make([]map[string]any, 0, len(vectors))
	for index, vector := range vectors {
		data = append(data, map[string]any{"object": "embedding", "index": index, "embedding": vector})
	}
	response, err := json.Marshal(map[string]any{
		"object": "list",
		"data":   data,
		"model":  request.Model,
		"usage":  map[string]any{"prompt_tokens": totalTokens, "total_tokens": totalTokens},
	})
	if err != nil {
		return nil, err
	}
	c.Data(http.StatusOK, "application/json", response)
	return &OpenAIForwardResult{
		RequestID:     requestID,
		Usage:         OpenAIUsage{InputTokens: totalTokens},
		Model:         request.Model,
		BillingModel:  billingModel,
		UpstreamModel: target,
		Stream:        false,
		Duration:      time.Since(startTime),
	}, nil
}

// ---------------------------------------------------------------------------
// rerank
// ---------------------------------------------------------------------------

type bedrockRerankRequest struct {
	Model           string
	Query           string
	Documents       []string
	NumberOfResults int
}

// parseBedrockRerankRequest reads the flat rerank contract already used by the
// Qianwen native adapter ({"model","query","documents","top_n","return_documents"}),
// so the shared rerank ingress keeps one client shape across platforms.
func parseBedrockRerankRequest(body []byte) (*bedrockRerankRequest, error) {
	if !gjson.ValidBytes(body) {
		return nil, fmt.Errorf("invalid rerank JSON")
	}
	root := gjson.ParseBytes(body)
	request := &bedrockRerankRequest{
		Model: strings.TrimSpace(root.Get("model").String()),
		Query: root.Get("query").String(),
	}
	if request.Model == "" {
		return nil, fmt.Errorf("model is required")
	}
	if strings.TrimSpace(request.Query) == "" {
		return nil, fmt.Errorf("rerank query is required")
	}
	if len([]rune(request.Query)) > bedrockMaxRerankTextRune {
		return nil, fmt.Errorf("rerank query is too long")
	}
	documents := root.Get("documents")
	if !documents.IsArray() || len(documents.Array()) == 0 {
		return nil, fmt.Errorf("rerank documents must be a nonempty array of strings")
	}
	if len(documents.Array()) > bedrockMaxRerankSources {
		return nil, fmt.Errorf("rerank accepts at most %d documents", bedrockMaxRerankSources)
	}
	for _, document := range documents.Array() {
		if document.Type != gjson.String {
			return nil, fmt.Errorf("bedrock rerank supports textual documents only")
		}
		if len([]rune(document.String())) > bedrockMaxRerankTextRune {
			return nil, fmt.Errorf("rerank document is too long")
		}
		request.Documents = append(request.Documents, document.String())
	}
	if topN := root.Get("top_n"); topN.Exists() {
		if topN.Type != gjson.Number || topN.Float() <= 0 || topN.Float() > bedrockMaxRerankResults || topN.Float() != float64(topN.Int()) {
			return nil, fmt.Errorf("rerank top_n must be an integer between 1 and %d", bedrockMaxRerankResults)
		}
		request.NumberOfResults = int(topN.Int())
	}
	if returnDocuments := root.Get("return_documents"); returnDocuments.Exists() && returnDocuments.Type != gjson.True && returnDocuments.Type != gjson.False {
		return nil, fmt.Errorf("return_documents must be a boolean")
	}
	return request, nil
}

func buildBedrockRerankBody(request *bedrockRerankRequest, target string) ([]byte, error) {
	sources := make([]map[string]any, 0, len(request.Documents))
	for _, document := range request.Documents {
		sources = append(sources, map[string]any{
			"type":                 "INLINE",
			"inlineDocumentSource": map[string]any{"type": "TEXT", "textDocument": map[string]any{"text": document}},
		})
	}
	bedrockReranking := map[string]any{"modelConfiguration": map[string]any{"modelArn": target}}
	if request.NumberOfResults > 0 {
		bedrockReranking["numberOfResults"] = request.NumberOfResults
	}
	return json.Marshal(map[string]any{
		"queries": []map[string]any{{"type": "TEXT", "textQuery": map[string]any{"text": request.Query}}},
		"rerankingConfiguration": map[string]any{
			"type":                          "BEDROCK_RERANKING_MODEL",
			"bedrockRerankingConfiguration": bedrockReranking,
		},
		"sources": sources,
	})
}

// ForwardBedrockRerank forwards the flat rerank contract to the documented
// Agents for Amazon Bedrock Runtime Rerank operation. The resolved target is
// passed through verbatim as modelArn, so an operator saves exactly the model ARN
// or ID their region serves; Amazon Rerank 1.0 is not offered in us-east-1 and
// only Cohere Rerank 3.5 is. Reranking is priced per query and the response
// exposes no token counters, so no usage is fabricated here: this model must be
// priced per request.
func (s *OpenAIGatewayService) ForwardBedrockRerank(ctx context.Context, c *gin.Context, account *Account, body []byte) (*OpenAIForwardResult, error) {
	if err := bedrockActionAccount(account); err != nil {
		return nil, err
	}
	startTime := time.Now()
	request, err := parseBedrockRerankRequest(body)
	if err != nil {
		writeOpenAIEmbeddingsError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return nil, err
	}
	target, valid := ResolveBedrockModelID(account, request.Model)
	if !valid {
		writeOpenAIEmbeddingsError(c, http.StatusBadRequest, "invalid_request_error", "bedrock target is required")
		return nil, fmt.Errorf("bedrock target is required")
	}
	// Rerank has no dedicated composite catalog axis (CompositeRouteEndpoint*
	// has no "rerank" member), so the binding is checked on the horizontal axis:
	// the target must be an eligible, evidence-backed model of this account.
	if err := validateAccountForwardModel(ctx, s.accountRepo, account, target, CompositeRouteEndpointAny); err != nil {
		writeOpenAIEmbeddingsError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return nil, err
	}
	payload, err := buildBedrockRerankBody(request, target)
	if err != nil {
		return nil, err
	}
	endpoint, err := bedrockServiceEndpoint(bedrockRuntimeRegion(account), bedrockAgentRuntimeServiceName)
	if err != nil {
		return nil, err
	}
	resp, err := s.sendBedrockActionRequest(ctx, account, http.MethodPost, endpoint+bedrockRerankPath, payload, "application/json", "application/json", bedrockSigV4ServiceName)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return s.handleBedrockActionHTTPError(ctx, c, account, resp, target)
	}
	respBody, err := readBedrockActionBody(resp)
	requestID := bedrockRequestID(resp)
	_ = resp.Body.Close()
	if err != nil {
		return nil, err
	}
	results := gjson.GetBytes(respBody, "results")
	if !results.IsArray() {
		writeOpenAIEmbeddingsError(c, http.StatusBadGateway, "upstream_error", "Bedrock rerank response is unknown")
		return nil, fmt.Errorf("bedrock rerank response is unknown")
	}
	flat := make([]map[string]any, 0, len(results.Array()))
	for _, result := range results.Array() {
		index := result.Get("index")
		score := result.Get("relevanceScore")
		if index.Type != gjson.Number || score.Type != gjson.Number {
			writeOpenAIEmbeddingsError(c, http.StatusBadGateway, "upstream_error", "Bedrock rerank result is unknown")
			return nil, fmt.Errorf("bedrock rerank result is unknown")
		}
		item := map[string]any{"index": int(index.Int()), "relevance_score": score.Float()}
		if document := result.Get("document.textDocument.text"); document.Exists() {
			item["document"] = map[string]any{"text": document.String()}
		}
		flat = append(flat, item)
	}
	response, err := json.Marshal(map[string]any{
		"id":      requestID,
		"model":   request.Model,
		"results": flat,
	})
	if err != nil {
		return nil, err
	}
	c.Data(http.StatusOK, "application/json", response)
	return &OpenAIForwardResult{
		RequestID:     requestID,
		Model:         request.Model,
		BillingModel:  resolveOpenAIForwardModel(account, request.Model, ""),
		UpstreamModel: target,
		Stream:        false,
		Duration:      time.Since(startTime),
	}, nil
}

// ---------------------------------------------------------------------------
// images
// ---------------------------------------------------------------------------

// ParseBedrockImagesRequest reads the OpenAI images shape for a Bedrock target.
//
// It deliberately does not reuse parseOpenAIImagesJSONRequest: that parser
// validates the OpenAI image model families and requires images[].image_url for
// edits, neither of which applies to a Bedrock serving family, and it defaults a
// missing model to gpt-image-2. Multipart parsing is shared, because the
// image/mask part names and the field names are the same client contract. The
// Bedrock-only extras (negative_prompt, seed, aspect_ratio, strength,
// output_format) stay in the raw body and are read by the family builders.
func ParseBedrockImagesRequest(c *gin.Context, body []byte, action string) (*OpenAIImagesRequest, error) {
	if c == nil || c.Request == nil {
		return nil, fmt.Errorf("missing request context")
	}
	if action != "image.generate" && action != "image.edit" && action != "image.variation" {
		return nil, fmt.Errorf("unsupported bedrock image action")
	}
	contentType := strings.TrimSpace(c.GetHeader("Content-Type"))
	request := &OpenAIImagesRequest{Endpoint: openAIImagesGenerationsEndpoint, ContentType: contentType, N: 1, Body: body}
	if action != "image.generate" {
		request.Endpoint = openAIImagesEditsEndpoint
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err == nil && strings.EqualFold(mediaType, "multipart/form-data") {
		request.Multipart = true
		if parseErr := parseOpenAIImagesMultipartRequest(body, contentType, request); parseErr != nil {
			return nil, parseErr
		}
	} else {
		if len(body) == 0 {
			return nil, fmt.Errorf("request body is empty")
		}
		if !gjson.ValidBytes(body) {
			return nil, fmt.Errorf("failed to parse request body")
		}
		if parseErr := parseBedrockImagesJSONFields(body, request); parseErr != nil {
			return nil, parseErr
		}
	}
	if request.N <= 0 {
		request.N = 1
	}
	request.Model = strings.TrimSpace(request.Model)
	if request.Model == "" {
		return nil, fmt.Errorf("model is required")
	}
	if action != "image.generate" && len(request.Uploads) == 0 {
		return nil, fmt.Errorf("bedrock image %s requires the input image bytes in a multipart request", strings.TrimPrefix(action, "image."))
	}
	request.SizeTier = normalizeOpenAIImageSizeTier(request.Size)
	return request, nil
}

func parseBedrockImagesJSONFields(body []byte, request *OpenAIImagesRequest) error {
	if model := gjson.GetBytes(body, "model"); model.Exists() {
		request.Model = strings.TrimSpace(model.String())
		request.ExplicitModel = request.Model != ""
	}
	request.Prompt = strings.TrimSpace(gjson.GetBytes(body, "prompt").String())
	if count := gjson.GetBytes(body, "n"); count.Exists() {
		if count.Type != gjson.Number {
			return fmt.Errorf("invalid n field type")
		}
		request.N = int(count.Int())
		if request.N <= 0 {
			return fmt.Errorf("n must be greater than 0")
		}
	}
	if size := gjson.GetBytes(body, "size"); size.Exists() {
		request.Size = strings.TrimSpace(size.String())
		request.ExplicitSize = request.Size != ""
	}
	if stream := gjson.GetBytes(body, "stream"); stream.Exists() {
		if stream.Type != gjson.True && stream.Type != gjson.False {
			return fmt.Errorf("invalid stream field type")
		}
		request.Stream = stream.Bool()
	}
	request.ResponseFormat = strings.ToLower(strings.TrimSpace(gjson.GetBytes(body, "response_format").String()))
	request.Quality = strings.TrimSpace(gjson.GetBytes(body, "quality").String())
	if gjson.GetBytes(body, "images").Exists() {
		return fmt.Errorf("bedrock image edits require the input image bytes in a multipart request")
	}
	return nil
}

// bedrockImageFamily resolves the serving family adapter for an image target.
// Only families with captured first-party request and response schemas qualify;
// other Stability variants stay out rather than borrowing a sibling schema.
func bedrockImageFamily(target string) string {
	switch {
	case strings.HasPrefix(target, "amazon.nova-canvas"):
		return "nova-canvas"
	case strings.HasPrefix(target, "amazon.titan-image-generator-v2"):
		return "titan-image-v2"
	case strings.HasPrefix(target, "amazon.titan-image-generator-v1"):
		return "titan-image-v1"
	case target == "stability.sd3-5-large-v1:0",
		target == "stability.stable-image-ultra-v1:1",
		target == "stability.stable-image-core-v1:1":
		return "stability"
	}
	return ""
}

// bedrockImageQuality passes through only the shared documented quality values;
// an unknown client quality is dropped instead of guessed into a price tier.
func bedrockImageQuality(quality string) string {
	switch normalized := strings.ToLower(strings.TrimSpace(quality)); normalized {
	case "standard", "premium":
		return normalized
	}
	return ""
}

// buildBedrockImageBody translates the parsed OpenAI images request into the
// serving family's documented task body. A maskless edit maps onto documented
// image conditioning where the family has it, and is rejected where it does not
// instead of silently generating an unrelated image.
func buildBedrockImageBody(family string, parsed *OpenAIImagesRequest, body []byte, action string) ([]byte, error) {
	root := gjson.ParseBytes(body)
	negativeText := strings.TrimSpace(root.Get("negative_prompt").String())
	seedValue := int64(0)
	hasSeed := false
	if seed := root.Get("seed"); seed.Exists() && seed.Type == gjson.Number {
		seedValue = seed.Int()
		hasSeed = true
	}
	prompt := strings.TrimSpace(parsed.Prompt)
	imageCount := parsed.N
	if imageCount <= 0 {
		imageCount = 1
	}
	width, height, hasSize := parseImageBillingDimensions(parsed.Size)

	uploads := make([]string, 0, len(parsed.Uploads))
	for _, upload := range parsed.Uploads {
		uploads = append(uploads, base64.StdEncoding.EncodeToString(upload.Data))
	}
	mask := ""
	if parsed.MaskUpload != nil {
		mask = base64.StdEncoding.EncodeToString(parsed.MaskUpload.Data)
	}

	switch family {
	case "nova-canvas", "titan-image-v1", "titan-image-v2":
		generationConfig := map[string]any{"numberOfImages": imageCount}
		if quality := bedrockImageQuality(parsed.Quality); quality != "" {
			generationConfig["quality"] = quality
		}
		if hasSeed {
			generationConfig["seed"] = seedValue
		}
		switch action {
		case "image.variation":
			if len(uploads) == 0 {
				return nil, fmt.Errorf("bedrock image variation requires an input image")
			}
			params := map[string]any{"images": uploads, "text": prompt}
			if negativeText != "" {
				params["negativeText"] = negativeText
			}
			return json.Marshal(map[string]any{"taskType": "IMAGE_VARIATION", "imageVariationParams": params, "imageGenerationConfig": map[string]any{"numberOfImages": imageCount}})
		case "image.edit":
			if len(uploads) == 0 {
				return nil, fmt.Errorf("bedrock image edit requires an input image")
			}
			if mask != "" {
				// The maskImage selects the region; an omitted text is the documented
				// "remove the elements inside the masked region" operation, so only a
				// provided prompt is forwarded.
				params := map[string]any{"image": uploads[0], "maskImage": mask}
				if prompt != "" {
					params["text"] = prompt
				}
				if negativeText != "" {
					params["negativeText"] = negativeText
				}
				return json.Marshal(map[string]any{"taskType": "INPAINTING", "inPaintingParams": params, "imageGenerationConfig": map[string]any{"numberOfImages": imageCount}})
			}
			if family == "titan-image-v1" {
				return nil, fmt.Errorf("this Bedrock image family has no maskless edit task; supply a mask")
			}
			if prompt == "" {
				return nil, fmt.Errorf("bedrock image edit requires a prompt")
			}
			params := map[string]any{"text": prompt, "conditionImage": uploads[0]}
			if negativeText != "" {
				params["negativeText"] = negativeText
			}
			if hasSize {
				generationConfig["width"] = width
				generationConfig["height"] = height
			}
			return json.Marshal(map[string]any{"taskType": "TEXT_IMAGE", "textToImageParams": params, "imageGenerationConfig": generationConfig})
		default:
			if prompt == "" {
				return nil, fmt.Errorf("bedrock image generation requires a prompt")
			}
			if hasSize {
				generationConfig["width"] = width
				generationConfig["height"] = height
			}
			params := map[string]any{"text": prompt}
			if negativeText != "" {
				params["negativeText"] = negativeText
			}
			return json.Marshal(map[string]any{"taskType": "TEXT_IMAGE", "textToImageParams": params, "imageGenerationConfig": generationConfig})
		}
	case "stability":
		payload := map[string]any{}
		if prompt != "" {
			payload["prompt"] = prompt
		}
		if negativeText != "" {
			payload["negative_prompt"] = negativeText
		}
		if hasSeed {
			payload["seed"] = seedValue
		}
		if aspect := strings.TrimSpace(root.Get("aspect_ratio").String()); aspect != "" {
			payload["aspect_ratio"] = aspect
		}
		if format := strings.TrimSpace(root.Get("output_format").String()); format != "" {
			payload["output_format"] = format
		}
		switch action {
		case "image.variation":
			return nil, fmt.Errorf("this Bedrock image family has no image variation task")
		case "image.edit":
			if len(uploads) == 0 {
				return nil, fmt.Errorf("bedrock image edit requires an input image")
			}
			if prompt == "" {
				return nil, fmt.Errorf("bedrock image edit requires a prompt")
			}
			strength := root.Get("strength")
			if strength.Type != gjson.Number || strength.Float() < 0 || strength.Float() > 1 {
				return nil, fmt.Errorf("this Bedrock image family requires a strength between 0 and 1 for image-to-image")
			}
			payload["mode"] = "image-to-image"
			payload["image"] = uploads[0]
			payload["strength"] = strength.Float()
		default:
			if prompt == "" {
				return nil, fmt.Errorf("bedrock image generation requires a prompt")
			}
			payload["mode"] = "text-to-image"
		}
		return json.Marshal(payload)
	}
	return nil, fmt.Errorf("unsupported Bedrock image family")
}

// bedrockImageResponse extracts the family's documented image list and enforces
// its documented filtering signal. A response without an image is a failure, not
// an empty success.
func bedrockImageResponse(family string, body []byte) ([]string, error) {
	images := gjson.GetBytes(body, "images")
	if family == "stability" {
		for _, reason := range gjson.GetBytes(body, "finish_reasons").Array() {
			if reason.Type != gjson.Null {
				return nil, fmt.Errorf("bedrock image generation was filtered: %s", reason.String())
			}
		}
	} else if message := strings.TrimSpace(gjson.GetBytes(body, "error").String()); message != "" && !images.IsArray() {
		return nil, fmt.Errorf("bedrock image generation failed: %s", message)
	}
	if !images.IsArray() || len(images.Array()) == 0 {
		return nil, fmt.Errorf("bedrock image response carries no image")
	}
	result := make([]string, 0, len(images.Array()))
	for _, image := range images.Array() {
		if image.Type != gjson.String || strings.TrimSpace(image.String()) == "" {
			return nil, fmt.Errorf("bedrock image response is unknown")
		}
		result = append(result, image.String())
	}
	return result, nil
}

// ForwardBedrockImageGeneration forwards the OpenAI-shaped images contract
// (generation, edit and variation) to the target's serving family on InvokeModel
// and answers with the OpenAI b64_json envelope. Bedrock returns base64 images
// and hosts nothing, so a requested url response format is answered with
// b64_json; the billable unit is the returned image count and the requested or
// detected size tier, both recorded on the result for the existing image billing
// resolution.
func (s *OpenAIGatewayService) ForwardBedrockImageGeneration(ctx context.Context, c *gin.Context, account *Account, body []byte, parsed *OpenAIImagesRequest, action, mappedModel string) (*OpenAIForwardResult, error) {
	if err := bedrockActionAccount(account); err != nil {
		return nil, err
	}
	if parsed == nil {
		return nil, fmt.Errorf("parsed images request is required")
	}
	switch action {
	case "image.generate", "image.edit", "image.variation":
	default:
		return nil, fmt.Errorf("unsupported bedrock image action")
	}
	startTime := time.Now()
	requestedModel := strings.TrimSpace(parsed.Model)
	if requestedModel == "" {
		writeOpenAIEmbeddingsError(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return nil, fmt.Errorf("model is required")
	}
	target, valid := ResolveBedrockModelID(account, requestedModel)
	if !valid {
		writeOpenAIEmbeddingsError(c, http.StatusBadRequest, "invalid_request_error", "bedrock target is required")
		return nil, fmt.Errorf("bedrock target is required")
	}
	family := bedrockImageFamily(target)
	if family == "" {
		writeOpenAIEmbeddingsError(c, http.StatusBadRequest, "invalid_request_error", "unsupported Bedrock image model")
		return nil, fmt.Errorf("unsupported Bedrock image model")
	}
	billingModel := resolveOpenAIForwardModel(account, requestedModel, mappedModel)
	if err := validateAccountForwardModel(ctx, s.accountRepo, account, target, CompositeRouteEndpointImages); err != nil {
		writeOpenAIEmbeddingsError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return nil, err
	}
	payload, err := buildBedrockImageBody(family, parsed, body, action)
	if err != nil {
		writeOpenAIEmbeddingsError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return nil, err
	}
	targetURL, err := bedrockModelTargetURL(bedrockRuntimeRegion(account), target, bedrockInvokeOperation)
	if err != nil {
		return nil, err
	}
	resp, err := s.sendBedrockActionRequest(ctx, account, http.MethodPost, targetURL, payload, "application/json", "application/json", bedrockSigV4ServiceName)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return s.handleBedrockActionHTTPError(ctx, c, account, resp, billingModel)
	}
	respBody, err := readBedrockActionBody(resp)
	requestID := bedrockRequestID(resp)
	_ = resp.Body.Close()
	if err != nil {
		return nil, err
	}
	images, err := bedrockImageResponse(family, respBody)
	if err != nil {
		writeOpenAIEmbeddingsError(c, http.StatusBadGateway, "upstream_error", err.Error())
		return nil, err
	}
	data := make([]map[string]string, 0, len(images))
	outputSizes := make([]string, 0, len(images))
	for _, image := range images {
		data = append(data, map[string]string{"b64_json": image})
		// The image bytes are authoritative for the billed tier: Bedrock reports
		// no dimensions of its own.
		if size := detectOpenAIImageResultSize(image); size != "" {
			outputSizes = append(outputSizes, size)
		}
	}
	envelope, err := json.Marshal(map[string]any{"created": time.Now().Unix(), "data": data})
	if err != nil {
		return nil, err
	}
	c.Data(http.StatusOK, "application/json", envelope)
	return &OpenAIForwardResult{
		RequestID:        requestID,
		Model:            requestedModel,
		BillingModel:     billingModel,
		UpstreamModel:    target,
		ImageCount:       len(images),
		ImageSize:        normalizeOpenAIImageSizeTier(parsed.Size),
		ImageInputSize:   parsed.Size,
		ImageOutputSizes: outputSizes,
		Stream:           false,
		Duration:         time.Since(startTime),
	}, nil
}
