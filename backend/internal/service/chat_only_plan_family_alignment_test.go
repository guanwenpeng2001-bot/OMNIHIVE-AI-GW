package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChatOnlyMediaPlanUsesDeclaredFamily(t *testing.T) {
	for _, platform := range []string{PlatformHunyuan, PlatformVolcengine, PlatformQianfan} {
		for _, evidence := range []struct {
			name     string
			metadata UpstreamModelMetadata
		}{
			{"explicit task and method", UpstreamModelMetadata{TaskType: "image_generation", SupportedMethods: []string{"images"}}},
			{"method without task", UpstreamModelMetadata{SupportedMethods: []string{"images"}}},
			{"task without methods", UpstreamModelMetadata{TaskType: "image_generation"}},
			{"absent media evidence", UpstreamModelMetadata{}},
		} {
			t.Run(platform+"/"+evidence.name, func(t *testing.T) {
				probe := &Account{Platform: platform, Type: AccountTypeAPIKey}
				declared := accountCatalogModelEndpoints(probe, "media-fixture", evidence.metadata)
				account := requestPlanGateAccount(platform, "media-fixture", declared...)
				ctx := requestPlanGateContext(GatewayRequestSpec{Action: "image.generate", Protocol: "media", Route: "/v1/images/generations", Model: "media-fixture"})
				family := openAICompatiblePlanCatalogEndpoint(ctx, &account)
				reason := requestPlanGateReason(ctx, &account, platform, "media-fixture", OpenAIEndpointCapabilityChatCompletions)
				selectable := (&OpenAIGatewayService{}).supportsOpenAIAccountModel(ctx, &account, "media-fixture", "")
				t.Logf("platform=%s evidence=%s declared=%v plan=%s gate=%q selectable=%t", platform, evidence.name, declared, family, reason, selectable)
				require.Equal(t, CompositeRouteEndpointImages, family)
				if evidence.name == "absent media evidence" {
					require.Equal(t, "model_not_supported", reason)
					require.False(t, selectable)
					return
				}
				require.Contains(t, declared, CompositeRouteEndpointImages)
				require.Empty(t, reason)
				require.True(t, selectable)
			})
		}
	}
}

func TestChatOnlyTaskDeclarationsRespectActionBindings(t *testing.T) {
	for _, platform := range []string{PlatformHunyuan, PlatformVolcengine, PlatformQianfan, PlatformMistral} {
		for _, scenario := range []struct{ task, endpoint, action string }{
			{"image_generation", "images", "image.generate"},
			{"video_generation", "images", "video.generate"},
			{"rerank", "rerank", "rerank"},
			{"reranking", "rerank", "rerank"},
			{"voice_clone", "voice", "voice"},
			{"audio.tts", "speech", "audio.tts"},
			{"audio_speech", "speech", "audio.tts"},
			{"text_to_speech", "speech", "audio.tts"},
			{"speech", "speech", "audio.tts"},
			{"audio.stt", "transcriptions", "audio.stt"},
			{"audio_transcription", "transcriptions", "audio.stt"},
			{"transcription", "transcriptions", "audio.stt"},
			{"speech_to_text", "transcriptions", "audio.stt"},
		} {
			t.Run(platform+"/"+scenario.task, func(t *testing.T) {
				account := &Account{Platform: platform, Type: AccountTypeAPIKey}
				declared := accountCatalogModelEndpoints(account, "task-fixture", UpstreamModelMetadata{TaskType: scenario.task})
				if platformActionAdapterBound(account, scenario.action) {
					require.Equal(t, []string{scenario.endpoint}, declared)
				} else {
					require.Empty(t, declared)
				}
				require.Empty(t, accountCatalogModelEndpoints(account, "task-fixture", UpstreamModelMetadata{TaskType: scenario.task, SupportedMethods: []string{}}))
			})
		}
	}
}

