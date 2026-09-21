//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// The fixtures below are synthetic bodies shaped exactly like the first-party
// AWS documentation captured on 2026-09-21 (Amazon Bedrock Runtime CountTokens
// and InvokeModel, Agents for Amazon Bedrock Runtime Rerank, Amazon Titan Text
// Embeddings, and the Nova Canvas / Titan Image Generator / Stability task
// bodies). They are not live account receipts; these tests perform no network,
// database, Redis or billable upstream action.

func bedrockContractAccount() *Account {
	return &Account{
		ID:       501,
		Platform: PlatformBedrock,
		Type:     AccountTypeBedrock,
		Credentials: map[string]any{
			"aws_access_key_id":     "AKIAFIXTUREEXAMPLE",
			"aws_secret_access_key": "fixture-secret",
			"aws_region":            "us-east-1",
		},
	}
}

func bedrockActionContext(method, target string, body []byte) (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	ginContext, _ := gin.CreateTestContext(recorder)
	ginContext.Request = httptest.NewRequest(method, target, bytes.NewReader(body))
	return ginContext, recorder
}

func bedrockJSONResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": {"application/json"}, "X-Amzn-Requestid": {"bedrock-req-1"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func bedrockPNG(t *testing.T, width, height int) string {
	t.Helper()
	buffer := &bytes.Buffer{}
	source := image.NewRGBA(image.Rect(0, 0, width, height))
	for x := 0; x < width; x++ {
		for y := 0; y < height; y++ {
			source.Set(x, y, color.RGBA{R: uint8(x % 251), G: uint8(y % 251), B: 64, A: 255})
		}
	}
	require.NoError(t, png.Encode(buffer, source))
	return base64.StdEncoding.EncodeToString(buffer.Bytes())
}

func TestBedrockCountTokensContract(test *testing.T) {
	gin.SetMode(gin.TestMode)

	test.Run("claude target uses the invokeModel member with the prepared native body", func(test *testing.T) {
		account := bedrockContractAccount()
		target := "us.anthropic.claude-sonnet-4-5-20250929-v1:0"
		account.Credentials["model_mapping"] = map[string]any{"claude-sonnet-4-5": target}
		body := []byte(`{"model":"claude-sonnet-4-5","max_tokens":64,"stream":true,"messages":[{"role":"user","content":"hello"}]}`)
		transport := &httpUpstreamRecorder{resp: bedrockJSONResponse(200, `{"inputTokens":42}`)}
		gateway := &GatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: transport}
		requestContext, recorder := bedrockActionContext(http.MethodPost, "/v1/messages/count_tokens", body)
		parsed := &ParsedRequest{Body: NewRequestBodyRef(body), Model: "claude-sonnet-4-5"}
		require.NoError(test, gateway.ForwardBedrockCountTokens(context.Background(), requestContext, account, parsed))

		require.Equal(test, "https://bedrock-runtime.us-east-1.amazonaws.com/model/us.anthropic.claude-sonnet-4-5-20250929-v1%3A0/count-tokens", transport.lastReq.URL.String())
		require.Equal(test, "application/json", transport.lastReq.Header.Get("Content-Type"))
		authorization := transport.lastReq.Header.Get("Authorization")
		require.True(test, strings.HasPrefix(authorization, "AWS4-HMAC-SHA256"), "bedrock accounts sign with SigV4")
		require.Contains(test, authorization, "/us-east-1/bedrock/aws4_request")
		prepared := gjson.GetBytes(transport.lastBody, "input.invokeModel.body").String()
		require.NotEmpty(test, prepared, "claude targets travel in the invokeModel union member")
		decoded, err := base64.StdEncoding.DecodeString(prepared)
		require.NoError(test, err)
		require.Equal(test, "bedrock-2023-05-31", gjson.GetBytes(decoded, "anthropic_version").String())
		require.False(test, gjson.GetBytes(decoded, "model").Exists(), "the native body must not carry the client model field")
		require.False(test, gjson.GetBytes(decoded, "stream").Exists(), "the native body must not carry the stream flag")
		require.Equal(test, `{"input_tokens":42}`, recorder.Body.String())
		require.Len(test, transport.requests, 1)
	})

	test.Run("non-claude target uses the converse member and never the claude parser", func(test *testing.T) {
		account := bedrockContractAccount()
		account.Credentials["model_mapping"] = map[string]any{"nova-lite": "amazon.nova-lite-v1:0"}
		body := []byte(`{"model":"nova-lite","max_tokens":32,"messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}],"system":"be brief"}`)
		transport := &httpUpstreamRecorder{resp: bedrockJSONResponse(200, `{"inputTokens":7}`)}
		gateway := &GatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: transport}
		requestContext, _ := bedrockActionContext(http.MethodPost, "/v1/messages/count_tokens", body)
		parsed := &ParsedRequest{Body: NewRequestBodyRef(body), Model: "nova-lite"}
		require.NoError(test, gateway.ForwardBedrockCountTokens(context.Background(), requestContext, account, parsed))

		require.Contains(test, transport.lastReq.URL.String(), "/model/amazon.nova-lite-v1%3A0/count-tokens")
		converse := gjson.GetBytes(transport.lastBody, "input.converse")
		require.True(test, converse.Exists(), "non-claude targets travel in the converse union member")
		require.Equal(test, "hi", converse.Get("messages.0.content.0.text").String())
		require.False(test, gjson.GetBytes(transport.lastBody, "input.invokeModel").Exists())
		require.False(test, converse.Get("inferenceConfig").Exists(), "the documented token input carries no sampling config")
	})

	test.Run("an unknown token count is an error, never a trusted zero", func(test *testing.T) {
		account := bedrockContractAccount()
		account.Credentials["model_mapping"] = map[string]any{"alias": "us.anthropic.claude-sonnet-4-5-20250929-v1:0"}
		body := []byte(`{"model":"alias","messages":[{"role":"user","content":"hello"}]}`)
		for _, upstream := range []string{`{}`, `{"inputTokens":null}`, `{"inputTokens":-1}`, `{"inputTokens":1.5}`, `not-json`} {
			transport := &httpUpstreamRecorder{resp: bedrockJSONResponse(200, upstream)}
			gateway := &GatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: transport}
			requestContext, recorder := bedrockActionContext(http.MethodPost, "/v1/messages/count_tokens", body)
			parsed := &ParsedRequest{Body: NewRequestBodyRef(body), Model: "alias"}
			require.Error(test, gateway.ForwardBedrockCountTokens(context.Background(), requestContext, account, parsed), upstream)
			require.Contains(test, recorder.Body.String(), `"error"`, upstream)
		}
	})

	test.Run("non-bedrock accounts and empty bodies never reach upstream", func(test *testing.T) {
		transport := &httpUpstreamRecorder{resp: bedrockJSONResponse(200, `{"inputTokens":1}`)}
		gateway := &GatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: transport}
		body := []byte(`{"model":"alias","messages":[{"role":"user","content":"hello"}]}`)
		otherPlatform := bedrockContractAccount()
		otherPlatform.Platform = PlatformAnthropic
		otherPlatform.Type = AccountTypeAPIKey
		requestContext, recorder := bedrockActionContext(http.MethodPost, "/v1/messages/count_tokens", body)
		require.Error(test, gateway.ForwardBedrockCountTokens(context.Background(), requestContext, otherPlatform, &ParsedRequest{Body: NewRequestBodyRef(body), Model: "alias"}))
		require.Contains(test, recorder.Body.String(), "bedrock account")
		emptyContext, _ := bedrockActionContext(http.MethodPost, "/v1/messages/count_tokens", nil)
		require.Error(test, gateway.ForwardBedrockCountTokens(context.Background(), emptyContext, bedrockContractAccount(), &ParsedRequest{Body: NewRequestBodyRef(nil), Model: "alias"}))
		unsupported := bedrockContractAccount()
		unsupported.Credentials["model_mapping"] = map[string]any{"nova-lite": "amazon.nova-lite-v1:0"}
		unsupportedBody := []byte(`{"model":"nova-lite","messages":[{"role":"user","content":"hi"}],"output_format":{"type":"json_schema","schema":{"type":"object"}}}`)
		unsupportedContext, _ := bedrockActionContext(http.MethodPost, "/v1/messages/count_tokens", unsupportedBody)
		require.ErrorContains(test, gateway.ForwardBedrockCountTokens(context.Background(), unsupportedContext, unsupported, &ParsedRequest{Body: NewRequestBodyRef(unsupportedBody), Model: "nova-lite"}), "unsupported Converse request field")
		require.Nil(test, transport.lastReq, "rejected requests must never reach upstream")
	})
}

