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
		"user_affiliate_ledger", "usage_cleanup_tasks",
		"billing_movement_links", "billing_settlement_outbox", "billing_export_rows",
		"payment_adjustment_links", "billing_adjustment_effects",
		"billing_case_resolutions", "billing_commands",
		"payment_wallet_links", "payment_reversals",
		"payment_wallet_command_ids", "payment_order_history", "payment_provider_events",
		"payment_trade_claims", "payment_entitlement_events", "payment_order_terms",
		"payment_historical_trades", "payment_orders", "billing_wallet_events",
		"billing_funding_accounts",
		"task_media_attributes", "task_media_verifications",
		"omnihive_extension_action_grants",
		"byok_lifecycle_events", "byok_attempt_funding", "byok_operations", "byok_secret_versions",
		"byok_credentials", "byok_provider_classes", "byok_service_accounts", "byok_pilot_providers",
		"routing_candidate_snapshots", "routing_account_preference_owners",
		"routing_legacy_intents",
		"connection_pool_members", "connection_verifications", "connection_events",
		"connection_operations", "connection_previews", "connection_pools",
		"provider_connections", "connection_credential_versions", "connection_migration_quarantine",
		"account_secret_backfill", "account_secret_backfill_done", "credential_secrets", "api_key_credentials",
		"api_key_budget_periods", "api_key_budget_reservations", "api_key_budget_adjustments",
		"project_key_policies", "user_subscriptions", "user_allowed_groups", "api_keys",
		"redeem_codes",
		"identity_adoption_decisions", "auth_identity_channels", "auth_identities",
		"pending_auth_sessions", "announcement_reads", "promo_code_usages",
		"passkey_credentials", "passkey_user_handles", "user_affiliates",
		"user_attribute_values", "user_avatars", "user_group_rate_multipliers",
		"user_platform_quotas", "user_provider_default_grants", "project_grants", "project_access_requests",
		"mcp_grants", "mcp_clients", "registration_agreements", "workspace_members", "users",
		"scheduled_test_results", "scheduled_test_plans", "account_groups",
	}
	for _, table := range tables {
		_, err := tx.ExecContext(ctx, "ALTER TABLE "+pq.QuoteIdentifier(table)+" DISABLE TRIGGER USER")
		require.NoError(t, err, "disable fixture triggers for table %s", table)
	}
	// Bound the reviewed FK closure, including references that keep their rows
	// through SET NULL. A new dependent table requires an ownership review.
	parents := append(append([]string{}, tables...), "accounts", "groups")
	dependents := append(append([]string{}, parents...),
		"announcements", "content_moderation_logs", "prompt_audit_events", "prompt_audit_jobs",
		"redeem_codes", "sub2api_plugin_installations", "channel_monitors",
		"channel_groups", "composite_model_routes", "model_access_groups",
		"task_media_attributes", "task_media_verifications", "omnihive_extension_action_grants",
		"byok_lifecycle_events", "byok_provider_classes", "byok_service_accounts",
		"connection_pool_members", "connection_verifications", "connection_events",
		"connection_operations", "connection_previews", "connection_pools",
		"provider_connections", "connection_credential_versions", "connection_migration_quarantine",
		"project_key_policies", "project_grants")
	var unreviewed string
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT coalesce(string_agg(DISTINCT conrelid::regclass::text, ', '), '') FROM pg_constraint
 WHERE contype = 'f' AND confrelid = ANY($1::regclass[])
 AND NOT (conrelid = ANY($2::regclass[]))`, pq.Array(parents), pq.Array(dependents)).Scan(&unreviewed))
	require.Empty(t, unreviewed,
		"fixture cleanup has unreviewed foreign-key dependents: %s; review them and add to the delete order above", unreviewed)
	for _, table := range tables {
		_, err := tx.ExecContext(ctx, "DELETE FROM "+pq.QuoteIdentifier(table))
		require.NoError(t, err, "clear fixture table %s", table)
	}
	// Flush deferred foreign-key trigger events before re-enabling user triggers;
	// PostgreSQL rejects ALTER TABLE while a table still has pending events.
	_, err = tx.ExecContext(ctx, "SET CONSTRAINTS ALL IMMEDIATE")
	require.NoError(t, err, "flush deferred fixture constraints")
	for _, table := range tables {
		_, err := tx.ExecContext(ctx, "ALTER TABLE "+pq.QuoteIdentifier(table)+" ENABLE TRIGGER USER")
		require.NoError(t, err, "re-enable fixture triggers for table %s", table)
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
