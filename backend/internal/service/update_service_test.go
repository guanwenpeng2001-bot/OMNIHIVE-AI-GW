//go:build unit

package service

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

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
	if s.archive != nil {
		return os.WriteFile(dest, s.archive, 0600)
	}
	panic("DownloadFile should not be called when no update is available")
}

func (s *updateServiceGitHubClientStub) FetchChecksumFile(context.Context, string) ([]byte, error) {
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
				TagName: "v0.1.132",
				Name:    "v0.1.132",
			},
		},
		"0.1.132",
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
		{TagName: "v0.1.148", PublishedAt: "2026-07-09T00:00:00Z"},                       // newer than current: excluded
		{TagName: "v0.1.147", PublishedAt: "2026-07-08T00:00:00Z"},                       // current: excluded
		{TagName: "v0.1.146-rc1", PublishedAt: "2026-07-07T12:00:00Z", Prerelease: true}, // prerelease: excluded
		{TagName: "v0.1.146", PublishedAt: "2026-07-07T00:00:00Z"},
		{TagName: "v0.1.145", PublishedAt: "2026-07-06T00:00:00Z", Draft: true}, // draft: excluded
		{TagName: "v0.1.144", PublishedAt: "2026-07-05T00:00:00Z"},
		{TagName: "v0.1.144", PublishedAt: "2026-07-05T00:00:00Z"}, // duplicate: excluded
		{TagName: "v0.1.143", PublishedAt: "2026-07-04T00:00:00Z"},
		{TagName: "v0.1.142", PublishedAt: "2026-07-03T00:00:00Z"}, // beyond cap of 3: excluded
	}
	svc := newRollbackTestService("0.1.147", releases)

	versions, err := svc.ListRollbackVersions(context.Background())

	require.NoError(t, err)
	require.Len(t, versions, 3)
	require.Equal(t, "0.1.146", versions[0].Version)
	require.Equal(t, "0.1.144", versions[1].Version)
	require.Equal(t, "0.1.143", versions[2].Version)
}

func TestUpdateServiceListRollbackVersionsSortsUnorderedInput(t *testing.T) {
	releases := []*GitHubRelease{
		{TagName: "v0.1.144"},
		{TagName: "v0.1.146"},
		{TagName: "v0.1.145"},
	}
	svc := newRollbackTestService("0.1.147", releases)

	versions, err := svc.ListRollbackVersions(context.Background())

	require.NoError(t, err)
	require.Len(t, versions, 3)
	require.Equal(t, "0.1.146", versions[0].Version)
	require.Equal(t, "0.1.145", versions[1].Version)
	require.Equal(t, "0.1.144", versions[2].Version)
}

func TestUpdateServiceListRollbackVersionsEmptyWhenNoneOlder(t *testing.T) {
	releases := []*GitHubRelease{
		{TagName: "v0.1.147"},
		{TagName: "v0.1.148"},
	}
	svc := newRollbackTestService("0.1.147", releases)

	versions, err := svc.ListRollbackVersions(context.Background())

	require.NoError(t, err)
	require.Empty(t, versions)
}

func TestUpdateServiceListRollbackVersionsPropagatesFetchError(t *testing.T) {
	svc := NewUpdateService(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{recentErr: errors.New("github unavailable")},
		"0.1.147",
		"release",
	)

	_, err := svc.ListRollbackVersions(context.Background())

	require.Error(t, err)
	require.Contains(t, err.Error(), "github unavailable")
}

