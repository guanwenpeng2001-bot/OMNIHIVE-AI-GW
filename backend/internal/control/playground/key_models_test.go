package playground

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSelectedKeyDispatchRecognizesLiveWalletHold(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	g := &Gateway{DB: db}
	key := &service.APIKey{ID: 1, UserID: 2}
	hold := &service.KeyBudgetReservation{ID: 3, APIKeyID: 1, UserID: 99, RequestID: "request", BalanceHeld: true}
	held, err := g.currentWalletHold(t.Context(), key, hold)
	require.NoError(t, err)
	require.False(t, held)
	hold.UserID = key.UserID
	for _, live := range []bool{true, false} {
		mock.ExpectQuery("SELECT EXISTS .*FROM api_key_budget_reservations").
			WithArgs(hold.ID, key.ID, key.UserID, hold.RequestID).
			WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(live))
		held, err = g.currentWalletHold(t.Context(), key, hold)
		require.NoError(t, err)
		require.Equal(t, live, held)
	}
	require.NoError(t, mock.ExpectationsWereMet())
}
func TestSelectedKeyModelProjectionIntersectsAccountEndpointAndActions(t *testing.T) {
	now := time.Now().UTC()
	observed := now.Add(-time.Minute)
	model := catalog.Detail{
		Model: catalog.Summary{ID: "stable", Model: "public"}, Aliases: []string{"shared"},
		Endpoints: []catalog.Endpoint{
			{ID: "live", Capabilities: []catalog.Capability{
				{Action: "text.chat", Protocol: "openai_chat", VerifiedAt: &observed, Evidence: catalog.Evidence{State: "fresh"}},
				{Action: "text.chat", Protocol: "openai_responses", VerifiedAt: &observed, Evidence: catalog.Evidence{State: "fresh", ExpiresAt: &now}},
				{Action: "image.generate", Protocol: "openai_images", VerifiedAt: &observed, Evidence: catalog.Evidence{State: "fresh"}},
			}},
		},
	}
	group := groupCatalog{snapshot: catalog.Snapshot{Details: []catalog.Detail{model}},
		accounts: map[string]int64{"live": 8}, byID: map[string]string{"stable": "stable"}, byName: map[string]string{"public": "stable", "shared": ""}}
	key := selectedKey{actions: []string{"text.chat"}, models: service.ModelAccessSnapshot{
		KeyRestriction:      &identity.EffectiveKeyModels{Ceiling: []string{"stable"}, Rule: []string{"public"}},
		AccountRestrictions: map[int64]identity.EffectiveKeyModels{8: {Rule: []string{"public"}, Named: []string{"stable"}}},
	}}
	items := group.keyModelItems(key, now)
	require.Len(t, items, 1)
	require.Equal(t, []modelPair{{"text.chat", "openai_chat"}}, items[0].Pairs)
	key.models.KeyRestriction.Rule = []string{"shared"}
	require.Empty(t, group.keyModelItems(key, now))
	key.models.KeyRestriction.Rule = nil
	key.models.KeyRestriction.Exclude = []string{"shared"}
	require.Empty(t, group.keyModelItems(key, now))
	key.models.KeyRestriction.Exclude = nil
	key.models.AccountRestrictions[8] = identity.EffectiveKeyModels{Rule: []string{}}
	require.Empty(t, group.keyModelItems(key, now))
	delete(key.models.AccountRestrictions, 8)
	key.actions = []string{"models.read"}
	require.Empty(t, group.keyModelItems(key, now))
}

func TestSelectedKeyModelCursorUsesSharedSecretAndRejectsTampering(t *testing.T) {
	first := New(nil, false, []byte("test key"))
	second := New(nil, false, []byte("test key"))
	token := keyModelsCursor{Actor: 1, Workspace: 2, Project: 3, Key: 4, ScopeVersion: 5,
		CatalogVersion: "catalog", Revision: "policy", After: "stable", AsOf: time.Now().UTC()}
	sealed := first.sealKeyModelCursor(token)
	got, err := second.openKeyModelCursor(sealed)
	require.NoError(t, err)
	require.Equal(t, token, got)
	_, err = second.openKeyModelCursor("X" + sealed)
	require.Error(t, err)
	_, err = New(nil, false, []byte("other key")).openKeyModelCursor(sealed)
	require.Error(t, err)
}

func TestCatalogKeyBindingRequiresResolvedIdentityAndAdmittedAccount(t *testing.T) {
	identityKey := catalog.NewModelIdentity(service.PlatformOpenAI, "shared")
	group := &service.Group{ID: 7, Platform: service.PlatformComposite}
	binding := catalog.ModelBinding{Identity: identityKey, AccountID: 23, BillingGroupID: group.ID, Action: "text.chat"}
	selection := catalogKeySelection{
		key: selectedKey{
			key:     &service.APIKey{Group: group},
			actions: []string{"text.chat"},
			models: service.ModelAccessSnapshot{
				KeyRestriction: &identity.EffectiveKeyModels{Ceiling: []string{identityKey.ID()}, Rule: []string{identityKey.Model}},
			},
		},
		accounts: map[int64]struct{}{23: {}},
	}
	ctx := context.WithValue(t.Context(), catalogKeyContextKey{}, selection)
	g := &Gateway{}
	require.False(t, g.CatalogKeyAllows(ctx, binding, map[string]string{"shared": ""}))
	names := map[string]string{"shared": identityKey.ID()}
	require.True(t, g.CatalogKeyAllows(ctx, binding, names))
	binding.AccountID++
	require.False(t, g.CatalogKeyAllows(ctx, binding, names))
	binding.AccountID--
	binding.Action = "text.embed"
	require.False(t, g.CatalogKeyAllows(ctx, binding, names))
}