func TestBedrockEmbeddingsContract(test *testing.T) {
	gin.SetMode(gin.TestMode)

	test.Run("titan fan-out reaches the documented invoke target with summed usage", func(test *testing.T) {
		transport := &httpUpstreamRecorder{responses: []*http.Response{
			bedrockJSONResponse(200, `{"embedding":[0.5,0.25],"inputTextTokenCount":3}`),
			bedrockJSONResponse(200, `{"embedding":[0.125],"inputTextTokenCount":4}`),
		}}
		service := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: transport}
		body := []byte(`{"model":"amazon.titan-embed-text-v2:0","input":["alpha","beta"],"dimensions":512}`)
		ginContext, recorder := bedrockActionContext(http.MethodPost, "/v1/embeddings", body)
		result, err := service.ForwardBedrockEmbeddings(context.Background(), ginContext, bedrockContractAccount(), body, "")
		require.NoError(test, err)
		require.Len(test, transport.requests, 2, "titan takes one inputText per invocation")
		for _, request := range transport.requests {
			require.Equal(test, "https://bedrock-runtime.us-east-1.amazonaws.com/model/amazon.titan-embed-text-v2%3A0/invoke", request.URL.String())
			require.Equal(test, "application/json", request.Header.Get("Content-Type"))
			require.True(test, strings.HasPrefix(request.Header.Get("Authorization"), "AWS4-HMAC-SHA256"))
		}
		require.Equal(test, "alpha", gjson.GetBytes(transport.bodies[0], "inputText").String())
		require.EqualValues(test, 512, gjson.GetBytes(transport.bodies[0], "dimensions").Int())
		require.Equal(test, "beta", gjson.GetBytes(transport.bodies[1], "inputText").String())
		require.Equal(test, 7, result.Usage.InputTokens, "usage is the summed documented inputTextTokenCount")
		require.Equal(test, "amazon.titan-embed-text-v2:0", result.UpstreamModel)
		require.Equal(test, "bedrock-req-1", result.RequestID)
		clientBody := recorder.Body.Bytes()
		require.Equal(test, "list", gjson.GetBytes(clientBody, "object").String())
		require.Equal(test, 0.5, gjson.GetBytes(clientBody, "data.0.embedding.0").Float())
		require.EqualValues(test, 0, gjson.GetBytes(clientBody, "data.0.index").Int())
		require.Equal(test, 0.125, gjson.GetBytes(clientBody, "data.1.embedding.0").Float())
		require.EqualValues(test, 1, gjson.GetBytes(clientBody, "data.1.index").Int())
		require.EqualValues(test, 7, gjson.GetBytes(clientBody, "usage.total_tokens").Int())
	})

	test.Run("g1 keeps its documented inputText-only field set", func(test *testing.T) {
		transport := &httpUpstreamRecorder{resp: bedrockJSONResponse(200, `{"embedding":[1],"inputTextTokenCount":1}`)}
		service := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: transport}
		body := []byte(`{"model":"amazon.titan-embed-text-v1","input":"alpha","dimensions":256}`)
		ginContext, _ := bedrockActionContext(http.MethodPost, "/v1/embeddings", body)
		_, err := service.ForwardBedrockEmbeddings(context.Background(), ginContext, bedrockContractAccount(), body, "")
		require.NoError(test, err)
		require.False(test, gjson.GetBytes(transport.lastBody, "dimensions").Exists(), "G1 documents inputText only")
	})

	test.Run("invalid requests never reach upstream", func(test *testing.T) {
		transport := &httpUpstreamRecorder{resp: bedrockJSONResponse(200, `{"embedding":[1],"inputTextTokenCount":1}`)}
		service := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: transport}
		for _, body := range []string{
			`{"input":"hello"}`,
			`{"model":"amazon.titan-embed-text-v2:0"}`,
			`{"model":"amazon.titan-embed-text-v2:0","input":[]}`,
			`{"model":"amazon.titan-embed-text-v2:0","input":[1,2]}`,
			`{"model":"amazon.titan-embed-text-v2:0","input":"x","dimensions":-1}`,
			`{"model":"amazon.titan-embed-text-v2:0","input":"x","dimensions":1.5}`,
			`{"model":"amazon.titan-embed-text-v2:0","input":"x","encoding_format":"base64"}`,
			`not-json`,
		} {
			ginContext, recorder := bedrockActionContext(http.MethodPost, "/v1/embeddings", []byte(body))
			_, err := service.ForwardBedrockEmbeddings(context.Background(), ginContext, bedrockContractAccount(), []byte(body), "")
			require.Error(test, err, body)
			require.Contains(test, recorder.Body.String(), `"invalid_request_error"`, body)
		}
		cohereBody := []byte(`{"model":"cohere.embed-v4:0","input":"x"}`)
		cohereContext, _ := bedrockActionContext(http.MethodPost, "/v1/embeddings", cohereBody)
		_, err := service.ForwardBedrockEmbeddings(context.Background(), cohereContext, bedrockContractAccount(), cohereBody, "")
		require.ErrorContains(test, err, "unsupported Bedrock embedding model")
		require.Nil(test, transport.lastReq, "invalid requests must never reach upstream")
	})

	test.Run("unknown usage is an error and never settles as zero", func(test *testing.T) {
		body := []byte(`{"model":"amazon.titan-embed-text-v2:0","input":"alpha"}`)
		for _, upstream := range []string{`{}`, `{"embedding":[]}`, `{"embedding":[1]}`, `{"embedding":[1],"inputTextTokenCount":-1}`, `{"embedding":[1],"inputTextTokenCount":1.5}`} {
			transport := &httpUpstreamRecorder{resp: bedrockJSONResponse(200, upstream)}
			service := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: transport}
			ginContext, _ := bedrockActionContext(http.MethodPost, "/v1/embeddings", body)
			result, err := service.ForwardBedrockEmbeddings(context.Background(), ginContext, bedrockContractAccount(), body, "")
			require.ErrorContains(test, err, "unknown", upstream)
			require.Nil(test, result, upstream)
		}
	})

	test.Run("no blind retry and upstream classification is preserved", func(test *testing.T) {
		for _, status := range []int{400, 403, 429, 500, 503} {
			transport := &httpUpstreamRecorder{resp: bedrockJSONResponse(status, `{"message":"synthetic failure"}`)}
			service := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: transport}
			body := []byte(`{"model":"amazon.titan-embed-text-v2:0","input":"alpha"}`)
			ginContext, recorder := bedrockActionContext(http.MethodPost, "/v1/embeddings", body)
			_, err := service.ForwardBedrockEmbeddings(context.Background(), ginContext, bedrockContractAccount(), body, "")
			require.Error(test, err, status)
			require.Len(test, transport.requests, 1, "one forward is exactly one upstream call")
			if recorder.Body.Len() > 0 {
				require.Contains(test, recorder.Body.String(), `"error"`, status)
			}
		}
	})
}

