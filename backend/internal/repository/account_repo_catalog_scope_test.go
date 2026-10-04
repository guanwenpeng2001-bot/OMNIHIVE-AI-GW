package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
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
	// The ordinary query has four parameters; the authorized-groups query has
	// five; access/generation reads have zero or one. Append the fixture after
	// every shape without replacing any argument or leaving parameter gaps.
	fixtureSQL := fmt.Sprintf(`WITH accounts AS (
 SELECT * FROM jsonb_to_recordset($%d::jsonb) AS a(
 id bigint, platform text, type text, status text, schedulable boolean,
 parent_account_id bigint, expires_at timestamptz, auto_pause_on_expired boolean,
 temp_unschedulable_until timestamptz, workspace_id bigint, proxy_id bigint, proxy_group_id bigint, credentials jsonb, extra jsonb, deleted_at timestamptz)
), account_groups(account_id, group_id) AS (VALUES (1::bigint, 3::bigint), (2, 3), (3, 4), (5, 3), (6, 3), (7, 3), (8, 3), (1, 5), (1, 6), (1, 7), (1, 8)),
groups(id, platform, account_type) AS (VALUES
 (3::bigint, 'composite'::text, ''::text), (4, 'composite', NULL),
 (5, 'openai', 'oauth'), (6, 'bedrock', ''), (7, 'vertex', NULL), (8, 'bedrock', 'apikey')),
model_catalog_generations(scope, generation) AS (VALUES ('catalog'::text, 1::bigint)),
model_access_rules AS (
 SELECT NULL::bigint AS account_id, NULL::bigint AS api_key_id,
 NULL::bigint AS access_group_id, NULL::bigint AS workspace_id,
 NULL::jsonb AS models, '[]'::jsonb AS exclude WHERE FALSE),
model_access_groups AS (
 SELECT NULL::bigint AS id, NULL::bigint AS workspace_id, NULL::jsonb AS models WHERE FALSE)
`, len(args)+1)
	// #nosec G701 -- Only repository-generated SELECTs reach this read-only CTE fixture; account data is bound as JSON, and the sole formatted value is an integer parameter index.
	return f.DB.QueryContext(ctx, fixtureSQL+query, append(args, f.accounts)...)
}

