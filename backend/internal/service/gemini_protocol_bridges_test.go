package service

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestGeminiIngressToolCorrelationAndControls(t *testing.T) {
	body := []byte(`{"metadata":{"future":true},"systemInstruction":{"parts":[{"text":"system"}]},"contents":[{"role":"model","parts":[{"functionCall":{"id":"a","name":"lookup","args":{"q":"a"}},"thoughtSignature":"native-secret"},{"functionCall":{"id":"b","name":"lookup","args":{"q":"b"}}}]},{"role":"user","parts":[{"functionResponse":{"id":"b","name":"lookup","response":{"answer":2}}},{"functionResponse":{"id":"a","name":"lookup","response":{"answer":1}}}]}],"tools":[{"functionDeclarations":[{"name":"lookup","parameters":{"type":"OBJECT","properties":{"q":{"type":"STRING"}}}}]}],"generationConfig":{"maxOutputTokens":42,"temperature":0.2,"stopSequences":["END"]},"toolConfig":{"functionCallingConfig":{"mode":"ANY","allowedFunctionNames":["lookup"]}}}`)
	got, err := GeminiToMessages(body, "public/model", true)
	require.NoError(t, err)
	require.Equal(t, "public/model", gjson.GetBytes(got, "model").String())
	require.True(t, gjson.GetBytes(got, "stream").Bool())
	require.Equal(t, int64(42), gjson.GetBytes(got, "max_tokens").Int())
	require.Equal(t, "system", gjson.GetBytes(got, "system").String())
	require.Equal(t, "b", gjson.GetBytes(got, "messages.1.content.0.tool_use_id").String())
	require.Equal(t, "a", gjson.GetBytes(got, "messages.1.content.1.tool_use_id").String())
	require.Equal(t, "object", gjson.GetBytes(got, "tools.0.input_schema.type").String())
	require.Equal(t, "tool", gjson.GetBytes(got, "tool_choice.type").String())
	require.NotContains(t, string(got), "native-secret")
	back, err := convertClaudeMessagesToGeminiGenerateContent(got)
	require.NoError(t, err)
	require.Equal(t, "b", gjson.GetBytes(back, "contents.1.parts.0.functionResponse.id").String())
	require.Equal(t, "ANY", gjson.GetBytes(back, "toolConfig.functionCallingConfig.mode").String())
}

func TestGeminiIngressPlaintextThoughtReplay(t *testing.T) {
	for _, signature := range []string{"", `,"thoughtSignature":"opaque-native-secret"`} {
		body := []byte(`{"contents":[{"role":"model","parts":[{"thought":true,"text":"  fixture thought\n"` + signature + `},{"thought":true,"text":"second thought"},{"text":"ordinary text"},{"functionCall":{"id":"echo-call","name":"validation_echo","args":{"value":"echoed"}},"thoughtSignature":"opaque-call-secret"}]},{"role":"user","parts":[{"functionResponse":{"id":"echo-call","name":"validation_echo","response":{"value":"echoed","ok":true}}}]}],"generationConfig":{"thinkingConfig":{"includeThoughts":true}}}`)
		converted, err := GeminiToMessages(body, "private-custom-model", true)
		require.NoError(t, err)
		require.JSONEq(t, `[{"type":"thinking","thinking":"  fixture thought\n"},{"type":"thinking","thinking":"second thought"},{"type":"text","text":"ordinary text"},{"type":"tool_use","id":"echo-call","name":"validation_echo","input":{"value":"echoed"}}]`, gjson.GetBytes(converted, "messages.0.content").Raw)
		require.Equal(t, "assistant", gjson.GetBytes(converted, "messages.0.role").String())
		require.Equal(t, "echo-call", gjson.GetBytes(converted, "messages.1.content.0.tool_use_id").String())
		require.Equal(t, "tool_result", gjson.GetBytes(converted, "messages.1.content.0.type").String())
		require.JSONEq(t, `{"value":"echoed","ok":true}`, gjson.GetBytes(converted, "messages.1.content.0.content").String())
		require.NotContains(t, string(converted), "opaque-")
		require.NotContains(t, string(converted), "signature")
		require.NotContains(t, string(converted), "thoughtSignature")
		require.False(t, gjson.GetBytes(converted, "thinking").Exists())
	}
}

