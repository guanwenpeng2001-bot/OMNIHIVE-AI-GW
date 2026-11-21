package catalog

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fixedAcquisition struct {
	version string
	input   Input
}

func (f fixedAcquisition) Load(context.Context, Perspective, Window, time.Time) (Input, error) {
	in := f.input
	in.RegistryVersion = f.version
	return in, nil
}

type countingPublisher struct{ calls int }

func (c *countingPublisher) Publish(context.Context, Snapshot, string, time.Time) error {
	c.calls++
	return nil
}

func TestPublicSnapshotDoesNotRepublishUnchangedProjection(t *testing.T) {
	now := time.Now().UTC()
	pub := &countingPublisher{}
	source := fixedAcquisition{version: "reg_1", input: Input{Models: []Candidate{
		{Provider: "openai", Model: "public-model", Offers: []Offer{{Eligible: true}}},
	}}}
	reader := NewReader(source, pub)

	_, err := reader.Snapshot(context.Background(), Perspective{}, time.Time{})
	require.NoError(t, err)
	_, err = reader.Snapshot(context.Background(), Perspective{}, time.Time{})
	require.NoError(t, err)
	require.Equal(t, 1, pub.calls, "an unchanged public projection must not write twice")

	_, err = reader.Snapshot(context.Background(), Perspective{}, now.Add(-time.Hour))
	require.NoError(t, err)
	require.Equal(t, 1, pub.calls, "historical snapshots are read-only")

	source.version = "reg_2"
	reader = NewReader(source, pub)
	_, err = reader.Snapshot(context.Background(), Perspective{}, time.Time{})
	require.NoError(t, err)
	require.Equal(t, 2, pub.calls)
}

func TestProjectionRedactsPrivateEvidenceAndPreservesUnknown(t *testing.T) {
	now := time.Now().UTC()
	disabled, zero := false, int64(0)
	id := StableID("openai", "owner/test-model")
	last := Observation("upstream_discovery", "unknown", now.Add(-48*time.Hour), now.Add(-24*time.Hour), now)
	model := Candidate{Provider: "openai", Model: "owner/test-model", Offers: []Offer{
		{Eligible: true, Facts: []ModelFacts{{Facts: Facts{Tools: &disabled, Output: &zero}, Evidence: last}},
			Endpoints:    []Endpoint{{ID: "private-endpoint", Regions: []string{"DE"}}},
			Capabilities: []Capability{{Action: "text.chat", Protocol: "openai_responses", Evidence: last}}},
	}}
	input := Input{Models: []Candidate{model}, Metrics: map[string]Metrics{id: {Requests: &zero, State: "insufficient_samples"}},
		Registry: []RegistryModel{{Provider: model.Provider, Model: model.Model, Canonical: "private-alias", Facts: ModelFacts{
			Evidence: Observation("approved_registry", "stable", now, time.Time{}, now)}}}}
	public := Project(Perspective{Workspace: 1}, input, Window{}, now).Details[0]
	require.Nil(t, public.Model.Scope)
	require.Nil(t, public.Metrics.Requests)
	require.Nil(t, public.Facts[0].Tools)
	require.Equal(t, "requires_scope", public.Model.RouteState)
	body, err := json.Marshal(public)
	require.NoError(t, err)
	require.NotContains(t, string(body), "private-endpoint")
	require.NotContains(t, string(body), "private-alias")
	require.NotContains(t, string(body), "upstream_discovery")
	p := Perspective{Actor: 2, Workspace: 1, Project: 3, AccessVersion: 4, EndpointMetadata: true}
	scoped := Project(p, input, Window{}, now).Details[0]
	require.Equal(t, "denied", scoped.Model.AccessState)
	require.Equal(t, "eligible", scoped.Model.RouteState)
	require.Equal(t, "stale", scoped.Model.EvidenceState)
	require.Equal(t, &zero, scoped.Metrics.Requests)
	require.Empty(t, scoped.Model.Protocols)
	require.Equal(t, "stale", scoped.Capabilities[0].State)
	require.Contains(t, scoped.Facts, ModelFacts{Facts: Facts{Tools: &disabled, Output: &zero}, Evidence: last})
	require.Len(t, scoped.Endpoints, 1)
	p.EndpointMetadata = false
	require.Empty(t, Project(p, input, Window{}, now).Details[0].Endpoints)
}

func TestPublicBuiltinMethodPairsAndUnknownModels(t *testing.T) {
	now := time.Now().UTC()
	in := Input{Models: []Candidate{{Provider: "openai", Model: "gpt-5"}, {Provider: "openai", Model: "unlisted-gpt-5"}, {Provider: "other", Model: "gpt-5"}}}
	for _, detail := range Project(Perspective{}, in, Window{}, now).Details {
		if detail.Model.Provider != "openai" || detail.Model.Model != "gpt-5" {
			require.Nil(t, detail.Model.Context)
			require.Empty(t, detail.Model.Protocols)
			continue
		}
		require.Equal(t, int64(400000), *detail.Model.Context)
		require.Contains(t, detail.Model.Modalities, "text")
		require.Contains(t, detail.Model.Protocols, "openai_chat")
		require.Contains(t, detail.Model.Protocols, "openai_responses")
		require.Contains(t, detail.Model.Actions, "text.chat")
		require.NotEmpty(t, detail.Capabilities)
		for _, capability := range detail.Capabilities {
			require.Equal(t, "approved_registry", capability.Source)
			require.Nil(t, capability.VerifiedAt)
		}
		require.Equal(t, "requires_scope", detail.Model.RouteState)
	}
}
