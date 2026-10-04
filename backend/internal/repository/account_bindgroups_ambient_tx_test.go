package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/accountgroup"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func bindGroupsTestRepository(t *testing.T) (*accountRepository, *sql.DB) {
	t.Helper()
	dsn := os.Getenv("CREDENTIAL_DRAFT_TEST_DSN")
	if dsn == "" {
		t.Skip("set CREDENTIAL_DRAFT_TEST_DSN to an isolated PostgreSQL database")
	}
	connection, err := url.Parse(dsn)
	require.NoError(t, err)
	// Apply to every connection, including an incorrectly opened inner transaction.
	options := connection.Query()
	options.Set("lock_timeout", "5s")
	connection.RawQuery = options.Encode()
	database, err := sql.Open("postgres", connection.String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })
	require.NoError(t, ApplyMigrations(context.Background(), database))
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, database)))
	return newAccountRepositoryWithSQL(client, database, nil), database
}

func lockBindGroupsAccount(ctx context.Context, transaction *dbent.Tx, accountID int64) (resultErr error) {
	rows, err := transaction.Client().QueryContext(ctx, "SELECT id FROM accounts WHERE id=$1 FOR UPDATE", accountID)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, rows.Close()) }()
	for rows.Next() {
		var lockedID int64
		if err := rows.Scan(&lockedID); err != nil {
			return err
		}
	}
	return rows.Err()
}

func TestBindGroupsAmbientTransaction(t *testing.T) {
	repository, database := bindGroupsTestRepository(t)
	ctx := context.Background()
	originalGroup, err := repository.client.Group.Create().SetName("bindgroups-original" + "-" + uuid.NewString()).SetPlatform(service.PlatformAnthropic).Save(ctx)
	require.NoError(t, err)
	stagedGroup, err := repository.client.Group.Create().SetName("bindgroups-staged" + "-" + uuid.NewString()).SetPlatform(service.PlatformAnthropic).Save(ctx)
	require.NoError(t, err)
	replacementGroup, err := repository.client.Group.Create().SetName("bindgroups-replacement" + "-" + uuid.NewString()).SetPlatform(service.PlatformAnthropic).Save(ctx)
	require.NoError(t, err)
	for _, scenario := range []struct {
		name     string
		groupIDs []int64
		commit   bool
	}{
		{name: "replace_rollback", groupIDs: []int64{replacementGroup.ID}},
		{name: "clear_rollback"},
		{name: "replace_commit", groupIDs: []int64{replacementGroup.ID}, commit: true},
		{name: "clear_commit", commit: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			account, err := repository.client.Account.Create().SetName(scenario.name + "-" + uuid.NewString()).SetPlatform(service.PlatformAnthropic).SetType(service.AccountTypeOAuth).SetCredentials(map[string]any{"access_token": "fake"}).Save(ctx)
			require.NoError(t, err)
			_, err = repository.client.AccountGroup.Create().SetAccountID(account.ID).SetGroupID(originalGroup.ID).SetPriority(7).Save(ctx)
			require.NoError(t, err)
			transaction, err := repository.client.Tx(ctx)
			require.NoError(t, err)
			defer func() {
				if err := transaction.Rollback(); !errors.Is(err, sql.ErrTxDone) {
					require.NoError(t, err)
				}
			}()
			writeContext, cancel := context.WithTimeout(dbent.NewTxContext(ctx, transaction), 8*time.Second)
			defer cancel()
			require.NoError(t, lockBindGroupsAccount(writeContext, transaction, account.ID))
			require.NoError(t, transaction.Account.UpdateOneID(account.ID).SetCredentials(map[string]any{"access_token": "updated"}).Exec(writeContext))
			// This binding is visible only inside the caller transaction. Its presence
			// in the outbox proves the old-group read also respects that transaction.
			_, err = transaction.AccountGroup.Create().SetAccountID(account.ID).SetGroupID(stagedGroup.ID).SetPriority(8).Save(writeContext)
			require.NoError(t, err)
			started := time.Now()
			require.NoError(t, repository.BindGroups(writeContext, account.ID, scenario.groupIDs))
			elapsed := time.Since(started)
			require.Less(t, elapsed, 5*time.Second)
			t.Logf("BindGroups with account FOR UPDATE completed in %s", elapsed)
			readBack, err := repository.GetByID(writeContext, account.ID)
			require.NoError(t, err)
			require.Equal(t, "updated", readBack.GetCredential("access_token"))
			require.ElementsMatch(t, scenario.groupIDs, readBack.GroupIDs)
			require.Len(t, readBack.Groups, len(scenario.groupIDs))
			actual, err := transaction.AccountGroup.Query().Where(accountgroup.AccountIDEQ(account.ID)).All(writeContext)
			require.NoError(t, err)
			require.Len(t, actual, len(scenario.groupIDs))
			for index, binding := range actual {
				require.Equal(t, scenario.groupIDs[index], binding.GroupID)
				require.Equal(t, index+1, binding.Priority)
			}
			rows, err := transaction.Client().QueryContext(writeContext, "SELECT payload FROM scheduler_outbox WHERE account_id=$1", account.ID)
			require.NoError(t, err)
			require.True(t, rows.Next())
			var raw []byte
			require.NoError(t, rows.Scan(&raw))
			var payload struct {
				GroupIDs []int64 `json:"group_ids"`
			}
			require.NoError(t, json.Unmarshal(raw, &payload))
			require.ElementsMatch(t, append([]int64{originalGroup.ID, stagedGroup.ID}, scenario.groupIDs...), payload.GroupIDs)
			require.False(t, rows.Next())
			require.NoError(t, rows.Err())
			require.NoError(t, rows.Close())
			var published int
			require.NoError(t, database.QueryRowContext(ctx, "SELECT COUNT(*) FROM scheduler_outbox WHERE account_id=$1", account.ID).Scan(&published))
			require.Zero(t, published, "outbox must not escape before caller commit")
			if scenario.commit {
				require.NoError(t, transaction.Commit())
			} else {
				require.NoError(t, transaction.Rollback())
			}
			actual, err = repository.client.AccountGroup.Query().Where(accountgroup.AccountIDEQ(account.ID)).All(ctx)
			require.NoError(t, err)
			if scenario.commit {
				require.Len(t, actual, len(scenario.groupIDs))
			} else {
				require.Len(t, actual, 1)
				require.Equal(t, "fake", repository.client.Account.GetX(ctx, account.ID).Credentials["access_token"])
				require.Equal(t, originalGroup.ID, actual[0].GroupID)
				require.Equal(t, 7, actual[0].Priority)
			}
			require.NoError(t, database.QueryRowContext(ctx, "SELECT COUNT(*) FROM scheduler_outbox WHERE account_id=$1", account.ID).Scan(&published))
			if scenario.commit {
				require.Equal(t, 1, published)
			} else {
				require.Zero(t, published)
			}
		})
	}
}

