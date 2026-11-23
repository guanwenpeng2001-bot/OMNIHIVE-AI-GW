//go:build unit

package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSettingAuthorityAndDefaultsMigrationIsAdditive(t *testing.T) {
	content, err := FS.ReadFile("341_setting_authority_and_defaults.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))
	require.Contains(t, sql, "create table if not exists setting_overrides")
	require.Contains(t, sql, "on conflict (key) do nothing")
	require.Contains(t, sql, "on conflict (setting_key) do nothing")
	require.Contains(t, sql, "('model_plaza_enabled', 'false')")
	require.Contains(t, sql, "('default_concurrency', '5')")
	require.NotContains(t, sql, "update settings")
	require.NotContains(t, sql, "delete from settings")
}