func TestBedrockRerankContract(test *testing.T) {
	gin.SetMode(gin.TestMode)

	test.Run("rerank reaches the agents runtime endpoint with the documented body", func(test *testing.T) {
		upstream := `{"results":[{"index":1,"relevanceScore":0.91,"document":{"type":"TEXT","textDocument":{"text":"second"}}},{"index":0,"relevanceScore":0.12,"document":{"type":"TEXT","textDocument":{"text":"first"}}}]}`
		transport := &httpUpstreamRecorder{resp: bedrockJSONResponse(200, upstream)}
		service := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: transport}
		body := []byte(`{"model":"cohere.rerank-v3-5:0","query":"what is bedrock","documents":["first","second"],"top_n":2,"return_documents":true}`)
		ginContext, recorder := bedrockActionContext(http.MethodPost, "/v1/rerank", body)
		result, err := service.ForwardBedrockRerank(context.Background(), ginContext, bedrockContractAccount(), body)
		require.NoError(test, err)
		require.Equal(test, "https://bedrock-agent-runtime.us-east-1.amazonaws.com/rerank", transport.lastReq.URL.String())
		authorization := transport.lastReq.Header.Get("Authorization")
		require.True(test, strings.HasPrefix(authorization, "AWS4-HMAC-SHA256"), "the agents runtime signs with the same bedrock signing name")
		require.Contains(test, authorization, "/us-east-1/bedrock/aws4_request")
		sent := gjson.ParseBytes(transport.lastBody)
		require.Equal(test, "TEXT", sent.Get("queries.0.type").String())
		require.Equal(test, "what is bedrock", sent.Get("queries.0.textQuery.text").String())
		require.Len(test, sent.Get("sources").Array(), 2)
		require.Equal(test, "INLINE", sent.Get("sources.0.type").String())
		require.Equal(test, "TEXT", sent.Get("sources.0.inlineDocumentSource.type").String())
		require.Equal(test, "first", sent.Get("sources.0.inlineDocumentSource.textDocument.text").String())
		require.Equal(test, "second", sent.Get("sources.1.inlineDocumentSource.textDocument.text").String())
		require.Equal(test, "BEDROCK_RERANKING_MODEL", sent.Get("rerankingConfiguration.type").String())
		require.Equal(test, "cohere.rerank-v3-5:0", sent.Get("rerankingConfiguration.bedrockRerankingConfiguration.modelConfiguration.modelArn").String())
		require.EqualValues(test, 2, sent.Get("rerankingConfiguration.bedrockRerankingConfiguration.numberOfResults").Int())
		clientBody := recorder.Body.Bytes()
		require.EqualValues(test, 1, gjson.GetBytes(clientBody, "results.0.index").Int())
		require.Equal(test, 0.91, gjson.GetBytes(clientBody, "results.0.relevance_score").Float())
		require.Equal(test, "second", gjson.GetBytes(clientBody, "results.0.document.text").String())
		require.Equal(test, "bedrock-req-1", gjson.GetBytes(clientBody, "id").String())
		require.Equal(test, "cohere.rerank-v3-5:0", result.UpstreamModel)
		require.EqualValues(test, 0, result.Usage.InputTokens, "AWS publishes no token counters for Rerank, so usage stays unknown rather than invented")
		require.Equal(test, "bedrock-req-1", result.RequestID)
		require.Len(test, transport.requests, 1)
	})

	test.Run("the operator's saved rerank target is passed through verbatim", func(test *testing.T) {
		target := "arn:aws:bedrock:us-east-1::foundation-model/cohere.rerank-v3-5:0"
		transport := &httpUpstreamRecorder{resp: bedrockJSONResponse(200, `{"results":[{"index":0,"relevanceScore":0.5}]}`)}
		service := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: transport}
		account := bedrockContractAccount()
		account.Credentials["model_mapping"] = map[string]any{"rerank-public": target}
		body := []byte(`{"model":"rerank-public","query":"q","documents":["a"]}`)
		ginContext, _ := bedrockActionContext(http.MethodPost, "/v1/rerank", body)
		_, err := service.ForwardBedrockRerank(context.Background(), ginContext, account, body)
		require.NoError(test, err)
		require.Equal(test, target, gjson.GetBytes(transport.lastBody, "rerankingConfiguration.bedrockRerankingConfiguration.modelConfiguration.modelArn").String())
		require.Equal(test, "https://bedrock-agent-runtime.us-east-1.amazonaws.com/rerank", transport.lastReq.URL.String())
	})

	test.Run("invalid rerank requests never reach upstream", func(test *testing.T) {
		transport := &httpUpstreamRecorder{resp: bedrockJSONResponse(200, `{"results":[]}`)}
		service := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: transport}
		for _, body := range []string{
			`{"query":"q","documents":["a"]}`,
			`{"model":"cohere.rerank-v3-5:0","documents":["a"]}`,
			`{"model":"cohere.rerank-v3-5:0","query":"q"}`,
			`{"model":"cohere.rerank-v3-5:0","query":"q","documents":[]}`,
			`{"model":"cohere.rerank-v3-5:0","query":"q","documents":[{"text":"a"}]}`,
			`{"model":"cohere.rerank-v3-5:0","query":"q","documents":["a"],"top_n":0}`,
			`{"model":"cohere.rerank-v3-5:0","query":"q","documents":["a"],"top_n":1001}`,
			`{"model":"cohere.rerank-v3-5:0","query":"q","documents":["a"],"top_n":1.5}`,
			`not-json`,
		} {
			ginContext, _ := bedrockActionContext(http.MethodPost, "/v1/rerank", []byte(body))
			_, err := service.ForwardBedrockRerank(context.Background(), ginContext, bedrockContractAccount(), []byte(body))
			require.Error(test, err, body)
		}
		require.Nil(test, transport.lastReq, "invalid requests must never reach upstream")
	})

	test.Run("an unrecognized rerank result is an error", func(test *testing.T) {
		for _, upstream := range []string{`{}`, `{"results":null}`, `{"results":[{"relevanceScore":0.5}]}`, `{"results":[{"index":0}]}`} {
			transport := &httpUpstreamRecorder{resp: bedrockJSONResponse(200, upstream)}
			service := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: transport}
			body := []byte(`{"model":"cohere.rerank-v3-5:0","query":"q","documents":["a"]}`)
			ginContext, _ := bedrockActionContext(http.MethodPost, "/v1/rerank", body)
			_, err := service.ForwardBedrockRerank(context.Background(), ginContext, bedrockContractAccount(), body)
			require.Error(test, err, upstream)
		}
	})
}

