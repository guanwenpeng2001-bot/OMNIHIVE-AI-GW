package service

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/cursor"
	"golang.org/x/sync/singleflight"
)

// CursorSessionCache implementations own distributed refresh locking. The
// credential digest prevents a changed key from reusing its predecessor's JWT.
type CursorSessionCache interface {
	Delete(context.Context, string) error
	Get(context.Context, string) (cursor.Session, error)
	Set(context.Context, string, cursor.Session) error
	Lock(context.Context, string) (func(), bool, error)
}

type CursorSessionProvider struct {
	cache      CursorSessionCache
	flights    singleflight.Group
	exchange   func(context.Context, string) (cursor.Session, error)
	refreshAPI *OAuthRefreshAPI
	refresher  *CursorTokenRefresher
}

func ProvideCursorSessionProvider(cache CursorSessionCache, api *OAuthRefreshAPI) *CursorSessionProvider {
	provider := NewCursorSessionProvider(cache)
	provider.refreshAPI = api
	provider.refresher = NewCursorTokenRefresher()
	return provider
}

func NewCursorSessionProvider(cache CursorSessionCache) *CursorSessionProvider {
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &CursorSessionProvider{cache: cache, exchange: func(ctx context.Context, key string) (cursor.Session, error) {
		return cursor.ExchangeKey(ctx, client, cursor.APIBaseURL, key)
	}}
}

func cursorSessionKey(account *Account) string {
	if account.Type == AccountTypeOAuth {
		digest := sha256.Sum256([]byte(account.GetCredential("access_token") + "\x00" + account.GetCredential("refresh_token")))
		return fmt.Sprintf("cursor:session:%d:%x", account.ID, digest)
	}
	digest := sha256.Sum256([]byte(account.GetCredential("api_key")))
	return fmt.Sprintf("cursor:session:%d:%x", account.ID, digest)
}

// rejectedToken is empty for ordinary reads. A rejected token is replaced once;
// another caller's already-refreshed token can be reused without another exchange.
func (provider *CursorSessionProvider) GetSessionToken(ctx context.Context, account *Account, rejectedToken string) (string, error) {
	ctx = cursorAccountContext(ctx, account)
	if account.IsCursor() && account.Type == AccountTypeOAuth {
		// Unsaved previews cannot enter the durable account refresh API.
		if account.ID == 0 {
			token := account.GetCredential("access_token")
			expiry := account.GetCredentialAsTime("expires_at")
			if token == "" || token == rejectedToken || (expiry != nil && !time.Now().Before(*expiry)) {
				return "", &cursor.HTTPError{Status: http.StatusUnauthorized}
			}
			return token, nil
		}
		return provider.getOAuthSessionToken(ctx, account, rejectedToken)
	}
	if !account.IsCursor() || account.Type != AccountTypeAPIKey {
		return "", fmt.Errorf("cursor: unsupported session account type")
	}
	key := cursorSessionKey(account)
	valid := func(session cursor.Session) bool {
		return session.Token != "" && session.Token != rejectedToken && time.Now().Before(session.ExpiresAt)
	}
	if session, err := provider.cache.Get(ctx, key); err == nil && valid(session) {
		return session.Token, nil
	}
	flightKey := fmt.Sprintf("%s:%x", key, sha256.Sum256([]byte(rejectedToken)))
	result := provider.flights.DoChan(flightKey, func() (any, error) {
		refreshCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
		defer cancel()
		for {
			if session, err := provider.cache.Get(refreshCtx, key); err == nil && valid(session) {
				return session.Token, nil
			}
			release, acquired, err := provider.cache.Lock(refreshCtx, key)
			if err != nil {
				return nil, fmt.Errorf("cursor: session cache unavailable")
			}
			if acquired {
				defer release()
				if session, err := provider.cache.Get(refreshCtx, key); err == nil && valid(session) {
					return session.Token, nil
				}
				session, err := provider.exchange(refreshCtx, account.GetCredential("api_key"))
				if err != nil {
					return nil, err
				}
				if err := provider.cache.Set(refreshCtx, key, session); err != nil {
					return nil, fmt.Errorf("cursor: storing session failed")
				}
				return session.Token, nil
			}
			timer := time.NewTimer(50 * time.Millisecond)
			select {
			case <-refreshCtx.Done():
				timer.Stop()
				return nil, refreshCtx.Err()
			case <-timer.C:
			}
		}
	})
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case completed := <-result:
		if completed.Err != nil {
			return "", completed.Err
		}
		return completed.Val.(string), nil
	}
}

func (provider *CursorSessionProvider) getOAuthSessionToken(ctx context.Context, account *Account, rejected string) (string, error) {
	flightKey := fmt.Sprintf("cursor:oauth:%d:%x", account.ID, sha256.Sum256([]byte(rejected)))
	result := provider.flights.DoChan(flightKey, func() (any, error) {
		return provider.loadOAuthSessionToken(context.WithoutCancel(ctx), account, rejected)
	})
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case completed := <-result:
		if completed.Err != nil {
			return "", completed.Err
		}
		return completed.Val.(string), nil
	}
}

func (provider *CursorSessionProvider) loadOAuthSessionToken(ctx context.Context, account *Account, rejected string) (string, error) {
	if provider.refreshAPI == nil || provider.refresher == nil {
		return "", fmt.Errorf("cursor: OAuth refresh unavailable")
	}
	refreshCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	// Always enter the shared API: it rereads durable credentials and checks
	// eligibility under its lease, so a stale scheduler snapshot cannot restore
	// a rejected token after a rotation, reauthorization, or Redis flush.
	executor := cursorRejectedTokenExecutor{CursorTokenRefresher: provider.refresher, rejected: rejected}
	for {
		result, err := provider.refreshAPI.RefreshIfNeeded(withOAuthRefreshRequestPath(refreshCtx), account, executor, cursorRefreshWindow)
		if err != nil {
			return "", fmt.Errorf("cursor: OAuth session unavailable")
		}
		if !result.LockHeld {
			if result.Account == nil {
				return "", fmt.Errorf("cursor: OAuth account unavailable")
			}
			token := result.Account.GetCredential("access_token")
			if token == "" || token == rejected {
				return "", fmt.Errorf("cursor: OAuth session rejected")
			}
			// The cache is only derived state. The database remains authoritative
			// after restart and when a different instance refreshed this account.
			if expiry := result.Account.GetCredentialAsTime("expires_at"); expiry != nil && provider.cache != nil {
				key := cursorSessionKey(result.Account)
				cached, cacheErr := provider.cache.Get(refreshCtx, key)
				if cacheErr == nil && cached.Token == token && time.Now().Before(cached.ExpiresAt) {
					return cached.Token, nil
				}
				_ = provider.cache.Set(refreshCtx, key, cursor.Session{Token: token, ExpiresAt: expiry.Add(-cursorRefreshWindow)})
			}
			return token, nil
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-refreshCtx.Done():
			timer.Stop()
			return "", fmt.Errorf("cursor: OAuth refresh wait timed out")
		case <-timer.C:
		}
	}
}
