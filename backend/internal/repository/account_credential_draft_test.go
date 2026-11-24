package repository

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"sync"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent/account"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent/accountgroup"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCredentialDraftRepositoryTransactionOwnership(t *testing.T) {
	dsn := os.Getenv("CREDENTIAL_DRAFT_TEST_DSN")
	if dsn == "" {
		t.Skip("set CREDENTIAL_DRAFT_TEST_DSN to an isolated PostgreSQL database")
	}
	database, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })
	ctx := context.Background()
	require.NoError(t, ApplyMigrations(ctx, database))
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, database)))
	repository := newAccountRepositoryWithSQL(client, database, nil)
	repository.credentialEncryptor = persistentTestAES(t, true)
	fixture := uniqueName("credential-draft-test")
	t.Cleanup(func() {
		_, err := database.ExecContext(ctx, "DELETE FROM pending_auth_sessions WHERE browser_session_key=$1", fixture)
		require.NoError(t, err)
		_, err = database.ExecContext(ctx, "DELETE FROM credential_secrets WHERE account_id IN (SELECT id FROM accounts WHERE name LIKE $1)", fixture+"-%")
		require.NoError(t, err)
		_, err = database.ExecContext(ctx, "DELETE FROM accounts WHERE name LIKE $1", fixture+"-%")
		require.NoError(t, err)
		_, err = database.ExecContext(ctx, "DELETE FROM groups WHERE name=$1", fixture)
		require.NoError(t, err)
	})
	group, err := client.Group.Create().SetName(fixture).SetPlatform(service.PlatformAnthropic).Save(ctx)
	require.NoError(t, err)
	for _, withGroups := range []bool{false, true} {
		for _, callerTransaction := range []bool{false, true} {
			candidate := &service.Account{Name: fixture + "-transaction", Platform: service.PlatformAnthropic, Type: service.AccountTypeOAuth, Status: service.StatusActive, Credentials: map[string]any{"access_token": "secret"}}
			writeContext := ctx
			var transaction *dbent.Tx
			if callerTransaction {
				transaction, err = client.Tx(ctx)
				require.NoError(t, err)
				writeContext = dbent.NewTxContext(ctx, transaction)
			}
			if withGroups {
				err = repository.CreateWithAccountGroups(writeContext, candidate, []service.AccountGroup{{GroupID: group.ID, Priority: 1}})
			} else {
				err = repository.Create(writeContext, candidate)
			}
			require.NoError(t, err)
			if callerTransaction {
				require.Equal(t, 1, transaction.Account.Query().Where(account.IDEQ(candidate.ID)).CountX(writeContext))
				require.NoError(t, transaction.Rollback())
				require.Zero(t, client.Account.Query().Where(account.IDEQ(candidate.ID)).CountX(ctx))
				require.Zero(t, client.AccountGroup.Query().Where(accountgroup.AccountIDEQ(candidate.ID)).CountX(ctx))
				var outboxCount int
				require.NoError(t, database.QueryRowContext(ctx, "SELECT COUNT(*) FROM scheduler_outbox WHERE account_id=$1", candidate.ID).Scan(&outboxCount))
				require.Zero(t, outboxCount)
			} else {
				require.Equal(t, 1, client.Account.Query().Where(account.IDEQ(candidate.ID)).CountX(ctx))
			}
		}
	}

	pending := service.NewAuthPendingIdentityService(client)
	draft, err := pending.IssueAccountCredentialDraft(ctx, fixture, service.PlatformAnthropic, service.AccountTypeOAuth, map[string]any{"access_token": "secret"})
	require.NoError(t, err)
	failure := errors.New("failure after account and group writes")
	var failedID int64
	_, err = pending.ConfirmAccountCredentialDraft(ctx, draft.Reference, fixture, draft.Revision, "submission", func(txCtx context.Context, candidate *service.Account) (int64, error) {
		candidate.Name, candidate.Status = fixture+"-failed", service.StatusActive
		if err := repository.CreateWithAccountGroups(txCtx, candidate, []service.AccountGroup{{GroupID: group.ID, Priority: 1}}); err != nil {
			return 0, err
		}
		failedID = candidate.ID
		return 0, failure
	})
	require.ErrorIs(t, err, failure)
	require.Positive(t, failedID)
	require.Zero(t, client.Account.Query().Where(account.IDEQ(failedID)).CountX(ctx))
	_, err = pending.PreviewAccountCredentialDraft(ctx, draft.Reference, fixture)
	require.NoError(t, err)
	var workers sync.WaitGroup
	results := make(chan int64, 8)
	failures := make(chan error, 8)
	for range 8 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			confirmedID, confirmErr := pending.ConfirmAccountCredentialDraft(ctx, draft.Reference, fixture, draft.Revision, "submission", func(txCtx context.Context, candidate *service.Account) (int64, error) {
				candidate.Name, candidate.Status = fixture+"-concurrent", service.StatusActive
				writeErr := repository.CreateWithAccountGroups(txCtx, candidate, []service.AccountGroup{{GroupID: group.ID, Priority: 1}})
				return candidate.ID, writeErr
			})
			results <- confirmedID
			failures <- confirmErr
		}()
	}
	workers.Wait()
	close(results)
	close(failures)
	for failure := range failures {
		require.NoError(t, failure)
	}
	var winner int64
	for result := range results {
		if winner == 0 {
			winner = result
		}
		require.Equal(t, winner, result)
	}
	require.Equal(t, 1, client.Account.Query().Where(account.NameEQ(fixture+"-concurrent")).CountX(ctx))

	admin := &credentialGuardAdmin{repository: repository}
	for _, platform := range []string{service.PlatformAnthropic, service.PlatformOpenAI, service.PlatformGemini, service.PlatformAntigravity, service.PlatformGrok} {
		candidate := &service.Account{Name: fixture + "-refresh-" + platform, Platform: platform, Type: service.AccountTypeOAuth, Status: service.StatusActive, Credentials: map[string]any{"access_token": "old", "base_url": "https://old.example"}}
		require.NoError(t, repository.Create(ctx, candidate))
		baseline, err := repository.GetByID(ctx, candidate.ID)
		require.NoError(t, err)
		replacement := map[string]any{"access_token": "other-authorization", "base_url": "https://new.example"}
		err = repository.UpdateCredentialsCAS(ctx, baseline, replacement)
		require.NoError(t, err)
		_, err = pending.UpdateAccountCredentialsIfUnchanged(ctx, baseline, admin, &service.UpdateAccountInput{Credentials: map[string]any{"access_token": "stale-refresh"}})
		require.ErrorIs(t, err, service.ErrAccountCredentialDraftConflict)
		current, err := repository.GetByID(ctx, candidate.ID)
		require.NoError(t, err)
		service.StripCredentialReferenceKeys(current.Credentials)
		require.Equal(t, replacement, current.Credentials)
		baseline, err = repository.GetByID(ctx, candidate.ID)
		require.NoError(t, err)
		updated, err := pending.UpdateAccountCredentialsIfUnchanged(service.WithCredentialRevision(ctx, baseline.CredentialRevision), baseline, admin, &service.UpdateAccountInput{Credentials: map[string]any{"access_token": "fresh"}})
		require.NoError(t, err)
		require.Equal(t, "fresh", updated.GetCredential("access_token"))
	}
}

type credentialGuardAdmin struct {
	service.AdminService
	repository *accountRepository
}

func (admin *credentialGuardAdmin) GetAccount(ctx context.Context, id int64) (*service.Account, error) {
	return admin.repository.GetByID(ctx, id)
}

func (admin *credentialGuardAdmin) UpdateAccount(ctx context.Context, id int64, input *service.UpdateAccountInput) (*service.Account, error) {
	candidate, err := admin.repository.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	candidate.Credentials = service.MergeCredentials(candidate.Credentials, input.Credentials)
	return candidate, admin.repository.Update(ctx, candidate)
}
