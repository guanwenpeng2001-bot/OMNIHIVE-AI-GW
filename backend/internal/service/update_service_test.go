//go:build unit

package service

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	infraerrors "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

type updateServiceCacheStub struct {
	data string
}

func (s *updateServiceCacheStub) GetUpdateInfo(context.Context) (string, error) {
	if s.data == "" {
		return "", errors.New("cache miss")
	}
	return s.data, nil
}

func (s *updateServiceCacheStub) SetUpdateInfo(_ context.Context, data string, _ time.Duration) error {
	s.data = data
	return nil
}

type updateServiceGitHubClientStub struct {
	latestErr      error
	repo           string
	archive        []byte
	checksums      []byte
	checksumErr    error
	manifest       []byte
	release        *GitHubRelease
	recentReleases []*GitHubRelease
	recentErr      error
}

func (s *updateServiceGitHubClientStub) FetchLatestRelease(_ context.Context, repo string) (*GitHubRelease, error) {
	s.repo = repo
	return s.release, s.latestErr
}

func (s *updateServiceGitHubClientStub) FetchRecentReleases(context.Context, string, int) ([]*GitHubRelease, error) {
	return s.recentReleases, s.recentErr
}

func (s *updateServiceGitHubClientStub) DownloadFile(_ context.Context, _ string, dest string, _ int64) error {
	if filepath.Base(dest) == releaseManifestName || filepath.Base(dest) == "release-manifest.json" {
		return os.WriteFile(dest, s.manifest, 0600)
	}
	if s.archive != nil {
		return os.WriteFile(dest, s.archive, 0600)
	}
	panic("DownloadFile should not be called when no update is available")
}

func (s *updateServiceGitHubClientStub) FetchChecksumFile(context.Context, string) ([]byte, error) {
	if s.checksumErr != nil {
		return nil, s.checksumErr
	}
	if s.checksums != nil {
		return s.checksums, nil
	}
	panic("FetchChecksumFile should not be called when no update is available")
}

func TestUpdateServicePerformUpdateNoUpdateReturnsSentinel(t *testing.T) {
	svc := NewUpdateService(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{
			release: &GitHubRelease{
				TagName: "v0.2.132",
				Name:    "v0.2.132",
			},
		},
		"0.2.132",
		"release",
	)

	err := svc.PerformUpdate(context.Background())

	require.Error(t, err)
	require.True(t, errors.Is(err, ErrNoUpdateAvailable))
	require.ErrorIs(t, err, ErrNoUpdateAvailable)
}

func newRollbackTestService(current string, releases []*GitHubRelease) *UpdateService {
	return NewUpdateService(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{recentReleases: releases},
		current,
		"release",
	)
}

func TestUpdateServiceListRollbackVersionsFiltersAndCaps(t *testing.T) {
	releases := []*GitHubRelease{
		{TagName: "v0.2.148", PublishedAt: "2026-07-09T00:00:00Z"},                       // newer than current: excluded
		{TagName: "v0.2.147", PublishedAt: "2026-07-08T00:00:00Z"},                       // current: excluded
		{TagName: "v0.2.146-rc1", PublishedAt: "2026-07-07T12:00:00Z", Prerelease: true}, // prerelease: excluded
		{TagName: "v0.2.146", PublishedAt: "2026-07-07T00:00:00Z"},
		{TagName: "v0.2.145", PublishedAt: "2026-07-06T00:00:00Z", Draft: true}, // draft: excluded
		{TagName: "v0.2.144", PublishedAt: "2026-07-05T00:00:00Z"},
		{TagName: "v0.2.144", PublishedAt: "2026-07-05T00:00:00Z"}, // duplicate: excluded
		{TagName: "v0.2.143", PublishedAt: "2026-07-04T00:00:00Z"},
		{TagName: "v0.2.142", PublishedAt: "2026-07-03T00:00:00Z"}, // beyond cap of 3: excluded
	}
	svc := newRollbackTestService("0.2.147", releases)

	versions, err := svc.ListRollbackVersions(context.Background())

	require.NoError(t, err)
	require.Len(t, versions, 3)
	require.Equal(t, "0.2.146", versions[0].Version)
	require.Equal(t, "0.2.144", versions[1].Version)
	require.Equal(t, "0.2.143", versions[2].Version)
}

