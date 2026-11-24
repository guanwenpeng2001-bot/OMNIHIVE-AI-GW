//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent/pendingauthsession"
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

func TestAccountCredentialDraftBatchRollbackAndRetry(t *testing.T) {
	pending, client := newAuthPendingIdentityServiceTestClient(t)
	ctx := context.Background()
	first, err := pending.IssueAccountCredentialDraft(ctx, "browser", PlatformAnthropic, AccountTypeOAuth, map[string]any{"access_token": "first"})
	require.NoError(t, err)
	second, err := pending.IssueAccountCredentialDraft(ctx, "browser", PlatformGemini, AccountTypeOAuth, map[string]any{"access_token": "second"})
	require.NoError(t, err)
	failed := errors.New("second account group binding failed")
	effects := 0
	err = pending.RunAccountWriteTransaction(ctx, func(transactionContext context.Context) error {
		_, err := pending.ConfirmAccountCredentialDraft(transactionContext, first.Reference, "browser", first.Revision, "first-request", createCredentialDraftTestAccount)
		require.NoError(t, err)
		AfterAccountWriteCommit(transactionContext, func() { effects++ })
		_, err = pending.ConfirmAccountCredentialDraft(transactionContext, second.Reference, "browser", second.Revision, "second-request", func(ctx context.Context, account *Account) (int64, error) {
			_, err := createCredentialDraftTestAccount(ctx, account)
			require.NoError(t, err)
			return 0, failed
		})
		return err
	})
	require.ErrorIs(t, err, failed)
	require.Zero(t, client.Account.Query().CountX(ctx))
	require.Zero(t, effects)
	for _, draft := range []*AccountCredentialDraft{first, second} {
		_, err := pending.PreviewAccountCredentialDraft(ctx, draft.Reference, "browser")
		require.NoError(t, err, "rollback must preserve every draft for retry")
	}
	err = pending.RunAccountWriteTransaction(ctx, func(transactionContext context.Context) error {
		for _, draft := range []*AccountCredentialDraft{first, second} {
			if _, err := pending.ConfirmAccountCredentialDraft(transactionContext, draft.Reference, "browser", draft.Revision, draft.Reference, createCredentialDraftTestAccount); err != nil {
				return err
			}
		}
		AfterAccountWriteCommit(transactionContext, func() { effects++ })
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, 2, client.Account.Query().CountX(ctx))
	require.Equal(t, 1, effects)
}

func TestAccountCredentialDraftConfigurationRoundTrip(t *testing.T) {
	options := map[string]any{
		"model_mapping":         map[string]any{"public": "native"},
		"compact_model_mapping": map[string]any{"compact": "native-compact"},
		"plan_type":             "legacy-custom-team", "client_version": "1.2.3", "antigravity_project_id": "configured-project",
		"base_url": "https://relay.example/custom/v1", "account_scheduling_threshold": float64(71),
		"intercept_warmup_requests": true, "temp_unschedulable_enabled": true, "header_override_enabled": true,
		"header_overrides":         map[string]any{"x-routing-hint": "configured"},
		"temp_unschedulable_rules": []any{map[string]any{"error_code": float64(429), "keywords": []any{"rate limit"}, "duration_minutes": float64(5), "description": "configured"}},
	}
	for key, value := range options {
		t.Run(key, func(t *testing.T) {
			pending, client := newAuthPendingIdentityServiceTestClient(t)
			ctx := context.Background()
			draft, err := pending.IssueAccountCredentialDraft(ctx, "browser", PlatformOpenAI, AccountTypeOAuth, map[string]any{"access_token": "server-secret"})
			require.NoError(t, err)
			confirm := func(ctx context.Context, account *Account) (int64, error) {
				if err := ApplyAccountCredentialDraftOptions(account, map[string]any{key: value}); err != nil {
					return 0, err
				}
				return createCredentialDraftTestAccount(ctx, account)
			}
			accountID, err := pending.ConfirmAccountCredentialDraft(ctx, draft.Reference, "browser", draft.Revision, key, confirm)
			require.NoError(t, err)
			replayedID, err := pending.ConfirmAccountCredentialDraft(ctx, draft.Reference, "browser", draft.Revision, key, confirm)
			require.NoError(t, err)
			require.Equal(t, accountID, replayedID)
			stored := client.Account.GetX(ctx, accountID)
			require.Equal(t, value, stored.Credentials[key])
			require.Equal(t, "server-secret", stored.Credentials["access_token"])
			require.Equal(t, 1, client.Account.Query().CountX(ctx))
		})
	}
}

func TestAccountCredentialDraftOptionsRejectSecretsIdentityAndUnknownKeys(t *testing.T) {
	for _, key := range []string{"access_token", "refresh_token", "machine_id", "mac_machine_id", "client_id", "credential_owner_id", "owner_id", "email", "account_uuid", "unknown_option"} {
		t.Run(key, func(t *testing.T) {
			account := &Account{Credentials: map[string]any{"access_token": "server-only"}}
			require.Error(t, ApplyAccountCredentialDraftOptions(account, map[string]any{key: "replacement"}))
			require.Equal(t, map[string]any{"access_token": "server-only"}, account.Credentials)
		})
	}
	for _, invalid := range []map[string]any{
		{"plan_type": false}, {"base_url": "javascript:invalid"}, {"account_scheduling_threshold": 0},
		{"intercept_warmup_requests": "true"}, {"header_overrides": map[string]any{"authorization": "secret"}},
		{"temp_unschedulable_rules": []any{map[string]any{"duration_minutes": 0}}},
	} {
		require.Error(t, ApplyAccountCredentialDraftOptions(&Account{}, invalid))
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

func TestAccountCredentialDraftBindsReviewedCredentialRevision(t *testing.T) {
	pending, _ := newAuthPendingIdentityServiceTestClient(t)
	ctx := t.Context()
	draft, err := pending.IssueAccountCredentialDraft(ctx, "browser", PlatformOpenAI, AccountTypeOAuth, map[string]any{"access_token": "fixture-new-access"})
	require.NoError(t, err)
	review := func(revision, credentialRevision string) (*AccountCredentialDraft, error) {
		return pending.ReviewAccountCredentialDraft(WithCredentialRevision(ctx, credentialRevision), draft.Reference, "browser", revision, "same-settings")
	}
	first, err := review(draft.Revision, "first-credential-revision")
	require.NoError(t, err)
	retry, err := review(draft.Revision, "first-credential-revision")
	require.NoError(t, err)
	require.Equal(t, first.Revision, retry.Revision)

	_, err = review(draft.Revision, "second-credential-revision")
	require.ErrorIs(t, err, ErrAccountCredentialDraftConflict)
	second, err := review(first.Revision, "second-credential-revision")
	require.NoError(t, err)
	require.NotEqual(t, first.Revision, second.Revision)
	_, err = pending.ConfirmAccountCredentialDraft(ctx, draft.Reference, "browser", first.Revision, "same-settings", createCredentialDraftTestAccount)
	require.ErrorIs(t, err, ErrAccountCredentialDraftConflict)
	_, err = pending.ConfirmAccountCredentialDraft(ctx, draft.Reference, "browser", second.Revision, "same-settings", func(txCtx context.Context, _ *Account) (int64, error) {
		require.Equal(t, "second-credential-revision", ExpectedCredentialRevision(txCtx))
		return 42, nil
	})
	require.NoError(t, err)
}
