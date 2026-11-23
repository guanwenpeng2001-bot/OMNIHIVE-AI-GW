package catalog

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
	"github.com/stretchr/testify/require"
)

func TestCatalogAvailabilitySeparatesPublicationAndScope(t *testing.T) {
	in := Input{Models: []Candidate{
		{Provider: "openai", Model: "visible", Offers: []Offer{{Published: true, Eligible: true, Access: true}}},
		{Provider: "openai", Model: "disabled", Offers: []Offer{{Published: true, CatalogOnly: true}}},
		{Provider: "openai", Model: "discovered", Offers: []Offer{{Discovered: true, Access: true, CatalogOnly: true}}},
	}}
	now := time.Now()
	public := Project(Perspective{}, in, Window{}, now)
	d, ok := public.Find("openai/visible")
	require.True(t, ok)
	require.Equal(t, "live", d.Model.AvailabilityState)
	require.Equal(t, &Availability{State: "requires_scope", Publication: "published", Access: "requires_scope", Reason: "scope_required"}, d.Model.Availability)
	require.Nil(t, d.Model.Source.Discovered)
	d, _ = public.Find("openai/disabled")
	require.Equal(t, "published_without_eligible_binding", d.Model.Availability.Reason)
	project := Project(Perspective{Workspace: 1, Project: 2, AccessVersion: 3}, in, Window{}, now)
	d, _ = project.Find("openai/visible")
	require.Equal(t, "live", d.Model.Availability.State)
	require.Equal(t, "no_sale_tariff", d.Model.Billing.Reason)
	d, _ = project.Find("openai/discovered")
	require.Equal(t, "unavailable", d.Model.Availability.State)
	require.Equal(t, "discovered_without_eligible_binding", d.Model.Availability.Reason)
	require.True(t, *d.Model.Source.Discovered)
}

func TestCatalogBillingDistinguishesMissingPartialConflictAndFree(t *testing.T) {
	in, _ := money.NewRate("USD", "0.00001", "input_token", "1")
	out, _ := money.NewRate("USD", "0.00002", "output_token", "1")
	zeroIn, _ := money.NewRate("USD", "0", "input_token", "1")
	zeroOut, _ := money.NewRate("USD", "0", "output_token", "1")
	complete := Price{State: "priced", Source: "group", Input: &in, Output: &out, Version: new("v1")}
	equivalent := complete
	equivalent.Source, equivalent.Version = "channel", new("v2")
	for _, tc := range []struct {
		prices                []Price
		state, reason, legacy string
	}{
		{[]Price{{}}, "unpriced", "no_sale_tariff", "unpriced"},
		{[]Price{{State: "error"}}, "error", "price_read_failed", "error"},
		{[]Price{complete, {}}, "partial", "offer_price_missing", "unpriced"},
		{[]Price{{State: "priced", Input: &in}}, "partial", "required_dimension_missing", "unpriced"},
		{[]Price{{State: "free", Input: &zeroIn, Output: &zeroOut}}, "free", "", "free"},
		{[]Price{complete, {State: "free", Input: &zeroIn, Output: &zeroOut}}, "conflict", "offers_disagree", "unpriced"},
		{[]Price{complete, equivalent}, "priced", "", "priced"},
		{[]Price{{State: "priced", Input: &in, Output: &out, ContextBasis: "whole_request"}}, "conditional", "scenario_required", "priced"},
	} {
		t.Run(tc.state+"/"+tc.reason, func(t *testing.T) {
			var offers []Offer
			for _, price := range tc.prices {
				offers = append(offers, Offer{Eligible: true, Price: price})
			}
			b, legacy := offerBilling(offers, true)
			require.Equal(t, tc.state, b.State)
			require.Equal(t, tc.reason, b.Reason)
			require.Equal(t, tc.legacy, legacy.State)
			if b.SalePrice != nil && b.State == "priced" && len(tc.prices) > 1 {
				require.Nil(t, b.SalePrice.Version)
			}
		})
	}
}

func TestCatalogIdentityProvenanceAndVersionAreProviderScoped(t *testing.T) {
	id := NewModelIdentity(" OPENAI ", "Owner/Case")
	other := NewModelIdentity("openrouter", "Owner/Case")
	require.NotEqual(t, id.ID(), other.ID())
	require.Equal(t, StableID("openai", "Owner/Case"), id.ID())
	require.NotEqual(t, id.ID(), NewModelIdentity("openai", "Owner/case").ID())
	in := Input{RegistryVersion: "r", Models: []Candidate{{Provider: id.Provider, Model: id.Model}, {Provider: other.Provider, Model: other.Model}},
		Registry: []RegistryModel{
			{Provider: id.Provider, Model: id.Model, Canonical: "private", Facts: ModelFacts{
				Facts:      Facts{Context: new(int64(1000)), ContextKind: "input", Tools: new(false), Modalities: []string{}},
				Origin:     Provenance{SourceKind: BuiltinReviewed, Scope: "public", Profile: "a", RecordKey: "private-record"},
				Suppressed: []string{"max_output_tokens"},
			}},
			{Provider: id.Provider, Model: id.Model, Facts: ModelFacts{
				Facts:  Facts{Context: new(int64(1000)), ContextKind: "total", Output: new(int64(90)), Tools: new(true), Modalities: []string{"text"}},
				Origin: Provenance{SourceKind: PlatformPreset, Scope: "public", Profile: "a"},
			}},
			{Provider: id.Provider, Model: id.Model, Facts: ModelFacts{
				Facts:  Facts{Context: new(int64(1000)), ContextKind: "total"},
				Origin: Provenance{SourceKind: BuiltinReviewed, Scope: "public", Profile: "b"},
			}},
			{Provider: other.Provider, Model: other.Model, Facts: ModelFacts{
				Facts:  Facts{Context: new(int64(2000)), ContextKind: "total"},
				Origin: Provenance{SourceKind: BuiltinReviewed, Scope: "public"},
			}},
		}}
	now := time.Now()
	v := Project(Perspective{}, in, Window{End: now}, now)
	d, ok := v.Find("OPENAI/Owner/Case")
	require.True(t, ok)
	require.Equal(t, id.ID(), d.Model.CanonicalID)
	require.Nil(t, d.CommonFacts.Context)
	require.Nil(t, d.CommonFacts.Output)
	require.Equal(t, new(false), d.CommonFacts.Tools)
	require.Equal(t, []string{}, d.CommonFacts.Modalities)
	require.Contains(t, d.Provenance, Provenance{Field: "context_window", State: "conflict"})
	raw, err := json.Marshal(d)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "private")
	d, ok = v.Find(other.ID())
	require.True(t, ok)
	require.EqualValues(t, 2000, *d.Model.Context)
	_, ok = v.Find("Owner/Case")
	require.False(t, ok)
	slices.Reverse(in.Models)
	slices.Reverse(in.Registry)
	later := now.Add(time.Hour)
	require.Equal(t, v.Version, Project(Perspective{}, in, Window{End: later}, later).Version)
}
