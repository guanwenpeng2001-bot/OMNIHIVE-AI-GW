//go:build unit || integration

package securityintegration

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/repository"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil/testresources"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestEmailAtomicMiniRedis(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	testEmailAtomic(t, client)
	cache := repository.NewEmailCache(client)
	svc := service.NewEmailService(nil, cache)
	mr.Close()
	require.ErrorIs(t, svc.VerifyCode(t.Context(), "closed@example.com", "123456"), service.ErrInvalidVerifyCode)
	require.ErrorIs(t, svc.ConsumePasswordResetToken(t.Context(), "closed@example.com", "token"), service.ErrInvalidResetToken)
}

func TestEmailAtomicRealRedis(t *testing.T) {
	address := os.Getenv("SECURITY_REDIS_TEST_ADDR")
	if address == "" {
		t.Skip("set SECURITY_REDIS_TEST_ADDR to an isolated Redis fixture")
	}
	require.NoError(t, testresources.ValidateRedis(address))
	client := redis.NewClient(&redis.Options{Addr: address, MaxRetries: -1, DialTimeout: 2 * time.Second})
	t.Cleanup(func() { _ = client.Close() })
	require.NoError(t, client.Ping(t.Context()).Err())
	testEmailAtomic(t, client)
}

func testEmailAtomic(t *testing.T, client *redis.Client) {
	cache := repository.NewEmailCache(client)
	svc := service.NewEmailService(nil, cache)
	email := "security-audit-" + uuid.NewString() + "@example.com"
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = cache.DeleteVerificationCode(ctx, email)
		_ = cache.DeleteNotifyVerifyCode(ctx, email)
		_ = cache.DeletePasswordResetToken(ctx, email)
	})
	for _, notify := range []bool{false, true} {
		t.Run(fmt.Sprintf("notify=%v", notify), func(t *testing.T) {
			set, get, verify := cache.SetVerificationCode, cache.GetVerificationCode, svc.VerifyCode
			if notify {
				set, get = cache.SetNotifyVerifyCode, cache.GetNotifyVerifyCode
				verify = func(ctx context.Context, email, code string) error {
					return cache.VerifyNotifyCode(ctx, email, code, 5)
				}
			}
			seed := func() {
				require.NoError(t, set(t.Context(), email, &service.VerificationCodeData{Code: "123456", CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Minute)}, time.Minute))
			}
			seed()
			var invalid, locked atomic.Int32
			var wg sync.WaitGroup
			for range 64 {
				wg.Go(func() {
					err := verify(t.Context(), email, "wrong")
					if errors.Is(err, service.ErrInvalidVerifyCode) {
						invalid.Add(1)
					}
					if errors.Is(err, service.ErrVerifyCodeMaxAttempts) {
						locked.Add(1)
					}
				})
			}
			wg.Wait()
			require.EqualValues(t, 4, invalid.Load())
			require.EqualValues(t, 60, locked.Load())
			stored, err := get(t.Context(), email)
			require.NoError(t, err)
			require.Equal(t, 5, stored.Attempts)
			require.ErrorIs(t, verify(t.Context(), email, "123456"), service.ErrVerifyCodeMaxAttempts)
			seed()
			var successes atomic.Int32
			for range 64 {
				wg.Go(func() {
					if verify(t.Context(), email, "123456") == nil {
						successes.Add(1)
					}
				})
			}
			wg.Wait()
			require.EqualValues(t, 1, successes.Load())
			_, err = get(t.Context(), email)
			require.ErrorIs(t, err, redis.Nil)
		})
	}
	for _, hashed := range []bool{false, true} {
		t.Run(fmt.Sprintf("reset-hashed=%v", hashed), func(t *testing.T) {
			token := "synthetic-legacy-reset-token"
			storedToken := token
			if hashed {
				storedToken = fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(token)))
			}
			require.NoError(t, cache.SetPasswordResetToken(t.Context(), email, &service.PasswordResetTokenData{Token: storedToken, CreatedAt: time.Now()}, time.Minute))
			require.NoError(t, svc.VerifyPasswordResetToken(t.Context(), email, token))
			require.ErrorIs(t, svc.ConsumePasswordResetToken(t.Context(), email, "bad"), service.ErrInvalidResetToken)
			if hashed {
				require.ErrorIs(t, svc.ConsumePasswordResetToken(t.Context(), email, storedToken), service.ErrInvalidResetToken)
			}
			var success atomic.Int32
			var wg sync.WaitGroup
			for range 64 {
				wg.Go(func() {
					if svc.ConsumePasswordResetToken(t.Context(), email, token) == nil {
						success.Add(1)
					}
				})
			}
			wg.Wait()
			require.EqualValues(t, 1, success.Load())
		})
	}
	t.Run("reset-generation-and-expiry", func(t *testing.T) {
		require.NoError(t, cache.SetPasswordResetToken(t.Context(), email, &service.PasswordResetTokenData{Token: "replacement"}, time.Minute))
		consumed, err := cache.ConsumePasswordResetToken(t.Context(), email, "older")
		require.NoError(t, err)
		require.False(t, consumed)
		require.NoError(t, svc.VerifyPasswordResetToken(t.Context(), email, "replacement"))
		expired, err := client.PExpire(t.Context(), "password_reset:"+email, 0).Result()
		require.NoError(t, err)
		require.True(t, expired)
		require.ErrorIs(t, svc.VerifyPasswordResetToken(t.Context(), email, "replacement"), service.ErrInvalidResetToken)
	})
}