func TestUpdateServiceListRollbackVersionsSortsUnorderedInput(t *testing.T) {
	releases := []*GitHubRelease{
		{TagName: "v0.2.144"},
		{TagName: "v0.2.146"},
		{TagName: "v0.2.145"},
	}
	svc := newRollbackTestService("0.2.147", releases)

	versions, err := svc.ListRollbackVersions(context.Background())

	require.NoError(t, err)
	require.Len(t, versions, 3)
	require.Equal(t, "0.2.146", versions[0].Version)
	require.Equal(t, "0.2.145", versions[1].Version)
	require.Equal(t, "0.2.144", versions[2].Version)
}

func TestUpdateServiceListRollbackVersionsEmptyWhenNoneOlder(t *testing.T) {
	releases := []*GitHubRelease{
		{TagName: "v0.2.147"},
		{TagName: "v0.2.148"},
	}
	svc := newRollbackTestService("0.2.147", releases)

	versions, err := svc.ListRollbackVersions(context.Background())

	require.NoError(t, err)
	require.Empty(t, versions)
}

func TestUpdateServiceListRollbackVersionsPropagatesFetchError(t *testing.T) {
	svc := NewUpdateService(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{recentErr: errors.New("github unavailable")},
		"0.2.147",
		"release",
	)

	_, err := svc.ListRollbackVersions(context.Background())

	require.Error(t, err)
	require.Contains(t, err.Error(), "github unavailable")
}

func TestUpdateServiceRollbackToVersionRejectsDisallowedTargets(t *testing.T) {
	releases := []*GitHubRelease{
		{TagName: "v0.2.148"},
		{TagName: "v0.2.147"},
		{TagName: "v0.2.146"},
		{TagName: "v0.2.145"},
		{TagName: "v0.2.144"},
		{TagName: "v0.2.143"},
		{TagName: "v0.2.142"},
	}
	svc := newRollbackTestService("0.2.147", releases)

	for _, target := range []string{
		"",         // empty
		"0.2.147",  // current version
		"v0.2.147", // current version with prefix
		"0.2.148",  // newer than current
		"0.2.142",  // older than the 3 most recent
		"9.9.9",    // nonexistent
	} {
		err := svc.RollbackToVersion(context.Background(), target)
		require.ErrorIs(t, err, ErrRollbackVersionNotAllowed, "target %q should be rejected", target)
	}
}

func TestUpdateServiceRollbackToVersionAcceptsVPrefix(t *testing.T) {
	// No platform asset in the release: the target passes the allowlist check
	// and fails later at asset lookup, proving the version itself was accepted.
	releases := []*GitHubRelease{
		{TagName: "v0.2.147"},
		{TagName: "v0.2.146"},
	}
	svc := newRollbackTestService("0.2.147", releases)

	err := svc.RollbackToVersion(context.Background(), "v0.2.146")

	require.Error(t, err)
	require.NotErrorIs(t, err, ErrRollbackVersionNotAllowed)
	require.Contains(t, err.Error(), "no compatible release found")
}

func TestUpdateServiceNoPublishedRelease(t *testing.T) {
	for _, cached := range []string{"", `{"current_version":"0.2.4","latest_version":"0.2.5","has_update":true}`} {
		client := &updateServiceGitHubClientStub{latestErr: errors.New("GitHub API returned 404")}
		svc := NewUpdateService(&updateServiceCacheStub{data: cached}, client, "0.2.4", "release")
		info, err := svc.CheckUpdate(context.Background(), true)
		require.NoError(t, err)
		require.False(t, info.HasUpdate)
		require.Empty(t, info.Warning)
		require.Equal(t, "0.2.4", info.CurrentVersion)
		require.Equal(t, "0.2.4", info.LatestVersion)
		require.Equal(t, "release", info.BuildType)
	}
}

