//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/cursor"
	"github.com/stretchr/testify/require"
)

// Any read or generic cleanup after Update is a tripwire: credentials have
// committed, so such a failure must not change PKCE's observable outcome.
type cursorReauthorizationRepo struct {
	*cursorOAuthMemoryRepo
	committed bool
}

func (repo *cursorReauthorizationRepo) GetByID(ctx context.Context, id int64) (*Account, error) {
	if repo.committed {
		return nil, errors.New("synthetic post-commit read failure")
	}
	return repo.cursorOAuthMemoryRepo.GetByID(ctx, id)
}
func (repo *cursorReauthorizationRepo) Update(ctx context.Context, account *Account) error {
	err := repo.cursorOAuthMemoryRepo.Update(ctx, account)
	repo.committed = err == nil
	return err
}
func (repo *cursorReauthorizationRepo) UpdateWithCursorRefreshRecovery(ctx context.Context, account *Account, _, _ *bool, _ *float64) error {
	return repo.Update(ctx, account)
}

func (repo *cursorReauthorizationRepo) ClearError(context.Context, int64) error {
	return errors.New("generic cleanup must not run")
}
func (repo *cursorReauthorizationRepo) ClearTempUnschedulable(context.Context, int64) error {
	return errors.New("post-commit cleanup must not run")
}

func TestCursorAuthorizationEditPersistsTargetedRecoveryAtomically(t *testing.T) {
	for _, cursorError := range []bool{false, true} {
		name := "unrelated_error"
		if cursorError {
			name = "cursor_reauth"
		}
		t.Run(name, func(t *testing.T) {
			account := cursorAutoTestAccount()
			until := time.Now().Add(time.Hour)
			account.Status, account.ErrorMessage = StatusError, "manual account review required"
			account.TempUnschedulableUntil, account.TempUnschedulableReason = &until, "unrelated provider cooldown"
			account.RateLimitedAt, account.RateLimitResetAt = &until, &until
			account.Extra = map[string]any{"model_rate_limits": map[string]any{"model": "quota"}}
			if cursorError {
				account.ErrorMessage = "Cursor reauth required: token refresh rejected"
				account.TempUnschedulableReason = "Cursor token refresh temporarily unavailable"
			}
			repo := &cursorReauthorizationRepo{cursorOAuthMemoryRepo: &cursorOAuthMemoryRepo{account: account}}
			admin := &adminServiceImpl{accountRepo: repo}
			auth := NewCursorAuthService()
			auth.poll = func(context.Context, cursor.PKCE) (*cursor.OAuthTokens, error) {
				return &cursor.OAuthTokens{AccessToken: "synthetic-browser-access", RefreshToken: "synthetic-browser-refresh", ExpiresAt: until}, nil
			}
			state, err := auth.Start(func(ctx context.Context, credentials map[string]any) (int64, error) {
				updated, err := admin.UpdateAccount(ctx, account.ID, &UpdateAccountInput{Credentials: credentials})
				if err != nil {
					return 0, err
				}
				return updated.ID, nil
			})
			require.NoError(t, err)
			for range 2 {
				state, err = auth.Poll(context.Background(), state.ID, false)
				require.NoError(t, err)
				require.Equal(t, "success", state.Status)
			}
			require.True(t, repo.committed)
			current := repo.account
			require.Equal(t, "synthetic-browser-access", current.GetCredential("access_token"))
			require.Equal(t, "synthetic-browser-refresh", current.GetCredential("refresh_token"))
			require.Equal(t, account.RateLimitedAt, current.RateLimitedAt)
			require.Equal(t, account.RateLimitResetAt, current.RateLimitResetAt)
			require.Equal(t, account.Extra, current.Extra)
			if cursorError {
				require.Equal(t, StatusActive, current.Status)
				require.True(t, current.Schedulable)
				require.Empty(t, current.ErrorMessage)
				require.Nil(t, current.TempUnschedulableUntil)
			} else {
				require.Equal(t, StatusError, current.Status)
				require.Equal(t, account.ErrorMessage, current.ErrorMessage)
				require.Equal(t, account.TempUnschedulableUntil, current.TempUnschedulableUntil)
				require.Equal(t, account.TempUnschedulableReason, current.TempUnschedulableReason)
			}
		})
	}
}
