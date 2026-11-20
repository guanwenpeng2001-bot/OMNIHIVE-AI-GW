package service

import "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/cursor"

// Cursor capability snapshot: see cursor_model_capabilities.md for sources,
// semantics and maintenance. Values are published maximum context capacities,
// not an assertion that every plan/request enables extended context.
var cursorPublishedContextWindows = map[string]int64{
	"claude-sonnet-4-5": 1000000,
	"claude-sonnet-4-6": 1000000,
	"claude-sonnet-5":   1000000,
	"claude-opus-4-6":   1000000,
	"claude-opus-4-7":   1000000,
	"claude-opus-4-8":   1000000,
	"claude-opus-5":     1000000,
	"gpt-5.4":           1000000,
	"gpt-5.5":           1000000,
	"gpt-5.6-sol":       1000000,
	"gpt-5.6-terra":     1000000,
	"gpt-5.6-luna":      1000000,
	"kimi-k3":           1000000,
	"muse-spark-1.3":    1000000,
}

func builtinCursorModelMetadata(ids []string) map[string]UpstreamModelMetadata {
	known := map[string]cursor.AvailableModel{}
	for _, model := range cursor.ProtocolVariantModels() {
		known[model.ID] = model
	}
	result := map[string]UpstreamModelMetadata{}
	for _, id := range ids {
		model, ok := known[id]
		// Auto/default is a changing router, not a fixed model capability contract.
		if !ok || id == "default" {
			continue
		}
		entry := UpstreamModelMetadata{ID: id, InputModalities: []string{"text"}, ContextWindow: cursorPublishedContextWindows[id]}
		entry.SupportedReasoningLevels = cursor.ModelReasoningLevels(model)
		if len(entry.SupportedReasoningLevels) > 0 {
			reasoning := true
			entry.Reasoning = &reasoning
		}
		result[id] = entry
	}
	return result
}