func TestUpdateServicePrivateAssetSelection(t *testing.T) {
	a := GitHubAsset{Name: "checksums.txt", URL: "https://api.github.com/repos/owner/repo/releases/assets/123", BrowserDownloadURL: "https://github.com/owner/repo/releases/download/v0.2.5/checksums.txt"}
	t.Setenv("UPDATE_GITHUB_REPO", " owner/private ")
	for _, token := range []string{"", "  ", "test-token"} {
		t.Setenv("UPDATE_GITHUB_TOKEN", token)
		client := &updateServiceGitHubClientStub{release: &GitHubRelease{TagName: "v0.2.5", Assets: []GitHubAsset{a}}}
		svc := NewUpdateService(&updateServiceCacheStub{}, client, "0.2.4", "release")
		info, err := svc.CheckUpdate(context.Background(), true)
		require.NoError(t, err)
		require.Equal(t, "owner/private", client.repo)
		want := a.BrowserDownloadURL
		if token == "test-token" {
			want = a.URL
		}
		require.Equal(t, want, info.ReleaseInfo.Assets[0].DownloadURL)
	}
	require.NoError(t, validateDownloadURL(a.URL))
	require.Error(t, validateDownloadURL("https://api.github.com.evil.test/asset"))
	require.Error(t, validateDownloadURL("http://api.github.com/asset"))
	a.URL = "https://objects.githubusercontent.com/asset"
	require.Equal(t, a.BrowserDownloadURL, resolveAssetDownloadURL(a))
	t.Setenv("UPDATE_GITHUB_REPO", " ")
	require.Equal(t, "guanwenpeng2001-bot/OMNIHIVE-AI-GW", resolveGitHubRepo())
}

func updateTestArchive(t *testing.T, name, content string) []byte {
	t.Helper()
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: name, Mode: 0755, Size: int64(len(content)), Typeflag: tar.TypeReg}))
	_, err := tw.Write([]byte(content))
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	return b.Bytes()
}

func TestUpdateServicePersistentUpdateAndRollback(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(fmt.Sprint("existing-staged-", existing), func(t *testing.T) {
			root := t.TempDir()
			dataDir := filepath.Join(root, "data")
			require.NoError(t, os.MkdirAll(dataDir, 0755))
			t.Setenv("OMNIHIVE_DATA_DIR", dataDir)
			exe := filepath.Join(root, "sub2api")
			require.NoError(t, os.WriteFile(exe, []byte("running-old"), 0755))
			current := filepath.Join(dataDir, ".updates", "current", "omnihive")
			previous := "running-old"
			if existing {
				require.NoError(t, os.MkdirAll(filepath.Dir(current), 0755))
				previous = "staged-old"
				require.NoError(t, os.WriteFile(current, []byte(previous), 0755))
			}
			archive := updateTestArchive(t, "omnihive", "new-binary")
			client, assets := updateTestRelease("0.2.5", archive, "omnihive", "0.2.5")
			svc := NewUpdateService(&updateServiceCacheStub{}, client, "0.2.4", "release")
			prepareRollbackService(t, svc)
			svc.executablePath = func() (string, error) { return exe, nil }
			require.NoError(t, svc.applyReleaseAssets(context.Background(), "0.2.5", assets))
			content, err := os.ReadFile(current)
			require.NoError(t, err)
			require.Equal(t, "new-binary", string(content))
			backup := filepath.Join(dataDir, ".updates", "backup", "omnihive")
			content, err = os.ReadFile(backup)
			require.NoError(t, err)
			require.Equal(t, previous, string(content))
			content, err = os.ReadFile(exe)
			require.NoError(t, err)
			require.Equal(t, "new-binary", string(content))
			// Container recreation loses the executable-local backup, but retains the volume.
			require.NoError(t, os.Remove(exe+".backup"))
			require.NoError(t, svc.Rollback())
			content, err = os.ReadFile(current)
			require.NoError(t, err)
			require.Equal(t, previous, string(content))
			require.NoFileExists(t, backup)
			if runtime.GOOS != "windows" {
				stat, err := os.Stat(current)
				require.NoError(t, err)
				require.Equal(t, os.FileMode(0755), stat.Mode().Perm())
			}
		})
	}
}

