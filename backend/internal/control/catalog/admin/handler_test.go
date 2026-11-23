//go:build unit

package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	identityhttp "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/identity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/stretchr/testify/require"
)

const listPath = "/api/v2/admin/catalog/models"

type fixture struct {
	version, readerError string
	details              []catalog.Detail
	readerCalls, scopes  int
	revision             int64
	perspective          catalog.Perspective
	asOf                 time.Time
	router               *gin.Engine
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	f := &fixture{version: "cat_" + strings.Repeat("1", 64), revision: 17, router: gin.New()}
	h := New(f, f, &config.Config{})
	r := f.router.Group(listPath, identityhttp.TenantHeaders, func(c *gin.Context) {
		switch c.GetHeader("Test-Identity") {
		case "admin", "other-admin", "stale-admin":
			actor := int64(9)
			switch c.GetHeader("Test-Identity") {
			case "other-admin":
				actor = 10
			case "stale-admin":
				actor = 11
			}
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: actor})
			c.Set(string(middleware.ContextKeyUserRole), "admin")
		case "workspace-admin":
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 4})
			c.Set(string(middleware.ContextKeyUserRole), "user")
		}
	})
	r.GET("", h.List)
	r.GET("/:id", h.Detail)
	return f
}

func (f *fixture) Evaluate(_ context.Context, actor, workspace, project int64, action string) (identity.Access, error) {
	f.scopes++
	if workspace != 7 && workspace != 8 || project != 0 && (workspace != 7 || project != 12) {
		return identity.Access{}, fault.NotFound
	}
	if actor == 11 || action != "catalog.verify" {
		return identity.Access{}, fault.Forbidden
	}
	return identity.Access{ActorID: actor, WorkspaceID: workspace, ProjectID: project, Version: f.revision}, nil
}

func (f *fixture) Snapshot(_ context.Context, p catalog.Perspective, asOf time.Time) (catalog.Snapshot, error) {
	f.readerCalls++
	f.perspective, f.asOf = p, asOf
	if f.readerError != "" {
		return catalog.Snapshot{}, errors.New(f.readerError)
	}
	if asOf.IsZero() {
		asOf = time.Now().UTC()
	}
	return catalog.Snapshot{Version: f.version, AsOf: asOf, Details: f.details}, nil
}

func (f *fixture) get(path, actor string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.Header.Set("Test-Identity", actor)
	f.router.ServeHTTP(w, r)
	return w
}

func payload[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "private, no-store", w.Header().Get("Cache-Control"))
	require.NotEmpty(t, w.Header().Get("X-Request-ID"))
	var out struct {
		Code    int
		Message string
		Data    T
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	require.Equal(t, 0, out.Code)
	require.Equal(t, "success", out.Message)
	return out.Data
}

func model(provider, name, availability, publication, billing string, kinds ...string) catalog.Detail {
	return catalog.Detail{Model: catalog.Summary{
		ID: catalog.StableID(provider, name), Model: name, DisplayName: name, Provider: provider,
		Source: &catalog.SourceAxis{Kinds: kinds}, Availability: &catalog.Availability{State: availability, Access: "allowed", Publication: publication},
		Billing: &catalog.Billing{State: billing},
	}}
}

func TestPlatformAdminScopeGuard(t *testing.T) {
	f := newFixture(t)
	f.details = []catalog.Detail{model("openai", "test", "live", "published", "priced")}
	id := f.details[0].Model.ID
	for _, tc := range []struct {
		path, identity, reason string
		status                 int
	}{
		{listPath + "?workspace_id=7", "", "UNAUTHENTICATED", 401},
		{listPath + "/" + id + "?workspace_id=7", "", "UNAUTHENTICATED", 401},
		{listPath + "?workspace_id=7", "workspace-admin", "ACTION_DENIED", 403},
		{listPath + "/" + id + "?workspace_id=7", "workspace-admin", "ACTION_DENIED", 403},
		{listPath + "?workspace_id=7", "stale-admin", "ACTION_DENIED", 403},
		{listPath, "admin", "INVALID_INPUT", 422},
		{listPath + "?workspace_id=1&workspace_id=7", "admin", "INVALID_INPUT", 422},
		{listPath + "?workspace_id=0", "admin", "INVALID_INPUT", 422},
		{listPath + "?workspace_id=9223372036854775808", "admin", "INVALID_INPUT", 422},
		{listPath + "?workspace_id=99", "admin", "NOT_FOUND", 404},
		{listPath + "/" + id + "?workspace_id=8&project_id=12", "admin", "NOT_FOUND", 404},
		{listPath + "/test?workspace_id=7", "admin", "NOT_FOUND", 404},
	} {
		w := f.get(tc.path, tc.identity)
		require.Equal(t, tc.status, w.Code, tc.path)
		require.Contains(t, w.Body.String(), tc.reason)
		require.Equal(t, "private, no-store", w.Header().Get("Cache-Control"))
	}
	require.Zero(t, f.readerCalls)
	require.Equal(t, 4, f.scopes)
	w := f.get(listPath+"/m_"+strings.Repeat("0", 32)+"?workspace_id=7", "admin")
	require.Equal(t, 404, w.Code)
	require.Equal(t, 1, f.readerCalls)
	payload[detailResponse](t, f.get(listPath+"/"+id+"?workspace_id=7&project_id=12", "admin"))
	require.Equal(t, catalog.Perspective{Kind: catalog.PerspectiveAdmin, Actor: 9, Workspace: 7, Project: 12, AccessVersion: 17, EndpointMetadata: true}, f.perspective)
	payload[listResponse](t, f.get(listPath+"?workspace_id=8", "admin"))
	require.EqualValues(t, 8, f.perspective.Workspace)
	require.Zero(t, f.perspective.Project)
	f.readerError = "credential-owner://secret"
	w = f.get(listPath+"?workspace_id=7", "admin")
	require.Equal(t, 503, w.Code)
	require.NotContains(t, w.Body.String(), f.readerError)
}

