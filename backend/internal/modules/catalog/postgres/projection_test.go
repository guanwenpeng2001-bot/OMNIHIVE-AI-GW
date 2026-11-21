package postgres

import (
	"testing"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	"github.com/stretchr/testify/require"
)

func TestConfiguredCatalogPublicationRetainsPublicEvidenceBoundary(t *testing.T) {
	input := catalog.Input{Models: []catalog.Candidate{{Provider: "grok", Model: "video", Offers: []catalog.Offer{{CatalogOnly: true, Actions: []string{"private.action"}, Facts: []catalog.ModelFacts{{Evidence: catalog.Evidence{Source: "upstream_discovery"}}}}}}},
		Registry: []catalog.RegistryModel{{Provider: "grok", Model: "video", Actions: []string{"video.generate"}, Facts: catalog.ModelFacts{Evidence: catalog.Evidence{Source: "approved_registry"}, Facts: catalog.Facts{Modalities: []string{"video"}}}}}}
	public := catalog.Project(catalog.Perspective{}, input, catalog.Window{}, time.Now())
	require.NoError(t, validatePublic(public))
	require.Equal(t, []string{"video.generate"}, public.Details[0].Model.Actions)
	public.Details[0].Facts = append(public.Details[0].Facts, catalog.ModelFacts{Evidence: catalog.Evidence{Source: "upstream_discovery"}})
	require.Error(t, validatePublic(public))
}