func TestBedrockImageContract(test *testing.T) {
	gin.SetMode(gin.TestMode)

	test.Run("nova canvas generation reaches the documented task body and reports the billable unit", func(test *testing.T) {
		pngBase64 := bedrockPNG(test, 1024, 1024)
		transport := &httpUpstreamRecorder{resp: bedrockJSONResponse(200, `{"images":["`+pngBase64+`"]}`)}
		service := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: transport}
		body := []byte(`{"model":"amazon.nova-canvas-v1:0","prompt":"a lighthouse","n":2,"size":"1024x1024","negative_prompt":"blurry"}`)
		parsed := &OpenAIImagesRequest{Model: "amazon.nova-canvas-v1:0", Prompt: "a lighthouse", N: 2, Size: "1024x1024", Quality: "standard"}
		ginContext, recorder := bedrockActionContext(http.MethodPost, "/v1/images/generations", body)
		result, err := service.ForwardBedrockImageGeneration(context.Background(), ginContext, bedrockContractAccount(), body, parsed, "image.generate", "")
		require.NoError(test, err)
		require.Equal(test, "https://bedrock-runtime.us-east-1.amazonaws.com/model/amazon.nova-canvas-v1%3A0/invoke", transport.lastReq.URL.String())
		require.True(test, strings.HasPrefix(transport.lastReq.Header.Get("Authorization"), "AWS4-HMAC-SHA256"))
		sent := gjson.ParseBytes(transport.lastBody)
		require.Equal(test, "TEXT_IMAGE", sent.Get("taskType").String())
		require.Equal(test, "a lighthouse", sent.Get("textToImageParams.text").String())
		require.Equal(test, "blurry", sent.Get("textToImageParams.negativeText").String())
		require.EqualValues(test, 2, sent.Get("imageGenerationConfig.numberOfImages").Int())
		require.EqualValues(test, 1024, sent.Get("imageGenerationConfig.width").Int())
		require.EqualValues(test, 1024, sent.Get("imageGenerationConfig.height").Int())
		require.Equal(test, "standard", sent.Get("imageGenerationConfig.quality").String())
		require.Equal(test, pngBase64, gjson.GetBytes(recorder.Body.Bytes(), "data.0.b64_json").String())
		require.EqualValues(test, 1, result.ImageCount, "the returned image count is the billable unit")
		require.Equal(test, ImageBillingSize1K, result.ImageSize)
		require.Equal(test, []string{"1024x1024"}, result.ImageOutputSizes)
		require.Equal(test, "amazon.nova-canvas-v1:0", result.UpstreamModel)
	})

	test.Run("a masked edit uses the documented inpainting task", func(test *testing.T) {
		raw := []byte("fixture-image-bytes")
		transport := &httpUpstreamRecorder{resp: bedrockJSONResponse(200, `{"images":["`+base64.StdEncoding.EncodeToString(raw)+`"]}`)}
		service := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: transport}
		parsed := &OpenAIImagesRequest{
			Model:      "amazon.nova-canvas-v1:0",
			Prompt:     "make it dusk",
			N:          1,
			Uploads:    []OpenAIImagesUpload{{FieldName: "image", Data: raw}},
			MaskUpload: &OpenAIImagesUpload{FieldName: "mask", Data: []byte("fixture-mask")},
		}
		body := []byte(`{"model":"amazon.nova-canvas-v1:0","prompt":"make it dusk"}`)
		ginContext, _ := bedrockActionContext(http.MethodPost, "/v1/images/edits", body)
		_, err := service.ForwardBedrockImageGeneration(context.Background(), ginContext, bedrockContractAccount(), body, parsed, "image.edit", "")
		require.NoError(test, err)
		sent := gjson.ParseBytes(transport.lastBody)
		require.Equal(test, "INPAINTING", sent.Get("taskType").String())
		require.Equal(test, base64.StdEncoding.EncodeToString(raw), sent.Get("inPaintingParams.image").String())
		require.Equal(test, base64.StdEncoding.EncodeToString([]byte("fixture-mask")), sent.Get("inPaintingParams.maskImage").String())
		require.Equal(test, "make it dusk", sent.Get("inPaintingParams.text").String())
		require.False(test, sent.Get("imageGenerationConfig.width").Exists(), "the documented inpainting task takes no output size")
	})

	test.Run("a maskless edit uses documented image conditioning", func(test *testing.T) {
		raw := []byte("fixture-image-bytes")
		transport := &httpUpstreamRecorder{resp: bedrockJSONResponse(200, `{"images":["`+base64.StdEncoding.EncodeToString(raw)+`"]}`)}
		service := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: transport}
		parsed := &OpenAIImagesRequest{Model: "amazon.titan-image-generator-v2:0", Prompt: "make it dusk", N: 1, Uploads: []OpenAIImagesUpload{{FieldName: "image", Data: raw}}}
		body := []byte(`{"model":"amazon.titan-image-generator-v2:0","prompt":"make it dusk"}`)
		ginContext, _ := bedrockActionContext(http.MethodPost, "/v1/images/edits", body)
		_, err := service.ForwardBedrockImageGeneration(context.Background(), ginContext, bedrockContractAccount(), body, parsed, "image.edit", "")
		require.NoError(test, err)
		sent := gjson.ParseBytes(transport.lastBody)
		require.Equal(test, "TEXT_IMAGE", sent.Get("taskType").String())
		require.Equal(test, base64.StdEncoding.EncodeToString(raw), sent.Get("textToImageParams.conditionImage").String())
		require.Equal(test, "make it dusk", sent.Get("textToImageParams.text").String())
	})

	test.Run("titan v1 rejects a maskless edit it cannot express", func(test *testing.T) {
		transport := &httpUpstreamRecorder{resp: bedrockJSONResponse(200, `{"images":["`+base64.StdEncoding.EncodeToString([]byte("i"))+`"]}`)}
		service := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: transport}
		parsed := &OpenAIImagesRequest{Model: "amazon.titan-image-generator-v1", Prompt: "p", N: 1, Uploads: []OpenAIImagesUpload{{Data: []byte("i")}}}
		body := []byte(`{"model":"amazon.titan-image-generator-v1","prompt":"p"}`)
		ginContext, _ := bedrockActionContext(http.MethodPost, "/v1/images/edits", body)
		_, err := service.ForwardBedrockImageGeneration(context.Background(), ginContext, bedrockContractAccount(), body, parsed, "image.edit", "")
		require.ErrorContains(test, err, "no maskless edit task")
		require.Nil(test, transport.lastReq)
	})

	test.Run("variation reaches the documented variation task", func(test *testing.T) {
		transport := &httpUpstreamRecorder{resp: bedrockJSONResponse(200, `{"images":["`+base64.StdEncoding.EncodeToString([]byte("i"))+`"]}`)}
		service := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: transport}
		parsed := &OpenAIImagesRequest{Model: "amazon.nova-canvas-v1:0", N: 1, Uploads: []OpenAIImagesUpload{{Data: []byte("i")}}}
		body := []byte(`{"model":"amazon.nova-canvas-v1:0"}`)
		ginContext, _ := bedrockActionContext(http.MethodPost, "/v1/images/variations", body)
		_, err := service.ForwardBedrockImageGeneration(context.Background(), ginContext, bedrockContractAccount(), body, parsed, "image.variation", "")
		require.NoError(test, err)
		require.Equal(test, "IMAGE_VARIATION", gjson.GetBytes(transport.lastBody, "taskType").String())
		require.Equal(test, base64.StdEncoding.EncodeToString([]byte("i")), gjson.GetBytes(transport.lastBody, "imageVariationParams.images.0").String())
	})

	test.Run("stability uses its own envelope and treats filtering as a failure", func(test *testing.T) {
		okBody := `{"seeds":[1],"finish_reasons":[null],"images":["` + base64.StdEncoding.EncodeToString([]byte("s")) + `"]}`
		transport := &httpUpstreamRecorder{resp: bedrockJSONResponse(200, okBody)}
		service := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: transport}
		body := []byte(`{"model":"stability.sd3-5-large-v1:0","prompt":"a fox","aspect_ratio":"16:9","output_format":"png"}`)
		parsed := &OpenAIImagesRequest{Model: "stability.sd3-5-large-v1:0", Prompt: "a fox", N: 1}
		ginContext, _ := bedrockActionContext(http.MethodPost, "/v1/images/generations", body)
		_, err := service.ForwardBedrockImageGeneration(context.Background(), ginContext, bedrockContractAccount(), body, parsed, "image.generate", "")
		require.NoError(test, err)
		sent := gjson.ParseBytes(transport.lastBody)
		require.Equal(test, "text-to-image", sent.Get("mode").String())
		require.Equal(test, "16:9", sent.Get("aspect_ratio").String())
		require.Equal(test, "a fox", sent.Get("prompt").String())
		require.False(test, sent.Get("taskType").Exists(), "stability does not use the nova/titan task envelope")

		filtered := &httpUpstreamRecorder{resp: bedrockJSONResponse(200, `{"finish_reasons":["Filter reason: prompt"]}`)}
		filteredService := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: filtered}
		filteredContext, _ := bedrockActionContext(http.MethodPost, "/v1/images/generations", body)
		_, err = filteredService.ForwardBedrockImageGeneration(context.Background(), filteredContext, bedrockContractAccount(), body, parsed, "image.generate", "")
		require.ErrorContains(test, err, "filtered")
	})

	test.Run("stability image-to-image requires the documented strength", func(test *testing.T) {
		transport := &httpUpstreamRecorder{resp: bedrockJSONResponse(200, `{"seeds":[1],"finish_reasons":[null],"images":["`+base64.StdEncoding.EncodeToString([]byte("s"))+`"]}`)}
		service := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: transport}
		parsed := &OpenAIImagesRequest{Model: "stability.sd3-5-large-v1:0", Prompt: "a fox", N: 1, Uploads: []OpenAIImagesUpload{{Data: []byte("i")}}}
		for _, scenario := range []struct {
			body     string
			ok       bool
			strength string
		}{
			{`{"model":"stability.sd3-5-large-v1:0","prompt":"a fox","strength":0.6}`, true, "0.6"},
			{`{"model":"stability.sd3-5-large-v1:0","prompt":"a fox"}`, false, ""},
			{`{"model":"stability.sd3-5-large-v1:0","prompt":"a fox","strength":1.5}`, false, ""},
		} {
			ginContext, _ := bedrockActionContext(http.MethodPost, "/v1/images/edits", []byte(scenario.body))
			_, err := service.ForwardBedrockImageGeneration(context.Background(), ginContext, bedrockContractAccount(), []byte(scenario.body), parsed, "image.edit", "")
			if !scenario.ok {
				require.ErrorContains(test, err, "strength", scenario.body)
				continue
			}
			require.NoError(test, err, scenario.body)
			require.Equal(test, "image-to-image", gjson.GetBytes(transport.lastBody, "mode").String())
			require.Equal(test, scenario.strength, gjson.GetBytes(transport.lastBody, "strength").Raw)
		}
	})

	test.Run("unknown families and non-bedrock accounts never reach upstream", func(test *testing.T) {
		transport := &httpUpstreamRecorder{resp: bedrockJSONResponse(200, `{"images":["`+base64.StdEncoding.EncodeToString([]byte("i"))+`"]}`)}
		service := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: transport}
		body := []byte(`{"model":"stability.sd3-large-v1:0","prompt":"p"}`)
		parsed := &OpenAIImagesRequest{Model: "stability.sd3-large-v1:0", Prompt: "p", N: 1}
		ginContext, _ := bedrockActionContext(http.MethodPost, "/v1/images/generations", body)
		_, err := service.ForwardBedrockImageGeneration(context.Background(), ginContext, bedrockContractAccount(), body, parsed, "image.generate", "")
		require.ErrorContains(test, err, "unsupported Bedrock image model")

		otherPlatform := bedrockContractAccount()
		otherPlatform.Platform = PlatformOpenAI
		otherPlatform.Type = AccountTypeAPIKey
		novaBody := []byte(`{"model":"amazon.nova-canvas-v1:0","prompt":"p"}`)
		novaParsed := &OpenAIImagesRequest{Model: "amazon.nova-canvas-v1:0", Prompt: "p", N: 1}
		otherContext, _ := bedrockActionContext(http.MethodPost, "/v1/images/generations", novaBody)
		_, err = service.ForwardBedrockImageGeneration(context.Background(), otherContext, otherPlatform, novaBody, novaParsed, "image.generate", "")
		require.ErrorContains(test, err, "bedrock account")
		require.Nil(test, transport.lastReq)
	})
}

