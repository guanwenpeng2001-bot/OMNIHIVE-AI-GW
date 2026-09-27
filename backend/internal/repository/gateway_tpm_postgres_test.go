//go:build unit

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/internal/testutil/testresources"
	"github.com/stretchr/testify/require"
)

type tpmPostgresFixture struct {
	database   *sql.DB
	repository *usageSettlementReceiptRepository
	command    service.TPMReservationCommand
}

func newTPMPostgresFixture(t *testing.T) tpmPostgresFixture {
	t.Helper()
	dsn := os.Getenv("UPSTREAM_JOB_TEST_DSN")
	if dsn == "" {
		t.Skip("set UPSTREAM_JOB_TEST_DSN to isolated job_test; PostgreSQL TPM scenarios were not executed")
	}
	ctx := context.Background()
	database, err := testresources.OpenPostgres(ctx, dsn, "job_test")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, database.Close()) })
	require.NoError(t, ApplyMigrations(ctx, database))
	userID := createKeyBudgetTestUser(t, ctx, database, 10)
	keyID := createKeyBudgetTestKey(t, ctx, database, userID, service.APIKeyBudgetPolicy{})
	var workspaceID, groupID, accountID int64
	require.NoError(t, database.QueryRow(`SELECT workspace_id FROM api_keys WHERE id=$1`, keyID).Scan(&workspaceID))
	require.NoError(t, database.QueryRow(`INSERT INTO groups(name,platform,tpm_limit) VALUES($1,'openai',100) RETURNING id`, fmt.Sprintf("tpm-group-%d", keyID)).Scan(&groupID))
	require.NoError(t, database.QueryRow(`INSERT INTO accounts(name,platform,type,tpm_limit) VALUES($1,'openai','apikey',100) RETURNING id`, fmt.Sprintf("tpm-account-%d", keyID)).Scan(&accountID))
	_, err = database.Exec(`UPDATE api_keys SET tpm_limit=100 WHERE id=$1`, keyID)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, cleanupErr := database.Exec(`DELETE FROM gateway_tpm_reservations WHERE api_key_id=$1`, keyID)
		require.NoError(t, cleanupErr)
		_, cleanupErr = database.Exec(`DELETE FROM gateway_tpm_windows WHERE workspace_id=$1 AND ((scope_kind='api_key' AND scope_id=$2) OR (scope_kind='group' AND scope_id=$3) OR (scope_kind='account' AND scope_id=$4))`, workspaceID, keyID, groupID, accountID)
		require.NoError(t, cleanupErr)
		_, cleanupErr = database.Exec(`DELETE FROM groups WHERE id=$1`, groupID)
		require.NoError(t, cleanupErr)
		_, cleanupErr = database.Exec(`DELETE FROM accounts WHERE id=$1`, accountID)
		require.NoError(t, cleanupErr)
	})
	return tpmPostgresFixture{database: database, repository: &usageSettlementReceiptRepository{db: database}, command: service.TPMReservationCommand{
		TPMReservationIdentity: service.TPMReservationIdentity{WorkspaceID: workspaceID, APIKeyID: keyID, ExecutionID: fmt.Sprintf("tpm-%d", keyID), AttemptIndex: 1},
		BillingGroupID:         groupID, AccountID: accountID, Fingerprint: "body-model-account",
		Estimate: service.TPMEstimate{InputBound: 10, OutputBound: 15, Candidates: 1, Verified: true},
	}}
}

func (fixture tpmPostgresFixture) assertCounters(t *testing.T, used, reserved int64) {
	t.Helper()
	rows, err := fixture.database.Query(`SELECT used_tokens,reserved_tokens FROM gateway_tpm_windows WHERE workspace_id=$1 AND ((scope_kind='api_key' AND scope_id=$2) OR (scope_kind='group' AND scope_id=$3) OR (scope_kind='account' AND scope_id=$4))`, fixture.command.WorkspaceID, fixture.command.APIKeyID, fixture.command.BillingGroupID, fixture.command.AccountID)
	require.NoError(t, err)
	defer rows.Close()
	count := 0
	for rows.Next() {
		var actualUsed, actualReserved int64
		require.NoError(t, rows.Scan(&actualUsed, &actualReserved))
		require.Equal(t, used, actualUsed)
		require.Equal(t, reserved, actualReserved)
		count++
	}
	require.NoError(t, rows.Err())
	require.Equal(t, 3, count)
}

func TestTPMReservationPostgres_ConcurrentInstancesRespectEveryScope(t *testing.T) {
	fixture := newTPMPostgresFixture(t)
	const workers = 20
	results := make([]error, workers)
	var pending sync.WaitGroup
	for worker := range workers {
		pending.Add(1)
		go func() {
			defer pending.Done()
			command := fixture.command
			command.ExecutionID = fmt.Sprintf("%s-%d", command.ExecutionID, worker)
			// Separate repository objects use independent pool connections.
			repository := &usageSettlementReceiptRepository{db: fixture.database}
			_, results[worker] = repository.ReserveTPMAttempt(context.Background(), command)
		}()
	}
	pending.Wait()
	admitted := 0
	for _, err := range results {
		if err == nil {
			admitted++
			continue
		}
		var exceeded *service.TPMLimitExceeded
		require.ErrorAs(t, err, &exceeded)
	}
	require.Equal(t, 4, admitted)
	fixture.assertCounters(t, 0, 100)
}

