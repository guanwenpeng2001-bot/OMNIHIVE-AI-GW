package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"

	"github.com/lib/pq"
)

// PostgreSQL cannot build a partitioned index concurrently. Build ONLY parent
// metadata, build each leaf concurrently, then attach complete child indexes.
// Keep this handling specific to 274; other migration SQL executes unchanged.
func applyUsageSessionIndexMigration(ctx context.Context, connection migrationConnection) error {
	var schemaName, tableName string
	err := connection.QueryRowContext(ctx, `SELECT namespace.nspname, relation.relname
		FROM pg_class relation JOIN pg_namespace namespace ON namespace.oid = relation.relnamespace
		WHERE relation.oid = 'usage_logs'::regclass`).Scan(&schemaName, &tableName)
	if err != nil {
		return err
	}
	return ensureUsageSessionIndex(ctx, connection, schemaName, tableName, usageLogsSessionIDIndex)
}

const usageSessionIndexMatches = `index_state.indnkeyatts = 1 AND index_state.indnatts = 1
	AND NOT index_state.indisunique AND index_state.indexprs IS NULL
	AND index_state.indoption[0] = 0 AND access_method.amname = 'btree'
	AND pg_get_indexdef(index_state.indexrelid, 1, TRUE) = 'session_id'
	AND pg_get_expr(index_state.indpred, index_state.indrelid) = '(session_id IS NOT NULL)'`

func ensureUsageSessionIndex(ctx context.Context, connection migrationConnection, schemaName, tableName, indexName string) error {
	tableIdentifier := pq.QuoteIdentifier(schemaName) + "." + pq.QuoteIdentifier(tableName)
	indexIdentifier := pq.QuoteIdentifier(schemaName) + "." + pq.QuoteIdentifier(indexName)
	var partitioned bool
	if err := connection.QueryRowContext(ctx, `SELECT relkind = 'p' FROM pg_class WHERE oid = $1::regclass`, tableIdentifier).Scan(&partitioned); err != nil {
		return err
	}

	var valid, matches bool
	err := connection.QueryRowContext(ctx, `SELECT index_state.indisvalid AND index_state.indisready,
		COALESCE(index_state.indrelid = $2::regclass AND `+usageSessionIndexMatches+`, FALSE)
		FROM pg_index index_state JOIN pg_class index_class ON index_class.oid = index_state.indexrelid
		JOIN pg_am access_method ON access_method.oid = index_class.relam
		WHERE index_state.indexrelid = to_regclass($1)`, indexIdentifier, tableIdentifier).Scan(&valid, &matches)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil && !matches {
		return fmt.Errorf("index %s does not match the required session_id partial btree index", indexIdentifier)
	}
	if errors.Is(err, sql.ErrNoRows) {
		mode, target := "CONCURRENTLY ", tableIdentifier
		if partitioned {
			mode, target = "", "ONLY "+tableIdentifier
		}
		_, err = connection.ExecContext(ctx, "CREATE INDEX "+mode+"IF NOT EXISTS "+pq.QuoteIdentifier(indexName)+
			" ON "+target+" (session_id) WHERE session_id IS NOT NULL")
		if err != nil {
			return fmt.Errorf("create index %s: %w", indexIdentifier, err)
		}
	} else if !valid && !partitioned {
		// REINDEX also repairs an invalid leaf already attached to a parent;
		// dropping that leaf would fail because the parent depends on it.
		if _, err := connection.ExecContext(ctx, "REINDEX INDEX CONCURRENTLY "+indexIdentifier); err != nil {
			return fmt.Errorf("repair index %s: %w", indexIdentifier, err)
		}
	}

	if partitioned {
		if err := attachUsageSessionPartitionIndexes(ctx, connection, tableIdentifier, indexIdentifier); err != nil {
			return err
		}
	}
	if err := connection.QueryRowContext(ctx, `SELECT indisvalid AND indisready FROM pg_index WHERE indexrelid = $1::regclass`, indexIdentifier).Scan(&valid); err != nil {
		return err
	}
	if !valid {
		return fmt.Errorf("index %s is not complete and valid", indexIdentifier)
	}
	return nil
}

func attachUsageSessionPartitionIndexes(ctx context.Context, connection migrationConnection, tableIdentifier, indexIdentifier string) error {
	rows, err := connection.QueryContext(ctx, `SELECT namespace.nspname, child.relname
		FROM pg_inherits inheritance JOIN pg_class child ON child.oid = inheritance.inhrelid
		JOIN pg_namespace namespace ON namespace.oid = child.relnamespace
		WHERE inheritance.inhparent = $1::regclass ORDER BY child.oid`, tableIdentifier)
	if err != nil {
		return err
	}
	type partition struct{ schemaName, tableName string }
	var partitions []partition
	for rows.Next() {
		var child partition
		if err := rows.Scan(&child.schemaName, &child.tableName); err != nil {
			_ = rows.Close()
			return err
		}
		partitions = append(partitions, child)
	}
	readErr := rows.Err()
	_ = rows.Close() // The migration uses one dedicated connection, including recursion.
	if readErr != nil {
		return readErr
	}
	for _, child := range partitions {
		childTable := pq.QuoteIdentifier(child.schemaName) + "." + pq.QuoteIdentifier(child.tableName)
		var childIndexName string
		// Reuse an attached index first, or an equivalent unattached index left by
		// an interrupted build/manual preparation. Never attach another parent's index.
		err := connection.QueryRowContext(ctx, `SELECT index_class.relname
			FROM pg_index index_state JOIN pg_class index_class ON index_class.oid = index_state.indexrelid
			JOIN pg_am access_method ON access_method.oid = index_class.relam
			LEFT JOIN pg_inherits inheritance ON inheritance.inhrelid = index_state.indexrelid
			WHERE index_state.indrelid = $1::regclass
			AND (inheritance.inhparent = $2::regclass OR (inheritance.inhparent IS NULL AND `+usageSessionIndexMatches+`))
			ORDER BY inheritance.inhparent NULLS LAST, index_state.indisvalid DESC, index_class.oid LIMIT 1`,
			childTable, indexIdentifier).Scan(&childIndexName)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if errors.Is(err, sql.ErrNoRows) {
			identity := sha256.Sum256([]byte(childTable))
			childIndexName = fmt.Sprintf("idx_usage_session_%x", identity[:12])
		}
		if err := ensureUsageSessionIndex(ctx, connection, child.schemaName, child.tableName, childIndexName); err != nil {
			return err
		}
		childIndex := pq.QuoteIdentifier(child.schemaName) + "." + pq.QuoteIdentifier(childIndexName)
		if _, err := connection.ExecContext(ctx, "ALTER INDEX "+indexIdentifier+" ATTACH PARTITION "+childIndex); err != nil {
			return fmt.Errorf("attach index %s: %w", childIndex, err)
		}
	}
	return nil
}
