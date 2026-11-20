package service

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/errors"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/runtimeenv"
	"golang.org/x/mod/semver"
)

var (
	ErrNoUpdateAvailable         = infraerrors.Conflict("ALREADY_UP_TO_DATE", "no update available; current version is latest")
	ErrRollbackVersionNotAllowed = infraerrors.BadRequest("ROLLBACK_VERSION_NOT_ALLOWED", "version is not in the allowed rollback list")
)

const (
	updateCacheKey         = "update_check_cache"
	updateCacheTTL         = 1200 // 20 minutes
	updateBinaryName       = "omnihive"
	legacyUpdateBinaryName = "sub2api"
	githubRepo             = "guanwenpeng2001-bot/OMNIHIVE-AI-GW"
	releaseManifestName    = "omnihive-release-manifest.json"
	maxReleaseManifestSize = 64 * 1024

	// Versions before the OmniHive v2 schema may not serve a migrated database.
	minimumRollbackVersion = "v0.2.5"

	// Security: allowed download domains for updates
	allowedDownloadHost = "github.com"
	allowedAssetHost    = "objects.githubusercontent.com"

	// Security: max download size (500MB)
	maxDownloadSize = 500 * 1024 * 1024

	// Rollback: expose at most the 3 most recent versions older than current
	maxRollbackVersions = 3
	// Fetch a few extra releases so filtering (current/newer/prerelease) still leaves enough candidates
	rollbackFetchPageSize = 15
)

// UpdateCache defines cache operations for update service
type UpdateCache interface {
	GetUpdateInfo(ctx context.Context) (string, error)
	SetUpdateInfo(ctx context.Context, data string, ttl time.Duration) error
}

// GitHubReleaseClient 获取 GitHub release 信息的接口
type GitHubReleaseClient interface {
	FetchLatestRelease(ctx context.Context, repo string) (*GitHubRelease, error)
	FetchRecentReleases(ctx context.Context, repo string, perPage int) ([]*GitHubRelease, error)
	DownloadFile(ctx context.Context, url, dest string, maxSize int64) error
	FetchChecksumFile(ctx context.Context, url string) ([]byte, error)
}

// UpdateService handles software updates
type UpdateService struct {
	executablePath    func() (string, error)
	runningBinary     func() (string, error)
	backupKey         []byte
	readRollbackFloor func(context.Context) (string, error)
	cache             UpdateCache
	githubClient      GitHubReleaseClient
	currentVersion    string
	buildType         string // "source" for manual builds, "release" for CI builds
}

// NewUpdateService creates a new UpdateService
func NewUpdateService(cache UpdateCache, githubClient GitHubReleaseClient, version, buildType string) *UpdateService {
	return &UpdateService{
		executablePath: os.Executable,
		runningBinary: func() (string, error) {
			if runtime.GOOS == "linux" {
				return "/proc/self/exe", nil
			}
			return os.Executable()
		},
		cache:          cache,
		githubClient:   githubClient,
		currentVersion: version,
		buildType:      buildType,
	}
}

// UpdateInfo contains update information
type UpdateInfo struct {
	CurrentVersion string       `json:"current_version"`
	LatestVersion  string       `json:"latest_version"`
	HasUpdate      bool         `json:"has_update"`
	ReleaseInfo    *ReleaseInfo `json:"release_info,omitempty"`
	Cached         bool         `json:"cached"`
	Warning        string       `json:"warning,omitempty"`
	BuildType      string       `json:"build_type"` // "source" or "release"
}

// ReleaseInfo contains GitHub release details
type ReleaseInfo struct {
	Name        string  `json:"name"`
	Body        string  `json:"body"`
	PublishedAt string  `json:"published_at"`
	HTMLURL     string  `json:"html_url"`
	Assets      []Asset `json:"assets,omitempty"`
}

// Asset represents a release asset
type Asset struct {
	Name        string `json:"name"`
	DownloadURL string `json:"download_url"`
	Size        int64  `json:"size"`
}

// GitHubRelease represents GitHub API response
type GitHubRelease struct {
	TagName     string        `json:"tag_name"`
	Name        string        `json:"name"`
	Body        string        `json:"body"`
	PublishedAt string        `json:"published_at"`
	HTMLURL     string        `json:"html_url"`
	Draft       bool          `json:"draft"`
	Prerelease  bool          `json:"prerelease"`
	Assets      []GitHubAsset `json:"assets"`
}

