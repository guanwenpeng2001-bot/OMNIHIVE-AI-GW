package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type automaticPrivacyAdmin struct {
	AdminService
	fresh                      *Account
	err                        error
	reads, openAI, antigravity int
}

func (admin *automaticPrivacyAdmin) GetAccount(context.Context, int64) (*Account, error) {
	admin.reads++
	return admin.fresh, admin.err
}
func (admin *automaticPrivacyAdmin) EnsureOpenAIPrivacy(context.Context, *Account) string {
	admin.openAI++
	return ""
}
func (admin *automaticPrivacyAdmin) EnsureAntigravityPrivacy(context.Context, *Account) string {
	admin.antigravity++
	return AntigravityPrivacySet
}

func TestAutomaticPrivacyPreservesAntigravityResponseMode(t *testing.T) {
	account := &Account{ID: 42, Status: StatusActive, Platform: PlatformAntigravity, Type: AccountTypeOAuth, Extra: map[string]any{"existing": true}}
	fresh := *account
	admin := &automaticPrivacyAdmin{fresh: &fresh}
	EnsureAutomaticAccountPrivacy(context.Background(), admin, account)
	require.Equal(t, AntigravityPrivacySet, account.Extra["privacy_mode"])
	require.Equal(t, true, account.Extra["existing"])
}

func TestAutomaticPrivacyChecksEnqueueAndFreshStatus(t *testing.T) {
	for _, platform := range []string{PlatformOpenAI, PlatformAntigravity} {
		for _, state := range []string{StatusActive, "inactive", StatusError, "queued-inactive", "queued-error", "read-failure", "missing"} {
			t.Run(platform+"/"+state, func(t *testing.T) {
				account := &Account{ID: 42, Status: StatusActive, Platform: platform, Type: AccountTypeOAuth}
				fresh := *account
				admin := &automaticPrivacyAdmin{fresh: &fresh}
				switch state {
				case "inactive", StatusError:
					account.Status = state
				case "queued-inactive":
					fresh.Status = "inactive"
				case "queued-error":
					fresh.Status = StatusError
				case "read-failure":
					admin.err = errors.New("unavailable")
				case "missing":
					admin.fresh = nil
				}
				EnsureAutomaticAccountPrivacy(context.Background(), admin, account)
				if state == StatusActive {
					if platform == PlatformOpenAI {
						require.Equal(t, 1, admin.openAI)
						require.Zero(t, admin.antigravity)
					} else {
						require.Equal(t, 1, admin.antigravity)
						require.Zero(t, admin.openAI)
					}
				} else {
					require.Zero(t, admin.openAI+admin.antigravity)
				}
				if state == "inactive" || state == StatusError {
					require.Zero(t, admin.reads)
				} else {
					require.Equal(t, 1, admin.reads)
				}
			})
		}
	}
}
