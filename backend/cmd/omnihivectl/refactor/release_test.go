package refactor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestReleaseCheck(t *testing.T) {
	for _, mode := range []string{"fresh", "existing", "db-raised", "db-unavailable", "invalid-db", "missing-dsn", "tampered-manifest", "wrong-product", "wrong-version", "tampered-archive"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			manifest := filepath.Join(dir, "omnihive-release-manifest.json")
			checksums := filepath.Join(dir, "checksums.txt")
			archive := filepath.Join(dir, "omnihive_0.3.4_linux_amd64.tar.gz")
			body := `{"product":"omnihive","version":"0.3.4","minimum_rollback_version":"0.3.0"}`
			if mode == "wrong-product" {
				body = `{"product":"sub2api","version":"0.3.4","minimum_rollback_version":"0.3.0"}`
			}
			if mode == "wrong-version" {
				body = `{"product":"omnihive","version":"0.3.3","minimum_rollback_version":"0.3.0"}`
			}
			require.NoError(t, os.WriteFile(manifest, []byte(body), 0600))
			require.NoError(t, os.WriteFile(archive, []byte("archive"), 0600))
			require.NoError(t, os.WriteFile(checksums, []byte(fmt.Sprintf("%x  %s\n%x  %s\n", sha256.Sum256([]byte(body)), filepath.Base(manifest), sha256.Sum256([]byte("archive")), filepath.Base(archive))), 0600))
			if mode == "tampered-manifest" {
				require.NoError(t, os.WriteFile(manifest, []byte("{}"), 0600))
			}
			if mode == "tampered-archive" {
				require.NoError(t, os.WriteFile(archive, []byte("tampered"), 0600))
			}
			args := []string{"check", "--target", "0.3.4", "--manifest", manifest, "--checksums", checksums, "--archive", archive, "--json"}
			t.Setenv("OMNIHIVE_RELEASE_DSN", "password=sentinel_secret")
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			if mode == "fresh" {
				args = append(args, "--fresh")
			} else if mode == "missing-dsn" {
				t.Setenv("OMNIHIVE_RELEASE_DSN", "")
			} else {
				q := mock.ExpectQuery("SELECT minimum_rollback_version FROM release_schema_compatibility WHERE singleton")
				floor := "0.2.23-omnihive.2"
				if mode == "db-raised" {
					floor = "0.3.5"
				}
				if mode == "invalid-db" {
					floor = "corrupt"
				}
				if mode == "db-unavailable" {
					q.WillReturnError(errors.New("sentinel_secret"))
				} else {
					q.WillReturnRows(sqlmock.NewRows([]string{"minimum_rollback_version"}).AddRow(floor))
				}
				mock.ExpectClose()
			}
			var out, stderr bytes.Buffer
			code := runReleaseWithDB(context.Background(), args, &out, &stderr, func(string) (*sql.DB, error) { return db, nil })
			if mode == "fresh" || mode == "existing" {
				require.Zero(t, code, stderr.String())
				require.Contains(t, out.String(), `"effective_floor":"v0.3.0"`)
				require.Contains(t, out.String(), `"eligible":true`)
			} else {
				require.Equal(t, 1, code)
			}
			require.NotContains(t, stderr.String()+out.String(), "sentinel_secret")
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
