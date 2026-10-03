//go:build unit

package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"testing"
)

func TestCompatibleProviderHandlerTarget(t *testing.T) {
	row := domain.CompatibleProvider{ID: "fixture-cloud", Label: "Fixture", LabelZh: "Fixture", BaseSource: "preset", DefaultMode: "payg", EvidenceURL: "https://docs.example.com", EvidenceObserved: "2026-10-03", Modes: []domain.CompatibleMode{{Mode: "payg", DefaultProtocol: domain.APIProtocolChatCompletions, Endpoints: []domain.CompatibleEndpoint{{Protocol: domain.APIProtocolChatCompletions, BaseURL: "https://api.example.com/v1", Auth: "bearer"}}, Bridges: []string{"messages_to_chat"}, Discovery: "none", CapabilityProfile: "fixture-cloud"}}}
	restore := domain.SetCompatibleProvidersForTest([]domain.CompatibleProvider{row})
	defer restore()
	for _, platform := range []string{"fixture-cloud", "unknown-cloud", service.PlatformOpenAI} {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
		c.Request = c.Request.WithContext(service.WithResolvedTargetPlatform(c.Request.Context(), platform))
		key := &service.APIKey{Group: &service.Group{Platform: service.PlatformComposite}}
		require.Equal(t, platform != "unknown-cloud", openAICompatibleTextTargetAllowed(c, key, "private-model"))
	}
}