func TestUpdateServiceExtractBinaryNames(t *testing.T) {
	for _, name := range []string{"sub2api", "sub2api.exe", "omnihive", "omnihive.exe", "omnihive-ai-gw", "omnihive-ai-gw.exe"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			archive := filepath.Join(dir, "release.tar.gz")
			require.NoError(t, os.WriteFile(archive, updateTestArchive(t, name, "binary"), 0600))
			svc := &UpdateService{}
			dest := filepath.Join(dir, "extracted")
			err := svc.extractBinary(archive, dest)
			if name == "omnihive" || name == "omnihive.exe" {
				require.NoError(t, err)
				require.FileExists(t, dest)
			} else {
				require.ErrorContains(t, err, "wrong product executable")
				require.NoFileExists(t, dest)
			}
		})
	}
}

func updateTestRelease(version string, archive []byte, product, floor string) (*updateServiceGitHubClientStub, []Asset) {
	archiveName := fmt.Sprintf("omnihive_%s_%s_%s.tar.gz", version, runtime.GOOS, runtime.GOARCH)
	manifest := []byte(fmt.Sprintf(`{"product":%q,"version":%q,"minimum_rollback_version":%q}`, product, version, floor))
	checksums := []byte(fmt.Sprintf("%x  %s\n%x  %s\n", sha256.Sum256(archive), archiveName, sha256.Sum256(manifest), releaseManifestName))
	assets := []Asset{
		{Name: archiveName, DownloadURL: "https://api.github.com/repos/o/r/releases/assets/1"},
		{Name: "checksums.txt", DownloadURL: "https://api.github.com/repos/o/r/releases/assets/2"},
		{Name: releaseManifestName, DownloadURL: "https://api.github.com/repos/o/r/releases/assets/3"},
	}
	return &updateServiceGitHubClientStub{archive: archive, checksums: checksums, manifest: manifest}, assets
}

func TestUpdateServiceRejectsUnverifiedReleasesBeforeReplacingBinary(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*updateServiceGitHubClientStub, []Asset)
		want   string
	}{
		{"wrong_product_asset", func(_ *updateServiceGitHubClientStub, assets []Asset) {
			assets[0].Name = strings.Replace(assets[0].Name, "omnihive_", "sub2api_", 1)
		}, "wrong product release asset"},
		{"missing_checksums", func(_ *updateServiceGitHubClientStub, assets []Asset) {
			assets[1] = Asset{}
		}, "checksums.txt not found"},
		{"checksums_download_failure", func(client *updateServiceGitHubClientStub, _ []Asset) {
			client.checksumErr = errors.New("HTTP 404")
		}, "failed to download checksums"},
		{"missing_checksum_entry", func(client *updateServiceGitHubClientStub, _ []Asset) {
			_, rest, _ := strings.Cut(string(client.checksums), "\n")
			client.checksums = []byte(rest)
		}, "checksum not found for omnihive_"},
		{"tampered_binary", func(client *updateServiceGitHubClientStub, _ []Asset) {
			client.archive = updateTestArchive(t, "omnihive", "tampered binary")
		}, "checksum mismatch"},
		{"missing_manifest", func(_ *updateServiceGitHubClientStub, assets []Asset) {
			assets[2] = Asset{}
		}, "release manifest"},
		{"tampered_manifest", func(client *updateServiceGitHubClientStub, _ []Asset) {
			client.manifest = []byte("{}")
		}, "release manifest verification failed"},
		{"wrong_manifest_product", func(client *updateServiceGitHubClientStub, _ []Asset) {
			bad, _ := updateTestRelease("0.2.5", client.archive, "sub2api", "0.2.5")
			client.manifest, client.checksums = bad.manifest, bad.checksums
		}, "wrong product"},
		{"below_manifest_floor", func(client *updateServiceGitHubClientStub, _ []Asset) {
			bad, _ := updateTestRelease("0.2.5", client.archive, "omnihive", "0.2.6")
			client.manifest, client.checksums = bad.manifest, bad.checksums
		}, "database rollback compatibility floor"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("OMNIHIVE_DATA_DIR", root)
			exe := filepath.Join(root, "omnihive")
			require.NoError(t, os.WriteFile(exe, []byte("original"), 0755))
			client, assets := updateTestRelease("0.2.5", updateTestArchive(t, "omnihive", "candidate"), "omnihive", "0.2.5")
			tc.mutate(client, assets)
			release := &GitHubRelease{TagName: "v0.2.5"}
			for _, asset := range assets {
				release.Assets = append(release.Assets, GitHubAsset{Name: asset.Name, BrowserDownloadURL: asset.DownloadURL})
			}
			client.release = release
			client.recentReleases = []*GitHubRelease{release}
			update := NewUpdateService(&updateServiceCacheStub{}, client, "0.2.4", "release")
			update.executablePath = func() (string, error) { return exe, nil }
			require.ErrorContains(t, update.PerformUpdate(context.Background()), tc.want)
			rollback := NewUpdateService(&updateServiceCacheStub{}, client, "0.2.6", "release")
			rollback.executablePath = update.executablePath
			require.ErrorContains(t, rollback.RollbackToVersion(context.Background(), "0.2.5"), tc.want)

			content, err := os.ReadFile(exe)
			require.NoError(t, err)
			require.Equal(t, "original", string(content))
			require.NoFileExists(t, exe+".backup")
			require.NoDirExists(t, filepath.Join(root, ".updates"))
		})
	}
}

