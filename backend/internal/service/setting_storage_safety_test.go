//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/stretchr/testify/require"
)

func TestStorageSettingsDatabaseFailureNeverMeansMissing(t *testing.T) {
	ctx := t.Context()
	repo := newMockSettingRepo()
	backup := newTestBackupService(repo, nil, nil)
	fallback := config.ImageStorageConfig{Enabled: true, Bucket: "fallback", AccessKeyID: "id", SecretAccessKey: "secret"}
	images := NewImageStorageSettingService(repo, &plainEncryptor{}, backup, nil, fallback)
	offline := errors.New("database unavailable")
	repo.getValueErr = offline
	_, err := backup.loadS3Config(ctx)
	require.ErrorIs(t, err, offline)
	_, err = images.effectiveConfig(ctx)
	require.ErrorIs(t, err, offline)
	require.False(t, images.SecretConfigured(ctx))
	_, err = backup.UpdateS3Config(ctx, BackupS3Config{Bucket: "updated"})
	require.ErrorIs(t, err, offline)
	_, err = images.Update(ctx, ImageStorageSettings{Bucket: "updated"})
	require.ErrorIs(t, err, offline)
	require.Empty(t, repo.data)
	repo.getValueErr = ErrSettingNotFound
	got, err := images.effectiveConfig(ctx)
	require.NoError(t, err)
	require.Equal(t, fallback, *got)
}

func TestStoragePersistedSecretFailsClosedAndCanBeRepaired(t *testing.T) {
	ctx := t.Context()
	repo := newMockSettingRepo()
	backup := newTestBackupService(repo, nil, nil)
	raw, err := json.Marshal(BackupS3Config{Bucket: "bucket", AccessKeyID: "id", SecretAccessKey: "not-a-ciphertext"})
	require.NoError(t, err)
	require.NoError(t, repo.Set(ctx, settingKeyBackupS3Config, string(raw)))
	_, err = backup.loadS3Config(ctx)
	require.ErrorIs(t, err, ErrStorageSecretInvalid)
	view, err := backup.GetS3Config(ctx)
	require.NoError(t, err)
	require.Equal(t, "unreadable", view.SecretStatus)
	require.Empty(t, view.SecretAccessKey)
	_, err = backup.UpdateS3Config(ctx, BackupS3Config{Bucket: "bucket", AccessKeyID: "id", SecretAccessKey: "replacement"})
	require.NoError(t, err)
	loaded, err := backup.loadS3Config(ctx)
	require.NoError(t, err)
	require.Equal(t, "replacement", loaded.SecretAccessKey)
}

func TestImageConnectionRequestPlaintextIsNotStoredCiphertext(t *testing.T) {
	ctx := t.Context()
	svc, repo, built := newImageStorageFixture(t, config.ImageStorageConfig{})
	input := ImageStorageSettings{Enabled: true, Bucket: "bucket", AccessKeyID: "id", SecretAccessKey: "request-secret"}
	require.NoError(t, svc.TestConnection(ctx, input))
	require.Equal(t, "request-secret", (*built)[0].SecretAccessKey)
	require.Empty(t, repo.values)
	raw, err := json.Marshal(input)
	require.NoError(t, err)
	require.NoError(t, repo.Set(ctx, settingKeyImageStorageConfig, string(raw)))
	_, err = svc.effectiveConfig(ctx)
	require.ErrorIs(t, err, ErrStorageSecretInvalid)
	input.SecretAccessKey = ""
	require.ErrorIs(t, svc.TestConnection(ctx, input), ErrStorageSecretInvalid)
	require.Len(t, *built, 1)
}

