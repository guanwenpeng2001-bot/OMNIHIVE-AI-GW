//go:build integration

package pricing_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/pricing"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestPriceBookActivationPreservesHistoryAndProviderIsolation(t *testing.T) {
	db, _, _ := testutil.NewCapabilityPostgres(t)
	store := pricing.VersionStore{DB: db}
	ctx := t.Context()
	start := time.Now().UTC().Truncate(time.Second).Add(-2 * time.Hour)
	change, expire := start.Add(time.Hour), start.Add(2*time.Hour)
	pc := pricing.PriceContext{WorkspaceID: 1, ProjectID: 1, GroupID: 1, PayerID: 1, Provider: "provider-a", Model: uuid.NewString(), Funding: "wallet", PricingAt: start}
	rate := func(amount string) money.Rate {
		r, err := money.NewRate("USD", amount, "input_token", "1")
		require.NoError(t, err)
		return r
	}
	old := pricing.Version{ID: "old", Kind: "sale", Source: "group", Currency: "USD", EffectiveFrom: start, Rules: []pricing.Rule{{Kind: "input", Rate: rate("0.01")}}}
	next := pricing.Version{ID: "next", Kind: "sale", Source: "channel", Currency: "USD", EffectiveFrom: change, EffectiveUntil: &expire, Rules: []pricing.Rule{{Kind: "input", Rate: rate("0.02")}}}
	require.NoError(t, store.Save(ctx, pc.ScopeKey(), old))
	require.NoError(t, store.Publish(ctx, pc.ScopeKey(), old.ID, 7, start))
	require.NoError(t, store.Save(ctx, pc.ScopeKey(), next))
	require.NoError(t, store.Publish(ctx, pc.ScopeKey(), next.ID, 7, change))
	require.NoError(t, store.Publish(ctx, pc.ScopeKey(), next.ID, 7, change), "retry is idempotent")
	for _, test := range []struct {
		at time.Time
		id string
	}{{start, old.ID}, {change.Add(-time.Microsecond), old.ID}, {change, next.ID}, {expire.Add(-time.Microsecond), next.ID}} {
		versions, err := store.At(ctx, pc.ScopeKey(), test.at)
		require.NoError(t, err)
		require.Len(t, versions, 1)
		require.Equal(t, test.id, versions[0].ID)
	}
	_, err := store.At(ctx, pc.ScopeKey(), expire)
	require.ErrorIs(t, err, pricing.ErrPriceInactive, "expiry cannot resurrect an old legacy open tariff")
	changed := old
	changed.Rules = []pricing.Rule{{Kind: "input", Rate: rate("10")}}
	require.ErrorIs(t, store.Save(ctx, pc.ScopeKey(), changed), pricing.ErrVersionConflict)
	require.ErrorIs(t, store.Publish(ctx, pc.ScopeKey(), old.ID, 7, start.Add(time.Minute)), pricing.ErrVersionConflict)

	other := pc
	other.Provider = "provider-b"
	require.NoError(t, store.Save(ctx, other.ScopeKey(), changed))
	require.NoError(t, store.Publish(ctx, other.ScopeKey(), changed.ID, 7, start))
	a, err := store.At(ctx, pc.ScopeKey(), start)
	require.NoError(t, err)
	b, err := store.At(ctx, other.ScopeKey(), start)
	require.NoError(t, err)
	require.Equal(t, "0.01", a[0].Rules[0].Rate.Amount().String())
	require.Equal(t, "10", b[0].Rules[0].Rate.Amount().String())
	other.PayerID = 2
	none, err := store.At(ctx, other.ScopeKey(), start)
	require.NoError(t, err)
	require.Empty(t, none, "a personal price cannot be borrowed by another payer")
	_, err = db.ExecContext(ctx, `UPDATE exact_price_versions SET source='changed' WHERE scope_key=$1`, pc.ScopeKey())
	require.Error(t, err, "historical tariff rows must remain immutable")
	ref := old
	ref.ID, ref.Source = "reference", "pi-ai"
	require.ErrorIs(t, store.Save(ctx, pc.ScopeKey(), ref), pricing.ErrReferencePrice)
	var count int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM exact_price_versions WHERE scope_key=$1 AND version_id='reference'`, pc.ScopeKey()).Scan(&count))
	require.Zero(t, count)
}
