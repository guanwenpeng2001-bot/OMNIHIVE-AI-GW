//go:build unit

package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/cursor"
	"github.com/stretchr/testify/require"
)

func TestCursorRefreshRaceWithAdminImport(t *testing.T) {
	for _, background := range []bool{false, true} {
		path := "request"
		if background {
			path = "background"
		}
		for _, outcome := range []string{"success", "temporary", "reauth", "cancelled"} {
			t.Run(path+"/"+outcome, func(t *testing.T) {
				account := cursorAutoTestAccount()
				repo := &cursorOAuthMemoryRepo{account: snapshotOAuthRefreshAccount(account)}
				api := NewOAuthRefreshAPI(repo, nil)
				refresher := NewCursorTokenRefresher()
				entered, release := make(chan struct{}), make(chan struct{})
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				refresher.refresh = func(context.Context, string) (cursor.OAuthTokens, error) {
					close(entered)
					<-release
					switch outcome {
					case "temporary":
						return cursor.OAuthTokens{}, errors.New("cursor: refresh transport failed")
					case "reauth":
						return cursor.OAuthTokens{}, cursor.ErrReauthRequired
					}
					return cursor.OAuthTokens{AccessToken: "synthetic-stale-result", RefreshToken: "synthetic-stale-rotation", ExpiresAt: time.Now().Add(time.Hour)}, nil
				}
				done := make(chan struct{})
				var result *OAuthRefreshResult
				var refreshErr error
				go func() {
					defer close(done)
					if background {
						service := NewTokenRefreshService(repo, nil, nil, nil, nil, nil, nil, nil, nil)
						service.SetRefreshAPI(api)
						refreshErr = service.refreshWithRetry(ctx, account, refresher, refresher, cursorRefreshWindow)
					} else {
						result, refreshErr = api.RefreshIfNeeded(withOAuthRefreshRequestPath(ctx), account, refresher, cursorRefreshWindow)
					}
				}()
				<-entered
				credentials := shallowCopyMap(account.Credentials)
				credentials["access_token"], credentials["refresh_token"] = "synthetic-imported", "synthetic-imported-refresh"
				credentials["machine_id"], credentials["mac_machine_id"] = strings.Repeat("c", 64), strings.Repeat("d", 64)
				credentials["client_version"] = "import-version"
				admin := &adminServiceImpl{accountRepo: repo}
				updated, updateErr := admin.UpdateAccount(context.Background(), account.ID, &UpdateAccountInput{Credentials: credentials, Status: StatusError})
				// Release even if the update assertion fails, so no worker is stranded.
				if outcome == "cancelled" {
					cancel()
				}
				close(release)
				<-done
				require.NoError(t, updateErr)
				current, err := repo.GetByID(context.Background(), account.ID)
				require.NoError(t, err)
				require.Equal(t, updated.Credentials, current.Credentials)
				require.Equal(t, updated.Status, current.Status)
				require.Equal(t, updated.ErrorMessage, current.ErrorMessage)
				require.Equal(t, updated.Schedulable, current.Schedulable)
				require.Equal(t, updated.TempUnschedulableUntil, current.TempUnschedulableUntil)
				require.Equal(t, updated.TempUnschedulableReason, current.TempUnschedulableReason)
				require.Empty(t, repo.writes)
				if !background {
					require.NoError(t, refreshErr)
					require.False(t, result.Refreshed)
					require.Equal(t, current, result.Account)
				} else if outcome != "cancelled" {
					require.True(t, refreshErr == nil || errors.Is(refreshErr, errRefreshSkipped))
				}
			})
		}
	}
}
