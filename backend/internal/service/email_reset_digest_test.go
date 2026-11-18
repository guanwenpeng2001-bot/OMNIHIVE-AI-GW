//go:build unit

package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type resetDigestCaptureCache struct {
	EmailCache
	stored *PasswordResetTokenData
}

func (c *resetDigestCaptureCache) SetPasswordResetToken(_ context.Context, _ string, data *PasswordResetTokenData, ttl time.Duration) error {
	copy := *data
	c.stored = &copy
	return nil
}

func TestSecurityBackportResetIssuanceStoresDigest(t *testing.T) {
	cache := &resetDigestCaptureCache{}
	svc := NewEmailService(newMockSettingRepo(), cache)
	// No SMTP configuration is needed to inspect the cache write before delivery.
	err := svc.SendPasswordResetEmail(t.Context(), "fixture@example.com", "test", "https://example.com/reset")
	require.ErrorIs(t, err, ErrEmailNotConfigured)
	require.NotNil(t, cache.stored)
	require.True(t, strings.HasPrefix(cache.stored.Token, "sha256:"))
	require.Len(t, cache.stored.Token, len("sha256:")+64)
	firstDigest := cache.stored.Token
	_ = svc.SendPasswordResetEmail(t.Context(), "fixture@example.com", "test", "https://example.com/reset")
	require.NotEqual(t, firstDigest, cache.stored.Token)
}