func TestUpdateServiceRollbackRejectsVersionBelowSchemaFloor(t *testing.T) {
	svc := newRollbackTestService("0.2.6", []*GitHubRelease{{TagName: "v0.2.5"}, {TagName: "v0.2.4"}})
	versions, err := svc.ListRollbackVersions(context.Background())
	require.NoError(t, err)
	require.Len(t, versions, 1)
	require.Equal(t, "0.2.5", versions[0].Version)
	err = svc.RollbackToVersion(context.Background(), "v0.2.4")
	require.ErrorIs(t, err, ErrRollbackVersionNotAllowed)
	require.ErrorContains(t, err, "database rollback compatibility floor v0.2.5")
}

func TestUpdateServiceStagingDirectoryFallback(t *testing.T) {
	root := t.TempDir()
	t.Setenv("OMNIHIVE_DATA_DIR", root)
	// A file cannot contain a temporary directory, regardless of OS or root privileges.
	blocked := filepath.Join(root, "blocked")
	require.NoError(t, os.WriteFile(blocked, []byte("not a directory"), 0600))
	stage, direct, err := createUpdateTempDir(blocked)
	require.NoError(t, err)
	require.False(t, direct)
	require.Equal(t, filepath.Join(root, ".updates"), filepath.Dir(stage))
	require.DirExists(t, stage)
	stage, direct, err = createUpdateTempDir(root)
	require.NoError(t, err)
	require.True(t, direct)
	require.Equal(t, root, filepath.Dir(stage))
}

func TestUpdateServiceDirectRollback(t *testing.T) {
	root := t.TempDir()
	t.Setenv("OMNIHIVE_DATA_DIR", filepath.Join(root, "absent"))
	exe := filepath.Join(root, "omnihive")
	require.NoError(t, os.WriteFile(exe, []byte("new"), 0755))
	require.NoError(t, os.WriteFile(exe+".backup", []byte("old"), 0755))
	svc := NewUpdateService(nil, nil, "0.2.5", "release")
	prepareRollbackService(t, svc)
	svc.executablePath = func() (string, error) { return exe, nil }
	require.Error(t, svc.Rollback())
	content, err := os.ReadFile(exe)
	require.NoError(t, err)
	require.Equal(t, "new", string(content))
}

