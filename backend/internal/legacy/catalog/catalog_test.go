package legacy

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog/postgres"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/pkg/pluginapi/v2/actions"
)

func TestCatalogEndpointVerificationAndModelEvidence(t *testing.T) {
	now := time.Now().UTC()
	observed := now.Add(-time.Hour)
	b := service.PlazaCatalogBinding{
		AccountModelBinding: service.AccountModelBinding{AccountID: 2, UpstreamModel: "private-model", Endpoint: "chat_completions", EvidenceObservedAt: observed.Format(time.RFC3339Nano)},
		Identity:            domain.ActionIdentity{Platform: "openai"},
	}
	p := catalog.Perspective{Workspace: 1, Project: 3}
	e := postgres.EndpointEvidence{Provider: "openai", ConnectionActive: true, VerificationAt: observed,
		VerificationExpiry: now.Add(time.Hour), VerifiedModels: []string{"another-model"}}
	endpoint, ok := projectEndpoint(p, "openai", b, e, now)
	require.True(t, ok)
	require.Nil(t, endpoint.Capabilities[0].VerifiedAt)
	e.VerifiedModels = []string{"private-model"}
	endpoint, ok = projectEndpoint(p, "openai", b, e, now)
	require.True(t, ok)
	require.Equal(t, "fresh", endpoint.Capabilities[0].State)
	require.NotNil(t, endpoint.Capabilities[0].VerifiedAt)
	require.Equal(t, "unknown", endpoint.Capabilities[0].Tools)
	require.Empty(t, b.CatalogModelFacts(discoveryExpiry(b, e), now))
	e.VerificationExpiry = now
	endpoint, _ = projectEndpoint(p, "openai", b, e, now)
	require.Equal(t, "stale", endpoint.Capabilities[0].State)

	e.LegacyAttempt, e.LegacyResult = now.Add(-time.Minute).Format(time.RFC3339Nano), "failed"
	disabled := false
	b.Metadata = service.UpstreamModelMetadata{ID: "private-model", ProviderProfile: "openai", SupportsFunctionCalling: &disabled,
		FieldEvidence: map[string]service.ModelCapabilityEvidence{"supports_function_calling": {
			Source: "upstream", MatchRule: "native_id", ProviderProfile: "openai", MatchedModelID: "private-model"}}}
	facts := b.CatalogModelFacts(discoveryExpiry(b, e), now)
	require.Len(t, facts, 1)
	require.Equal(t, "stale", facts[0].State)
	require.Equal(t, &disabled, facts[0].Tools)
}

func TestReviewedEndpointActions(t *testing.T) {
	now := time.Now()
	c := catalog.Capability{Protocol: "openai_images", Action: "image.generate", Evidence: catalog.Evidence{State: "fresh"}, VerifiedAt: &now}
	require.Empty(t, verifiedExtensionActions(c, []string{actions.ProviderResponses}))
	require.Equal(t, []string{actions.TaskImageGenerate}, verifiedExtensionActions(c, []string{actions.ProviderResponses, actions.TaskImageGenerate}))
	c.State = "stale"
	require.Empty(t, verifiedExtensionActions(c, []string{actions.TaskImageGenerate}))
}
