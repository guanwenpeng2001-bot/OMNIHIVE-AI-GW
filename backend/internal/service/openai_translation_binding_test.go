package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAITranslationRequiresDeclaredTranslationCapability(t *testing.T) {
	account := requestPlanGateAccount(PlatformOpenAI, "whisper-1", "audio_translations")
	ctx := WithGatewayRequestPlan(context.Background(), NewGatewayRequestPlan(GatewayRequestSpec{Action: "audio.translate", Protocol: "audio", Route: "/v1/audio/translations", Model: "whisper-1"}))
	require.True(t, AccountSupportsResolvedModel(ctx, &account, "whisper-1", AccountModelBindingOptions{FinalModel: "whisper-1", Endpoint: openAIActionCatalogEndpoint("audio.translate")}))
	account = requestPlanGateAccount(PlatformOpenAI, "whisper-1", "chat_completions")
	require.False(t, AccountSupportsResolvedModel(ctx, &account, "whisper-1", AccountModelBindingOptions{FinalModel: "whisper-1", Endpoint: openAIActionCatalogEndpoint("audio.translate")}))
}
