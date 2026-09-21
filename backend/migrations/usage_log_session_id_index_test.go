package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUsageLogSessionIDIndexMigration(t *testing.T) {
	content, err := FS.ReadFile("273_add_usage_log_session_id_index_notx.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_usage_logs_session_id")
	require.Contains(t, sql, "ON usage_logs (session_id)")
	require.Contains(t, sql, "WHERE session_id IS NOT NULL")
}