func TestImageSettingsFailureRecoversAndExternalChangeExpires(t *testing.T) {
	ctx := t.Context()
	repo := newMockSettingRepo()
	backup := newTestBackupService(repo, nil, nil)
	builds := 0
	svc := NewImageStorageSettingService(repo, &plainEncryptor{}, backup, func(context.Context, *config.ImageStorageConfig) (ImageStorage, error) {
		builds++
		return &recordingStorage{}, nil
	}, config.ImageStorageConfig{Enabled: true, Bucket: "bucket", AccessKeyID: "id", SecretAccessKey: "secret"})
	repo.getValueErr = errors.New("temporary outage")
	_, enabled := svc.resolve()
	require.False(t, enabled)
	repo.getValueErr = nil
	svc.expiresAt = time.Time{}
	_, enabled = svc.resolve()
	require.True(t, enabled)
	require.Equal(t, 1, builds)
	raw, err := json.Marshal(ImageStorageSettings{Enabled: false})
	require.NoError(t, err)
	require.NoError(t, repo.Set(ctx, settingKeyImageStorageConfig, string(raw)))
	svc.expiresAt = time.Time{}
	_, enabled = svc.resolve()
	require.False(t, enabled, "a change saved by another instance must not remain cached forever")
}

func TestBackupCredentialChangeInvalidatesDependentImages(t *testing.T) {
	ctx := t.Context()
	svc, _, built := newImageStorageFixture(t, config.ImageStorageConfig{})
	_, err := svc.backup.UpdateS3Config(ctx, BackupS3Config{Bucket: "bucket", AccessKeyID: "id", SecretAccessKey: "first"})
	require.NoError(t, err)
	_, err = svc.Update(ctx, ImageStorageSettings{Enabled: true, ReuseBackupS3: true})
	require.NoError(t, err)
	_, enabled := svc.resolve()
	require.True(t, enabled)
	_, err = svc.backup.UpdateS3Config(ctx, BackupS3Config{Bucket: "bucket", AccessKeyID: "id", SecretAccessKey: "second"})
	require.NoError(t, err)
	_, enabled = svc.resolve()
	require.True(t, enabled)
	require.Len(t, *built, 2)
	require.Equal(t, "second", (*built)[1].SecretAccessKey)
}

func TestIntegrationSecretVersionKeepsLegacyReadableWithoutRewrite(t *testing.T) {
	ctx := t.Context()
	repo := newMockSettingRepo()
	legacy := &plainEncryptor{}
	cfg := &config.Config{Totp: config.TotpConfig{EncryptionKeyConfigured: true}, Secrets: config.SecretsConfig{EncryptionKey: strings.Repeat("a1", 32)}}
	codec := newSettingSecretEncryptor(cfg, legacy)
	oldValue, err := legacy.Encrypt("old")
	require.NoError(t, err)
	plain, err := codec.Decrypt(oldValue)
	require.NoError(t, err)
	require.Equal(t, "old", plain)
	sealed, err := codec.Encrypt("new")
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(sealed, settingSecretPrefix))
	plain, err = codec.Decrypt(sealed)
	require.NoError(t, err)
	require.Equal(t, "new", plain)
	wrong := newSettingSecretEncryptor(&config.Config{Secrets: config.SecretsConfig{EncryptionKey: strings.Repeat("b2", 32)}}, legacy)
	_, err = wrong.Decrypt(sealed)
	require.Error(t, err)
	_, err = codec.Decrypt(sealed[:len(sealed)-2] + "xx")
	require.Error(t, err)
	backup := NewBackupService(repo, cfg, legacy, nil, nil)
	raw, err := json.Marshal(BackupS3Config{Bucket: "bucket", AccessKeyID: "id", SecretAccessKey: oldValue})
	require.NoError(t, err)
	require.NoError(t, repo.Set(ctx, settingKeyBackupS3Config, string(raw)))
	_, err = backup.UpdateS3Config(ctx, BackupS3Config{Bucket: "renamed", AccessKeyID: "id"})
	require.NoError(t, err)
	stored, err := backup.loadStoredS3Config(ctx)
	require.NoError(t, err)
	require.Equal(t, oldValue, stored.SecretAccessKey, "omitted secret must preserve the existing ciphertext byte for byte")
}
