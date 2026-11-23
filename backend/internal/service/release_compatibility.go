package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/mod/semver"
)

const OldestSupportedRelease = "v0.2.5"

func ReleaseChecksum(data []byte, name string) (string, error) {
	return findReleaseChecksum(data, name)
}

func CheckReleaseArchive(path, target string, checksums []byte) error {
	if !strings.HasPrefix(filepath.Base(path), updateBinaryName+"_"+strings.TrimPrefix(backupVersion(target), "v")+"_") {
		return fmt.Errorf("wrong product or version in release archive name")
	}
	hash, err := findReleaseChecksum(checksums, filepath.Base(path))
	if err != nil {
		return err
	}
	return verifyReleaseChecksum(path, hash)
}

type ReleaseEligibility struct {
	Target          string `json:"target"`
	EffectiveFloor  string `json:"effective_floor"`
	Eligible        bool   `json:"eligible"`
	ExclusionReason string `json:"exclusion_reason,omitempty"`
}

type RollbackEligibilityReport struct {
	DatabaseFloor    string            `json:"database_floor"`
	Versions         []RollbackVersion `json:"versions"`
	ExcludedVersions []RollbackVersion `json:"excluded_versions"`
}

// ResolveReleaseCompatibility is shared by online updates and the installer CLI.
// An empty database floor is permitted only for an explicitly fresh installation.
func ResolveReleaseCompatibility(target, databaseFloor, artifactFloor string, fresh bool) (ReleaseEligibility, error) {
	r := ReleaseEligibility{Target: backupVersion(target), EffectiveFloor: OldestSupportedRelease}
	if r.Target == "" {
		return r, fmt.Errorf("invalid release version %q", target)
	}
	if databaseFloor == "" && !fresh {
		return r, fmt.Errorf("database rollback floor is unavailable")
	}
	for _, raw := range []string{databaseFloor, artifactFloor} {
		if raw == "" {
			continue
		}
		v := backupVersion(raw)
		if v == "" {
			return r, fmt.Errorf("invalid rollback floor %q", raw)
		}
		if semver.Compare(v, r.EffectiveFloor) > 0 {
			r.EffectiveFloor = v
		}
	}
	if semver.Compare(r.Target, r.EffectiveFloor) < 0 {
		err := rollbackBelowFloor(r.Target, r.EffectiveFloor)
		r.ExclusionReason = err.Error()
		return r, err
	}
	r.Eligible = true
	return r, nil
}

// CheckReleaseManifest consumes a checksum-verified manifest, preserving the
// v0.3.3 manifest contract while accepting SemVer prerelease floors.
func CheckReleaseManifest(path, hash, target, databaseFloor string, fresh bool) (ReleaseEligibility, error) {
	if err := verifyReleaseChecksum(path, hash); err != nil {
		return ReleaseEligibility{}, err
	}
	f, err := os.Open(path)
	if err != nil {
		return ReleaseEligibility{}, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxReleaseManifestSize+1))
	if err != nil {
		return ReleaseEligibility{}, err
	}
	var m struct {
		Product string `json:"product"`
		Version string `json:"version"`
		Floor   string `json:"minimum_rollback_version"`
	}
	if len(data) > maxReleaseManifestSize {
		return ReleaseEligibility{}, fmt.Errorf("release manifest exceeds size limit")
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return ReleaseEligibility{}, err
	}
	if m.Product != updateBinaryName {
		return ReleaseEligibility{}, fmt.Errorf("wrong product in release manifest")
	}
	if backupVersion(m.Version) == "" || backupVersion(m.Version) != backupVersion(target) {
		return ReleaseEligibility{}, fmt.Errorf("release manifest version does not match requested release")
	}
	if backupVersion(m.Floor) == "" {
		return ReleaseEligibility{}, fmt.Errorf("invalid minimum rollback version")
	}
	return ResolveReleaseCompatibility(target, databaseFloor, m.Floor, fresh)
}

func (s *UpdateService) checkRelease(ctx context.Context, target, artifactFloor string) (ReleaseEligibility, error) {
	floor, err := s.databaseRollbackFloor(ctx)
	if err != nil {
		return ReleaseEligibility{}, err
	}
	return ResolveReleaseCompatibility(target, floor, artifactFloor, false)
}

func (s *UpdateService) rollbackCandidateEligibility(ctx context.Context, r *GitHubRelease) (ReleaseEligibility, error) {
	if eligibility, err := s.checkRelease(ctx, r.TagName, ""); err != nil {
		return eligibility, err
	}
	assets := make([]Asset, 0, len(r.Assets))
	for _, a := range r.Assets {
		assets = append(assets, Asset{Name: a.Name, DownloadURL: resolveAssetDownloadURL(a)})
	}
	archive, checksums, manifest, err := selectUpdateAssets(releaseVersion(r.TagName), assets)
	if err != nil {
		return ReleaseEligibility{}, err
	}
	for _, a := range []Asset{archive, checksums, manifest} {
		if err := validateDownloadURL(a.DownloadURL); err != nil {
			return ReleaseEligibility{}, err
		}
	}
	data, err := s.githubClient.FetchChecksumFile(ctx, checksums.DownloadURL)
	if err != nil {
		return ReleaseEligibility{}, err
	}
	if _, err := findReleaseChecksum(data, archive.Name); err != nil {
		return ReleaseEligibility{}, err
	}
	hash, err := findReleaseChecksum(data, manifest.Name)
	if err != nil {
		return ReleaseEligibility{}, err
	}
	dir, err := os.MkdirTemp("", "omnihive-eligibility-*")
	if err != nil {
		return ReleaseEligibility{}, err
	}
	defer os.RemoveAll(dir)
	path := dir + "/" + manifest.Name
	if err := s.githubClient.DownloadFile(ctx, manifest.DownloadURL, path, maxReleaseManifestSize); err != nil {
		return ReleaseEligibility{}, err
	}
	floor, err := s.databaseRollbackFloor(ctx)
	if err != nil {
		return ReleaseEligibility{}, err
	}
	return CheckReleaseManifest(path, hash, r.TagName, floor, false)
}
