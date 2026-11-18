//go:build integration

package datamanagementintegration

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/Wei-Shaw/sub2api/ent"
	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// The full pg_dump/psql cycle is destructive to this dedicated fixture only.
// Reject the endpoint before opening a connection, then verify database identity.
func TestBackupDataPostgres(t *testing.T) {
	dsn := os.Getenv("BACKUP_DATA_TEST_DSN")
	if dsn == "" {
		t.Skip("requires a dedicated backup_data_test PostgreSQL and pg_dump/psql")
	}
	u, err := url.Parse(dsn)
	require.NoError(t, err)
	require.Contains(t, []string{"postgres", "postgresql"}, u.Scheme)
	require.Contains(t, []string{"127.0.0.1:55480", "127.0.0.1:5432"}, u.Host)
	require.Equal(t, "/backup_data_test", u.Path)
	require.Equal(t, "sslmode=disable", u.RawQuery)
	require.Empty(t, u.Fragment)
	require.NotNil(t, u.User)
	for _, key := range []string{"PGHOSTADDR", "PGSERVICE", "PGSERVICEFILE", "PGOPTIONS"} {
		require.Empty(t, os.Getenv(key), "connection overrides are not supported")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	var name string
	require.NoError(t, db.QueryRowContext(ctx, "SELECT current_database()").Scan(&name))
	require.Equal(t, "backup_data_test", name)
	require.NoError(t, repository.ApplyMigrations(ctx, db))
	require.NoError(t, repository.ApplyMigrations(ctx, db))
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	repo := repository.NewSettingRepository(client)
	port, err := strconv.Atoi(u.Port())
	require.NoError(t, err)
	databasePassword, _ := u.User.Password()
	cfg := &config.Config{
		Database: config.DatabaseConfig{Host: u.Hostname(), Port: port, User: u.User.Username(), Password: databasePassword, DBName: name, SSLMode: "disable"},
		Totp:     config.TotpConfig{EncryptionKeyConfigured: true},
	}
	store := &memoryStore{objects: make(map[string][]byte)}
	dumper := repository.NewPgDumper(cfg, db)
	newService := func() *service.BackupService {
		return service.NewBackupService(repo, cfg, fixtureEncryptor{}, func(context.Context, *service.BackupS3Config) (service.BackupObjectStore, error) {
			return store, nil
		}, dumper)
	}
	svc := newService()
	t.Cleanup(svc.Stop)
	_, err = svc.UpdateS3Config(ctx, service.BackupS3Config{Bucket: "memory", AccessKeyID: "synthetic", SecretAccessKey: "synthetic"})
	require.NoError(t, err)
	const recordsKey = "backup_records"
	t.Run("corrupt inventory rejects async backup without overwrite", func(t *testing.T) {
		const corrupt = `[{"id":"keep",`
		require.NoError(t, repo.Set(ctx, recordsKey, corrupt))
		_, err := svc.StartBackup(ctx, "manual", 0)
		require.ErrorIs(t, err, service.ErrBackupRecordsCorrupt)
		raw, err := repo.GetValue(ctx, recordsKey)
		require.NoError(t, err)
		require.Equal(t, corrupt, raw)
		require.Empty(t, store.objects)
	})
	t.Run("canceled SQL read is surfaced", func(t *testing.T) {
		canceled, stop := context.WithCancel(ctx)
		stop()
		_, err := svc.ListBackups(canceled)
		require.Error(t, err)
	})
	t.Run("SQL completion write failure reaches caller", func(t *testing.T) {
		require.NoError(t, repo.Set(ctx, recordsKey, "[]"))
		_, err := db.ExecContext(ctx, `CREATE OR REPLACE FUNCTION backup_fixture_reject_completed() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
		 IF NEW.key = 'backup_records' AND EXISTS (SELECT 1 FROM jsonb_array_elements(NEW.value::jsonb) r WHERE r->>'status' = 'completed') THEN
		  RAISE EXCEPTION 'synthetic completion failure';
		 END IF;
		 RETURN NEW;
		END $$;
		CREATE TRIGGER backup_fixture_completion BEFORE INSERT OR UPDATE ON settings FOR EACH ROW EXECUTE FUNCTION backup_fixture_reject_completed()`)
		require.NoError(t, err)
		t.Cleanup(func() {
			_, err := db.ExecContext(context.Background(), "DROP TRIGGER IF EXISTS backup_fixture_completion ON settings; DROP FUNCTION IF EXISTS backup_fixture_reject_completed()")
			require.NoError(t, err)
		})
		record, err := svc.CreateBackup(ctx, "manual", 0)
		require.ErrorContains(t, err, "synthetic completion failure")
		require.NotNil(t, record)
		persisted, err := svc.GetBackupRecord(ctx, record.ID)
		require.NoError(t, err)
		require.Equal(t, "running", persisted.Status)
	})
	t.Run("more than 100 records survive backup restart and restore", func(t *testing.T) {
		seed := make([]service.BackupRecord, 105)
		for i := range seed {
			seed[i] = service.BackupRecord{ID: fmt.Sprintf("legacy-%d", i), Status: "completed", S3Key: fmt.Sprintf("legacy/%d.sql.gz", i)}
		}
		data, err := json.Marshal(seed)
		require.NoError(t, err)
		require.NoError(t, repo.Set(ctx, recordsKey, string(data)))
		require.NoError(t, repo.Set(ctx, "backup_fixture_marker", "before dump"))
		var checksums string
		require.NoError(t, db.QueryRowContext(ctx, "SELECT string_agg(filename || ':' || checksum, ',' ORDER BY filename) FROM schema_migrations").Scan(&checksums))
		record, err := svc.CreateBackup(ctx, "manual", 0)
		require.NoError(t, err)
		restarted := newService()
		t.Cleanup(restarted.Stop)
		records, err := restarted.ListBackups(ctx)
		require.NoError(t, err)
		require.Len(t, records, 106)
		_, err = restarted.GetBackupDownloadURL(ctx, "legacy-0")
		require.NoError(t, err, "legacy single-file keys retain their download contract")
		require.NoError(t, repo.Set(ctx, "backup_fixture_marker", "after dump"))
		require.NoError(t, restarted.RestoreBackup(ctx, record.ID))
		marker, err := repo.GetValue(ctx, "backup_fixture_marker")
		require.NoError(t, err)
		require.Equal(t, "before dump", marker)
		records, err = restarted.ListBackups(ctx)
		require.NoError(t, err)
		require.Len(t, records, 105, "dump predates registration of its own backup record")
		require.NoError(t, repository.ApplyMigrations(ctx, db))
		var after string
		require.NoError(t, db.QueryRowContext(ctx, "SELECT string_agg(filename || ':' || checksum, ',' ORDER BY filename) FROM schema_migrations").Scan(&after))
		require.Equal(t, checksums, after)
	})
	t.Run("failed SQL restore rolls back atomically", func(t *testing.T) {
		err := dumper.Restore(ctx, strings.NewReader("UPDATE settings SET value='wrong' WHERE key='backup_fixture_marker'; SELECT * FROM backup_fixture_missing_table;"))
		require.Error(t, err)
		value, err := repo.GetValue(ctx, "backup_fixture_marker")
		require.NoError(t, err)
		require.Equal(t, "before dump", value)
	})
	t.Run("schedule errors surface with real repository", func(t *testing.T) {
		require.NoError(t, repo.Set(ctx, "backup_schedule", "{broken"))
		_, err := svc.GetSchedule(ctx)
		require.Error(t, err)
		canceled, stop := context.WithCancel(ctx)
		stop()
		_, err = svc.GetSchedule(canceled)
		require.Error(t, err)
	})
	t.Run("cron preserves permanent backups and counts completed records", func(t *testing.T) {
		seed := []service.BackupRecord{
			{ID: "recent-failure", Status: "failed", StartedAt: time.Now().Format(time.RFC3339)},
			{ID: "older", Status: "completed", StartedAt: "2026-01-01T12:00:00+08:00"},
			{ID: "newer", Status: "completed", StartedAt: "2026-01-01T06:00:00Z"},
		}
		data, err := json.Marshal(seed)
		require.NoError(t, err)
		require.NoError(t, repo.Set(ctx, recordsKey, string(data)))
		require.NoError(t, repo.Set(ctx, "backup_schedule", `{"enabled":true,"cron_expr":"* * * * *","retain_days":0,"retain_count":2}`))
		scheduled := newService()
		scheduled.Start()
		t.Cleanup(scheduled.Stop)
		require.Eventually(t, func() bool {
			records, err := scheduled.ListBackups(ctx)
			if err != nil {
				return false
			}
			for _, record := range records {
				if record.TriggeredBy == "scheduled" && record.Status == "completed" {
					_, err = scheduled.GetBackupRecord(ctx, "older")
					return err != nil
				}
			}
			return false
		}, 75*time.Second, 100*time.Millisecond)
		scheduled.Stop()
		records, err := scheduled.ListBackups(ctx)
		require.NoError(t, err)
		require.Len(t, records, 3)
		_, err = scheduled.GetBackupRecord(ctx, "newer")
		require.NoError(t, err)
		for _, record := range records {
			if record.TriggeredBy == "scheduled" {
				require.Empty(t, record.ExpiresAt)
			}
		}
		_, err = scheduled.UpdateSchedule(ctx, service.BackupScheduleConfig{})
		require.NoError(t, err)
	})
}

// Object I/O stays in memory while database I/O uses real PG and pg_dump/psql.
type memoryStore struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func (s *memoryStore) Upload(_ context.Context, key string, r io.Reader, _ string) (int64, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects[key] = data
	return int64(len(data)), nil
}
func (s *memoryStore) UploadFile(ctx context.Context, key, path, contentType string) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	return s.Upload(ctx, key, f, contentType)
}
func (s *memoryStore) Download(_ context.Context, key string) (io.ReadCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.objects[key]
	if !ok {
		return nil, fmt.Errorf("missing fixture object")
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}
func (s *memoryStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.objects, key)
	return nil
}
func (*memoryStore) PresignURL(_ context.Context, key string, _ time.Duration) (string, error) {
	return "https://fixture.invalid/" + key, nil
}
func (*memoryStore) HeadBucket(context.Context) error { return nil }

type fixtureEncryptor struct{}

func (fixtureEncryptor) Encrypt(s string) (string, error) { return "fixture:" + s, nil }
func (fixtureEncryptor) Decrypt(s string) (string, error) {
	return strings.TrimPrefix(s, "fixture:"), nil
}