func TestListFilters(t *testing.T) {
	f := newFixture(t)
	route := model("openai", "Route", "live", "published", "unpriced", catalog.BuiltinReviewed)
	route.Aliases = []string{"alternate"}
	route.Offers = []catalog.Offer{{GroupID: 5, ChannelID: 2, Access: true, Eligible: true, Bindings: []catalog.ModelBinding{
		{AccountID: 3, Eligible: true},
	}}, {Bindings: []catalog.ModelBinding{{AccountID: 3, DiscoveryState: "current"}}}}
	discovered := model("gemini", "test", "unavailable", "unpublished", "unpriced", catalog.AccountDiscovery)
	discovered.Offers = []catalog.Offer{{Bindings: []catalog.ModelBinding{{AccountID: 8, DiscoveryState: "current"}}}}
	f.details = []catalog.Detail{
		route, discovered,
		model("openai", "pi", "unavailable", "unpublished", "unpriced", catalog.PiAIReference),
		model("openai", "hidden", "unavailable", "published", "unpriced", catalog.BuiltinReviewed),
		model("openai", "variations", "live", "published", "conflict", catalog.BuiltinReviewed),
		model("anthropic", "partial", "live", "published", "partial", catalog.BuiltinReviewed),
		model("anthropic", "price-error", "live", "published", "error", catalog.BuiltinReviewed),
		model("anthropic", "free", "live", "published", "free", catalog.BuiltinReviewed),
	}
	f.details[5].Provenance = []catalog.Provenance{{State: "stale"}}
	for _, tc := range []struct {
		filter string
		want   []string
	}{
		{"provider=OPENAI", []string{"Route", "hidden", "pi", "variations"}},
		{"model=Route", []string{"Route"}},
		{"model=route", []string{}},
		{"q=ALTERNATE", []string{"Route"}},
		{"q=openai%2Froute", []string{"Route"}},
		{"q=" + route.Model.ID, []string{"Route"}},
		{"source=account_discovery", []string{"test", "Route"}},
		{"availability=live&billing=unpriced&publication=published&provider=openai", []string{"Route"}},
		{"quick_filter=routable_but_unpriced", []string{"Route"}},
		{"quick_filter=discovered_not_enabled", []string{"test"}},
		{"quick_filter=listed_unrouted", []string{"hidden"}},
		{"quick_filter=group_price_conflict", []string{"variations"}},
		{"quick_filter=pi_only", []string{"pi"}},
		{"quick_filter=stale", []string{"partial"}},
	} {
		t.Run(tc.filter, func(t *testing.T) {
			out := payload[listResponse](t, f.get(listPath+"?workspace_id=7&"+tc.filter, "admin"))
			got := make([]string, 0, len(out.Items))
			for _, row := range out.Items {
				got = append(got, row.Model)
			}
			require.Equal(t, tc.want, got)
		})
	}
	for _, filter := range []string{"availability=oops", "source=secret", "billing=reference", "quick_filter=typo", "limit=0", "limit=101", "sort=name"} {
		require.Equal(t, 422, f.get(listPath+"?workspace_id=7&"+filter, "admin").Code)
	}
}

