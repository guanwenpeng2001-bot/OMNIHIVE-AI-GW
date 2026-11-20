package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil/testresources"
	"github.com/stretchr/testify/require"
)

func accountExtraPostgresFixture(t *testing.T) (*sql.DB, *accountRepository) {
	t.Helper()
	dsn := os.Getenv("ACCOUNT_EXTRA_TEST_DSN")
	if dsn == "" {
		t.Skip("set ACCOUNT_EXTRA_TEST_DSN to an isolated local job_test PostgreSQL database")
	}
	// Validates the explicit loopback address and database name before connecting,
	// then owns and cleans up only a unique schema. No production migrations run.
	db := testresources.OpenSchema(t, dsn)
	_, err := db.Exec(`CREATE TABLE accounts (
		id bigserial PRIMARY KEY, platform text NOT NULL DEFAULT 'cursor', type text NOT NULL DEFAULT 'oauth',
		credentials jsonb, extra jsonb, deleted_at timestamptz, updated_at timestamptz,
		workspace_id bigint NOT NULL DEFAULT 1, proxy_id bigint, proxy_group_id bigint, proxy_fallback_origin_id bigint,
		temp_unschedulable_until timestamptz, temp_unschedulable_reason text,
		rate_limited_at timestamptz, rate_limit_reset_at timestamptz
	);
	CREATE TABLE scheduler_outbox (
		id bigserial PRIMARY KEY, event_type text, account_id bigint, group_id bigint, payload jsonb, dedup_key text
	);
	CREATE UNIQUE INDEX ON scheduler_outbox (dedup_key) WHERE dedup_key IS NOT NULL`)
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	return db, newAccountRepositoryWithSQL(client, db, nil)
}

func insertAccountExtraFixture(t *testing.T, db *sql.DB, extra any) int64 {
	t.Helper()
	var payload any
	if extra != nil {
		encoded, err := json.Marshal(extra)
		require.NoError(t, err)
		payload = string(encoded)
	}
	var id int64
	require.NoError(t, db.QueryRow(`INSERT INTO accounts (credentials, extra)
		VALUES ('{"access_token":"old","keep":"credential"}', $1::jsonb) RETURNING id`, payload).Scan(&id))
	return id
}

func readAccountExtraFixture(t *testing.T, db *sql.DB, id int64) map[string]any {
	t.Helper()
	var raw []byte
	require.NoError(t, db.QueryRow("SELECT extra FROM accounts WHERE id=$1", id).Scan(&raw))
	var extra map[string]any
	require.NoError(t, json.Unmarshal(raw, &extra))
	return extra
}

func writeAccountExtraFixture(ctx context.Context, repo *accountRepository, bulk bool, id int64, patch map[string]any) error {
	if bulk {
		_, err := repo.BulkUpdate(ctx, []int64{id}, service.AccountBulkUpdate{Extra: patch})
		return err
	}
	return repo.UpdateExtra(ctx, id, patch)
}

