//go:build integration

package compare

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/catalog/quotes"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/pricing"
)

type testCatalog struct{ snapshot catalog.Snapshot }

func (s testCatalog) Snapshot(context.Context, catalog.Perspective, time.Time) (catalog.Snapshot, error) {
	return s.snapshot, nil
}

type testKeys struct{ store pricing.VersionStore }

func (s testKeys) ResolveKeyPrice(ctx context.Context, p catalog.Perspective, key int64, d catalog.Detail, protocol, action string, usage pricing.Usage, at time.Time) (pricing.Version, error) {
	if p.Project != 2 || p.Actor != 4 || key != 5 {
		return pricing.Version{}, fault.NotFound
	}
	versions, err := s.store.At(ctx, "test/key/5", at)
	if err != nil {
		return pricing.Version{}, err
	}
	v, err := pricing.Resolve(pricing.Candidates{Group: versions}, at)
	if err != nil || v == nil {
		return pricing.Version{}, err
	}
	return *v, nil
}

func TestBE07QuoteCompareAndMigration(t *testing.T) {
	dsn, addr := os.Getenv("BE07_POSTGRES_DSN"), os.Getenv("BE07_REDIS_ADDR")
	if dsn == "" || addr == "" {
		t.Skip("BE-07 requires disposable PostgreSQL and Redis")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer db.Close()
	cache := redis.NewClient(&redis.Options{Addr: addr})
	defer cache.Close()
	require.NoError(t, cache.Ping(ctx).Err())
	migration, err := os.ReadFile("../../../../migrations/296_exact_price_versions.sql")
	require.NoError(t, err)
	for range 2 {
		_, err = db.ExecContext(ctx, string(migration))
		require.NoError(t, err)
	}
	at := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)
	r, err := money.NewRate("USD", "0.000078125", "input_token", "1")
	require.NoError(t, err)
	store := pricing.VersionStore{DB: db}
	version := pricing.Version{ID: "pr_golden", Currency: "USD", Source: "published", EffectiveFrom: at.Add(-time.Hour), Rules: []pricing.Rule{{Kind: "input", Rate: r}}}
	require.NoError(t, store.Save(ctx, "test/key/5", version))
	require.NoError(t, store.Save(ctx, "test/key/5", version))
	conflict := version
	conflict.Source = "group"
	require.ErrorIs(t, store.Save(ctx, "test/key/5", conflict), pricing.ErrVersionConflict)
	_, err = db.ExecContext(ctx, `UPDATE exact_price_versions SET source='changed' WHERE scope_key='test/key/5'`)
	require.ErrorContains(t, err, "immutable")
	other, err := store.At(ctx, "test/key/6", at)
	require.NoError(t, err)
	require.Empty(t, other)
	before, err := store.At(ctx, "test/key/5", at.Add(-2*time.Hour))
	require.NoError(t, err)
	require.Empty(t, before)

	const model = "m_0123456789abcdef0123456789abcdef"
	const unknown = "m_11111111111111111111111111111111"
	capability := catalog.Capability{Action: "text.chat", Protocol: "openai_chat", Tools: "unsupported"}
	d := catalog.Detail{Model: catalog.Summary{ID: model, Input: &r, PriceState: "priced", AccessState: "allowed"},
		Capabilities: []catalog.Capability{capability}, PriceSource: "published", PriceVersion: &version.ID}
	missing := catalog.Detail{Model: catalog.Summary{ID: unknown, AccessState: "allowed"}, Capabilities: []catalog.Capability{capability}}
	reader := testCatalog{catalog.Snapshot{Version: "cat_test", AsOf: at, Details: []catalog.Detail{d, missing}}}
	scope := catalog.Perspective{Actor: 4, Workspace: 1, Project: 2, AccessVersion: 3}
	public := catalog.Perspective{Workspace: 1}
	router := gin.New()
	wrap := func(p catalog.Perspective, call func(context.Context, catalog.Perspective, *gin.Context) (any, error)) gin.HandlerFunc {
		return func(c *gin.Context) {
			value, err := call(c.Request.Context(), p, c)
			if err != nil {
				var f *fault.Fault
				require.ErrorAs(t, err, &f)
				c.Status(f.Status)
				return
			}
			c.JSON(200, value)
		}
	}
	history := version
	history.ID = "pr_history"
	until := at
	history.EffectiveUntil = &until
	require.NoError(t, store.Save(ctx, quotes.PublicationScope(public, model), history))
	cmp := Service{Catalog: reader, Published: store}
	quote := quotes.Service{Catalog: reader, Keys: testKeys{store}}
	router.POST("/public/models/compare", wrap(public, cmp.Handle))
	router.POST("/projects/2/models/compare", wrap(scope, cmp.Handle))
	router.POST("/projects/2/quotes", wrap(scope, quote.Handle))
	post := func(path, body string, code int) string {
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
		require.Equal(t, code, resp.Code)
		return resp.Body.String()
	}
	for _, path := range []string{"/public/models/compare", "/projects/2/models/compare"} {
		var res Result
		out := post(path, `{"model_ids":["`+unknown+`","`+model+`"],"scenario":{"input_tokens":"1"}}`, 200)
		require.NoError(t, json.Unmarshal([]byte(out), &res))
		require.Equal(t, "cat_test", res.Version)
		require.Equal(t, unknown, res.Rows[0].ID)
		require.Nil(t, res.Rows[0].Quote.Charge)
		require.Nil(t, res.Rows[0].Metrics.Requests)
		require.Equal(t, "unknown", res.Rows[0].Capabilities["stream"])
		require.Equal(t, "unsupported", res.Rows[0].Capabilities["tools"])
		require.Equal(t, "0.00007813", res.Rows[1].Quote.Charge.String())
	}
	var historical Result
	hist := `{"model_ids":["` + model + `"],"price_at":"` + at.Add(-time.Minute).Format(time.RFC3339) + `","scenario":{"input_tokens":"1"}}`
	require.NoError(t, json.Unmarshal([]byte(post("/public/models/compare", hist, 200)), &historical))
	require.Equal(t, "pr_history", *historical.Rows[0].Quote.PriceVersion)
	require.NoError(t, json.Unmarshal([]byte(post("/projects/2/models/compare", hist, 200)), &historical))
	require.Nil(t, historical.Rows[0].Quote.PriceVersion)

	input, err := os.ReadFile("../../../../../contracts/api/BE-07/quote-input.json")
	require.NoError(t, err)
	expected, err := os.ReadFile("../../../../../contracts/api/BE-07/quote-result.json")
	require.NoError(t, err)
	require.JSONEq(t, string(expected), post("/projects/2/quotes", string(input), 200))
	keyed := func(id string) string { return strings.Replace(string(input), "{", `{"api_key_id":"`+id+`",`, 1) }
	require.JSONEq(t, string(expected), post("/projects/2/quotes", keyed("5"), 200))
	post("/projects/2/quotes", keyed("6"), 404)
	unpriced, err := os.ReadFile("../../../../../contracts/api/BE-07/quote-unpriced.json")
	require.NoError(t, err)
	require.JSONEq(t, string(unpriced), post("/projects/2/quotes", strings.ReplaceAll(string(input), model, unknown), 200))
}
