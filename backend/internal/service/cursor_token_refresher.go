package service

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/cursor"
)

const cursorRefreshWindow = 5 * time.Minute

type CursorTokenRefresher struct {
	refresh func(context.Context, string) (cursor.OAuthTokens, error)
}

func NewCursorTokenRefresher() *CursorTokenRefresher {
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &CursorTokenRefresher{refresh: func(ctx context.Context, token string) (cursor.OAuthTokens, error) {
		return cursor.RefreshToken(ctx, client, cursor.APIBaseURL, token)
	}}
}

func (refresher *CursorTokenRefresher) CacheKey(account *Account) string {
	return fmt.Sprintf("cursor:oauth:%d", account.ID)
}

func (refresher *CursorTokenRefresher) CanRefresh(account *Account) bool {
	return account.IsCursor() && account.Type == AccountTypeOAuth
}

func (refresher *CursorTokenRefresher) NeedsRefresh(account *Account, window time.Duration) bool {
	if !refresher.CanRefresh(account) {
		return false
	}
	if account.TempUnschedulableUntil != nil && time.Now().Before(*account.TempUnschedulableUntil) {
		return false
	}
	if window < cursorRefreshWindow {
		window = cursorRefreshWindow
	}
	expiry := account.GetCredentialAsTime("expires_at")
	return account.GetCredential("access_token") == "" || expiry == nil || time.Until(*expiry) <= window
}

func (refresher *CursorTokenRefresher) Refresh(ctx context.Context, account *Account) (map[string]any, error) {
	if account.GetCredential("refresh_token") == "" {
		return nil, cursor.ErrReauthRequired
	}
	tokens, err := refresher.refresh(ctx, account.GetCredential("refresh_token"))
	if err != nil {
		return nil, err
	}
	credentials := map[string]any{"access_token": tokens.AccessToken, "expires_at": strconv.FormatInt(tokens.ExpiresAt.Unix(), 10)}
	if tokens.RefreshToken != "" {
		credentials["refresh_token"] = tokens.RefreshToken
	}
	return MergeCredentials(account.Credentials, credentials), nil
}

// The rejected token is request-local: the locked DB recheck reuses a concurrent
// winner's token instead of forcing a second refresh with an infinite window.
type cursorRejectedTokenExecutor struct {
	*CursorTokenRefresher
	rejected string
}

func (executor cursorRejectedTokenExecutor) NeedsRefresh(account *Account, window time.Duration) bool {
	return executor.rejected != "" && account.GetCredential("access_token") == executor.rejected || executor.CursorTokenRefresher.NeedsRefresh(account, window)
}
