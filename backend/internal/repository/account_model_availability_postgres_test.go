//go:build unit

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/google/uuid"
	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil/testresources"
	"github.com/stretchr/testify/require"
)

func TestModelProbeBudgetPostgres(test *testing.T) {
	dsn := os.Getenv("UPSTREAM_JOB_TEST_DSN")
	if dsn == "" {
		test.Skip("set UPSTREAM_JOB_TEST_DSN to isolated job_test")
	}
	ctx := context.Background()
	database, err := testresources.OpenPostgres(ctx, dsn, "job_test")
	require.NoError(test, err)
	test.Cleanup(func() { _ = database.Close() })
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, database)))
	repository := newAccountRepositoryWithSQL(client, database, nil)
	repository.credentialEncryptor = persistentTestAES(test, true)
	if rawID := os.Getenv("MODEL_PROBE_RESTART_ACCOUNT_ID"); rawID != "" {
		accountID, err := strconv.ParseInt(rawID, 10, 64)
		require.NoError(test, err)
		account, err := repository.GetByID(ctx, accountID)
		require.NoError(test, err)
		_, err = repository.ReserveModelProbe(ctx, accountID, account.ModelCatalogBaseline(), "private", "chat_completions")
		require.ErrorIs(test, err, service.ErrModelProbeBudgetExceeded)
		return
	}
	require.NoError(test, ApplyMigrations(ctx, database))
	otherDatabase, err := testresources.OpenPostgres(ctx, dsn, "job_test")
	require.NoError(test, err)
	test.Cleanup(func() { _ = otherDatabase.Close() })
	other := newAccountRepositoryWithSQL(dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, otherDatabase))), otherDatabase, nil)
	other.credentialEncryptor = repository.credentialEncryptor
	var databaseNow time.Time
	require.NoError(test, database.QueryRowContext(ctx, "SELECT clock_timestamp()").Scan(&databaseNow))
	databaseNow = databaseNow.UTC()
	create := func(test *testing.T) *service.Account {
		row := &service.Account{Name: "model-availability-" + uuid.NewString(), Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Credentials: map[string]any{"api_key": "isolated-fixture", "model_mapping": map[string]string{"alias": "private", "other": "other-private"}}}
		err := repository.Create(ctx, row)
		require.NoError(test, err)
		encoded, err := json.Marshal(service.AccountModelCatalogSnapshot{Revision: 1, Complete: true, ObservedAt: databaseNow.Format(time.RFC3339Nano), DiscoveredModels: []string{"native"}})
		require.NoError(test, err)
		_, err = database.ExecContext(ctx, `UPDATE accounts SET extra=extra||jsonb_build_object('upstream_model_catalog',$2::jsonb) WHERE id=$1`, row.ID, encoded)
		require.NoError(test, err)
		_, err = database.ExecContext(ctx, `DELETE FROM audit_logs WHERE extra->>'account_id'=$1`, row.ID)
		require.NoError(test, err)
		_, err = database.ExecContext(ctx, `DELETE FROM scheduler_outbox WHERE account_id=$1`, row.ID)
		require.NoError(test, err)
		test.Cleanup(func() {
			_, err := database.ExecContext(ctx, `DELETE FROM scheduler_outbox WHERE account_id=$1`, row.ID)
			require.NoError(test, err)
			_, err = database.ExecContext(ctx, `DELETE FROM credential_secrets WHERE account_id=$1`, row.ID)
			require.NoError(test, err)
			_, err = database.ExecContext(ctx, `DELETE FROM audit_logs WHERE extra->>'account_id'=$1`, strconv.FormatInt(row.ID, 10))
			require.NoError(test, err)
			_, err = database.ExecContext(ctx, `DELETE FROM accounts WHERE id=$1`, row.ID)
			require.NoError(test, err)
		})
		account, err := repository.GetByID(ctx, row.ID)
		require.NoError(test, err)
		return account
	}
	get := func(test *testing.T, id int64) *service.Account {
		account, err := repository.GetByID(ctx, id)
		require.NoError(test, err)
		return account
	}
	seedBudget := func(test *testing.T, id int64, budget service.ModelProbeBudget) {
		encoded, err := json.Marshal(budget)
		require.NoError(test, err)
		_, err = database.ExecContext(ctx, `UPDATE accounts SET extra=extra||jsonb_build_object('model_probe_budget',$2::jsonb) WHERE id=$1`, id, string(encoded))
		require.NoError(test, err)
	}
	change := func(account *service.Account) service.ModelAvailabilityChange {
		return service.ModelAvailabilityChange{AccountModelCatalogBaseline: account.ModelCatalogBaseline(), Model: "private", Endpoint: "chat_completions", Reason: "isolated deployment fixture", Confirmed: true}
	}
	reserve := func(account *service.Account, endpoint string) (*service.ModelProbeAttempt, error) {
		return repository.ReserveModelProbe(ctx, account.ID, account.ModelCatalogBaseline(), "private", endpoint)
	}

	test.Run("concurrent_reservations_claim_once_and_process_restart", func(test *testing.T) {
		account := create(test)
		var wait sync.WaitGroup
		attempts := make(chan *service.ModelProbeAttempt, 12)
		failures := make(chan error, 12)
		for index := 0; index < 12; index++ {
			wait.Add(1)
			go func(index int) {
				defer wait.Done()
				writer := repository
				if index%2 == 1 {
					writer = other
				}
				attempt, err := writer.ReserveModelProbe(ctx, account.ID, account.ModelCatalogBaseline(), "private", "chat_completions")
				if err != nil {
					failures <- err
				} else {
					attempts <- attempt
				}
			}(index)
		}
		wait.Wait()
		close(attempts)
		close(failures)
		require.Len(test, attempts, 5)
		require.Len(test, failures, 7)
		for err := range failures {
			require.ErrorIs(test, err, service.ErrModelProbeBudgetExceeded)
		}
		attempt := <-attempts
		claims := make(chan error, 12)
		for index := 0; index < 12; index++ {
			wait.Add(1)
			go func() { defer wait.Done(); _, err := other.ClaimModelProbe(ctx, account.ID, attempt.ID); claims <- err }()
		}
		wait.Wait()
		close(claims)
		winners := 0
		for err := range claims {
			if err == nil {
				winners++
			} else {
				require.ErrorIs(test, err, service.ErrModelProbeAttemptClaimed)
			}
		}
		require.Equal(test, 1, winners)
		executable, err := os.Executable()
		require.NoError(test, err)
		command := exec.Command(executable, "-test.run=^TestModelProbeBudgetPostgres$", "-test.v")
		command.Env = append(os.Environ(), "MODEL_PROBE_RESTART_ACCOUNT_ID="+strconv.FormatInt(account.ID, 10))
		output, err := command.CombinedOutput()
		require.NoError(test, err, string(output))
		require.Contains(test, string(output), "PASS")
		budget, err := decodeModelProbeBudget(get(test, account.ID))
		require.NoError(test, err)
		require.Equal(test, 5, budget.DailyUsed)
		require.Equal(test, 5, budget.MonthlyUsed)
		require.EqualValues(test, 6, budget.Revision)
	})

	test.Run("monthly_media_limits_and_utc_rollover", func(test *testing.T) {
		account := create(test)
		budget := service.ModelProbeBudget{Revision: 49, Day: databaseNow.Format("2006-01-02"), Month: databaseNow.Format("2006-01"), MonthlyUsed: 49}
		seedBudget(test, account.ID, budget)
		_, err := reserve(account, "chat_completions")
		require.NoError(test, err)
		_, err = reserve(account, "chat_completions")
		require.ErrorIs(test, err, service.ErrModelProbeBudgetExceeded)
		budget.MonthlyUsed, budget.MediaMonthlyUsed = 7, 7
		seedBudget(test, account.ID, budget)
		_, err = reserve(account, "images")
		require.NoError(test, err)
		_, err = reserve(account, "videos")
		require.ErrorIs(test, err, service.ErrModelProbeBudgetExceeded)
		persisted, err := decodeModelProbeBudget(get(test, account.ID))
		require.NoError(test, err)
		require.Equal(test, 8, persisted.MonthlyUsed)
		require.Equal(test, 8, persisted.MediaMonthlyUsed)
		persisted.Day = databaseNow.AddDate(0, 0, -1).Format("2006-01-02")
		seedBudget(test, account.ID, persisted)
		_, err = reserve(account, "images")
		require.ErrorIs(test, err, service.ErrModelProbeBudgetExceeded, "new day does not replenish monthly media")
		persisted.Month, persisted.Day = databaseNow.AddDate(0, -1, -1).Format("2006-01"), databaseNow.AddDate(0, -1, -1).Format("2006-01-02")
		seedBudget(test, account.ID, persisted)
		_, err = reserve(account, "videos")
		require.NoError(test, err)
		persisted, err = decodeModelProbeBudget(get(test, account.ID))
		require.NoError(test, err)
		require.Equal(test, 1, persisted.MonthlyUsed)
		require.Equal(test, 1, persisted.MediaMonthlyUsed)
		require.Len(test, persisted.Attempts, 1)
	})

	test.Run("generic_writes_cannot_forge_or_refund", func(test *testing.T) {
		account := create(test)
		require.NoError(test, repository.ChangeModelAvailability(ctx, account.ID, change(account), 7, false))
		account = get(test, account.ID)
		_, err := reserve(account, "chat_completions")
		require.NoError(test, err)
		before := get(test, account.ID)
		forged := map[string]any{service.ModelProbeBudgetExtraKey: map[string]any{}, service.ModelAvailabilityExtraKey: []any{}}
		require.NoError(test, repository.UpdateExtra(ctx, account.ID, forged))
		_, err = repository.BulkUpdate(ctx, []int64{account.ID}, service.AccountBulkUpdate{Extra: forged})
		require.NoError(test, err)
		stale := *account
		stale.Extra = forged
		require.NoError(test, repository.Update(ctx, &stale))
		stale.Extra = forged
		require.NoError(test, repository.UpdateWithAccountBillingSettings(ctx, &stale, nil, nil, nil))
		after := get(test, account.ID)
		require.Equal(test, before.Extra[service.ModelProbeBudgetExtraKey], after.Extra[service.ModelProbeBudgetExtraKey])
		require.Equal(test, before.Extra[service.ModelAvailabilityExtraKey], after.Extra[service.ModelAvailabilityExtraKey])
		after.Credentials["api_key"] = "replacement-fixture"
		require.NoError(test, repository.Update(ctx, after))
		after = get(test, account.ID)
		require.Equal(test, before.Extra[service.ModelProbeBudgetExtraKey], after.Extra[service.ModelProbeBudgetExtraKey])
		require.False(test, after.ModelAvailabilityDeclarations()[0].ValidFor(after, nil, time.Now()))
		copyAccount := *before
		copyAccount.ID, copyAccount.Name = 0, "model-availability-copy-"+uuid.NewString()
		copyAccount.Credentials = map[string]any{"api_key": "copied-fixture", "model_mapping": map[string]string{"alias": "private", "other": "other-private"}}
		require.NoError(test, repository.Create(ctx, &copyAccount))
		test.Cleanup(func() {
			_, err := database.ExecContext(ctx, "DELETE FROM scheduler_outbox WHERE account_id=$1", copyAccount.ID)
			require.NoError(test, err)
			_, err = database.ExecContext(ctx, "DELETE FROM credential_secrets WHERE account_id=$1", copyAccount.ID)
			require.NoError(test, err)
			_, err = database.ExecContext(ctx, "DELETE FROM accounts WHERE id=$1", copyAccount.ID)
			require.NoError(test, err)
		})
		copyAccount = *get(test, copyAccount.ID)
		require.NotContains(test, copyAccount.Extra, service.ModelAvailabilityExtraKey)
		require.NotContains(test, copyAccount.Extra, service.ModelProbeBudgetExtraKey)
	})

	test.Run("discovery_and_attempt_boundaries", func(test *testing.T) {
		account := create(test)
		attempt, err := reserve(account, "chat_completions")
		require.NoError(test, err)
		budget, err := decodeModelProbeBudget(get(test, account.ID))
		require.NoError(test, err)
		budget.Attempts[0].ReservedAt = databaseNow.AddDate(0, 0, -1)
		seedBudget(test, account.ID, budget)
		_, err = repository.ClaimModelProbe(ctx, account.ID, attempt.ID)
		require.ErrorIs(test, err, service.ErrModelProbeAttemptClaimed, "a prior UTC day's reservation cannot increase today's outbound budget")
		otherAccount := create(test)
		_, err = repository.ClaimModelProbe(ctx, otherAccount.ID, attempt.ID)
		require.ErrorIs(test, err, service.ErrModelProbeAttemptClaimed)
		_, err = repository.ReserveModelProbe(ctx, account.ID, account.ModelCatalogBaseline(), "not-mapped", "chat_completions")
		require.ErrorIs(test, err, service.ErrModelAvailabilityInvalid)
		for _, result := range []string{"failed", "credentials_invalid"} {
			_, err = database.Exec(`UPDATE accounts SET extra=extra||jsonb_build_object('upstream_model_catalog_state',jsonb_build_object('result',$2::text)) WHERE id=$1`, account.ID, result)
			require.NoError(test, err)
			_, err = reserve(get(test, account.ID), "chat_completions")
			require.ErrorIs(test, err, service.ErrModelAvailabilityInvalid)
		}
	})

	test.Run("shadow_parent_identity_cannot_be_reused", func(test *testing.T) {
		parent, child := create(test), create(test)
		_, err := database.Exec(`UPDATE accounts SET type='oauth' WHERE id=$1`, parent.ID)
		require.NoError(test, err)
		_, err = database.Exec(`UPDATE accounts SET parent_account_id=$2, quota_dimension='spark', extra=jsonb_set(extra,'{upstream_model_catalog,credential_account_id}',to_jsonb($2::bigint)) WHERE id=$1`, child.ID, parent.ID)
		require.NoError(test, err)
		child = get(test, child.ID)
		attempt, err := reserve(child, "chat_completions")
		require.NoError(test, err)
		require.Equal(test, parent.ID, attempt.CredentialAccountID)
		_, err = repository.ClaimModelProbe(ctx, child.ID, attempt.ID)
		require.NoError(test, err)
		require.NoError(test, repository.CompleteModelProbe(ctx, child.ID, attempt.ID, service.ModelProbeSucceeded))
		child = get(test, child.ID)
		require.True(test, child.ModelAvailabilityDeclarations()[0].ValidFor(child, get(test, parent.ID), time.Now()))
		attempt, err = reserve(child, "chat_completions")
		require.NoError(test, err)
		_, err = database.Exec(`UPDATE accounts SET extra=extra||'{"upstream_model_catalog_state":{"identity_revision":1}}'::jsonb WHERE id=$1`, parent.ID)
		require.NoError(test, err)
		_, err = repository.ClaimModelProbe(ctx, child.ID, attempt.ID)
		require.ErrorIs(test, err, service.ErrAccountModelCatalogConflict)
		require.False(test, child.ModelAvailabilityDeclarations()[0].ValidFor(child, get(test, parent.ID), time.Now()))
		budget, err := decodeModelProbeBudget(get(test, child.ID))
		require.NoError(test, err)
		require.Equal(test, 2, budget.MonthlyUsed)
	})

	test.Run("declaration_revoke_cas_projection_and_audit", func(test *testing.T) {
		account := create(test)
		request := change(account)
		require.NoError(test, repository.ChangeModelAvailability(ctx, account.ID, request, 7, false))
		require.ErrorIs(test, other.ChangeModelAvailability(ctx, account.ID, request, 8, false), service.ErrAccountModelCatalogConflict)
		account = get(test, account.ID)
		require.True(test, account.ModelAvailabilityDeclarations()[0].ValidFor(account, nil, time.Now()))
		projected, err := repository.GetModelCatalogAccount(ctx, account.ID)
		require.NoError(test, err)
		require.True(test, service.AccountSupportsResolvedModel(ctx, projected, "alias", service.AccountModelBindingOptions{Endpoint: "chat_completions"}))
		require.Equal(test, []string{"native"}, projected.GetUpstreamModelCatalog().DiscoveredModels)
		require.NoError(test, repository.ChangeModelAvailability(ctx, account.ID, change(account), 7, true))
		account = get(test, account.ID)
		require.False(test, account.ModelAvailabilityDeclarations()[0].ValidFor(account, nil, time.Now()))
		var auditCount, outboxCount int
		require.NoError(test, database.QueryRow(`SELECT count(*) FROM audit_logs WHERE extra->>'account_id'=$1`, strconv.FormatInt(account.ID, 10)).Scan(&auditCount))
		require.NoError(test, database.QueryRow(`SELECT count(*) FROM scheduler_outbox WHERE account_id=$1`, account.ID).Scan(&outboxCount))
		require.Equal(test, 2, auditCount)
		require.Equal(test, 1, outboxCount, "existing outbox deduplicates an unconsumed account change")
	})

	test.Run("outbox_failure_rolls_back_declaration_and_audit", func(test *testing.T) {
		account := create(test)
		name := "model_availability_" + strconv.FormatInt(account.ID, 10)
		_, err := database.Exec(`CREATE FUNCTION ` + name + `() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'availability outbox fixture failure'; END $$`)
		require.NoError(test, err)
		test.Cleanup(func() {
			_, err := database.Exec(`DROP TRIGGER IF EXISTS ` + name + ` ON scheduler_outbox`)
			require.NoError(test, err)
			_, err = database.Exec(`DROP FUNCTION IF EXISTS ` + name + `() `)
			require.NoError(test, err)
		})
		_, err = database.Exec(fmt.Sprintf(`CREATE TRIGGER %s BEFORE INSERT ON scheduler_outbox FOR EACH ROW WHEN (NEW.account_id=%d) EXECUTE FUNCTION %s()`, name, account.ID, name))
		require.NoError(test, err)
		require.Error(test, repository.ChangeModelAvailability(ctx, account.ID, change(account), 7, false))
		require.Empty(test, get(test, account.ID).ModelAvailabilityDeclarations())
		var count int
		require.NoError(test, database.QueryRow(`SELECT count(*) FROM audit_logs WHERE extra->>'account_id'=$1`, strconv.FormatInt(account.ID, 10)).Scan(&count))
		require.Zero(test, count)
	})

	test.Run("probe_results_preserve_temporary_evidence_and_reject_late_identity", func(test *testing.T) {
		account := create(test)
		attempt, err := reserve(account, "chat_completions")
		require.NoError(test, err)
		_, err = repository.ClaimModelProbe(ctx, account.ID, attempt.ID)
		require.NoError(test, err)
		require.NoError(test, repository.CompleteModelProbe(ctx, account.ID, attempt.ID, service.ModelProbeSucceeded))
		account = get(test, account.ID)
		proof := account.ModelAvailabilityDeclarations()[0]
		require.Equal(test, service.ModelEvidenceProbe, proof.EvidenceKind)
		require.NotNil(test, proof.ExpiresAt)
		attempt, err = reserve(account, "chat_completions")
		require.NoError(test, err)
		_, err = repository.ClaimModelProbe(ctx, account.ID, attempt.ID)
		require.NoError(test, err)
		require.NoError(test, repository.CompleteModelProbe(ctx, account.ID, attempt.ID, service.ModelProbeTemporary))
		account = get(test, account.ID)
		require.Equal(test, proof, account.ModelAvailabilityDeclarations()[0])
		require.ErrorIs(test, repository.CompleteModelProbe(ctx, account.ID, attempt.ID, service.ModelProbeSucceeded), service.ErrModelProbeAttemptClaimed)
		attempt, err = reserve(account, "chat_completions")
		require.NoError(test, err)
		_, err = repository.ClaimModelProbe(ctx, account.ID, attempt.ID)
		require.NoError(test, err)
		require.NoError(test, repository.CompleteModelProbe(ctx, account.ID, attempt.ID, service.ModelProbeMissing))
		account = get(test, account.ID)
		require.NotNil(test, account.ModelAvailabilityDeclarations()[0].RevokedAt)
		attempt, err = reserve(account, "chat_completions")
		require.NoError(test, err)
		_, err = repository.ClaimModelProbe(ctx, account.ID, attempt.ID)
		require.NoError(test, err)
		account.Credentials["api_key"] = "different-fixture"
		require.NoError(test, repository.Update(ctx, account))
		require.NoError(test, repository.CompleteModelProbe(ctx, account.ID, attempt.ID, service.ModelProbeSucceeded))
		account = get(test, account.ID)
		require.False(test, account.ModelAvailabilityDeclarations()[0].ValidFor(account, nil, time.Now()))
		budget, err := decodeModelProbeBudget(account)
		require.NoError(test, err)
		require.Equal(test, 4, budget.MonthlyUsed)
	})
}