func TestAccountExtraSQLPostgresMergeAndRevisions(t *testing.T) {
	db, repo := accountExtraPostgresFixture(t)
	ctx := context.Background()
	for _, bulk := range []bool{false, true} {
		name := "single"
		if bulk {
			name = "bulk"
		}
		t.Run(name, func(t *testing.T) {
			id := insertAccountExtraFixture(t, db, map[string]any{
				"quota_used": 17, "cursor_usage_snapshot": "old", "credential_secret_revision": 1,
				"nested": map[string]any{"old": true}, "upstream_billing_probe": map[string]any{"status": "ok"},
				"upstream_model_catalog_state": map[string]any{"identity_revision": 7, "rules_revision": 11, "legacy_policy_revision": 13, "result": "keep"},
			})
			require.NoError(t, writeAccountExtraFixture(ctx, repo, bulk, id, map[string]any{
				"note": "new", "nullable": nil, "nested": map[string]any{"new": true}, "credential_secret_revision": 1,
			}))
			extra := readAccountExtraFixture(t, db, id)
			require.Equal(t, "old", extra["cursor_usage_snapshot"], "same identity preserves observations")
			require.Equal(t, map[string]any{"new": true}, extra["nested"], "merge remains shallow")
			require.Contains(t, extra, "nullable")
			require.Nil(t, extra["nullable"])
			state, ok := extra["upstream_model_catalog_state"].(map[string]any)
			require.True(t, ok)
			require.Equal(t, float64(11), state["rules_revision"])

			require.NoError(t, writeAccountExtraFixture(ctx, repo, bulk, id, map[string]any{
				"credential_secret_revision": 2, "mixed_scheduling": true, "upstream_billing_probe": nil,
				"upstream_model_catalog_state": map[string]any{"rules_revision": 999},
				"codex_fingerprint_seed":       "untrusted",
			}))
			extra = readAccountExtraFixture(t, db, id)
			require.Equal(t, float64(17), extra["quota_used"])
			require.Equal(t, "new", extra["note"])
			require.NotContains(t, extra, "cursor_usage_snapshot")
			require.NotContains(t, extra, "upstream_billing_probe", "explicit null removes the probe key")
			require.NotContains(t, extra, "codex_fingerprint_seed")
			require.Equal(t, map[string]any{"identity_revision": float64(7), "rules_revision": float64(12), "legacy_policy_revision": float64(14), "result": "keep"}, extra["upstream_model_catalog_state"])
			_, err := repo.ReplaceAccountModelSelectionScope(ctx, id, service.AccountModelCatalogBaseline{IdentityRevision: 7, RulesRevision: 11}, service.AccountModelAllowlist{}, nil)
			require.ErrorIs(t, err, service.ErrAccountModelCatalogConflict, "stale catalog configuration must still conflict")

			legacyID := insertAccountExtraFixture(t, db, nil)
			require.NoError(t, writeAccountExtraFixture(ctx, repo, bulk, legacyID, map[string]any{"note": "legacy"}))
			legacy := readAccountExtraFixture(t, db, legacyID)
			require.Equal(t, "legacy", legacy["note"])
			require.Equal(t, map[string]any{"identity_revision": float64(0), "rules_revision": float64(0), "legacy_policy_revision": float64(0)}, legacy["upstream_model_catalog_state"])
		})
	}
	t.Run("outbox_failure_rolls_back_all_columns", func(t *testing.T) {
		id := insertAccountExtraFixture(t, db, map[string]any{"note": "before"})
		_, err := db.Exec("ALTER TABLE scheduler_outbox ADD CONSTRAINT reject_insert CHECK (false) NOT VALID")
		require.NoError(t, err)
		defer func() {
			_, err := db.Exec("ALTER TABLE scheduler_outbox DROP CONSTRAINT reject_insert")
			require.NoError(t, err)
		}()
		for _, bulk := range []bool{false, true} {
			require.Error(t, writeAccountExtraFixture(ctx, repo, bulk, id, map[string]any{"note": "after", "mixed_scheduling": true}))
			require.Equal(t, map[string]any{"note": "before"}, readAccountExtraFixture(t, db, id))
		}
	})
	t.Run("deleted_and_missing_rows", func(t *testing.T) {
		id := insertAccountExtraFixture(t, db, map[string]any{"note": "before"})
		_, err := db.Exec("UPDATE accounts SET deleted_at=NOW() WHERE id=$1", id)
		require.NoError(t, err)
		require.ErrorIs(t, repo.UpdateExtra(ctx, id, map[string]any{"note": "after"}), service.ErrAccountNotFound)
		require.ErrorIs(t, repo.UpdateExtra(ctx, -1, map[string]any{"note": "after"}), service.ErrAccountNotFound)
		count, err := repo.BulkUpdate(ctx, []int64{id, -1}, service.AccountBulkUpdate{Extra: map[string]any{"note": "after"}})
		require.NoError(t, err)
		require.Zero(t, count)
		require.Equal(t, map[string]any{"note": "before"}, readAccountExtraFixture(t, db, id))
	})
}

func TestAccountExtraSQLPostgresFingerprintPerRow(t *testing.T) {
	db, repo := accountExtraPostgresFixture(t)
	const validSeed = "11111111-1111-4111-8111-111111111111"
	ids := []int64{
		insertAccountExtraFixture(t, db, map[string]any{"codex_fingerprint_seed": validSeed}),
		insertAccountExtraFixture(t, db, map[string]any{"codex_fingerprint_seed": "invalid"}),
		insertAccountExtraFixture(t, db, nil),
		insertAccountExtraFixture(t, db, nil),
	}
	_, err := db.Exec("UPDATE accounts SET platform='openai'")
	require.NoError(t, err)
	_, err = db.Exec("UPDATE accounts SET type='apikey' WHERE id=$1", ids[3])
	require.NoError(t, err)
	patch := service.AccountBulkUpdate{Extra: map[string]any{"codex_fingerprint_mode": "session"}, EnsureCodexFingerprintSeed: true}
	count, err := repo.BulkUpdate(context.Background(), ids, patch)
	require.NoError(t, err)
	require.Equal(t, int64(4), count)
	seeds := make([]string, 3)
	for i := range seeds {
		extra := readAccountExtraFixture(t, db, ids[i])
		seed, ok := extra["codex_fingerprint_seed"].(string)
		require.True(t, ok)
		seeds[i] = seed
		require.Regexp(t, codexFingerprintSeedCanonicalPattern, seeds[i])
		require.NotEqual(t, codexFingerprintNilSeed, seeds[i])
	}
	require.Equal(t, validSeed, seeds[0])
	require.NotEqual(t, seeds[1], seeds[2])
	require.NotContains(t, readAccountExtraFixture(t, db, ids[3]), "codex_fingerprint_seed")
	for i := range seeds {
		require.NoError(t, repo.UpdateExtra(context.Background(), ids[i], map[string]any{"codex_fingerprint_mode": "device"}))
		require.Equal(t, seeds[i], readAccountExtraFixture(t, db, ids[i])["codex_fingerprint_seed"], "single update preserves existing seed")
	}
}