// RollbackVersion describes a release version the system can roll back to
type RollbackVersion struct {
	Version     string `json:"version"` // without "v" prefix, e.g. "0.1.146"
	PublishedAt string `json:"published_at"`
	HTMLURL     string `json:"html_url"`
}

type GitHubAsset struct {
	URL                string `json:"url"`
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

func resolveGitHubRepo() string {
	if repo := strings.TrimSpace(os.Getenv("UPDATE_GITHUB_REPO")); repo != "" {
		return repo
	}
	return githubRepo
}

func resolveAssetDownloadURL(a GitHubAsset) string {
	if strings.TrimSpace(os.Getenv("UPDATE_GITHUB_TOKEN")) != "" && strings.HasPrefix(a.URL, "https://api.github.com/") {
		return a.URL
	}
	return a.BrowserDownloadURL
}

func resolveDockerDataDir() string {
	if dir := runtimeenv.DataDir(); dir != "" {
		return dir
	}
	return runtimeenv.ContainerDataDir
}

func createUpdateTempDir(exeDir string) (string, bool, error) {
	tempDir, err := os.MkdirTemp(exeDir, ".omnihive-update-*")
	if err == nil {
		return tempDir, true, nil
	}
	updatesDir := filepath.Join(resolveDockerDataDir(), ".updates")
	if err := os.MkdirAll(updatesDir, 0755); err != nil {
		return "", false, fmt.Errorf("failed to create persistent update directory: %w", err)
	}
	tempDir, err = os.MkdirTemp(updatesDir, ".stage-*")
	if err != nil {
		return "", false, fmt.Errorf("failed to create temp dir: %w", err)
	}
	return tempDir, false, nil
}

// copyUpdateBinary publishes a complete file by renaming within the target filesystem.
func copyUpdateBinary(source, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.CreateTemp(filepath.Dir(target), ".binary-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(out.Name()) }()
	defer func() { _ = out.Close() }()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	if err := out.Chmod(0755); err != nil {
		return err
	}
	if err := out.Sync(); err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(out.Name(), target)
}

func (s *UpdateService) stagePersistentBinary(exePath, newBinaryPath string) (bool, error) {
	dataDir := resolveDockerDataDir()
	if info, err := os.Stat(dataDir); err != nil || !info.IsDir() {
		return false, nil
	}
	updatesDir := filepath.Join(dataDir, ".updates")
	if err := os.MkdirAll(updatesDir, 0755); err != nil {
		return false, nil
	}
	probe, err := os.MkdirTemp(updatesDir, ".probe-*")
	if err != nil {
		return false, nil
	}
	defer func() { _ = os.Remove(probe) }()
	current := filepath.Join(updatesDir, "current", updateBinaryName)
	previous := current
	if _, err := os.Stat(current); os.IsNotExist(err) {
		previous = exePath
	} else if err != nil {
		return false, err
	}
	if err := copyUpdateBinary(previous, filepath.Join(updatesDir, "backup", updateBinaryName)); err != nil {
		return false, err
	}
	if err := s.prepareBackupManifest(filepath.Join(updatesDir, "backup", updateBinaryName)); err != nil {
		return false, err
	}
	if err := copyUpdateBinary(newBinaryPath, current); err != nil {
		return false, err
	}
	if err := bridgeLegacyContainerExecutable(exePath, current); err != nil {
		return false, err
	}
	return true, nil
}

func bridgeLegacyContainerExecutable(exePath, current string) error {
	if exePath != "/app/"+legacyUpdateBinaryName {
		return nil
	}
	legacy := filepath.Join(filepath.Dir(current), legacyUpdateBinaryName)
	runtimeenv.WarnLegacy(exePath, "/app/"+updateBinaryName)
	// Old container entrypoints read this path on restart. Point them to the verified stage.
	linkDir, err := os.MkdirTemp(filepath.Dir(current), ".legacy-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(linkDir) }()
	link := filepath.Join(linkDir, legacyUpdateBinaryName)
	if err := os.Symlink(updateBinaryName, link); err != nil {
		return err
	}
	return os.Rename(link, legacy)
}

// CheckUpdate checks for available updates
func (s *UpdateService) CheckUpdate(ctx context.Context, force bool) (*UpdateInfo, error) {
	// Try cache first
	if !force {
		if cached, err := s.getFromCache(ctx); err == nil && cached != nil {
			return cached, nil
		}
	}

	// Fetch from GitHub
	info, err := s.fetchLatestRelease(ctx)
	if err != nil {
		// Return cached on error
		if cached, cacheErr := s.getFromCache(ctx); cacheErr == nil && cached != nil {
			cached.Warning = "Using cached data: " + err.Error()
			return cached, nil
		}
		return &UpdateInfo{
			CurrentVersion: s.currentVersion,
			LatestVersion:  s.currentVersion,
			HasUpdate:      false,
			Warning:        err.Error(),
			BuildType:      s.buildType,
		}, nil
	}

	// Cache result
	s.saveToCache(ctx, info)
	return info, nil
}

// PerformUpdate downloads and applies the update
// Uses atomic file replacement pattern for safe in-place updates
func (s *UpdateService) PerformUpdate(ctx context.Context) error {
	info, err := s.CheckUpdate(ctx, true)
	if err != nil {
		return err
	}

	if !info.HasUpdate {
		return ErrNoUpdateAvailable
	}

	return s.applyReleaseAssets(ctx, info.LatestVersion, info.ReleaseInfo.Assets)
}

// applyReleaseAssets downloads the platform archive from the given release assets,
// verifies its checksum, and atomically swaps the running binary.
// Shared by PerformUpdate (latest) and RollbackToVersion (specific older version).
func (s *UpdateService) applyReleaseAssets(ctx context.Context, version string, releaseAssets []Asset) error {
	tag := releaseVersion(version)
	if tag == "" {
		return fmt.Errorf("invalid release version %q", version)
	}
	if semver.Compare(tag, minimumRollbackVersion) < 0 {
		return rollbackBelowFloor(tag, minimumRollbackVersion)
	}

	asset, checksum, manifest, err := selectUpdateAssets(tag, releaseAssets)
	if err != nil {
		return err
	}
	for _, a := range []Asset{asset, checksum, manifest} {
		if err := validateDownloadURL(a.DownloadURL); err != nil {
			return fmt.Errorf("invalid download URL for %s: %w", a.Name, err)
		}
	}
	checksums, err := s.githubClient.FetchChecksumFile(ctx, checksum.DownloadURL)
	if err != nil {
		return fmt.Errorf("failed to download checksums: %w", err)
	}
	// Resolve both digests before downloading any executable bytes.
	archiveHash, err := findReleaseChecksum(checksums, asset.Name)
	if err != nil {
		return err
	}
	manifestHash, err := findReleaseChecksum(checksums, manifest.Name)
	if err != nil {
		return err
	}

	// Get current executable path
	exePath, err := s.executablePath()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}
	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		return fmt.Errorf("failed to resolve symlinks: %w", err)
	}

	exeDir := filepath.Dir(exePath)

	// Create temp directory in the SAME directory as executable
	// This ensures os.Rename is atomic (same filesystem)
	tempDir, exeDirWritable, err := createUpdateTempDir(exeDir)
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	if err := s.verifyReleaseManifest(ctx, tempDir, manifest, manifestHash, tag); err != nil {
		return err
	}

	archivePath := filepath.Join(tempDir, asset.Name)
	if err := s.downloadFile(ctx, asset.DownloadURL, archivePath); err != nil {
		return fmt.Errorf("download failed: %w", err)
	}

	if err := verifyReleaseChecksum(archivePath, archiveHash); err != nil {
		return fmt.Errorf("checksum verification failed: %w", err)
	}

	// Extract binary from archive
	newBinaryPath := filepath.Join(tempDir, updateBinaryName)
	if err := s.extractBinary(archivePath, newBinaryPath); err != nil {
		return fmt.Errorf("extraction failed: %w", err)
	}

	// Set executable permission before replacement
	if err := os.Chmod(newBinaryPath, 0755); err != nil {
		return fmt.Errorf("chmod failed: %w", err)
	}

	staged, err := s.stagePersistentBinary(exePath, newBinaryPath)
	if err != nil {
		return fmt.Errorf("persistent staging failed: %w", err)
	}
	if !exeDirWritable {
		if staged {
			return nil // The root entrypoint installs the staged binary on restart.
		}
		return fmt.Errorf("executable directory and persistent data directory are not writable")
	}

	// Atomic replacement using rename pattern:
	// 1. Rename current -> backup (atomic on Unix)
	// 2. Rename new -> current (atomic on Unix, same filesystem)
	// If step 2 fails, restore backup
	backupPath := exePath + ".backup"
	manifest, err := s.runningManifest()
	if err != nil {
		return fmt.Errorf("backup manifest failed: %w", err)
	}

	// Remove old backup if exists
	_ = os.Remove(backupPath)

	// Step 1: Move current binary to backup
	if err := os.Rename(exePath, backupPath); err != nil {
		if staged {
			return nil
		}
		return fmt.Errorf("backup failed: %w", err)
	}
	if err := writeBackupManifest(backupPath, manifest); err != nil {
		if restoreErr := os.Rename(backupPath, exePath); restoreErr != nil {
			return fmt.Errorf("backup manifest failed and restore failed: %w (restore error: %v)", err, restoreErr)
		}
		return fmt.Errorf("backup manifest failed: %w", err)
	}

	// Step 2: Move new binary to target location (atomic, same filesystem)
	if err := os.Rename(newBinaryPath, exePath); err != nil {
		// Restore backup on failure
		if restoreErr := os.Rename(backupPath, exePath); restoreErr != nil {
			return fmt.Errorf("replace failed and restore failed: %w (restore error: %v)", err, restoreErr)
		}
		if staged {
			return nil
		}
		return fmt.Errorf("replace failed (restored backup): %w", err)
	}

	// Success - backup file is kept for rollback capability
	// It will be cleaned up on next successful update
	return nil
}

// Rollback restores the previous version
func (s *UpdateService) Rollback() error {
	return s.rollback(context.Background())
}

func (s *UpdateService) rollback(ctx context.Context) error {
	exePath, err := s.executablePath()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}
	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		return fmt.Errorf("failed to resolve symlinks: %w", err)
	}

	floor, err := s.databaseRollbackFloor(ctx)
	if err != nil {
		return fmt.Errorf("rollback database compatibility check failed: %w", err)
	}
	persistentBackup := filepath.Join(resolveDockerDataDir(), ".updates", "backup", updateBinaryName)
	persistentCurrent := filepath.Join(resolveDockerDataDir(), ".updates", "current", updateBinaryName)
	if _, err := os.Stat(persistentBackup); os.IsNotExist(err) {
		legacy := filepath.Join(filepath.Dir(persistentBackup), legacyUpdateBinaryName)
		if _, err := os.Stat(legacy); err == nil {
			runtimeenv.WarnLegacy(legacy, persistentBackup)
			persistentBackup = legacy
		}
	}
	staged := false
	if _, err := os.Stat(persistentBackup); err == nil {
		if err := os.MkdirAll(filepath.Dir(persistentCurrent), 0755); err != nil {
			return err
		}
		if err := s.verifyBackup(persistentBackup, floor); err != nil {
			return fmt.Errorf("persistent rollback verification failed: %w", err)
		}
		if err := os.Rename(persistentBackup, persistentCurrent); err != nil {
			return fmt.Errorf("persistent rollback failed: %w", err)
		}
		if err := bridgeLegacyContainerExecutable(exePath, persistentCurrent); err != nil {
			return fmt.Errorf("persistent rollback compatibility link failed: %w", err)
		}
		staged = true
	} else if !os.IsNotExist(err) {
		return err
	}

	backupFile := exePath + ".backup"
	if _, err := os.Stat(backupFile); err == nil {
		if err := s.verifyBackup(backupFile, floor); err != nil {
			return fmt.Errorf("rollback verification failed: %w", err)
		}
		if err := os.Rename(backupFile, exePath); err != nil && !staged {
			return fmt.Errorf("rollback failed: %w", err)
		}
	} else if !staged {
		return fmt.Errorf("no backup found: %w", err)
	}

	return nil
}