func TestUpdateServicePersistentStageWhenDirectSwapFails(t *testing.T) {
	root := t.TempDir()
	t.Setenv("OMNIHIVE_DATA_DIR", root)
	exe := filepath.Join(root, "omnihive")
	require.NoError(t, os.WriteFile(exe, []byte("original"), 0755))
	// A nonempty backup directory deterministically prevents the direct rename.
	require.NoError(t, os.Mkdir(exe+".backup", 0755))
	require.NoError(t, os.WriteFile(filepath.Join(exe+".backup", "blocker"), []byte("x"), 0600))
	archive := updateTestArchive(t, "omnihive", "updated")
	client, assets := updateTestRelease("0.2.5", archive, "omnihive", "0.2.5")
	svc := NewUpdateService(nil, client, "0.2.4", "release")
	prepareRollbackService(t, svc)
	svc.executablePath = func() (string, error) { return exe, nil }
	require.NoError(t, svc.applyReleaseAssets(context.Background(), "0.2.5", assets))
	content, err := os.ReadFile(exe)
	require.NoError(t, err)
	require.Equal(t, "original", string(content))
	current := filepath.Join(root, ".updates", "current", "omnihive")
	content, err = os.ReadFile(current)
	require.NoError(t, err)
	require.Equal(t, "updated", string(content))
	require.NoError(t, svc.Rollback())
	content, err = os.ReadFile(current)
	require.NoError(t, err)
	require.Equal(t, "original", string(content))
}

func prepareRollbackService(t *testing.T, svc *UpdateService) {
	t.Helper()
	svc.currentVersion = "v0.2.23-omnihive.2"
	svc.backupKey = []byte("test-backup-key-32-bytes-long-value")
	svc.runningBinary = svc.executablePath
	svc.readRollbackFloor = func(context.Context) (string, error) { return "v0.2.23-omnihive.2", nil }
}

func TestBackupVersionNormalizesPersistedFloor(t *testing.T) {
	for input, expected := range map[string]string{
		"0.2.23-omnihive.2":  "v0.2.23-omnihive.2",
		"v0.2.23-omnihive.2": "v0.2.23-omnihive.2",
		"0.2.5":              "v0.2.5",
		"development":        "",
	} {
		require.Equal(t, expected, backupVersion(input))
	}
}

func TestUpdateServiceDirectRollbackVerifiesManifestDigestAndFloor(t *testing.T) {
	root := t.TempDir()
	t.Setenv("OMNIHIVE_DATA_DIR", filepath.Join(root, "absent"))
	exe := filepath.Join(root, "omnihive")
	require.NoError(t, os.WriteFile(exe, []byte("new"), 0755))
	backup := exe + ".backup"
	require.NoError(t, os.WriteFile(backup, []byte("old"), 0755))
	svc := NewUpdateService(nil, nil, "v0.2.23-omnihive.2", "release")
	prepareRollbackService(t, svc)
	svc.executablePath = func() (string, error) { return exe, nil }
	require.NoError(t, svc.prepareBackupManifest(backup))
	require.NoError(t, svc.Rollback())
	content, err := os.ReadFile(exe)
	require.NoError(t, err)
	require.Equal(t, "old", string(content))

	require.NoError(t, os.WriteFile(exe, []byte("new"), 0755))
	require.NoError(t, os.WriteFile(backup, []byte("tampered"), 0755))
	require.ErrorContains(t, svc.Rollback(), "checksum mismatch")
	content, err = os.ReadFile(exe)
	require.NoError(t, err)
	require.Equal(t, "new", string(content))

	require.NoError(t, os.WriteFile(backup, []byte("old"), 0755))
	require.NoError(t, svc.prepareBackupManifest(backup))
	raw, err := os.ReadFile(backup + ".manifest.json")
	require.NoError(t, err)
	var manifest backupManifest
	require.NoError(t, json.Unmarshal(raw, &manifest))
	manifest.MAC[0] ^= 1
	require.NoError(t, writeBackupManifest(backup, manifest))
	status, body := infraerrors.ToHTTP(svc.Rollback())
	require.Equal(t, 409, status)
	require.Equal(t, "ROLLBACK_MANIFEST_INVALID", body.Reason)
	content, err = os.ReadFile(exe)
	require.NoError(t, err)
	require.Equal(t, "new", string(content))

	svc.currentVersion = "v0.2.5"
	require.NoError(t, svc.prepareBackupManifest(backup))
	require.ErrorIs(t, svc.Rollback(), ErrRollbackVersionNotAllowed)
}
