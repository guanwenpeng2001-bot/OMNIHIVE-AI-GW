package service

import (
	"context"
	"strings"
)

type AccountProbeInput struct {
	AccountID int64
	ModelID   string
	Prompt    string
	Mode      string
	Options   AccountTestOptions
}

type AccountProbeResult struct {
	Success      bool
	ResponseText string
	Error        string
	Code         string
}

type accountProbeContext struct {
	ctx                context.Context
	emit               func(TestEvent)
	suppressCompletion bool
}

// AccountModelProbeSupported describes native test support, not credential health.
func AccountModelProbeSupported(account *Account, model string) bool {
	model = account.GetMappedModel(model)
	if account.Platform == PlatformQianwen {
		return !IsQianwenNativeImageModel(model) && ValidateQianwenNativeOperation(QianwenNativeMultimodal, model, nil, true) == nil
	}
	if !isCodexDedicatedMediaModel(model) && !strings.Contains(strings.ToLower(model), "video") {
		return true
	}
	switch account.Platform {
	case PlatformOpenAI:
		return IsGPTImageGenerationModel(model)
	case PlatformGemini, PlatformAntigravity:
		return isImageGenerationModel(model)
	case PlatformGrok:
		return true
	default:
		return false
	}
}

// ProbeAccount is the transport-independent account health port. Success needs
// an explicit terminal event and a clean return, regardless of output length.
func (s *AccountTestService) ProbeAccount(ctx context.Context, input AccountProbeInput, emit func(TestEvent)) AccountProbeResult {
	result := AccountProbeResult{}
	errorSent := false
	c := &accountProbeContext{ctx: ctx, emit: func(event TestEvent) {
		switch event.Type {
		case "content":
			result.ResponseText += event.Text
		case "error":
			result.Error, result.Code = event.Error, event.Code
			errorSent = true
		case "test_complete":
			result.Success = event.Success
		}
		if emit != nil && event.Type != "test_complete" {
			emit(event)
		}
	}}
	err := s.probeTestAccountConnection(c, input.AccountID, input.ModelID, input.Prompt, input.Mode, input.Options)
	if err != nil {
		result.Error = err.Error()
	}
	if ctx.Err() != nil {
		result.Error = ctx.Err().Error()
	}
	result.Success = result.Success && result.Error == ""
	if !result.Success && result.Error == "" {
		result.Error = "Probe ended without successful completion"
	}
	if emit != nil {
		if result.Success {
			emit(TestEvent{Type: "test_complete", Success: true})
		} else if !errorSent {
			emit(TestEvent{Type: "error", Error: result.Error, Code: result.Code})
		}
	}
	return result
}