// ListRollbackVersions returns up to maxRollbackVersions release versions that are
// strictly older than the current version (the current version itself is excluded),
// newest first. Draft and prerelease entries are skipped.
func (s *UpdateService) ListRollbackVersions(ctx context.Context) ([]RollbackVersion, error) {
	releases, err := s.fetchRollbackCandidates(ctx)
	if err != nil {
		return nil, err
	}

	versions := make([]RollbackVersion, 0, len(releases))
	for _, r := range releases {
		versions = append(versions, RollbackVersion{
			Version:     strings.TrimPrefix(r.TagName, "v"),
			PublishedAt: r.PublishedAt,
			HTMLURL:     r.HTMLURL,
		})
	}
	return versions, nil
}

// RollbackToVersion downloads and installs a specific older version.
// The target must be one of the versions returned by ListRollbackVersions;
// anything else (including the current version) is rejected.
func (s *UpdateService) RollbackToVersion(ctx context.Context, version string) error {
	target := releaseVersion(strings.TrimSpace(version))
	if target == "" {
		return ErrRollbackVersionNotAllowed
	}
	if semver.Compare(target, minimumRollbackVersion) < 0 {
		return rollbackBelowFloor(target, minimumRollbackVersion)
	}

	releases, err := s.fetchRollbackCandidates(ctx)
	if err != nil {
		return err
	}

	var match *GitHubRelease
	for _, r := range releases {
		if releaseVersion(r.TagName) == target {
			match = r
			break
		}
	}
	if match == nil {
		return ErrRollbackVersionNotAllowed
	}

	assets := make([]Asset, len(match.Assets))
	for i, a := range match.Assets {
		assets[i] = Asset{
			Name:        a.Name,
			DownloadURL: resolveAssetDownloadURL(a),
			Size:        a.Size,
		}
	}

	return s.applyReleaseAssets(ctx, match.TagName, assets)
}

