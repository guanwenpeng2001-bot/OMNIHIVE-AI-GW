package retirement

import (
	"context"
	"database/sql"
	"errors"

	"github.com/lib/pq"
)

// Only read ordinary or partitioned tables accessible without row filtering.
// Each probe names its physical inputs explicitly to avoid calling arbitrary views.
func readable(ctx context.Context, tx *sql.Tx, table string) (bool, error) {
	var ok bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS (
        SELECT FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE c.oid = to_regclass($1) AND c.relkind IN ('r','p')
        AND has_schema_privilege(n.oid,'USAGE') AND has_table_privilege(c.oid,'SELECT')
        AND NOT row_security_active(c.oid))`, table).Scan(&ok)
	return ok, err
}

func readObservations(ctx context.Context, tx *sql.Tx) (map[string]Observation, bool, error) {
	ok, err := readable(ctx, tx, "public.retirement_observations")
	if !ok || err != nil {
		return nil, false, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT ON (contract_id) contract_id, evidence_id, observed_at,
        deployed_versions, remaining_callers, complete_census, coverage_from, valid_until,
        notice_id, window_start, window_end, minimum_binary, minimum_schema, reconciliation_id, recovery_drill_id
        FROM public.retirement_observations ORDER BY contract_id, observed_at DESC`)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	out := make(map[string]Observation)
	for rows.Next() {
		var id string
		var o Observation
		if err := rows.Scan(&id, &o.EvidenceID, &o.ObservedAt, pq.Array(&o.DeployedVersions), &o.ObservedCallers, &o.Complete, &o.CoverageFrom, &o.ValidUntil, &o.NoticeID, &o.WindowStart, &o.WindowEnd, &o.MinimumBinary, &o.MinimumSchema, &o.ReconciliationID, &o.RecoveryDrillID); err != nil {
			return nil, false, err
		}
		out[id] = o
	}
	return out, true, rows.Err()
}

type probe struct {
	key    string
	tables []string
	query  string
}

var probes = []probe{
	{"plugin_installations_without_v2_manifest", []string{"public.sub2api_plugin_installations"},
		`SELECT count(*) FROM public.sub2api_plugin_installations WHERE (manifest->>'schema_version') IS DISTINCT FROM '2'`},
	{"verified_plugin_migrations", []string{"public.omnihive_extension_migrations"},
		`SELECT count(*) FROM public.omnihive_extension_migrations`},
	{"live_accounts_without_clean_connection", []string{"public.accounts", "public.provider_connections"},
		`SELECT count(*) FROM public.accounts a WHERE a.deleted_at IS NULL AND NOT EXISTS (
            SELECT FROM public.provider_connections c
            WHERE c.account_id=a.id AND c.workspace_id=a.workspace_id AND NOT c.legacy_dirty)`},
	{"credential_versions_without_snapshot", []string{"public.credential_secrets", "public.connection_credential_versions"},
		`SELECT count(*) FROM public.credential_secrets s WHERE NOT EXISTS (
            SELECT FROM public.connection_credential_versions v
            WHERE (v.account_id,v.workspace_id,v.version,v.reference)=(s.account_id,s.workspace_id,s.version,s.reference))`},
	{"receipts_awaiting_settlement_or_review", []string{"public.usage_settlement_receipts"},
		`SELECT count(*) FROM public.usage_settlement_receipts WHERE state IN ('pending','needs_review')`},
	{"receipts_without_journal_link", []string{"public.usage_settlement_receipts", "public.gateway_execution_receipts"},
		`SELECT count(*) FROM public.usage_settlement_receipts r WHERE NOT EXISTS (
            SELECT FROM public.gateway_execution_receipts l WHERE l.receipt_id=r.id)`},
}

func readCounts(ctx context.Context, tx *sql.Tx) (map[string]*int64, error) {
	counts := make(map[string]*int64, len(probes))
	for _, p := range probes {
		value, err := probeCount(ctx, tx, p)
		if err != nil {
			return nil, err
		}
		counts[p.key] = value
	}
	return counts, nil
}

func probeCount(ctx context.Context, tx *sql.Tx, p probe) (*int64, error) {
	for _, table := range p.tables {
		ok, err := readable(ctx, tx, table)
		if !ok || err != nil {
			return nil, err
		}
	}
	// Older schemas can have the table but lack a column. Recover this probe's
	// snapshot instead of treating absent coverage as zero or losing other counts.
	if _, err := tx.ExecContext(ctx, "SAVEPOINT retirement_count"); err != nil {
		return nil, err
	}
	var rows int64
	var value *int64
	err := tx.QueryRowContext(ctx, p.query).Scan(&rows)
	if err == nil {
		value = &rows
	} else {
		var pg *pq.Error
		if !errors.As(err, &pg) || (pg.Code != "42703" && pg.Code != "42501" && pg.Code != "42P01") {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT retirement_count"); err != nil {
			return nil, err
		}
	}
	_, err = tx.ExecContext(ctx, "RELEASE SAVEPOINT retirement_count")
	return value, err
}
