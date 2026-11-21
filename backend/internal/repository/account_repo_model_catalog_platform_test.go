//go:build unit

package repository

import (
	"context"
	"os"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil/testresources"
	"github.com/stretchr/testify/require"
)

func TestModelCatalogCompositePlatformArgument(t *testing.T) {
	for _, platforms := range [][]string{nil, {}} {
		name := "empty"
		if platforms == nil {
			name = "nil"
		}
		t.Run(name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			repo := &accountRepository{sql: db}
			mock.ExpectQuery(regexp.QuoteMeta("cardinality($2::text[]) = 0 OR a.platform = ANY($2::text[])")).
				WithArgs(int64(0), "{}", int64(42), false).
				WillReturnRows(sqlmock.NewRows([]string{
					"id", "platform", "type", "status", "schedulable", "parent_account_id", "expires_at", "auto_pause_on_expired", "temp_unschedulable_until", "workspace_id", "proxy_id", "proxy_group_id", "credentials", "extra",
				}).AddRow(1, "openai", "apikey", "active", true, nil, nil, false, nil, 1, nil, nil, `{}`, `{}`).
					AddRow(2, "anthropic", "apikey", "active", true, nil, nil, false, nil, 1, nil, nil, `{}`, `{}`))
			groupID := int64(42)
			accounts, err := repo.ListModelCatalogAccounts(context.Background(), &groupID, platforms, false)
			require.NoError(t, err)
			require.Len(t, accounts, 2)
			require.Equal(t, "openai", accounts[0].Platform)
			require.Equal(t, "anthropic", accounts[1].Platform)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestModelCatalogPlatformFilteringPostgres(t *testing.T) {
	dsn := os.Getenv("UPSTREAM_JOB_TEST_DSN")
	if dsn == "" {
		t.Skip("set UPSTREAM_JOB_TEST_DSN to isolated job_test")
	}
	db := testresources.OpenSchema(t, dsn)
	ctx := domain.WithWorkspaceScope(context.Background(), 1)
	_, err := db.ExecContext(ctx, `
CREATE TABLE accounts (
 id bigint PRIMARY KEY, workspace_id bigint DEFAULT 1, proxy_id bigint, proxy_group_id bigint, platform text, type text,
 status text DEFAULT 'active', schedulable boolean DEFAULT true,
 parent_account_id bigint, expires_at timestamptz, auto_pause_on_expired boolean DEFAULT false,
 temp_unschedulable_until timestamptz, deleted_at timestamptz,
 credentials jsonb DEFAULT '{}', extra jsonb DEFAULT '{}'
);
CREATE TABLE groups (id bigint PRIMARY KEY, platform text, account_type text);
CREATE TABLE account_groups (account_id bigint, group_id bigint);
INSERT INTO groups VALUES (42, 'composite', ''), (25, 'antigravity', 'oauth'), (43, 'composite', 'oauth');
INSERT INTO accounts (id, platform, type) VALUES
 (1, 'openai', 'apikey'), (2, 'anthropic', 'apikey'), (3, 'antigravity', 'oauth'),
 (4, 'bedrock', 'bedrock'), (5, 'anthropic', 'bedrock'),
 (6, 'vertex', 'service_account'), (7, 'gemini', 'service_account'),
 (8, 'openai', 'apikey'), (9, 'openai', 'apikey'), (10, 'openai', 'apikey'),
 (11, 'openai', 'apikey'), (12, 'openai', 'apikey');
INSERT INTO account_groups SELECT id, 42 FROM accounts WHERE id <> 8;
INSERT INTO account_groups VALUES (3,25), (1,25), (1,43), (3,43);
UPDATE accounts SET deleted_at = now() WHERE id = 9;
UPDATE accounts SET status = 'error' WHERE id = 10;
UPDATE accounts SET schedulable = false WHERE id = 11;
UPDATE accounts SET workspace_id = 2 WHERE id = 12;
`)
	require.NoError(t, err)
	repo := &accountRepository{sql: db}
	batched, err := repo.ListModelCatalogAccountsForGroups(ctx, []int64{42, 25})
	require.NoError(t, err)
	require.Len(t, batched, 7)
	require.Equal(t, []int64{25, 42}, batched[0].GroupIDs)
	require.Equal(t, []int64{25, 42}, batched[2].GroupIDs)
	configured, err := repo.ListConfiguredModelCatalogAccountsForGroups(ctx, []int64{42, 25})
	require.NoError(t, err)
	require.Len(t, configured, 8, "active paused accounts configure inventory without becoming routable")
	require.Equal(t, int64(11), configured[7].ID)
	require.False(t, configured[7].Schedulable)
	for _, account := range configured {
		require.Equal(t, "active", account.Status)
		require.Equal(t, int64(1), account.WorkspaceID)
		require.Empty(t, account.GetCredential("api_key"))
	}
	_, err = db.ExecContext(ctx, `INSERT INTO accounts (id,platform,type,status,schedulable) VALUES (13,'openai','oauth','error',false); UPDATE accounts SET parent_account_id=13 WHERE id=1`)
	require.NoError(t, err)
	withOwner, err := repo.ListModelCatalogAccountsForGroups(ctx, []int64{25})
	require.NoError(t, err)
	require.Len(t, withOwner, 3)
	require.Equal(t, int64(13), withOwner[2].ID)
	require.Empty(t, withOwner[2].GroupIDs)
	composite, antigravity, typed := int64(42), int64(25), int64(43)
	for _, tc := range []struct {
		name           string
		group          *int64
		platforms      []string
		includeGrouped bool
		want           []int64
	}{
		{"composite nil", &composite, nil, false, []int64{1, 2, 3, 4, 5, 6, 7}},
		{"composite empty", &composite, []string{}, false, []int64{1, 2, 3, 4, 5, 6, 7}},
		{"explicit openai", &composite, []string{"openai"}, false, []int64{1}},
		{"anthropic includes bedrock", &composite, []string{"anthropic"}, false, []int64{2, 4, 5}},
		{"bedrock includes legacy anthropic", &composite, []string{"bedrock"}, false, []int64{4, 5}},
		{"gemini includes vertex", &composite, []string{"gemini"}, false, []int64{6, 7}},
		{"vertex includes legacy gemini", &composite, []string{"vertex"}, false, []int64{6, 7}},
		{"antigravity remains filtered", &antigravity, []string{"antigravity"}, false, []int64{3}},
		{"account type remains filtered", &typed, nil, false, []int64{3}},
		{"ungrouped remains filtered", nil, nil, false, []int64{8}},
		{"include grouped", nil, nil, true, []int64{1, 2, 3, 4, 5, 6, 7, 8}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			accounts, err := repo.ListModelCatalogAccounts(ctx, tc.group, tc.platforms, tc.includeGrouped)
			require.NoError(t, err)
			ids := make([]int64, 0, len(accounts))
			for _, account := range accounts {
				ids = append(ids, account.ID)
			}
			require.Equal(t, tc.want, ids)
		})
	}
}

func TestModelCatalogBatchReadsSharedAccountOnce(tester *testing.T) {
	database, expectations, err := sqlmock.New()
	require.NoError(tester, err)
	tester.Cleanup(func() { _ = database.Close() })
	repository := &accountRepository{sql: database}
	expectations.ExpectQuery(regexp.QuoteMeta("membership.group_id=ANY($5::bigint[])")).
		WithArgs(int64(0), "{}", nil, true, "{7,9}").
		WillReturnRows(sqlmock.NewRows([]string{"id", "platform", "type", "status", "schedulable", "parent_account_id", "expires_at", "auto_pause_on_expired", "temp_unschedulable_until", "workspace_id", "proxy_id", "proxy_group_id", "credentials", "extra", "group_ids"}).
			AddRow(11, "openai", "apikey", "active", true, nil, nil, false, nil, 1, nil, nil, `{}`, `{}`, "{7,9}"))
	accounts, err := repository.ListModelCatalogAccountsForGroups(context.Background(), []int64{7, 9})
	require.NoError(tester, err)
	require.Len(tester, accounts, 1)
	require.Equal(tester, []int64{7, 9}, accounts[0].GroupIDs)
	empty, err := repository.ListModelCatalogAccountsForGroups(context.Background(), nil)
	require.NoError(tester, err)
	require.Empty(tester, empty)
	require.NoError(tester, expectations.ExpectationsWereMet())
}
