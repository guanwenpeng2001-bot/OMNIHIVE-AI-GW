//go:build unit

package service

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/cursor"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type cursorOAuthMemoryRepo struct {
	AccountRepository
	mu      sync.Mutex
	account *Account
	writes  []map[string]any
}

func (repo *cursorOAuthMemoryRepo) GetByID(context.Context, int64) (*Account, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	return snapshotOAuthRefreshAccount(repo.account), nil
}
func (repo *cursorOAuthMemoryRepo) UpdateCredentials(_ context.Context, _ int64, credentials map[string]any) error {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	repo.account.Credentials = shallowCopyMap(credentials)
	repo.writes = append(repo.writes, shallowCopyMap(credentials))
	return nil
}
func (repo *cursorOAuthMemoryRepo) SetError(_ context.Context, _ int64, message string) error {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	repo.account.Status = StatusError
	repo.account.ErrorMessage = message
	return nil
}
func (repo *cursorOAuthMemoryRepo) SetTempUnschedulable(_ context.Context, _ int64, until time.Time, message string) error {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	repo.account.TempUnschedulableUntil = &until
	repo.account.TempUnschedulableReason = message
	return nil
}

func (repo *cursorOAuthMemoryRepo) UpdateCursorOAuthCredentialsIfUnchanged(_ context.Context, _ int64, expected, credentials map[string]any) (bool, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	if !reflect.DeepEqual(repo.account.Credentials, expected) {
		return false, nil
	}
	repo.account.Credentials = shallowCopyMap(credentials)
	repo.writes = append(repo.writes, shallowCopyMap(credentials))
	return true, nil
}
func (repo *cursorOAuthMemoryRepo) SetCursorOAuthRefreshFailureIfUnchanged(_ context.Context, _ int64, expected map[string]any, permanent bool, until time.Time) (bool, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	if !reflect.DeepEqual(repo.account.Credentials, expected) {
		return false, nil
	}
	if permanent {
		repo.account.Status = StatusError
		repo.account.ErrorMessage = "Cursor reauth required: token refresh rejected"
	} else {
		repo.account.TempUnschedulableUntil = &until
		repo.account.TempUnschedulableReason = "Cursor token refresh temporarily unavailable"
	}
	return true, nil
}
func (repo *cursorOAuthMemoryRepo) Update(_ context.Context, account *Account) error {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	repo.account = snapshotOAuthRefreshAccount(account)
	return nil
}

func (repo *cursorOAuthMemoryRepo) ClearTempUnschedulable(context.Context, int64) error {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	repo.account.TempUnschedulableUntil = nil
	repo.account.TempUnschedulableReason = ""
	return nil
}

func cursorAutoTestAccount() *Account {
	return &Account{ID: 71, Platform: PlatformCursor, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Credentials: map[string]any{
		"access_token": "synthetic-expiring-access", "refresh_token": "synthetic-refresh", "expires_at": strconv.FormatInt(time.Now().Add(time.Minute).Unix(), 10),
		"machine_id": strings.Repeat("a", 64), "mac_machine_id": strings.Repeat("b", 64),
	}}
}

func TestCursorAutoImportValidation(t *testing.T) {
	account := cursorAutoTestAccount()
	account.Concurrency = 8
	require.NoError(t, NormalizeCursorAccount(account))
	require.Equal(t, 1, account.Concurrency)
	for _, field := range []string{"access_token", "refresh_token", "machine_id", "mac_machine_id"} {
		t.Run("missing_"+field, func(t *testing.T) {
			account := cursorAutoTestAccount()
			delete(account.Credentials, field)
			require.ErrorContains(t, NormalizeCursorAccount(account), field)
		})
	}
	for _, value := range []string{"", strings.Repeat("g", 64), strings.Repeat("a", 63), strings.Repeat("a", 65)} {
		for _, field := range []string{"machine_id", "mac_machine_id"} {
			account := cursorAutoTestAccount()
			account.Credentials[field] = value
			require.Error(t, NormalizeCursorAccount(account))
		}
	}
	for _, field := range []string{"api_key", "base_url", "api_base_urls", "api_protocol", "cached_access_token"} {
		account := cursorAutoTestAccount()
		account.Credentials[field] = ""
		require.ErrorContains(t, NormalizeCursorAccount(account), field)
	}
}

