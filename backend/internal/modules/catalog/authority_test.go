package catalog

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
	"github.com/stretchr/testify/require"
)

func TestPublicationAvailabilityAndBillingRemainIndependent(t *testing.T) {
	m := Candidate{Provider: "p", Model: "model", Published: true, Offers: []Offer{{GroupID: 1, Access: true, Eligible: true, VerifiedLive: true, Price: Price{Kind: "sale", State: "unpriced", BillingState: "missing", Source: "unavailable"}}}}
	input := Input{Models: []Candidate{m}}
	public := Project(Perspective{Audience: "public"}, input, Window{}, time.Now()).Details[0]
	scoped := Project(Perspective{Audience: "scoped", ScopeKind: "project", Project: 1}, input, Window{}, time.Now()).Details[0]
	require.Equal(t, "published", public.Model.PublicationState)
	require.Equal(t, "requires_scope", public.Model.AvailabilityState)
	require.Equal(t, "live", scoped.Model.AvailabilityState)
	require.Equal(t, "missing", public.Model.BillingState)
	require.Equal(t, "missing", scoped.Model.BillingState)
	m.Published = false
	m.Offers[0].CatalogOnly = true
	input.Models[0] = m
	discovered := Project(Perspective{Audience: "scoped", Project: 1}, input, Window{}, time.Now()).Details[0]
	require.Equal(t, "draft", discovered.Model.PublicationState)
	require.Equal(t, "unknown", discovered.Model.AvailabilityState)
	require.Empty(t, discovered.GroupPrices, "a discovered account is not an executable offering")
}

func TestConflictingGroupOffersNeverSelectTheLastPrice(t *testing.T) {
	rate := func(amount string) *money.Rate {
		r, err := money.NewRate("USD", amount, "input_token", "1")
		require.NoError(t, err)
		return &r
	}
	price := func(amount string) Price {
		return Price{Kind: "sale", State: "priced", Source: "group", Input: rate(amount)}
	}
	m := Candidate{Provider: "p", Model: "same", Offers: []Offer{
		{GroupID: 1, Access: true, Eligible: true, Price: price("1")},
		{GroupID: 1, Access: true, Eligible: true, Price: price("2")},
		{GroupID: 1, Access: true, Eligible: true, Price: price("3")},
	}}
	d := Project(Perspective{Audience: "scoped", Project: 1}, Input{Models: []Candidate{m}}, Window{}, time.Now()).Details[0]
	require.Equal(t, "conflict", d.Model.BillingState)
	require.Equal(t, "conflict", d.GroupPrices[1].BillingState)
	require.Equal(t, "unpriced", d.GroupPrices[1].State)
	require.Nil(t, d.GroupPrices[1].Input)
	m.Offers = m.Offers[:2]
	m.Offers[1].GroupID = 2
	d = Project(Perspective{Audience: "scoped", Project: 1}, Input{Models: []Candidate{m}}, Window{}, time.Now()).Details[0]
	require.Equal(t, "multiple_offers", d.Model.BillingState)
	require.Equal(t, "priced", d.GroupPrices[1].State)
	require.Equal(t, "priced", d.GroupPrices[2].State)
}

func TestPublicProjectionWithholdsDiagnosticAccountsAndPersonalPriceContext(t *testing.T) {
	m := Candidate{Provider: "p", Model: "same", Offers: []Offer{{Access: true, GroupID: 8, AccountIDs: []string{"private-account"}, Accounts: []AccountDiagnostic{{AccountID: "private-account", ConnectionState: "active"}}}}}
	snap := Project(Perspective{Audience: "public"}, Input{Models: []Candidate{m}}, Window{}, time.Now())
	body, err := json.Marshal(snap)
	require.NoError(t, err)
	require.NotContains(t, string(body), "private-account")
	require.NotContains(t, string(body), "offers")
	require.NotContains(t, string(body), "group_id")
	p := Perspective{Audience: "scoped", ScopeKind: "project", Actor: 1, Workspace: 1, Project: 2, PricingActor: 3, EndpointMetadata: true}
	scoped := Project(p, Input{Models: []Candidate{m}}, Window{}, time.Now())
	require.Len(t, scoped.Details[0].Offers, 1)
	p.PricingActor = 4
	other := Project(p, Input{Models: []Candidate{m}}, Window{}, scoped.AsOf)
	require.NotEqual(t, scoped.Version, other.Version, "payer-sensitive snapshots must not share a version/cache key")
	require.False(t, (Perspective{Audience: "scoped", ScopeKind: "personal"}).Public())
}
