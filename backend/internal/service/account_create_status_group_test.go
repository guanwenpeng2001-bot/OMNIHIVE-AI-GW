//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/imroc/req/v3"
	"github.com/stretchr/testify/require"
)

func TestCreateAccountStatusPreservesDefaultGroupBinding(t *testing.T) {
	for _, skip := range []bool{false, true} {
		repo := &accountRepoStubForBulkUpdate{createID: 99}
		groups := &sparkShadowGroupRepoStub{groups: []Group{{ID: 7, Name: PlatformAnthropic + "-default"}}}
		admin := &adminServiceImpl{accountRepo: repo, groupRepo: groups}
		account, err := admin.CreateAccount(context.Background(), &CreateAccountInput{Name: "inactive", Status: "inactive", Platform: PlatformAnthropic, Type: AccountTypeAPIKey, SkipDefaultGroupBind: skip, SkipMixedChannelCheck: true})
		require.NoError(t, err)
		require.Equal(t, "inactive", account.Status)
		require.Equal(t, "inactive", repo.createAccount.Status)
		if skip {
			require.Empty(t, repo.bindGroupsCalls)
		} else {
			require.Equal(t, []int64{7}, repo.bindGroupsByAccount[99])
		}
	}
}

func TestCreateNonActiveOAuthDoesNotStartPrivacyClient(t *testing.T) {
	for _, status := range []string{"inactive", StatusError} {
		t.Run(status, func(t *testing.T) {
			repo := &upstreamBillingProbeAccountRepo{}
			calls := make(chan struct{}, 1)
			admin := &adminServiceImpl{accountRepo: repo, privacyClientFactory: func(string) (*req.Client, error) { calls <- struct{}{}; return nil, errors.New("test factory") }}
			account, err := admin.CreateAccount(context.Background(), &CreateAccountInput{Name: "non-active", Status: status, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "test"}, SkipDefaultGroupBind: true})
			require.NoError(t, err)
			require.Equal(t, status, account.Status)
			stored, err := repo.GetByID(context.Background(), account.ID)
			require.NoError(t, err)
			require.Equal(t, status, stored.Status)
			require.Empty(t, calls)
			// The same inactive account is still eligible for a manual refresh's
			// privacy helper. The failing factory ensures no real HTTP is sent.
			require.Equal(t, PrivacyModeFailed, admin.EnsureOpenAIPrivacy(context.Background(), account))
			require.Len(t, calls, 1)
		})
	}
}
