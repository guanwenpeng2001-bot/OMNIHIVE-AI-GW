package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/shirou/gopsutil/v4/disk"
)

// PgDumper implements service.DBDumper using pg_dump/psql
type PgDumper struct {
	cfg              *config.DatabaseConfig
	db               *sql.DB
	commandContext   func(context.Context, string, ...string) *exec.Cmd
	restoreDiskUsage func(context.Context, string) (*disk.UsageStat, error)
}

// NewPgDumper creates a new PgDumper
func NewPgDumper(cfg *config.Config, db *sql.DB) *PgDumper {
	return &PgDumper{
		cfg:            &cfg.Database,
		db:             db,
		commandContext: exec.CommandContext,
	}
}

// Dump executes pg_dump and returns a streaming reader of the output
func (d *PgDumper) Dump(ctx context.Context) (io.ReadCloser, error) {
	if d.db == nil {
		return nil, errors.New("acquire backup migration lock: nil sql db")
	}
	lockConn, err := d.db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire backup migration lock connection: %w", err)
	}
	if err := pgAdvisoryLock(ctx, lockConn); err != nil {
		// Lock acquisition can fail after PostgreSQL accepted the request. Discard
		// the session because ownership is ambiguous and advisory locks are session-scoped.
		discardSQLConnection(lockConn)
		return nil, fmt.Errorf("acquire backup migration lock: %w", err)
	}
	releaseLock := func() error {
		return releaseBackupMigrationLock(lockConn)
	}

	args := []string{
		"-h", d.cfg.Host,
		"-p", fmt.Sprintf("%d", d.cfg.Port),
		"-U", d.cfg.User,
		"-d", d.cfg.DBName,
		"--no-owner",
		"--no-acl",
		"--clean",
		"--if-exists",
	}

	commandContext := d.commandContext
	if commandContext == nil {
		commandContext = exec.CommandContext
	}
	cmd := commandContext(ctx, "pg_dump", args...)
	d.setCommandEnv(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, errors.Join(fmt.Errorf("create stdout pipe: %w", err), releaseLock())
	}

	if err := cmd.Start(); err != nil {
		_ = stdout.Close()
		return nil, errors.Join(fmt.Errorf("start pg_dump: %w", err), releaseLock())
	}

	return &cmdReadCloser{ReadCloser: stdout, cmd: cmd, release: releaseLock}, nil
}

func (d *PgDumper) setCommandEnv(cmd *exec.Cmd) {
	cmd.Env = append(cmd.Environ(), "PGTZ=UTC", "PGDATESTYLE=ISO, YMD")
	if d.cfg.Password != "" {
		cmd.Env = append(cmd.Environ(), "PGPASSWORD="+d.cfg.Password)
	}
	if d.cfg.SSLMode != "" {
		cmd.Env = append(cmd.Environ(), "PGSSLMODE="+d.cfg.SSLMode)
	}
}

func releaseBackupMigrationLock(conn *sql.Conn) error {
	unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := pgAdvisoryUnlock(unlockCtx, conn); err != nil {
		// A failed unlock must not return a possibly lock-owning session to the pool.
		discardSQLConnection(conn)
		return fmt.Errorf("release backup migration lock: %w", err)
	}
	if err := conn.Close(); err != nil {
		return fmt.Errorf("close backup migration lock connection: %w", err)
	}
	return nil
}

func discardSQLConnection(conn *sql.Conn) {
	_ = conn.Raw(func(any) error { return driver.ErrBadConn })
	_ = conn.Close()
}

// Restore validates the entire input before psql can commit any changes.
func (d *PgDumper) Restore(ctx context.Context, data io.Reader) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// A gzip/S3 reader can fail after yielding valid SQL. Piping it directly to
	// psql turns that failure into EOF, which may commit an incomplete restore.
	// Stage the complete SQL privately (CreateTemp uses 0600) before execution.
	script, err := os.CreateTemp("", "omnihive-restore-*.sql")
	if err != nil {
		return fmt.Errorf("create restore script: %w", err)
	}
	defer func() {
		_ = script.Close()
		_ = os.Remove(script.Name())
	}()
	usage := d.restoreDiskUsage
	if usage == nil {
		usage = disk.UsageWithContext
	}
	writer := &restoreScriptWriter{ctx: ctx, file: script, usage: usage}
	// Hide Reader.WriteTo so every chunk is bounded and capacity is rechecked
	// throughout decompression, even when the final SQL size is unknown.
	if _, err := io.CopyBuffer(writer, struct{ io.Reader }{data}, make([]byte, 1024*1024)); err != nil {
		return fmt.Errorf("read complete restore script: %w", err)
	}
	if err := script.Sync(); err != nil {
		return fmt.Errorf("sync restore script: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := script.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind restore script: %w", err)
	}
	args := []string{
		"-h", d.cfg.Host,
		"-p", fmt.Sprintf("%d", d.cfg.Port),
		"-U", d.cfg.User,
		"-d", d.cfg.DBName,
		"--single-transaction",
		"--no-psqlrc",
		"--no-password",
		"--set", "ON_ERROR_STOP=on",
		"--file", "-",
	}

	// --single-transaction requires -c or -f; explicitly read the dump from
	// stdin as a script and fail on SQL errors so a rollback cannot look like
	// a successful restore. Ignore local psqlrc files for deterministic behavior.
	commandContext := d.commandContext
	if commandContext == nil {
		commandContext = exec.CommandContext
	}
	cmd := commandContext(ctx, "psql", args...)
	d.setCommandEnv(cmd)

	cmd.Stdin = script

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, string(output))
	}
	return nil
}

// Keep a small reserve for other processes sharing the temporary filesystem.
// This is a best-effort check; actual write errors must still abort staging.
const restoreDiskReserveBytes = 16 * 1024 * 1024

type restoreScriptWriter struct {
	ctx   context.Context
	file  *os.File
	usage func(context.Context, string) (*disk.UsageStat, error)
}

func (w *restoreScriptWriter) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	usage, err := w.usage(w.ctx, filepath.Dir(w.file.Name()))
	if err != nil {
		return 0, fmt.Errorf("check restore disk space: %w", err)
	}
	if usage.Free < uint64(len(p))+restoreDiskReserveBytes {
		return 0, fmt.Errorf("insufficient disk space for restore script")
	}
	return w.file.Write(p)
}

// cmdReadCloser wraps a command stdout pipe and waits for the process on Close
type cmdReadCloser struct {
	io.ReadCloser
	cmd       *exec.Cmd
	release   func() error
	closeOnce sync.Once
	closeErr  error
}

func (c *cmdReadCloser) Close() error {
	c.closeOnce.Do(func() {
		var closeErrs []error
		if err := c.ReadCloser.Close(); err != nil {
			closeErrs = append(closeErrs, fmt.Errorf("close pg_dump stdout: %w", err))
		}
		if err := c.cmd.Wait(); err != nil {
			closeErrs = append(closeErrs, fmt.Errorf("pg_dump exited with error: %w", err))
		}
		if c.release != nil {
			if err := c.release(); err != nil {
				closeErrs = append(closeErrs, err)
			}
		}
		c.closeErr = errors.Join(closeErrs...)
	})
	return c.closeErr
}
