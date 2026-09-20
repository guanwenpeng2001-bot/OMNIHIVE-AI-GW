package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDingTalkClient_ExchangeCodeForUserToken_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "POST", r.Method)
		require.Equal(t, "/v1.0/oauth2/userAccessToken", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"accessToken":"USER_TOKEN_X","expireIn":7200,"refreshToken":"R","corpId":"dingABC"}`))
	}))
	defer server.Close()

	cli := &DingTalkClient{
		cfg: dingTalkClientConfig{
			ClientID: "k", ClientSecret: "s",
			TokenURL: server.URL + "/v1.0/oauth2/userAccessToken",
		},
		httpClient: server.Client(),
	}
	resp, err := cli.ExchangeCodeForUserToken(context.Background(), "AUTH_CODE")
	require.NoError(t, err)
	require.Equal(t, "USER_TOKEN_X", resp.AccessToken)
	require.Equal(t, "dingABC", resp.CorpID)
}

func TestDingTalkClient_GetUnionIdByUserToken_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "USER_TOKEN_X", r.Header.Get("x-acs-dingtalk-access-token"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"nick":"张三","unionId":"UID_AAA","openId":"OPEN","avatarUrl":"http://x"}`))
	}))
	defer server.Close()

	cli := &DingTalkClient{
		cfg:        dingTalkClientConfig{UserInfoURL: server.URL + "/v1.0/contact/users/me"},
		httpClient: server.Client(),
	}
	unionID, nick, err := cli.GetUnionIdByUserToken(context.Background(), "USER_TOKEN_X")
	require.NoError(t, err)
	require.Equal(t, "UID_AAA", unionID)
	require.Equal(t, "张三", nick)
}

func TestDingTalkClient_GetAppToken_Cached(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		_, _ = w.Write([]byte(`{"accessToken":"APP_TKN","expireIn":7200}`))
	}))
	defer server.Close()

	cli := &DingTalkClient{
		cfg:        dingTalkClientConfig{ClientID: "k", ClientSecret: "s", TokenURL: server.URL + "/gettoken"},
		httpClient: server.Client(),
	}
	t1, err := cli.GetAppToken(context.Background())
	require.NoError(t, err)
	t2, err := cli.GetAppToken(context.Background())
	require.NoError(t, err)
	require.Equal(t, t1, t2)
	require.Equal(t, 1, callCount, "second call should hit cache")
}

func TestDingTalkClient_GetUserIdByUnionId_60011(t *testing.T) {
	appTokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"accessToken":"APP_TKN","expireIn":7200}`))
	}))
	defer appTokenServer.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"errcode":60011,"errmsg":"not in directory"}`))
	}))
	defer server.Close()

	cli := &DingTalkClient{
		cfg:        dingTalkClientConfig{TokenURL: appTokenServer.URL + "/gettoken"},
		httpClient: server.Client(),
	}
	cli.appToken = "APP_TKN"
	cli.appTokenExp = time.Now().Add(time.Hour)
	cli.cfg.UserInfoURL = server.URL + "/v1.0/contact/users/byUnionId"

	_, err := cli.GetUserIdByUnionId(context.Background(), "UID_AAA")
	require.Error(t, err)
	apiErr, ok := err.(*DingTalkAPIError)
	require.True(t, ok)
	require.Equal(t, "60011", apiErr.Code)
}

// TestDingTalkClient_GetDeptInfo_Success 验证 GetDeptInfo 正常情况返回部门信息。
func TestDingTalkClient_GetDeptInfo_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok","result":{"dept_id":42,"name":"AI数据","parent_id":1}}`))
	}))
	defer server.Close()

	cli := &DingTalkClient{
		cfg: dingTalkClientConfig{
			UserInfoURL: server.URL + "/stub", // 不含 /contact/users/me，走 test stub 路径
		},
		httpClient: server.Client(),
	}
	cli.appToken = "APP_TKN"
	cli.appTokenExp = time.Now().Add(time.Hour)

	info, err := cli.GetDeptInfo(context.Background(), 42)
	require.NoError(t, err)
	require.Equal(t, int64(42), info.DeptID)
	require.Equal(t, "AI数据", info.Name)
	require.Equal(t, int64(1), info.ParentID)
}