func TestAccountExtraSQLPostgresBulkCredentialCleanup(t *testing.T) {
	db, repo := accountExtraPostgresFixture(t)
	ctx := context.Background()
	ids := make([]int64, 3)
	for i := range ids {
		ids[i] = insertAccountExtraFixture(t, db, map[string]any{
			"quota_used": 17, "note": "keep", "upstream_billing_probe": "old",
			"ollama_cloud_usage_session": "session", "ollama_cloud_usage_auto_refresh": true,
			"ollama_cloud_usage_snapshot": "snapshot",
		})
		_, err := db.Exec(`UPDATE accounts SET platform='openai', type='apikey', credentials='{
			"api_key":"new", "base_url":"https://www.ollama.com:443/v1", "keep":"credential",
			"billing_plan":"old", "model_mapping":{"alias":"target"}
		}'::jsonb WHERE id=$1`, ids[i])
		require.NoError(t, err)
	}
	_, err := db.Exec(`UPDATE accounts SET credentials=credentials || '{"api_key":"old"}'::jsonb WHERE id=$1`, ids[1])
	require.NoError(t, err)
	_, err = db.Exec("UPDATE accounts SET platform='cursor', type='oauth' WHERE id=$1", ids[2])
	require.NoError(t, err)
	count, err := repo.BulkUpdate(ctx, ids, service.AccountBulkUpdate{
		Credentials: map[string]any{"api_key": "new", "base_url": "https://ollama.com", "model_mapping": nil, "billing_plan": nil},
		Extra:       map[string]any{"upstream_billing_probe": nil}, EnsureCodexFingerprintSeed: true,
	})
	require.NoError(t, err)
	require.Equal(t, int64(3), count)
	for i, id := range ids {
		extra := readAccountExtraFixture(t, db, id)
		require.Equal(t, float64(17), extra["quota_used"])
		require.Equal(t, "keep", extra["note"])
		require.NotContains(t, extra, "upstream_billing_probe")
		require.NotContains(t, extra, "codex_fingerprint_seed", "non-OpenAI-OAuth rows must not receive seeds")
		require.Equal(t, true, extra["model_selection_baseline_frozen"])
		require.Equal(t, map[string]any{"enabled": true, "models": []any{}}, extra["upstream_model_allowlist"])
		if i == 1 {
			require.NotContains(t, extra, "ollama_cloud_usage_session")
			require.NotContains(t, extra, "ollama_cloud_usage_auto_refresh")
			require.NotContains(t, extra, "ollama_cloud_usage_snapshot")
		} else {
			require.Equal(t, "session", extra["ollama_cloud_usage_session"], "equivalent URLs and ineligible platforms preserve the session")
			require.Equal(t, "snapshot", extra["ollama_cloud_usage_snapshot"])
		}
		var raw []byte
		require.NoError(t, db.QueryRow("SELECT credentials FROM accounts WHERE id=$1", id).Scan(&raw))
		var credentials map[string]any
		require.NoError(t, json.Unmarshal(raw, &credentials))
		require.Equal(t, "credential", credentials["keep"])
		require.NotContains(t, credentials, "billing_plan")
		require.Contains(t, credentials, "model_mapping")
		require.Nil(t, credentials["model_mapping"])
	}
	_, err = db.Exec("UPDATE accounts SET proxy_id=9 WHERE id=$1", ids[0])
	require.NoError(t, err)
	clearProxy := int64(0)
	_, err = repo.BulkUpdate(ctx, ids[:1], service.AccountBulkUpdate{ProxyID: &clearProxy})
	require.NoError(t, err)
	extra := readAccountExtraFixture(t, db, ids[0])
	require.Equal(t, "session", extra["ollama_cloud_usage_session"])
	require.NotContains(t, extra, "ollama_cloud_usage_snapshot", "proxy change clears only the snapshot")
}