func TestBedrockActionCancellationAndIdempotency(test *testing.T) {
	gin.SetMode(gin.TestMode)

	test.Run("client cancellation is retained and never retried", func(test *testing.T) {
		transport := &httpUpstreamRecorder{resp: bedrockJSONResponse(200, `{"embedding":[1],"inputTextTokenCount":2}`)}
		service := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: transport}
		body := []byte(`{"model":"amazon.titan-embed-text-v2:0","input":"alpha"}`)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		ginContext, _ := bedrockActionContext(http.MethodPost, "/v1/embeddings", body)
		result, err := service.ForwardBedrockEmbeddings(ctx, ginContext, bedrockContractAccount(), body, "")
		require.ErrorContains(test, err, "context canceled")
		require.Nil(test, result)
		require.Empty(test, transport.requests, "a cancelled client must not reach upstream and must not be retried")
	})

	test.Run("a repeated identical request is a fresh upstream call with no adapter-side replay", func(test *testing.T) {
		transport := &httpUpstreamRecorder{responses: []*http.Response{
			bedrockJSONResponse(200, `{"results":[{"index":0,"relevanceScore":0.5}]}`),
			bedrockJSONResponse(200, `{"results":[{"index":0,"relevanceScore":0.5}]}`),
		}}
		service := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: transport}
		body := []byte(`{"model":"cohere.rerank-v3-5:0","query":"q","documents":["a"]}`)
		for i := 0; i < 2; i++ {
			ginContext, _ := bedrockActionContext(http.MethodPost, "/v1/rerank", body)
			_, err := service.ForwardBedrockRerank(context.Background(), ginContext, bedrockContractAccount(), body)
			require.NoError(test, err)
		}
		require.Len(test, transport.requests, 2, "the adapter holds no idempotency cache; the request plan owns replay")
	})

	test.Run("a bearer api key account is not SigV4 signed", func(test *testing.T) {
		transport := &httpUpstreamRecorder{resp: bedrockJSONResponse(200, `{"embedding":[1],"inputTextTokenCount":1}`)}
		service := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: transport}
		account := bedrockContractAccount()
		account.Credentials = map[string]any{"api_key": "fixture-key", "auth_mode": "apikey", "aws_region": "us-east-1"}
		body := []byte(`{"model":"amazon.titan-embed-text-v2:0","input":"alpha"}`)
		ginContext, _ := bedrockActionContext(http.MethodPost, "/v1/embeddings", body)
		_, err := service.ForwardBedrockEmbeddings(context.Background(), ginContext, account, body, "")
		require.NoError(test, err)
		require.Equal(test, "Bearer fixture-key", transport.lastReq.Header.Get("Authorization"))
	})

	test.Run("the account region drives the runtime endpoint", func(test *testing.T) {
		transport := &httpUpstreamRecorder{resp: bedrockJSONResponse(200, `{"embedding":[1],"inputTextTokenCount":1}`)}
		service := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: transport}
		account := bedrockContractAccount()
		account.Credentials["aws_region"] = "eu-west-1"
		body := []byte(`{"model":"amazon.titan-embed-text-v2:0","input":"alpha"}`)
		ginContext, _ := bedrockActionContext(http.MethodPost, "/v1/embeddings", body)
		_, err := service.ForwardBedrockEmbeddings(context.Background(), ginContext, account, body, "")
		require.NoError(test, err)
		require.Equal(test, "https://bedrock-runtime.eu-west-1.amazonaws.com/model/amazon.titan-embed-text-v2%3A0/invoke", transport.lastReq.URL.String())
	})
}