// fetchRollbackCandidates fetches recent releases and keeps the newest
// maxRollbackVersions entries strictly older than the current version.
func (s *UpdateService) fetchRollbackCandidates(ctx context.Context) ([]*GitHubRelease, error) {
	releases, err := s.githubClient.FetchRecentReleases(ctx, resolveGitHubRepo(), rollbackFetchPageSize)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool, len(releases))
	candidates := make([]*GitHubRelease, 0, maxRollbackVersions)
	for _, r := range releases {
		if r == nil || r.Draft || r.Prerelease {
			continue
		}
		v := releaseVersion(r.TagName)
		if v == "" || seen[v] || semver.Prerelease(v) != "" || semver.Compare(v, minimumRollbackVersion) < 0 {
			continue
		}
		// Only versions strictly older than current (also excludes current itself)
		if compareVersions(v, s.currentVersion) >= 0 {
			continue
		}
		seen[v] = true
		candidates = append(candidates, r)
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		return compareVersions(
			strings.TrimPrefix(candidates[i].TagName, "v"),
			strings.TrimPrefix(candidates[j].TagName, "v"),
		) > 0
	})

	if len(candidates) > maxRollbackVersions {
		candidates = candidates[:maxRollbackVersions]
	}
	return candidates, nil
}

func (s *UpdateService) fetchLatestRelease(ctx context.Context) (*UpdateInfo, error) {
	release, err := s.githubClient.FetchLatestRelease(ctx, resolveGitHubRepo())
	if err != nil && strings.Contains(err.Error(), "GitHub API returned 404") {
		return &UpdateInfo{CurrentVersion: s.currentVersion, LatestVersion: s.currentVersion, BuildType: s.buildType}, nil
	}
	if err != nil {
		return nil, err
	}

	latestVersion := strings.TrimPrefix(release.TagName, "v")

	assets := make([]Asset, len(release.Assets))
	for i, a := range release.Assets {
		assets[i] = Asset{
			Name:        a.Name,
			DownloadURL: resolveAssetDownloadURL(a),
			Size:        a.Size,
		}
	}

	return &UpdateInfo{
		CurrentVersion: s.currentVersion,
		LatestVersion:  latestVersion,
		HasUpdate:      compareVersions(s.currentVersion, latestVersion) < 0,
		ReleaseInfo: &ReleaseInfo{
			Name:        release.Name,
			Body:        release.Body,
			PublishedAt: release.PublishedAt,
			HTMLURL:     release.HTMLURL,
			Assets:      assets,
		},
		Cached:    false,
		BuildType: s.buildType,
	}, nil
}