func TestCursorAutoRefreshPersistenceConcurrencyRestartAndForce(t *testing.T) {
	for _, rotation := range []bool{false, true} {
		t.Run(fmt.Sprintf("rotation=%v", rotation), func(t *testing.T) {
			account := cursorAutoTestAccount()
			repo := &cursorOAuthMemoryRepo{account: account}
			api := NewOAuthRefreshAPI(repo, nil)
			provider := ProvideCursorSessionProvider(newCursorMemoryCache(), api)
			var calls atomic.Int32
			provider.refresher.refresh = func(context.Context, string) (cursor.OAuthTokens, error) {
				count := calls.Add(1)
				tokens := cursor.OAuthTokens{AccessToken: fmt.Sprintf("synthetic-new-%d", count), ExpiresAt: time.Now().Add(time.Hour)}
				if rotation {
					tokens.RefreshToken = "synthetic-rotated"
				}
				return tokens, nil
			}
			var wait sync.WaitGroup
			for range 20 {
				wait.Add(1)
				go func() {
					defer wait.Done()
					token, err := provider.GetSessionToken(context.Background(), account, "")
					require.NoError(t, err)
					require.Equal(t, "synthetic-new-1", token)
				}()
			}
			wait.Wait()
			require.Equal(t, int32(1), calls.Load())
			require.Len(t, repo.writes, 1)
			wantRefresh := "synthetic-refresh"
			if rotation {
				wantRefresh = "synthetic-rotated"
			}
			require.Equal(t, wantRefresh, repo.writes[0]["refresh_token"])
			require.Equal(t, "synthetic-new-1", repo.writes[0]["access_token"])
			require.Equal(t, strings.Repeat("a", 64), repo.writes[0]["machine_id"])
			// A new provider with empty Redis state reads durable credentials. Its
			// HTTP refresher is a tripwire proving no credentials need reimport.
			restarted := ProvideCursorSessionProvider(newCursorMemoryCache(), NewOAuthRefreshAPI(repo, nil))
			restarted.refresher.refresh = func(context.Context, string) (cursor.OAuthTokens, error) {
				t.Error("unexpected refresh after restart")
				return cursor.OAuthTokens{}, errors.New("unexpected")
			}
			token, err := restarted.GetSessionToken(context.Background(), account, "")
			require.NoError(t, err)
			require.Equal(t, "synthetic-new-1", token)
			for range 2 {
				token, err = provider.GetSessionToken(context.Background(), account, "synthetic-new-1")
				require.NoError(t, err)
				require.Equal(t, "synthetic-new-2", token)
			}
			require.Equal(t, int32(2), calls.Load())
		})
	}
}

func TestCursorAutoRefreshFailureStopsHotLoop(t *testing.T) {
	for _, permanent := range []bool{false, true} {
		t.Run(fmt.Sprintf("permanent=%v", permanent), func(t *testing.T) {
			account := cursorAutoTestAccount()
			repo := &cursorOAuthMemoryRepo{account: account}
			provider := ProvideCursorSessionProvider(newCursorMemoryCache(), NewOAuthRefreshAPI(repo, nil))
			var calls int
			provider.refresher.refresh = func(context.Context, string) (cursor.OAuthTokens, error) {
				calls++
				if permanent {
					return cursor.OAuthTokens{}, cursor.ErrReauthRequired
				}
				return cursor.OAuthTokens{}, errors.New("cursor: refresh transport failed")
			}
			for range 3 {
				_, err := provider.GetSessionToken(context.Background(), account, "")
				require.Error(t, err)
			}
			require.Equal(t, 1, calls)
			if permanent {
				require.Equal(t, StatusError, repo.account.Status)
				require.Contains(t, repo.account.ErrorMessage, "reauth")
			} else {
				require.Equal(t, StatusActive, repo.account.Status)
				require.True(t, repo.account.TempUnschedulableUntil.After(time.Now()))
				require.False(t, provider.refresher.NeedsRefresh(repo.account, time.Hour))
			}
		})
	}
}

