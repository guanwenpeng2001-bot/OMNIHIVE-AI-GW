package repository

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

// This rollback restores only the imported Cursor entry. A subsequent Cursor
// edit is a conflict; unrelated platform edits remain intact. Keep the receipt
// (marked rolled_back) so reapplying the forward migration cannot re-enable it.
const rollbackCursorInventory = `DO $$
DECLARE
    config_row channel_monitor_v2_config%ROWTYPE;
    current_cursor JSONB;
    restored_platforms JSONB;
BEGIN
    SELECT * INTO config_row FROM channel_monitor_v2_config WHERE id = 1 FOR UPDATE;
    IF config_row.cursor_legacy_import IS NULL OR config_row.cursor_legacy_import->>'rolled_back' = 'true' THEN RETURN; END IF;
    SELECT entry INTO current_cursor FROM jsonb_array_elements(config_row.platforms) AS entry WHERE entry->>'platform' = 'cursor';
    IF current_cursor IS DISTINCT FROM config_row.cursor_legacy_import->'after' THEN
        RAISE EXCEPTION 'Cursor configuration changed after import; reconcile Cursor changes before rollback';
    END IF;
    SELECT COALESCE(jsonb_agg(
        CASE WHEN entry->>'platform' = 'cursor' THEN config_row.cursor_legacy_import->'before' ELSE entry END ORDER BY position
    ) FILTER (WHERE entry->>'platform' <> 'cursor' OR config_row.cursor_legacy_import->'before' <> 'null'::jsonb), '[]'::jsonb)
    INTO restored_platforms FROM jsonb_array_elements(config_row.platforms) WITH ORDINALITY AS entries(entry, position);
    UPDATE channel_monitor_v2_config SET platforms = restored_platforms, version = version + 1, updated_at = NOW(),
        cursor_legacy_import = cursor_legacy_import || '{"rolled_back":true}'::jsonb WHERE id = 1;
END $$;`