// TestBedrockCountTokensTrunkDelegation proves the Anthropic-shaped trunk
// (ForwardCountTokens) now serves Bedrock through the native CountTokens
// operation instead of the previous 404.
func TestBedrockCountTokensTrunkDelegation(test *testing.T) {
	gin.SetMode(gin.TestMode)

	test.Run("a bedrock account reaches the native count-tokens operation", func(test *testing.T) {
		account := bedrockContractAccount()
		target := "us.anthropic.claude-sonnet-4-5-20250929-v1:0"
		account.Credentials["model_mapping"] = map[string]any{"claude-sonnet-4-5": target}
		body := []byte(`{"model":"claude-sonnet-4-5","max_tokens":64,"messages":[{"role":"user","content":"hello"}]}`)
		transport := &httpUpstreamRecorder{resp: bedrockJSONResponse(200, `{"inputTokens":42}`)}
		gateway := &GatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: transport}
		requestContext, recorder := bedrockActionContext(http.MethodPost, "/v1/messages/count_tokens", body)
		parsed := &ParsedRequest{Body: NewRequestBodyRef(body), Model: "claude-sonnet-4-5"}
		require.NoError(test, gateway.ForwardCountTokens(context.Background(), requestContext, account, parsed))

		require.Equal(test, "https://bedrock-runtime.us-east-1.amazonaws.com/model/us.anthropic.claude-sonnet-4-5-20250929-v1%3A0/count-tokens", transport.lastReq.URL.String())
		require.True(test, strings.HasPrefix(transport.lastReq.Header.Get("Authorization"), "AWS4-HMAC-SHA256"))
		require.NotEmpty(test, gjson.GetBytes(transport.lastBody, "input.invokeModel.body").String())
		require.Equal(test, `{"input_tokens":42}`, recorder.Body.String())
		require.NotContains(test, recorder.Body.String(), "not supported for Bedrock")
	})

	test.Run("an unknown token count still fails through the trunk", func(test *testing.T) {
		account := bedrockContractAccount()
		account.Credentials["model_mapping"] = map[string]any{"alias": "amazon.nova-lite-v1:0"}
		body := []byte(`{"model":"alias","messages":[{"role":"user","content":"hi"}]}`)
		transport := &httpUpstreamRecorder{resp: bedrockJSONResponse(200, `{}`)}
		gateway := &GatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: transport}
		requestContext, recorder := bedrockActionContext(http.MethodPost, "/v1/messages/count_tokens", body)
		parsed := &ParsedRequest{Body: NewRequestBodyRef(body), Model: "alias"}
		require.Error(test, gateway.ForwardCountTokens(context.Background(), requestContext, account, parsed))
		require.Contains(test, recorder.Body.String(), `"error"`)
	})
}

