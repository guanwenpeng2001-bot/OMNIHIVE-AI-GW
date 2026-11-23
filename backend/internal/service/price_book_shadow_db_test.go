//go:build unit && pricebook_shadow

package service_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"

	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/repository"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

// TestPriceBookShadowDB is a release rehearsal against a read-only database
// copy. Run it with -tags=unit,pricebook_shadow and OH_PRICEBOOK_SHADOW_DSN.
func TestPriceBookShadowDB(t *testing.T) {
	dsn := os.Getenv("OH_PRICEBOOK_SHADOW_DSN")
	require.NotEmpty(t, dsn, "set OH_PRICEBOOK_SHADOW_DSN to a read-only database copy")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	db, err := sql.Open(dialect.Postgres, dsn)
	require.NoError(t, err)
	defer db.Close()
	var readOnly string
	require.NoError(t, db.QueryRowContext(ctx, "SHOW transaction_read_only").Scan(&readOnly))
	require.Equal(t, "on", readOnly, "use a database connection with default_transaction_read_only=on")
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	groups := repository.NewGroupRepository(client, db)
	channels := service.NewChannelService(repository.NewChannelRepository(db), groups, nil, nil, nil)
	resolver, err := service.NewPriceBookShadowResolver(os.Getenv("OH_PRICEBOOK_SHADOW_PRICES"),
		os.Getenv("OH_PRICEBOOK_SHADOW_FALLBACK"), os.Getenv("OH_PRICEBOOK_SHADOW_OVERRIDES"), channels)
	require.NoError(t, err)

	rows, err := db.QueryContext(ctx, `SELECT r.id, u.group_id, r.pricing_snapshot, a.platform, COALESCE(u.reasoning_effort,'')
		FROM usage_settlement_receipts r JOIN usage_logs u ON u.id = r.usage_log_id
		JOIN accounts a ON a.id = r.account_id
		WHERE r.pricing_state IN ('priced','free') AND r.pricing_snapshot <> ''
		AND COALESCE(u.billing_mode,'token') = 'token' ORDER BY r.id DESC LIMIT 1000`)
	require.NoError(t, err)
	defer rows.Close()
	readGroups := map[int64]*service.Group{}
	var inspected, changed, skipped int
	for rows.Next() {
		var id int64
		var groupID sql.NullInt64
		var encoded, provider, effort string
		require.NoError(t, rows.Scan(&id, &groupID, &encoded, &provider, &effort))
		var frozen service.PricingSnapshot
		if err := json.Unmarshal([]byte(encoded), &frozen); err != nil || frozen.TokenPrices == nil || frozen.PricingAt.IsZero() {
			skipped++
			continue
		}
		if frozen.Provider != "" {
			provider = frozen.Provider
		}
		if groupID.Valid && readGroups[groupID.Int64] == nil {
			group, err := groups.GetByID(ctx, groupID.Int64)
			if err != nil {
				t.Logf("receipt %d group unavailable: %v", id, err)
				skipped++
				continue
			}
			readGroups[groupID.Int64] = group
		}
		replayed, err := service.RecalculatePricingSnapshot(encoded)
		require.NoError(t, err, "receipt %d", id)
		require.Equal(t, frozen.PriceCardVersion, replayed.PriceCardVersion)
		require.Equal(t, frozen.Source, replayed.PriceSource)
		require.Equal(t, frozen.ChargedAmount, service.QuantizeUsageBillingAmount(replayed.ActualCost), "receipt %d", id)
		// Comparing live candidates is distinct from replay. Inputs are the
		// original quantities, time and multiplier against the same DB copy.
		group := readGroups[groupID.Int64]
		out := service.PriceBookShadow(ctx, resolver, service.CostInput{
			Model: frozen.Model, Provider: provider, Group: group, Tokens: frozen.Tokens, PricingAt: frozen.PricingAt,
			SizeTier: frozen.SizeTier, ServiceTier: frozen.ServiceTier, ReasoningEffort: effort,
			ThinkingEnabled: frozen.ThinkingEnabled, RateMultiplier: frozen.RateMultiplier,
		})
		inspected++
		if out.Legacy.Amount != out.Current.Amount || out.Legacy.State != out.Current.State {
			changed++
			t.Logf("receipt=%d group=%d provider=%s model=%s recorded=%0.8f legacy=%+v current=%+v", id, groupID.Int64, provider, frozen.Model, frozen.ChargedAmount, out.Legacy, out.Current)
		} else if out.Legacy.Source != out.Current.Source {
			t.Logf("receipt=%d provider=%s model=%s source %s -> %s (%s)", id, provider, frozen.Model, out.Legacy.Source, out.Current.Source, out.Current.Amount)
		}
	}
	require.NoError(t, rows.Err())
	t.Logf("compared=%d changed=%d skipped=%d", inspected, changed, skipped)
	require.Positive(t, inspected, "no replayable token samples in the copy")
	require.Zero(t, changed, "review every shadow drift before cutover")
}
