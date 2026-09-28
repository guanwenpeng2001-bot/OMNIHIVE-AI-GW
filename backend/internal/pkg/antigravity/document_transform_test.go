package antigravity

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildPartsDocumentInlineData(t *testing.T) {
	for _, scenario := range []struct {
		name, source string
		wantParts    int
	}{
		{"pdf", `{"type":"base64","media_type":"application/pdf","data":"JVBERi0="}`, 2},
		{"empty", `{"type":"base64","media_type":"application/pdf","data":" "}`, 1},
		{"url", `{"type":"url","url":"https://example.com/test.pdf"}`, 1},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			content := json.RawMessage(`[{"type":"text","text":"read this"},{"type":"document","source":` + scenario.source + `}]`)
			parts, stripped, err := buildParts(content, map[string]string{}, true)
			require.NoError(t, err)
			require.False(t, stripped)
			require.Len(t, parts, scenario.wantParts)
			if scenario.wantParts == 2 {
				require.Equal(t, &GeminiInlineData{MimeType: "application/pdf", Data: "JVBERi0="}, parts[1].InlineData)
			}
		})
	}
}
