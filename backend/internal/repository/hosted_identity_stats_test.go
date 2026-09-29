package repository

import (
	"database/sql"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// Use a dedicated disposable database, never the shared usage statistics DSN.
func TestHostedIdentityStatisticsPreserveAmountsAndRowCount(t *testing.T) {
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
	_, err = transaction.Exec(`
		CREATE TEMP TABLE groups (id bigint, platform text);
		CREATE TEMP TABLE accounts (id bigint, platform text);
		CREATE TEMP TABLE usage_logs (id bigint, group_id bigint, account_id bigint, actual_cost numeric(20,10));
		INSERT INTO groups VALUES (1,'bedrock'),(2,'anthropic'),(3,'composite'),(4,'vertex');
		INSERT INTO accounts VALUES (1,'bedrock'),(2,'anthropic'),(3,'vertex'),(4,'gemini');
		INSERT INTO usage_logs VALUES (1,1,1,1.25),(2,2,2,2.5),(3,3,1,3.75),(4,4,3,4.125),(5,NULL,4,5.125);
	`)
	require.NoError(t, err)
	query := `SELECT ` + usageLogEffectivePlatformExpr + `, COUNT(*), SUM(ul.actual_cost)::text
		FROM usage_logs ul LEFT JOIN groups g ON g.id=ul.group_id LEFT JOIN accounts a ON a.id=ul.account_id
		GROUP BY ` + usageLogEffectivePlatformExpr + ` ORDER BY 1`
	readAmounts := func() {
		rows, queryErr := transaction.Query(query)
		require.NoError(t, queryErr)
		defer func() { _ = rows.Close() }()
		for _, expected := range []struct {
			platform string
			count    int
			amount   string
		}{
			{"anthropic", 3, "7.5000000000"}, {"gemini", 2, "9.2500000000"},
		} {
			require.True(t, rows.Next())
			var platform, amount string
			var count int
			require.NoError(t, rows.Scan(&platform, &count, &amount))
			require.Equal(t, expected.platform, platform)
			require.Equal(t, expected.count, count)
			require.Equal(t, expected.amount, amount)
		}
		require.False(t, rows.Next())
		require.NoError(t, rows.Err())
	}
	readAmounts()
	_, err = transaction.Exec(`UPDATE accounts SET platform=CASE platform WHEN 'bedrock' THEN 'anthropic' WHEN 'vertex' THEN 'gemini' ELSE platform END;
		UPDATE groups SET platform=CASE platform WHEN 'bedrock' THEN 'anthropic' WHEN 'vertex' THEN 'gemini' ELSE platform END;`)
	require.NoError(t, err)
	readAmounts()
}
