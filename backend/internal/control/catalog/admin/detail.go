package admin

import (
	"cmp"
	"net/url"
	"slices"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
)

type detailResponse struct {
	snapshotMeta
	Model        modelSummary       `json:"model"`
	Aliases      []string           `json:"aliases"`
	CommonFacts  *commonFacts       `json:"common_facts"`
	Provenance   []fieldObservation `json:"provenance"`
	Accounts     []account          `json:"accounts"`
	Offers       []offer            `json:"offers"`
	Publications []publication      `json:"publications"`
	NextAccounts *string            `json:"next_accounts_cursor"`
	NextOffers   *string            `json:"next_offers_cursor"`
}

type account struct {
	ID               int64      `json:"account_id,string"`
	Label            string     `json:"label"`
	Type             string     `json:"account_type"`
	DiscoveryState   string     `json:"discovery_state"`
	Membership       string     `json:"membership_state"`
	ObservedAt       *time.Time `json:"observed_at"`
	IdentityRevision int64      `json:"identity_revision,string"`
	SnapshotRevision int64      `json:"snapshot_revision,string"`
	RulesRevision    int64      `json:"rules_revision,string"`
}

type groupChannel struct {
	GroupID   int64 `json:"group_id,string"`
	ChannelID int64 `json:"channel_id,string"`
}

type offer struct {
	groupChannel
	AccountID    *int64  `json:"account_id,string"`
	Action       string  `json:"action,omitempty"`
	Protocol     string  `json:"protocol,omitempty"`
	Eligible     bool    `json:"eligible"`
	Reason       string  `json:"reason,omitempty"`
	PriceContext string  `json:"price_context"`
	Billing      billing `json:"billing"`
}

type publication struct {
	groupChannel
	State  string  `json:"state"`
	Basis  *string `json:"basis"`
	Reason string  `json:"reason"`
}

func (h *Handler) detail(c *gin.Context, p catalog.Perspective, q url.Values) (any, error) {
	id := c.Param("id")
	if !modelID.MatchString(id) {
		return nil, fault.NotFound
	}
	for key := range q {
		switch key {
		case "workspace_id", "project_id", "catalog_version", "accounts_cursor", "offers_cursor":
		default:
			return nil, fault.Invalid("/" + key)
		}
	}
	version := q.Get("catalog_version")
	if version != "" && !catalogVersion.MatchString(version) {
		return nil, fault.Invalid("/catalog_version")
	}
	match := func(kind string) string {
		return catalog.Digest(struct {
			Perspective catalog.Perspective
			ID, Kind    string
		}{p, id, kind})
	}
	accountsMatch, offersMatch := match("accounts"), match("offers")
	a, err := h.openCursor(q.Get("accounts_cursor"), accountsMatch, "accounts_cursor")
	if err != nil {
		return nil, err
	}
	o, err := h.openCursor(q.Get("offers_cursor"), offersMatch, "offers_cursor")
	if err != nil {
		return nil, err
	}
	prior := cursor{Version: version}
	for _, token := range []cursor{a, o} {
		if token.Version == "" {
			continue
		}
		if prior.Version != "" && token.Version != prior.Version {
			return nil, snapshotExpired
		}
		prior.Version = token.Version
		if prior.AsOf.IsZero() || token.AsOf.Before(prior.AsOf) {
			prior.AsOf = token.AsOf
		}
	}
	s, err := h.readSnapshot(c, p, prior)
	if err != nil {
		return nil, err
	}
	d, ok := s.Find(id)
	if !ok {
		return nil, fault.NotFound
	}
	meta := newSnapshotMeta(s, p)
	accounts, nextAccounts, err := window(h, projectAccounts(d.Offers), a, meta, accountsMatch, nestedLimit)
	if err != nil {
		return nil, err
	}
	offers, publications := projectOffers(d.Offers)
	offers, nextOffers, err := window(h, offers, o, meta, offersMatch, nestedLimit)
	if err != nil {
		return nil, err
	}
	return detailResponse{
		snapshotMeta: meta, Model: projectSummary(d, summarizeBindings(d.Offers)),
		Aliases: stringsList(d.Aliases), CommonFacts: projectFacts(d.CommonFacts), Provenance: projectProvenance(d.Provenance),
		Accounts: accounts, Offers: offers, Publications: pagePublications(offers, publications),
		NextAccounts: nextAccounts, NextOffers: nextOffers,
	}, nil
}

