package repository

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestModelAccessPostgresRoundTripReferencesAndRevision(t *testing.T) {
	dsn := os.Getenv("MODEL_ACCESS_TEST_DSN")
	if dsn == "" {
		t.Skip("MODEL_ACCESS_TEST_DSN must target disposable model_access_test")
	}
	database, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })
	var databaseName string
	require.NoError(t, database.QueryRow(`SELECT current_database()`).Scan(&databaseName))
	require.Equal(t, "model_access_test", databaseName)
	ctx := domain.WithWorkspaceScope(context.Background(), 1)
	require.NoError(t, ApplyMigrations(ctx, database))
	var userID, keyID, groupID, accountID int64
	require.NoError(t, database.QueryRow(`INSERT INTO users(email,password_hash) VALUES('access-test@example.invalid','fixture') RETURNING id`).Scan(&userID))
	require.NoError(t, database.QueryRow(`INSERT INTO groups(name,platform,model_allowlist) VALUES('access-test','openai','{"enabled":true,"models":["public"]}') RETURNING id`).Scan(&groupID))
	require.NoError(t, database.QueryRow(`INSERT INTO api_keys(user_id,key,name,group_id) VALUES($1,'model-access-fixture','fixture',$2) RETURNING id`, userID, groupID).Scan(&keyID))
	require.NoError(t, database.QueryRow(`INSERT INTO accounts(name,platform,type,credentials) VALUES('access-test','openai','apikey','{}') RETURNING id`).Scan(&accountID))
	repository := NewModelAccessRepository(database)
	named, err := repository.SaveGroup(ctx, service.ModelAccessGroup{Name: "Public set", Models: []string{"public"}})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = database.Exec(`DELETE FROM model_access_rules WHERE api_key_id=$1 OR account_id=$2`, keyID, accountID)
		_, _ = database.Exec(`DELETE FROM model_access_groups WHERE id=$1`, named.ID)
		_, _ = database.Exec(`DELETE FROM api_keys WHERE id=$1`, keyID)
		_, _ = database.Exec(`DELETE FROM accounts WHERE id=$1`, accountID)
		_, _ = database.Exec(`DELETE FROM groups WHERE id=$1`, groupID)
		_, _ = database.Exec(`DELETE FROM users WHERE id=$1`, userID)
	})
	current, err := repository.Rule(ctx, "keys", keyID, userID)
	require.NoError(t, err)
	require.Nil(t, current.Models)
	for _, models := range [][]string{nil, {}, {"public"}} {
		current.Models = models
		require.NoError(t, repository.SaveRule(ctx, "keys", keyID, userID, *current))
		current, err = repository.Rule(ctx, "keys", keyID, userID)
		require.NoError(t, err)
		require.Equal(t, models, current.Models)
		snapshot, err := readModelAccessSnapshot(ctx, database, keyID)
		require.NoError(t, err)
		require.Equal(t, models, snapshot.Key)
	}
	current.Models = []string{"private"}
	require.ErrorIs(t, repository.SaveRule(ctx, "keys", keyID, userID, *current), service.ErrModelAccessExpansion)
	current.Models = nil
	current.AccessGroupID = &named.ID
	require.NoError(t, repository.SaveRule(ctx, "keys", keyID, userID, *current))
	require.ErrorIs(t, repository.DeleteGroup(ctx, named.ID, named.Revision), service.ErrModelAccessConflict)
	_, err = repository.Rule(ctx, "keys", keyID, userID+1)
	require.ErrorIs(t, err, service.ErrModelAccessNotFound)
	_, err = repository.Rule(domain.WithWorkspaceScope(ctx, 2), "keys", keyID, userID)
	require.ErrorIs(t, err, service.ErrModelAccessNotFound)
	require.NoError(t, repository.SaveRule(ctx, "accounts", accountID, 0, service.ModelAccessRule{AccessGroupID: &named.ID}))
	listener := pq.NewListener(dsn, time.Millisecond*10, time.Second, nil)
	defer func() { _ = listener.Close() }()
	require.NoError(t, listener.Listen("model_catalog_changed"))
	before, err := readModelAccessSnapshot(ctx, database, keyID)
	require.NoError(t, err)
	require.Equal(t, []string{"public"}, before.Key)
	named.Models = []string{}
	named, err = repository.SaveGroup(ctx, *named)
	require.NoError(t, err)
	anotherInstance, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer func() { _ = anotherInstance.Close() }()
	after, err := readModelAccessSnapshot(ctx, anotherInstance, keyID)
	require.NoError(t, err)
	require.Greater(t, after.Generation, before.Generation)
	require.Equal(t, []string{}, after.Key)
	require.Equal(t, []string{}, after.Accounts[accountID])
	select {
	case notification := <-listener.Notify:
		require.NotNil(t, notification)
	case <-time.After(time.Second * 3):
		t.Fatal("missing committed catalog notification")
	}
	var outboxCount int
	require.NoError(t, database.QueryRow(`SELECT count(*) FROM scheduler_outbox WHERE event_type='model_catalog_changed' AND payload->>'model_access'='true'`).Scan(&outboxCount))
	require.Positive(t, outboxCount)
	migration, err := migrations.FS.ReadFile("268_add_model_access_groups.sql")
	require.NoError(t, err)
	for range 2 {
		_, err = database.Exec(string(migration))
		require.NoError(t, err)
	}
	restored, err := readModelAccessSnapshot(ctx, database, keyID)
	require.NoError(t, err)
	require.Equal(t, []string{}, restored.Key)
	current, err = repository.Rule(ctx, "keys", keyID, userID)
	require.NoError(t, err)
	current.AccessGroupID = nil
	current.Models = []string{}
	require.NoError(t, repository.SaveRule(ctx, "keys", keyID, userID, *current))
	accountRule, err := repository.Rule(ctx, "accounts", accountID, 0)
	require.NoError(t, err)
	accountRule.AccessGroupID = nil
	require.NoError(t, repository.SaveRule(ctx, "accounts", accountID, 0, *accountRule))
	require.NoError(t, repository.DeleteGroup(ctx, named.ID, named.Revision))
}
