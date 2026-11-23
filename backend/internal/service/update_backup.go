package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
)

// Backup metadata is authenticated separately from the writable update volume.
type backupManifest struct {
	Product string `json:"product"`
	Version string `json:"version"`
	Digest  string `json:"sha256"`
	MAC     []byte `json:"hmac_sha256"`
}

// Preserve existing signatures when an explicit TOTP key was used. Otherwise
// derive a separate local-backup key from the server's persisted JWT secret.
func updateBackupKey(cfg *config.Config) []byte {
	if cfg.Totp.EncryptionKeyConfigured {
		return []byte(cfg.Totp.EncryptionKey)
	}
	if cfg.JWT.Secret == "" {
		return nil
	}
	mac := hmac.New(sha256.New, []byte(cfg.JWT.Secret))
	_, _ = mac.Write([]byte("omnihive/update-backup-key/v1"))
	return mac.Sum(nil)
}

func (s *UpdateService) backupMAC(m backupManifest) ([]byte, error) {
	if len(s.backupKey) < 32 {
		return nil, fmt.Errorf("persistent backup authentication key is unavailable")
	}
	mac := hmac.New(sha256.New, s.backupKey)
	fmt.Fprintf(mac, "omnihive-local-backup-v1\x00%s\x00%s\x00%s", m.Product, m.Version, m.Digest)
	return mac.Sum(nil), nil
}

func (s *UpdateService) runningManifest() (backupManifest, error) {
	path, err := s.runningBinary()
	if err != nil {
		return backupManifest{}, err
	}
	return s.backupManifestFor(path)
}

func (s *UpdateService) backupManifestFor(path string) (backupManifest, error) {
	m := backupManifest{Product: updateBinaryName, Version: backupVersion(s.currentVersion)}
	if m.Version == "" {
		return m, fmt.Errorf("invalid running release version")
	}
	file, err := os.Open(path)
	if err != nil {
		return m, err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err = io.Copy(hash, file); err != nil {
		return m, err
	}
	m.Digest = hex.EncodeToString(hash.Sum(nil))
	m.MAC, err = s.backupMAC(m)
	return m, err
}

func writeBackupManifest(backup string, m backupManifest) error {
	tmp, err := os.CreateTemp(filepath.Dir(backup), ".manifest-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err = json.NewEncoder(tmp).Encode(m); err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(tmp.Name(), backup+".manifest.json")
}

func (s *UpdateService) prepareBackupManifest(path string) error {
	m, err := s.backupManifestFor(path)
	if err != nil {
		return err
	}
	return writeBackupManifest(path, m)
}

func (s *UpdateService) databaseRollbackFloor(ctx context.Context) (string, error) {
	if s.readRollbackFloor == nil {
		return "", fmt.Errorf("database rollback floor is unavailable")
	}
	value, err := s.readRollbackFloor(ctx)
	if err != nil {
		return "", err
	}
	floor := backupVersion(value)
	if floor == "" {
		return "", fmt.Errorf("invalid persisted rollback floor")
	}
	return floor, nil
}

// Digest verification may take time on a large backup. Refresh DB policy after
// it completes, before changing either the persistent or executable path.
func (s *UpdateService) revalidateRollbackFloor(ctx context.Context, previous string, paths ...string) error {
	floor, err := s.databaseRollbackFloor(ctx)
	if err != nil {
		return err
	}
	if floor == previous {
		return nil
	}
	for _, path := range paths {
		if err := s.verifyBackup(path, floor); err != nil {
			return err
		}
	}
	return nil
}

func (s *UpdateService) verifyBackup(path, floor string) error {
	f, err := os.Open(path + ".manifest.json")
	if errors.Is(err, os.ErrNotExist) {
		return ErrRollbackManifestInvalid.WithCause(err)
	}
	if err != nil {
		return fmt.Errorf("read backup manifest: %w", err)
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxReleaseManifestSize+1))
	if err != nil {
		return fmt.Errorf("read backup manifest: %w", err)
	}
	var m backupManifest
	if len(b) > maxReleaseManifestSize {
		return ErrRollbackManifestInvalid
	}
	if err = json.Unmarshal(b, &m); err != nil {
		return ErrRollbackManifestInvalid.WithCause(err)
	}
	mac, err := s.backupMAC(m)
	if err != nil {
		return ErrRollbackManifestInvalid.WithCause(err)
	}
	if !hmac.Equal(m.MAC, mac) || m.Product != updateBinaryName || backupVersion(m.Version) == "" {
		return ErrRollbackManifestInvalid
	}
	if err := verifyReleaseChecksum(path, m.Digest); err != nil {
		if errors.Is(err, errReleaseChecksumMismatch) || errors.Is(err, os.ErrNotExist) {
			return ErrRollbackManifestInvalid.WithCause(err)
		}
		return err
	}
	_, err = ResolveReleaseCompatibility(m.Version, floor, "", false)
	return err
}