func TestCursorPassiveInventoryPostgres(test *testing.T) {
	connectionString := os.Getenv("CURSOR_MONITOR_TEST_POSTGRES_DSN")
	if connectionString == "" {
		test.Skip("set CURSOR_MONITOR_TEST_POSTGRES_DSN for disposable PostgreSQL tests")
	}
	adminDatabase, err := sql.Open("postgres", connectionString)
	require.NoError(test, err)
	defer adminDatabase.Close()
	databaseName := fmt.Sprintf("cursor_monitor_test_%d", time.Now().UnixNano())
	_, err = adminDatabase.Exec(`CREATE DATABASE "` + databaseName + `"`)
	require.NoError(test, err)
	defer func() {
		_, cleanupError := adminDatabase.Exec(`DROP DATABASE "` + databaseName + `" WITH (FORCE)`)
		require.NoError(test, cleanupError)
	}()
	parsedURL, err := url.Parse(connectionString)
	require.NoError(test, err)
	parsedURL.Path = "/" + databaseName
	database, err := sql.Open("postgres", parsedURL.String())
	require.NoError(test, err)
	defer database.Close()
	contextValue, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	require.NoError(test, ApplyMigrations(contextValue, database))
	migration, err := migrations.FS.ReadFile("257_cursor_passive_monitor_inventory.sql")
	require.NoError(test, err)
	var upstreamCalls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		upstreamCalls.Add(1)
		writer.WriteHeader(http.StatusInternalServerError)
	}))
	defer upstream.Close()

	for _, scenario := range []struct {
		name            string
		platforms       string
		expectedEnabled bool
	}{
		{"missing Cursor", `[{"platform":"openai","enabled":true,"models":["existing"]}]`, false},
		{"explicitly disabled Cursor", `[{"platform":"cursor","enabled":false,"models":["chosen"]}]`, false},
		{"existing passive Cursor", `[{"platform":"cursor","enabled":true,"models":["chosen"]}]`, true},
		{"all models remains unrestricted", `[{"platform":"cursor","enabled":false,"models":[]}]`, false},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			_, err := database.ExecContext(contextValue, `DELETE FROM channel_monitors; DELETE FROM channel_monitor_request_templates;`)
			require.NoError(test, err)
			_, err = database.ExecContext(contextValue, `UPDATE channel_monitor_v2_config SET platforms=$1, cursor_legacy_import=NULL, enabled=false, version=10`, scenario.platforms)
			require.NoError(test, err)
			_, err = database.ExecContext(contextValue, `INSERT INTO channel_monitors(name, provider, endpoint, api_key_encrypted, primary_model, extra_models, enabled, interval_seconds, created_by)
VALUES ('disabled legacy', 'cursor', $1, 'never-decrypt', ' legacy-primary ', '["legacy-extra", "legacy-primary", ""]', false, 60, 1),
('enabled legacy', 'cursor', $1, 'never-decrypt', 'legacy-primary', '[]', true, 60, 1);
`, upstream.URL)
			require.NoError(test, err)
			_, err = database.ExecContext(contextValue, `INSERT INTO channel_monitor_request_templates(name, provider, body_override_mode, body_override)
VALUES ('legacy template','cursor','replace','{"model":"template-model","messages":[]}'),
('ignored body','cursor','off','{"model":"ignored-model"}')`)
			require.NoError(test, err)
			repository := &channelMonitorV2Repository{db: database}
			staleConfig, err := repository.GetConfig(contextValue)
			require.NoError(test, err)
			// Both migrations contend on the same PostgreSQL row/table locks.
			migrationResults := make(chan error, 2)
			for range 2 {
				go func() {
					_, migrationError := database.ExecContext(contextValue, string(migration))
					migrationResults <- migrationError
				}()
			}
			require.NoError(test, <-migrationResults)
			require.NoError(test, <-migrationResults)
			importedConfig, err := repository.GetConfig(contextValue)
			require.NoError(test, err)
			require.Equal(test, 11, importedConfig.Version)
			require.False(test, importedConfig.Enabled)
			var cursorConfig service.ChannelMonitorV2PlatformConfig
			for _, platformConfig := range importedConfig.Platforms {
				if platformConfig.Platform == "cursor" {
					cursorConfig = platformConfig
				}
			}
			require.Equal(test, scenario.expectedEnabled, cursorConfig.Enabled)
			if scenario.name == "all models remains unrestricted" {
				require.Empty(test, cursorConfig.Models)
			} else {
				require.Subset(test, cursorConfig.Models, []string{"legacy-extra", "legacy-primary", "template-model"})
			}
			require.NotContains(test, cursorConfig.Models, "ignored-model")
			_, err = repository.UpdateConfig(contextValue, *staleConfig, staleConfig.Version)
			require.ErrorIs(test, err, service.ErrChannelMonitorV2ConfigConflict)
			_, err = database.ExecContext(contextValue, string(migration))
			require.NoError(test, err)
			repeatedConfig, err := repository.GetConfig(contextValue)
			require.NoError(test, err)
			require.Equal(test, importedConfig, repeatedConfig)
			var disabledCount, metricsCount int
			require.NoError(test, database.QueryRow(`SELECT count(*) FROM channel_monitors WHERE enabled=false`).Scan(&disabledCount))
			require.Equal(test, 1, disabledCount)
			require.NoError(test, database.QueryRow(`SELECT count(*) FROM channel_monitor_v2_metrics_1m`).Scan(&metricsCount))
			require.Zero(test, metricsCount)
			require.Zero(test, upstreamCalls.Load())
			// An unrelated admin edit survives rollback; optimistic saves still work.
			importedConfig.Platforms = append(importedConfig.Platforms, service.ChannelMonitorV2PlatformConfig{Platform: "gemini", Enabled: false, Models: []string{"later-choice"}})
			_, err = repository.UpdateConfig(contextValue, *importedConfig, importedConfig.Version)
			require.NoError(test, err)
			_, err = database.ExecContext(contextValue, rollbackCursorInventory)
			require.NoError(test, err)
			rolledBack, err := repository.GetConfig(contextValue)
			require.NoError(test, err)
			expectedPlatforms := append(staleConfig.Platforms, service.ChannelMonitorV2PlatformConfig{Platform: "gemini", Enabled: false, Models: []string{"later-choice"}})
			require.Equal(test, expectedPlatforms, rolledBack.Platforms)
			_, err = database.ExecContext(contextValue, string(migration))
			require.NoError(test, err)
			_, err = database.ExecContext(contextValue, rollbackCursorInventory)
			require.NoError(test, err)
			afterRetry, err := repository.GetConfig(contextValue)
			require.NoError(test, err)
			require.Equal(test, rolledBack, afterRetry)
		})
	}
	test.Run("template only and rollback conflict", func(test *testing.T) {
		_, err := database.ExecContext(contextValue, `DELETE FROM channel_monitors;
UPDATE channel_monitor_v2_config SET platforms='[]', cursor_legacy_import=NULL, version=20;`)
		require.NoError(test, err)
		_, err = database.ExecContext(contextValue, string(migration))
		require.NoError(test, err)
		repository := &channelMonitorV2Repository{db: database}
		importedConfig, err := repository.GetConfig(contextValue)
		require.NoError(test, err)
		require.Equal(test, []service.ChannelMonitorV2PlatformConfig{{Platform: "cursor", Enabled: false, Models: []string{"template-model"}}}, importedConfig.Platforms)
		importedConfig.Platforms[0].Enabled = true
		editedConfig, err := repository.UpdateConfig(contextValue, *importedConfig, importedConfig.Version)
		require.NoError(test, err)
		_, err = database.ExecContext(contextValue, rollbackCursorInventory)
		require.ErrorContains(test, err, "Cursor configuration changed after import")
		afterConflict, err := repository.GetConfig(contextValue)
		require.NoError(test, err)
		require.Equal(test, editedConfig, afterConflict)
		require.Zero(test, upstreamCalls.Load())
	})
	test.Run("no legacy inventory is a no-op", func(test *testing.T) {
		_, err := database.ExecContext(contextValue, `DELETE FROM channel_monitor_request_templates;
UPDATE channel_monitor_v2_config SET platforms='[]', cursor_legacy_import=NULL, version=30;`)
		require.NoError(test, err)
		_, err = database.ExecContext(contextValue, string(migration))
		require.NoError(test, err)
		var version int
		var receipt sql.NullString
		require.NoError(test, database.QueryRow(`SELECT version, cursor_legacy_import FROM channel_monitor_v2_config WHERE id=1`).Scan(&version, &receipt))
		require.Equal(test, 30, version)
		require.False(test, receipt.Valid)
	})
}
