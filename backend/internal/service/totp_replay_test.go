//go:build unit

package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/require"
)

type replayTotpUsers struct {
	UserRepository
	user *User
}

func (users *replayTotpUsers) GetByID(context.Context, int64) (*User, error) {
	return users.user, nil
}

type replayTotpCache struct {
	TotpCache
	mu       sync.Mutex
	consumed map[int64]bool
	err      error
}

type replayTotpEncryptor struct{ secret string }

func (encryptor replayTotpEncryptor) Encrypt(string) (string, error) { return "encrypted", nil }
func (encryptor replayTotpEncryptor) Decrypt(string) (string, error) {
	return encryptor.secret, nil
}

func (cache *replayTotpCache) GetVerifyAttempts(context.Context, int64) (int, error) { return 0, nil }
func (cache *replayTotpCache) ClearVerifyAttempts(context.Context, int64) error      { return nil }
func (cache *replayTotpCache) IncrementVerifyAttempts(context.Context, int64) (int, error) {
	return 1, nil
}

func (cache *replayTotpCache) ConsumeCodeTimeStep(_ context.Context, _ int64, timeStep int64, _ time.Duration) (bool, error) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.err != nil {
		return false, cache.err
	}
	if cache.consumed == nil {
		cache.consumed = make(map[int64]bool)
	}
	if cache.consumed[timeStep] {
		return false, nil
	}
	cache.consumed[timeStep] = true
	return true, nil
}

func TestTotpVerifyCodeConsumesTimeStep(t *testing.T) {
	const secret = "JBSWY3DPEHPK3PXP"
	now := time.Unix(1_700_000_000, 0)
	ciphertext := "encrypted"
	cache := &replayTotpCache{}
	users := &replayTotpUsers{user: &User{TotpEnabled: true, TotpSecretEncrypted: &ciphertext}}
	svc := NewTotpService(users, replayTotpEncryptor{secret: secret}, cache, nil, nil, nil)
	svc.now = func() time.Time { return now }
	code, err := totp.GenerateCodeCustom(secret, now, totp.ValidateOpts{Period: 30, Skew: 1, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
	require.NoError(t, err)

	require.NoError(t, svc.VerifyCode(context.Background(), 42, code))
	require.ErrorIs(t, svc.VerifyCode(context.Background(), 42, code), ErrTotpInvalidCode)

	now = now.Add(30 * time.Second)
	nextCode, err := totp.GenerateCodeCustom(secret, now, totp.ValidateOpts{Period: 30, Skew: 1, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
	require.NoError(t, err)
	require.NoError(t, svc.VerifyCode(context.Background(), 42, nextCode))
}

func TestTotpVerifyCodeConcurrentReplay(t *testing.T) {
	const secret = "JBSWY3DPEHPK3PXP"
	ciphertext := "encrypted"
	cache := &replayTotpCache{}
	users := &replayTotpUsers{user: &User{TotpEnabled: true, TotpSecretEncrypted: &ciphertext}}
	svc := NewTotpService(users, replayTotpEncryptor{secret: secret}, cache, nil, nil, nil)
	code, err := totp.GenerateCode(secret, time.Now())
	require.NoError(t, err)

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- svc.VerifyCode(context.Background(), 42, code)
		}()
	}
	wg.Wait()
	close(errs)
	var successes int
	for err := range errs {
		if err == nil {
			successes++
		} else {
			require.ErrorIs(t, err, ErrTotpInvalidCode)
		}
	}
	require.Equal(t, 1, successes)
}

func TestTotpVerifyCodeFailsClosedWhenConsumptionUnavailable(t *testing.T) {
	ciphertext := "encrypted"
	cache := &replayTotpCache{err: errors.New("redis unavailable")}
	users := &replayTotpUsers{user: &User{TotpEnabled: true, TotpSecretEncrypted: &ciphertext}}
	svc := NewTotpService(users, replayTotpEncryptor{secret: "JBSWY3DPEHPK3PXP"}, cache, nil, nil, nil)
	code, err := totp.GenerateCode("JBSWY3DPEHPK3PXP", time.Now())
	require.NoError(t, err)

	require.Error(t, svc.VerifyCode(context.Background(), 42, code))
}
