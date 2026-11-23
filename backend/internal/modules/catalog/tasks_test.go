package catalog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestCatalogOnlyPresentationKeepsRoutingAndPricesSeparate(t *testing.T) {
	now := time.Now()
	in := Input{Models: []Candidate{{Provider: "gemini", Model: "gemini-1.5-flash", Offers: []Offer{{Access: true, CatalogOnly: true}}}}}
	for _, perspective := range []Perspective{{}, {Workspace: 1, Project: 2}} {
		detail := Project(perspective, in, Window{}, now).Details[0]
		require.Equal(t, "catalog_only", detail.Model.AvailabilityState)
		require.Equal(t, "listed", detail.Model.CatalogState)
		require.Equal(t, "unpriced", detail.Model.PriceState)
		require.Nil(t, detail.Model.Input)
		require.Empty(t, detail.EligibleGroups)
		require.NotNil(t, detail.ReferencePrice)
		require.Equal(t, "reference", detail.ReferencePrice.State)
		if perspective.Public() {
			require.Equal(t, "requires_scope", detail.Model.AccessState)
			require.Equal(t, "requires_scope", detail.Model.RouteState)
		} else {
			require.Equal(t, "allowed", detail.Model.AccessState)
			require.Equal(t, "unknown", detail.Model.RouteState)
		}
	}
	rate, err := money.NewRate("USD", "0.02", "input_token", "1")
	require.NoError(t, err)
	output, err := money.NewRate("USD", "0.04", "output_token", "1")
	require.NoError(t, err)
	live := Offer{GroupID: 9, Access: true, Eligible: true, Price: Price{State: "priced", Source: "public_plaza_reference", Input: &rate, Output: &output}}
	in.Models[0].Offers = append(in.Models[0].Offers, live)
	for _, perspective := range []Perspective{{}, {Workspace: 1, Project: 2}} {
		detail := Project(perspective, in, Window{}, now).Details[0]
		require.Equal(t, "live", detail.Model.AvailabilityState)
		require.Equal(t, "priced", detail.Model.PriceState)
		require.Equal(t, "priced", detail.Model.Billing.State)
		require.Equal(t, &rate, detail.Model.Input)
		require.Nil(t, detail.ReferencePrice)
		require.NotNil(t, detail.Model.Billing.ReferencePrice)
		if !perspective.Public() {
			require.Equal(t, []int64{9}, detail.EligibleGroups)
			require.Equal(t, "eligible", detail.Model.RouteState)
		}
	}
}

func TestCatalogTaskPresentation(t *testing.T) {
	for _, tc := range []struct{ task, modality, action string }{
		{"text_generation", "text", "text.chat"}, {"code_generation", "text", "text.chat"},
		{"image_generation", "image", "image.generate"}, {"video_generation", "video", "video.generate"},
		{"audio_generation", "audio", "audio.generate"}, {"speech", "audio", "audio.speech"},
		{"transcription", "audio", "audio.transcription"}, {"embedding", "embedding", "text.embeddings"},
		{"realtime", "audio", "realtime"}, {"three_dimensional_generation", "3d", "3d.generate"},
	} {
		t.Run(tc.task, func(t *testing.T) {
			modalities, actions := TaskPresentation(tc.task)
			require.Contains(t, modalities, tc.modality)
			require.Equal(t, []string{tc.action}, actions)
		})
	}
	modalities, actions := TaskPresentation("unknown")
	require.Empty(t, modalities)
	require.Empty(t, actions)
}

func TestCatalogOpenAPIContractAlignment(t *testing.T) {
	specBytes, err := os.ReadFile(filepath.Join("..", "..", "..", "api", "openapi", "fragments", "catalog.yaml"))
	require.NoError(t, err)
	var spec struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]any `yaml:"properties"`
			} `yaml:"schemas"`
		} `yaml:"components"`
	}
	require.NoError(t, yaml.Unmarshal(specBytes, &spec))

	input := Input{Models: []Candidate{{Provider: "gemini", Model: "gemini-1.5-flash", Offers: []Offer{{Access: true, CatalogOnly: true}}}}}
	detail := Project(Perspective{}, input, Window{}, time.Now()).Details[0]
	require.NotNil(t, detail.ReferencePrice)

	rawDetail, err := json.Marshal(detail)
	require.NoError(t, err)
	var detailMap map[string]any
	require.NoError(t, json.Unmarshal(rawDetail, &detailMap))
	for key := range detailMap {
		require.Contains(t, spec.Components.Schemas["ModelDetail"].Properties, key, "ModelDetail missing runtime key %q", key)
	}

	modelMap, ok := detailMap["model"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "catalog_only", modelMap["availability_state"])
	availability, ok := modelMap["availability"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "requires_scope", availability["state"])
	for key := range modelMap {
		require.Contains(t, spec.Components.Schemas["ModelSummary"].Properties, key, "ModelSummary missing runtime key %q", key)
	}

	refPriceMap, ok := detailMap["reference_price"].(map[string]any)
	require.True(t, ok)
	for key := range refPriceMap {
		require.Contains(t, spec.Components.Schemas["CatalogReferencePrice"].Properties, key, "CatalogReferencePrice missing runtime key %q", key)
	}
}
