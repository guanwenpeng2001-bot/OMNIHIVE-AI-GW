//go:build unit

package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestCompositeTextPreflightMatchesOpenAIChatDescriptor(t *testing.T) {
	for _, descriptor := range domain.PlatformDescriptors() {
		cell := domain.PlatformAction(domain.ActionIdentity{Platform: descriptor.ID, HostedAccountType: service.AccountTypeAPIKey}, "chat")
		if cell.Adapter != "openai.chat" {
			continue
		}
		group := &service.Group{ID: 3, Platform: service.PlatformComposite}
		request, _ := requestPlanHTTPContext("/v1/chat/completions", `{}`, group)
		request.Request = request.Request.WithContext(service.WithCompositeRouteDecision(request.Request.Context(), service.CompositeRouteDecision{Matched: true, Source: service.CompositeRouteSourceExplicit, PublicModel: "vendor/future", UpstreamModel: "vendor/future", TargetPlatform: descriptor.ID}))
		require.True(t, openAICompatibleTextTargetAllowed(request, &service.APIKey{Group: group}, "vendor/future"), descriptor.ID)
	}
}