func TestChatOnlyApprovedPresetProjection(t *testing.T) {
	registry := NewModelRegistry().Snapshot()
	for _, scenario := range []struct{ platform, model, endpoint string }{
		{PlatformVolcengine, "doubao-seedream-4-0-250828", "images"},
		{PlatformVolcengine, "doubao-seedance-1-0-pro-250528", "images"},
		{PlatformQianfan, "qwen-image", "images"},
		{PlatformQianfan, "bce-reranker-base", "rerank"},
		{PlatformMistral, "voxtral-mini-tts-2603", "speech"},
	} {
		t.Run(scenario.platform+"/"+scenario.model, func(t *testing.T) {
			metadata := lookupCapabilityMetadata(registry.CapabilitySnapshot(), scenario.platform, scenario.model).Metadata
			require.True(t, metadata.TaskType != "" || len(metadata.SupportedMethods) > 0, "approved registry must supply task or method evidence")
			account := &Account{Platform: scenario.platform, Type: AccountTypeAPIKey}
			catalog := &UpstreamModelCatalog{Models: []string{scenario.model}, Metadata: map[string]UpstreamModelMetadata{scenario.model: metadata}}
			snapshot := projectAccountModelCatalog(account, catalog)
			t.Logf("platform=%s model=%s task=%s methods=%v eligible=%v excluded=%v", scenario.platform, scenario.model, metadata.TaskType, metadata.SupportedMethods, snapshot.EligibleModels, snapshot.ExcludedModels)
			require.Empty(t, snapshot.ExcludedModels)
			require.Len(t, snapshot.EligibleModels, 1)
			require.Contains(t, snapshot.EligibleModels[0].Endpoints, scenario.endpoint)
		})
	}
}

func TestChatOnlyAudioAndRerankPlansUseActionFamily(t *testing.T) {
	for _, scenario := range []struct{ platform, task, method, protocol, action, route string }{
		{PlatformQianfan, "rerank", "rerank", "rerank", "rerank", "/v1/rerank"},
		{PlatformHunyuan, "voice_clone", "voice", "audio", "voice", "/v1/custom-voices"},
		{PlatformQianfan, "speech", "speech", "audio", "audio.tts", "/v1/audio/speech"},
		{PlatformHunyuan, "transcription", "transcriptions", "audio", "audio.stt", "/v1/audio/transcriptions"},
		{PlatformMistral, "audio_speech", "speech", "audio", "audio.tts", "/v1/audio/speech"},
		{PlatformMistral, "audio_transcription", "transcriptions", "audio", "audio.stt", "/v1/audio/transcriptions"},
	} {
		t.Run(scenario.platform+"/"+scenario.task, func(t *testing.T) {
			for _, metadata := range []UpstreamModelMetadata{{TaskType: scenario.task}, {SupportedMethods: []string{scenario.method}}} {
				probe := &Account{Platform: scenario.platform, Type: AccountTypeAPIKey}
				declared := accountCatalogModelEndpoints(probe, "action-fixture", metadata)
				require.Contains(t, declared, scenario.method)
				account := requestPlanGateAccount(scenario.platform, "action-fixture", declared...)
				for _, action := range []string{scenario.action, ""} {
					ctx := requestPlanGateContext(GatewayRequestSpec{Action: action, Protocol: scenario.protocol, Route: scenario.route, Model: "action-fixture"})
					require.Equal(t, scenario.method, openAICompatiblePlanCatalogEndpoint(ctx, &account))
					require.Empty(t, requestPlanGateReason(ctx, &account, scenario.platform, "action-fixture", OpenAIEndpointCapabilityChatCompletions))
					require.True(t, (&OpenAIGatewayService{}).supportsOpenAIAccountModel(ctx, &account, "action-fixture", ""))
					textOnly := requestPlanGateAccount(scenario.platform, "action-fixture", "responses", "chat_completions")
					require.Equal(t, "model_not_supported", requestPlanGateReason(ctx, &textOnly, scenario.platform, "action-fixture", OpenAIEndpointCapabilityChatCompletions))
				}
			}
		})
	}
}

func TestChatOnlyAffinityUsesSchedulingFamily(t *testing.T) {
	for _, scenario := range []struct{ platform, protocol, endpoint, route string }{
		{PlatformHunyuan, "media", "images", "/v1/images/generations"},
		{PlatformHunyuan, "audio", "voice", "/v1/custom-voices"},
		{PlatformVolcengine, "media", "images", "/v1/videos"},
		{PlatformQianfan, "rerank", "rerank", "/v1/rerank"},
		{PlatformMistral, "audio", "speech", "/v1/audio/speech"},
		{PlatformMistral, "chat_completions", "chat_completions", "/v1/chat/completions"},
	} {
		t.Run(scenario.platform+"/"+scenario.protocol, func(t *testing.T) {
			account := requestPlanGateAccount(scenario.platform, "affinity-fixture", scenario.endpoint)
			repo := &forwardCatalogRepo{current: &account}
			ctx := requestPlanGateContext(GatewayRequestSpec{Protocol: scenario.protocol, Route: scenario.route, Model: "affinity-fixture"})
			require.False(t, requestAffinityPermanentlyInvalid(ctx, repo, account.ID))
			revoked := requestPlanGateAccount(scenario.platform, "affinity-fixture", "unrelated")
			repo.current = &revoked
			require.True(t, requestAffinityPermanentlyInvalid(ctx, repo, revoked.ID))
		})
	}
}
