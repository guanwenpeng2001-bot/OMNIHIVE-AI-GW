//go:build integration

package migrations_test

import (
	"context"
	"os"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil/testresources"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/migrations"
	"github.com/stretchr/testify/require"
)

func TestConfigurationAuthorityUpgrade(t *testing.T) {
	dsn := os.Getenv("UPSTREAM_JOB_TEST_DSN")
	if dsn == "" {
		t.Skip("set UPSTREAM_JOB_TEST_DSN to isolated job_test")
	}
	require.NoError(t, testresources.ValidatePostgres(dsn, "job_test"))
	body, err := migrations.FS.ReadFile("340_configuration_authority_and_safe_rollback.sql")
	require.NoError(t, err)
	for _, scenario := range []struct {
		name     string
		setup    string
		plaza    string
		source   string
		schema   string
		semantic string
		minimum  string
	}{
		{name: "fresh_database", schema: "0.3.0", semantic: "0.3.5", minimum: "0.3.5"},
		{name: "existing_user", setup: "INSERT INTO users VALUES (7)", plaza: "true", source: "migration_snapshot", schema: "0.3.0", semantic: "0.3.5", minimum: "0.3.5"},
		{name: "existing_registration", setup: "INSERT INTO settings(key,value) VALUES ('registration_enabled','false')", plaza: "true", source: "migration_snapshot", schema: "0.3.0", semantic: "0.3.5", minimum: "0.3.5"},
		{name: "explicit_disabled", setup: "INSERT INTO users VALUES (7); INSERT INTO settings(key,value) VALUES ('model_plaza_enabled','false')", plaza: "false", schema: "0.3.0", semantic: "0.3.5", minimum: "0.3.5"},
		{name: "explicit_enabled", setup: "INSERT INTO users VALUES (7); INSERT INTO settings(key,value) VALUES ('model_plaza_enabled','true')", plaza: "true", schema: "0.3.0", semantic: "0.3.5", minimum: "0.3.5"},
		{name: "historical_floor", setup: "UPDATE release_schema_compatibility SET minimum_rollback_version='0.2.23-omnihive.2'", schema: "0.2.23-omnihive.2", semantic: "0.3.5", minimum: "0.3.5"},
		{name: "higher_floor", setup: "ALTER TABLE release_schema_compatibility ADD COLUMN schema_rollback_floor text, ADD COLUMN semantic_rollback_floor text; UPDATE release_schema_compatibility SET minimum_rollback_version='v0.4.1',schema_rollback_floor='0.4.0',semantic_rollback_floor='0.4.1'", schema: "0.4.0", semantic: "0.4.1", minimum: "v0.4.1"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			db := testresources.OpenSchema(t, dsn)
			ctx := context.Background()
			_, err := db.ExecContext(ctx, `
CREATE TABLE users(id bigint PRIMARY KEY);
CREATE TABLE settings(id bigserial PRIMARY KEY,key text UNIQUE NOT NULL,value text NOT NULL,updated_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE release_schema_compatibility(singleton boolean PRIMARY KEY,minimum_rollback_version text NOT NULL);
INSERT INTO release_schema_compatibility VALUES(true,'0.3.0');
INSERT INTO settings(key,value,updated_at) VALUES('unrelated_legacy_setting','preserve-verbatim','2026-01-01T00:00:00Z');`)
			require.NoError(t, err)
			if scenario.setup != "" {
				_, err = db.ExecContext(ctx, scenario.setup)
				require.NoError(t, err)
			}
			for attempt := 0; attempt < 2; attempt++ {
				_, err = db.ExecContext(ctx, string(body))
				require.NoError(t, err)
				var plaza, source, schema, semantic, minimum, preserved string
				require.NoError(t, db.QueryRowContext(ctx, "SELECT COALESCE((SELECT value FROM settings WHERE key='model_plaza_enabled'),'')").Scan(&plaza))
				require.NoError(t, db.QueryRowContext(ctx, "SELECT COALESCE((SELECT value FROM settings WHERE key='model_plaza_enabled_source'),'')").Scan(&source))
				require.NoError(t, db.QueryRowContext(ctx, "SELECT schema_rollback_floor,semantic_rollback_floor,minimum_rollback_version FROM release_schema_compatibility WHERE singleton").Scan(&schema, &semantic, &minimum))
				require.NoError(t, db.QueryRowContext(ctx, "SELECT value || ':' || to_char(updated_at AT TIME ZONE 'UTC','YYYY-MM-DD') FROM settings WHERE key='unrelated_legacy_setting'").Scan(&preserved))
				require.Equal(t, scenario.plaza, plaza)
				require.Equal(t, scenario.source, source)
				require.Equal(t, scenario.schema, schema)
				require.Equal(t, scenario.semantic, semantic)
				require.Equal(t, scenario.minimum, minimum)
				require.Equal(t, "preserve-verbatim:2026-01-01", preserved)
			}
		})
	}
}