func TestBindGroupsCredentialDraftConcurrentConfirmation(t *testing.T) {
	repository, _ := bindGroupsTestRepository(t)
	ctx := context.Background()
	group, err := repository.client.Group.Create().SetName("bindgroups-confirmation" + "-" + uuid.NewString()).SetPlatform(service.PlatformAnthropic).Save(ctx)
	require.NoError(t, err)
	for _, sameSubmission := range []bool{true, false} {
		name := "different_payload"
		if sameSubmission {
			name = "same_submission"
		}
		t.Run(name, func(t *testing.T) {
			account, err := repository.client.Account.Create().SetName(name + "-" + uuid.NewString()).SetPlatform(service.PlatformAnthropic).SetType(service.AccountTypeOAuth).SetCredentials(map[string]any{"access_token": "old"}).Save(ctx)
			require.NoError(t, err)
			pending := service.NewAuthPendingIdentityService(repository.client)
			draft, err := pending.IssueAccountCredentialDraft(ctx, "browser", service.PlatformAnthropic, service.AccountTypeOAuth, map[string]any{"access_token": "new"})
			require.NoError(t, err)
			write := func(writeContext context.Context, candidate *service.Account) (int64, error) {
				transaction := dbent.TxFromContext(writeContext)
				if err := lockBindGroupsAccount(writeContext, transaction, account.ID); err != nil {
					return 0, err
				}
				if err := transaction.Account.UpdateOneID(account.ID).SetCredentials(candidate.Credentials).Exec(writeContext); err != nil {
					return 0, err
				}
				if err := repository.BindGroups(writeContext, account.ID, []int64{group.ID}); err != nil {
					return 0, err
				}
				readBack, err := repository.GetByID(writeContext, account.ID)
				if err != nil {
					return 0, err
				}
				if readBack.GetCredential("access_token") != "new" || len(readBack.GroupIDs) != 1 || readBack.GroupIDs[0] != group.ID {
					return 0, errors.New("confirmation returned stale account data")
				}
				return readBack.ID, nil
			}
			// A failed confirmation must leave the draft available and all writes undone.
			failure := errors.New("confirmation interrupted after binding")
			_, err = pending.ConfirmAccountCredentialDraft(ctx, draft.Reference, "browser", draft.Revision, "submission", func(writeContext context.Context, candidate *service.Account) (int64, error) {
				if _, err := write(writeContext, candidate); err != nil {
					return 0, err
				}
				return 0, failure
			})
			require.ErrorIs(t, err, failure)
			require.Equal(t, "old", repository.client.Account.GetX(ctx, account.ID).Credentials["access_token"])
			require.Zero(t, repository.client.AccountGroup.Query().Where(accountgroup.AccountIDEQ(account.ID)).CountX(ctx))
			_, err = pending.PreviewAccountCredentialDraft(ctx, draft.Reference, "browser")
			require.NoError(t, err)
			type confirmationResult struct {
				accountID int64
				err       error
			}
			results := make(chan confirmationResult, 2)
			start := make(chan struct{})
			var workers sync.WaitGroup
			for _, submission := range []string{"submission", func() string {
				if sameSubmission {
					return "submission"
				}
				return "different-payload"
			}()} {
				workers.Add(1)
				go func(submission string) {
					defer workers.Done()
					<-start
					writeContext, cancel := context.WithTimeout(ctx, 8*time.Second)
					defer cancel()
					accountID, err := pending.ConfirmAccountCredentialDraft(writeContext, draft.Reference, "browser", draft.Revision, submission, write)
					results <- confirmationResult{accountID, err}
				}(submission)
			}
			close(start)
			workers.Wait()
			close(results)
			successes, conflicts := 0, 0
			for result := range results {
				if result.err == nil {
					successes++
					require.Equal(t, account.ID, result.accountID)
				} else {
					require.ErrorIs(t, result.err, service.ErrAccountCredentialDraftConflict)
					conflicts++
				}
			}
			if sameSubmission {
				require.Equal(t, 2, successes)
				require.Zero(t, conflicts)
			} else {
				require.Equal(t, 1, successes)
				require.Equal(t, 1, conflicts)
			}
			require.Equal(t, "new", repository.client.Account.GetX(ctx, account.ID).Credentials["access_token"])
			require.Equal(t, 1, repository.client.AccountGroup.Query().Where(accountgroup.AccountIDEQ(account.ID)).CountX(ctx))
		})
	}
}

