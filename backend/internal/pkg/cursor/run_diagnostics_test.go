package cursor

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunDiagnosticsBoundsModelAndKeepsOnlyShape(t *testing.T) {
	for _, model := range []string{"composer-2.5-fast", "cursor-grok-4.7-xhigh", "sk-fakeCredential123456", "https://private.example/model?token=fake", strings.Repeat("x", 129)} {
		var observed RunDiagnostics
		ctx := WithRunDiagnosticsHook(context.Background(), func(diagnostic RunDiagnostics) { observed = diagnostic })
		recordRunDiagnostics(ctx, []ChatMessage{{Role: "user", Content: "PRIVATE"}, {Role: "developer", Content: "INSTRUCTION"}}, model, 2, 321)
		if model == "composer-2.5-fast" || model == "cursor-grok-4.7-xhigh" {
			require.Equal(t, model, observed.Model)
		} else {
			require.Empty(t, observed.Model)
		}
		require.Equal(t, 2, observed.ToolCount)
		require.Equal(t, 2, observed.MessageCount)
		require.Equal(t, 321, observed.EncodedBytes)
		require.True(t, observed.InstructionPresent)
	}
	var observed RunDiagnostics
	ctx := WithRunDiagnosticsHook(context.Background(), func(diagnostic RunDiagnostics) { observed = diagnostic })
	recordRunDiagnostics(ctx, []ChatMessage{{Role: "user", Content: "PRIVATE"}}, "default", 0, 100)
	require.False(t, observed.InstructionPresent)
	require.Zero(t, observed.ToolCount)
}
