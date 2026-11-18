//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// A transient SELECT failure must not turn a read-modify-write into replacement
// of the entire backup inventory. The settings writer may still be available.
func TestBackupMetadataReadFailurePreservesInventory(t *testing.T) {
	for _, readErr := range []error{errors.New("database unavailable"), context.Canceled, context.DeadlineExceeded} {
		t.Run(readErr.Error(), func(t *testing.T) {
			repo := newMockSettingRepo()
			original := `[{"id":"retained","status":"completed","s3_key":"retained.sql.gz"}]`
			repo.data[settingKeyBackupRecords] = original
			repo.getValueErr = readErr
			svc := newTestBackupService(repo, &mockDumper{}, newMockObjectStore())
			_, err := svc.ListBackups(context.Background())
			require.ErrorIs(t, err, readErr)
			require.ErrorIs(t, svc.saveRecord(context.Background(), &BackupRecord{ID: "new"}), readErr)
			require.Equal(t, original, repo.data[settingKeyBackupRecords])
		})
	}
}

func TestBackupMetadataCorruptionRejectsStart(t *testing.T) {
	repo := newMockSettingRepo()
	seedS3Config(t, repo)
	const original = `[{"id":"retained",`
	repo.data[settingKeyBackupRecords] = original
	store := newMockObjectStore()
	svc := newTestBackupService(repo, &mockDumper{}, store)
	t.Cleanup(svc.Stop)
	_, err := svc.StartBackup(context.Background(), "manual", 0)
	require.ErrorIs(t, err, ErrBackupRecordsCorrupt)
	require.Equal(t, original, repo.data[settingKeyBackupRecords])
	require.False(t, svc.backingUp)
	require.Empty(t, store.objects)
}

func TestBackupMetadataMissingSettingAllowsFirstRecord(t *testing.T) {
	repo := newMockSettingRepo()
	repo.getValueErr = fmt.Errorf("missing: %w", ErrSettingNotFound)
	svc := newTestBackupService(repo, &mockDumper{}, newMockObjectStore())
	require.NoError(t, svc.saveRecord(context.Background(), &BackupRecord{ID: "first"}))
	repo.getValueErr = nil
	records, err := svc.ListBackups(context.Background())
	require.NoError(t, err)
	require.Len(t, records, 1)
}

func TestBackupMetadataRetainsMoreThan100RecoverableRecords(t *testing.T) {
	repo := newMockSettingRepo()
	svc := newTestBackupService(repo, &mockDumper{}, newMockObjectStore())
	for i := 0; i < 105; i++ {
		require.NoError(t, svc.saveRecord(context.Background(), &BackupRecord{
			ID: fmt.Sprint(i), Status: "completed", S3Key: fmt.Sprintf("retained/%d.sql.gz", i),
		}))
	}
	records, err := svc.ListBackups(context.Background())
	require.NoError(t, err)
	require.Len(t, records, 105, "retention must remove objects and records together")
	first, err := svc.GetBackupRecord(context.Background(), "0")
	require.NoError(t, err)
	require.Equal(t, "retained/0.sql.gz", first.S3Key)
}

type completionFailSettingRepo struct {
	SettingRepository
	err error
}

func (r *completionFailSettingRepo) Set(ctx context.Context, key, value string) error {
	if key == settingKeyBackupRecords {
		var records []BackupRecord
		if err := json.Unmarshal([]byte(value), &records); err != nil {
			return err
		}
		for _, record := range records {
			if record.Status == "completed" {
				return r.err
			}
		}
	}
	return r.SettingRepository.Set(ctx, key, value)
}

func TestBackupMetadataCompletionWriteFailureIsReturned(t *testing.T) {
	repo := newMockSettingRepo()
	seedS3Config(t, repo)
	svc := newTestBackupService(repo, &mockDumper{dumpData: []byte("SELECT 1;")}, newMockObjectStore())
	failure := errors.New("completion write failed")
	svc.settingRepo = &completionFailSettingRepo{SettingRepository: repo, err: failure}
	record, err := svc.CreateBackup(context.Background(), "manual", 0)
	require.ErrorIs(t, err, failure)
	require.NotNil(t, record, "caller retains the uploaded object's identity")
	stored, err := svc.GetBackupRecord(context.Background(), record.ID)
	require.NoError(t, err)
	require.Equal(t, "running", stored.Status)
}
