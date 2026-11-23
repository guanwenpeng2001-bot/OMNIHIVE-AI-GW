//go:build unit

package repository

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil/testresources"
	"github.com/stretchr/testify/require"
)

// Uses a disposable schema in the pre-existing isolated database, never business data.
func TestModelPlazaConfirmationPostgres(t *testing.T) {
	dsn := os.Getenv("UPSTREAM_JOB_TEST_DSN")
	if dsn == "" {
		t.Skip("requires isolated job_test lease")
	}
	ctx := context.Background()
	database, err := testresources.OpenPostgres(ctx, dsn, "job_test")
	require.NoError(t, err)
	defer func() { require.NoError(t, database.Close()) }()
	schema := "plaza_confirmation_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = database.ExecContext(ctx, `CREATE SCHEMA `+schema)
	require.NoError(t, err)
	defer func() {
		_, cleanupErr := database.ExecContext(ctx, `DROP SCHEMA `+schema+` CASCADE`)
		require.NoError(t, cleanupErr)
	}()
	for _, table := range []string{"channels", "channel_groups", "channel_model_pricing", "channel_pricing_intervals", "channel_account_stats_pricing_rules", "groups", "accounts", "account_groups", "audit_logs"} {
		_, err = database.ExecContext(ctx, `CREATE TABLE `+schema+`.`+table+` (LIKE public.`+table+` INCLUDING DEFAULTS INCLUDING CONSTRAINTS)`)
		require.NoError(t, err)
	}
	_, err = database.ExecContext(ctx, `CREATE SEQUENCE `+schema+`.audit_log_id`)
	require.NoError(t, err)
	_, err = database.ExecContext(ctx, `ALTER TABLE `+schema+`.audit_logs ALTER COLUMN id SET DEFAULT nextval('`+schema+`.audit_log_id')`)
	require.NoError(t, err)
	parsed, err := url.Parse(dsn)
	require.NoError(t, err)
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	isolated, err := sql.Open("postgres", parsed.String())
	require.NoError(t, err)
	defer func() { require.NoError(t, isolated.Close()) }()
	statements := []string{
		`INSERT INTO channels (id,workspace_id,name) VALUES (1,17,'plaza-test')`,
		`INSERT INTO groups (id,workspace_id,name,platform) VALUES (1,17,'plaza-group','openai')`,
		`INSERT INTO channel_groups (id,channel_id,group_id) VALUES (1,1,1)`,
		`INSERT INTO accounts (id,workspace_id,name,platform,type,credentials) VALUES (1,17,'plaza-account','openai','apikey','{"model_mapping":{"alias":"candidate"}}')`,
		`INSERT INTO account_groups (account_id,group_id) VALUES (1,1)`,
	}
	for _, statement := range statements {
		_, err = isolated.ExecContext(ctx, statement)
		require.NoError(t, err)
	}
	repository := &channelRepository{db: isolated}
	view, err := repository.PlazaCandidates(ctx, 1, 1)
	require.NoError(t, err)
	require.Equal(t, []service.PlazaModelIdentity{{Platform: "openai", Name: "candidate"}}, view.Candidates)
	input := service.PlazaConfirmationInput{GroupID: 1, Revision: view.Revision, Models: view.Candidates}
	require.NoError(t, repository.ConfirmPlazaModels(ctx, 1, input, 7))
	confirmed, err := repository.PlazaCandidates(ctx, 1, 1)
	require.NoError(t, err)
	require.Len(t, confirmed.Selected, 1)
	require.Equal(t, int64(7), confirmed.Selected[0].ConfirmedBy)
	require.NotEmpty(t, confirmed.Selected[0].ConfirmedAt)
	var auditCount int
	require.NoError(t, isolated.QueryRowContext(ctx, `SELECT count(*) FROM audit_logs WHERE action='channel.plaza.confirm' AND actor_user_id=7`).Scan(&auditCount))
	require.Equal(t, 1, auditCount)
	require.ErrorIs(t, repository.ConfirmPlazaModels(ctx, 1, input, 7), service.ErrPlazaConfirmationConflict)

	// Fail the audit insertion inside this private schema, after the configuration UPDATE.
	_, err = isolated.ExecContext(ctx, `ALTER TABLE audit_logs ADD CONSTRAINT reject_plaza_test CHECK (action <> 'channel.plaza.confirm') NOT VALID`)
	require.NoError(t, err)
	input.Revision = confirmed.Revision
	input.Models = []service.PlazaModelIdentity{}
	require.Error(t, repository.ConfirmPlazaModels(ctx, 1, input, 7))
	retained, err := repository.PlazaCandidates(ctx, 1, 1)
	require.NoError(t, err)
	require.Equal(t, confirmed, retained)
	require.NoError(t, isolated.QueryRowContext(ctx, `SELECT count(*) FROM audit_logs`).Scan(&auditCount))
	require.Equal(t, 1, auditCount)
	_, err = isolated.ExecContext(ctx, `ALTER TABLE audit_logs DROP CONSTRAINT reject_plaza_test`)
	require.NoError(t, err)

	// Two independent clients cannot both commit the same reviewed revision.
	independent, err := sql.Open("postgres", parsed.String())
	require.NoError(t, err)
	defer func() { require.NoError(t, independent.Close()) }()
	results := make(chan error, 2)
	for _, client := range []*sql.DB{isolated, independent} {
		go func(connection *sql.DB) {
			results <- (&channelRepository{db: connection}).ConfirmPlazaModels(ctx, 1, input, 7)
		}(client)
	}
	first, second := <-results, <-results
	if first == nil {
		require.ErrorIs(t, second, service.ErrPlazaConfirmationConflict)
	} else {
		require.ErrorIs(t, first, service.ErrPlazaConfirmationConflict)
		require.NoError(t, second)
	}
	require.NoError(t, isolated.QueryRowContext(ctx, `SELECT count(*) FROM audit_logs`).Scan(&auditCount))
	require.Equal(t, 2, auditCount)
}
