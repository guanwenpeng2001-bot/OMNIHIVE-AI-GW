package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCursorRefreshCASUsesFullCredentialsAndAtomicOutbox(t *testing.T) {
	for _, mutation := range []string{"success", "temporary", "reauth"} {
		for _, affected := range []int64{0, 1} {
			t.Run(mutation+string(rune('0'+affected)), func(t *testing.T) {
				exec := &recordingSQLExecutor{result: rowsAffectedResult(affected)}
				repo := newAccountRepositoryWithSQL(nil, exec, nil)
				expected := map[string]any{"refresh_token": "synthetic-attempt", "machine_id": "synthetic-machine", "_token_version": 9}
				var applied bool
				var err error
				if mutation == "success" {
					applied, err = repo.UpdateCursorOAuthCredentialsIfUnchanged(context.Background(), 42, expected, map[string]any{"access_token": "synthetic-result"})
				} else {
					applied, err = repo.SetCursorOAuthRefreshFailureIfUnchanged(context.Background(), 42, expected, mutation == "reauth", time.Now().Add(time.Minute))
				}
				require.NoError(t, err)
				require.Equal(t, affected == 1, applied)
				require.Len(t, exec.execQueries, 1)
				query := normalizeSQLWhitespace(exec.execQueries[0])
				require.Contains(t, query, "WITH updated AS")
				if mutation == "reauth" {
					require.Contains(t, query, "status = 'error', error_message = $5, schedulable = false")
				}
				require.Contains(t, query, "WHERE id = $1 AND deleted_at IS NULL AND platform = $2 AND type = $3 AND credentials = $4::jsonb")
				require.Contains(t, query, "INSERT INTO scheduler_outbox")
				require.Contains(t, query, "FROM updated")
				require.Equal(t, service.PlatformCursor, exec.execArgs[0][1])
				require.Equal(t, service.AccountTypeOAuth, exec.execArgs[0][2])
				require.JSONEq(t, `{"refresh_token":"synthetic-attempt","machine_id":"synthetic-machine","_token_version":9}`, exec.execArgs[0][3].(string))
			})
		}
	}
}