func TestGeminiIngressUnrepresentableThoughtReplay(t *testing.T) {
	for _, scenario := range []struct{ name, role, part, message string }{
		{"empty", "model", `{"thought":true,"text":""}`, "Gemini thought replay cannot be authenticated by another provider"},
		{"whitespace", "model", `{"thought":true,"text":" \n\t"}`, "Gemini thought replay cannot be authenticated by another provider"},
		{"user", "user", `{"thought":true,"text":"fixture thought"}`, "Gemini thought replay cannot be authenticated by another provider"},
		{"nonstring", "model", `{"thought":true,"text":42}`, "part text must be a string"},
		{"signature_only", "model", `{"thoughtSignature":"opaque-secret"}`, "part must contain exactly one supported content field"},
		{"missing_text", "model", `{"thought":true}`, "part must contain exactly one supported content field"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			_, err := GeminiToMessages([]byte(fmt.Sprintf(`{"contents":[{"role":%q,"parts":[%s]}]}`, scenario.role, scenario.part)), "private-custom-model", false)
			require.EqualError(t, err, scenario.message)
		})
	}
}

func TestGeminiIngressRejectsLossySemanticParts(t *testing.T) {
	for _, body := range []string{
		`{"contents":[{"parts":[{"inlineData":{"mimeType":"audio/wav","data":"YQ=="}}]}]}`,
		`{"contents":[{"parts":[{"fileData":{"fileUri":"https://example.com/a.png"}}]}]}`,
		`{"contents":[{"parts":[{"text":"x"}]}],"generationConfig":{"responseModalities":["IMAGE"]}}`,
		`{"contents":[{"role":"model","parts":[{"functionCall":{"name":"f","args":{}}},{"functionCall":{"name":"f","args":{}}}]},{"role":"user","parts":[{"functionResponse":{"name":"f","response":{}}}]}]}`,
		`{"contents":[{"parts":[{"functionResponse":{"id":"absent","name":"f","response":{}}}]}]}`,
	} {
		_, err := GeminiToMessages([]byte(body), "m", false)
		require.Error(t, err, body)
	}
	image, err := GeminiToMessages([]byte(`{"contents":[{"parts":[{"inlineData":{"mimeType":"image/png","data":"YQ=="}}]}]}`), "m", false)
	require.NoError(t, err)
	require.Equal(t, "image", gjson.GetBytes(image, "messages.0.content.0.type").String())
}

func TestGeminiProtoJSONAliasesPreserveSchemaAndArgumentData(t *testing.T) {
	body := []byte(`{"system_instruction":{"parts":[{"text":"first"},{"text":"second"}]},"generation_config":{"max_output_tokens":17,"top_p":0.5},"contents":[{"role":"model","parts":[{"function_call":{"id":"id","name":"f","args":{"system_instruction":"literal","type":"OBJECT"}},"thought_signature":"secret"}]},{"role":"user","parts":[{"function_response":{"id":"id","name":"f","response":{"inline_data":"literal"}}}]}],"tools":[{"function_declarations":[{"name":"f","parameters":{"type":"OBJECT","properties":{"type":{"type":"STRING","enum":["UPPER","LOWER"]}},"default":{"type":"UPPER"}}}]}]}`)
	converted, err := GeminiToMessages(body, "m", false)
	require.NoError(t, err)
	require.Equal(t, "firstsecond", gjson.GetBytes(converted, "system").String())
	require.Equal(t, int64(17), gjson.GetBytes(converted, "max_tokens").Int())
	require.Equal(t, "literal", gjson.GetBytes(converted, "messages.0.content.0.input.system_instruction").String())
	require.Equal(t, "OBJECT", gjson.GetBytes(converted, "messages.0.content.0.input.type").String())
	require.Equal(t, "string", gjson.GetBytes(converted, "tools.0.input_schema.properties.type.type").String())
	require.Equal(t, "UPPER", gjson.GetBytes(converted, "tools.0.input_schema.properties.type.enum.0").String())
	require.Equal(t, "UPPER", gjson.GetBytes(converted, "tools.0.input_schema.default.type").String())
	_, err = GeminiToMessages([]byte(`{"systemInstruction":{"parts":[{"text":"x"}]},"system_instruction":{"parts":[{"text":"y"}]},"contents":[{"parts":[{"text":"hi"}]}]}`), "m", false)
	require.ErrorContains(t, err, "conflicting")
}

