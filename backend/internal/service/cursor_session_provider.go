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
	cache    CursorSessionCache
	flights  singleflight.Group
	exchange func(context.Context, string) (cursor.Session, error)
}

func NewCursorSessionProvider(cache CursorSessionCache) *CursorSessionProvider {
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &CursorSessionProvider{cache: cache, exchange: func(ctx context.Context, key string) (cursor.Session, error) {
		return cursor.ExchangeKey(ctx, client, cursor.APIBaseURL, key)
	}}
}

func cursorSessionKey(account *Account) string {
	digest := sha256.Sum256([]byte(account.GetCredential("api_key")))
	return fmt.Sprintf("cursor:session:%d:%x", account.ID, digest)
}

// rejectedToken is empty for ordinary reads. A rejected token is replaced once;
// another caller's already-refreshed token can be reused without another exchange.
func (provider *CursorSessionProvider) GetSessionToken(ctx context.Context, account *Account, rejectedToken string) (string, error) {
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
