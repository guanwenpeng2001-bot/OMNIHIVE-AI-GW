//go:build unit

package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/cursor"
	"github.com/stretchr/testify/require"
)

type cursorMemoryCache struct {
	mutex    sync.Mutex
	sessions map[string]cursor.Session
	locked   map[string]bool
}

func newCursorMemoryCache() *cursorMemoryCache {
	return &cursorMemoryCache{sessions: map[string]cursor.Session{}, locked: map[string]bool{}}
}
func (cache *cursorMemoryCache) Delete(_ context.Context, key string) error {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	delete(cache.sessions, key)
	return nil
}

func (cache *cursorMemoryCache) Get(_ context.Context, key string) (cursor.Session, error) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	return cache.sessions[key], nil
}
func (cache *cursorMemoryCache) Set(_ context.Context, key string, session cursor.Session) error {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	cache.sessions[key] = session
	return nil
}
func (cache *cursorMemoryCache) Lock(_ context.Context, key string) (func(), bool, error) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	if cache.locked[key] {
		return func() {}, false, nil
	}
	cache.locked[key] = true
	return func() { cache.mutex.Lock(); delete(cache.locked, key); cache.mutex.Unlock() }, true, nil
}

func TestCursorSessionReuseRefreshAndCredentialChange(t *testing.T) {
	cache := newCursorMemoryCache()
	provider := NewCursorSessionProvider(cache)
	var exchanges atomic.Int32
	provider.exchange = func(context.Context, string) (cursor.Session, error) {
		count := exchanges.Add(1)
		return cursor.Session{Token: fmt.Sprintf("session-%d", count), ExpiresAt: time.Now().Add(time.Hour)}, nil
	}
	account := &Account{ID: 21, Platform: PlatformCursor, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "crsr_secret"}}
	var workers sync.WaitGroup
	for range 10 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			token, err := provider.GetSessionToken(context.Background(), account, "")
			require.NoError(t, err)
			require.Equal(t, "session-1", token)
		}()
	}
	workers.Wait()
	require.Equal(t, int32(1), exchanges.Load())
	token, err := provider.GetSessionToken(context.Background(), account, "session-1")
	require.NoError(t, err)
	require.Equal(t, "session-2", token)
	token, err = provider.GetSessionToken(context.Background(), account, "session-1")
	require.NoError(t, err)
	require.Equal(t, "session-2", token)
	account.Credentials["api_key"] = "crsr_replacement"
	_, err = provider.GetSessionToken(context.Background(), account, "")
	require.NoError(t, err)
	require.Equal(t, int32(3), exchanges.Load())
	require.False(t, strings.Contains(cursorSessionKey(account), "crsr_"))
}

func TestCursorSessionDistributedLockAcrossProviders(t *testing.T) {
	cache := newCursorMemoryCache()
	var exchanges atomic.Int32
	exchange := func(context.Context, string) (cursor.Session, error) {
		exchanges.Add(1)
		return cursor.Session{Token: "shared", ExpiresAt: time.Now().Add(time.Hour)}, nil
	}
	first, second := NewCursorSessionProvider(cache), NewCursorSessionProvider(cache)
	first.exchange = exchange
	second.exchange = exchange
	account := &Account{ID: 1, Platform: PlatformCursor, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "key"}}
	var workers sync.WaitGroup
	for _, provider := range []*CursorSessionProvider{first, second} {
		workers.Add(1)
		go func() {
			defer workers.Done()
			_, err := provider.GetSessionToken(context.Background(), account, "")
			require.NoError(t, err)
		}()
	}
	workers.Wait()
	require.Equal(t, int32(1), exchanges.Load())
}
