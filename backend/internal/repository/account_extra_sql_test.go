package repository

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

// Capture SQL at the repository boundary so adding a new wrapper at a call
// site cannot silently bypass the size/structure regression checks.
func captureAccountExtraSQL(t *testing.T, bulk bool, write func(*accountRepository) error) string {
	t.Helper()
	var query string
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherFunc(func(expected, actual string) error {
		if strings.HasPrefix(actual, "UPDATE accounts SET ") {
			query = actual
		}
		return sqlmock.QueryMatcherRegexp.Match(expected, actual)
	})))
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	if bulk {
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT platform, credentials FROM accounts").
			WillReturnRows(sqlmock.NewRows([]string{"platform", "credentials"}))
		mock.ExpectQuery("SELECT workspace_id, proxy_id, proxy_group_id FROM accounts").
			WillReturnRows(sqlmock.NewRows([]string{"workspace_id", "proxy_id", "proxy_group_id"}))
	}
	mock.ExpectExec("UPDATE accounts SET").WillReturnResult(sqlmock.NewResult(0, 1))
	if bulk {
		mock.ExpectExec("INSERT INTO scheduler_outbox").WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()
	}
	require.NoError(t, write(newAccountRepositoryWithSQL(client, db, nil)))
	require.NoError(t, mock.ExpectationsWereMet())
	require.NotEmpty(t, query)
	return query
}

func requireBoundedAccountExtraSQL(t *testing.T, query string) {
	t.Helper()
	t.Logf("SQL bytes=%d CASE=%d COALESCE=%d DISTINCT=%d", len(query), strings.Count(query, "CASE"), strings.Count(query, "COALESCE"), strings.Count(query, "IS DISTINCT FROM"))
	// The observed single-account incident was 82,923 bytes / 37 CASEs /
	// 311 COALESCEs / 665 DISTINCTs; the bulk path amplified it further.
	require.Less(t, len(query), 24*1024)
	require.Less(t, strings.Count(query, "CASE"), 20)
	require.Less(t, strings.Count(query, "COALESCE"), 100)
	require.Less(t, strings.Count(query, "IS DISTINCT FROM"), 120)
	require.Equal(t, 1, strings.Count(query, "AS merged_extra"), "merge once per target row")
	require.Equal(t, 1, strings.Count(query, "AS catalog_extra"), "revision patch once per target row")
	require.Contains(t, query, "CROSS JOIN LATERAL")
	require.Contains(t, query, "OFFSET 0")
	require.NotContains(t, query, "MATERIALIZED", "CTE tuplestores survive the concurrent-update recheck")
	require.NotContains(t, query, "FROM accounts", "do not pre-read a stale target snapshot")
	require.NotContains(t, query, "JOIN accounts")
}

func TestUpdateExtraSQLDoesNotExpandIntermediateExpressions(t *testing.T) {
	ctx := context.Background()
	// This observation-only path is the production hot path and has no outbox.
	query := captureAccountExtraSQL(t, false, func(repo *accountRepository) error {
		return repo.UpdateExtra(ctx, 42, map[string]any{"codex_5h_used_percent": 17})
	})
	requireBoundedAccountExtraSQL(t, query)
	require.Equal(t, 1, strings.Count(query, "|| $1::jsonb"))
	require.True(t, strings.HasSuffix(query, "WHERE id = $2 AND deleted_at IS NULL"))

	// Payload size must remain in parameters, not expand the statement text.
	large := map[string]any{}
	for i := 0; i < 1000; i++ {
		large[fmt.Sprintf("codex_primary_%d", i)] = strings.Repeat("x", 256)
	}
	require.Equal(t, query, captureAccountExtraSQL(t, false, func(repo *accountRepository) error {
		return repo.UpdateExtra(ctx, 42, large)
	}))
}

func TestBulkUpdateExtraSQLDoesNotExpandIntermediateExpressions(t *testing.T) {
	ctx := context.Background()
	proxy := int64(0)
	patch := service.AccountBulkUpdate{
		ProxyID: &proxy,
		Credentials: map[string]any{
			"api_key": "new", "base_url": "https://ollama.com", "model_mapping": nil,
		},
		Extra: map[string]any{
			"credential_secret_revision": 2, "codex_fingerprint_mode": "session",
			"upstream_billing_probe": nil, "ollama_cloud_usage_snapshot": nil,
		},
		EnsureCodexFingerprintSeed: true,
	}
	var previous string
	for _, count := range []int{1, 1000, 10000} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			ids := make([]int64, count)
			for i := range ids {
				ids[i] = int64(i + 1)
			}
			query := captureAccountExtraSQL(t, true, func(repo *accountRepository) error {
				_, err := repo.BulkUpdate(ctx, ids, patch)
				return err
			})
			requireBoundedAccountExtraSQL(t, query)
			require.Equal(t, 1, strings.Count(query, "gen_random_uuid()"), "one seed generation per row")
			require.Equal(t, 1, strings.Count(query, "AS opencode_identity_changed"))
			require.Equal(t, 1, strings.Count(query, "AS runtime_extra"))
			require.Regexp(t, regexp.MustCompile(`WHERE id = ANY\(\$\d+\) AND deleted_at IS NULL$`), query)
			if previous != "" {
				require.Equal(t, previous, query, "batch size belongs in the array parameter")
			}
			previous = query
		})
	}
}
