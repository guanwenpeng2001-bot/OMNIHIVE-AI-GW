package service

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMediaHintsRemainDisplayOnly(t *testing.T) {
	for model, task := range map[string]string{"private-image": "image_generation", "private-video": "video_generation", "private-tts": "audio_generation", "private-whisper": "transcription", "private-rerank": "rerank"} {
		hint := inferModelCapabilityHints(model)
		require.Equal(t, task, hint.TaskType)
		require.Equal(t, "estimated", hint.FieldEvidence["task_type"].Confidence)
		require.Nil(t, hint.SupportedMethods)
		require.Zero(t, hint.ContextWindow)
		account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true}
		require.False(t, AccountSupportsResolvedModel(context.Background(), account, model, AccountModelBindingOptions{}))
		native, _ := mergeUpstreamModelMetadata(UpstreamModelMetadata{TaskType: "text_generation"}, hint)
		require.Equal(t, "text_generation", native.TaskType)
	}
}

func TestModelCapabilityEmbeddingNameHints(t *testing.T) {
	for _, id := range []string{"qwen3.7-text-embedding", "qwen3.7-text-embedding-flash", "text-embedding-v4", "QWEN3-TEXT-EMBEDDING"} {
		t.Run(id, func(t *testing.T) {
			m := inferModelCapabilityHints(id)
			require.Equal(t, "embedding", m.TaskType)
			require.Equal(t, []string{"text"}, m.InputModalities)
			require.Equal(t, []string{"text"}, m.OutputModalities)
			for _, field := range []string{"task_type", "input_modalities", "output_modalities"} {
				require.Equal(t, "heuristic", m.FieldEvidence[field].Source)
				require.Equal(t, "estimated", m.FieldEvidence[field].Confidence)
			}
			require.Zero(t, m.ContextWindow)
			require.Zero(t, m.EmbeddingDimensions)
			require.Nil(t, m.SupportedMethods)
			require.NotEqual(t, "complete", evaluateModelMetadataCompleteness(m).Status)
			// Even with all other required facts, guessed fields stay estimated.
			complete := UpstreamModelMetadata{ID: id, ProviderProfile: "alibaba-cn", TaskType: "embedding",
				InputModalities: []string{"text"}, OutputModalities: []string{"text"},
				SupportedMethods: []string{"embeddings"}, ContextWindow: 128000, EmbeddingDimensions: 1024}
			stampCapabilityEvidence(&complete, "fixture", "v1", "alibaba-cn", id, "exact", "verified")
			for _, field := range []string{"task_type", "input_modalities", "output_modalities"} {
				e := m.FieldEvidence[field]
				e.ProviderProfile = "alibaba-cn"
				complete.FieldEvidence[field] = e
			}
			status := evaluateModelMetadataCompleteness(complete)
			require.Equal(t, "estimated", status.Status)
			require.ElementsMatch(t, []string{"task_type", "input_modalities", "output_modalities"}, status.EstimatedFields)
			// A hint never replaces an explicit upstream modality/task declaration.
			native := UpstreamModelMetadata{TaskType: "text_generation", InputModalities: []string{"text", "image"}, OutputModalities: []string{"text"}}
			merged, _ := mergeUpstreamModelMetadata(native, m)
			require.Equal(t, native.TaskType, merged.TaskType)
			require.Equal(t, native.InputModalities, merged.InputModalities)
		})
	}
	for _, id := range []string{"qwen-plus", "qwen3-asr-flash", "private-embeddings", "private-embeddingish", "embedding", strings.Repeat("x", 512) + "-embedding"} {
		t.Run(id, func(t *testing.T) {
			m := inferModelCapabilityHints(id)
			require.Empty(t, m.TaskType)
			require.Nil(t, m.InputModalities)
			require.Nil(t, m.OutputModalities)
		})
	}
}
