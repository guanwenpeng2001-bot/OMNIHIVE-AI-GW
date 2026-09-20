//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/pendingauthsession"
	"github.com/stretchr/testify/require"
)

func createCredentialDraftTestAccount(ctx context.Context, account *Account) (int64, error) {
	created, err := dbent.TxFromContext(ctx).Account.Create().SetName("confirmed").SetPlatform(account.Platform).SetType(account.Type).SetCredentials(account.Credentials).Save(ctx)
	if err != nil {
		return 0, err
	}
	return created.ID, nil
}

func TestAccountCredentialDraftLifecycle(t *testing.T) {
	for _, entry := range []struct{ platform, accountType string }{{PlatformAnthropic, AccountTypeOAuth}, {PlatformAnthropic, AccountTypeSetupToken}, {PlatformCursor, AccountTypeOAuth}} {
		t.Run(entry.platform+entry.accountType, func(t *testing.T) {
			pending, client := newAuthPendingIdentityServiceTestClient(t)
			ctx := context.Background()
			draft, err := pending.IssueAccountCredentialDraft(ctx, "browser-session", entry.platform, entry.accountType, map[string]any{"access_token": "access-secret", "refresh_token": "refresh-secret"})
			require.NoError(t, err)
			encoded, err := json.Marshal(draft)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), "secret")
			require.Zero(t, client.Account.Query().CountX(ctx))
			_, err = pending.PreviewAccountCredentialDraft(ctx, draft.Reference, "another-browser")
			require.ErrorIs(t, err, ErrPendingAuthBrowserMismatch)
			_, err = pending.ConfirmAccountCredentialDraft(ctx, draft.Reference, "another-browser", draft.Revision, "request", createCredentialDraftTestAccount)
			require.ErrorIs(t, err, ErrPendingAuthBrowserMismatch)
			_, err = pending.ConfirmAccountCredentialDraft(ctx, draft.Reference, "browser-session", "stale-review", "request", createCredentialDraftTestAccount)
			require.ErrorIs(t, err, ErrAccountCredentialDraftConflict)
			accountID, err := pending.ConfirmAccountCredentialDraft(ctx, draft.Reference, "browser-session", draft.Revision, "request", createCredentialDraftTestAccount)
			require.NoError(t, err)
			retriedID, err := pending.ConfirmAccountCredentialDraft(ctx, draft.Reference, "browser-session", draft.Revision, "request", createCredentialDraftTestAccount)
			require.NoError(t, err)
			require.Equal(t, accountID, retriedID)
			require.Equal(t, 1, client.Account.Query().CountX(ctx))
			_, err = pending.ConfirmAccountCredentialDraft(ctx, draft.Reference, "browser-session", draft.Revision, "different-request", createCredentialDraftTestAccount)
			require.ErrorIs(t, err, ErrAccountCredentialDraftConflict)
			stored := client.PendingAuthSession.Query().OnlyX(ctx)
			require.NotContains(t, stored.LocalFlowState["credential_draft_state"], "secret")
			require.ErrorIs(t, pending.CancelAccountCredentialDraft(ctx, draft.Reference, "browser-session"), ErrPendingAuthSessionConsumed)
		})
	}
}

func TestAccountCredentialDraftFailureRollsBackAndCanRetry(t *testing.T) {
	pending, client := newAuthPendingIdentityServiceTestClient(t)
	ctx := context.Background()
	draft, err := pending.IssueAccountCredentialDraft(ctx, "browser", PlatformAnthropic, AccountTypeOAuth, map[string]any{"access_token": "secret"})
	require.NoError(t, err)
	failure := errors.New("group binding failed")
	_, err = pending.ConfirmAccountCredentialDraft(ctx, draft.Reference, "browser", draft.Revision, "request", func(ctx context.Context, account *Account) (int64, error) {
		_, err := createCredentialDraftTestAccount(ctx, account)
		require.NoError(t, err)
		return 0, failure
	})
	require.ErrorIs(t, err, failure)
	require.Zero(t, client.Account.Query().CountX(ctx))
	_, err = pending.PreviewAccountCredentialDraft(ctx, draft.Reference, "browser")
	require.NoError(t, err)
	_, err = pending.ConfirmAccountCredentialDraft(ctx, draft.Reference, "browser", draft.Revision, "request", createCredentialDraftTestAccount)
	require.NoError(t, err)
	require.Equal(t, 1, client.Account.Query().CountX(ctx))
}

func TestAccountCredentialDraftCancellationAndExpiry(t *testing.T) {
	pending, client := newAuthPendingIdentityServiceTestClient(t)
	ctx := context.Background()
	for _, cancel := range []bool{true, false} {
		draft, err := pending.IssueAccountCredentialDraft(ctx, "browser", PlatformCursor, AccountTypeOAuth, map[string]any{"access_token": "secret"})
		require.NoError(t, err)
		expected := ErrPendingAuthSessionConsumed
		if cancel {
			require.NoError(t, pending.CancelAccountCredentialDraft(ctx, draft.Reference, "browser"))
			require.NoError(t, pending.CancelAccountCredentialDraft(ctx, draft.Reference, "browser"))
		} else {
			expected = ErrPendingAuthSessionExpired
			_, err = client.PendingAuthSession.Update().Where(pendingauthsession.SessionTokenEQ(hashPendingAuthCode(draft.Reference))).SetExpiresAt(time.Now().Add(-time.Second)).Save(ctx)
			require.NoError(t, err)
		}
		_, err = pending.ConfirmAccountCredentialDraft(ctx, draft.Reference, "browser", draft.Revision, "request", createCredentialDraftTestAccount)
		require.ErrorIs(t, err, expected)
	}
	require.Zero(t, client.Account.Query().CountX(ctx))
}

