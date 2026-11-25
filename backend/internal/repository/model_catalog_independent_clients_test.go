//go:build unit

package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil/testprocess"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil/testresources"
	"github.com/stretchr/testify/require"
)

type catalogProcessRequest struct {
	Operation  string
	AccountID  int64
	Generation int64
}

type catalogProcessResponse struct {
	PID           int
	BackendPID    int
	Database      string
	ServerVersion string
	Baseline      service.AccountModelCatalogBaseline
	Outcome       string
}

func TestModelCatalogIndependentClientsCAS(test *testing.T) {
	ctx := context.Background()
	if os.Getenv("UPSTREAM_JOB_TEST_DSN") == "" {
		test.Skip("requires exclusive job_test")
	}
	database, err := testresources.OpenPostgres(ctx, os.Getenv("UPSTREAM_JOB_TEST_DSN"), "job_test")
	require.NoError(test, err)
	database.SetMaxOpenConns(1)
	test.Cleanup(func() { require.NoError(test, database.Close()) })
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, database)))
	repository := newAccountRepositoryWithSQL(client, database, nil)
	repository.credentialEncryptor = persistentTestAES(test, true)
	if testprocess.Role() != "" {
		var account *service.Account
		testprocess.Serve(test, func(payload json.RawMessage) any {
			var request catalogProcessRequest
			require.NoError(test, json.Unmarshal(payload, &request))
			response := catalogProcessResponse{PID: os.Getpid()}
			require.NoError(test, database.QueryRowContext(ctx, `SELECT pg_backend_pid(), current_database(), version()`).Scan(&response.BackendPID, &response.Database, &response.ServerVersion))
			if request.Operation == "load" {
				account, err = repository.GetByID(ctx, request.AccountID)
				require.NoError(test, err)
				response.Baseline = account.ModelCatalogBaseline()
				return response
			}
			require.Equal(test, "commit", request.Operation)
			require.NotNil(test, account)
			err := repository.CommitAccountModelCatalog(ctx, account.ID, account.ModelCatalogBaseline(), independentCatalogSnapshot(account, request.Generation), account.ModelCatalogState(), &service.UpstreamModelMetadataSnapshot{}, false)
			switch {
			case err == nil:
				response.Outcome = "committed"
			case errors.Is(err, service.ErrAccountModelCatalogConflict):
				response.Outcome = "conflict"
			default:
				response.Outcome = err.Error()
			}
			return response
		})
		return
	}
	var otherConnections int
	require.NoError(test, database.QueryRowContext(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid()`).Scan(&otherConnections))
	require.Zero(test, otherConnections, "job_test must be exclusively leased before fixture migration")
	require.NoError(test, ApplyMigrations(ctx, database))
	first, second := testprocess.Start(test, "catalog-first"), testprocess.Start(test, "catalog-second")
	load := func(participant *testprocess.Client, accountID int64) catalogProcessResponse {
		participant.Send(catalogProcessRequest{Operation: "load", AccountID: accountID})
		var response catalogProcessResponse
		participant.Receive(&response)
		test.Logf("participant pid=%d pg_backend_pid=%d database=%s version=%s baseline=%+v", response.PID, response.BackendPID, response.Database, response.ServerVersion, response.Baseline)
		require.Equal(test, "job_test", response.Database)
		return response
	}
	commit := func(participant *testprocess.Client, generation int64) string {
		participant.Send(catalogProcessRequest{Operation: "commit", Generation: generation})
		var response catalogProcessResponse
		participant.Receive(&response)
		return response.Outcome
	}
	for _, scenario := range []string{"same_baseline", "stale_identity", "stale_rules", "stale_publication", "outbox_rollback"} {
		test.Run(scenario, func(test *testing.T) {
			row := createEncryptedCatalogFixture(test, ctx, client, database, repository, &service.Account{Name: "independent-catalog-" + scenario, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture", "model_mapping": map[string]any{"alias": "target"}}})
			test.Cleanup(func() {
				_, err := database.ExecContext(ctx, `DELETE FROM scheduler_outbox WHERE account_id=$1`, row.ID)
				require.NoError(test, err)
				require.NoError(test, client.Account.DeleteOneID(row.ID).Exec(ctx))
			})
			initial, other := load(first, row.ID), load(second, row.ID)
			require.NotEqual(test, initial.PID, other.PID)
			require.NotEqual(test, os.Getpid(), initial.PID)
			require.NotEqual(test, os.Getpid(), other.PID)
			require.NotEqual(test, initial.BackendPID, other.BackendPID)
			require.Equal(test, initial.Baseline, other.Baseline)
			account, err := repository.GetByID(ctx, row.ID)
			require.NoError(test, err)
			var expected *service.AccountModelCatalogSnapshot
			switch scenario {
			case "same_baseline":
				// Both children acknowledged the exact same baseline before either is released.
				first.Send(catalogProcessRequest{Operation: "commit", Generation: 7})
				second.Send(catalogProcessRequest{Operation: "commit", Generation: 7})
				var left, right catalogProcessResponse
				first.Receive(&left)
				second.Receive(&right)
				require.ElementsMatch(test, []string{"committed", "conflict"}, []string{left.Outcome, right.Outcome})
				expected = independentCatalogSnapshot(account, 7)
			case "stale_identity":
				require.NoError(test, repository.UpdateCredentials(ctx, row.ID, map[string]any{"api_key": "rotated", "model_mapping": account.Credentials["model_mapping"], service.CredentialSecretVersionKey: service.CredentialVersion(account.Credentials)}))
				require.Equal(test, "conflict", commit(first, 7))
				require.Equal(test, "conflict", commit(second, 7))
			case "stale_rules":
				selection, err := service.NewAccountModelSelectionService(repository).SaveModelSelection(ctx, row.ID, service.ModelSelectionCommand{
					ModelMapping: map[string]string{"new-alias": "new-target"},
					Source:       service.ModelSelectionAccountWrite,
				})
				require.NoError(test, err)
				require.Equal(test, initial.Baseline.RulesRevision+1, selection.RulesRevision)
				require.Equal(test, "conflict", commit(first, 7))
				require.Equal(test, "conflict", commit(second, 7))
			case "stale_publication":
				require.Equal(test, "committed", commit(second, 8))
				expected = independentCatalogSnapshot(account, 8)
				load(first, row.ID) // Fresh account CAS, but a process pinned to the older release.
				require.Equal(test, "conflict", commit(first, 7))
			case "outbox_rollback":
				// Fail AFTER notification insertion so both the inserted row and account update must roll back.
				trigger := fmt.Sprintf("independent_catalog_outbox_%d", os.Getpid())
				_, err = database.ExecContext(ctx, fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.account_id=%d THEN RAISE EXCEPTION 'independent catalog outbox failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER %s AFTER INSERT ON scheduler_outbox FOR EACH ROW EXECUTE FUNCTION %s()`, trigger, row.ID, trigger, trigger))
				require.NoError(test, err)
				test.Cleanup(func() {
					_, err := database.ExecContext(ctx, fmt.Sprintf(`DROP TRIGGER %s ON scheduler_outbox; DROP FUNCTION %s()`, trigger, trigger))
					require.NoError(test, err)
				})
				require.Contains(test, commit(first, 7), "independent catalog outbox failure")
				require.Contains(test, commit(second, 7), "independent catalog outbox failure")
				var count int
				require.NoError(test, database.QueryRowContext(ctx, `SELECT count(*) FROM scheduler_outbox WHERE account_id=$1`, row.ID).Scan(&count))
				require.Zero(test, count)
			}
			current, err := repository.GetByID(ctx, row.ID)
			require.NoError(test, err)
			require.Equal(test, expected, current.GetUpstreamModelCatalog())
			if scenario == "same_baseline" || scenario == "stale_publication" || scenario == "outbox_rollback" {
				require.Equal(test, service.CredentialVersion(account.Credentials), service.CredentialVersion(current.Credentials))
				require.Equal(test, account.GetCredential("api_key"), current.GetCredential("api_key"))
			}
			if scenario == "outbox_rollback" {
				require.Equal(test, account.Extra, current.Extra)
			}
		})
	}
}

func independentCatalogSnapshot(account *service.Account, generation int64) *service.AccountModelCatalogSnapshot {
	baseline := account.ModelCatalogBaseline()
	return &service.AccountModelCatalogSnapshot{SchemaVersion: service.AccountModelCatalogSchemaVersion, EnrichmentVersion: "capability-v2", SourceGeneration: generation, SourceVersions: map[string]string{"catalog": fmt.Sprintf("fixture-generation-%d", generation)}, Revision: baseline.SnapshotRevision + 1, IdentityRevision: baseline.IdentityRevision, RulesRevision: baseline.RulesRevision, Complete: true}
}
