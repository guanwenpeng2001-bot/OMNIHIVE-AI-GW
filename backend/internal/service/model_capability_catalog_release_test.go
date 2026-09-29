package service

import (
	"bytes"
	"encoding/json"
	"reflect"
	"regexp"
	"testing"

	modelcapabilities "github.com/Wei-Shaw/sub2api/resources/model-capabilities"
	modelpricing "github.com/Wei-Shaw/sub2api/resources/model-pricing"
	"github.com/stretchr/testify/require"
)

// Exercise the published embed, rather than a miniature synthetic catalog.
func TestModelCapabilityReleaseEveryPublishedRecordLoadsOffline(t *testing.T) {
	var catalog struct {
		Records []CapabilityRecord `json:"records"`
	}
	require.NoError(t, json.Unmarshal(modelcapabilities.JSON(), &catalog))
	require.Len(t, catalog.Records, 296)
	snapshot, err := buildModelCapabilitySnapshot(nil)
	require.NoError(t, err)
	hashPattern := regexp.MustCompile(`^[0-9a-f]{64}$`)
	for _, published := range catalog.Records {
		t.Run(published.Profile+"/"+published.ModelID, func(t *testing.T) {
			require.NoError(t, validateCapabilityRecord(published))
			require.NotEmpty(t, published.ApplicableContentHashes)
			for _, sourceHash := range published.ApplicableContentHashes {
				require.Regexp(t, hashPattern, sourceHash)
			}
			require.Regexp(t, hashPattern, published.SourceContentHash)
			loaded, ok := snapshot.LookupExact(published.Profile, published.ModelID)
			require.True(t, ok)
			require.Equal(t, published.SupportedMethods, loaded.SupportedMethods)
			require.Equal(t, published.EmbeddingDimensions, loaded.EmbeddingDimensions)
			want := reflect.ValueOf(published.LiteLLMCapabilityFields)
			got := reflect.ValueOf(loaded.LiteLLMCapabilityFields)
			for i := 0; i < want.NumField(); i++ {
				if !want.Field(i).IsNil() {
					require.Equal(t, want.Field(i).Interface(), got.Field(i).Interface(), want.Type().Field(i).Name)
				}
			}
		})
	}
}

func TestModelCapabilityReleaseQwenEmbeddingsLoadOffline(t *testing.T) {
	lf := bytes.ReplaceAll(modelpricing.JSON(), []byte("\r\n"), []byte("\n"))
	for _, body := range [][]byte{nil, lf, bytes.ReplaceAll(lf, []byte("\n"), []byte("\r\n"))} {
		snapshot, err := buildModelCapabilitySnapshot(body)
		require.NoError(t, err)
		for _, id := range []string{"qwen3.7-text-embedding", "qwen3.7-text-embedding-flash"} {
			match := lookupCapabilityMetadata(snapshot, "alibaba-cn", id)
			require.Equal(t, "matched", match.Status)
			m := match.Metadata
			require.Equal(t, "embedding", m.TaskType)
			require.Equal(t, []string{"text"}, m.InputModalities)
			require.Equal(t, []string{"embedding"}, m.OutputModalities)
			require.Equal(t, []string{"embeddings"}, m.SupportedMethods)
			require.EqualValues(t, 128000, m.ContextWindow)
			require.EqualValues(t, 1024, m.EmbeddingDimensions)
			require.Zero(t, m.MaxOutputTokens)
			require.Equal(t, "complete", evaluateModelMetadataCompleteness(m).Status)
			for _, field := range []string{"task_type", "supported_methods", "embedding_dimensions", "context_window", "input_modalities", "output_modalities"} {
				require.Equal(t, "verified", m.FieldEvidence[field].Confidence)
				require.Equal(t, "https://help.aliyun.com/zh/model-studio/text-embedding-synchronous-api/", m.FieldEvidence[field].Source)
			}
			for _, profile := range []string{"alibaba", "cursor", "openai"} {
				require.Equal(t, "not_found", lookupCapabilityMetadata(snapshot, profile, id).Status)
			}
			// Neither record nor metadata callers may mutate the shared snapshot.
			record, ok := snapshot.LookupExact("alibaba-cn", id)
			require.True(t, ok)
			record.SupportedMethods[0] = "responses"
			*record.EmbeddingDimensions = 1
			m.SupportedMethods[0] = "chat_completions"
			again := lookupCapabilityMetadata(snapshot, "alibaba-cn", id).Metadata
			require.Equal(t, []string{"embeddings"}, again.SupportedMethods)
			require.EqualValues(t, 1024, again.EmbeddingDimensions)
		}
	}
}

func TestModelCapabilityEmbeddingRecordValidation(t *testing.T) {
	zero := int64(0)
	require.Error(t, validateCapabilityRecord(CapabilityRecord{ModelID: "embedding", EmbeddingDimensions: &zero}))
	require.Error(t, validateCapabilityRecord(CapabilityRecord{ModelID: "embedding", SupportedMethods: []string{" "}}))
	record := cloneCapabilityRecord(CapabilityRecord{ModelID: "embedding", SupportedMethods: []string{}})
	require.NotNil(t, record.SupportedMethods, "explicit empty method declarations must survive cloning")
}

func TestModelCapabilityReleaseConflictFallsBackToItsOwnBaseline(t *testing.T) {
	snapshot, err := buildModelCapabilitySnapshot(nil)
	require.NoError(t, err)
	// The pinned runtime says 400000; the embed says 128000. The supplement
	// deliberately omits this disputed field rather than arbitrating it.
	model, ok := snapshot.LookupExact("openai", "gpt-5-pro")
	require.True(t, ok)
	require.EqualValues(t, 128000, *model.MaxInputTokens)
	require.Equal(t, "litellm:embed", model.FieldSources["max_input_tokens"].Source)
	_, ok = snapshot.LookupExact("cursor", "gpt-5-pro")
	require.False(t, ok, "official observations must not manufacture hosted aliases")
	_, ok = snapshot.LookupExact("alibaba-cn", "qwen-plus")
	require.False(t, ok, "absent Qwen evidence must remain absent")
}

func TestModelCapabilityReleaseSupportsReviewedGitLineEndings(t *testing.T) {
	lf := bytes.ReplaceAll(modelpricing.JSON(), []byte("\r\n"), []byte("\n"))
	for _, body := range [][]byte{lf, bytes.ReplaceAll(lf, []byte("\n"), []byte("\r\n"))} {
		snapshot, err := buildModelCapabilitySnapshot(body)
		require.NoError(t, err)
		model, ok := snapshot.LookupExact("deepseek", "deepseek-reasoner")
		require.True(t, ok, "reviewed LF/CRLF checkout must retain offline supplement")
		require.False(t, *model.SupportsFunctionCalling)
		require.EqualValues(t, 65536, *model.MaxOutputTokens)
	}
}
