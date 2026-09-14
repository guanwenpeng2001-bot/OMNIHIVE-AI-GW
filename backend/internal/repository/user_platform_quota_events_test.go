//go:build unit

package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestUserPlatformQuotaEventsPostgres(t *testing.T) {
	dsn := os.Getenv("UPSTREAM_JOB_TEST_DSN")
	addr := os.Getenv("QUOTA_TEST_REDIS_ADDR")
	if dsn == "" || addr == "" {
		t.Skip("requires isolated job_test PostgreSQL and QUOTA_TEST_REDIS_ADDR")
	}
	ctx := context.Background()
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	var name string
	require.NoError(t, db.QueryRow(`SELECT current_database()`).Scan(&name))
	require.Equal(t, "job_test", name)
	require.NoError(t, ApplyMigrations(ctx, db))
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	repo := &userPlatformQuotaRepository{client: client}
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { require.NoError(t, rdb.Close()) })
	require.NoError(t, rdb.Ping(ctx).Err())
	cache := &billingCache{rdb: rdb}
	invalidate := cache.DeleteUserPlatformQuotaCache
	fixture := func(at time.Time) *service.UsageSettlementReceipt {
		var user, key, account int64
		require.NoError(t, db.QueryRow(`INSERT INTO users(email,password_hash,balance) VALUES($1,'test',100) RETURNING id`, uuid.NewString()+"@quota.test").Scan(&user))
		require.NoError(t, db.QueryRow(`INSERT INTO api_keys(user_id,key,name) VALUES($1,$2,'quota') RETURNING id`, user, uuid.NewString()).Scan(&key))
		require.NoError(t, db.QueryRow(`INSERT INTO accounts(name,platform,type) VALUES('quota','qianwen','apikey') RETURNING id`).Scan(&account))
		_, err := db.Exec(`INSERT INTO user_platform_quotas(user_id,platform,daily_limit_usd,weekly_limit_usd,monthly_limit_usd,created_at) VALUES($1,'qianwen',100,100,100,$2)`, user, at.Add(-time.Hour))
		require.NoError(t, err)
		amount := 1.0
		snapshot, err := json.Marshal(service.PricingSnapshot{PricingState: service.PricingStatePriced, Model: "quota", Provider: "qianwen", Mode: "image", Source: "configured", UnitPrice: &amount, Units: 1, RateMultiplier: 1, TimeMultiplier: 1, ReasoningMultiplier: 1, TotalCost: 1, ActualCost: 1, ChargedAmount: 1})
		require.NoError(t, err)
		id := uuid.NewString()
		return &service.UsageSettlementReceipt{Key: service.UsageSettlementKey{RequestID: id, UserID: user, APIKeyID: key}, Command: service.UsageBillingCommand{RequestID: id, UserID: user, APIKeyID: key, AccountID: account, AccountType: service.AccountTypeAPIKey, Model: "quota", BalanceCost: 1}, Usage: service.UsageLog{RequestID: id, UserID: user, APIKeyID: key, AccountID: account, Model: "quota", QuotaPlatform: "qianwen", CreatedAt: at, TotalCost: 1, ActualCost: 1, RateMultiplier: 1, PricingState: service.PricingStatePriced, PricingSnapshot: string(snapshot)}, ResolutionState: "resolved", CompletionState: "complete"}
	}
	settle := func(input *service.UsageSettlementReceipt) *service.UsageSettlementReceipt {
		receipts := NewUsageSettlementReceiptWithQuotaRepository(db)
		stored, err := receipts.Persist(ctx, input)
		require.NoError(t, err)
		result, err := receipts.Settle(ctx, stored.Key, stored.Command.RequestFingerprint)
		require.NoError(t, err)
		return result.Receipt
	}
	usage := func(input *service.UsageSettlementReceipt, d, w, m float64) {
		rec, err := repo.GetByUserPlatform(ctx, input.Key.UserID, "qianwen")
		require.NoError(t, err)
		require.Equal(t, d, rec.DailyUsageUSD)
		require.Equal(t, w, rec.WeeklyUsageUSD)
		require.Equal(t, m, rec.MonthlyUsageUSD)
	}
	drain := func() { _, err := repo.ConsumeQuotaEvents(ctx, 1000, invalidate); require.NoError(t, err) }
	t.Run("commit_exit_redis_failure_duplicate_multiconsumer_and_cache_clear", func(t *testing.T) {
		input := settle(fixture(time.Now().UTC()))
		usage(input, 0, 0, 0)
		// Recreate repositories after the money commit, as a restarted worker does.
		restarted := &userPlatformQuotaRepository{client: client}
		rejectedRedis := redis.NewClient(&redis.Options{Addr: addr, Username: "quota-test-no-such-user", Password: "quota-test-rejected", MaxRetries: -1})
		defer func() { require.NoError(t, rejectedRedis.Close()) }()
		rejectedCache := &billingCache{rdb: rejectedRedis}
		_, err := restarted.ConsumeQuotaEvents(ctx, 100, rejectedCache.DeleteUserPlatformQuotaCache)
		require.Error(t, err)
		usage(input, 1, 1, 1)
		var balance float64
		require.NoError(t, db.QueryRow(`SELECT balance FROM users WHERE id=$1`, input.Key.UserID).Scan(&balance))
		require.Equal(t, 99.0, balance)
		settle(input)
		var wg sync.WaitGroup
		errs := make(chan error, 8)
		for range 8 {
			wg.Add(1)
			go func() { defer wg.Done(); _, e := restarted.ConsumeQuotaEvents(ctx, 100, invalidate); errs <- e }()
		}
		wg.Wait()
		close(errs)
		for e := range errs {
			require.NoError(t, e)
		}
		require.NoError(t, rdb.FlushDB(ctx).Err())
		drain()
		usage(input, 1, 1, 1)
		now := time.Now()
		require.NoError(t, repo.BatchSnapshotUsage(ctx, []UserPlatformQuotaSnapshot{{UserID: input.Key.UserID, Platform: "qianwen", DailyUsageUSD: 99, WeeklyUsageUSD: 99, MonthlyUsageUSD: 99, DailyWindowStart: now, WeeklyWindowStart: now, MonthlyWindowStart: now}}, now))
		usage(input, 1, 1, 1)
	})

	t.Run("actual_process_exit_after_money_commit", func(t *testing.T) {
		input := fixture(time.Now().UTC())
		raw, err := json.Marshal(input)
		require.NoError(t, err)
		child := exec.Command(os.Args[0], "-test.run=^TestQuotaEventCommitProcess$")
		child.Env = append(os.Environ(), "QUOTA_PROCESS_RECEIPT="+string(raw))
		output, err := child.CombinedOutput()
		require.NoError(t, err, string(output))
		usage(input, 0, 0, 0)
		stored, err := NewUsageSettlementReceiptWithQuotaRepository(db).Get(ctx, input.Key)
		require.NoError(t, err)
		require.Equal(t, service.UsageSettlementSettled, stored.State)
		// Distinct OS processes each construct an independent DB pool and consumer.
		var wg sync.WaitGroup
		results := make(chan error, 4)
		for range 4 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				command := exec.Command(os.Args[0], "-test.run=^TestQuotaEventConsumerProcess$")
				command.Env = append(os.Environ(), "QUOTA_PROCESS_CONSUMER=1")
				out, e := command.CombinedOutput()
				if e != nil {
					e = fmt.Errorf("%w: %s", e, out)
				}
				results <- e
			}()
		}
		wg.Wait()
		close(results)
		for e := range results {
			require.NoError(t, e)
		}
		usage(input, 1, 1, 1)
	})
	t.Run("async_settlement_records_event_in_money_transaction", func(t *testing.T) {
		input := fixture(time.Now().UTC())
		jobs := service.NewUpstreamJobService(NewUpstreamJobRepository(db), nil, service.QianwenVideoJobCharge)
		job, err := jobs.Submit(ctx, &service.UpstreamJob{UserID: input.Key.UserID, APIKeyID: input.Key.APIKeyID, AccountID: input.Usage.AccountID, Platform: "qianwen", Family: "video", Billing: service.UpstreamJobBilling{Model: "wan2.6-t2v", RequestedModel: "wan2.6-t2v", Resolution: "720p", UnitPrice: .2, RateMultiplier: 1, AccountRateMultiplier: 1}}, []byte(`{"model":"wan2.6-t2v","input":{"prompt":"quota"}}`), func(context.Context) (service.UpstreamJobUpdate, error) {
			return service.UpstreamJobUpdate{TaskID: "quota-upstream", Status: "submitted"}, nil
		})
		require.NoError(t, err)
		evidence := service.UpstreamJobUpdate{TaskID: "quota-upstream", Status: "succeeded", ResultRef: "https://example.test/video.mp4", Usage: json.RawMessage(`{"duration":5,"video_count":1,"SR":720}`)}
		_, err = jobs.Settle(ctx, job.PublicRequestID, evidence)
		require.NoError(t, err)
		usage(input, 0, 0, 0)
		drain()
		usage(input, 1, 1, 1)
		_, err = jobs.Settle(ctx, job.PublicRequestID, evidence)
		require.NoError(t, err)
		drain()
		usage(input, 1, 1, 1)
	})
	t.Run("consumer_transaction_rollback", func(t *testing.T) {
		input := settle(fixture(time.Now().UTC()))
		_, err := db.Exec(`CREATE FUNCTION quota_test_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected progress failure'; END $$; CREATE TRIGGER quota_test_fail BEFORE UPDATE OF applied_at ON user_platform_quota_events FOR EACH ROW EXECUTE FUNCTION quota_test_fail()`)
		require.NoError(t, err)
		_, consumeErr := repo.ConsumeQuotaEvents(ctx, 100, invalidate)
		_, err = db.Exec(`DROP TRIGGER quota_test_fail ON user_platform_quota_events; DROP FUNCTION quota_test_fail()`)
		require.NoError(t, err)
		require.Error(t, consumeErr)
		usage(input, 0, 0, 0)
		drain()
		usage(input, 1, 1, 1)
	})
	t.Run("money_transaction_event_failure", func(t *testing.T) {
		input := fixture(time.Now().UTC())
		receipts := NewUsageSettlementReceiptRepository(db, InsertUserPlatformQuotaEvent, func(context.Context, *sql.Tx, *service.UsageLog) error { return errors.New("after event") })
		stored, err := receipts.Persist(ctx, input)
		require.NoError(t, err)
		_, err = receipts.Settle(ctx, stored.Key, stored.Command.RequestFingerprint)
		require.Error(t, err)
		var balance float64
		var n int
		require.NoError(t, db.QueryRow(`SELECT balance FROM users WHERE id=$1`, input.Key.UserID).Scan(&balance))
		require.Equal(t, 100.0, balance)
		require.NoError(t, db.QueryRow(`SELECT count(*) FROM user_platform_quota_events WHERE user_id=$1`, input.Key.UserID).Scan(&n))
		require.Zero(t, n)
		settle(input)
		drain()
		usage(input, 1, 1, 1)
	})

	t.Run("independent_daily_weekly_and_rolling_month_windows", func(t *testing.T) {
		at := time.Date(2026, 3, 2, 12, 0, 0, 0, time.UTC)
		input := settle(fixture(at))
		drain()
		usage(input, 1, 1, 1)
		next := func(days int) {
			copy := *input
			copy.Usage = input.Usage
			copy.Command = input.Command
			id := uuid.NewString()
			copy.Key.RequestID = id
			copy.Command.RequestID = id
			copy.Command.RequestFingerprint = ""
			copy.Usage.ID = 0
			copy.Usage.RequestID = id
			copy.Usage.CreatedAt = at.AddDate(0, 0, days)
			copy.State = ""
			copy.UsageLogID = nil
			settle(&copy)
			drain()
		}
		next(1)
		usage(input, 1, 2, 2)
		next(7)
		usage(input, 1, 1, 3)
		next(31)
		usage(input, 1, 1, 1)
		next(0)
		usage(input, 1, 1, 1)
	})
	t.Run("late_windows_and_explicit_reset", func(t *testing.T) {
		at := time.Now().UTC().Add(-70 * 24 * time.Hour)
		input := settle(fixture(at))
		drain()
		usage(input, 1, 1, 1)
		next := func(when time.Time) {
			copy := *input
			copy.Usage = input.Usage
			copy.Command = input.Command
			id := uuid.NewString()
			copy.Key.RequestID = id
			copy.Command.RequestID = id
			copy.Command.RequestFingerprint = ""
			copy.Usage.ID = 0
			copy.Usage.RequestID = id
			copy.Usage.CreatedAt = when
			copy.State = ""
			copy.UsageLogID = nil
			settle(&copy)
		}
		next(at.Add(35 * 24 * time.Hour))
		drain()
		usage(input, 1, 1, 1)
		next(at.Add(time.Hour))
		drain()
		usage(input, 1, 1, 1)
		// A pending event from the old generation must not undo a reset.
		next(at.Add(35*24*time.Hour + time.Hour))
		for _, window := range []string{"daily", "weekly", "monthly"} {
			require.NoError(t, repo.ResetExpiredWindow(ctx, input.Key.UserID, "qianwen", window, time.Now().UTC()))
		}
		drain()
		usage(input, 0, 0, 0)
		// Receipt recorded after reset, but consumed before it, is also excluded.
		next(at.Add(36 * 24 * time.Hour))
		drain()
		usage(input, 0, 0, 0)
		next(time.Now().UTC())
		drain()
		usage(input, 1, 1, 1)
	})
}