func TestAccountCredentialDraftConcurrentConfirmation(t *testing.T) {
	pending, client := newAuthPendingIdentityServiceTestClient(t)
	ctx := context.Background()
	draft, err := pending.IssueAccountCredentialDraft(ctx, "browser", PlatformCursor, AccountTypeOAuth, map[string]any{"access_token": "secret"})
	require.NoError(t, err)
	var workers sync.WaitGroup
	results := make(chan int64, 8)
	failures := make(chan error, 8)
	for range 8 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			accountID, err := pending.ConfirmAccountCredentialDraft(ctx, draft.Reference, "browser", draft.Revision, "request", createCredentialDraftTestAccount)
			if err != nil {
				failures <- err
				return
			}
			results <- accountID
		}()
	}
	workers.Wait()
	close(results)
	close(failures)
	for failure := range failures {
		require.NoError(t, failure)
	}
	var confirmedID int64
	for accountID := range results {
		if confirmedID == 0 {
			confirmedID = accountID
		}
		require.Equal(t, confirmedID, accountID)
	}
	require.Positive(t, confirmedID)
	require.Equal(t, 1, client.Account.Query().CountX(ctx))
}

func TestAccountCredentialDraftReviewRejectsChangedRules(t *testing.T) {
	pending, client := newAuthPendingIdentityServiceTestClient(t)
	ctx := context.Background()
	draft, err := pending.IssueAccountCredentialDraft(ctx, "browser", PlatformAnthropic, AccountTypeOAuth, map[string]any{"access_token": "secret"})
	require.NoError(t, err)
	reviewed, err := pending.ReviewAccountCredentialDraft(ctx, draft.Reference, "browser", draft.Revision, "reviewed-rules-and-snapshot")
	require.NoError(t, err)
	require.NotEqual(t, draft.Revision, reviewed.Revision)
	_, err = pending.ConfirmAccountCredentialDraft(ctx, draft.Reference, "browser", draft.Revision, "reviewed-rules-and-snapshot", createCredentialDraftTestAccount)
	require.ErrorIs(t, err, ErrAccountCredentialDraftConflict)
	_, err = pending.ConfirmAccountCredentialDraft(ctx, draft.Reference, "browser", reviewed.Revision, "changed-rules", createCredentialDraftTestAccount)
	require.ErrorIs(t, err, ErrAccountCredentialDraftConflict)
	require.Zero(t, client.Account.Query().CountX(ctx))
	_, err = pending.ConfirmAccountCredentialDraft(ctx, draft.Reference, "browser", reviewed.Revision, "reviewed-rules-and-snapshot", createCredentialDraftTestAccount)
	require.NoError(t, err)
}

type draftAtomicCreateRecorder struct {
	groups      []AccountGroup
	transaction *dbent.Tx
}

func (recorder *draftAtomicCreateRecorder) CreateWithAccountGroups(ctx context.Context, account *Account, groups []AccountGroup) error {
	recorder.groups, recorder.transaction = groups, dbent.TxFromContext(ctx)
	account.ID = 99
	return nil
}

func TestAdminCreateCredentialDraftUsesAtomicWriterAndOrdinaryCreateIsUnchanged(t *testing.T) {
	_, client := newAuthPendingIdentityServiceTestClient(t)
	for _, transactional := range []bool{false, true} {
		ctx := context.Background()
		repository := &accountRepoStubForBulkUpdate{createID: 99}
		atomic := &draftAtomicCreateRecorder{}
		admin := &adminServiceImpl{accountRepo: repository, accountDuplicateRepo: atomic, groupRepo: &groupRepoStubForAdmin{getByIDByID: map[int64]*Group{17: {ID: 17, Platform: PlatformComposite}, 23: {ID: 23, Platform: PlatformComposite}}}}
		if transactional {
			transaction, err := client.Tx(ctx)
			require.NoError(t, err)
			defer func() { _ = transaction.Rollback() }()
			ctx = dbent.NewTxContext(ctx, transaction)
		}
		_, err := admin.CreateAccount(ctx, &CreateAccountInput{Name: "confirmed", Platform: PlatformAnthropic, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "secret"}, GroupIDs: []int64{17, 23}, SkipDefaultGroupBind: true, SkipMixedChannelCheck: true})
		require.NoError(t, err)
		if transactional {
			require.NotNil(t, atomic.transaction)
			require.Equal(t, []AccountGroup{{GroupID: 17, Priority: 1}, {GroupID: 23, Priority: 2}}, atomic.groups)
			require.Nil(t, repository.createAccount)
			require.Empty(t, repository.bindGroupsCalls)
		} else {
			require.Nil(t, atomic.transaction)
			require.NotNil(t, repository.createAccount)
			require.Equal(t, []int64{17, 23}, repository.bindGroupsByAccount[99])
		}
	}
}
