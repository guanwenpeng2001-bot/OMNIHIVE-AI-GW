package cursor

import (
	"context"
	"strings"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/util/logredact"
)

// RunDiagnostics describes the encoded request without retaining its contents.
type RunDiagnostics struct {
	Model              string `json:"model"`
	ToolCount          int    `json:"tool_count"`
	InstructionPresent bool   `json:"instruction_present"`
	MessageCount       int    `json:"message_count"`
	EncodedBytes       int    `json:"encoded_bytes"`
}

type runDiagnosticsKey struct{}

// WithRunDiagnosticsHook observes encoding synchronously, before transport or
// admission. It neither sends a request nor authorizes a replay.
func WithRunDiagnosticsHook(ctx context.Context, hook func(RunDiagnostics)) context.Context {
	return context.WithValue(ctx, runDiagnosticsKey{}, hook)
}

func recordRunDiagnostics(ctx context.Context, messages []ChatMessage, model string, toolCount, encodedBytes int) {
	hook, ok := ctx.Value(runDiagnosticsKey{}).(func(RunDiagnostics))
	if !ok {
		return
	}
	// Model IDs are upstream data too. Retain only bounded slug syntax and
	// reject credential-like values using the shared log redactor.
	safeModel := model
	if len(model) > 128 || logredact.RedactText(model) != model || strings.IndexFunc(model, func(character rune) bool {
		return !strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_.", character)
	}) >= 0 {
		safeModel = ""
	}
	diagnostic := RunDiagnostics{Model: safeModel, ToolCount: toolCount, MessageCount: len(messages), EncodedBytes: encodedBytes}
	for _, item := range messages {
		if isInstructionRole(item.Role) {
			diagnostic.InstructionPresent = true
			break
		}
	}
	hook(diagnostic)
}