func TestAccountExtraSQLPostgresConcurrentMerge(t *testing.T) {
	db, repo := accountExtraPostgresFixture(t)
	for _, bulk := range []bool{false, true} {
		name := "single"
		if bulk {
			name = "bulk"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			id := insertAccountExtraFixture(t, db, map[string]any{"credential_secret_revision": 1, "quota_used": 17})
			writer, err := db.BeginTx(ctx, nil)
			require.NoError(t, err)
			defer func() { _ = writer.Rollback() }()
			var blocker int
			require.NoError(t, writer.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&blocker))
			_, err = writer.ExecContext(ctx, `UPDATE accounts SET extra = extra || '{
				"credential_secret_revision":2, "first":1, "mixed_scheduling":true, "cursor_usage_snapshot":"fresh",
				"upstream_model_catalog_state":{"identity_revision":0,"rules_revision":1,"legacy_policy_revision":1}
			}'::jsonb WHERE id=$1`, id)
			require.NoError(t, err)
			done := make(chan error, 1)
			go func() {
				done <- writeAccountExtraFixture(ctx, repo, bulk, id, map[string]any{
					"credential_secret_revision": 2, "second": 2, "openai_responses_mode": "force_chat_completions",
				})
			}()
			// Wait for the actual UPDATE lock wait, not a scheduling-dependent sleep.
			require.Eventually(t, func() bool {
				var blocked bool
				err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity
					WHERE datname=current_database() AND $1 = ANY(pg_blocking_pids(pid)))`, blocker).Scan(&blocked)
				return err == nil && blocked
			}, 5*time.Second, 10*time.Millisecond)
			require.NoError(t, writer.Commit())
			require.NoError(t, <-done)
			extra := readAccountExtraFixture(t, db, id)
			require.Equal(t, float64(1), extra["first"])
			require.Equal(t, float64(2), extra["second"])
			require.Equal(t, float64(17), extra["quota_used"])
			require.Equal(t, "fresh", extra["cursor_usage_snapshot"], "identity comparison must use the row after the lock wait")
			require.Equal(t, map[string]any{"identity_revision": float64(0), "rules_revision": float64(2), "legacy_policy_revision": float64(2)}, extra["upstream_model_catalog_state"])
		})
	}
}

func TestAccountExtraSQLPostgresPlanKeepsSubqueryStages(t *testing.T) {
	db, _ := accountExtraPostgresFixture(t)
	id := insertAccountExtraFixture(t, db, nil)
	query := captureAccountExtraSQL(t, false, func(repo *accountRepository) error {
		return repo.UpdateExtra(context.Background(), id, map[string]any{"codex_5h_used_percent": 17})
	})
	var plan string
	require.NoError(t, db.QueryRow("EXPLAIN (VERBOSE, FORMAT JSON) "+query, `{"codex_5h_used_percent":17}`, id).Scan(&plan))
	var explained []map[string]any
	require.NoError(t, json.Unmarshal([]byte(plan), &explained))
	projections, catalogProjections := 0, 0
	var inspect func(map[string]any)
	inspect = func(node map[string]any) {
		children, _ := node["Plans"].([]any)
		if node["Node Type"] == "Result" && len(children) == 0 {
			projections++
			output, err := json.Marshal(node["Output"])
			require.NoError(t, err)
			if strings.Contains(string(output), "jsonb_build_object(") {
				catalogProjections++
				require.Equal(t, 2, strings.Count(string(output), "jsonb_build_object("))
			}
		}
		for _, child := range children {
			childPlan, ok := child.(map[string]any)
			require.True(t, ok)
			inspect(childPlan)
		}
	}
	rootPlan, ok := explained[0]["Plan"].(map[string]any)
	require.True(t, ok)
	inspect(rootPlan)
	// PostgreSQL can remove a trivial Subquery Scan and VERBOSE deparses parent
	// Vars as their source expressions. Inspect the actual projection nodes,
	// not the size/count of expressions printed again in the parent Output.
	require.Contains(t, plan, `"Node Type": "Nested Loop"`)
	require.Equal(t, 2, projections, "merge and catalog patch must retain separate projections")
	require.Equal(t, 1, catalogProjections)
}
