package service

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestGeminiWriterOmitsUnrequestedThoughts(t *testing.T) {
	for _, stream := range []bool{false, true} {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		writer := NewGeminiProtocolWriter(ctx.Writer, "m", "generateContent", stream, 0)
		if stream {
			writer.Header().Set("Content-Type", "text/event-stream")
			_, err := writer.Write([]byte("data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"private summary\"}}\n\n"))
			require.NoError(t, err)
			require.False(t, writer.Written())
		} else {
			_, err := writer.Write([]byte(`{"type":"message","content":[{"type":"thinking","thinking":"private summary","signature":"opaque"},{"type":"text","text":"answer"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":2}}`))
			require.NoError(t, err)
			require.NoError(t, writer.Finish())
			require.Contains(t, recorder.Body.String(), "answer")
		}
		require.NotContains(t, recorder.Body.String(), "private summary")
		require.NotContains(t, recorder.Body.String(), "opaque")
	}
}

func TestGeminiWriterReturnsRequestedThoughtsWithoutSignature(t *testing.T) {
	for _, config := range []string{`{"generationConfig":{"thinkingConfig":{"includeThoughts":true}}}`, `{"generation_config":{"thinking_config":{"include_thoughts":true}}}`} {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		writer := NewGeminiProtocolWriter(ctx.Writer, "m", "generateContent", false, 0)
		writer.ConfigureThinking([]byte(config))
		_, err := writer.Write([]byte(`{"type":"message","content":[{"type":"thinking","thinking":"actual summary","signature":"opaque"},{"type":"text","text":"answer"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":2}}`))
		require.NoError(t, err)
		require.NoError(t, writer.Finish())
		require.Contains(t, recorder.Body.String(), "actual summary")
		require.NotContains(t, recorder.Body.String(), "opaque")
		require.NotContains(t, recorder.Body.String(), "thoughtSignature")
	}
}

func TestGeminiIngressThinkingAndStructuredOutput(t *testing.T) {
	for _, config := range []string{
		`{"thinkingConfig":{"thinkingBudget":8192},"responseMimeType":"application/json","responseSchema":{"type":"OBJECT","properties":{"thinking_budget":{"type":"STRING","enum":["OBJECT"]}}}}`,
		`{"thinking_config":{"thinking_budget":8192},"response_mime_type":"application/json","response_json_schema":{"type":"object","properties":{"thinking_budget":{"type":"string","enum":["OBJECT"]}}}}`,
	} {
		body := []byte(`{"systemInstruction":{"parts":[{"text":"Return JSON"}]},"contents":[{"parts":[{"text":"answer"}]}],"generationConfig":` + config + `}`)
		converted, err := GeminiToMessages(body, "public", false)
		require.NoError(t, err)
		require.Equal(t, int64(8192), gjson.GetBytes(converted, "thinking.budget_tokens").Int())
		require.Equal(t, "OBJECT", gjson.GetBytes(converted, "output_config.format.schema.properties.thinking_budget.enum.0").String())
		var messages apicompat.AnthropicRequest
		require.NoError(t, json.Unmarshal(converted, &messages))
		responses, err := apicompat.AnthropicToResponses(&messages)
		require.NoError(t, err)
		require.Equal(t, "medium", responses.Reasoning.Effort)
		require.Equal(t, "string", gjson.GetBytes(responses.Text.Format, "schema.properties.thinking_budget.type").String())
		chat, err := apicompat.AnthropicToChatCompletionsRequest(&messages)
		require.NoError(t, err)
		require.Equal(t, "medium", chat.ReasoningEffort)
		require.Equal(t, "string", gjson.GetBytes(chat.ResponseFormat, "json_schema.schema.properties.thinking_budget.type").String())
	}
}

func TestGeminiIngressThinkingModes(t *testing.T) {
	for _, scenario := range []struct{ config, kind, effort string }{
		{`{"thinkingBudget":0}`, "disabled", "none"},
		{`{"thinkingBudget":-1}`, "adaptive", ""},
		{`{"thinkingLevel":"HIGH"}`, "adaptive", "high"},
		{`{"thinkingLevel":"MINIMAL"}`, "adaptive", "minimal"},
	} {
		converted, err := GeminiToMessages([]byte(`{"contents":[{"parts":[{"text":"hello"}]}],"generationConfig":{"thinkingConfig":`+scenario.config+`}}`), "m", false)
		require.NoError(t, err)
		require.Equal(t, scenario.kind, gjson.GetBytes(converted, "thinking.type").String())
		var messages apicompat.AnthropicRequest
		require.NoError(t, json.Unmarshal(converted, &messages))
		responses, err := apicompat.AnthropicToResponses(&messages)
		require.NoError(t, err)
		if scenario.effort != "" {
			require.Equal(t, scenario.effort, responses.Reasoning.Effort)
		}
	}
}

func TestGeminiIngressInvalidControls(t *testing.T) {
	for _, config := range []string{
		`{"thinkingConfig":{"thinkingBudget":-2}}`,
		`{"thinkingConfig":{"thinkingBudget":1.5}}`,
		`{"thinkingConfig":{"thinkingLevel":"unknown"}}`,
		`{"thinkingConfig":{"thinkingBudget":0,"thinkingLevel":"high"}}`,
		`{"responseMimeType":"image/png"}`,
		`{"responseMimeType":42}`,
		`{"responseMimeType":"text/plain","responseSchema":{"type":"OBJECT"}}`,
		`{"responseSchema":{},"responseJsonSchema":{}}`,
	} {
		_, err := GeminiToMessages([]byte(`{"contents":[{"parts":[{"text":"hello"}]}],"generationConfig":`+config+`}`), "m", false)
		require.Error(t, err, config)
	}
}

func TestGeminiIngressSchemaNullability(t *testing.T) {
	converted, err := GeminiToMessages([]byte(`{"contents":[{"parts":[{"text":"JSON"}]}],"generationConfig":{"responseMimeType":"application/json","responseSchema":{"type":"OBJECT","properties":{"answer":{"type":"STRING","nullable":true,"enum":["OBJECT"]}}}}}`), "m", false)
	require.NoError(t, err)
	require.Equal(t, "null", gjson.GetBytes(converted, "output_config.format.schema.properties.answer.anyOf.1.type").String())
	require.Equal(t, "OBJECT", gjson.GetBytes(converted, "output_config.format.schema.properties.answer.anyOf.0.enum.0").String())
}

func TestGeminiIngressJSONWithoutSchema(t *testing.T) {
	converted, err := GeminiToMessages([]byte(`{"contents":[{"parts":[{"text":"JSON"}]}],"generationConfig":{"responseMimeType":"application/json"}}`), "m", false)
	require.NoError(t, err)
	require.Equal(t, "json_schema", gjson.GetBytes(converted, "output_config.format.type").String())
	require.JSONEq(t, `{}`, gjson.GetBytes(converted, "output_config.format.schema").Raw)
}