func TestUpdateServiceRollbackToVersionRejectsDisallowedTargets(t *testing.T) {
	releases := []*GitHubRelease{
		{TagName: "v0.1.148"},
		{TagName: "v0.1.147"},
		{TagName: "v0.1.146"},
		{TagName: "v0.1.145"},
		{TagName: "v0.1.144"},
		{TagName: "v0.1.143"},
		{TagName: "v0.1.142"},
	}
	svc := newRollbackTestService("0.1.147", releases)

	for _, target := range []string{
		"",         // empty
		"0.1.147",  // current version
		"v0.1.147", // current version with prefix
		"0.1.148",  // newer than current
		"0.1.142",  // older than the 3 most recent
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
		{TagName: "v0.1.147"},
		{TagName: "v0.1.146"},
	}
	svc := newRollbackTestService("0.1.147", releases)

	err := svc.RollbackToVersion(context.Background(), "v0.1.146")

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
			archive := updateTestArchive(t, "omnihive-ai-gw", "new-binary")
			archiveName := fmt.Sprintf("omnihive-ai-gw_0.2.5_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
			client := &updateServiceGitHubClientStub{archive: archive, checksums: []byte(fmt.Sprintf("%x  %s\n", sha256.Sum256(archive), archiveName))}
			svc := NewUpdateService(&updateServiceCacheStub{}, client, "0.2.4", "release")
			svc.executablePath = func() (string, error) { return exe, nil }
			assets := []Asset{{Name: archiveName, DownloadURL: "https://api.github.com/repos/owner/repo/releases/assets/123"}, {Name: "checksums.txt", DownloadURL: "https://api.github.com/repos/owner/repo/releases/assets/124"}}
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
			require.NoError(t, svc.extractBinary(archive, filepath.Join(dir, "extracted")))
		})
	}
}

func TestUpdateServiceChecksumFailureDoesNotStage(t *testing.T) {
	root := t.TempDir()
	t.Setenv("OMNIHIVE_DATA_DIR", root)
	exe := filepath.Join(root, "sub2api")
	require.NoError(t, os.WriteFile(exe, []byte("original"), 0755))
	client := &updateServiceGitHubClientStub{archive: updateTestArchive(t, "sub2api", "bad"), checksums: []byte("incorrect  omnihive_0.2.5_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz\n")}
	svc := NewUpdateService(&updateServiceCacheStub{}, client, "0.2.4", "release")
	svc.executablePath = func() (string, error) { return exe, nil }
	err := svc.applyReleaseAssets(context.Background(), "0.2.5", []Asset{{Name: "omnihive_0.2.5_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz", DownloadURL: "https://api.github.com/repos/o/r/releases/assets/1"}, {Name: "checksums.txt", DownloadURL: "https://api.github.com/repos/o/r/releases/assets/2"}})
	require.ErrorContains(t, err, "checksum mismatch")
	content, err := os.ReadFile(exe)
	require.NoError(t, err)
	require.Equal(t, "original", string(content))
	require.NoDirExists(t, filepath.Join(root, ".updates"))
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
	exe := filepath.Join(root, "sub2api")
	require.NoError(t, os.WriteFile(exe, []byte("new"), 0755))
	require.NoError(t, os.WriteFile(exe+".backup", []byte("old"), 0755))
	svc := NewUpdateService(nil, nil, "0.2.5", "release")
	svc.executablePath = func() (string, error) { return exe, nil }
	require.NoError(t, svc.Rollback())
	content, err := os.ReadFile(exe)
	require.NoError(t, err)
	require.Equal(t, "old", string(content))
}

func TestUpdateServicePersistentStageWhenDirectSwapFails(t *testing.T) {
	root := t.TempDir()
	t.Setenv("OMNIHIVE_DATA_DIR", root)
	exe := filepath.Join(root, "sub2api")
	require.NoError(t, os.WriteFile(exe, []byte("original"), 0755))
	// A nonempty backup directory deterministically prevents the direct rename.
	require.NoError(t, os.Mkdir(exe+".backup", 0755))
	require.NoError(t, os.WriteFile(filepath.Join(exe+".backup", "blocker"), []byte("x"), 0600))
	name := "omnihive-ai-gw_0.2.5_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	archive := updateTestArchive(t, "sub2api", "updated")
	client := &updateServiceGitHubClientStub{archive: archive, checksums: []byte(fmt.Sprintf("%x  %s\n", sha256.Sum256(archive), name))}
	svc := NewUpdateService(nil, client, "0.2.4", "release")
	svc.executablePath = func() (string, error) { return exe, nil }
	require.NoError(t, svc.applyReleaseAssets(context.Background(), "0.2.5", []Asset{{Name: name, DownloadURL: "https://api.github.com/repos/o/r/releases/assets/1"}, {Name: "checksums.txt", DownloadURL: "https://api.github.com/repos/o/r/releases/assets/2"}}))
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
