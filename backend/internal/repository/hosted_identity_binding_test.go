package repository

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	dbaccount "github.com/Wei-Shaw/sub2api/ent/account"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestHostedIdentityDatabaseBindingsAndDualRead(t *testing.T) {
	dsn := os.Getenv("HOSTED_IDENTITY_TEST_DSN")
	if dsn == "" {
		t.Skip("HOSTED_IDENTITY_TEST_DSN is not configured")
	}
	database, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer func() { _ = database.Close() }()
	ctx := context.Background()
	require.NoError(t, ApplyMigrations(ctx, database))
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, database)))
	transaction, err := client.Tx(ctx)
	require.NoError(t, err)
	defer func() { _ = transaction.Rollback() }()
	transactionalClient := transaction.Client()
	repository := newAccountRepositoryWithSQL(transactionalClient, transactionalClient, nil)
	unrestricted, err := transactionalClient.Group.Create().SetName("hosted-fixture-unrestricted").SetPlatform(service.PlatformAnthropic).Save(ctx)
	require.NoError(t, err)
	restricted, err := transactionalClient.Group.Create().SetName("hosted-fixture-restricted").SetPlatform(service.PlatformAnthropic).SetAccountType(service.AccountTypeBedrock).Save(ctx)
	require.NoError(t, err)
	createAccount := func(platform, accountType string) *dbent.Account {
		account, createErr := transactionalClient.Account.Create().SetName(platform + "-" + accountType).SetPlatform(platform).SetType(accountType).SetCredentials(map[string]any{}).SetExtra(map[string]any{}).Save(ctx)
		require.NoError(t, createErr)
		return account
	}
	legacy := createAccount(service.PlatformBedrock, service.AccountTypeBedrock)
	migrated := createAccount(service.PlatformAnthropic, service.AccountTypeBedrock)
	direct := createAccount(service.PlatformAnthropic, service.AccountTypeAPIKey)
	require.NoError(t, repository.BindGroups(ctx, legacy.ID, []int64{restricted.ID}))
	require.NoError(t, repository.BindGroups(ctx, direct.ID, []int64{unrestricted.ID}))
	require.ErrorContains(t, repository.BindGroups(ctx, direct.ID, []int64{restricted.ID}), "does not satisfy hosted group")
	memberships, err := repository.loadAccountGroupIDs(ctx, direct.ID)
	require.NoError(t, err)
	require.Equal(t, []int64{unrestricted.ID}, memberships, "rejected rebind retains existing membership")
	groupRepository := newGroupRepositoryWithSQL(transactionalClient, transactionalClient)
	require.ErrorContains(t, groupRepository.BindAccountsToGroup(ctx, restricted.ID, []int64{direct.ID}), "does not satisfy hosted group")
	require.NoError(t, groupRepository.BindAccountsToGroup(ctx, restricted.ID, []int64{migrated.ID}))
	for _, platform := range []string{service.PlatformAnthropic, service.PlatformBedrock} {
		query := transactionalClient.Account.Query().Where(accountPlatformPredicate(platform), dbaccount.IDIn(legacy.ID, migrated.ID, direct.ID))
		rows, queryErr := query.All(ctx)
		require.NoError(t, queryErr)
		if platform == service.PlatformAnthropic {
			require.Len(t, rows, 3)
		} else {
			require.Len(t, rows, 2)
		}
	}
	rows, err := transactionalClient.Account.Query().Where(accountPlatformsPredicate(service.PlatformAnthropic, service.PlatformBedrock), dbaccount.IDIn(legacy.ID, migrated.ID, direct.ID)).All(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 3, "overlapping alias filters must count each account once")
	_, err = transactionalClient.ExecContext(ctx, `SELECT activate_hosted_account_identity()`)
	require.NoError(t, err)
	persisted, err := transactionalClient.Account.Get(ctx, legacy.ID)
	require.NoError(t, err)
	require.Equal(t, service.PlatformAnthropic, persisted.Platform)
	require.Equal(t, service.AccountTypeBedrock, persisted.Type)
	require.Equal(t, "converse", persisted.Extra["hosted_bedrock_runtime"])
	createdGroup := &service.Group{Name: "hosted-fixture-new-legacy", Platform: service.PlatformBedrock, Status: service.StatusActive, SubscriptionType: service.SubscriptionTypeStandard, RateMultiplier: 1}
	require.NoError(t, createGroupRecord(ctx, transactionalClient, createdGroup))
	require.Equal(t, service.PlatformAnthropic, createdGroup.Platform)
	require.Equal(t, service.AccountTypeBedrock, createdGroup.AccountType)
	createdAccount := &service.Account{Name: "hosted-fixture-new-account", Platform: service.PlatformBedrock, Type: service.AccountTypeBedrock, Credentials: map[string]any{}, Extra: map[string]any{}, Status: service.StatusActive}
	require.NoError(t, createAccountRecord(ctx, transactionalClient, createdAccount))
	require.Equal(t, service.PlatformAnthropic, createdAccount.Platform)
	require.True(t, createdAccount.UsesBedrockCatalog())
	_, err = transactionalClient.ExecContext(ctx, `SELECT rollback_hosted_account_identity()`)
	require.NoError(t, err)
	newGroup, err := transactionalClient.Group.Get(ctx, createdGroup.ID)
	require.NoError(t, err)
	require.Equal(t, service.PlatformAnthropic, newGroup.Platform, "rollback excludes new records on the complete schema")
}
