package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChatOnlyExplicitNonTextMethodsNeverPublishText(t *testing.T) {
	for _, platform := range []string{PlatformHunyuan, PlatformVolcengine, PlatformQianfan, PlatformMistral} {
		for _, scenario := range []struct{ method, action string }{
			{"videos", ""}, {"embeddings", "embeddings"}, {"rerank", "rerank"},
			{"audio_transcriptions", ""}, {"transcriptions", "audio.stt"}, {"speech", "audio.tts"},
			{"images", "image.generate"}, {"unknown", ""}, {"text_chat_unknown", ""},
		} {
			t.Run(platform+"/"+scenario.method, func(t *testing.T) {
				account := &Account{Platform: platform, Type: AccountTypeAPIKey}
				declared := accountCatalogModelEndpoints(account, "method-fixture", UpstreamModelMetadata{SupportedMethods: []string{scenario.method}})
				for _, family := range []string{"chat_completions", "messages", "responses"} {
					require.NotContains(t, declared, family)
				}
				if scenario.action != "" && platformActionAdapterBound(account, scenario.action) {
					require.Equal(t, []string{scenario.method}, declared)
				} else {
					require.Empty(t, declared)
				}
			})
		}
	}
}

func TestChatOnlyMethodEvidencePrecedesOutputModalities(t *testing.T) {
	for _, platform := range []string{PlatformHunyuan, PlatformVolcengine, PlatformQianfan, PlatformMistral} {
		for _, method := range []string{"videos", "transcriptions", "rerank", "embeddings", "images", "unknown"} {
			for _, output := range [][]string{{"text"}, {"video"}, {"image"}, {"audio"}} {
				t.Run(platform+"/"+method+"/"+output[0], func(t *testing.T) {
					account := &Account{Platform: platform, Type: AccountTypeAPIKey}
					metadata := UpstreamModelMetadata{SupportedMethods: []string{method}, InputModalities: []string{"video"}, OutputModalities: output}
					require.Empty(t, accountCatalogEndpointTask(account, "modality-fixture", metadata))
					require.Equal(t, accountCatalogModelEndpoints(account, "modality-fixture", UpstreamModelMetadata{SupportedMethods: []string{method}}), accountCatalogModelEndpoints(account, "modality-fixture", metadata))
				})
			}
		}
	}
}

func TestChatOnlyNilEmptyAndMixedMethods(t *testing.T) {
	for _, platform := range []string{PlatformHunyuan, PlatformVolcengine, PlatformQianfan, PlatformMistral} {
		t.Run(platform, func(t *testing.T) {
			account := &Account{Platform: platform, Type: AccountTypeAPIKey}
			textFamilies := []string{"chat_completions"}
			switch platform {
			case PlatformHunyuan:
				textFamilies = append(textFamilies, "messages", "responses")
			case PlatformVolcengine, PlatformQianfan:
				textFamilies = append(textFamilies, "responses")
			}
			require.Equal(t, textFamilies, accountCatalogModelEndpoints(account, "fixture", UpstreamModelMetadata{}))
			for _, task := range []string{"", "text_generation", "video_generation", "audio_transcription"} {
				require.Empty(t, accountCatalogModelEndpoints(account, "fixture", UpstreamModelMetadata{TaskType: task, SupportedMethods: []string{}, OutputModalities: []string{"text"}}))
			}
			expected := append([]string{}, textFamilies...)
			expected = append(expected, "embeddings")
			if platform != PlatformMistral {
				expected = append(expected, "images")
			}
			for _, methods := range [][]string{
				{"images", "unknown", "embeddings", "chat_completions", "images"},
				{"chat_completions", "embeddings", "images", "unknown"},
			} {
				require.ElementsMatch(t, expected, accountCatalogModelEndpoints(account, "fixture", UpstreamModelMetadata{SupportedMethods: methods, OutputModalities: []string{"image"}}))
			}
			require.Empty(t, accountCatalogModelEndpoints(account, "fixture", UpstreamModelMetadata{TaskType: "text_generation", SupportedMethods: []string{"videos"}}))
			require.Equal(t, []string{"embeddings"}, accountCatalogModelEndpoints(account, "fixture", UpstreamModelMetadata{TaskType: "text_generation", SupportedMethods: []string{"embeddings"}}))
			conflicting := accountCatalogModelEndpoints(account, "fixture", UpstreamModelMetadata{TaskType: "image_generation", SupportedMethods: []string{"chat_completions", "responses", "messages"}})
			if platform == PlatformMistral {
				require.Empty(t, conflicting)
			} else {
				require.Equal(t, []string{"images"}, conflicting)
			}
		})
	}
}

func TestChatOnlyTextMethodDeclarationsRemainSchedulable(t *testing.T) {
	for _, platform := range []string{PlatformHunyuan, PlatformVolcengine, PlatformQianfan, PlatformMistral} {
		for _, method := range []string{"chat_completions", "responses", "messages"} {
			t.Run(platform+"/"+method, func(t *testing.T) {
				probe := &Account{Platform: platform, Type: AccountTypeAPIKey}
				declared := accountCatalogModelEndpoints(probe, "text-fixture", UpstreamModelMetadata{SupportedMethods: []string{method}})
				expected := []string{}
				if method == "chat_completions" {
					expected = append(expected, "chat_completions")
				}
				switch platform {
				case PlatformHunyuan:
					expected = append(expected, "messages", "responses")
				case PlatformVolcengine, PlatformQianfan:
					expected = append(expected, "responses")
				}
				require.Equal(t, expected, declared)
				account := requestPlanGateAccount(platform, "text-fixture", declared...)
				for _, spec := range []GatewayRequestSpec{
					{Action: "chat", Protocol: "chat_completions", Route: "/v1/chat/completions", Model: "text-fixture"},
					{Action: "responses", Protocol: "responses", Route: "/v1/responses", Model: "text-fixture"},
					{Action: "messages", Protocol: "anthropic", Route: "/v1/messages", Model: "text-fixture"},
				} {
					ctx := requestPlanGateContext(spec)
					allowed := platformActionAdapterBound(probe, spec.Action) && len(declared) > 0
					reason := requestPlanGateReason(ctx, &account, platform, "text-fixture", OpenAIEndpointCapabilityChatCompletions)
					if allowed {
						require.Empty(t, reason)
					} else {
						require.Equal(t, "model_not_supported", reason)
					}
					require.Equal(t, allowed, (&OpenAIGatewayService{}).supportsOpenAIAccountModel(ctx, &account, "text-fixture", OpenAIEndpointCapabilityChatCompletions))
				}
			})
		}
	}
}
