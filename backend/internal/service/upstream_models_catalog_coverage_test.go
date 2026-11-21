package service

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestKimiCodingModelDiscoveryUsesExactRegionalPlan(t *testing.T) {
	t.Setenv("MODEL_CAPABILITY_OFFLINE", "true")
	for _, host := range []string{"api.kimi.com", "api.kimi.ai"} {
		for _, protocol := range []string{APIProtocolChatCompletions, APIProtocolAnthropic, APIProtocolAdaptive} {
			for _, suffix := range []string{"/coding", "/coding/v1", "/coding/v1/"} {
				t.Run(host+protocol+suffix, func(t *testing.T) {
					account := nativeProfileAccount(PlatformKimi, AccountModeCoding, "https://"+host+suffix)
					account.Credentials["api_protocol"] = protocol
					upstream := &httpUpstreamRecorder{responses: []*http.Response{
						catalogFixtureResponse(`{"data":[{"id":"kimi-for-coding","context_length":262144,"supports_reasoning":true,"supports_vision":true}],"has_more":true,"last_id":"kimi-for-coding"}`),
						catalogFixtureResponse(`{"data":[{"id":"kimi-future-coding","supports_reasoning":false,"supports_vision":false}]}`),
					}}
					service := &AccountTestService{cfg: upstreamModelSyncTestConfig(), httpUpstream: upstream}
					catalog, err := service.SyncUpstreamModelCatalog(context.Background(), account)
					require.NoError(t, err)
					require.ElementsMatch(t, []string{"kimi-for-coding", "kimi-future-coding"}, catalog.DiscoveredModels)
					require.True(t, catalog.DiscoveryComplete)
					require.Len(t, upstream.requests, 2)
					for _, request := range upstream.requests {
						require.Equal(t, host, request.URL.Host)
						require.Equal(t, "/coding/v1/models", request.URL.Path)
						require.Equal(t, "Bearer fixture-key", request.Header.Get("Authorization"))
					}
					require.EqualValues(t, 262144, catalog.Metadata["kimi-for-coding"].ContextWindow)
					require.Equal(t, new(true), catalog.Metadata["kimi-for-coding"].Reasoning)
					require.Equal(t, new(true), catalog.Metadata["kimi-for-coding"].SupportsVision)
					require.Equal(t, new(false), catalog.Metadata["kimi-future-coding"].Reasoning)
				})
			}
		}
	}
}

func TestKimiCodingDiscoveryFailureDoesNotInventModels(t *testing.T) {
	for status, kind := range map[int]UpstreamModelSyncErrorKind{401: UpstreamModelSyncErrorCredentials, 404: UpstreamModelSyncErrorEndpointNotFound, 429: UpstreamModelSyncErrorRateLimited} {
		upstream := &httpUpstreamRecorder{responses: []*http.Response{catalogFixtureResponse(`{"error":"rejected"}`)}}
		upstream.responses[0].StatusCode = status
		service := &AccountTestService{cfg: upstreamModelSyncTestConfig(), httpUpstream: upstream}
		account := nativeProfileAccount(PlatformKimi, AccountModeCoding, "https://api.kimi.com/coding")
		catalog, err := service.SyncUpstreamModelCatalog(context.Background(), account)
		var failure *UpstreamModelSyncError
		require.ErrorAs(t, err, &failure)
		require.Equal(t, kind, failure.Kind)
		require.Nil(t, catalog)
	}
}

func TestUpstreamModelsDiscoveryPreservesEveryTaskAcrossPlatforms(t *testing.T) {
	entries := []map[string]any{}
	for _, task := range []string{"text_generation", "code_generation", "image_generation", "video_generation", "audio_generation", "speech", "transcription", "embedding", "realtime", "three_dimensional_generation", "unknown"} {
		entries = append(entries, map[string]any{"id": task, "task_type": task})
	}
	body, err := json.Marshal(map[string]any{"data": entries})
	require.NoError(t, err)
	for _, platform := range []string{PlatformOpenAI, PlatformAnthropic, PlatformGemini, PlatformAntigravity, PlatformGrok, PlatformKimi, PlatformDeepseek, PlatformCursor, PlatformQianwen, PlatformVolcengine, PlatformZhipu, PlatformHunyuan, PlatformOpenRouter} {
		t.Run(platform, func(t *testing.T) {
			ids, metadata, err := extractUpstreamModelCatalog(body, platform == PlatformGrok, platform)
			require.NoError(t, err)
			require.Len(t, ids, len(entries))
			for _, id := range ids {
				require.Equal(t, id, metadata[id].TaskType)
			}
			account := &Account{Platform: platform, Type: AccountTypeAPIKey}
			catalog := &UpstreamModelCatalog{Models: ids, Metadata: metadata, CatalogSource: "live"}
			projectAccountModelCatalog(account, catalog)
			require.ElementsMatch(t, ids, catalog.DiscoveredModels, "eligibility must never erase discovery")
			require.True(t, catalog.DiscoveryComplete)
		})
	}
}

func TestUpstreamModelsGeminiMultimodalPagination(t *testing.T) {
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		catalogFixtureResponse(`{"models":[{"name":"models/chat","supportedGenerationMethods":["generateContent"]}],"nextPageToken":"media"}`),
		catalogFixtureResponse(`{"models":[{"name":"models/video","supportedGenerationMethods":["predictLongRunning"]},{"name":"models/embed","supportedGenerationMethods":["embedContent"]},{"name":"models/audio","output_modalities":["audio"]}]}`),
	}}
	service := &AccountTestService{cfg: upstreamModelSyncTestConfig(), httpUpstream: upstream}
	account := &Account{Platform: PlatformGemini, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture"}}
	models, err := service.FetchUpstreamSupportedModels(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, []string{"audio", "chat", "embed", "video"}, models)
	require.Equal(t, "media", upstream.requests[1].URL.Query().Get("pageToken"))
	require.False(t, strings.Contains(strings.Join(models, ","), "models/"))
}
