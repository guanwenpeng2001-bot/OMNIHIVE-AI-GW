package repository

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/internal/testutil/testresources"
	"github.com/stretchr/testify/require"
)

func TestOpenCodeQuotaPostgresRecoveryAndIdentity(t *testing.T) {
	dsn := os.Getenv("OPENCODE_QUOTA_TEST_DSN")
	if dsn == "" {
		t.Skip("OPENCODE_QUOTA_TEST_DSN must name the assigned disposable job_test database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	database, err := testresources.OpenPostgres(ctx, dsn, "job_test")
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })
	require.NoError(t, ApplyMigrations(ctx, database))
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, database)))
	repo := newAccountRepositoryWithSQL(client, database, nil)
	reset := time.Now().UTC().Add(10 * 24 * time.Hour).Truncate(time.Second).Add(123 * time.Millisecond)
	result := &service.CNProviderQuotaProbeResult{StatusCode: 200, Success: true, CredentialValid: true, FetchedAt: time.Now().Unix(),
		Tiers: []service.CNQuotaTier{{Window: "5h"}, {Window: "weekly"}, {Window: "monthly", UsedPercent: 100, ResetAt: reset.Format(time.RFC3339Nano)}}}
	create := func(status string, schedulable bool) *service.Account {
		entity, err := client.Account.Create().SetName("opencode-quota-fixture").SetPlatform(service.PlatformOpenCodeGo).
			SetType(service.AccountTypeAPIKey).SetStatus(status).SetSchedulable(schedulable).
			SetCredentials(map[string]any{"api_key": "old-test-only"}).SetExtra(map[string]any{}).Save(ctx)
		require.NoError(t, err)
		t.Cleanup(func() {
			_, err := database.Exec("DELETE FROM scheduler_outbox WHERE account_id = $1", entity.ID)
			require.NoError(t, err)
			require.NoError(t, client.Account.DeleteOneID(entity.ID).Exec(context.Background()))
		})
		account, err := repo.GetByID(ctx, entity.ID)
		require.NoError(t, err)
		return account
	}
	for _, test := range []struct {
		name, status, want string
		schedulable        bool
	}{
		{"legacy error", service.StatusError, service.StatusActive, true},
		{"manual pause", service.StatusActive, service.StatusActive, false},
		{"inactive", "inactive", "inactive", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			account := create(test.status, test.schedulable)
			require.NoError(t, repo.ApplyOpenCodeQuotaProbe(ctx, account, result))
			current, err := repo.GetByID(ctx, account.ID)
			require.NoError(t, err)
			require.Equal(t, test.want, current.Status)
			require.Equal(t, test.schedulable, current.Schedulable)
			require.Equal(t, float64(100), current.Extra["opencode_go_monthly_used_percent"])
			if test.status != "inactive" {
				require.Nil(t, current.TempUnschedulableUntil)
				available := *result
				available.Tiers = []service.CNQuotaTier{{Window: "5h"}, {Window: "weekly"}, {Window: "monthly"}}
				require.NoError(t, repo.ApplyOpenCodeQuotaProbe(ctx, current, &available))
				current, err = repo.GetByID(ctx, account.ID)
				require.NoError(t, err)
				require.Nil(t, current.TempUnschedulableUntil)
				require.Equal(t, test.schedulable, current.Schedulable)
			}
		})
	}
	t.Run("rollback", func(t *testing.T) {
		account := create(service.StatusError, true)
		tx, err := client.Tx(ctx)
		require.NoError(t, err)
		defer func() { _ = tx.Rollback() }()
		require.NoError(t, repo.ApplyOpenCodeQuotaProbe(dbent.NewTxContext(ctx, tx), account, result))
		require.NoError(t, tx.Rollback())
		current, err := repo.GetByID(ctx, account.ID)
		require.NoError(t, err)
		require.Equal(t, service.StatusError, current.Status)
		require.NotContains(t, current.Extra, "opencode_go_monthly_used_percent")
		require.Nil(t, current.TempUnschedulableUntil)
		var outboxCount int
		require.NoError(t, database.QueryRowContext(ctx, "SELECT count(*) FROM scheduler_outbox WHERE account_id=$1", account.ID).Scan(&outboxCount))
		require.Zero(t, outboxCount)
	})
	t.Run("concurrent replacement rejects old probe", func(t *testing.T) {
		account := create(service.StatusError, true)
		tx, err := client.Tx(ctx)
		require.NoError(t, err)
		defer func() { _ = tx.Rollback() }()
		_, err = tx.Client().Account.UpdateOneID(account.ID).SetCredentials(map[string]any{"api_key": "replacement-test-only"}).Save(ctx)
		require.NoError(t, err)
		done := make(chan error, 1)
		go func() { done <- repo.ApplyOpenCodeQuotaProbe(ctx, account, result) }()
		select {
		case err := <-done:
			t.Fatalf("probe bypassed locked replacement: %v", err)
		case <-time.After(50 * time.Millisecond):
		}
		require.NoError(t, tx.Commit())
		require.ErrorIs(t, <-done, service.ErrAccountRuntimeIdentityChanged)
		current, err := repo.GetByID(ctx, account.ID)
		require.NoError(t, err)
		require.Equal(t, service.StatusError, current.Status)
		require.NotContains(t, current.Extra, "opencode_go_monthly_used_percent")
	})
	t.Run("key update clears persisted quota cooldown", func(t *testing.T) {
		account := create(service.StatusActive, false)
		_, err := client.Account.UpdateOneID(account.ID).SetTempUnschedulableUntil(reset).SetTempUnschedulableReason("cn_quota_exhausted: legacy").SetRateLimitedAt(time.Now()).SetRateLimitResetAt(reset).SetExtra(map[string]any{"opencode_go_monthly_reset_at": reset.Format(time.RFC3339Nano)}).Save(ctx)
		require.NoError(t, err)
		account, err = repo.GetByID(ctx, account.ID)
		require.NoError(t, err)
		account.Name = "same-key presentation update"
		require.NoError(t, repo.Update(ctx, account))
		account, err = repo.GetByID(ctx, account.ID)
		require.NoError(t, err)
		require.NotNil(t, account.TempUnschedulableUntil)
		account.Credentials["api_key"] = "replacement-test-only"
		require.NoError(t, repo.Update(ctx, account))
		current, err := repo.GetByID(ctx, account.ID)
		require.NoError(t, err)
		require.Nil(t, current.TempUnschedulableUntil)
		require.Empty(t, current.TempUnschedulableReason)
		require.False(t, current.Schedulable)
		require.NotContains(t, current.Extra, "opencode_go_monthly_used_percent")
	})

	for _, bulk := range []bool{false, true} {
		for _, manual := range []bool{false, true} {
			t.Run(fmt.Sprintf("identity cooldown invalidation bulk=%t manual=%t", bulk, manual), func(t *testing.T) {
				account := create(service.StatusActive, false)
				reason := "cn_quota_exhausted: old key"
				if manual {
					reason = "operator maintenance"
				}
				_, err := client.Account.UpdateOneID(account.ID).SetTempUnschedulableUntil(reset).SetTempUnschedulableReason(reason).SetRateLimitedAt(time.Now()).SetRateLimitResetAt(reset).SetExtra(map[string]any{
					"opencode_go_monthly_reset_at": reset.Format(time.RFC3339Nano),
					"model_rate_limits": map[string]any{
						"kimi-k3":       map[string]any{"reason": "opencode_payment_required", "rate_limit_reset_at": reset.Format(time.RFC3339Nano)},
						"private-model": map[string]any{"reason": "operator cooldown", "rate_limit_reset_at": reset.Format(time.RFC3339Nano)},
					},
				}).Save(ctx)
				require.NoError(t, err)
				if bulk {
					count, err := repo.BulkUpdate(ctx, []int64{account.ID}, service.AccountBulkUpdate{Credentials: map[string]any{"api_key": "replacement-test-only"}})
					require.NoError(t, err)
					require.EqualValues(t, 1, count)
				} else {
					account, err = repo.GetByID(ctx, account.ID)
					require.NoError(t, err)
					account.Credentials["api_key"] = "replacement-test-only"
					require.NoError(t, repo.Update(ctx, account))
				}
				current, err := repo.GetByID(ctx, account.ID)
				require.NoError(t, err)
				require.False(t, current.Schedulable)
				require.Nil(t, current.RateLimitResetAt)
				if manual {
					require.Equal(t, reason, current.TempUnschedulableReason)
					require.NotNil(t, current.TempUnschedulableUntil)
				} else {
					require.Nil(t, current.TempUnschedulableUntil)
					require.Empty(t, current.TempUnschedulableReason)
				}
				limits := current.Extra["model_rate_limits"].(map[string]any)
				require.NotContains(t, limits, "kimi-k3")
				require.Contains(t, limits, "private-model")
			})
		}
	}
	t.Run("same identity stale probe preserves newer snapshot", func(t *testing.T) {
		account := create(service.StatusActive, true)
		fresh := *result
		fresh.FetchedAt = time.Now().Unix() + 2
		require.NoError(t, repo.ApplyOpenCodeQuotaProbe(ctx, account, &fresh))
		stale := *result
		stale.Tiers = []service.CNQuotaTier{{Window: "monthly", UsedPercent: 0}}
		require.Error(t, repo.ApplyOpenCodeQuotaProbe(ctx, account, &stale))
		stale.FetchedAt = fresh.FetchedAt
		require.Error(t, repo.ApplyOpenCodeQuotaProbe(ctx, account, &stale), "same-second stale start cannot replace a newer observation")
		current, err := repo.GetByID(ctx, account.ID)
		require.NoError(t, err)
		require.Equal(t, float64(100), current.Extra["opencode_go_monthly_used_percent"])
		// Two starts can share both an identity and the persisted second stamp.
		// The row update time still distinguishes the later committed result.
		newer := fresh
		newer.Tiers = []service.CNQuotaTier{{Window: "monthly", UsedPercent: 50}}
		require.NoError(t, repo.ApplyOpenCodeQuotaProbe(ctx, current, &newer))
		require.Error(t, repo.ApplyOpenCodeQuotaProbe(ctx, current, &stale))
		current, err = repo.GetByID(ctx, account.ID)
		require.NoError(t, err)
		require.Equal(t, float64(50), current.Extra["opencode_go_monthly_used_percent"])
	})
	t.Run("model cooldown identity and monotonic retry", func(t *testing.T) {
		account := create(service.StatusActive, true)
		later := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
		require.NoError(t, repo.SetOpenCodeModelQuotaLimit(ctx, account, "kimi-k3", later, "opencode_usage_limit"))
		// The same stale in-memory account cannot shorten a concurrent proven retry.
		require.NoError(t, repo.SetOpenCodeModelQuotaLimit(ctx, account, "kimi-k3", time.Now().Add(20*time.Minute), "opencode_payment_required"))
		current, err := repo.GetByID(ctx, account.ID)
		require.NoError(t, err)
		limits := current.Extra["model_rate_limits"].(map[string]any)
		require.Equal(t, later.Format(time.RFC3339), limits["kimi-k3"].(map[string]any)["rate_limit_reset_at"])
		current.Credentials["api_key"] = "replacement-test-only"
		require.NoError(t, repo.Update(ctx, current))
		require.ErrorIs(t, repo.SetOpenCodeModelQuotaLimit(ctx, account, "kimi-k3", later, "opencode_payment_required"), service.ErrAccountRuntimeIdentityChanged)
	})
	for _, mode := range []string{"bulk", "extra identity revision"} {
		t.Run("CommandCode metadata "+mode, func(t *testing.T) {
			account := create(service.StatusActive, false)
			extra := map[string]any{"configured_option": true}
			for _, suffix := range []string{"usage_supported", "usage_checked_at", "monthlyCredits", "purchasedCredits", "freeCredits", "plan_level", "period_basis", "totalCount", "totalTokens", "totalCredits"} {
				extra["commandcode_"+suffix] = "old observation"
			}
			_, err := client.Account.UpdateOneID(account.ID).SetPlatform(service.PlatformCommandCode).SetExtra(extra).Save(ctx)
			require.NoError(t, err)
			if mode == "bulk" {
				count, err := repo.BulkUpdate(ctx, []int64{account.ID}, service.AccountBulkUpdate{Credentials: map[string]any{"api_key": "replacement-test-only"}})
				require.NoError(t, err)
				require.EqualValues(t, 1, count)
			} else {
				require.NoError(t, repo.UpdateExtra(ctx, account.ID, map[string]any{"credential_secret_revision": 2}))
			}
			current, err := repo.GetByID(ctx, account.ID)
			require.NoError(t, err)
			for key := range extra {
				if key != "configured_option" {
					require.NotContains(t, current.Extra, key)
				}
			}
			require.Equal(t, true, current.Extra["configured_option"])
			require.False(t, current.Schedulable)
		})
	}
}
