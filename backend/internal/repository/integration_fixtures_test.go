//go:build integration

package repository

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// prepareRepositoryFixtures owns disposable business rows in the isolated test
// database, not migration/configuration records. Register it BEFORE testEntTx so
// LIFO cleanup rolls transactions back before deleting rows. Concurrent tests
// must join their own goroutines before returning, as the UserRepo tests do.
func prepareRepositoryFixtures(t *testing.T) {
	t.Helper()
	cleanupRepositoryRows(t)
	rows, err := integrationDB.Query("SELECT id FROM groups")
	require.NoError(t, err)
	baselineGroups := make([]int64, 0)
	for rows.Next() {
		var id int64
		require.NoError(t, rows.Scan(&id))
		baselineGroups = append(baselineGroups, id)
	}
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())
	t.Cleanup(func() {
		cleanupRepositoryRows(t)
		// Preserve every pre-existing group, including actual default records, without
		// assuming a sequence value or translating a display name into an identity.
		_, err := integrationDB.Exec("DELETE FROM groups WHERE NOT (id = ANY($1))", pq.Array(baselineGroups))
		require.NoError(t, err, "remove only groups created by this fixture")
	})
}

func cleanupRepositoryRows(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	tx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { require.NoError(t, rollbackFixtureTx(tx), "rollback fixture cleanup") }()
	// Explicit dependency order, reviewed against pg_constraint. No schema scan or
	// TRUNCATE CASCADE: new restrictive dependencies fail here instead of silently
	// broadening the set. These children all belong to disposable users/accounts.
	// model_access_rules/groups come first: their change trigger writes scheduler_outbox.
	tables := []string{
		"model_access_rules", "model_access_groups",
		"upstream_jobs", "billing_usage_entries", "usage_logs",
		"usage_dashboard_hourly_users", "usage_dashboard_daily_users",
		"usage_dashboard_hourly", "usage_dashboard_daily", "scheduler_outbox",
		"user_affiliate_ledger", "payment_orders", "usage_cleanup_tasks",
		"user_subscriptions", "user_allowed_groups", "api_keys",
		"identity_adoption_decisions", "auth_identity_channels", "auth_identities",
		"pending_auth_sessions", "announcement_reads", "promo_code_usages",
		"passkey_credentials", "passkey_user_handles", "user_affiliates",
		"user_attribute_values", "user_avatars", "user_group_rate_multipliers",
		"user_platform_quotas", "user_provider_default_grants", "workspace_members", "users",
		"scheduled_test_results", "scheduled_test_plans", "account_groups",
	}
	// Bound the reviewed FK closure, including references that keep their rows
	// through SET NULL. A new dependent table requires an ownership review.
	parents := append(append([]string{}, tables...), "accounts", "groups")
	dependents := append(append([]string{}, parents...),
		"announcements", "content_moderation_logs", "prompt_audit_events", "prompt_audit_jobs",
		"redeem_codes", "sub2api_plugin_installations", "channel_monitors",
		"channel_groups", "composite_model_routes", "model_access_groups")
	var unreviewed int
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT count(*) FROM pg_constraint
 WHERE contype = 'f' AND confrelid = ANY($1::regclass[])
 AND NOT (conrelid = ANY($2::regclass[]))`, pq.Array(parents), pq.Array(dependents)).Scan(&unreviewed))
	require.Zero(t, unreviewed, "fixture cleanup has unreviewed foreign-key dependents")
	for _, table := range tables {
		_, err := tx.ExecContext(ctx, "DELETE FROM "+pq.QuoteIdentifier(table))
		require.NoError(t, err, "clear fixture table %s", table)
	}
	// ON DELETE SET NULL references in announcements, moderation/audit records,
	// redeem codes, plugin installations and channel monitors retain their records.
	// The restrictive account parent edge must be released before deleting parents.
	_, err = tx.ExecContext(ctx, "UPDATE accounts SET parent_account_id = NULL WHERE parent_account_id IS NOT NULL")
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, "DELETE FROM accounts")
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
}

func rollbackFixtureTx(tx *sql.Tx) error {
	err := tx.Rollback()
	if errors.Is(err, sql.ErrTxDone) {
		return nil
	}
	return err
}