// TestBedrockCatalogServingEndpoints covers the control-plane modality
// projection: each output modality maps to the endpoint its serving family
// adapter implements, and VIDEO stays unmapped until the async job adapter
// exists.
func TestBedrockCatalogServingEndpoints(test *testing.T) {
	require.Equal(test, []string{CompositeRouteEndpointMessages}, bedrockCatalogServingEndpoints([]string{"TEXT"}))
	require.Equal(test, []string{CompositeRouteEndpointEmbeddings}, bedrockCatalogServingEndpoints([]string{"EMBEDDING"}))
	require.Equal(test, []string{CompositeRouteEndpointImages}, bedrockCatalogServingEndpoints([]string{"IMAGE"}))
	require.Equal(test, []string{CompositeRouteEndpointEmbeddings}, bedrockCatalogServingEndpoints([]string{"embedding"}), "the projection normalizes case")
	require.Empty(test, bedrockCatalogServingEndpoints([]string{"VIDEO"}), "Nova Reel output stays out until its job adapter exists")
	require.Empty(test, bedrockCatalogServingEndpoints(nil))
}

// TestBedrockImagesRequestParsing covers the JSON and multipart request shapes
// the image handler accepts, and the rejections that must never reach the
// adapter.
func TestBedrockImagesRequestParsing(test *testing.T) {
	gin.SetMode(gin.TestMode)

	test.Run("json generation parses the shared field names", func(test *testing.T) {
		body := []byte(`{"model":"amazon.nova-canvas-v1:0","prompt":"a lighthouse","n":2,"size":"1024x1024","quality":"standard","negative_prompt":"blurry","seed":7}`)
		ginContext, _ := bedrockActionContext(http.MethodPost, "/v1/images/generations", body)
		parsed, err := ParseBedrockImagesRequest(ginContext, body, "image.generate")
		require.NoError(test, err)
		require.Equal(test, "amazon.nova-canvas-v1:0", parsed.Model)
		require.Equal(test, "a lighthouse", parsed.Prompt)
		require.Equal(test, 2, parsed.N)
		require.Equal(test, "1024x1024", parsed.Size)
		require.Equal(test, ImageBillingSize1K, parsed.SizeTier)
		require.Equal(test, "standard", parsed.Quality)
		require.Empty(test, parsed.Uploads)
	})

	test.Run("json edits ask for the multipart image bytes", func(test *testing.T) {
		body := []byte(`{"model":"amazon.nova-canvas-v1:0","prompt":"dusk"}`)
		ginContext, _ := bedrockActionContext(http.MethodPost, "/v1/images/edits", body)
		_, err := ParseBedrockImagesRequest(ginContext, body, "image.edit")
		require.ErrorContains(test, err, "input image bytes")

		withURL := []byte(`{"model":"amazon.nova-canvas-v1:0","prompt":"dusk","images":[{"image_url":"https://example.invalid/a.png"}]}`)
		urlContext, _ := bedrockActionContext(http.MethodPost, "/v1/images/edits", withURL)
		_, err = ParseBedrockImagesRequest(urlContext, withURL, "image.edit")
		require.ErrorContains(test, err, "input image bytes")
	})

	test.Run("a missing model never reaches the adapter", func(test *testing.T) {
		body := []byte(`{"prompt":"a lighthouse"}`)
		ginContext, _ := bedrockActionContext(http.MethodPost, "/v1/images/generations", body)
		_, err := ParseBedrockImagesRequest(ginContext, body, "image.generate")
		require.ErrorContains(test, err, "model is required")
	})

	test.Run("multipart carries the image and mask bytes", func(test *testing.T) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		require.NoError(test, writer.WriteField("model", "amazon.nova-canvas-v1:0"))
		require.NoError(test, writer.WriteField("prompt", "make it dusk"))
		require.NoError(test, writer.WriteField("n", "1"))
		imagePart, err := writer.CreateFormFile("image", "input.png")
		require.NoError(test, err)
		_, err = imagePart.Write([]byte("fixture-image-bytes"))
		require.NoError(test, err)
		maskPart, err := writer.CreateFormFile("mask", "mask.png")
		require.NoError(test, err)
		_, err = maskPart.Write([]byte("fixture-mask-bytes"))
		require.NoError(test, err)
		require.NoError(test, writer.Close())

		recorder := httptest.NewRecorder()
		ginContext, _ := gin.CreateTestContext(recorder)
		ginContext.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", bytes.NewReader(body.Bytes()))
		ginContext.Request.Header.Set("Content-Type", writer.FormDataContentType())
		parsed, err := ParseBedrockImagesRequest(ginContext, body.Bytes(), "image.edit")
		require.NoError(test, err)
		require.Equal(test, "amazon.nova-canvas-v1:0", parsed.Model)
		require.Equal(test, "make it dusk", parsed.Prompt)
		require.Len(test, parsed.Uploads, 1)
		require.Equal(test, []byte("fixture-image-bytes"), parsed.Uploads[0].Data)
		require.NotNil(test, parsed.MaskUpload)
		require.Equal(test, []byte("fixture-mask-bytes"), parsed.MaskUpload.Data)
	})
}