func TestGeminiEmbeddingConversionAndVectorOrder(t *testing.T) {
	body := []byte(`{"requests":[{"model":"models/m","content":{"parts":[{"text":"first"}]}},{"model":"m","content":{"parts":[{"text":"second"}]}}]}`)
	converted, count, err := GeminiToEmbeddings(body, "m", true)
	require.NoError(t, err)
	require.Equal(t, 2, count)
	require.Equal(t, `["first","second"]`, gjson.GetBytes(converted, "input").Raw)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	writer := NewGeminiProtocolWriter(c.Writer, "m", "batchEmbedContents", false, count)
	_, err = writer.WriteString(`{"data":[{"index":1,"embedding":[3,4]},{"index":0,"embedding":[1,2]}]}`)
	require.NoError(t, err)
	require.NoError(t, writer.Finish())
	require.JSONEq(t, `{"embeddings":[{"values":[1,2]},{"values":[3,4]}]}`, rec.Body.String())
	for _, bad := range []string{`{"content":{"parts":[{"inlineData":{"data":"YQ=="}}]}}`, `{"content":{"parts":[{"text":"x"}]},"outputDimensionality":{}}`, `{"content":{"parts":[{"text":"x"}]},"taskType":"RETRIEVAL_QUERY"}`} {
		_, _, err := GeminiToEmbeddings([]byte(bad), "m", false)
		require.Error(t, err)
	}
}

func TestGeminiReplyWriterToolsAndInvalidVectors(t *testing.T) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	w := NewGeminiProtocolWriter(c.Writer, "public", "streamGenerateContent", true, 0)
	w.Header().Set("Content-Type", "text/event-stream")
	for _, event := range []string{
		`{"type":"message_start","message":{"usage":{"input_tokens":5,"cache_read_input_tokens":2}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"call-a","name":"lookup","input":{}}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"q\":"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"\"x\"}"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":3}}`,
		`{"type":"message_stop"}`,
	} {
		_, err := w.WriteString("data: " + event + "\n\n")
		require.NoError(t, err)
	}
	require.NoError(t, w.Finish())
	require.Contains(t, rec.Body.String(), `"functionCall":{"args":{"q":"x"},"id":"call-a","name":"lookup"}`)
	require.Contains(t, rec.Body.String(), `"totalTokenCount":10`)
	require.NotContains(t, rec.Body.String(), "thoughtSignature")
	for _, reply := range []string{`{"data":[{"index":0,"embedding":[null]}]}`, `{"data":[{"embedding":[1]}]}`, `{"data":[{"index":0,"embedding":[]}]}`} {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		w := NewGeminiProtocolWriter(c.Writer, "m", "embedContent", false, 1)
		_, err := w.WriteString(reply)
		require.NoError(t, err)
		require.Error(t, w.Finish())
		require.Equal(t, 502, rec.Code)
		require.NotContains(t, rec.Body.String(), `"embedding"`)
	}
}

func geminiResponsesFixture(t *testing.T, body io.ReadCloser, stream bool) (*GeminiMessagesCompatService, *geminiCompatHTTPUpstreamStub, *Account) {
	t.Helper()
	contentType := "application/json"
	if stream {
		contentType = "text/event-stream"
	}
	upstream := &geminiCompatHTTPUpstreamStub{response: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{contentType}}, Body: body}}
	svc := &GeminiMessagesCompatService{httpUpstream: upstream, cfg: &config.Config{}}
	account := &Account{ID: 1, Platform: PlatformGemini, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "vendor-secret"}, Concurrency: 1}
	return svc, upstream, account
}

func TestGeminiResponsesForwardJSONToolUsageAndCallerModel(t *testing.T) {
	reply := `{"candidates":[{"content":{"parts":[{"text":"hello"},{"functionCall":{"id":"call_lookup","name":"lookup","args":{"q":"x"}}}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":9,"candidatesTokenCount":3,"cachedContentTokenCount":2}}`
	svc, upstream, account := geminiResponsesFixture(t, io.NopCloser(strings.NewReader(reply)), false)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	c.Request.Header.Set("Authorization", "Bearer caller-secret")
	result, err := svc.ForwardAsResponses(context.Background(), c, account, []byte(`{"model":"gemini-2.5-flash","input":[{"type":"function_call","call_id":"call_previous","name":"lookup","arguments":"{}"},{"type":"function_call_output","call_id":"call_previous","output":"answer"}],"tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}],"tool_choice":{"type":"function","name":"lookup"}}`), "PublicAlias")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 1, upstream.calls)
	require.Equal(t, "vendor-secret", upstream.lastReq.Header.Get("x-goog-api-key"))
	require.Empty(t, upstream.lastReq.Header.Get("Authorization"))
	require.Equal(t, "PublicAlias", gjson.Get(rec.Body.String(), "model").String())
	require.Equal(t, int64(9), gjson.Get(rec.Body.String(), "usage.input_tokens").Int())
	require.Equal(t, int64(3), gjson.Get(rec.Body.String(), "usage.output_tokens").Int())
	require.Contains(t, rec.Body.String(), `"call_id":"call_lookup"`)
	sent, _ := io.ReadAll(upstream.lastReq.Body)
	require.Equal(t, gjson.GetBytes(sent, "contents.0.parts.0.functionCall.id").String(), gjson.GetBytes(sent, "contents.1.parts.0.functionResponse.id").String())
	require.Contains(t, string(sent), `"mode":"ANY"`)
}

