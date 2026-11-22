//go:build integration

package migrations_test

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil/testresources"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/migrations"
	"github.com/stretchr/testify/require"
)

func TestConnectionPlanObservationFence(t *testing.T) {
	dsn := os.Getenv("UPSTREAM_JOB_TEST_DSN")
	if dsn == "" {
		t.Skip("set UPSTREAM_JOB_TEST_DSN to isolated job_test")
	}
	require.NoError(t, testresources.ValidatePostgres(dsn, "job_test"))
	body, err := migrations.FS.ReadFile("341_connection_runtime_observation_fence.sql")
	require.NoError(t, err)
	for _, scenario := range []struct {
		name, marker, update, setup string
		dirty                       bool
		version                     int64
		verified                    bool
		events                      int
	}{
		{name: "observed_plan_invalidates_without_hiding_connection", marker: "7", update: `credentials=credentials||'{"plan_type":"free"}'`, version: 2, events: 1},
		{name: "unmarked_plan_is_configuration", update: `credentials=credentials||'{"plan_type":"free"}'`, dirty: true, version: 2},
		{name: "marker_for_another_account_is_rejected", marker: "8", update: `credentials=credentials||'{"plan_type":"free"}'`, dirty: true, version: 2},
		{name: "mixed_endpoint_is_configuration", marker: "7", update: `credentials=credentials||'{"plan_type":"free","base_url":"https://changed.example"}'`, dirty: true, version: 2},
		{name: "mixed_name_is_configuration", marker: "7", update: `name='changed',credentials=credentials||'{"plan_type":"free"}'`, dirty: true, version: 2},
		{name: "mixed_extra_is_configuration", marker: "7", update: `extra='{"header_overrides":{"X-Example":"changed"}}',credentials=credentials||'{"plan_type":"free"}'`, dirty: true, version: 2},
		{name: "mixed_secret_version_is_configuration", marker: "7", update: `credentials=credentials||'{"plan_type":"free","credential_secret_version":2}'`, dirty: true, version: 2},
		{name: "unchanged_plan_keeps_verification", marker: "7", update: `credentials=credentials||'{"plan_type":"plus"}'`, version: 1, verified: true},
		{name: "empty_plan_is_not_an_observation", marker: "7", update: `credentials=credentials||'{"plan_type":""}'`, dirty: true, version: 2},
		{name: "other_provider_keeps_legacy_fence", marker: "7", setup: "UPDATE accounts SET platform='anthropic'", update: `credentials=credentials||'{"plan_type":"free"}'`, dirty: true, version: 2},
		{name: "observation_cannot_clear_existing_quarantine", marker: "7", setup: "UPDATE provider_connections SET legacy_dirty=true,verified_id=NULL", update: `credentials=credentials||'{"plan_type":"free"}'`, dirty: true, version: 1},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			db := testresources.OpenSchema(t, dsn)
			_, err := db.Exec(`CREATE TABLE accounts(id bigint PRIMARY KEY,name text,platform text,type text,proxy_id bigint,proxy_group_id bigint,concurrency integer,status text,schedulable boolean,credentials jsonb,extra jsonb DEFAULT '{}');
CREATE TABLE provider_connections(account_id bigint PRIMARY KEY,workspace_id bigint,legacy_dirty boolean NOT NULL DEFAULT false,verified_id uuid,version bigint NOT NULL DEFAULT 1);
CREATE TABLE connection_events(workspace_id bigint,account_id bigint,type text,version bigint);
INSERT INTO accounts(id,name,platform,type,concurrency,status,schedulable,credentials) VALUES(7,'managed','openai','oauth',3,'active',true,'{"plan_type":"plus","credential_secret_version":1,"base_url":"https://original.example"}');
INSERT INTO provider_connections(account_id,workspace_id,verified_id) VALUES(7,1,'00000000-0000-0000-0000-000000000001');`)
			require.NoError(t, err)
			if scenario.setup != "" {
				_, err = db.Exec(scenario.setup)
				require.NoError(t, err)
			}
			_, err = db.Exec(string(body))
			require.NoError(t, err)
			_, err = db.Exec(string(body))
			require.NoError(t, err, "replacement migration is replay safe")
			_, err = db.Exec(`CREATE TRIGGER connection_fence_legacy_edit AFTER UPDATE OF credentials,name,platform,type,proxy_id,proxy_group_id,concurrency,status,schedulable ON accounts FOR EACH ROW EXECUTE FUNCTION connection_fence_legacy_edit()`)
			require.NoError(t, err)
			tx, err := db.BeginTx(context.Background(), nil)
			require.NoError(t, err)
			defer func() { _ = tx.Rollback() }()
			_, err = tx.Exec("SELECT set_config('omnihive.connection_observation_account',$1,true)", scenario.marker)
			require.NoError(t, err)
			_, err = tx.Exec("UPDATE accounts SET " + scenario.update + " WHERE id=7")
			require.NoError(t, err)
			require.NoError(t, tx.Commit())
			var dirty bool
			var version int64
			var verified sql.NullString
			var events int
			require.NoError(t, db.QueryRow("SELECT legacy_dirty,version,verified_id::text FROM provider_connections WHERE account_id=7").Scan(&dirty, &version, &verified))
			require.NoError(t, db.QueryRow("SELECT count(*) FROM connection_events WHERE type='connection.plan_observed'").Scan(&events))
			require.Equal(t, scenario.dirty, dirty)
			require.Equal(t, scenario.version, version)
			require.Equal(t, scenario.verified, verified.Valid)
			require.Equal(t, scenario.events, events)
			if scenario.events == 1 {
				var visible, routable int
				require.NoError(t, db.QueryRow("SELECT count(*) FROM provider_connections WHERE NOT legacy_dirty").Scan(&visible))
				require.NoError(t, db.QueryRow("SELECT count(*) FROM provider_connections WHERE NOT legacy_dirty AND verified_id IS NOT NULL").Scan(&routable))
				require.Equal(t, 1, visible, "the operator must still be able to inspect and reverify the connection")
				require.Zero(t, routable, "a changed subscription must not keep stale verified routing authority")
			}
		})
	}
}
