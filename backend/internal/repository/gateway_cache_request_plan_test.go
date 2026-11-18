//go:build unit

package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"
)

func TestAffinityOpenAIRequestPlanRealRedisCAS(t *testing.T) {
	address := os.Getenv("AFFINITY_REDIS_TEST_ADDR")
	if address == "" {
		t.Skip("set AFFINITY_REDIS_TEST_ADDR to existing isolated Redis")
	}
	rdb := redis.NewClient(&redis.Options{Addr: address, Password: os.Getenv("AFFINITY_REDIS_TEST_PASSWORD"), MaxRetries: -1})
	t.Cleanup(func() { _ = rdb.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	require.NoError(t, rdb.Ping(ctx).Err())
	cache := NewGatewayCache(rdb)
	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.SessionHashReadOldFallback = true
	cfg.Gateway.OpenAIWS.SessionHashDualWriteOld = true
	svc := service.NewOpenAIGatewayService(nil, nil, nil, nil, nil, nil, cache, cfg, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	seed := "t7-" + uuid.NewString()
	hash := "v2:binding:" + seed
	group := int64(777)
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil).WithContext(ctx)
	c.Request.Header.Set("session_id", seed)
	legacyCurrent := svc.GenerateSessionHash(c, nil)
	sum := sha256.Sum256([]byte(seed))
	legacySHA := hex.EncodeToString(sum[:])
	realKey := buildSessionKey(group, hash)
	wrongKey := buildSessionKey(group, "openai:"+hash)
	oldCurrentKey := buildSessionKey(group, "openai:"+legacyCurrent)
	oldSHAKey := buildSessionKey(group, "openai:"+legacySHA)
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second*3)
		defer cancel()
		require.NoError(t, rdb.Del(cleanup, realKey, wrongKey, oldCurrentKey, oldSHAKey).Err())
	})
	left, right := service.WithAffinityBindingState(c.Request.Context()), service.WithAffinityBindingState(c.Request.Context())
	_, err := cache.GetSessionAccountID(left, group, hash)
	require.ErrorIs(t, err, service.ErrStickySessionNotFound)
	_, err = cache.GetSessionAccountID(right, group, hash)
	require.ErrorIs(t, err, service.ErrStickySessionNotFound)
	// Even a direct pre-admission wrapper call cannot create a v2 key.
	require.NoError(t, svc.BindStickySession(left, &group, hash, 11))
	require.Zero(t, rdb.Exists(ctx, realKey, wrongKey).Val())
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	start := make(chan struct{})
	for index, request := range []context.Context{left, right} {
		wg.Add(1)
		go func(request context.Context, id int64) {
			defer wg.Done()
			<-start
			errs <- svc.BindStickySession(service.WithAffinityAdmission(request), &group, hash, id)
		}(request, int64(index+11))
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	raw, err := rdb.Get(ctx, realKey).Result()
	require.NoError(t, err)
	winner, err := cache.GetSessionAccountID(service.WithAffinityBindingState(ctx), group, hash)
	require.NoError(t, err)
	require.Contains(t, []int64{11, 12}, winner)
	require.Contains(t, raw, fmt.Sprintf("v2:%d:", winner))
	require.Positive(t, rdb.TTL(ctx, realKey).Val())
	require.Zero(t, rdb.Exists(ctx, wrongKey, oldCurrentKey, oldSHAKey).Val(), "no prefixed v2 or legacy writes")
	require.NoError(t, svc.BindStickySession(service.WithAffinityAdmission(left), &group, hash, 99))
	require.Equal(t, raw, rdb.Get(ctx, realKey).Val(), "old attempt cannot replace winning binding")
	require.NoError(t, svc.BindStickySession(c.Request.Context(), &group, legacyCurrent, 55))
	require.Equal(t, "55", rdb.Get(ctx, oldCurrentKey).Val())
	require.Equal(t, "55", rdb.Get(ctx, oldSHAKey).Val(), "legacy dual write unchanged")
	t.Logf("key=%s value=%s ttl=%s; prefixed-v2 absent; legacy only populated by explicit legacy call", realKey, raw, rdb.TTL(ctx, realKey).Val())
}