func TestListPagination(t *testing.T) {
	f := newFixture(t)
	for i := 105; i >= 1; i-- {
		f.details = append(f.details, model("openai", fmt.Sprintf("test-%03d", i), "live", "published", "priced"))
	}
	base := listPath + "?workspace_id=7"
	first := payload[listResponse](t, f.get(base, "admin"))
	require.Len(t, first.Items, 50)
	require.Equal(t, "test-001", first.Items[0].Model)
	require.EqualValues(t, 17, first.ScopeVersion)
	require.NotNil(t, first.Next)
	cursorParam := "&cursor=" + url.QueryEscape(*first.Next)
	second := payload[listResponse](t, f.get(base+"&limit=100"+cursorParam, "admin"))
	require.Len(t, second.Items, 55)
	require.Equal(t, "test-051", second.Items[0].Model)
	require.Equal(t, "test-105", second.Items[54].Model)
	require.Equal(t, first.Version, second.Version)
	require.True(t, first.AsOf.Equal(second.AsOf))
	require.True(t, first.AsOf.Equal(f.asOf))
	require.Nil(t, second.Next)
	for _, tc := range []struct{ path, actor string }{
		{base + "&billing=priced" + cursorParam, "admin"},
		{base + "&project_id=12" + cursorParam, "admin"},
		{listPath + "?workspace_id=8" + cursorParam, "admin"},
		{base + cursorParam, "other-admin"},
	} {
		require.Equal(t, 410, f.get(tc.path, tc.actor).Code)
	}
	badCursor := strings.Replace(*first.Next, ".", ".!", 1)
	require.Equal(t, 422, f.get(base+"&cursor="+url.QueryEscape(badCursor), "admin").Code)
	f.revision++
	require.Equal(t, 410, f.get(base+cursorParam, "admin").Code)
	f.revision--
	f.version = "cat_" + strings.Repeat("2", 64)
	require.Equal(t, 410, f.get(base+cursorParam, "admin").Code)
}

func TestNestedPagination(t *testing.T) {
	f := newFixture(t)
	d := model("openai", "test", "live", "published", "priced")
	for i := int64(52); i > 0; i-- {
		b := catalog.ModelBinding{AccountID: i, SnapshotRevision: 1, DiscoveryState: "current", Eligible: true}
		o := catalog.Offer{GroupID: i, ChannelID: 3, Access: true, Eligible: true, Published: true, PublicationBasis: "explicit", Bindings: []catalog.ModelBinding{b}}
		d.Offers = append(d.Offers, o, o)
	}
	f.details = []catalog.Detail{d, model("openai", "other", "unavailable", "unpublished", "unpriced")}
	base := listPath + "/" + d.Model.ID + "?workspace_id=7"
	out := payload[detailResponse](t, f.get(base, "admin"))
	require.Len(t, out.Accounts, 50)
	require.Len(t, out.Offers, 50)
	require.Len(t, out.Publications, 50)
	require.NotNil(t, out.NextAccounts)
	require.NotNil(t, out.NextOffers)
	require.EqualValues(t, 52, out.Model.DiscoveredAccountCount)
	require.EqualValues(t, 52, out.Model.GroupCount)
	require.EqualValues(t, 1, out.Model.ChannelCount)
	a, o := "&accounts_cursor="+url.QueryEscape(*out.NextAccounts), "&offers_cursor="+url.QueryEscape(*out.NextOffers)
	aNext := payload[detailResponse](t, f.get(base+a, "admin"))
	require.Len(t, aNext.Accounts, 2)
	require.EqualValues(t, 51, aNext.Accounts[0].ID)
	require.EqualValues(t, 52, aNext.Accounts[1].ID)
	require.Equal(t, "eligible", aNext.Accounts[0].Membership)
	require.Nil(t, aNext.NextAccounts)
	require.Equal(t, out.Offers, aNext.Offers)
	oNext := payload[detailResponse](t, f.get(base+o, "admin"))
	require.Equal(t, out.Accounts, oNext.Accounts)
	require.Len(t, oNext.Offers, 2)
	require.Len(t, oNext.Publications, 2)
	require.EqualValues(t, 51, oNext.Offers[0].GroupID)
	require.Nil(t, oNext.NextOffers)
	both := payload[detailResponse](t, f.get(base+a+o+"&catalog_version="+f.version, "admin"))
	require.Equal(t, aNext.Accounts, both.Accounts)
	require.Equal(t, oNext.Offers, both.Offers)
	require.True(t, both.AsOf.Equal(out.AsOf))
	require.Equal(t, 410, f.get(base+"&accounts_cursor="+url.QueryEscape(*out.NextOffers), "admin").Code)
	require.Equal(t, 410, f.get(listPath+"/"+f.details[1].Model.ID+"?workspace_id=7"+a, "admin").Code)
	f.version = "cat_" + strings.Repeat("3", 64)
	require.Equal(t, 410, f.get(base+a, "admin").Code)
	newPage := payload[detailResponse](t, f.get(base, "admin"))
	require.Equal(t, 410, f.get(base+a+"&offers_cursor="+url.QueryEscape(*newPage.NextOffers), "admin").Code)
	require.Equal(t, 410, f.get(base+"&catalog_version="+out.Version, "admin").Code)
}

