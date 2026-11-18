//go:build unit

package repository

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type failingRenewalRepository struct {
	service.UserSubscriptionRepository
	failStatus, failRead bool
}

func (r failingRenewalRepository) UpdateStatus(ctx context.Context, id int64, status string) error {
	if r.failStatus {
		return errors.New("injected status update failure")
	}
	return r.UserSubscriptionRepository.UpdateStatus(ctx, id, status)
}

func (r failingRenewalRepository) GetByID(ctx context.Context, id int64) (*service.UserSubscription, error) {
	if r.failRead {
		return nil, errors.New("injected response read failure")
	}
	return r.UserSubscriptionRepository.GetByID(ctx, id)
}

// Both endpoints must be disposable instances. The database guard matches the
// existing receipt integration suite; Redis uses only this fixture's key.
func TestSubscriptionRenewalPostgresRedis(t *testing.T) {
	dsn, redisAddr := os.Getenv("UPSTREAM_JOB_TEST_DSN"), os.Getenv("ACCOUNTING_TEST_REDIS_ADDR")
	expectedDatabase := "job_test"
	if auditDSN := os.Getenv("UPSTREAM_BILLING_AUDIT_DSN"); auditDSN != "" {
		dsn, expectedDatabase = auditDSN, "billing_audit_test"
	}
	if dsn == "" || redisAddr == "" {
		t.Skip("requires isolated job_test or billing_audit_test PostgreSQL and ACCOUNTING_TEST_REDIS_ADDR")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	db.SetMaxOpenConns(32)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	var database string
	require.NoError(t, db.QueryRowContext(ctx, "SELECT current_database()").Scan(&database))
	require.Equal(t, expectedDatabase, database)
	require.NoError(t, ApplyMigrations(ctx, db))
	rdb := redis.NewClient(&redis.Options{Addr: redisAddr})
	t.Cleanup(func() { require.NoError(t, rdb.Close()) })
	require.NoError(t, rdb.Ping(ctx).Err())
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	repo := NewUserSubscriptionRepository(client)
	cache := service.NewBillingCacheService(NewBillingCache(rdb), nil, repo, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(cache.Stop)
	svc := service.NewSubscriptionService(nil, repo, cache, client, nil)
	t.Cleanup(svc.Stop)
	var user, group, sub int64
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO users(email,password_hash) VALUES($1,'test') RETURNING id`, uuid.NewString()+"@renewal.test").Scan(&user))
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO groups(name,platform) VALUES($1,'openai') RETURNING id`, uuid.NewString()).Scan(&group))
	initial := time.Now().UTC().Truncate(time.Second).AddDate(0, 0, 10)
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO user_subscriptions(user_id,group_id,starts_at,expires_at) VALUES($1,$2,NOW(),$3) RETURNING id`, user, group, initial).Scan(&sub))
	key := billingSubKey(user, group)
	t.Cleanup(func() {
		_, err := db.Exec(`DELETE FROM users WHERE id=$1`, user)
		require.NoError(t, err)
		_, err = db.Exec(`DELETE FROM groups WHERE id=$1`, group)
		require.NoError(t, err)
		require.NoError(t, rdb.Del(context.Background(), key).Err())
	})
	seedCache := func() { require.NoError(t, rdb.Set(ctx, key, "cached", time.Minute).Err()) }
	assertCached := func(want int64) {
		count, err := rdb.Exists(ctx, key).Result()
		require.NoError(t, err)
		require.Equal(t, want, count)
	}
	reset := func(expiry time.Time, status string) {
		_, err := db.ExecContext(ctx, `UPDATE user_subscriptions SET expires_at=$1,status=$2 WHERE id=$3`, expiry, status, sub)
		require.NoError(t, err)
	}
	t.Run("concurrent extensions accumulate", func(t *testing.T) {
		var wg sync.WaitGroup
		errs := make(chan error, 24)
		for range 24 {
			wg.Add(1)
			go func() { defer wg.Done(); _, err := svc.ExtendSubscription(ctx, sub, 1); errs <- err }()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			require.NoError(t, err)
		}
		row, err := repo.GetByID(ctx, sub)
		require.NoError(t, err)
		require.True(t, initial.AddDate(0, 0, 24).Equal(row.ExpiresAt))
	})
	t.Run("expired and negative adjustments", func(t *testing.T) {
		reset(initial, service.SubscriptionStatusActive)
		row, err := svc.ExtendSubscription(ctx, sub, -2)
		require.NoError(t, err)
		require.True(t, initial.AddDate(0, 0, -2).Equal(row.ExpiresAt))
		_, err = svc.ExtendSubscription(ctx, sub, -100)
		require.ErrorIs(t, err, service.ErrAdjustWouldExpire)
		reset(time.Now().Add(-time.Hour), service.SubscriptionStatusExpired)
		_, err = svc.ExtendSubscription(ctx, sub, -1)
		require.ErrorContains(t, err, "cannot shorten")
		started := time.Now()
		row, err = svc.ExtendSubscription(ctx, sub, 3)
		require.NoError(t, err)
		require.Equal(t, service.SubscriptionStatusActive, row.Status)
		require.WithinDuration(t, started.AddDate(0, 0, 3), row.ExpiresAt, time.Second)
	})
	t.Run("status failure rolls back expiry and retains cache", func(t *testing.T) {
		expired := initial.AddDate(0, 0, -20)
		reset(expired, service.SubscriptionStatusExpired)
		seedCache()
		broken := service.NewSubscriptionService(nil, failingRenewalRepository{UserSubscriptionRepository: repo, failStatus: true}, cache, client, nil)
		_, err := broken.ExtendSubscription(ctx, sub, 3)
		require.ErrorContains(t, err, "injected status")
		row, err := repo.GetByID(ctx, sub)
		require.NoError(t, err)
		require.True(t, expired.Equal(row.ExpiresAt))
		require.Equal(t, service.SubscriptionStatusExpired, row.Status)
		assertCached(1)
	})
	t.Run("outer transaction invalidates only after commit", func(t *testing.T) {
		reset(initial, service.SubscriptionStatusActive)
		for _, commit := range []bool{false, true} {
			seedCache()
			tx, err := client.Tx(ctx)
			require.NoError(t, err)
			defer tx.Rollback()
			_, err = svc.ExtendSubscription(dbent.NewTxContext(ctx, tx), sub, 2)
			require.NoError(t, err)
			assertCached(1)
			if commit {
				require.NoError(t, tx.Commit())
				assertCached(0)
			} else {
				require.NoError(t, tx.Rollback())
				assertCached(1)
			}
		}
		row, err := repo.GetByID(ctx, sub)
		require.NoError(t, err)
		require.True(t, initial.AddDate(0, 0, 2).Equal(row.ExpiresAt))
	})
	t.Run("response read failure still invalidates committed renewal", func(t *testing.T) {
		seedCache()
		broken := service.NewSubscriptionService(nil, failingRenewalRepository{UserSubscriptionRepository: repo, failRead: true}, cache, client, nil)
		_, err := broken.ExtendSubscription(ctx, sub, 1)
		require.ErrorContains(t, err, "injected response")
		assertCached(0)
	})
}
