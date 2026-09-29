package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// The fixture shadows table names with CTEs, leaving the production SELECT and
// its parameters unchanged. PostgreSQL evaluates the actual predicates and JSON
// projection without migrations, temporary tables, or database writes.
type catalogScopeFixture struct {
	*sql.DB
	accounts string
}

func (f catalogScopeFixture) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return f.DB.QueryContext(ctx, `WITH accounts AS (
 SELECT * FROM jsonb_to_recordset($5::jsonb) AS a(
 id bigint, platform text, type text, status text, schedulable boolean,
 parent_account_id bigint, expires_at timestamptz, auto_pause_on_expired boolean,
 temp_unschedulable_until timestamptz, credentials jsonb, extra jsonb, deleted_at timestamptz)
), account_groups(account_id, group_id) AS (VALUES (1::bigint, 3::bigint), (2, 3), (3, 4), (5, 3), (6, 3), (7, 3), (8, 3))
`+query, append(args, f.accounts)...)
}

func catalogScopeAccounts(t *testing.T) string {
	t.Helper()
	rows := make([]map[string]any, 0, 8)
	for i, platform := range []string{"openai", "anthropic", "openai", "openai", "openai", "openai", "openai", "composite"} {
		rows = append(rows, map[string]any{
			"id": i + 1, "platform": platform, "type": "apikey", "status": "active",
			"schedulable": true, "auto_pause_on_expired": false,
			"credentials": map[string]any{"api_key": "secret", "model_mapping": map[string]string{"alias": "native"}},
			"extra": map[string]any{service.UpstreamModelCatalogExtraKey: service.AccountModelCatalogSnapshot{
				Revision: 7, Complete: true, DiscoveryScope: "account", DiscoveredModels: []string{"native"},
				EligibleModels: []service.AccountCatalogModel{{ID: "native", Endpoints: []string{"responses", "messages"}, EvidenceKind: "native_account_catalog", EvidenceObservedAt: "2026-09-15T00:00:00Z"}},
			}},
		})
	}
	rows[4]["status"] = "disabled"
	rows[5]["schedulable"] = false
	rows[6]["deleted_at"] = "2026-09-01T00:00:00Z"
	data, err := json.Marshal(rows)
	require.NoError(t, err)
	return string(data)
}

