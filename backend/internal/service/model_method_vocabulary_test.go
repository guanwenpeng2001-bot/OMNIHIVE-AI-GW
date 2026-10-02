package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReviewedMethodVocabularyRegistryProjection(t *testing.T) {
	registry := NewModelRegistry().Snapshot()
	for _, tc := range []struct {
		platform, id string
		methods      []string
		endpoint     string
	}{
		{PlatformMistral, "voxtral-mini-tts-2603", []string{"speech"}, "speech"},
		{PlatformMistral, "voxtral-mini-2602", []string{"transcriptions"}, "transcriptions"},
		{PlatformMistral, "voxtral-mini-transcribe-realtime-2602", []string{"transcriptions"}, "transcriptions"},
		{PlatformOpenAI, "gpt-4o-mini-tts", []string{"speech"}, ""},
		{PlatformOpenAI, "gpt-4o-mini-transcribe", []string{"transcriptions", "realtime"}, "live"},
		{PlatformOpenAI, "gpt-4o-transcribe", []string{"transcriptions", "realtime"}, "live"},
		{PlatformOpenAI, "gpt-4o-transcribe-diarize", []string{"transcriptions"}, ""},
		{PlatformOpenAI, "gpt-transcribe", []string{"transcriptions"}, ""},
		{PlatformOpenAI, "chatgpt-image-latest", []string{"images/edits", "images/generations"}, "images"},
		{PlatformOpenAI, "gpt-image-1", []string{"images/edits", "images/generations", "responses"}, "images"},
		{PlatformOpenAI, "gpt-image-1-mini", []string{"images/edits", "images/generations"}, "images"},
		{PlatformOpenAI, "gpt-image-1.5", []string{"images/edits", "images/generations"}, "images"},
		{PlatformOpenAI, "gpt-image-2", []string{"images/edits", "images/generations"}, "images"},
		{PlatformOpenAI, "gpt-image-2.5-flare", []string{"images/edits", "images/generations"}, "images"},
		{PlatformOpenAI, "gpt-image-2.5-sunburst", []string{"images/edits", "images/generations"}, "images"},
	} {
		t.Run(tc.platform+"/"+tc.id, func(t *testing.T) {
			account := &Account{Platform: tc.platform, Type: AccountTypeAPIKey}
			lookup := lookupCapabilityMetadata(registry.CapabilitySnapshot(), tc.platform, tc.id)
			require.Equal(t, "matched", lookup.Status)
			require.ElementsMatch(t, tc.methods, lookup.Metadata.SupportedMethods)
			catalog := projectAccountModelCatalog(account, &UpstreamModelCatalog{Models: []string{tc.id}, Metadata: map[string]UpstreamModelMetadata{tc.id: lookup.Metadata}})
			if tc.platform == PlatformOpenAI {
				// Canonical metadata never installs an unbound audio adapter.
				require.False(t, platformActionAdapterBound(account, "audio.tts"))
				require.False(t, platformActionAdapterBound(account, "audio.stt"))
			} else {
				require.True(t, platformActionAdapterBound(account, chatOnlyCatalogEndpointAction(tc.endpoint)))
			}
			if tc.endpoint == "" {
				require.Empty(t, catalog.EligibleModels)
				require.Equal(t, []ExcludedCatalogModel{{ID: tc.id, Reason: CatalogReasonEndpointUnsupported}}, catalog.ExcludedModels)
				return
			}
			require.Empty(t, catalog.ExcludedModels)
			require.Len(t, catalog.EligibleModels, 1)
			require.Contains(t, catalog.EligibleModels[0].Endpoints, tc.endpoint)
			require.NotContains(t, catalog.EligibleModels[0].Endpoints, "chat_completions")
			if tc.endpoint == "live" {
				// Preserve independently declared realtime; do not promote STT.
				require.Equal(t, []string{"live"}, catalog.EligibleModels[0].Endpoints)
			}
		})
	}
}

func TestReviewedMethodVocabularyUnknownMethodsDoNotAuthorizeChat(t *testing.T) {
	for _, method := range []string{"unknown", "fim", "ocr", "future_method", "audio_speech", "audio_transcriptions", "images_generations", "images_edits"} {
		for _, platform := range []string{PlatformMistral, PlatformOpenAI} {
			t.Run(platform+"/"+method, func(t *testing.T) {
				account := &Account{Platform: platform, Type: AccountTypeAPIKey}
				require.Empty(t, accountCatalogModelEndpoints(account, "unknown-model", UpstreamModelMetadata{SupportedMethods: []string{method}}))
			})
		}
	}
	// Existing live/realtime semantics remain their own family, never chat.
	for _, method := range []string{"realtime", "live"} {
		account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
		require.Equal(t, []string{"live"}, accountCatalogModelEndpoints(account, "unknown-model", UpstreamModelMetadata{SupportedMethods: []string{method}}))
	}
}