// TestDingTalkClient_GetDeptInfo_ErrCode60003 验证 errcode=60003（部门不存在）时返回错误。
func TestDingTalkClient_GetDeptInfo_ErrCode60003(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"errcode":60003,"errmsg":"dept not found"}`))
	}))
	defer server.Close()

	cli := &DingTalkClient{
		cfg:        dingTalkClientConfig{UserInfoURL: server.URL + "/stub"},
		httpClient: server.Client(),
	}
	cli.appToken = "APP_TKN"
	cli.appTokenExp = time.Now().Add(time.Hour)

	_, err := cli.GetDeptInfo(context.Background(), 999)
	require.Error(t, err)
	apiErr, ok := err.(*DingTalkAPIError)
	require.True(t, ok)
	require.Equal(t, "60003", apiErr.Code)
}

func TestDingTalkSharedTokenRefreshExpiryAndIdentity(t *testing.T) {
	cache := newDingTalkTokenCacheFake()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		_, _ = writer.Write([]byte(`{"accessToken":"shared-token","expireIn":7200}`))
	}))
	defer server.Close()
	config := dingTalkClientConfig{ClientID: "app", ClientSecret: "secret", TokenURL: server.URL}
	first := &DingTalkClient{cfg: config, httpClient: server.Client(), tokenCache: cache}
	second := &DingTalkClient{cfg: config, httpClient: server.Client(), tokenCache: cache}
	var workers sync.WaitGroup
	for _, client := range []*DingTalkClient{first, second} {
		workers.Add(1)
		go func(client *DingTalkClient) {
			defer workers.Done()
			token, err := client.GetAppToken(context.Background())
			assert.NoError(t, err)
			assert.Equal(t, "shared-token", token)
		}(client)
	}
	workers.Wait()
	require.EqualValues(t, 1, calls.Load())
	require.Equal(t, 7000*time.Second, cache.TTL(first.appTokenCacheKey()))
	require.False(t, cache.Exists(first.appTokenCacheKey()+":refresh"))
	cache.FastForward(7000 * time.Second)
	_, err := second.GetAppToken(context.Background())
	require.NoError(t, err)
	require.EqualValues(t, 2, calls.Load())
	second.cfg.ClientSecret = "rotated"
	require.NotEqual(t, first.appTokenCacheKey(), second.appTokenCacheKey())
	_, err = second.GetAppToken(context.Background())
	require.NoError(t, err)
	require.EqualValues(t, 3, calls.Load())
}

func TestDingTalkSharedTokenLockWaitCancellation(t *testing.T) {
	cache := newDingTalkTokenCacheFake()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"accessToken":"unexpected","expireIn":7200}`))
	}))
	defer server.Close()
	client := &DingTalkClient{cfg: dingTalkClientConfig{TokenURL: server.URL}, httpClient: server.Client(), tokenCache: cache}
	require.NoError(t, cache.Set(client.appTokenCacheKey()+":refresh", "other-owner"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cache.afterAcquire = cancel // Cancel after observing the other owner's lock.
	_, err := client.GetAppToken(ctx)
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, calls.Load())
	owner, err := cache.Get(context.Background(), client.appTokenCacheKey()+":refresh")
	require.NoError(t, err)
	require.Equal(t, "other-owner", owner)
}

func TestDingTalkSharedTokenRedisCancellationDoesNotFetch(t *testing.T) {
	for _, command := range []string{"get", "set"} {
		for _, scenario := range []struct {
			name         string
			err          error
			cancelParent bool
		}{
			{"canceled_error", context.Canceled, false},
			{"deadline_error", context.DeadlineExceeded, false},
			{"parent_canceled", context.Canceled, true},
		} {
			t.Run(command+"/"+scenario.name, func(t *testing.T) {
				cache := newDingTalkTokenCacheFake()
				var calls atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					_, _ = w.Write([]byte(`{"accessToken":"unexpected","expireIn":7200}`))
				}))
				defer server.Close()
				client := &DingTalkClient{cfg: dingTalkClientConfig{TokenURL: server.URL}, httpClient: server.Client(), tokenCache: cache}
				require.NoError(t, cache.Set(client.appTokenCacheKey()+":refresh", "other-owner"))
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				injected := fmt.Errorf("redis operation: %w", scenario.err)
				var intercepted bool
				cache.before = func(ctx context.Context, operation string) error {
					if operation != command {
						return nil
					}
					intercepted = true
					if scenario.cancelParent {
						cancel()
						return ctx.Err()
					}
					return injected
				}
				// Error-only cases keep the caller live so HTTP cannot mask a fallback;
				// parent_canceled reproduces cancellation inside the Redis operation.
				token, err := client.GetAppToken(ctx)
				require.True(t, intercepted)
				assert.ErrorIs(t, err, scenario.err)
				if !scenario.cancelParent {
					assert.Same(t, injected, err)
				}
				assert.Empty(t, token)
				assert.Zero(t, calls.Load(), "Redis cancellation must not reach upstream")
				cache.before = nil
				owner, err := cache.Get(context.Background(), client.appTokenCacheKey()+":refresh")
				require.NoError(t, err)
				require.Equal(t, "other-owner", owner)
			})
		}
	}
}

