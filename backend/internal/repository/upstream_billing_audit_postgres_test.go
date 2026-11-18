//go:build unit

package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// This suite only accepts its dedicated disposable database; never the business DB.
func TestUpstreamBillingAuditPostgres(t *testing.T) {
	dsn := os.Getenv("UPSTREAM_BILLING_AUDIT_DSN")
	if dsn == "" {
		t.Skip("requires isolated billing_audit_test PostgreSQL")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(12)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var database string
	require.NoError(t, db.QueryRowContext(ctx, "SELECT current_database()").Scan(&database))
	require.Equal(t, "billing_audit_test", database)
	require.NoError(t, ApplyMigrations(ctx, db))
	// A second pass verifies stored migration checksums with the unchanged ledger.
	require.NoError(t, ApplyMigrations(ctx, db))
	var group, user1, user2 int64
	suffix := fmt.Sprint(time.Now().UnixNano())
	require.NoError(t, db.QueryRowContext(ctx, "INSERT INTO groups(name,platform) VALUES($1,'openai') RETURNING id", "billing-audit-"+suffix).Scan(&group))
	require.NoError(t, db.QueryRowContext(ctx, "INSERT INTO users(email,password_hash) VALUES($1,'synthetic') RETURNING id", "one-"+suffix+"@audit.test").Scan(&user1))
	require.NoError(t, db.QueryRowContext(ctx, "INSERT INTO users(email,password_hash) VALUES($1,'synthetic') RETURNING id", "two-"+suffix+"@audit.test").Scan(&user2))
	repo := NewUserGroupRateRepository(db)
	reset := func() {
		_, err := db.ExecContext(ctx, "DELETE FROM user_group_rate_multipliers WHERE group_id=$1", group)
		require.NoError(t, err)
		_, err = db.ExecContext(ctx, "INSERT INTO user_group_rate_multipliers(user_id,group_id,rate_multiplier,rpm_override) VALUES($1,$3,1.25,8),($2,$3,2.5,9)", user1, user2, group)
		require.NoError(t, err)
	}
	check := func(user int64, rate float64, rpm int) {
		t.Helper()
		got, err := repo.GetByUserAndGroup(ctx, user, group)
		require.NoError(t, err)
		require.NotNil(t, got)
		require.Equal(t, rate, *got)
		gotRPM, err := repo.GetRPMOverrideByUserAndGroup(ctx, user, group)
		require.NoError(t, err)
		require.NotNil(t, gotRPM)
		require.Equal(t, rpm, *gotRPM)
	}
	t.Run("rate insert FK failure rolls back clear and delete", func(t *testing.T) {
		reset()
		err := repo.SyncGroupRateMultipliers(ctx, group, []service.GroupRateMultiplierInput{{UserID: -999, RateMultiplier: 3}})
		require.Error(t, err)
		check(user1, 1.25, 8)
		check(user2, 2.5, 9)
	})
	t.Run("RPM insert FK failure rolls back clear and delete", func(t *testing.T) {
		reset()
		v := 3
		err := repo.SyncGroupRPMOverrides(ctx, group, []service.GroupRPMOverrideInput{{UserID: -999, RPMOverride: &v}})
		require.Error(t, err)
		check(user1, 1.25, 8)
		check(user2, 2.5, 9)
	})
	t.Run("duplicate rates and nil RPM keep last value", func(t *testing.T) {
		reset()
		require.NoError(t, repo.SyncGroupRateMultipliers(ctx, group, []service.GroupRateMultiplierInput{{UserID: user1, RateMultiplier: 4}, {UserID: user1, RateMultiplier: 0}}))
		check(user1, 0, 8)
		v := 3
		require.NoError(t, repo.SyncGroupRPMOverrides(ctx, group, []service.GroupRPMOverrideInput{{UserID: user1, RPMOverride: &v}, {UserID: user1, RPMOverride: nil}}))
		got, err := repo.GetRPMOverrideByUserAndGroup(ctx, user1, group)
		require.NoError(t, err)
		require.Nil(t, got)
		gotRate, err := repo.GetByUserAndGroup(ctx, user1, group)
		require.NoError(t, err)
		require.NotNil(t, gotRate)
		require.Zero(t, *gotRate)
	})
	t.Run("workspace destructive statements preserve nonmembers", func(t *testing.T) {
		reset()
		var ws int64
		require.NoError(t, db.QueryRowContext(ctx, "INSERT INTO workspaces(name) VALUES('billing-audit') RETURNING id").Scan(&ws))
		_, err := db.ExecContext(ctx, "INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'member')", ws, user1)
		require.NoError(t, err)
		scoped := domain.WithWorkspaceScope(ctx, ws)
		require.NoError(t, repo.SyncGroupRateMultipliers(scoped, group, nil))
		require.NoError(t, repo.SyncGroupRPMOverrides(scoped, group, nil))
		check(user2, 2.5, 9)
		var count int
		require.NoError(t, db.QueryRowContext(ctx, "SELECT COUNT(*) FROM user_group_rate_multipliers WHERE group_id=$1 AND user_id=$2", group, user1).Scan(&count))
		require.Zero(t, count)
	})
	t.Run("concurrent rate and RPM updates preserve both columns", func(t *testing.T) {
		reset()
		v := 12
		start := make(chan struct{})
		errs := make(chan error, 2)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			errs <- repo.SyncGroupRateMultipliers(ctx, group, []service.GroupRateMultiplierInput{{UserID: user1, RateMultiplier: 0.75}})
		}()
		go func() {
			defer wg.Done()
			<-start
			errs <- repo.SyncGroupRPMOverrides(ctx, group, []service.GroupRPMOverrideInput{{UserID: user1, RPMOverride: &v}})
		}()
		close(start)
		wg.Wait()
		close(errs)
		for err := range errs {
			require.NoError(t, err)
		}
		check(user1, 0.75, 12)
	})
	t.Run("persisted replay reconstructs exact IDs and amounts", func(t *testing.T) {
		cfg := service.DefaultIdempotencyConfig()
		r := NewIdempotencyRepository(nil, db)
		opts := service.IdempotencyExecuteOptions{Scope: "audit.precision." + suffix, Method: "POST", Route: "/audit", ActorScope: "admin:synthetic", RequireKey: true, IdempotencyKey: "exact", Payload: map[string]any{"id": int64(9007199254740993)}}
		calls := 0
		run := func(context.Context) (any, error) {
			calls++
			return map[string]any{"id": int64(9007199254740993), "amount": json.Number("0.1234567890123456789")}, nil
		}
		first, err := service.NewIdempotencyCoordinator(r, cfg).Execute(ctx, opts, run)
		require.NoError(t, err)
		// New repository and coordinator, persisted response only.
		second, err := service.NewIdempotencyCoordinator(NewIdempotencyRepository(nil, db), cfg).Execute(ctx, opts, run)
		require.NoError(t, err)
		require.True(t, second.Replayed)
		require.Equal(t, 1, calls)
		a, err := json.Marshal(first.Data)
		require.NoError(t, err)
		b, err := json.Marshal(second.Data)
		require.NoError(t, err)
		require.Equal(t, string(a), string(b))
		opts.Payload = map[string]any{"id": int64(9007199254740994)}
		_, err = service.NewIdempotencyCoordinator(r, cfg).Execute(ctx, opts, run)
		require.Error(t, err)
		require.Equal(t, 1, calls)
	})
	t.Run("concurrent identical claims execute once", func(t *testing.T) {
		opts := service.IdempotencyExecuteOptions{Scope: "audit.concurrent." + suffix, Method: "POST", Route: "/audit", ActorScope: "admin:synthetic", RequireKey: true, IdempotencyKey: "same", Payload: 1}
		var calls atomic.Int64
		var wg sync.WaitGroup
		for range 12 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				c := service.NewIdempotencyCoordinator(NewIdempotencyRepository(nil, db), service.DefaultIdempotencyConfig())
				_, _ = c.Execute(ctx, opts, func(context.Context) (any, error) {
					calls.Add(1)
					time.Sleep(15 * time.Millisecond)
					return map[string]any{"id": int64(9007199254740993)}, nil
				})
			}()
		}
		wg.Wait()
		require.Equal(t, int64(1), calls.Load())
		result, err := service.NewIdempotencyCoordinator(NewIdempotencyRepository(nil, db), service.DefaultIdempotencyConfig()).Execute(ctx, opts, func(context.Context) (any, error) { t.Fatal("must replay"); return nil, nil })
		require.NoError(t, err)
		require.True(t, result.Replayed)
	})
}
