//go:build unit

package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReleaseCompatibilityFloors(t *testing.T) {
	for _, tc := range []struct {
		target, db, artifact, floor string
		fresh, eligible             bool
	}{
		{"0.3.4", "0.2.23-omnihive.2", "0.3.0", "v0.3.0", false, true},
		{"0.3.3", "0.3.4", "0.3.0", "v0.3.4", false, false},
		{"0.3.4-rc.2", "0.3.4-rc.10", "0.3.0", "v0.3.4-rc.10", false, false},
		{"0.3.4-rc.10", "0.3.4-rc.2", "0.3.0", "v0.3.4-rc.2", false, true},
		{"0.3.4-rc.10", "0.3.4", "", "v0.3.4", false, false},
		{"0.3.4", "", "0.3.0", "v0.3.0", true, true},
		{"0.3.4", "", "0.3.0", "v0.2.5", false, false},
		{"0.3.4", "broken", "0.3.0", "v0.2.5", false, false},
		{"0.2.4", "0.2.3", "0.2.3", "v0.2.5", false, false},
	} {
		r, err := ResolveReleaseCompatibility(tc.target, tc.db, tc.artifact, tc.fresh)
		require.Equal(t, tc.eligible, r.Eligible, "%+v", tc)
		require.Equal(t, tc.floor, r.EffectiveFloor)
		if tc.eligible {
			require.NoError(t, err)
		} else {
			require.Error(t, err)
		}
	}
}

func TestUpdateServiceDBFloorBeforeSwap(t *testing.T) {
	for _, mode := range []string{"raised", "unavailable", "changed-during-download", "upgrade-033-to-034"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("OMNIHIVE_DATA_DIR", root)
			exe := filepath.Join(root, "omnihive")
			require.NoError(t, os.WriteFile(exe, []byte("original"), 0755))
			client, assets := updateTestRelease("0.3.4", updateTestArchive(t, "omnihive", "v034"), "omnihive", "0.3.0")
			s := NewUpdateService(nil, client, "0.3.3", "release")
			s.executablePath = func() (string, error) { return exe, nil }
			s.runningBinary = s.executablePath
			s.backupKey = []byte("test-backup-key-32-bytes-long-value")
			reads := 0
			s.readRollbackFloor = func(context.Context) (string, error) {
				reads++
				if mode == "unavailable" {
					return "", errors.New("DB unavailable")
				}
				if mode == "raised" || (mode == "changed-during-download" && reads >= 3) {
					return "0.3.5", nil
				}
				return "0.2.23-omnihive.2", nil
			}
			err := s.applyReleaseAssets(context.Background(), "0.3.4", assets)
			data, readErr := os.ReadFile(exe)
			require.NoError(t, readErr)
			if mode == "upgrade-033-to-034" {
				require.NoError(t, err)
				require.Equal(t, "v034", string(data))
			} else {
				require.Error(t, err)
				require.Equal(t, "original", string(data))
				require.NoDirExists(t, filepath.Join(root, ".updates"))
			}
		})
	}
}

func TestUpdateServiceCandidateDBFloor(t *testing.T) {
	s := newRollbackTestService("0.3.5", []*GitHubRelease{{TagName: "v0.3.4"}, {TagName: "v0.3.3"}})
	s.readRollbackFloor = func(context.Context) (string, error) { return "0.3.4", nil }
	versions, err := s.ListRollbackVersions(context.Background())
	require.NoError(t, err)
	require.Len(t, versions, 1)
	require.Equal(t, "v0.3.4", versions[0].EffectiveFloor)
	report, err := s.ListRollbackEligibility(context.Background())
	require.NoError(t, err)
	require.Len(t, report.ExcludedVersions, 1)
	require.Equal(t, "0.3.3", report.ExcludedVersions[0].Version)
	require.Contains(t, report.ExcludedVersions[0].ExclusionReason, "v0.3.4")
	require.Error(t, s.RollbackToVersion(context.Background(), "0.3.3"))
	s.readRollbackFloor = func(context.Context) (string, error) { return "", errors.New("DB unavailable") }
	_, err = s.ListRollbackVersions(context.Background())
	require.Error(t, err)
}

func TestUpdateServiceCandidateArtifactFloor(t *testing.T) {
	client, assets := updateTestRelease("0.3.3", []byte("archive"), "omnihive", "0.3.4")
	r := &GitHubRelease{TagName: "v0.3.3"}
	for _, a := range assets {
		r.Assets = append(r.Assets, GitHubAsset{Name: a.Name, BrowserDownloadURL: a.DownloadURL})
	}
	client.recentReleases = []*GitHubRelease{r}
	s := NewUpdateService(nil, client, "0.3.4", "release")
	s.readRollbackFloor = func(context.Context) (string, error) { return "0.3.0", nil }
	report, err := s.ListRollbackEligibility(context.Background())
	require.NoError(t, err)
	require.Empty(t, report.Versions)
	require.Len(t, report.ExcludedVersions, 1)
	require.Equal(t, "v0.3.4", report.ExcludedVersions[0].EffectiveFloor)
	exe := filepath.Join(t.TempDir(), "omnihive")
	require.NoError(t, os.WriteFile(exe, []byte("original"), 0755))
	s.executablePath = func() (string, error) { return exe, nil }
	require.ErrorContains(t, s.RollbackToVersion(context.Background(), "0.3.3"), "v0.3.4")
	data, err := os.ReadFile(exe)
	require.NoError(t, err)
	require.Equal(t, "original", string(data))
}

func TestUpdateServiceLocalRollbackRefreshesDBFloor(t *testing.T) {
	root := t.TempDir()
	t.Setenv("OMNIHIVE_DATA_DIR", root)
	exe := filepath.Join(root, "omnihive")
	require.NoError(t, os.WriteFile(exe, []byte("current"), 0755))
	require.NoError(t, os.WriteFile(exe+".backup", []byte("backup"), 0755))
	s := NewUpdateService(nil, nil, "0.3.3", "release")
	s.backupKey = make([]byte, 32)
	s.executablePath = func() (string, error) { return exe, nil }
	require.NoError(t, s.prepareBackupManifest(exe+".backup"))
	reads := 0
	s.readRollbackFloor = func(context.Context) (string, error) {
		reads++
		if reads == 1 {
			return "0.3.0", nil
		}
		return "0.3.4", nil
	}
	require.ErrorIs(t, s.Rollback(), ErrRollbackVersionNotAllowed)
	data, err := os.ReadFile(exe)
	require.NoError(t, err)
	require.Equal(t, "current", string(data))
	require.FileExists(t, exe+".backup")
}
