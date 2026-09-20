package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
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
	cache := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: cache.Addr()})
	defer redisClient.Close()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		_, _ = writer.Write([]byte(`{"accessToken":"shared-token","expireIn":7200}`))
	}))
	defer server.Close()
	config := dingTalkClientConfig{ClientID: "app", ClientSecret: "secret", TokenURL: server.URL}
	first := &DingTalkClient{cfg: config, httpClient: server.Client(), redisClient: redisClient}
	second := &DingTalkClient{cfg: config, httpClient: server.Client(), redisClient: redisClient}
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
	cache := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: cache.Addr()})
	defer redisClient.Close()
	client := &DingTalkClient{redisClient: redisClient}
	require.NoError(t, cache.Set(client.appTokenCacheKey()+":refresh", "other-owner"))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := client.GetAppToken(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	owner, err := cache.Get(client.appTokenCacheKey() + ":refresh")
	require.NoError(t, err)
	require.Equal(t, "other-owner", owner)
}

func TestDingTalkRedisFailureFallsBackWithoutLocalToken(t *testing.T) {
	cache := miniredis.RunT(t)
	cache.SetError("ERR unavailable")
	redisClient := redis.NewClient(&redis.Options{Addr: cache.Addr(), MaxRetries: -1})
	defer redisClient.Close()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		_, _ = writer.Write([]byte(`{"accessToken":"fresh","expireIn":7200}`))
	}))
	defer server.Close()
	client := &DingTalkClient{cfg: dingTalkClientConfig{TokenURL: server.URL}, httpClient: server.Client(), redisClient: redisClient, appToken: "stale", appTokenExp: time.Now().Add(time.Hour)}
	for attempt := 0; attempt < 2; attempt++ {
		token, err := client.GetAppToken(context.Background())
		require.NoError(t, err)
		require.Equal(t, "fresh", token)
	}
	require.EqualValues(t, 2, calls.Load())
}

func TestDingTalkRefreshCannotPublishOrUnlockNewOwner(t *testing.T) {
	cache := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: cache.Addr()})
	defer redisClient.Close()
	client := &DingTalkClient{redisClient: redisClient}
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
	token, err := cache.Get(client.appTokenCacheKey())
	require.NoError(t, err)
	require.Equal(t, "newer-token", token)
	owner, err := cache.Get(client.appTokenCacheKey() + ":refresh")
	require.NoError(t, err)
	require.Equal(t, "new-owner", owner)
}
