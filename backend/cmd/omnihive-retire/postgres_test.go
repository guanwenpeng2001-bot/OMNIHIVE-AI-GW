//go:build integration

package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/legacy/retirement"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/ops/restore/testfixture"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/migrations"
	"github.com/stretchr/testify/require"
)

func TestRetirementOnIsolatedPostgres(t *testing.T) {
	db, cfg := testfixture.Database(t, "BE30_INTEGRATION_TEST_DSN", "job_test")
	t.Setenv("OMNIHIVE_REFACTOR_DSN", cfg.Database.DSN()+" default_transaction_read_only=on")
	t.Setenv("OMNIHIVE_RESTORE_JOURNAL_DSN", "")
	execSQL := func(s string) { t.Helper(); _, err := db.ExecContext(t.Context(), s); require.NoError(t, err) }
	read := func(args ...string) string {
		t.Helper()
		var out, diag bytes.Buffer
		require.Equal(t, 0, run(t.Context(), args, &out, &diag), diag.String())
		require.NotContains(t, out.String(), "sensitive-fixture")
		return out.String()
	}
	report := func() retirement.Report {
		t.Helper()
		s := read("scan")
		require.Equal(t, s, read("report", "--format=json"))
		var r retirement.Report
		require.NoError(t, json.Unmarshal([]byte(s), &r))
		return r
	}
	entry := func(r retirement.Report, id string) retirement.Contract {
		t.Helper()
		for _, c := range r.Entries {
			if c.ID == id {
				return c
			}
		}
		t.Fatalf("missing %s", id)
		return retirement.Contract{}
	}
	historical, err := migrations.FS.ReadFile("254_usage_settlement_receipts.sql")
	require.NoError(t, err)
	execSQL(string(historical))
	execSQL(`INSERT INTO usage_settlement_receipts(request_id,api_key_id,user_id,account_id,request_fingerprint,usage_json,billing_command,pricing_state,pricing_snapshot,resolution_state,completion_state,state) VALUES ('legacy-request',7,2,3,'sensitive-fixture-fingerprint','{}','{}','priced','USD','resolved','complete','pending');
        CREATE TABLE payment_orders(id bigint PRIMARY KEY,out_trade_no text); INSERT INTO payment_orders VALUES(83,'sensitive-fixture-trade');
        CREATE TABLE sub2api_plugin_installations(manifest jsonb); INSERT INTO sub2api_plugin_installations VALUES ('{"schema_version":1}'),('{"schema_version":2}');
        CREATE TABLE accounts(id bigint, workspace_id bigint, deleted_at timestamptz); INSERT INTO accounts VALUES(1,2,NULL),(2,2,NULL);
        CREATE TABLE provider_connections(account_id bigint,workspace_id bigint,legacy_dirty boolean);INSERT INTO provider_connections VALUES(1,2,false);
        CREATE TABLE gateway_execution_receipts(receipt_id bigint)`)
	before := report()
	require.False(t, before.RegisterAvailable)
	require.Nil(t, entry(before, "plugin-list").RemainingCallers)
	require.EqualValues(t, 1, *before.Counts["plugin_installations_without_v2_manifest"])
	require.EqualValues(t, 1, *before.Counts["live_accounts_without_clean_connection"])
	require.EqualValues(t, 1, *before.Counts["receipts_without_journal_link"])
	require.Nil(t, before.Counts["credential_versions_without_snapshot"])
	ddl, err := migrations.FS.ReadFile("319_retirement_observations.sql")
	require.NoError(t, err)
	for range 2 {
		execSQL(string(ddl))
	}
	expanded := report()
	require.True(t, expanded.RegisterAvailable)
	require.Equal(t, before.Counts, expanded.Counts)
	require.Equal(t, before.Entries, expanded.Entries)
	execSQL(`INSERT INTO retirement_observations(contract_id, observed_at, evidence_id, deployed_versions, remaining_callers, complete_census, coverage_from, valid_until, notice_id, window_start, window_end, minimum_binary, minimum_schema, reconciliation_id, recovery_drill_id)
        SELECT id, now()-interval '1 hour', gen_random_uuid(), ARRAY['v2.2.0'], 0, true, now()-interval '100 days', now()+interval '1 day', gen_random_uuid(), now()-interval '95 days', now()-interval '2 days','v2.1.0',312,gen_random_uuid(),gen_random_uuid() FROM unnest(ARRAY['plugin-list','native-endpoints','payment-callback','admin-auth-ops','inline-credentials']) AS ids(id)`)
	r := report()
	require.Equal(t, "review", entry(r, "plugin-list").State)
	for _, id := range []string{"native-endpoints", "payment-callback", "admin-auth-ops"} {
		require.Equal(t, "preserve", entry(r, id).State)
	}
	require.Equal(t, "hold", entry(r, "inline-credentials").State)
	require.Contains(t, read("plan"), `"review":["plugin-list"]`)
	require.Equal(t, read("plan"), read("plan"))
	require.Equal(t, read("report", "--format=markdown"), read("report", "--format=markdown"))
	// An incomplete new census invalidates the old zero even across CLI restarts.
	execSQL(`INSERT INTO retirement_observations(contract_id,observed_at,evidence_id,remaining_callers) VALUES('plugin-list',now(),gen_random_uuid(),2)`)
	r = report()
	c := entry(r, "plugin-list")
	require.Equal(t, "hold", c.State)
	require.Nil(t, c.RemainingCallers)
	require.EqualValues(t, 2, *c.ObservedCallers)
	var trade, fingerprint string
	require.NoError(t, db.QueryRow(`SELECT out_trade_no FROM payment_orders WHERE id=83`).Scan(&trade))
	require.Equal(t, "sensitive-fixture-trade", trade)
	require.NoError(t, db.QueryRow(`SELECT request_fingerprint FROM usage_settlement_receipts WHERE request_id='legacy-request' AND api_key_id=7`).Scan(&fingerprint))
	require.Equal(t, "sensitive-fixture-fingerprint", fingerprint)
	require.NotContains(t, strings.ToLower(read("plan", "--format=markdown")), "sensitive-fixture")
}
