package service

import (
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPricingAtomicReplaceRetry(t *testing.T) {
	// These tests replace a package dependency and must remain non-parallel.
	for _, tc := range []struct {
		name     string
		code     syscall.Errno
		failures int
	}{
		{"sharing violation recovers", syscall.Errno(32), 2},
		{"access denied recovers", syscall.Errno(5), 2},
		{"sharing violation exhausted", syscall.Errno(32), 5},
		{"access denied exhausted", syscall.Errno(5), 5},
		{"other error returns immediately", syscall.Errno(2), 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "model_pricing.json")
			require.NoError(t, os.WriteFile(path, []byte("previous"), 0644))
			originalRename := renamePricingFile
			t.Cleanup(func() { renamePricingFile = originalRename })
			synctest.Test(t, func(t *testing.T) {
				attempts := 0
				var originalErr *os.LinkError
				var tempName string
				renamePricingFile = func(oldpath, newpath string) error {
					attempts++
					if tempName == "" {
						tempName = oldpath
						originalErr = &os.LinkError{Op: "rename", Old: oldpath, New: newpath, Err: tc.code}
					}
					require.Equal(t, tempName, oldpath)
					require.Equal(t, path, newpath)
					previous, err := os.ReadFile(path)
					require.NoError(t, err)
					require.Equal(t, "previous", string(previous), "destination survives every failed attempt")
					if attempts <= tc.failures {
						return originalErr
					}
					return originalRename(oldpath, newpath)
				}
				start := time.Now()
				err := writePricingFileAtomically(path, []byte("replacement"))
				wantContents := "previous"
				switch {
				case runtime.GOOS != "windows" || tc.code == syscall.Errno(2):
					require.Same(t, originalErr, err, "non-retryable errors are returned unchanged")
					require.Equal(t, 1, attempts)
					require.Zero(t, time.Since(start))
				case tc.failures < 5:
					require.NoError(t, err)
					require.Equal(t, 3, attempts)
					require.Equal(t, 75*time.Millisecond, time.Since(start))
					wantContents = "replacement"
				default:
					require.ErrorIs(t, err, originalErr)
					require.ErrorIs(t, err, tc.code)
					require.ErrorContains(t, err, originalErr.Error())
					require.ErrorContains(t, err, "5 attempts")
					require.Equal(t, 5, attempts)
					require.Equal(t, 250*time.Millisecond, time.Since(start))
				}
				contents, readErr := os.ReadFile(path)
				require.NoError(t, readErr)
				require.Equal(t, wantContents, string(contents))
				_, statErr := os.Stat(tempName)
				require.True(t, os.IsNotExist(statErr), "temporary file is cleaned up")
			})
		})
	}
}
