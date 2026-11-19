package repository

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/stretchr/testify/require"
)

func newTestPgDumper(t *testing.T, commandContext func(context.Context, string, ...string) *exec.Cmd) (*PgDumper, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return &PgDumper{
		cfg: &config.DatabaseConfig{
			Host:     "db.example.test",
			Port:     5432,
			User:     "sub2api",
			Password: "secret",
			DBName:   "sub2api",
			SSLMode:  "require",
		},
		db:             db,
		commandContext: commandContext,
	}, mock
}

func expectBackupMigrationLock(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(regexp.QuoteMeta("SELECT pg_try_advisory_lock($1)")).
		WithArgs(migrationsAdvisoryLockID).
		WillReturnRows(sqlmock.NewRows([]string{"pg_try_advisory_lock"}).AddRow(true))
}

func expectBackupMigrationUnlock(mock sqlmock.Sqlmock) {
	mock.ExpectExec(regexp.QuoteMeta("SELECT pg_advisory_unlock($1)")).
		WithArgs(migrationsAdvisoryLockID).
		WillReturnResult(sqlmock.NewResult(0, 1))
}

func TestPgDumperHoldsMigrationLockThroughReaderClose(t *testing.T) {
	var mock sqlmock.Sqlmock
	commandCreated := false
	dumper, createdMock := newTestPgDumper(t, func(ctx context.Context, name string, args ...string) *exec.Cmd {
		commandCreated = true
		require.Equal(t, "pg_dump", name)
		require.Contains(t, args, "--clean")
		require.NoError(t, mock.ExpectationsWereMet(), "migration lock must be acquired before pg_dump is created")
		return exec.CommandContext(ctx, "sh", "-c", "printf backup-data")
	})
	mock = createdMock
	expectBackupMigrationLock(mock)

	reader, err := dumper.Dump(context.Background())
	require.NoError(t, err)
	require.True(t, commandCreated)
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.Equal(t, "backup-data", string(data))

	expectBackupMigrationUnlock(mock)
	require.Error(t, mock.ExpectationsWereMet(), "migration lock was released before the reader closed")
	require.NoError(t, reader.Close())
	require.NoError(t, mock.ExpectationsWereMet())
	require.NoError(t, reader.Close(), "reader close must be idempotent")
}

func TestPgDumperReleasesMigrationLockWhenStdoutPipeSetupFails(t *testing.T) {
	dumper, mock := newTestPgDumper(t, func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, "sh", "-c", "true")
		cmd.Stdout = io.Discard
		return cmd
	})
	expectBackupMigrationLock(mock)
	expectBackupMigrationUnlock(mock)

	reader, err := dumper.Dump(context.Background())
	require.Nil(t, reader)
	require.ErrorContains(t, err, "create stdout pipe")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPgDumperReleasesMigrationLockWhenProcessStartFails(t *testing.T) {
	dumper, mock := newTestPgDumper(t, func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "/path/that/does/not/exist/pg_dump")
	})
	expectBackupMigrationLock(mock)
	expectBackupMigrationUnlock(mock)

	reader, err := dumper.Dump(context.Background())
	require.Nil(t, reader)
	require.ErrorContains(t, err, "start pg_dump")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPgDumperReleasesMigrationLockWhenProcessFails(t *testing.T) {
	dumper, mock := newTestPgDumper(t, func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "sh", "-c", "printf partial-backup; exit 7")
	})
	expectBackupMigrationLock(mock)

	reader, err := dumper.Dump(context.Background())
	require.NoError(t, err)
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.Equal(t, "partial-backup", string(data))
	expectBackupMigrationUnlock(mock)
	require.ErrorContains(t, reader.Close(), "pg_dump exited with error")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPgDumperReportsUnlockFailureAndDiscardsConnection(t *testing.T) {
	dumper, mock := newTestPgDumper(t, func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "sh", "-c", "printf backup-data")
	})
	expectBackupMigrationLock(mock)

	reader, err := dumper.Dump(context.Background())
	require.NoError(t, err)
	_, err = io.ReadAll(reader)
	require.NoError(t, err)
	mock.ExpectExec(regexp.QuoteMeta("SELECT pg_advisory_unlock($1)")).
		WithArgs(migrationsAdvisoryLockID).
		WillReturnError(errors.New("unlock unavailable"))
	require.ErrorContains(t, reader.Close(), "release backup migration lock")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPgDumperDoesNotStartProcessWhenMigrationLockFails(t *testing.T) {
	commandCreated := false
	dumper, mock := newTestPgDumper(t, func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		commandCreated = true
		return exec.CommandContext(ctx, "sh", "-c", "true")
	})
	mock.ExpectQuery(regexp.QuoteMeta("SELECT pg_try_advisory_lock($1)")).
		WithArgs(migrationsAdvisoryLockID).
		WillReturnError(errors.New("database unavailable"))

	reader, err := dumper.Dump(context.Background())
	require.Nil(t, reader)
	require.ErrorContains(t, err, "acquire backup migration lock")
	require.False(t, commandCreated)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPgDumperRejectsNilDatabase(t *testing.T) {
	dumper := &PgDumper{cfg: &config.DatabaseConfig{}}
	reader, err := dumper.Dump(context.Background())
	require.Nil(t, reader)
	require.ErrorContains(t, err, "nil sql db")
}