// TestBedrockImageBillingUnits proves the image adapter reports the complete
// billable unit the shared pricing resolver consumes: the returned image count,
// the requested size tier and the sizes detected from the returned bytes. A
// missing unit would silently under-bill rather than fail.
func TestBedrockImageBillingUnits(test *testing.T) {
	gin.SetMode(gin.TestMode)

	test.Run("the requested and detected size reach the pricing resolver", func(test *testing.T) {
		pngBase64 := bedrockPNG(test, 1024, 1024)
		transport := &httpUpstreamRecorder{resp: bedrockJSONResponse(200, `{"images":["`+pngBase64+`","`+pngBase64+`"]}`)}
		service := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: transport}
		body := []byte(`{"model":"amazon.nova-canvas-v1:0","prompt":"a lighthouse","n":2,"size":"1024x1024"}`)
		parsed := &OpenAIImagesRequest{Model: "amazon.nova-canvas-v1:0", Prompt: "a lighthouse", N: 2, Size: "1024x1024"}
		ginContext, _ := bedrockActionContext(http.MethodPost, "/v1/images/generations", body)
		result, err := service.ForwardBedrockImageGeneration(context.Background(), ginContext, bedrockContractAccount(), body, parsed, "image.generate", "")
		require.NoError(test, err)
		require.Equal(test, 2, result.ImageCount)
		require.Equal(test, ImageBillingSize1K, result.ImageSize)
		require.Equal(test, []string{"1024x1024", "1024x1024"}, result.ImageOutputSizes)

		ApplyOpenAIImageBillingResolution(result)
		require.Equal(test, ImageBillingSize1K, result.ImageSize)
		require.Equal(test, ImageSizeSourceOutput, result.ImageSizeSource, "the returned bytes are the authoritative tier source")
		require.Equal(test, map[string]int{ImageBillingSize1K: 2}, result.ImageSizeBreakdown)
	})

	test.Run("a requested tier without a detected size still bills on the request", func(test *testing.T) {
		transport := &httpUpstreamRecorder{resp: bedrockJSONResponse(200, `{"images":["`+base64.StdEncoding.EncodeToString([]byte("not-an-image"))+`"]}`)}
		service := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: transport}
		body := []byte(`{"model":"amazon.nova-canvas-v1:0","prompt":"a lighthouse","size":"2048x2048"}`)
		parsed := &OpenAIImagesRequest{Model: "amazon.nova-canvas-v1:0", Prompt: "a lighthouse", N: 1, Size: "2048x2048"}
		ginContext, _ := bedrockActionContext(http.MethodPost, "/v1/images/generations", body)
		result, err := service.ForwardBedrockImageGeneration(context.Background(), ginContext, bedrockContractAccount(), body, parsed, "image.generate", "")
		require.NoError(test, err)
		require.Equal(test, 1, result.ImageCount)
		require.Empty(test, result.ImageOutputSizes, "an undetectable payload is not invented into a size")

		ApplyOpenAIImageBillingResolution(result)
		require.Equal(test, ImageBillingSize2K, result.ImageSize)
		require.Equal(test, ImageSizeSourceInput, result.ImageSizeSource)
	})
}
