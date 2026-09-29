package gemini

import "testing"

func TestFallbackDoesNotPublishCandidates(t *testing.T) {
	if len(DefaultModels()) != 0 || len(FallbackModelsList().Models) != 0 {
		t.Fatal("public defaults must be empty")
	}
	for _, model := range []string{"gemini-3.1-pro-preview-customtools", "models/gemini-3.1-pro-preview-customtools", "unknown"} {
		if HasFallbackModel(model) {
			t.Fatal("candidate must not grant public membership")
		}
	}
}