func TestBindGroupsAmbientOutboxFailureRollsBack(t *testing.T) {
	repository, database := bindGroupsTestRepository(t)
	ctx := context.Background()
	account, err := repository.client.Account.Create().SetName("bindgroups-outbox-failure" + "-" + uuid.NewString()).SetPlatform(service.PlatformAnthropic).SetType(service.AccountTypeOAuth).SetCredentials(map[string]any{}).Save(ctx)
	require.NoError(t, err)
	group, err := repository.client.Group.Create().SetName("bindgroups-outbox-failure" + "-" + uuid.NewString()).SetPlatform(service.PlatformAnthropic).Save(ctx)
	require.NoError(t, err)
	for _, clear := range []bool{false, true} {
		transaction, err := repository.client.Tx(ctx)
		require.NoError(t, err)
		func() {
			defer func() {
				if err := transaction.Rollback(); !errors.Is(err, sql.ErrTxDone) {
					require.NoError(t, err)
				}
			}()
			writeContext := dbent.NewTxContext(ctx, transaction)
			// A transaction-local constraint injects an outbox failure without leaving
			// any schema changes or affecting another test's committed events.
			_, err = transaction.Client().ExecContext(writeContext, "ALTER TABLE scheduler_outbox ADD CONSTRAINT bindgroups_test_reject_outbox CHECK (account_id IS NULL) NOT VALID")
			require.NoError(t, err)
			_, err = transaction.AccountGroup.Create().SetAccountID(account.ID).SetGroupID(group.ID).SetPriority(1).Save(writeContext)
			require.NoError(t, err)
			groupIDs := []int64{group.ID}
			if clear {
				groupIDs = nil
			}
			require.Error(t, repository.BindGroups(writeContext, account.ID, groupIDs), "outbox failures must reach the caller")
			require.NoError(t, transaction.Rollback())
			require.Zero(t, repository.client.AccountGroup.Query().Where(accountgroup.AccountIDEQ(account.ID)).CountX(ctx))
			var published int
			require.NoError(t, database.QueryRowContext(ctx, "SELECT COUNT(*) FROM scheduler_outbox WHERE account_id=$1", account.ID).Scan(&published))
			require.Zero(t, published)
		}()
	}
}

