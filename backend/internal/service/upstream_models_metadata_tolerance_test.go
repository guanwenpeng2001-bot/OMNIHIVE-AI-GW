package service

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUpstreamMetadataReasoningFormats(t *testing.T) {
	for _, tc := range []struct {
		name, reasoning, extra string
		known, supported       bool
	}{
		{"true", `true`, "", true, true},
		{"false", `false`, "", true, false},
		{"null", `null`, "", false, false},
		{"empty object", `{}`, "", false, false},
		{"unknown type", `{"type":"future"}`, "", false, false},
		{"only", `{"type":"only"}`, "", true, true},
		{"supported false", `{"supported":false}`, "", true, false},
		{"bad scalar", `"true"`, "", false, false},
		{"explicit fallback", `{"type":"future"}`, `,"supports_reasoning":true`, true, true},
		{"valid type bad effort", `{"type":"only","effort":{"valid":42,"default":"high"}}`, "", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := fmt.Sprintf(`{"data":[{"id":"new-ID-not-in-registry","display_name":"New Model","context_length":4096,"reasoning":%s%s}]}`, tc.reasoning, tc.extra)
			ids, metadata, err := extractUpstreamModelCatalog([]byte(body), false)
			require.NoError(t, err)
			require.Equal(t, []string{"new-ID-not-in-registry"}, ids)
			m := metadata[ids[0]]
			require.Equal(t, "New Model", m.DisplayName)
			require.EqualValues(t, 4096, m.ContextWindow)
			if tc.known {
				require.NotNil(t, m.Reasoning)
				require.Equal(t, tc.supported, *m.Reasoning)
			} else {
				require.Nil(t, m.Reasoning)
			}
		})
	}
}