func TestGeminiResponsesStreamIsIncremental(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	svc, _, account := geminiResponsesFixture(t, reader, true)
	firstConsumed := make(chan struct{})
	result := make(chan error, 1)
	router := gin.New()
	router.POST("/v1/responses", func(c *gin.Context) {
		_, err := svc.ForwardAsResponses(c.Request.Context(), c, account, []byte(`{"model":"gemini-2.5-flash","input":"hi","stream":true}`), "caller-alias")
		result <- err
	})
	server := httptest.NewServer(router)
	defer server.Close()
	go func() {
		fmt.Fprint(writer, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"first\"}]}}]}\n\n")
		<-firstConsumed
		fmt.Fprint(writer, "data: {\"candidates\":[{\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":5,\"candidatesTokenCount\":1}}\n\n")
		writer.Close()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, "POST", server.URL+"/v1/responses", nil)
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	lines := bufio.NewReader(response.Body)
	var output strings.Builder
	for {
		line, err := lines.ReadString('\n')
		require.NoError(t, err)
		output.WriteString(line)
		if strings.Contains(line, `"delta":"first"`) {
			break
		}
	}
	close(firstConsumed)
	rest, err := io.ReadAll(lines)
	require.NoError(t, err)
	output.Write(rest)
	require.NoError(t, <-result)
	require.Contains(t, output.String(), "response.completed")
	require.Contains(t, output.String(), `"model":"caller-alias"`)
	require.Contains(t, output.String(), `"input_tokens":5`)
}

func TestGeminiResponsesErrorsNeverComplete(t *testing.T) {
	for _, reply := range []string{
		"data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"partial\"}]}}]}\n\n",
		"data: {\"error\":{\"code\":503,\"message\":\"failed upstream\"}}\n\n",
		"data: not-json\n\n",
	} {
		svc, _, account := geminiResponsesFixture(t, io.NopCloser(strings.NewReader(reply)), true)
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
		_, err := svc.ForwardAsResponses(context.Background(), c, account, []byte(`{"model":"gemini-2.5-flash","input":"hi","stream":true}`), "public")
		require.Error(t, err)
		require.Contains(t, rec.Body.String(), "response.failed")
		require.NotContains(t, rec.Body.String(), "response.completed")
	}
}

func TestGeminiResponsesBeforeOutputFailoverAndPolicyError(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusBadRequest} {
		svc, upstream, account := geminiResponsesFixture(t, io.NopCloser(strings.NewReader(`{"error":{"code":400,"message":"invalid tool schema","status":"INVALID_ARGUMENT"}}`)), false)
		upstream.response.StatusCode = status
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
		_, err := svc.ForwardAsResponses(context.Background(), c, account, []byte(`{"model":"gemini-2.5-flash","input":"hi"}`), "public")
		require.Error(t, err)
		if status == http.StatusUnauthorized {
			require.Empty(t, rec.Body.String())
			var failover *UpstreamFailoverError
			require.ErrorAs(t, err, &failover)
		} else {
			require.Equal(t, 400, rec.Code)
			require.Contains(t, rec.Body.String(), `"error"`)
		}
	}
}

func TestGeminiResponsesRejectsMediaBeforeTransport(t *testing.T) {
	for _, input := range []string{`[{"role":"user","content":[{"type":"input_audio","data":"x"}]}]`, `[{"role":"user","content":[{"type":"input_image","image_url":"https://example.com/image.png"}]}]`} {
		svc, upstream, account := geminiResponsesFixture(t, io.NopCloser(bytes.NewReader(nil)), false)
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
		_, err := svc.ForwardAsResponses(context.Background(), c, account, []byte(`{"model":"gemini-2.5-flash","input":`+input+`}`), "m")
		require.Error(t, err)
		require.Zero(t, upstream.calls)
		require.Equal(t, 400, rec.Code)
	}
}