func TestExpiredCursor(t *testing.T) {
	h := New(nil, nil, &config.Config{})
	_, next, err := window(h, []int{1, 2}, cursor{}, snapshotMeta{
		Version: "cat_" + strings.Repeat("a", 64), AsOf: time.Now().Add(-catalog.SnapshotTTL),
	}, "match", 1)
	require.NoError(t, err)
	require.NotNil(t, next)
	_, err = h.openCursor(*next, "match", "cursor")
	require.ErrorIs(t, err, snapshotExpired)
}

func TestDetailWhitelistsEvidenceAndPrices(t *testing.T) {
	f := newFixture(t)
	d := model("openai", "demo", "live", "published", "priced", catalog.BuiltinReviewed, catalog.PiAIReference, catalog.AccountDiscovery)
	secret := "internal-value-canary"
	d.Aliases = []string{"public-alias"}
	ctx, max := int64(200000), int64(32768)
	d.CommonFacts = &catalog.Facts{Context: &ctx, ContextKind: "input", Output: &max, Modalities: []string{"text"}, SupportedMethods: []string{secret}}
	sale, err := money.NewRate("USD", "2", "input_token", "1000000")
	require.NoError(t, err)
	saleOut, err := money.NewRate("USD", "8", "output_token", "1000000")
	require.NoError(t, err)
	ref, err := money.NewRate("USD", "3", "input_token", "1000000")
	require.NoError(t, err)
	version := "sha256:" + strings.Repeat("a", 64)
	d.Model.Billing.SalePrice = &catalog.SalePrice{Source: "channel", Scope: "authorized", Input: &sale, Output: &saleOut, Version: &version}
	d.Model.Billing.ReferencePrice = &catalog.Price{Source: "pi-ai", State: "reference", Input: &ref, Semantics: secret, Version: &secret}
	d.Model.Regions = []string{secret}
	d.Model.Scope = &catalog.EffectiveScope{Workspace: 6666666}
	d.PriceSource = secret
	d.DocumentationURL = &secret
	d.Provenance = []catalog.Provenance{
		{Field: "context_window", State: "known", SourceKind: catalog.BuiltinReviewed, ArtifactVersion: strings.Repeat("b", 64),
			Value: map[string]string{"tokens": "200000", "basis": "input", "internal_key": secret}},
		{Field: "max_output_tokens", State: "known", SourceKind: catalog.AccountDiscovery, Value: max,
			RecordKey: secret, ArtifactVersion: secret, Provider: secret, Profile: secret, MatchRule: secret, Trust: secret, Scope: secret},
		{Field: "supports_vision", State: "known", SourceKind: catalog.BuiltinReviewed, Value: false},
		{Field: "supported_methods", State: "known", SourceKind: catalog.AccountDiscovery, Value: []string{"responses", secret}},
		{Field: "supplier_cost", State: "known", SourceKind: catalog.AccountDiscovery, Value: secret},
	}
	now := time.Now().UTC().Truncate(time.Second)
	b := catalog.ModelBinding{
		AccountID: 9007199254740993, AccountLabel: "Observed account", AccountType: "apikey", WireModel: secret, Profile: secret,
		Region: secret, CredentialOwnerID: 7777777, CredentialRevision: 8888888, IdentityRevision: 1, RulesRevision: 20, SnapshotRevision: 4,
		BillingIdentity: catalog.NewModelIdentity("openai", secret), DiscoveryState: "current", ObservedAt: &now,
	}
	routeBinding := catalog.ModelBinding{AccountID: b.AccountID, Action: "text.chat", Protocol: "openai_responses", Eligible: true}
	stale := catalog.ModelBinding{AccountID: 3, DiscoveryState: "stale"}
	invalidated := catalog.ModelBinding{AccountID: 5, DiscoveryState: "invalidated"}
	d.Offers = []catalog.Offer{
		{GroupID: 8, ChannelID: 3, Access: true, Eligible: true, Published: true, PublicationBasis: "explicit",
			Price:    catalog.Price{State: "priced", Source: "channel", Input: &sale, Output: &saleOut, Version: &version, Semantics: secret},
			Bindings: []catalog.ModelBinding{routeBinding}},
		{GroupID: 9, ChannelID: 4, Access: true, CatalogOnly: true, Published: true, PublicationBasis: "legacy_default"},
		{GroupID: 10, ChannelID: 5, Access: true, CatalogOnly: true, PublicationBasis: "explicit"},
		{GroupID: 12, Access: true, CatalogOnly: true, EligibilityUnknown: true, Bindings: []catalog.ModelBinding{stale, invalidated}},
		{CatalogOnly: true, Bindings: []catalog.ModelBinding{b, b}},
	}
	f.details = []catalog.Detail{d}
	w := f.get(listPath+"/"+d.Model.ID+"?workspace_id=7", "admin")
	out := payload[detailResponse](t, w)
	require.Len(t, out.Provenance, 4)
	require.Len(t, out.Accounts, 3)
	require.Equal(t, "stale", out.Accounts[0].DiscoveryState)
	require.Equal(t, "invalidated", out.Accounts[1].DiscoveryState)
	require.Equal(t, "eligible", out.Accounts[2].Membership)
	require.Equal(t, "Observed account", out.Accounts[2].Label)
	require.Equal(t, "no_eligible_binding", out.Accounts[0].Membership)
	require.Len(t, out.Offers, 5)
	require.Equal(t, "group_default", out.Offers[0].PriceContext)
	require.Equal(t, "text.chat", out.Offers[0].Action)
	require.NotNil(t, out.Offers[0].Billing.Sale)
	require.True(t, out.Offers[0].Eligible)
	require.Equal(t, "no_eligible_binding", out.Offers[1].Reason)
	require.Equal(t, "eligibility_incomplete", out.Offers[3].Reason)
	require.Len(t, out.Publications, 4)
	require.Equal(t, "explicit", *out.Publications[0].Basis)
	require.Equal(t, "legacy_default", *out.Publications[1].Basis)
	require.Equal(t, "published_without_eligible_binding", out.Publications[1].Reason)
	require.Equal(t, "unpublished", out.Publications[2].State)
	require.Equal(t, "unknown", out.Publications[3].State)
	require.Nil(t, out.Publications[3].Basis)
	require.Contains(t, w.Body.String(), `"value":false`)
	require.Contains(t, w.Body.String(), `"value":"32768"`)
	require.Contains(t, w.Body.String(), `"value":{"tokens":"200000","basis":"input"}`)
	require.Contains(t, w.Body.String(), `"account_id":"`+strconv.FormatInt(b.AccountID, 10)+`"`)
	require.Contains(t, w.Body.String(), `"account_id":null`)
	require.Contains(t, w.Body.String(), `"reference_price":`)
	require.Contains(t, w.Body.String(), `"price_version":"`+version+`"`)
	for _, path := range []string{listPath, listPath + "/" + d.Model.ID} {
		raw := f.get(path+"?workspace_id=7", "admin").Body.String()
		for _, forbidden := range []string{secret, "7777777", "8888888", "6666666", "wire_model", "credential_owner", "record_key", "supplier_cost", "effective_scope", "billing_identity"} {
			require.NotContains(t, raw, forbidden)
		}
		require.Equal(t, 1+strings.Count(path, d.Model.ID), strings.Count(raw, `"price_version"`))
	}
}