func catalogScopeAccounts(t *testing.T) string {
	t.Helper()
	rows := make([]map[string]any, 0, 8)
	for i, platform := range []string{"openai", "anthropic", "openai", "openai", "openai", "openai", "openai", "composite"} {
		rows = append(rows, map[string]any{
			"id": i + 1, "workspace_id": 1, "platform": platform, "type": "apikey", "status": "active",
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
	// platform, persistent eligibility, and group isolation mandatory. Batch-12
	// normalizes nil to an empty array and treats cardinality 0 as no platform
	// restriction; composite entry still passes the explicit ConcretePlatforms.
	const query = `SELECT a.id, a.platform, a.type, a.status, a.schedulable,
 a.parent_account_id, a.expires_at, a.auto_pause_on_expired, a.temp_unschedulable_until, a.workspace_id, a.proxy_id, a.proxy_group_id,
 COALESCE((SELECT jsonb_object_agg(key,value) FROM jsonb_each(CASE WHEN jsonb_typeof(a.credentials) = 'object' THEN a.credentials ELSE '{}'::jsonb END)
 WHERE key IN ('model_mapping','compact_model_mapping','api_protocol','account_mode','base_url','aws_region','aws_force_global','protocol_rules','api_base_urls','openai_capabilities')), '{}'::jsonb),
 COALESCE((SELECT jsonb_object_agg(key,value) FROM jsonb_each(CASE WHEN jsonb_typeof(a.extra) = 'object' THEN a.extra ELSE '{}'::jsonb END)
 WHERE key IN ('upstream_model_catalog','upstream_model_catalog_state','upstream_model_allowlist','upstream_model_metadata','model_availability',
 'hosted_bedrock_runtime','mixed_scheduling','anthropic_passthrough','openai_passthrough','openai_oauth_passthrough','openai_responses_supported','openai_responses_mode')), '{}'::jsonb)
 FROM accounts a WHERE TRUE AND a.deleted_at IS NULL AND (
 (($1::bigint <> 0 AND a.id = $1) OR ($1::bigint = 0 AND a.status = 'active' AND a.schedulable
 AND (cardinality($2::text[]) = 0 OR a.platform = ANY($2::text[])
 OR (a.platform = 'bedrock' AND a.type = 'bedrock' AND 'anthropic' = ANY($2::text[]))
 OR (a.platform = 'anthropic' AND a.type = 'bedrock' AND 'bedrock' = ANY($2::text[]))
 OR (a.platform = 'vertex' AND a.type = 'service_account' AND 'gemini' = ANY($2::text[]))
 OR (a.platform = 'gemini' AND a.type = 'service_account' AND 'vertex' = ANY($2::text[])))
 AND ($3::bigint IS NULL OR EXISTS (SELECT 1 FROM groups requested_group WHERE requested_group.id = $3 AND (COALESCE(NULLIF(requested_group.account_type,''), CASE requested_group.platform WHEN 'bedrock' THEN 'bedrock' WHEN 'vertex' THEN 'service_account' ELSE '' END) IN ('',a.type)))) AND
 (($3::bigint IS NOT NULL AND EXISTS (SELECT 1 FROM account_groups ag WHERE ag.account_id = a.id AND ag.group_id = $3))
 OR ($3::bigint IS NULL AND ($4 OR NOT EXISTS (SELECT 1 FROM account_groups ag WHERE ag.account_id = a.id))))))
 ) ORDER BY a.id`
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
		{"nil normalizes to empty array", nil, "{}"},
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
	oauthGroup, bedrockGroup, vertexGroup, explicitTypeGroup := int64(5), int64(6), int64(7), int64(8)
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
		{"nil leaves platform unrestricted", &group3, nil, false, []int64{1, 2, 8}},
		{"empty leaves platform unrestricted", &group3, []string{}, false, []int64{1, 2, 8}},
		{"nil includes grouped without platform restriction", nil, nil, true, []int64{1, 2, 3, 4, 8}},
		{"empty includes grouped without platform restriction", nil, []string{}, true, []int64{1, 2, 3, 4, 8}},
		{"nil preserves ungrouped isolation", nil, nil, false, []int64{4}},
		{"empty preserves ungrouped isolation", nil, []string{}, false, []int64{4}},
		{"different group", &group4, domain.ConcretePlatforms(), false, []int64{3}},
		{"group still isolated when including grouped", &group3, domain.ConcretePlatforms(), true, []int64{1, 2}},
		{"no group members", &missing, domain.ConcretePlatforms(), true, []int64{}},
		{"platform mismatch", &group3, []string{"gemini"}, false, []int64{}},
		{"explicit account type mismatch", &oauthGroup, nil, true, []int64{}},
		{"legacy bedrock account type mismatch", &bedrockGroup, nil, true, []int64{}},
		{"legacy vertex account type mismatch", &vertexGroup, nil, true, []int64{}},
		{"explicit type overrides hosted default", &explicitTypeGroup, []string{"openai"}, false, []int64{1}},
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

	t.Run("authorized groups use fifth production parameter", func(t *testing.T) {
		for _, tc := range []struct {
			name   string
			groups []int64
			want   []int64
		}{
			{"one group", []int64{3}, []int64{1, 2, 8}},
			{"other group", []int64{4}, []int64{3}},
			{"multiple groups deduplicate", []int64{4, 3, 8}, []int64{1, 2, 3, 8}},
			{"unauthorized group", []int64{99}, []int64{}},
			{"no authorized groups", nil, []int64{}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				accounts, err := repo.ListModelCatalogAccountsForGroups(ctx, tc.groups)
				require.NoError(t, err)
				ids := make([]int64, 0, len(accounts))
				for _, account := range accounts {
					ids = append(ids, account.ID)
					require.NotContains(t, account.Credentials, "api_key")
					wantGroups := []int64{3}
					if account.ID == 3 {
						wantGroups = []int64{4}
					} else if account.ID == 1 && len(tc.groups) > 1 {
						wantGroups = []int64{3, 8}
					}
					require.Equal(t, wantGroups, account.GroupIDs, "project only authorized memberships")
				}
				require.Equal(t, tc.want, ids)
			})
		}
	})

	t.Run("single account evidence uses original id parameter", func(t *testing.T) {
		// Direct evidence reads intentionally include disabled/unschedulable
		// accounts; public listing eligibility remains enforced above.
		for _, id := range []int64{1, 5, 6} {
			account, err := repo.GetModelCatalogAccount(ctx, id)
			require.NoError(t, err)
			require.Equal(t, id, account.ID)
			require.NotContains(t, account.Credentials, "api_key")
		}
		for _, id := range []int64{7, 99} {
			account, err := repo.GetModelCatalogAccount(ctx, id)
			require.ErrorIs(t, err, service.ErrAccountNotFound)
			require.Nil(t, account)
		}
	})

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
		ids, err = gateway.CatalogModelIDs(service.WithVisibleModelKeyPolicy(ctx, []string{"company"}), &group3, service.PlatformComposite)
		require.NoError(t, err)
		require.Equal(t, []string{"company"}, ids, "Key policy still narrows eligible routes")
		ids, err = gateway.CatalogModelIDs(service.WithVisibleModelKeyPolicy(ctx, []string{}), &group3, service.PlatformComposite)
		require.NoError(t, err)
		require.Empty(t, ids, "explicit empty Key policy denies every route")
	})
}

type catalogScopeRoutes struct {
	service.CompositeModelRouteRepository
}

func (catalogScopeRoutes) ListByGroup(_ context.Context, groupID int64, _ bool) ([]service.CompositeModelRoute, error) {
	// Composite targets enter the account mapping as inputs: the fixture selects
	// alias -> native, so routing straight to native would bypass that whitelist.
	return []service.CompositeModelRoute{{ID: 1, GroupID: groupID, Enabled: true,
		MatchType: service.CompositeRouteMatchExact, PublicModel: "company", TargetPlatform: service.PlatformOpenAI,
		UpstreamModel: "alias", Endpoint: service.CompositeRouteEndpointAny},
		{ID: 2, GroupID: groupID, Enabled: true,
			MatchType: service.CompositeRouteMatchExact, PublicModel: "company-claude", TargetPlatform: service.PlatformAnthropic,
			UpstreamModel: "alias", Endpoint: service.CompositeRouteEndpointAny},
		{ID: 3, GroupID: groupID, Enabled: true,
			MatchType: service.CompositeRouteMatchExact, PublicModel: "unselected-target", TargetPlatform: service.PlatformOpenAI,
			UpstreamModel: "unselected", Endpoint: service.CompositeRouteEndpointAny}}, nil
}