func TestModelCatalogScopeQueryParameters(t *testing.T) {
	// Pin the complete production query, including the AND grouping that keeps
	// platform, persistent eligibility, and group isolation mandatory. ANY(NULL)
	// and ANY('{}') both match no rows; neither is an all-platform sentinel.
	const query = `SELECT a.id, a.platform, a.type, a.status, a.schedulable,
 a.parent_account_id, a.expires_at, a.auto_pause_on_expired, a.temp_unschedulable_until,
 COALESCE((SELECT jsonb_object_agg(key,value) FROM jsonb_each(CASE WHEN jsonb_typeof(a.credentials) = 'object' THEN a.credentials ELSE '{}'::jsonb END)
 WHERE key IN ('model_mapping','compact_model_mapping','api_protocol','account_mode','base_url','aws_region','aws_force_global','protocol_rules','api_base_urls','openai_capabilities')), '{}'::jsonb),
 COALESCE((SELECT jsonb_object_agg(key,value) FROM jsonb_each(CASE WHEN jsonb_typeof(a.extra) = 'object' THEN a.extra ELSE '{}'::jsonb END)
 WHERE key IN ('upstream_model_catalog','upstream_model_catalog_state','upstream_model_allowlist','upstream_model_metadata',
 'mixed_scheduling','anthropic_passthrough','openai_passthrough','openai_oauth_passthrough','openai_responses_supported','openai_responses_mode')), '{}'::jsonb)
 FROM accounts a WHERE a.deleted_at IS NULL AND
 (($1::bigint <> 0 AND a.id = $1) OR ($1::bigint = 0 AND a.status = 'active' AND a.schedulable
 AND a.platform = ANY($2::text[]) AND
 (($3::bigint IS NOT NULL AND EXISTS (SELECT 1 FROM account_groups ag WHERE ag.account_id = a.id AND ag.group_id = $3))
 OR ($3::bigint IS NULL AND ($4 OR NOT EXISTS (SELECT 1 FROM account_groups ag WHERE ag.account_id = a.id))))))
 ORDER BY a.id`
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := &accountRepository{sql: db}
	group := int64(3)
	for _, tc := range []struct {
		name      string
		platforms []string
		argument  any
	}{
		{"single", []string{service.PlatformOpenAI}, `{"openai"}`},
		{"multiple", []string{service.PlatformOpenAI, service.PlatformAnthropic}, `{"openai","anthropic"}`},
		{"all concrete", domain.ConcretePlatforms(), pq.Array(domain.ConcretePlatforms())},
		{"nil is SQL NULL", nil, nil},
		{"empty is SQL empty array", []string{}, "{}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, groupID := range []*int64{&group, nil} {
				for _, includeGrouped := range []bool{false, true} {
					mock.ExpectQuery(query).WithArgs(int64(0), tc.argument, groupID, includeGrouped).
						WillReturnRows(sqlmock.NewRows([]string{"id"}))
					accounts, err := repo.ListModelCatalogAccounts(context.Background(), groupID, tc.platforms, includeGrouped)
					require.NoError(t, err)
					require.Empty(t, accounts)
				}
			}
		})
	}
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestModelCatalogScopePostgres(t *testing.T) {
	dsn := os.Getenv("CATALOG_SCOPE_TEST_DSN")
	if dsn == "" {
		t.Skip("set CATALOG_SCOPE_TEST_DSN for read-only PostgreSQL CTE contract tests")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := &accountRepository{sql: catalogScopeFixture{DB: db, accounts: catalogScopeAccounts(t)}}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	group3, group4, missing := int64(3), int64(4), int64(99)
	for _, tc := range []struct {
		name           string
		group          *int64
		platforms      []string
		includeGrouped bool
		want           []int64
	}{
		{"single", &group3, []string{"openai"}, false, []int64{1}},
		{"multiple", &group3, []string{"openai", "anthropic"}, false, []int64{1, 2}},
		{"all concrete", &group3, domain.ConcretePlatforms(), false, []int64{1, 2}},
		{"nil matches none", &group3, nil, false, []int64{}},
		{"empty matches none", &group3, []string{}, false, []int64{}},
		{"nil remains empty when including grouped", nil, nil, true, []int64{}},
		{"empty remains empty when including grouped", nil, []string{}, true, []int64{}},
		{"different group", &group4, domain.ConcretePlatforms(), false, []int64{3}},
		{"group still isolated when including grouped", &group3, domain.ConcretePlatforms(), true, []int64{1, 2}},
		{"no group members", &missing, domain.ConcretePlatforms(), true, []int64{}},
		{"platform mismatch", &group3, []string{"gemini"}, false, []int64{}},
		{"ungrouped only", nil, domain.ConcretePlatforms(), false, []int64{4}},
		{"include grouped", nil, domain.ConcretePlatforms(), true, []int64{1, 2, 3, 4}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			accounts, err := repo.ListModelCatalogAccounts(ctx, tc.group, tc.platforms, tc.includeGrouped)
			require.NoError(t, err)
			ids := make([]int64, 0, len(accounts))
			for _, account := range accounts {
				ids = append(ids, account.ID)
				require.NotContains(t, account.Credentials, "api_key")
			}
			require.Equal(t, tc.want, ids)
		})
	}

	t.Run("query through composite resolver to public catalog", func(t *testing.T) {
		routes := catalogScopeRoutes{}
		resolver := service.NewCompositeRouteResolver(routes)
		gateway := service.NewGatewayService(repo, nil, nil, nil, nil, nil, nil, nil, nil,
			nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, resolver, nil, nil)
		// Explicit routes are the only public names for this fixture; ownership
		// ambiguity prevents implicit native/alias candidates from leaking out.
		resolver.SetModelOwnershipResolver(func(context.Context, int64, string) (service.CompositeModelOwnership, error) {
			return service.CompositeModelOwnership{Ambiguous: true}, nil
		})
		ids, err := gateway.CatalogModelIDs(ctx, &group3, service.PlatformComposite)
		require.NoError(t, err)
		require.Equal(t, []string{"company", "company-claude"}, ids)
		ids, err = gateway.CatalogModelIDs(ctx, &group4, service.PlatformComposite)
		require.NoError(t, err)
		require.Equal(t, []string{"company"}, ids, "Anthropic membership in another group must not grant visibility")
		ids, err = gateway.CatalogModelIDs(ctx, &missing, service.PlatformComposite)
		require.NoError(t, err)
		require.Empty(t, ids, "routes cannot grant visibility without eligible accounts in the group")
	})
}

type catalogScopeRoutes struct {
	service.CompositeModelRouteRepository
}

func (catalogScopeRoutes) ListByGroup(_ context.Context, groupID int64, _ bool) ([]service.CompositeModelRoute, error) {
	return []service.CompositeModelRoute{{ID: 1, GroupID: groupID, Enabled: true,
		MatchType: service.CompositeRouteMatchExact, PublicModel: "company", TargetPlatform: service.PlatformOpenAI,
		UpstreamModel: "native", Endpoint: service.CompositeRouteEndpointAny},
		{ID: 2, GroupID: groupID, Enabled: true,
			MatchType: service.CompositeRouteMatchExact, PublicModel: "company-claude", TargetPlatform: service.PlatformAnthropic,
			UpstreamModel: "native", Endpoint: service.CompositeRouteEndpointAny}}, nil
}