func TestUpstreamMetadataKnownNativeList(t *testing.T) {
	body, err := os.ReadFile("testdata/kimi_model_list.json")
	require.NoError(t, err)
	s := &AccountTestService{cfg: upstreamModelSyncTestConfig(), httpUpstream: &httpUpstreamRecorder{resp: catalogFixtureResponse(string(body))}}
	a := &Account{Platform: PlatformKimi, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture", "base_url": "https://relay.example/coding"}}
	ids, merged, err := s.fetchUpstreamModelList(context.Background(), a)
	require.NoError(t, err)
	require.Equal(t, []string{"k3", "k3-256k", "kimi-for-coding", "kimi-for-coding-highspeed"}, ids)
	_, metadata, err := extractUpstreamModelCatalog(merged, false, PlatformKimi)
	require.NoError(t, err)
	for id, name := range map[string]string{"k3": "K3", "k3-256k": "K3-256k", "kimi-for-coding": "K2.8 Preview", "kimi-for-coding-highspeed": "K2.7 Code Highspeed"} {
		m := metadata[id]
		require.Equal(t, name, m.DisplayName)
		require.NotNil(t, m.Reasoning)
		require.True(t, *m.Reasoning)
		require.Empty(t, m.Conflicts)
	}
}

func TestUpstreamMetadataNestedCapabilitiesKeepValidSiblings(t *testing.T) {
	_, metadata, err := extractUpstreamModelCatalog([]byte(`{"data":[{"id":"future","capabilities":{"thinking":{"supported":true,"types":"bad"},"image_input":{"supported":true,"types":42},"pdf_input":"bad"}}]}`), false)
	require.NoError(t, err)
	require.NotNil(t, metadata["future"].Reasoning)
	require.True(t, *metadata["future"].Reasoning)
	require.NotNil(t, metadata["future"].SupportsVision)
	require.True(t, *metadata["future"].SupportsVision)
	require.Nil(t, metadata["future"].SupportsPDF)
}

func TestUpstreamMetadataOptionalFieldsAreIndependent(t *testing.T) {
	for field, value := range map[string]string{
		"description": `42`, "created": `{}`, "context_window": `"8192"`,
		"supported_methods": `["chat_completions",42]`, "supports_function_calling": `"true"`,
		"modalities": `{"input":false,"output":["text"]}`, "limit": `{"context":"bad","output":2048}`,
		"name": `42`, "reasoning": `[true]`,
	} {
		t.Run(field, func(t *testing.T) {
			body := fmt.Sprintf(`{"data":[{"id":"new-model","display_name":"Valid Name","inputTokenLimit":8192,"supports_prompt_caching":false,%q:%s}]}`, field, value)
			ids, err := extractCatalogModelIDsWithSelector([]byte(body), upstreamModelEntryID)
			require.NoError(t, err)
			require.Equal(t, []string{"new-model"}, ids)
			_, metadata, err := extractUpstreamModelCatalog([]byte(body), false)
			require.NoError(t, err)
			m := metadata["new-model"]
			require.Equal(t, "Valid Name", m.DisplayName)
			require.EqualValues(t, 8192, m.ContextWindow)
			require.NotNil(t, m.SupportsPromptCaching)
			require.False(t, *m.SupportsPromptCaching)
			if field == "supported_methods" {
				require.Nil(t, m.SupportedMethods, "invalid lists must not retain partial decoder writes")
			}
			if field == "limit" {
				require.EqualValues(t, 2048, m.MaxOutputTokens)
			}
			if field == "modalities" {
				require.Equal(t, []string{"text"}, m.OutputModalities)
			}
		})
	}
}

// Minimal model-list fields from the real Kimi coding response; no credentials.
const nativeReasoningModelFixture = `{"data":[{"id":"kimi-for-coding","display_name":"K2.8 Preview","context_length":1048576,"supports_reasoning":true,"modalities":{"input":["text","image","video"],"output":["text"]},"reasoning":{"type":"only","effort":{"valid":["low","high","max"],"default":"max"}}}],"has_more":false}`

func TestUpstreamMetadataNativeReasoningDiscovery(t *testing.T) {
	s := &AccountTestService{cfg: upstreamModelSyncTestConfig(), httpUpstream: &httpUpstreamRecorder{resp: catalogFixtureResponse(nativeReasoningModelFixture)}}
	a := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture", "base_url": "https://relay.example/v1"}}
	ids, body, err := s.fetchUpstreamModelList(context.Background(), a)
	require.NoError(t, err)
	require.Equal(t, []string{"kimi-for-coding"}, ids)
	_, metadata, err := extractUpstreamModelCatalog(body, false)
	require.NoError(t, err)
	m := metadata["kimi-for-coding"]
	require.Equal(t, "K2.8 Preview", m.DisplayName)
	require.EqualValues(t, 1048576, m.ContextWindow)
	require.Equal(t, []string{"text", "image", "video"}, m.InputModalities)
	require.NotNil(t, m.Reasoning)
	require.True(t, *m.Reasoning)
	require.Equal(t, []string{"low", "high", "max"}, m.SupportedReasoningLevels)
	require.Equal(t, "max", m.DefaultReasoningLevel)
}

func TestUpstreamMetadataPreviewDoesNotChangeSelectedModels(t *testing.T) {
	t.Setenv("MODEL_CAPABILITY_OFFLINE", "true")
	selected := map[string]any{"public-chosen": "chosen-but-not-listed"}
	a := &Account{ID: 91, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{
		"api_key": "fixture", "base_url": "https://relay.example/v1", "model_mapping": selected,
	}}
	s := &AccountTestService{cfg: upstreamModelSyncTestConfig(), httpUpstream: &httpUpstreamRecorder{resp: catalogFixtureResponse(nativeReasoningModelFixture)}}
	preview, err := s.PreviewAccountModelCatalog(context.Background(), a)
	require.NoError(t, err)
	require.True(t, preview.DiscoveryComplete)
	require.Equal(t, []string{"kimi-for-coding"}, preview.DiscoveredModels)
	require.Equal(t, "K2.8 Preview", preview.Metadata["kimi-for-coding"].DisplayName)
	require.Equal(t, map[string]any{"public-chosen": "chosen-but-not-listed"}, a.Credentials["model_mapping"])
	require.Nil(t, a.GetUpstreamModelCatalog())
}

func TestUpstreamMetadataMalformedFieldsDoNotLoseDiscovery(t *testing.T) {
	for _, platform := range []string{PlatformOpenAI, PlatformMistral, PlatformQianfan, PlatformQianwen} {
		t.Run(platform, func(t *testing.T) {
			body := `{"object":"list","data":[{"id":"future-unlisted-ID","name":"Future Model","display_name":"Future Model","context_window":"broken","max_context_length":"broken","created":{},"modalities":{"input":42,"output":["text"]},"capabilities":{"vision":true,"function_calling":"broken","completion_chat":true}}],"has_more":false}`
			s := &AccountTestService{cfg: upstreamModelSyncTestConfig(), httpUpstream: &httpUpstreamRecorder{resp: catalogFixtureResponse(body)}}
			a := &Account{Platform: platform, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture", "base_url": "https://relay.example"}}
			ids, raw, err := s.fetchUpstreamModelList(context.Background(), a)
			require.NoError(t, err)
			require.Equal(t, []string{"future-unlisted-ID"}, ids)
			_, metadata, err := extractUpstreamModelCatalog(raw, false, platform)
			require.NoError(t, err)
			require.Equal(t, "Future Model", metadata[ids[0]].DisplayName)
			require.Zero(t, metadata[ids[0]].ContextWindow)
			if platform == PlatformMistral {
				require.NotNil(t, metadata[ids[0]].SupportsVision)
				require.True(t, *metadata[ids[0]].SupportsVision)
				require.Nil(t, metadata[ids[0]].SupportsFunctionCalling)
			} else {
				require.Equal(t, []string{"text"}, metadata[ids[0]].OutputModalities)
			}
		})
	}
}

func TestUpstreamMetadataDoesNotMaskInvalidDiscovery(t *testing.T) {
	for _, tail := range []string{`{"data":[{"id":42}]}`, `{"data":[{"name":null}]}`, `{"data":[],"has_more":true}`, `{"data":[],"next":"https://other.example/models"}`} {
		s := &AccountTestService{cfg: upstreamModelSyncTestConfig(), httpUpstream: &httpUpstreamRecorder{responses: []*http.Response{
			catalogFixtureResponse(`{"data":[{"id":"known","reasoning":{}}],"has_more":true,"last_id":"known"}`), catalogFixtureResponse(tail),
		}}}
		a := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture", "base_url": "https://relay.example/v1"}}
		_, _, err := s.fetchUpstreamModelList(context.Background(), a)
		var partial *UpstreamModelSyncError
		require.ErrorAs(t, err, &partial)
		require.NotNil(t, partial.PartialCatalog)
		require.Contains(t, partial.PartialCatalog.DiscoveredModels, "known")
	}
}
