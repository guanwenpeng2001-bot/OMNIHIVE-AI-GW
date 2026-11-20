package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/mod/semver"
)

// Backup metadata is authenticated separately from the writable update volume.
type backupManifest struct {
	Product string `json:"product"`
	Version string `json:"version"`
	Digest  string `json:"sha256"`
	MAC     []byte `json:"hmac_sha256"`
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
	m := backupManifest{Product: updateBinaryName, Version: backupVersion(s.currentVersion)}
	if m.Version == "" {
		return m, fmt.Errorf("invalid running release version")
	}
	path, err := s.runningBinary()
	if err != nil {
		return m, err
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
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	version := backupVersion(s.currentVersion)
	if version == "" {
		return fmt.Errorf("invalid running release version")
	}
	m := backupManifest{Product: updateBinaryName, Version: version, Digest: hex.EncodeToString(sha256.Sum256(data))}
	m.MAC, err = s.backupMAC(m)
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
	if semver.Compare(floor, minimumRollbackVersion) < 0 {
		floor = minimumRollbackVersion
	}
	return floor, nil
}

func (s *UpdateService) verifyBackup(path, floor string) error {
	b, err := os.ReadFile(path + ".manifest.json")
	if err != nil {
		return fmt.Errorf("read backup manifest: %w", err)
	}
	var m backupManifest
	if err = json.Unmarshal(b, &m); err != nil {
		return err
	}
	mac, err := s.backupMAC(m)
	if err != nil {
		return err
	}
	if !hmac.Equal(m.MAC, mac) || m.Product != updateBinaryName || backupVersion(m.Version) == "" {
		return fmt.Errorf("invalid backup identity or manifest authentication")
	}
	if semver.Compare(releaseVersion(m.Version), floor) < 0 {
		return rollbackBelowFloor(m.Version, floor)
	}
	return verifyReleaseChecksum(path, m.Digest)
}