func TestCursorRefresherOnlyScansOAuth(t *testing.T) {
	refresher := NewCursorTokenRefresher()
	for _, kind := range []string{AccountTypeAPIKey, AccountTypeUpstream, AccountTypeOAuth} {
		account := cursorAutoTestAccount()
		account.Type = kind
		require.Equal(t, kind == AccountTypeOAuth, refresher.CanRefresh(account))
	}
	service := NewTokenRefreshService(nil, nil, nil, nil, nil, nil, nil, nil, nil)
	require.Contains(t, service.eligiblePlatforms(), PlatformCursor)
}

func TestCursorBackgroundRefreshPersistsRotation(t *testing.T) {
	account := cursorAutoTestAccount()
	repo := &cursorOAuthMemoryRepo{account: account}
	service := NewTokenRefreshService(repo, nil, nil, nil, nil, nil, nil, nil, nil)
	service.SetRefreshAPI(NewOAuthRefreshAPI(repo, nil))
	refresher := NewCursorTokenRefresher()
	refresher.refresh = func(context.Context, string) (cursor.OAuthTokens, error) {
		return cursor.OAuthTokens{AccessToken: "synthetic-background", RefreshToken: "synthetic-background-rotation", ExpiresAt: time.Now().Add(time.Hour)}, nil
	}
	require.NoError(t, service.refreshWithRetry(context.Background(), account, refresher, refresher, cursorRefreshWindow))
	require.Len(t, repo.writes, 1)
	require.Equal(t, "synthetic-background", repo.writes[0]["access_token"])
	require.Equal(t, "synthetic-background-rotation", repo.writes[0]["refresh_token"])
}

func TestCursorAutoSharesNativeDriverPickerProbeAnd401Retry(t *testing.T) {
	account := cursorAutoTestAccount()
	account.Credentials["expires_at"] = strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)
	repo := &cursorOAuthMemoryRepo{account: account}
	provider := ProvideCursorSessionProvider(newCursorMemoryCache(), NewOAuthRefreshAPI(repo, nil))
	refreshes := 0
	provider.refresher.refresh = func(context.Context, string) (cursor.OAuthTokens, error) {
		refreshes++
		return cursor.OAuthTokens{AccessToken: "synthetic-refreshed", ExpiresAt: time.Now().Add(time.Hour)}, nil
	}
	gateway := NewCursorGatewayService(provider, nil)
	runs := 0
	driver := cursorProbeDriver{}
	driver.run = func(_ context.Context, credentials cursor.Credentials, messages []cursor.ChatMessage, model string, emit func(cursor.StreamEvent) error) (cursor.TokenUsage, error) {
		runs++
		require.Equal(t, "live-run", model)
		require.NotEmpty(t, messages)
		require.Equal(t, strings.Repeat("a", 64), credentials.MachineID)
		if credentials.AccessToken != "synthetic-refreshed" {
			return cursor.TokenUsage{}, &cursor.HTTPError{Status: 401}
		}
		return cursor.TokenUsage{InputTokens: 1, OutputTokens: 1}, emit(cursor.StreamEvent{Type: "text", Text: "OK"})
	}
	gateway.native = driver
	recorder := httptest.NewRecorder()
	requestContext, _ := gin.CreateTestContext(recorder)
	result, err := gateway.ForwardAsChatCompletions(context.Background(), requestContext, account, []byte(`{"model":"live","messages":[{"role":"user","content":"hi"}]}`))
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Contains(t, recorder.Body.String(), "OK")
	require.Equal(t, 2, runs)
	require.Equal(t, 1, refreshes)
	models, err := gateway.AvailableModels(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, []string{"live"}, models)
	probeRecorder := httptest.NewRecorder()
	probeContext, _ := gin.CreateTestContext(probeRecorder)
	probeContext.Request = httptest.NewRequest("POST", "/test", nil)
	require.NoError(t, (&AccountTestService{cursorGatewayService: gateway}).testCursorAccountConnection(probeContext, account))
	require.Contains(t, probeRecorder.Body.String(), `"success":true`)
	require.Equal(t, 3, runs)
	require.Equal(t, 1, refreshes)
}