func TestBindingPriceStates(t *testing.T) {
	rate, err := money.NewRate("USD", "0", "input_token", "1000000")
	require.NoError(t, err)
	cases := []struct {
		price         catalog.Price
		eligible      bool
		state, reason string
		sale          bool
	}{
		{catalog.Price{State: "priced"}, true, "unpriced", "no_sale_tariff", false},
		{catalog.Price{State: "unpriced", Reason: "billing_provider_undetermined"}, true, "unpriced", "billing_provider_undetermined", false},
		{catalog.Price{State: "priced", Input: &rate}, true, "partial", "required_dimension_missing", true},
		{catalog.Price{State: "priced", ContextBasis: "whole_request"}, true, "conditional", "scenario_required", true},
		{catalog.Price{State: "free"}, true, "free", "", true},
		{catalog.Price{State: "error"}, true, "error", "price_read_failed", false},
		{catalog.Price{State: "conflict"}, true, "conflict", "offers_disagree", false},
		{catalog.Price{State: "priced", Input: &rate}, false, "unpriced", "no_applicable_offer", false},
	}
	for _, tc := range cases {
		b := bindingBilling(tc.price, tc.eligible)
		require.Equal(t, tc.state, b.State)
		require.Equal(t, tc.reason, b.Reason)
		require.Equal(t, tc.sale, b.Sale != nil)
	}
}
