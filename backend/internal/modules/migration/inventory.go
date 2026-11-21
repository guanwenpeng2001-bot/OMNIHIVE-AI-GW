// Package migration provides the read-only evidence used to prepare backend cutovers.
package migration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

const instanceModule = "instance"

type Report struct {
	RunID                  *uuid.UUID        `json:"run_id"`
	Module                 string            `json:"module"`
	Checkpoint             Checkpoint        `json:"checkpoint"`
	Counts                 map[string]Count  `json:"counts"`
	Unmapped               map[string]*int64 `json:"unmapped"`
	UnexplainedDifferences *int64            `json:"unexplained_differences"`
	RollbackFloor          *string           `json:"rollback_floor"`
	EligibleForSwitch      bool              `json:"eligible_for_switch"`
}

type Checkpoint struct {
	State      string     `json:"state"`
	Revision   *int64     `json:"revision"`
	RecordedAt *time.Time `json:"recorded_at"`
}

// A nil value indicates an unavailable source or version field. An available
// version field has nil bounds only when there are no observed values.
type Count struct {
	State   string        `json:"state"`
	Rows    *int64        `json:"rows"`
	Version *VersionRange `json:"version"`
}

type VersionRange struct {
	Field string  `json:"field"`
	Min   *string `json:"min"`
	Max   *string `json:"max"`
}

type Reader struct {
	app     *sql.DB
	restore *sql.DB
}

// NewReader accepts the application DB and the optional independent BE-09
// restore journal DB. The caller owns both connections.
func NewReader(app, restore *sql.DB) *Reader {
	return &Reader{app: app, restore: restore}
}

func (r *Reader) Inventory(ctx context.Context) (Report, error) {
	return r.read(ctx, false)
}

func (r *Reader) Status(ctx context.Context) (Report, error) {
	return r.read(ctx, true)
}

type relation struct {
	schema string
	table  string
}

func (r relation) sqlName() string {
	return pq.QuoteIdentifier(r.schema) + "." + pq.QuoteIdentifier(r.table)
}

type source struct {
	key     string
	table   string
	version string
}

const (
	countKnown   = "known"
	countUnknown = "unknown"
)

var applicationSources = []source{
	{"workspaces", "workspaces", "scope_version"},
	{"projects", "projects", "version"},
	{"api_keys", "api_keys", ""},
	{"api_key_credentials", "api_key_credentials", ""},
	{"legacy_upstream_accounts", "accounts", ""},
	{"provider_connections", "provider_connections", "version"},
	{"legacy_credential_references", "credential_secrets", "version"},
	{"connection_credential_versions", "connection_credential_versions", "version"},
	{"legacy_wallet_accounts", "users", ""},
	{"upstream_jobs", "upstream_jobs", "version"},
	{"batch_image_jobs", "batch_image_jobs", "version"},
}

