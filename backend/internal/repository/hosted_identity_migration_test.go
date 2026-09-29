package repository

import (
	"database/sql"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHostedIdentityMigrationActivationAndRollback(t *testing.T) {
	dsn := os.Getenv("HOSTED_IDENTITY_TEST_DSN")
	if dsn == "" {
		t.Skip("HOSTED_IDENTITY_TEST_DSN is not configured")
	}
	database, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer func() { _ = database.Close() }()
	transaction, err := database.Begin()
	require.NoError(t, err)
	defer func() { _ = transaction.Rollback() }()
	_, err = transaction.Exec(`CREATE SCHEMA hosted_identity_test; SET LOCAL search_path TO hosted_identity_test;
		CREATE TABLE accounts (id bigint PRIMARY KEY, platform text, type text, extra jsonb);
		CREATE TABLE groups (id bigint PRIMARY KEY, platform text);
		CREATE TABLE composite_model_routes (id bigint PRIMARY KEY, target_platform text);
		CREATE TABLE user_platform_quotas (id bigint PRIMARY KEY, platform text, usage numeric);
		CREATE TABLE usage_logs (id bigint PRIMARY KEY, actual_cost numeric);
		INSERT INTO accounts VALUES (1,'bedrock','bedrock','{"quota_used":12.50}'),(2,'vertex','service_account','{}');
		INSERT INTO groups VALUES (1,'bedrock'),(2,'vertex');
		INSERT INTO composite_model_routes VALUES (1,'bedrock');
		INSERT INTO user_platform_quotas VALUES (1,'anthropic',5.25),(2,'bedrock',12.50);
		INSERT INTO usage_logs VALUES (1,17.75);
	`)
	require.NoError(t, err)
	migration, err := os.ReadFile("../../migrations/266_hosted_account_identity.sql")
	require.NoError(t, err)
	for range 2 {
		_, err = transaction.Exec(string(migration))
		require.NoError(t, err)
	}
	var platform, accountType string
	require.NoError(t, transaction.QueryRow(`SELECT platform FROM accounts WHERE id=1`).Scan(&platform))
	require.Equal(t, "bedrock", platform, "expand must not activate new writes")
	_, err = transaction.Exec(`INSERT INTO accounts VALUES (4,'bedrock','bedrock','{}')`)
	require.NoError(t, err)
	require.NoError(t, transaction.QueryRow(`SELECT platform FROM accounts WHERE id=4`).Scan(&platform))
	require.Equal(t, "bedrock", platform, "new writes remain legacy until fleet activation")
	for range 2 {
		_, err = transaction.Exec(`SELECT activate_hosted_account_identity()`)
		require.NoError(t, err)
	}
	require.NoError(t, transaction.QueryRow(`SELECT platform,account_type FROM groups WHERE id=1`).Scan(&platform, &accountType))
	require.Equal(t, "anthropic", platform)
	require.Equal(t, "bedrock", accountType)
	require.NoError(t, transaction.QueryRow(`SELECT target_platform,account_type FROM composite_model_routes WHERE id=1`).Scan(&platform, &accountType))
	require.Equal(t, "anthropic", platform)
	require.Equal(t, "bedrock", accountType)
	require.NoError(t, transaction.QueryRow(`SELECT platform,type FROM accounts WHERE id=2`).Scan(&platform, &accountType))
	require.Equal(t, "gemini", platform)
	require.Equal(t, "service_account", accountType)
	var mappingCount int
	require.NoError(t, transaction.QueryRow(`SELECT COUNT(*) FROM hosted_identity_migration_mapping`).Scan(&mappingCount))
	require.Equal(t, 7, mappingCount)
	var amount string
	require.NoError(t, transaction.QueryRow(`SELECT SUM(usage)::text FROM user_platform_quotas`).Scan(&amount))
	require.Equal(t, "17.75", amount)
	_, err = transaction.Exec(`INSERT INTO accounts VALUES (3,'bedrock','bedrock','{}');
		INSERT INTO groups(id,platform) VALUES (3,'bedrock');
        INSERT INTO composite_model_routes(id,target_platform) VALUES (3,'bedrock');
        UPDATE groups SET account_type='' WHERE id=1;
		UPDATE accounts SET extra=extra||'{"quota_used":15.50}'::jsonb WHERE id=1;
		SELECT rollback_hosted_account_identity(); SELECT rollback_hosted_account_identity();`)
	require.NoError(t, err)
	require.NoError(t, transaction.QueryRow(`SELECT platform FROM accounts WHERE id=3`).Scan(&platform))
	require.Equal(t, "anthropic", platform, "rollback must not touch newly created accounts")
	require.NoError(t, transaction.QueryRow(`SELECT platform FROM groups WHERE id=1`).Scan(&platform))
	require.Equal(t, "anthropic", platform, "rollback must not overwrite a changed restriction")
	require.NoError(t, transaction.QueryRow(`SELECT platform,extra->>'quota_used' FROM accounts WHERE id=1`).Scan(&platform, &amount))
	require.Equal(t, "bedrock", platform)
	require.Equal(t, "15.50", amount, "rollback must preserve consumption since activation")
	require.NoError(t, transaction.QueryRow(`SELECT actual_cost::text FROM usage_logs WHERE id=1`).Scan(&amount))
	require.Equal(t, "17.75", amount)
	require.NoError(t, transaction.QueryRow(`SELECT platform FROM groups WHERE id=3`).Scan(&platform))
	require.Equal(t, "anthropic", platform, "rollback excludes newly created groups")
	require.NoError(t, transaction.QueryRow(`SELECT target_platform FROM composite_model_routes WHERE id=3`).Scan(&platform))
	require.Equal(t, "anthropic", platform, "rollback excludes newly created routes")
	require.NoError(t, transaction.QueryRow(`SELECT target_platform,account_type FROM composite_model_routes WHERE id=1`).Scan(&platform, &accountType))
	require.Equal(t, "bedrock", platform)
	require.Empty(t, accountType)
	_, err = transaction.Exec(`SELECT activate_hosted_account_identity()`)
	require.NoError(t, err)
	require.NoError(t, transaction.QueryRow(`SELECT platform FROM accounts WHERE id=1`).Scan(&platform))
	require.Equal(t, "bedrock", platform, "one-time activation cannot replay after rollback")
}
