package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/restoregate"
)

// Admit one business statement or transaction at a time. A pin on an idle
// target connection must not prevent the worker from draining active writers.
type restoreConnector struct {
	driver.Connector
	journal *sql.DB
	gate    *restoregate.Gate
}

func (c restoreConnector) Close() error { return c.journal.Close() }

type pgConn interface {
	driver.Conn
	driver.ConnBeginTx
	driver.ConnPrepareContext
	driver.ExecerContext
	driver.QueryerContext
	driver.Pinger
	driver.SessionResetter
	driver.Validator
}

func (c restoreConnector) Connect(ctx context.Context) (driver.Conn, error) {
	raw, err := c.Connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	conn, ok := raw.(pgConn)
	if !ok {
		_ = raw.Close()
		return nil, fmt.Errorf("restore connector: unsupported driver connection %T", raw)
	}
	return &restoreConn{pgConn: conn, gate: c.gate}, nil
}

type restoreConn struct {
	pgConn
	transaction bool
	gate        *restoregate.Gate
}

func (c *restoreConn) enter(ctx context.Context) (func(), error) {
	if c.transaction || restoregate.IsSnapshot(ctx) {
		return func() {}, nil
	}
	leave, ok := c.gate.AdmitStatement(ctx)
	if !ok {
		return nil, errors.New("restore maintenance")
	}
	return leave, nil
}
func (c *restoreConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	leave, err := c.enter(ctx)
	if err != nil {
		return nil, err
	}
	tx, err := c.pgConn.BeginTx(ctx, opts)
	if err != nil {
		leave()
		return nil, err
	}
	c.transaction = true
	return &restoreTx{tx, func() { c.transaction = false; leave() }}, nil
}

type restoreTx struct {
	driver.Tx
	leave func()
}

func (t *restoreTx) Commit() error   { defer t.leave(); return t.Tx.Commit() }
func (t *restoreTx) Rollback() error { defer t.leave(); return t.Tx.Rollback() }

type pgStmt interface {
	driver.Stmt
	driver.StmtExecContext
	driver.StmtQueryContext
}
type restoreStmt struct {
	pgStmt
	conn *restoreConn
}

func (c *restoreConn) PrepareContext(ctx context.Context, q string) (driver.Stmt, error) {
	leave, err := c.enter(ctx)
	if err != nil {
		return nil, err
	}
	defer leave()
	stmt, err := c.pgConn.PrepareContext(ctx, q)
	if err != nil {
		return nil, err
	}
	// COPY FROM uses a special lib/pq statement inside an admitted transaction.
	if c.transaction {
		return stmt, nil
	}
	prepared, ok := stmt.(pgStmt)
	if !ok {
		_ = stmt.Close()
		return nil, fmt.Errorf("restore connector: unsupported driver statement %T", stmt)
	}
	return &restoreStmt{prepared, c}, nil
}
func (c *restoreConn) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	leave, err := c.enter(ctx)
	if err != nil {
		return nil, err
	}
	defer leave()
	return c.pgConn.ExecContext(ctx, q, args)
}
func (s *restoreStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	leave, err := s.conn.enter(ctx)
	if err != nil {
		return nil, err
	}
	defer leave()
	return s.pgStmt.ExecContext(ctx, args)
}
func (c *restoreConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	leave, err := c.enter(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := c.pgConn.QueryContext(ctx, q, args)
	return admittedRows(rows, err, leave)
}
func (s *restoreStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	leave, err := s.conn.enter(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.pgStmt.QueryContext(ctx, args)
	return admittedRows(rows, err, leave)
}

type pgRows interface {
	driver.RowsNextResultSet
	driver.RowsColumnTypeScanType
	driver.RowsColumnTypeDatabaseTypeName
	driver.RowsColumnTypeLength
	driver.RowsColumnTypePrecisionScale
}
type restoreRows struct {
	pgRows
	leave func()
}

func admittedRows(rows driver.Rows, err error, leave func()) (driver.Rows, error) {
	if err != nil {
		leave()
		return nil, err
	}
	wrapped, ok := rows.(pgRows)
	if !ok {
		err := rows.Close()
		leave()
		return nil, errors.Join(errors.New("unexpected postgres rows type"), err)
	}
	return &restoreRows{wrapped, leave}, nil
}
func (r *restoreRows) Close() error { defer r.leave(); return r.pgRows.Close() }