func TestTPMReservationPostgres_RejectionRollsBackEveryScope(t *testing.T) {
	fixture := newTPMPostgresFixture(t)
	_, err := fixture.database.Exec(`UPDATE groups SET tpm_limit=1 WHERE id=$1`, fixture.command.BillingGroupID)
	require.NoError(t, err)
	_, err = fixture.repository.ReserveTPMAttempt(context.Background(), fixture.command)
	var exceeded *service.TPMLimitExceeded
	require.ErrorAs(t, err, &exceeded)
	require.Equal(t, service.TPMScopeGroup, exceeded.Scope)
	var count int
	require.NoError(t, fixture.database.QueryRow(`SELECT count(*) FROM gateway_tpm_reservations WHERE api_key_id=$1`, fixture.command.APIKeyID).Scan(&count))
	require.Zero(t, count)
}

func TestTPMReservationPostgres_DuplicateAttemptIsIdempotent(t *testing.T) {
	fixture := newTPMPostgresFixture(t)
	for range 3 {
		_, err := fixture.repository.ReserveTPMAttempt(context.Background(), fixture.command)
		require.NoError(t, err)
	}
	fixture.assertCounters(t, 0, 25)
}

func TestTPMReservationPostgres_ConflictingFingerprintRejected(t *testing.T) {
	fixture := newTPMPostgresFixture(t)
	_, err := fixture.repository.ReserveTPMAttempt(context.Background(), fixture.command)
	require.NoError(t, err)
	fixture.command.Fingerprint = "different-body"
	_, err = fixture.repository.ReserveTPMAttempt(context.Background(), fixture.command)
	require.ErrorIs(t, err, service.ErrTPMReservationConflict)
	fixture.assertCounters(t, 0, 25)
}

func TestTPMReservationPostgres_PolicyUpdateCannotUseStaleUnlimited(t *testing.T) {
	fixture := newTPMPostgresFixture(t)
	_, err := fixture.database.Exec(`UPDATE api_keys SET tpm_limit=0 WHERE id=$1`, fixture.command.APIKeyID)
	require.NoError(t, err)
	_, err = fixture.database.Exec(`UPDATE api_keys SET tpm_limit=1 WHERE id=$1`, fixture.command.APIKeyID)
	require.NoError(t, err)
	_, err = fixture.repository.ReserveTPMAttempt(context.Background(), fixture.command)
	var exceeded *service.TPMLimitExceeded
	require.ErrorAs(t, err, &exceeded)
	require.Equal(t, service.TPMScopeAPIKey, exceeded.Scope)
}

func TestTPMReservationPostgres_SettlementAndReplayAdjustOnlyOnce(t *testing.T) {
	fixture := newTPMPostgresFixture(t)
	_, err := fixture.repository.ReserveTPMAttempt(context.Background(), fixture.command)
	require.NoError(t, err)
	for range 3 {
		require.NoError(t, fixture.repository.SettleTPMAttempt(context.Background(), fixture.command.TPMReservationIdentity, 13))
	}
	fixture.assertCounters(t, 13, 0)
}

func TestTPMReservationPostgres_PendingReceiptRetainsReservation(t *testing.T) {
	fixture := newTPMPostgresFixture(t)
	_, err := fixture.repository.ReserveTPMAttempt(context.Background(), fixture.command)
	require.NoError(t, err)
	require.NoError(t, fixture.repository.MarkTPMAttemptUnknown(context.Background(), fixture.command.TPMReservationIdentity))
	require.NoError(t, fixture.repository.ReleaseUnsentTPMAttempt(context.Background(), fixture.command.TPMReservationIdentity))
	fixture.assertCounters(t, 0, 25)
}

func TestTPMReservationPostgres_ReleaseAndSettlementRace(t *testing.T) {
	fixture := newTPMPostgresFixture(t)
	_, err := fixture.repository.ReserveTPMAttempt(context.Background(), fixture.command)
	require.NoError(t, err)
	var pending sync.WaitGroup
	results := make([]error, 2)
	pending.Add(2)
	go func() {
		defer pending.Done()
		results[0] = fixture.repository.ReleaseUnsentTPMAttempt(context.Background(), fixture.command.TPMReservationIdentity)
	}()
	go func() {
		defer pending.Done()
		results[1] = fixture.repository.SettleTPMAttempt(context.Background(), fixture.command.TPMReservationIdentity, 13)
	}()
	pending.Wait()
	for _, err := range results {
		require.NoError(t, err)
	}
	fixture.assertCounters(t, 13, 0)
}