func (s *UpdateService) downloadFile(ctx context.Context, downloadURL, dest string) error {
	return s.githubClient.DownloadFile(ctx, downloadURL, dest, maxDownloadSize)
}

func releaseVersion(version string) string {
	tag := "v" + strings.TrimPrefix(version, "v")
	core, _, _ := strings.Cut(tag, "-")
	core, _, _ = strings.Cut(core, "+")
	if strings.Count(core, ".") != 2 || !semver.IsValid(tag) {
		return ""
	}
	return tag
}

func backupVersion(version string) string {
	version = strings.TrimSpace(version)
	if releaseVersion(version) == "" {
		return ""
	}
	return "v" + strings.TrimPrefix(version, "v")
}

func rollbackBelowFloor(version, floor string) error {
	return fmt.Errorf("release %s is below the database rollback compatibility floor %s: %w", version, floor, ErrRollbackVersionNotAllowed)
}

func selectUpdateAssets(version string, releaseAssets []Asset) (archive, checksum, manifest Asset, err error) {
	suffix := fmt.Sprintf("_%s_%s_%s.tar.gz", strings.TrimPrefix(version, "v"), runtime.GOOS, runtime.GOARCH)
	var wrongProduct string
	for _, asset := range releaseAssets {
		switch asset.Name {
		case updateBinaryName + suffix:
			archive = asset
		case "checksums.txt":
			checksum = asset
		case releaseManifestName, "release-manifest.json":
			if manifest.Name != releaseManifestName {
				manifest = asset
			}
		default:
			if strings.HasSuffix(asset.Name, suffix) {
				wrongProduct = asset.Name
			}
		}
	}
	if archive.DownloadURL == "" {
		if wrongProduct != "" {
			err = fmt.Errorf("wrong product release asset %q: expected %s", wrongProduct, updateBinaryName+suffix)
			return
		}
		err = fmt.Errorf("no compatible release found for %s/%s", runtime.GOOS, runtime.GOARCH)
	} else if checksum.DownloadURL == "" {
		err = fmt.Errorf("checksum asset checksums.txt not found for %s", archive.Name)
	} else if manifest.DownloadURL == "" {
		err = fmt.Errorf("OmniHive release manifest %s not found", releaseManifestName)
	}
	return
}