func projectAccounts(offers []catalog.Offer) []account {
	type observationKey struct{ account, revision int64 }
	observations := map[observationKey]account{}
	eligible := map[int64]bool{}
	for _, o := range offers {
		for _, b := range o.Bindings {
			if b.AccountID <= 0 {
				continue
			}
			eligible[b.AccountID] = eligible[b.AccountID] || offerEligible(o) && b.Eligible
			if !discoveryState(b.DiscoveryState) {
				continue
			}
			next := account{ID: b.AccountID, Label: b.AccountLabel, Type: b.AccountType, DiscoveryState: b.DiscoveryState,
				ObservedAt: b.ObservedAt, IdentityRevision: b.IdentityRevision, SnapshotRevision: b.SnapshotRevision, RulesRevision: b.RulesRevision}
			key := observationKey{b.AccountID, b.SnapshotRevision}
			if prior, ok := observations[key]; !ok || compareObservations(next, prior) > 0 {
				observations[key] = next
			}
		}
	}
	rows := make([]account, 0, len(observations))
	for _, row := range observations {
		row.Membership = "no_eligible_binding"
		if eligible[row.ID] {
			row.Membership = "eligible"
		}
		rows = append(rows, row)
	}
	slices.SortFunc(rows, func(a, b account) int {
		return cmp.Or(cmp.Compare(a.ID, b.ID), cmp.Compare(a.SnapshotRevision, b.SnapshotRevision))
	})
	return rows
}

func compareObservations(a, b account) int {
	states := map[string]int{"invalidated": 0, "stale": 1, "current": 2}
	return cmp.Or(cmp.Compare(a.IdentityRevision, b.IdentityRevision), cmp.Compare(a.RulesRevision, b.RulesRevision),
		cmp.Compare(states[a.DiscoveryState], states[b.DiscoveryState]), observationTime(a.ObservedAt).Compare(observationTime(b.ObservedAt)),
		cmp.Compare(a.Label, b.Label), cmp.Compare(a.Type, b.Type))
}

func observationTime(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

func projectOffers(raw []catalog.Offer) ([]offer, map[groupChannel]publication) {
	type rankedOffer struct {
		offer
		accountID int64
		digest    string
	}
	rows := make([]rankedOffer, 0, len(raw))
	seen := map[string]bool{}
	publications := map[groupChannel]publication{}
	add := func(o catalog.Offer, pair groupChannel, b catalog.ModelBinding) {
		eligible := offerEligible(o) && (b.AccountID == 0 || b.Eligible)
		row := offer{groupChannel: pair, Action: b.Action, Protocol: b.Protocol, Eligible: eligible,
			Reason: eligibilityReason(o, eligible), PriceContext: "group_default", Billing: bindingBilling(o.Price, eligible)}
		if b.AccountID > 0 {
			row.AccountID = &b.AccountID
		}
		digest := catalog.Digest(row)
		if !seen[digest] {
			seen[digest] = true
			rows = append(rows, rankedOffer{row, b.AccountID, digest})
		}
	}
	for _, o := range raw {
		if o.GroupID <= 0 {
			continue
		}
		pair := groupChannel{o.GroupID, o.ChannelID}
		next := projectPublication(o, pair)
		if previous, ok := publications[pair]; !ok || publicationRank(next) > publicationRank(previous) {
			publications[pair] = next
		}
		if len(o.Bindings) == 0 {
			add(o, pair, catalog.ModelBinding{})
		}
		for _, b := range o.Bindings {
			add(o, pair, b)
		}
	}
	slices.SortFunc(rows, func(a, b rankedOffer) int {
		return cmp.Or(cmp.Compare(a.GroupID, b.GroupID), cmp.Compare(a.ChannelID, b.ChannelID), cmp.Compare(a.accountID, b.accountID),
			cmp.Compare(a.Action, b.Action), cmp.Compare(a.Protocol, b.Protocol), cmp.Compare(a.digest, b.digest))
	})
	out := make([]offer, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.offer)
	}
	return out, publications
}

func eligibilityReason(o catalog.Offer, eligible bool) string {
	switch {
	case eligible:
		return ""
	case !o.Access:
		return "access_denied"
	case o.EligibilityUnknown:
		return "eligibility_incomplete"
	default:
		return "no_eligible_binding"
	}
}

func projectPublication(o catalog.Offer, pair groupChannel) publication {
	p := publication{groupChannel: pair, State: "unknown", Reason: "publication_not_observed"}
	if o.PublicationBasis == "explicit" || o.PublicationBasis == "legacy_default" {
		p.Basis = &o.PublicationBasis
		p.State, p.Reason = "unpublished", "not_published"
	}
	if o.Published {
		p.State, p.Reason = "published", "published"
		if !offerEligible(o) {
			p.Reason = "published_without_eligible_binding"
		}
	}
	return p
}

func publicationRank(p publication) int {
	rank := 0
	switch p.State {
	case "published":
		rank = 6
		if p.Reason == "published" {
			rank += 3
		}
	case "unpublished":
		rank = 3
	}
	if p.Basis != nil {
		rank++
		if *p.Basis == "explicit" {
			rank++
		}
	}
	return rank
}

func pagePublications(offers []offer, publications map[groupChannel]publication) []publication {
	out := make([]publication, 0, len(offers))
	for _, offer := range offers {
		if len(out) == 0 || out[len(out)-1].groupChannel != offer.groupChannel {
			out = append(out, publications[offer.groupChannel])
		}
	}
	return out
}