func TestPgDumperRestoreStreamsAtomicScript(t *testing.T) {
	var cmd *exec.Cmd
	dumper, _ := newTestPgDumper(t, func(ctx context.Context, name string, args ...string) *exec.Cmd {
		require.Equal(t, "psql", name)
		require.Contains(t, args, "--single-transaction")
		require.Contains(t, args, "--no-psqlrc")
		require.Contains(t, args, "--no-password")
		require.Contains(t, strings.Join(args, " "), "--set ON_ERROR_STOP=on --file -")
		cmd = exec.CommandContext(ctx, "sh", "-c", `input=$(cat); [ "$input" = 'SELECT 1;' ]`)
		return cmd
	})
	require.NoError(t, dumper.Restore(context.Background(), strings.NewReader("SELECT 1;")))
	require.Contains(t, cmd.Env, "PGPASSWORD=secret")
	require.Contains(t, cmd.Env, "PGSSLMODE=require")
}

func TestPgDumperRestoreReportsScriptFailure(t *testing.T) {
	dumper, _ := newTestPgDumper(t, func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "sh", "-c", "cat >/dev/null; printf 'restore failed' >&2; exit 3")
	})
	err := dumper.Restore(context.Background(), strings.NewReader("invalid SQL"))
	require.ErrorContains(t, err, "restore failed")
	require.ErrorContains(t, err, "exit status 3")
}

func TestPgDumperRestoreRejectsIncompleteInputBeforeStarting(t *testing.T) {
	started := false
	dumper, _ := newTestPgDumper(t, func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		started = true
		return exec.CommandContext(ctx, "sh", "-c", "cat >/dev/null")
	})
	input := io.MultiReader(strings.NewReader("DROP TABLE important_data;\n"), iotest.ErrReader(io.ErrUnexpectedEOF))
	err := dumper.Restore(context.Background(), input)
	require.ErrorContains(t, err, "read complete restore script")
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	require.False(t, started, "partial SQL must never reach psql")
}

func TestPgDumperRestoreStagingFailures(t *testing.T) {
	for _, scenario := range []string{"read", "disk_full", "disk_check", "disk_fills_midstream", "cancelled", "temp_create"} {
		t.Run(scenario, func(t *testing.T) {
			tempDir := t.TempDir()
			t.Setenv("TMPDIR", tempDir)
			t.Setenv("TMP", tempDir)
			t.Setenv("TEMP", tempDir)
			started := false
			dumper, _ := newTestPgDumper(t, func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
				started = true
				return exec.CommandContext(ctx, "sh", "-c", "true")
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var input io.Reader = strings.NewReader("SELECT 1;")
			switch scenario {
			case "read":
				input = io.MultiReader(input, iotest.ErrReader(io.ErrUnexpectedEOF))
			case "disk_full", "disk_check":
				dumper.restoreDiskUsage = func(context.Context, string) (*disk.UsageStat, error) {
					if scenario == "disk_check" {
						return nil, os.ErrPermission
					}
					return &disk.UsageStat{Free: restoreDiskReserveBytes}, nil
				}
			case "cancelled":
				input = &cancelRestoreReader{cancel: cancel}
			case "disk_fills_midstream":
				input = strings.NewReader(strings.Repeat(" ", 2*1024*1024))
				checks := 0
				dumper.restoreDiskUsage = func(context.Context, string) (*disk.UsageStat, error) {
					checks++
					if checks == 1 {
						return &disk.UsageStat{Free: 1024 * 1024 * 1024}, nil
					}
					return &disk.UsageStat{}, nil
				}
			case "temp_create":
				// A regular file cannot be used as a temporary directory.
				file := tempDir + "/not-a-directory"
				require.NoError(t, os.WriteFile(file, nil, 0600))
				t.Setenv("TMPDIR", file)
				t.Setenv("TMP", file)
				t.Setenv("TEMP", file)
			}
			err := dumper.Restore(ctx, input)
			require.Error(t, err)
			require.False(t, started)
			if scenario == "disk_full" || scenario == "disk_fills_midstream" {
				require.ErrorContains(t, err, "insufficient disk space")
			}
			if scenario == "disk_check" {
				require.ErrorIs(t, err, os.ErrPermission)
			}
			if scenario == "cancelled" {
				require.ErrorIs(t, err, context.Canceled)
			}
			entries, err := os.ReadDir(tempDir)
			require.NoError(t, err)
			for _, entry := range entries {
				require.NotContains(t, entry.Name(), "omnihive-restore-")
			}
		})
	}
}

type cancelRestoreReader struct{ cancel context.CancelFunc }

func (r *cancelRestoreReader) Read(p []byte) (int, error) {
	r.cancel()
	return copy(p, "SELECT 1;"), io.EOF
}

func TestPgDumperRestorePrivateTemporaryFileAndCleanup(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			var scriptPath string
			dumper, _ := newTestPgDumper(t, func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
				if fail {
					return exec.CommandContext(ctx, "/missing/psql")
				}
				return exec.CommandContext(ctx, "sh", "-c", "cat >/dev/null")
			})
			dumper.restoreDiskUsage = func(_ context.Context, path string) (*disk.UsageStat, error) {
				entries, err := os.ReadDir(path)
				require.NoError(t, err)
				for _, entry := range entries {
					if strings.HasPrefix(entry.Name(), "omnihive-restore-") {
						scriptPath = path + "/" + entry.Name()
						info, err := entry.Info()
						require.NoError(t, err)
						if runtime.GOOS != "windows" {
							require.Equal(t, os.FileMode(0600), info.Mode().Perm())
						}
					}
				}
				return &disk.UsageStat{Free: 1024 * 1024 * 1024}, nil
			}
			tempDir := t.TempDir()
			t.Setenv("TMPDIR", tempDir)
			t.Setenv("TMP", tempDir)
			t.Setenv("TEMP", tempDir)
			err := dumper.Restore(context.Background(), strings.NewReader("SELECT 1;"))
			if fail {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.NotEmpty(t, scriptPath)
			_, err = os.Stat(scriptPath)
			require.ErrorIs(t, err, os.ErrNotExist)
		})
	}
}