// validateDownloadURL checks if the URL is from an allowed domain
// SECURITY: This prevents SSRF and ensures downloads only come from trusted GitHub domains
func validateDownloadURL(rawURL string) error {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	// Must be HTTPS
	if parsedURL.Scheme != "https" {
		return fmt.Errorf("only HTTPS URLs are allowed")
	}

	// Check against allowed hosts
	host := parsedURL.Host
	// GitHub release URLs can be from github.com or objects.githubusercontent.com
	if host != "api.github.com" && host != allowedDownloadHost &&
		!strings.HasSuffix(host, "."+allowedDownloadHost) &&
		host != allowedAssetHost &&
		!strings.HasSuffix(host, "."+allowedAssetHost) {
		return fmt.Errorf("download from untrusted host: %s", host)
	}

	return nil
}

func findReleaseChecksum(data []byte, name string) (string, error) {
	var expected string
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		parts := strings.Fields(scanner.Text())
		if len(parts) != 2 || strings.TrimPrefix(parts[1], "*") != name {
			continue
		}
		if expected != "" {
			return "", fmt.Errorf("duplicate checksum entry for %s", name)
		}
		hash, err := hex.DecodeString(parts[0])
		if err != nil || len(hash) != sha256.Size {
			return "", fmt.Errorf("invalid SHA256 checksum for %s", name)
		}
		expected = strings.ToLower(parts[0])
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("failed to read checksums: %w", err)
	}
	if expected == "" {
		return "", fmt.Errorf("checksum not found for %s", name)
	}
	return expected, nil
}

func verifyReleaseChecksum(filePath, expected string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	actual := hex.EncodeToString(h.Sum(nil))
	if actual != expected {
		return fmt.Errorf("checksum mismatch for %s: expected %s, got %s", filepath.Base(filePath), expected, actual)
	}
	return nil
}