// Subprocess-only helpers: os.Exit intentionally bypasses defers after commit.
func TestQuotaEventCommitProcess(t *testing.T) {
	raw := os.Getenv("QUOTA_PROCESS_RECEIPT")
	if raw == "" {
		t.Skip("subprocess helper")
	}
	var input service.UsageSettlementReceipt
	require.NoError(t, json.Unmarshal([]byte(raw), &input))
	db, err := sql.Open("postgres", os.Getenv("UPSTREAM_JOB_TEST_DSN"))
	require.NoError(t, err)
	repo := NewUsageSettlementReceiptWithQuotaRepository(db)
	ctx := context.Background()
	stored, err := repo.Persist(ctx, &input)
	require.NoError(t, err)
	_, err = repo.Settle(ctx, stored.Key, stored.Command.RequestFingerprint)
	require.NoError(t, err)
	os.Exit(0)
}
func TestQuotaEventConsumerProcess(t *testing.T) {
	if os.Getenv("QUOTA_PROCESS_CONSUMER") == "" {
		t.Skip("subprocess helper")
	}
	db, err := sql.Open("postgres", os.Getenv("UPSTREAM_JOB_TEST_DSN"))
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	rdb := redis.NewClient(&redis.Options{Addr: os.Getenv("QUOTA_TEST_REDIS_ADDR")})
	defer func() { require.NoError(t, rdb.Close()) }()
	repo := &userPlatformQuotaRepository{client: client}
	cache := &billingCache{rdb: rdb}
	_, err = repo.ConsumeQuotaEvents(context.Background(), 100, cache.DeleteUserPlatformQuotaCache)
	require.NoError(t, err)
}