func TestBindGroupsStandaloneClearPublishesRemovedGroups(t *testing.T) {
	repository, database := bindGroupsTestRepository(t)
	ctx := context.Background()
	account, err := repository.client.Account.Create().SetName("bindgroups-clear-" + uuid.NewString()).SetPlatform(service.PlatformAnthropic).SetType(service.AccountTypeOAuth).SetCredentials(map[string]any{}).Save(ctx)
	require.NoError(t, err)
	group, err := repository.client.Group.Create().SetName("bindgroups-clear-" + uuid.NewString()).SetPlatform(service.PlatformAnthropic).Save(ctx)
	require.NoError(t, err)
	_, err = repository.client.AccountGroup.Create().SetAccountID(account.ID).SetGroupID(group.ID).SetPriority(7).Save(ctx)
	require.NoError(t, err)
	require.NoError(t, repository.BindGroups(ctx, account.ID, nil))
	readBack, err := repository.GetByID(ctx, account.ID)
	require.NoError(t, err)
	require.Empty(t, readBack.GroupIDs)
	var raw []byte
	require.NoError(t, database.QueryRowContext(ctx, "SELECT payload FROM scheduler_outbox WHERE account_id=$1 AND event_type=$2", account.ID, service.SchedulerOutboxEventAccountGroupsChanged).Scan(&raw))
	var payload struct {
		GroupIDs []int64 `json:"group_ids"`
	}
	require.NoError(t, json.Unmarshal(raw, &payload))
	require.Equal(t, []int64{group.ID}, payload.GroupIDs)
}

func TestBindGroupsAmbientAccountReadIncludesUncommittedRelations(t *testing.T) {
	repository, database := bindGroupsTestRepository(t)
	encryptor, err := NewAESEncryptor(&config.Config{Totp: config.TotpConfig{EncryptionKey: strings.Repeat("42", 32)}})
	require.NoError(t, err)
	repository.credentialEncryptor = encryptor
	ctx := context.Background()
	transaction, err := repository.client.Tx(ctx)
	require.NoError(t, err)
	defer func() {
		if err := transaction.Rollback(); !errors.Is(err, sql.ErrTxDone) {
			require.NoError(t, err)
		}
	}()
	writeContext := dbent.NewTxContext(ctx, transaction)
	proxy, err := transaction.Proxy.Create().SetName("uncommitted-proxy").SetProtocol("http").SetHost("proxy.example.invalid").SetPort(8080).Save(writeContext)
	require.NoError(t, err)
	group, err := transaction.Group.Create().SetName("uncommitted-group-" + uuid.NewString()).SetPlatform(service.PlatformAnthropic).Save(writeContext)
	require.NoError(t, err)
	account, err := transaction.Account.Create().SetName("uncommitted-account").SetPlatform(service.PlatformAnthropic).SetType(service.AccountTypeOAuth).SetProxyID(proxy.ID).SetCredentials(map[string]any{}).Save(writeContext)
	require.NoError(t, err)
	metadata := service.CredentialSecret{Reference: uuid.NewString(), AccountID: account.ID, WorkspaceID: account.WorkspaceID, Version: 1, UpdatedAt: time.Now().UTC()}
	ciphertext, err := service.EncryptCredentialSecret(encryptor, metadata, map[string]any{"access_token": "fake-uncommitted-token"})
	require.NoError(t, err)
	_, err = transaction.Client().ExecContext(writeContext, `INSERT INTO credential_secrets(reference,workspace_id,account_id,version,ciphertext,updated_at) VALUES($1,$2,$3,1,$4,$5)`, metadata.Reference, metadata.WorkspaceID, account.ID, ciphertext, metadata.UpdatedAt)
	require.NoError(t, err)
	require.NoError(t, writeReferencedAccount(writeContext, transaction.Client(), account.ID, map[string]any{service.CredentialSecretReferenceKey: metadata.Reference, service.CredentialSecretVersionKey: metadata.Version}))
	require.NoError(t, repository.BindGroups(writeContext, account.ID, []int64{group.ID}))
	readBack, err := repository.GetByID(writeContext, account.ID)
	require.NoError(t, err)
	require.Equal(t, "fake-uncommitted-token", readBack.GetCredential("access_token"))
	require.NotNil(t, readBack.Proxy)
	require.Equal(t, proxy.Host, readBack.Proxy.Host)
	require.Equal(t, []int64{group.ID}, readBack.GroupIDs)
	require.Len(t, readBack.Groups, 1)
	require.Equal(t, group.Name, readBack.Groups[0].Name)
	require.NoError(t, transaction.Rollback())
	_, err = repository.GetByID(ctx, account.ID)
	require.ErrorIs(t, err, service.ErrAccountNotFound)
	var published int
	require.NoError(t, database.QueryRowContext(ctx, "SELECT COUNT(*) FROM scheduler_outbox WHERE account_id=$1", account.ID).Scan(&published))
	require.Zero(t, published)
}