func TestTPMReservationPostgres_ExpiryPreservesUnknownConsumption(t *testing.T) {
	fixture := newTPMPostgresFixture(t)
	fixture.command.ExpiresAt = time.Now().Add(-time.Hour)
	_, err := fixture.repository.ReserveTPMAttempt(context.Background(), fixture.command)
	require.NoError(t, err)
	require.NoError(t, fixture.repository.MarkTPMAttemptUnknown(context.Background(), fixture.command.TPMReservationIdentity))
	var safeExpiry bool
	require.NoError(t, fixture.database.QueryRow(`SELECT bool_and(expires_at > window_start + interval '60 seconds') FROM gateway_tpm_reservations WHERE api_key_id=$1`, fixture.command.APIKeyID).Scan(&safeExpiry))
	require.True(t, safeExpiry)
	fixture.assertCounters(t, 0, 25)
}

func TestTPMReservationPostgres_ActualAboveEstimateRecordsOverrun(t *testing.T) {
	fixture := newTPMPostgresFixture(t)
	_, err := fixture.repository.ReserveTPMAttempt(context.Background(), fixture.command)
	require.NoError(t, err)
	require.NoError(t, fixture.repository.SettleTPMAttempt(context.Background(), fixture.command.TPMReservationIdentity, 125))
	fixture.assertCounters(t, 125, 0)
	fixture.command.AttemptIndex++
	_, err = fixture.repository.ReserveTPMAttempt(context.Background(), fixture.command)
	var exceeded *service.TPMLimitExceeded
	require.ErrorAs(t, err, &exceeded)
}

func TestTPMReservationPostgres_MinuteBoundaryAndLateUsage(t *testing.T) {
	fixture := newTPMPostgresFixture(t)
	reservation, err := fixture.repository.ReserveTPMAttempt(context.Background(), fixture.command)
	require.NoError(t, err)
	var databaseNow time.Time
	require.NoError(t, fixture.database.QueryRow(`SELECT clock_timestamp()`).Scan(&databaseNow))
	require.WithinDuration(t, databaseNow.UTC().Truncate(time.Minute), reservation.WindowStart, time.Minute)
	// Move this fixture's frozen window into history instead of sleeping across
	// a real minute. Settlement must still address precisely that old window.
	oldWindow := reservation.WindowStart.Add(-2 * time.Minute)
	_, err = fixture.database.Exec(`UPDATE gateway_tpm_windows SET window_start=$5,window_end=$5+interval '60 seconds'
WHERE workspace_id=$1 AND ((scope_kind='api_key' AND scope_id=$2) OR (scope_kind='group' AND scope_id=$3) OR (scope_kind='account' AND scope_id=$4))`, fixture.command.WorkspaceID, fixture.command.APIKeyID, fixture.command.BillingGroupID, fixture.command.AccountID, oldWindow)
	require.NoError(t, err)
	_, err = fixture.database.Exec(`UPDATE gateway_tpm_reservations SET window_start=$2 WHERE api_key_id=$1`, fixture.command.APIKeyID, oldWindow)
	require.NoError(t, err)
	require.NoError(t, fixture.repository.SettleTPMAttempt(context.Background(), fixture.command.TPMReservationIdentity, 17))
	fixture.assertCounters(t, 17, 0)
	var newest time.Time
	require.NoError(t, fixture.database.QueryRow(`SELECT max(window_start) FROM gateway_tpm_windows WHERE scope_kind='api_key' AND scope_id=$1`, fixture.command.APIKeyID).Scan(&newest))
	require.True(t, oldWindow.Equal(newest))
}

func TestTPMReservationPostgres_SharedScopesAcrossKeysAndGroups(t *testing.T) {
	for _, sharedScope := range []service.TPMScope{service.TPMScopeGroup, service.TPMScopeAccount} {
		t.Run(string(sharedScope), func(t *testing.T) {
			fixture := newTPMPostgresFixture(t)
			command := fixture.command
			command.Estimate.InputBound, command.Estimate.OutputBound = 60, 0
			_, err := fixture.repository.ReserveTPMAttempt(context.Background(), command)
			require.NoError(t, err)
			otherUser := createKeyBudgetTestUser(t, context.Background(), fixture.database, 10)
			otherKey := createKeyBudgetTestKey(t, context.Background(), fixture.database, otherUser, service.APIKeyBudgetPolicy{})
			command.APIKeyID = otherKey
			command.ExecutionID = fmt.Sprintf("other-owner-%d", otherKey)
			if sharedScope == service.TPMScopeAccount {
				require.NoError(t, fixture.database.QueryRow(`INSERT INTO groups(name,platform,tpm_limit) VALUES($1,'openai',100) RETURNING id`, fmt.Sprintf("other-group-%d", otherKey)).Scan(&command.BillingGroupID))
				t.Cleanup(func() {
					_, cleanupErr := fixture.database.Exec(`DELETE FROM groups WHERE id=$1`, command.BillingGroupID)
					require.NoError(t, cleanupErr)
				})
			}
			_, err = fixture.repository.ReserveTPMAttempt(context.Background(), command)
			var exceeded *service.TPMLimitExceeded
			require.ErrorAs(t, err, &exceeded)
			require.Equal(t, sharedScope, exceeded.Scope)
			var reservations int
			require.NoError(t, fixture.database.QueryRow(`SELECT count(*) FROM gateway_tpm_reservations WHERE api_key_id=$1`, otherKey).Scan(&reservations))
			require.Zero(t, reservations)
		})
	}
}
