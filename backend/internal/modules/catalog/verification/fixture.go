package verification

import (
	"encoding/json"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/apicompat"
)

const (
	OpenAIChatAdapterVersion = "ad_openai_chat_responses_v1"
	OpenAIChatFixtureID      = "openai_chat_responses_v1"
)

const openAIChatExample = `{
	"model":"fixture-model",
	"messages":[{"role":"user","content":"Reply with OK."}],
	"stream":false
}`

// The release fixture exercises the Chat request and terminal response codecs
// for the Responses bridge. The endpoint probe only observes catalog discovery.
func checkOpenAIChatFixture() bool {
	var request apicompat.ChatCompletionsRequest
	if json.Unmarshal([]byte(openAIChatExample), &request) != nil {
		return false
	}
	forwarded, err := apicompat.ChatCompletionsToResponses(&request)
	if err != nil || forwarded.Model != "fixture-model" || !forwarded.Stream {
		return false
	}
	var input []apicompat.ResponsesInputItem
	if json.Unmarshal(forwarded.Input, &input) != nil || len(input) != 1 || input[0].Role != "user" {
		return false
	}
	var content string
	if json.Unmarshal(input[0].Content, &content) != nil || content != "Reply with OK." {
		return false
	}
	upstream := &apicompat.ResponsesResponse{
		ID: "resp_fixture", Status: "completed",
		Output: []apicompat.ResponsesOutput{{
			Type: "message", Role: "assistant",
			Content: []apicompat.ResponsesContentPart{{Type: "output_text", Text: "OK"}},
		}},
		Usage: &apicompat.ResponsesUsage{InputTokens: 5, OutputTokens: 1, TotalTokens: 6},
	}
	returned := apicompat.ResponsesToChatCompletions(upstream, request.Model)
	return returned.Object == "chat.completion" && returned.Model == request.Model && len(returned.Choices) == 1 &&
		returned.Choices[0].FinishReason == "stop" && string(returned.Choices[0].Message.Content) == `"OK"` &&
		returned.Usage != nil && returned.Usage.TotalTokens == 6
}