type cursorOAuthLeaseCache struct {
	GeminiTokenCache
	mu   sync.Mutex
	held bool
}

func (cache *cursorOAuthLeaseCache) AcquireRefreshLock(context.Context, string, time.Duration) (bool, error) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.held {
		return false, nil
	}
	cache.held = true
	return true, nil
}
func (cache *cursorOAuthLeaseCache) ReleaseRefreshLock(context.Context, string) error {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.held = false
	return nil
}

func TestCursorAutoRefreshLeaseAcrossProviders(t *testing.T) {
	account := cursorAutoTestAccount()
	repo := &cursorOAuthMemoryRepo{account: account}
	lease := &cursorOAuthLeaseCache{}
	first := ProvideCursorSessionProvider(newCursorMemoryCache(), NewOAuthRefreshAPI(repo, lease))
	second := ProvideCursorSessionProvider(newCursorMemoryCache(), NewOAuthRefreshAPI(repo, lease))
	var calls atomic.Int32
	refresh := func(context.Context, string) (cursor.OAuthTokens, error) {
		calls.Add(1)
		return cursor.OAuthTokens{AccessToken: "synthetic-shared", ExpiresAt: time.Now().Add(time.Hour)}, nil
	}
	first.refresher.refresh, second.refresher.refresh = refresh, refresh
	var wait sync.WaitGroup
	for index := 0; index < 20; index++ {
		provider := first
		if index%2 == 0 {
			provider = second
		}
		wait.Add(1)
		go func() {
			defer wait.Done()
			token, err := provider.GetSessionToken(context.Background(), account, "")
			require.NoError(t, err)
			require.Equal(t, "synthetic-shared", token)
		}()
	}
	wait.Wait()
	require.Equal(t, int32(1), calls.Load())
	require.Len(t, repo.writes, 1)
}

func TestCursorAutoImportRecoversRefreshFailureAndInvalidatesSession(t *testing.T) {
	account := cursorAutoTestAccount()
	account.Status = StatusError
	account.ErrorMessage = "Cursor reauth required: token refresh rejected"
	until := time.Now().Add(time.Hour)
	account.TempUnschedulableUntil = &until
	cache := newCursorMemoryCache()
	oldKey := cursorSessionKey(account)
	require.NoError(t, cache.Set(context.Background(), oldKey, cursor.Session{Token: "synthetic-old-cached", ExpiresAt: time.Now().Add(time.Hour)}))
	repo := &updateAccountCredsRepoStub{account: account}
	service := &adminServiceImpl{accountRepo: repo, cursorSessions: cache}
	credentials := shallowCopyMap(account.Credentials)
	credentials["access_token"] = "synthetic-imported-access"
	credentials["refresh_token"] = "synthetic-imported-refresh"
	updated, err := service.UpdateAccount(context.Background(), account.ID, &UpdateAccountInput{Credentials: credentials})
	require.NoError(t, err)
	require.Equal(t, StatusActive, updated.Status)
	require.Empty(t, updated.ErrorMessage)
	require.Nil(t, updated.TempUnschedulableUntil)
	cached, err := cache.Get(context.Background(), oldKey)
	require.NoError(t, err)
	require.Empty(t, cached.Token)
	require.NotEqual(t, oldKey, cursorSessionKey(updated))
	credentials = shallowCopyMap(updated.Credentials)
	credentials["base_url"] = "https://relay.example"
	_, err = service.UpdateAccount(context.Background(), account.ID, &UpdateAccountInput{Credentials: credentials})
	require.ErrorContains(t, err, "base_url")
	require.Equal(t, 1, repo.updateCalls)
}
