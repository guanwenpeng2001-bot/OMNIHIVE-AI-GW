package geminicli_test

import (
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/geminicli"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"testing"
)

func TestDefaultModels_ContainsImageModels(t *testing.T) {
	t.Parallel()

	byID := make(map[string]geminicli.Model, len(service.GeminiPresetModels()))
	for _, model := range service.GeminiPresetModels() {
		byID[model.ID] = model
	}

	required := []string{
		"gemini-2.5-flash-image",
		"gemini-3.1-flash-image",
	}

	for _, id := range required {
		if _, ok := byID[id]; !ok {
			t.Fatalf("expected curated Gemini model %q to exist", id)
		}
	}
}

func TestGoogleOneModels_ExcludeUnsupportedNewAndImageModels(t *testing.T) {
	t.Parallel()

	mapping := service.GoogleOneModelMapping()
	for _, id := range []string{"gemini-2.0-flash", "gemini-2.5-flash", "gemini-2.5-pro"} {
		if mapping[id] != id {
			t.Fatalf("expected Google One model %q to map to itself", id)
		}
	}
	for _, id := range []string{"gemini-2.5-flash-image", "gemini-3.1-flash-image", "gemini-3.5-flash"} {
		if _, ok := mapping[id]; ok {
			t.Fatalf("did not expect unsupported Google One model %q", id)
		}
	}

	mapping["gemini-2.5-flash"] = "mutated"
	if service.GoogleOneModelMapping()["gemini-2.5-flash"] != "gemini-2.5-flash" {
		t.Fatal("GoogleOneModelMapping must return a defensive copy")
	}
}
