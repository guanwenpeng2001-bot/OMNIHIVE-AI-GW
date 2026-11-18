//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type scheduleReadErrorRepo struct {
	SettingRepository
	err error
}

func (r *scheduleReadErrorRepo) GetValue(ctx context.Context, key string) (string, error) {
	if key == settingKeyBackupSchedule {
		return "", r.err
	}
	return r.SettingRepository.GetValue(ctx, key)
}

func TestBackupRetentionScheduleErrorsStopScheduledWork(t *testing.T) {
	for _, corrupt := range []bool{false, true} {
		name := "read error"
		if corrupt {
			name = "corrupt JSON"
		}
		t.Run(name, func(t *testing.T) {
			repo := newMockSettingRepo()
			seedS3Config(t, repo)
			store := newMockObjectStore()
			svc := newTestBackupService(repo, &mockDumper{dumpData: []byte("SELECT 1;")}, store)
			if corrupt {
				repo.data[settingKeyBackupSchedule] = "{broken"
			} else {
				svc.settingRepo = &scheduleReadErrorRepo{SettingRepository: repo, err: errors.New("read failed")}
			}
			_, err := svc.GetSchedule(context.Background())
			require.Error(t, err)
			svc.runScheduledBackup()
			require.Empty(t, store.objects)
			require.Empty(t, repo.data[settingKeyBackupRecords])
		})
	}
}

func TestBackupRetentionScheduledZeroDaysIsPermanent(t *testing.T) {
	repo := newMockSettingRepo()
	seedS3Config(t, repo)
	repo.data[settingKeyBackupSchedule] = `{"enabled":true,"retain_days":0,"retain_count":0}`
	svc := newTestBackupService(repo, &mockDumper{dumpData: []byte("SELECT 1;")}, newMockObjectStore())
	svc.runScheduledBackup()
	records, err := svc.ListBackups(context.Background())
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, "completed", records[0].Status)
	require.Empty(t, records[0].ExpiresAt)
}

func TestBackupRetentionFailedAndPendingDoNotConsumeCompletedQuota(t *testing.T) {
	repo := newMockSettingRepo()
	svc := newTestBackupService(repo, &mockDumper{}, newMockObjectStore())
	now := time.Now()
	for i, record := range []BackupRecord{
		{ID: "failed", Status: "failed"},
		{ID: "pending", Status: "running"},
		{ID: "keep", Status: "completed"},
		{ID: "expire", Status: "completed"},
	} {
		record.StartedAt = now.Add(-time.Duration(i) * time.Hour).Format(time.RFC3339)
		require.NoError(t, svc.saveRecord(context.Background(), &record))
	}
	require.NoError(t, svc.cleanupOldBackups(context.Background(), &BackupScheduleConfig{RetainCount: 1}))
	_, err := svc.GetBackupRecord(context.Background(), "keep")
	require.NoError(t, err)
	_, err = svc.GetBackupRecord(context.Background(), "expire")
	require.ErrorIs(t, err, ErrBackupNotFound)
	records, err := svc.ListBackups(context.Background())
	require.NoError(t, err)
	require.Len(t, records, 3)
}

func TestBackupRetentionOrdersOffsetTimestampsByInstant(t *testing.T) {
	repo := newMockSettingRepo()
	svc := newTestBackupService(repo, &mockDumper{}, newMockObjectStore())
	for _, record := range []BackupRecord{
		{ID: "older", Status: "completed", StartedAt: "2026-01-01T12:00:00+08:00"},
		{ID: "newer", Status: "completed", StartedAt: "2026-01-01T06:00:00Z"},
	} {
		require.NoError(t, svc.saveRecord(context.Background(), &record))
	}
	records, err := svc.ListBackups(context.Background())
	require.NoError(t, err)
	require.Equal(t, "newer", records[0].ID)
	require.NoError(t, svc.cleanupOldBackups(context.Background(), &BackupScheduleConfig{RetainCount: 1}))
	_, err = svc.GetBackupRecord(context.Background(), "newer")
	require.NoError(t, err)
}

func TestBackupRetentionOrderIsTransitiveWithLegacyDates(t *testing.T) {
	records := []BackupRecord{
		{ID: "a", StartedAt: "2026-01-01T12:00:00+08:00"},
		{ID: "b", StartedAt: "2026-01-01T06:00:00Z"},
		{ID: "c", StartedAt: "2026-01-01T08:broken"},
		{ID: "d", StartedAt: ""},
		{ID: "e", StartedAt: "2026-01-01T04:00:00Z"},
	}
	for _, a := range records {
		require.False(t, backupStartedAfter(a, a))
		for _, b := range records {
			if !backupStartedAfter(a, b) {
				continue
			}
			require.False(t, backupStartedAfter(b, a))
			for _, c := range records {
				if backupStartedAfter(b, c) {
					require.True(t, backupStartedAfter(a, c))
				}
			}
		}
	}
}

func TestBackupRetentionMissingScheduleKeepsLegacyDefaults(t *testing.T) {
	repo := newMockSettingRepo()
	repo.getValueErr = ErrSettingNotFound
	svc := newTestBackupService(repo, &mockDumper{}, newMockObjectStore())
	schedule, err := svc.GetSchedule(context.Background())
	require.NoError(t, err)
	require.Equal(t, &BackupScheduleConfig{}, schedule)
}