func (r *Reader) read(ctx context.Context, status bool) (Report, error) {
	report := Report{
		Module: instanceModule,
		Counts: map[string]Count{
			"billing_accounts":                 {State: countUnknown},
			"restore_operations":               {State: countUnknown},
			"restore_operations_outstanding":   {State: countUnknown},
			"restore_backups_requiring_review": {State: countUnknown},
		},
		Unmapped: make(map[string]*int64),
	}
	if !status {
		id, err := uuid.NewRandom()
		if err != nil {
			return Report{}, err
		}
		report.RunID = &id
		report.Checkpoint.State = "observed"
	}

	err := readSnapshot(ctx, r.app, func(tx *sql.Tx) error {
		for _, s := range applicationSources {
			count, err := readCount(ctx, tx, relation{"public", s.table}, s.version, "", nil)
			if err != nil {
				return fmt.Errorf("read %s inventory: %w", s.key, err)
			}
			report.Counts[s.key] = count
		}
		if err := readUnmapped(ctx, tx, report.Unmapped); err != nil {
			return err
		}
		if status {
			return readCheckpoint(ctx, tx, &report)
		}
		return nil
	})
	if err != nil {
		return Report{}, err
	}

	if r.restore != nil {
		// The journal lives in another database. A failed journal snapshot
		// leaves those observations unknown while retaining the app snapshot.
		journalCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		counts, err := readRestore(journalCtx, r.restore)
		cancel()
		if err == nil {
			for key, count := range counts {
				report.Counts[key] = count
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return Report{}, err
	}
	return report, nil
}

func readSnapshot(ctx context.Context, db *sql.DB, read func(*sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SET LOCAL search_path = pg_catalog; SET LOCAL row_security = off`); err != nil {
		return err
	}
	if err := read(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// Treat absent, unreadable and row-filtered sources as unknown. Restrict
// inventory to physical tables so its projection cannot invoke arbitrary views.
func available(ctx context.Context, tx *sql.Tx, source relation, version string, fields ...string) (bool, bool, error) {
	var ok, hasVersion bool
	err := tx.QueryRowContext(ctx, `
WITH readable AS (
	SELECT c.oid FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
	WHERE n.nspname = $1 AND c.relname = $2 AND c.relkind IN ('r', 'p')
	  AND has_schema_privilege(n.oid, 'USAGE')
	  AND (has_table_privilege(c.oid, 'SELECT') OR has_any_column_privilege(c.oid, 'SELECT'))
	  AND NOT row_security_active(c.oid)
	  AND NOT EXISTS (
	    SELECT FROM unnest($3::text[]) AS field(name)
	    WHERE NOT EXISTS (
	      SELECT FROM pg_attribute a
	      WHERE a.attrelid = c.oid AND a.attname = field.name AND a.attnum > 0
	        AND NOT a.attisdropped AND has_column_privilege(c.oid, a.attnum, 'SELECT')
	    )
	  )
)
SELECT EXISTS (SELECT FROM readable), EXISTS (
	SELECT FROM readable c JOIN pg_attribute a ON a.attrelid = c.oid
	WHERE a.attname = $4 AND a.attnum > 0 AND NOT a.attisdropped
	 AND a.atttypid IN ('int2'::regtype, 'int4'::regtype, 'int8'::regtype)
	 AND has_column_privilege(c.oid, a.attnum, 'SELECT')
)`, source.schema, source.table, pq.Array(fields), version).Scan(&ok, &hasVersion)
	return ok, hasVersion, err
}

// Source identifiers and predicates are supplied exclusively by this package.
// Only count and scalar version bounds cross the SQL boundary.
func readCount(ctx context.Context, tx *sql.Tx, source relation, version, predicate string, fields []string) (Count, error) {
	ok, hasVersion, err := available(ctx, tx, source, version, fields...)
	if err != nil || !ok {
		return Count{State: countUnknown}, err
	}
	from := " FROM " + source.sqlName() + predicate
	var rows int64
	count := Count{State: countKnown, Rows: &rows}
	if !hasVersion {
		err = tx.QueryRowContext(ctx, "SELECT count(*)"+from).Scan(&rows)
		return count, err
	}
	bounds := VersionRange{Field: version}
	column := pq.QuoteIdentifier(version)
	err = tx.QueryRowContext(ctx, "SELECT count(*), min("+column+")::text, max("+column+")::text"+from).
		Scan(&rows, &bounds.Min, &bounds.Max)
	count.Version = &bounds
	return count, err
}

func readUnmapped(ctx context.Context, tx *sql.Tx, unmapped map[string]*int64) error {
	for _, table := range []string{"identity_scope_quarantine", "connection_migration_quarantine"} {
		count, err := readCount(ctx, tx, relation{"public", table}, "", "", nil)
		if err != nil {
			return err
		}
		unmapped[table] = count.Rows
	}
	open, err := readCount(ctx, tx, relation{"public", "refactor_mapping_exceptions"}, "",
		" WHERE resolved_at IS NULL", []string{"resolved_at"})
	unmapped["open_mapping_exceptions"] = open.Rows
	return err
}

func readCheckpoint(ctx context.Context, tx *sql.Tx, report *Report) error {
	source := relation{"public", "refactor_checkpoints"}
	ok, _, err := available(ctx, tx, source, "", "run_id", "module", "workspace_id", "revision", "updated_at")
	if err != nil {
		return err
	}
	if !ok {
		report.Checkpoint.State = "unknown"
		return nil
	}
	var id uuid.UUID
	var revision int64
	var recordedAt time.Time
	err = tx.QueryRowContext(ctx, `
SELECT run_id, revision, updated_at FROM public.refactor_checkpoints
WHERE module = $1 AND workspace_id = 0
ORDER BY updated_at DESC, run_id DESC
LIMIT 1`, instanceModule).Scan(&id, &revision, &recordedAt)
	if errors.Is(err, sql.ErrNoRows) {
		report.Checkpoint.State = "not_started"
		return nil
	}
	if err != nil {
		return err
	}
	recordedAt = recordedAt.UTC()
	report.RunID = &id
	report.Checkpoint = Checkpoint{State: "recorded", Revision: &revision, RecordedAt: &recordedAt}
	return nil
}

func readRestore(ctx context.Context, db *sql.DB) (map[string]Count, error) {
	counts := make(map[string]Count)
	err := readSnapshot(ctx, db, func(tx *sql.Tx) error {
		operations := relation{"restore_journal", "operations"}
		total, err := readCount(ctx, tx, operations, "generation", "", nil)
		if err != nil {
			return err
		}
		outstanding, err := readCount(ctx, tx, operations, "generation",
			" WHERE state IN ('queued', 'running', 'unknown') OR stage='manual_recovery'", []string{"state", "stage"})
		if err != nil {
			return err
		}
		review, err := readCount(ctx, tx, relation{"restore_journal", "backups"}, "",
			" WHERE review_required", []string{"review_required"})
		counts["restore_operations"] = total
		counts["restore_operations_outstanding"] = outstanding
		counts["restore_backups_requiring_review"] = review
		return err
	})
	return counts, err
}