func (s *UpdateService) verifyReleaseManifest(ctx context.Context, dir string, asset Asset, hash, version string) error {
	manifestPath := filepath.Join(dir, asset.Name)
	if err := s.githubClient.DownloadFile(ctx, asset.DownloadURL, manifestPath, maxReleaseManifestSize); err != nil {
		return fmt.Errorf("download release manifest failed: %w", err)
	}
	if err := verifyReleaseChecksum(manifestPath, hash); err != nil {
		return fmt.Errorf("release manifest verification failed: %w", err)
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return err
	}
	var manifest struct {
		Product                string `json:"product"`
		Version                string `json:"version"`
		MinimumRollbackVersion string `json:"minimum_rollback_version"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("invalid release manifest: %w", err)
	}
	if manifest.Product != updateBinaryName {
		return fmt.Errorf("wrong product %q in release manifest: expected %s", manifest.Product, updateBinaryName)
	}
	if releaseVersion(manifest.Version) != version {
		return fmt.Errorf("release manifest version %q does not match requested release %s", manifest.Version, version)
	}
	floor := releaseVersion(manifest.MinimumRollbackVersion)
	if floor == "" || semver.Prerelease(floor) != "" || semver.Build(floor) != "" {
		return fmt.Errorf("invalid minimum rollback version %q in release manifest", manifest.MinimumRollbackVersion)
	}
	if semver.Compare(version, floor) < 0 {
		return rollbackBelowFloor(version, floor)
	}
	return nil
}

func (s *UpdateService) extractBinary(archivePath, destPath string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer func() { _ = gzr.Close() }()

	tr := tar.NewReader(gzr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return fmt.Errorf("OmniHive executable not found in archive")
		}
		if err != nil {
			return err
		}
		// The release binary is a regular file at the root of the archive.
		name := strings.TrimPrefix(hdr.Name, "./")
		if name == legacyUpdateBinaryName || name == legacyUpdateBinaryName+".exe" ||
			name == "omnihive-ai-gw" || name == "omnihive-ai-gw.exe" {
			return fmt.Errorf("wrong product executable %q in archive", hdr.Name)
		}
		if hdr.Typeflag != tar.TypeReg || (name != updateBinaryName && name != updateBinaryName+".exe") {
			continue
		}
		if hdr.Size <= 0 || hdr.Size > maxDownloadSize {
			return fmt.Errorf("invalid executable size %d in archive", hdr.Size)
		}
		out, err := os.Create(destPath)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(out, tr)
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	}
}

func (s *UpdateService) getFromCache(ctx context.Context) (*UpdateInfo, error) {
	data, err := s.cache.GetUpdateInfo(ctx)
	if err != nil {
		return nil, err
	}

	var cached struct {
		Latest      string       `json:"latest"`
		ReleaseInfo *ReleaseInfo `json:"release_info"`
		Timestamp   int64        `json:"timestamp"`
	}
	if err := json.Unmarshal([]byte(data), &cached); err != nil {
		return nil, err
	}

	if time.Now().Unix()-cached.Timestamp > updateCacheTTL {
		return nil, fmt.Errorf("cache expired")
	}

	return &UpdateInfo{
		CurrentVersion: s.currentVersion,
		LatestVersion:  cached.Latest,
		HasUpdate:      compareVersions(s.currentVersion, cached.Latest) < 0,
		ReleaseInfo:    cached.ReleaseInfo,
		Cached:         true,
		BuildType:      s.buildType,
	}, nil
}

func (s *UpdateService) saveToCache(ctx context.Context, info *UpdateInfo) {
	cacheData := struct {
		Latest      string       `json:"latest"`
		ReleaseInfo *ReleaseInfo `json:"release_info"`
		Timestamp   int64        `json:"timestamp"`
	}{
		Latest:      info.LatestVersion,
		ReleaseInfo: info.ReleaseInfo,
		Timestamp:   time.Now().Unix(),
	}

	data, _ := json.Marshal(cacheData)
	_ = s.cache.SetUpdateInfo(ctx, string(data), time.Duration(updateCacheTTL)*time.Second)
}

// compareVersions compares two semantic versions
func compareVersions(current, latest string) int {
	currentParts := parseVersion(current)
	latestParts := parseVersion(latest)

	for i := 0; i < 3; i++ {
		if currentParts[i] < latestParts[i] {
			return -1
		}
		if currentParts[i] > latestParts[i] {
			return 1
		}
	}
	return 0
}

func parseVersion(v string) [3]int {
	v = strings.TrimPrefix(v, "v")
	if idx := strings.IndexByte(v, '-'); idx != -1 {
		v = v[:idx]
	}
	parts := strings.Split(v, ".")
	result := [3]int{0, 0, 0}
	for i := 0; i < len(parts) && i < 3; i++ {
		if parsed, err := strconv.Atoi(parts[i]); err == nil {
			result[i] = parsed
		}
	}
	return result
}
