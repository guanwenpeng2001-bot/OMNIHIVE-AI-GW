//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestBackupService_SaveRecordDB_StaleWorkerCannotResurrectTombstone(t *testing.T) {
	repo := newMockSettingRepo()
	svc := newTestBackupService(repo, &mockDumper{}, newMockObjectStore())
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	svc.SetLeaderLock(nil, db)

	// A terminal deletion increments revision and retains the row. The late
	// completion sees zero matching rows and must not recreate it through an
	// upsert or overwrite its tombstone.
	mock.ExpectExec(`(?s)UPDATE backup_jobs.*WHERE id=\$10 AND attempt_id=\$11 AND revision=\$12 AND status <> 'deleted'`).
		WillReturnResult(sqlmock.NewResult(0, 0))
	record := &BackupRecord{ID: "backup-1", Status: "completed", AttemptID: "attempt-1", Revision: 2}
	require.ErrorIs(t, svc.saveRecordDB(context.Background(), record), ErrBackupRecordStale)
	require.Equal(t, int64(2), record.Revision)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestBackupService_SaveRecordDB_CreateConflictDoesNotUpsertTombstone(t *testing.T) {
	repo := newMockSettingRepo()
	svc := newTestBackupService(repo, &mockDumper{}, newMockObjectStore())
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	svc.SetLeaderLock(nil, db)

	mock.ExpectExec(`(?s)INSERT INTO backup_jobs.*ON CONFLICT \(id\) DO NOTHING`).
		WillReturnResult(sqlmock.NewResult(0, 0))
	record := &BackupRecord{ID: "backup-1", Status: "completed"}
	require.ErrorIs(t, svc.saveRecordDB(context.Background(), record), ErrBackupRecordStale)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestBackupService_SchedulerFollowerKeepsGlobalHealthAndWatermarks(t *testing.T) {
	repo := newMockSettingRepo()
	seedS3Config(t, repo)
	svc := newTestBackupService(repo, &mockDumper{dumpData: []byte("data")}, newMockObjectStore())
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	lastSuccess := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	svc.setSchedulerRuntime(context.Background(), func(runtime *BackupSchedulerRuntime) {
		runtime.Health = "healthy"
		runtime.LastSuccessAt = lastSuccess
		runtime.LastVerifiedAt = lastSuccess
	})
	cache := &fakeLeaderLockCache{}
	peerRelease, acquired := tryAcquireSingletonLeaderLock(context.Background(), cache, nil, backupScheduledLeaderLockKey, "leader", time.Minute)
	require.True(t, acquired)
	defer peerRelease()
	svc.SetLeaderLock(cache, db)

	// sqlmock has no expected writes: a follower must leave the shared row
	// untouched rather than persisting skipped_not_leader/degraded status.
	svc.runScheduledBackup()
	require.Equal(t, "healthy", svc.schedulerRuntime.Health)
	require.Equal(t, lastSuccess, svc.schedulerRuntime.LastSuccessAt)
	require.Equal(t, lastSuccess, svc.schedulerRuntime.LastVerifiedAt)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestBackupService_SchedulerGlobalPersistenceMergesWatermarksMonotonically(t *testing.T) {
	repo := newMockSettingRepo()
	svc := newTestBackupService(repo, &mockDumper{}, newMockObjectStore())
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	svc.SetLeaderLock(nil, db)

	// The SQL keeps the greatest success/verification timestamps and only lets
	// the newest attempt replace health/error metadata.
	mock.ExpectExec(`(?s)INSERT INTO backup_scheduler_state.*last_attempt_at = CASE.*last_success_at = CASE.*last_verified_at = CASE.*last_error = CASE`).
		WillReturnResult(sqlmock.NewResult(1, 1))
	svc.setSchedulerRuntimeGlobal(context.Background(), func(runtime *BackupSchedulerRuntime) {
		runtime.LastAttemptAt = "2026-11-23T00:00:00Z"
		runtime.LastSuccessAt = "2026-11-22T00:00:00Z"
		runtime.LastVerifiedAt = "2026-11-22T00:00:00Z"
		runtime.Health = "healthy"
	})
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestBackupService_OmittedSecretPropagatesLoadFailureWithoutWritingEmptyConfig(t *testing.T) {
	for _, loadFailure := range []struct {
		name  string
		apply func(*mockSettingRepo, *BackupService)
		want  string
	}{
		{
			name: "legacy config read",
			apply: func(repo *mockSettingRepo, _ *BackupService) {
				repo.getValueErr = errors.New("legacy secret unavailable")
			},
			want: "legacy secret unavailable",
		},
		{
			name: "envelope read",
			apply: func(_ *mockSettingRepo, svc *BackupService) {
				svc.secretPayloadRepo = &memoryBackupSecretPayloadRepo{payloads: make(map[string]BackupSecretEnvelope), getErr: errors.New("envelope unavailable")}
			},
			want: "envelope unavailable",
		},
	} {
		t.Run(loadFailure.name, func(t *testing.T) {
			repo := newMockSettingRepo()
			svc := newTestBackupService(repo, &mockDumper{}, newMockObjectStore())
			loadFailure.apply(repo, svc)

			_, err := svc.UpdateS3Config(context.Background(), BackupS3Config{
				Endpoint:    "https://changed.example.com",
				Bucket:      "changed-bucket",
				AccessKeyID: "changed-key",
			})
			require.ErrorContains(t, err, loadFailure.want)
			require.Empty(t, repo.data, "a failed inherited-secret read must not write an empty credential config")

			err = svc.TestS3Connection(context.Background(), BackupS3Config{
				Bucket:      "changed-bucket",
				AccessKeyID: "changed-key",
			})
			require.ErrorContains(t, err, loadFailure.want)
			require.Empty(t, repo.data)
		})
	}
}
