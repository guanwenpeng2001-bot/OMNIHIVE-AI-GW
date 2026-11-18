package service

import (
	"fmt"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/apicompat"
)

// Messages is only an intermediate representation here. The selected upstream
// is Gemini, so Claude's model effort catalog must not decide its parameters.
func responsesToGeminiMessages(request *apicompat.ResponsesRequest) (*apicompat.AnthropicRequest, error) {
	contentRequest := *request
	contentRequest.Reasoning = nil
	messages, err := apicompat.ResponsesToAnthropicRequest(&contentRequest)
	if err != nil {
		return nil, err
	}
	if request.Reasoning == nil || request.Reasoning.Effort == "" {
		return messages, nil
	}
	effort := request.Reasoning.Effort
	switch effort {
	case "none":
		messages.Thinking = &apicompat.AnthropicThinking{Type: "disabled"}
	case "minimal", "low", "medium", "high":
		messages.Thinking = &apicompat.AnthropicThinking{Type: "adaptive"}
		messages.OutputConfig = &apicompat.AnthropicOutputConfig{Effort: effort}
	default:
		return nil, fmt.Errorf("reasoning.effort %q cannot be represented by Gemini; expected none, minimal, low, medium, or high", effort)
	}
	return messages, nil
}
