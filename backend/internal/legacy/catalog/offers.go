package legacy

import (
	"context"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog/postgres"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

func (s *Source) modelOffers(ctx context.Context, p catalog.Perspective, model service.PlazaModel, group service.PlazaGroup, access bool, userRates map[int64]float64, priceErr error, endpoints map[int64]postgres.EndpointEvidence, now time.Time) []catalog.Offer {
	id := catalog.NewModelIdentity(model.Platform, model.Name)
	out := make([]catalog.Offer, 0, len(model.CatalogOffers)+1)
	for _, raw := range model.CatalogOffers {
		offer := catalog.Offer{GroupID: group.ID, ChannelID: raw.ChannelID, Access: access,
			Eligible:    raw.Eligible && (p.Public() || len(raw.Bindings) > 0),
			CatalogOnly: !raw.Eligible, Published: true, PublicationBasis: raw.PublicationBasis, EligibilityUnknown: model.EligibilityUnknown}
		applicable := offer.Eligible && (p.Public() || offer.Access)
		port := s.prices
		if port == nil && applicable {
			port = plazaPricePort{plaza: s.plaza, model: model, group: group, offer: raw, userRates: userRates}
		}
		if applicable && len(raw.Bindings) != 0 && (s.prices != nil || raw.BillingModelSource == service.BillingModelSourceUpstream) {
			prices := make(map[catalog.ModelIdentity]catalog.Price)
			for _, b := range raw.Bindings {
				entry := offer
				binding := bindingFor(id, group.ID, raw, b)
				if p.Public() {
					entry.Bindings = []catalog.ModelBinding{binding}
				} else {
					attachObservations(&entry, p, id, binding, b, endpoints[b.AccountID], now)
					binding = entry.Bindings[0]
				}
				cached, found := prices[binding.BillingIdentity]
				if s.prices != nil || !found {
					cached = describe(ctx, port, p, id, binding, group, userRates, now, priceErr)
					prices[binding.BillingIdentity] = cached
				}
				entry.Price = cached
				out = append(out, entry)
			}
			continue
		}
		for _, b := range raw.Bindings {
			binding := bindingFor(id, group.ID, raw, b)
			if p.Public() {
				offer.Bindings = append(offer.Bindings, binding)
			} else {
				attachObservations(&offer, p, id, binding, b, endpoints[b.AccountID], now)
			}
		}
		if applicable {
			binding := catalog.ModelBinding{ChannelID: raw.ChannelID, OfferGroupID: group.ID,
				BillingGroupID: group.ID, BillingIdentity: id}
			if len(offer.Bindings) != 0 {
				binding = offer.Bindings[0]
			}
			offer.Price = describe(ctx, port, p, id, binding, group, userRates, now, priceErr)
		}
		out = append(out, offer)
	}
	if len(out) == 0 || !p.Public() && len(model.CatalogObservations) > 0 {
		observation := catalog.Offer{GroupID: group.ID, Access: access, CatalogOnly: true, Discovered: !p.Public() && model.Discovered,
			EligibilityUnknown: model.EligibilityUnknown}
		if !p.Public() {
			for _, b := range model.CatalogObservations {
				binding := bindingFor(id, group.ID, service.PlazaCatalogOffer{}, b)
				attachObservations(&observation, p, id, binding, b, endpoints[b.AccountID], now)
			}
		}
		out = append(out, observation)
	}
	return out
}

func describe(ctx context.Context, port catalog.PricePort, p catalog.Perspective, id catalog.ModelIdentity, b catalog.ModelBinding, group service.PlazaGroup, userRates map[int64]float64, now time.Time, priceErr error) catalog.Price {
	if priceErr != nil {
		return catalog.Price{State: "error"}
	}
	multiplier := group.RateMultiplier
	if personal, ok := userRates[group.ID]; ok && !p.Public() {
		multiplier = personal
	}
	price, err := port.Describe(ctx, catalog.PriceInput{Perspective: p, Identity: id, Binding: b, GroupID: group.ID, Multiplier: multiplier, At: now})
	if err != nil {
		return catalog.Price{State: "error"}
	}
	return price
}

func bindingFor(id catalog.ModelIdentity, groupID int64, offer service.PlazaCatalogOffer, b service.PlazaCatalogBinding) catalog.ModelBinding {
	billingModel := id.Model
	switch offer.BillingModelSource {
	case service.BillingModelSourceChannelMapped:
		billingModel = offer.ChannelMappedModel
	case service.BillingModelSourceUpstream:
		billingModel = b.UpstreamModel
	case service.BillingModelSourceResponse:
		billingModel = ""
	}
	action, protocol, _ := catalog.GatewayAction(b.Endpoint)
	var observed *time.Time
	if at, err := time.Parse(time.RFC3339Nano, b.EvidenceObservedAt); err == nil {
		at = at.UTC()
		observed = &at
	}
	return catalog.ModelBinding{Identity: id, AccountID: b.AccountID, AccountLabel: b.AccountLabel, AccountType: b.Identity.HostedAccountType,
		CredentialOwnerID: b.CredentialAccountID, CredentialRevision: b.CredentialIdentityRevision,
		IdentityRevision: b.IdentityRevision, SnapshotRevision: b.SnapshotRevision, RulesRevision: b.RulesRevision,
		Profile: b.Metadata.ProviderProfile, WireModel: b.UpstreamModel, Action: action, Protocol: protocol, Mode: catalogBindingMode(b),
		ChannelID: offer.ChannelID, OfferGroupID: groupID, BillingGroupID: groupID, BillingModelSource: offer.BillingModelSource,
		BillingIdentity: catalog.NewModelIdentity(id.Provider, billingModel), Eligible: offer.Eligible, DiscoveryState: b.DiscoveryState, ObservedAt: observed}
}

func attachObservations(offer *catalog.Offer, p catalog.Perspective, id catalog.ModelIdentity, binding catalog.ModelBinding, b service.PlazaCatalogBinding, evidence postgres.EndpointEvidence, now time.Time) {
	if !evidence.ForceGlobal && awsRegion.MatchString(evidence.Region) {
		binding.Region = evidence.Region
	}
	offer.Bindings = append(offer.Bindings, binding)
	if !p.EndpointMetadata {
		return
	}
	expiry := discoveryExpiry(b, evidence)
	facts := b.CatalogFieldFacts(expiry, now)
	offer.Facts = append(offer.Facts, facts...)
	offer.BindingFacts = append(offer.BindingFacts, catalog.BindingFacts{Profile: binding.Profile, Facts: facts, Historical: !binding.Eligible})
	if service.AuthoritativeCatalogModelField(b.Metadata, "task_type") {
		_, actions := catalog.TaskPresentation(b.Metadata.TaskType)
		offer.Actions = append(offer.Actions, actions...)
	}
	if endpoint, ok := projectEndpoint(p, id.Provider, b, evidence, now); ok {
		if offer.Access {
			for i := range endpoint.Capabilities {
				capability := &endpoint.Capabilities[i]
				capability.VerifiedExtensionActions = verifiedExtensionActions(*capability, evidence.VerifiedActions[id.Model])
			}
		}
		offer.Endpoints = append(offer.Endpoints, endpoint)
		offer.Capabilities = append(offer.Capabilities, endpoint.Capabilities...)
	}
}