func TestDingTalkRedisFailureFallsBackWithoutLocalToken(t *testing.T) {
	cache := newDingTalkTokenCacheFake()
	cache.err = fmt.Errorf("cache unavailable")
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		_, _ = writer.Write([]byte(`{"accessToken":"fresh","expireIn":7200}`))
	}))
	defer server.Close()
	client := &DingTalkClient{cfg: dingTalkClientConfig{TokenURL: server.URL}, httpClient: server.Client(), tokenCache: cache, appToken: "stale", appTokenExp: time.Now().Add(time.Hour)}
	for attempt := 0; attempt < 2; attempt++ {
		token, err := client.GetAppToken(context.Background())
		require.NoError(t, err)
		require.Equal(t, "fresh", token)
	}
	require.EqualValues(t, 2, calls.Load())
}

func TestDingTalkRefreshCannotPublishOrUnlockNewOwner(t *testing.T) {
	cache := newDingTalkTokenCacheFake()
	client := &DingTalkClient{tokenCache: cache}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		cache.FastForward(31 * time.Second)
		assert.NoError(t, cache.Set(client.appTokenCacheKey()+":refresh", "new-owner"))
		assert.NoError(t, cache.Set(client.appTokenCacheKey(), "newer-token"))
		_, _ = writer.Write([]byte(`{"accessToken":"older-token","expireIn":7200}`))
	}))
	defer server.Close()
	client.cfg.TokenURL, client.httpClient = server.URL, server.Client()
	returned, err := client.GetAppToken(context.Background())
	require.NoError(t, err)
	require.Equal(t, "newer-token", returned)
	token, err := cache.Get(context.Background(), client.appTokenCacheKey())
	require.NoError(t, err)
	require.Equal(t, "newer-token", token)
	owner, err := cache.Get(context.Background(), client.appTokenCacheKey()+":refresh")
	require.NoError(t, err)
	require.Equal(t, "new-owner", owner)
}

// dingTalkTokenCacheFake models only the shared token and refresh ownership contract.
type dingTalkTokenCacheFake struct {
	mu           sync.Mutex
	values       map[string]string
	ttls         map[string]time.Duration
	err          error
	before       func(context.Context, string) error
	afterAcquire func()
}

func newDingTalkTokenCacheFake() *dingTalkTokenCacheFake {
	return &dingTalkTokenCacheFake{values: make(map[string]string), ttls: make(map[string]time.Duration)}
}

func (c *dingTalkTokenCacheFake) Get(ctx context.Context, key string) (string, error) {
	if c.before != nil {
		if err := c.before(ctx, "get"); err != nil {
			return "", err
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.values[key], c.err
}

func (c *dingTalkTokenCacheFake) AcquireRefresh(ctx context.Context, key, owner string, ttl time.Duration) (bool, error) {
	if c.before != nil {
		if err := c.before(ctx, "set"); err != nil {
			return false, err
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.afterAcquire != nil {
		defer c.afterAcquire()
	}
	if c.err != nil {
		return false, c.err
	}
	if _, exists := c.values[key]; exists {
		return false, nil
	}
	c.values[key], c.ttls[key] = owner, ttl
	return true, nil
}

func (c *dingTalkTokenCacheFake) ReleaseRefresh(_ context.Context, key, owner string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.values[key] == owner {
		delete(c.values, key)
		delete(c.ttls, key)
	}
	return c.err
}

func (c *dingTalkTokenCacheFake) Publish(_ context.Context, lockKey, key, owner, token string, ttl time.Duration) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return false, c.err
	}
	if c.values[lockKey] != owner {
		return false, nil
	}
	c.values[key], c.ttls[key] = token, ttl
	return true, nil
}

func (c *dingTalkTokenCacheFake) Set(key, value string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.values[key] = value
	delete(c.ttls, key)
	return nil
}

func (c *dingTalkTokenCacheFake) TTL(key string) time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ttls[key]
}

func (c *dingTalkTokenCacheFake) Exists(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, exists := c.values[key]
	return exists
}

func (c *dingTalkTokenCacheFake) FastForward(elapsed time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, ttl := range c.ttls {
		if ttl <= elapsed {
			delete(c.ttls, key)
			delete(c.values, key)
		} else {
			c.ttls[key] -= elapsed
		}
	}
}
