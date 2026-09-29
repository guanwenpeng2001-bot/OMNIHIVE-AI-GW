package antigravity

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStreamingProcessorContentExcludesSignatures(t *testing.T) {
	for _, scenario := range []struct {
		name, parts string
		content     bool
	}{
		{"signature", `[{"thought":true,"thoughtSignature":"opaque"}]`, false},
		{"text_signature", `[{"text":"hello","thoughtSignature":"opaque"}]`, true},
		{"text", `[{"text":"hello"}]`, true},
		{"thinking", `[{"thought":true,"text":"thinking"}]`, true},
		{"tool", `[{"functionCall":{"name":"lookup","args":{"query":"hello"}}}]`, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			processor := NewStreamingProcessor("claude-sonnet-4-6")
			processor.ProcessLine(`data: {"response":{"candidates":[{"content":{"role":"model","parts":` + scenario.parts + `}}]}}`)
			require.Equal(t, scenario.content, processor.HasContent())
			processor.Finish()
			require.Equal(t, scenario.content, processor.HasContent())
		})
	}
}
