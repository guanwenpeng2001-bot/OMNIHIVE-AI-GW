//go:build unit

package admin

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// Opt-in only. No migrations, new databases, flushes, provider clients or global
// cleanup. The runner must additionally verify Docker labels before execution.
type bulkLiveFixture struct {
	t                *testing.T
	db               *sql.DB
	client           *dbent.Client
	redis            *redis.Client
	repo             service.UserSubscriptionRepository
	handler          *SubscriptionHandler
	router           *gin.Engine
	auth             gin.HandlerFunc
	token, userToken string
	adminID, groupID int64
	ids, users       []int64
	initial          time.Time
	keys             []string
}

func newBulkLiveFixture(t *testing.T) *bulkLiveFixture {
	t.Helper()
	if os.Getenv("SUB2API_BULK_LIVE") != "1" {
		t.Skip("opt-in isolated PG17/Redis live test")
	}
	require.Equal(t, "postgres://omni_fixture@127.0.0.1:55480/job_test?sslmode=disable", os.Getenv("UPSTREAM_JOB_TEST_DSN"))
	require.Equal(t, "127.0.0.1:56380", os.Getenv("ACCOUNTING_TEST_REDIS_ADDR"))
	db, err := sql.Open("postgres", os.Getenv("UPSTREAM_JOB_TEST_DSN"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	db.SetMaxOpenConns(12)
	ctx := context.Background()
	var name, version string
	require.NoError(t, db.QueryRow(`SELECT current_database(), current_setting('server_version')`).Scan(&name, &version))
	require.Equal(t, "job_test", name)
	require.Contains(t, version, "17.")
	var missingIDs int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM user_subscriptions WHERE id IN (2147483647,9223372036854775807)`).Scan(&missingIDs))
	require.Zero(t, missingIDs, "failure fixtures must not refer to another test's rows")
	t.Logf("live database=%s version=%s redis=%s", name, version, os.Getenv("ACCOUNTING_TEST_REDIS_ADDR"))
	rdb := redis.NewClient(&redis.Options{Addr: os.Getenv("ACCOUNTING_TEST_REDIS_ADDR")})
	t.Cleanup(func() { require.NoError(t, rdb.Close()) })
	require.NoError(t, rdb.Ping(ctx).Err())
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	f := &bulkLiveFixture{t: t, db: db, client: client, redis: rdb, initial: time.Now().UTC().Truncate(time.Second).AddDate(0, 0, 10)}
	// Register cleanup before allocating fixtures; delete only recorded IDs/keys.
	t.Cleanup(func() {
		for _, key := range f.keys {
			_, e := db.Exec(`DELETE FROM idempotency_records WHERE idempotency_key_hash=$1`, service.HashIdempotencyKey(key))
			require.NoError(t, e)
		}
		for i, id := range f.ids {
			require.NoError(t, rdb.Del(ctx, f.cacheKey(i)).Err())
			_, e := db.Exec(`DELETE FROM user_subscriptions WHERE id=$1`, id)
			require.NoError(t, e)
		}
		for _, id := range f.users {
			require.NoError(t, rdb.Del(ctx, fmt.Sprintf("billing:sub:%d:%d", id, f.groupID)).Err())
			_, subscriptionErr := db.Exec(`DELETE FROM user_subscriptions WHERE user_id=$1 AND group_id=$2`, id, f.groupID)
			require.NoError(t, subscriptionErr)
			_, e := db.Exec(`DELETE FROM users WHERE id=$1`, id)
			require.NoError(t, e)
		}
		if f.groupID != 0 {
			_, e := db.Exec(`DELETE FROM groups WHERE id=$1`, f.groupID)
			require.NoError(t, e)
		}
	})
	group, err := client.Group.Create().SetName("bulk-live-" + uuid.NewString()).SetPlatform("openai").SetSubscriptionType("subscription").Save(ctx)
	require.NoError(t, err)
	f.groupID = group.ID
	for i := 0; i < 4; i++ {
		role := "user"
		if i == 0 {
			role = "admin"
		}
		u, e := client.User.Create().SetEmail(uuid.NewString() + "@bulk-live.invalid").SetPasswordHash("synthetic-fixture-no-login").SetRole(role).Save(ctx)
		require.NoError(t, e)
		f.users = append(f.users, u.ID)
		if i == 0 {
			f.adminID = u.ID
			continue
		}
		s, e := client.UserSubscription.Create().SetUserID(u.ID).SetGroupID(group.ID).SetStartsAt(time.Now()).SetExpiresAt(f.initial).SetDailyUsageUsd(11).SetWeeklyUsageUsd(22).SetMonthlyUsageUsd(33).Save(ctx)
		require.NoError(t, e)
		f.ids = append(f.ids, s.ID)
	}
	f.repo = repository.NewUserSubscriptionRepository(client)
	cache := service.NewBillingCacheService(repository.NewBillingCache(rdb), nil, f.repo, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(cache.Stop)
	svc := service.NewSubscriptionService(repository.NewGroupRepository(client, db), f.repo, cache, client, nil)
	t.Cleanup(svc.Stop)
	f.handler = NewSubscriptionHandler(svc)
	f.restartCoordinator()
	t.Cleanup(func() { service.SetDefaultIdempotencyCoordinator(nil) })
	userRepo := repository.NewUserRepository(client, db)
	cfg := &config.Config{JWT: config.JWTConfig{Secret: uuid.NewString() + uuid.NewString(), ExpireHour: 1}}
	authSvc := service.NewAuthService(client, userRepo, nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil, nil)
	for i, id := range []int64{f.adminID, f.users[1]} {
		u, e := userRepo.GetByID(ctx, id)
		require.NoError(t, e)
		tok, e := authSvc.GenerateToken(ctx, u)
		require.NoError(t, e)
		if i == 0 {
			f.token = tok
		} else {
			f.userToken = tok
		}
	}
	f.auth = gin.HandlerFunc(middleware.NewAdminAuthMiddleware(authSvc, service.NewUserService(userRepo, nil, nil, nil), nil, nil))
	gin.SetMode(gin.TestMode)
	f.router = gin.New()
	f.router.Use(gin.Recovery(), f.auth)
	f.router.POST("/api/v1/admin/subscriptions/bulk-action", f.handler.BulkAction)
	f.router.POST("/api/v1/admin/subscriptions/bulk-assign", f.handler.BulkAssign)
	f.router.GET("/api/v1/admin/subscriptions/:id", f.handler.GetByID)
	return f
}

func (f *bulkLiveFixture) restartCoordinator() {
	cfg := service.DefaultIdempotencyConfig()
	cfg.ObserveOnly = false
	service.SetDefaultIdempotencyCoordinator(service.NewIdempotencyCoordinator(repository.NewIdempotencyRepository(f.client, f.db), cfg))
}
func (f *bulkLiveFixture) key() string {
	k := "bulk-live-" + uuid.NewString()
	f.keys = append(f.keys, k)
	return k
}
func (f *bulkLiveFixture) cacheKey(i int) string {
	return fmt.Sprintf("billing:sub:%d:%d", f.users[i+1], f.groupID)
}
func (f *bulkLiveFixture) request(ctx context.Context, body, key, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/api/v1/admin/subscriptions/bulk-action", bytes.NewBufferString(body)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", key)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	return rec
}
func (f *bulkLiveFixture) body(action string, ids []int64, days int) string {
	b, e := json.Marshal(service.BulkSubscriptionActionInput{SubscriptionIDs: ids, Action: action, Days: days, Daily: action == "reset_quota"})
	require.NoError(f.t, e)
	return string(b)
}
func bulkLiveResult(t *testing.T, r *httptest.ResponseRecorder, successes, failures int) service.BulkSubscriptionActionResult {
	t.Helper()
	require.Equal(t, 200, r.Code, r.Body.String())
	var envelope struct {
		Data service.BulkSubscriptionActionResult `json:"data"`
	}
	require.NoError(t, json.Unmarshal(r.Body.Bytes(), &envelope))
	require.Equal(t, successes, envelope.Data.SuccessCount)
	require.Equal(t, failures, envelope.Data.FailedCount)
	return envelope.Data
}
func (f *bulkLiveFixture) expiry(i int) time.Time {
	var expiry time.Time
	require.NoError(f.t, f.db.QueryRow(`SELECT expires_at FROM user_subscriptions WHERE id=$1`, f.ids[i]).Scan(&expiry))
	return expiry
}
func (f *bulkLiveFixture) persisted(key string, count int) {
	var n int
	require.NoError(f.t, f.db.QueryRow(`SELECT count(*) FROM idempotency_records WHERE idempotency_key_hash=$1 AND status='succeeded' AND response_status=200 AND response_body IS NOT NULL`, service.HashIdempotencyKey(key)).Scan(&n))
	require.Equal(f.t, count, n)
}

func TestSubscriptionBulkLivePostgresRedis(t *testing.T) {
	f := newBulkLiveFixture(t)
	ctx := context.Background()
	t.Run("real_admin_auth_and_validation", func(t *testing.T) {
		body := f.body("extend", f.ids[:1], 7)
		require.Equal(t, 401, f.request(ctx, body, f.key(), "").Code)
		require.Equal(t, 403, f.request(ctx, body, f.key(), f.userToken).Code)
		require.Equal(t, 400, f.request(ctx, body, "", f.token).Code)
		k := f.key()
		require.Equal(t, 400, f.request(ctx, f.body("extend", f.ids[:1], 0), k, f.token).Code)
		var n int
		require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM idempotency_records WHERE idempotency_key_hash=$1`, service.HashIdempotencyKey(k)).Scan(&n))
		require.Zero(t, n)
		require.True(t, f.initial.Equal(f.expiry(0)))
	})
	t.Run("partial_duplicate_persistent_replay_and_conflict", func(t *testing.T) {
		require.NoError(t, f.redis.Set(ctx, f.cacheKey(0), "stale", time.Minute).Err())
		k := f.key()
		body := f.body("extend", []int64{f.ids[0], 2147483647, f.ids[0]}, 7)
		first := f.request(ctx, body, k, f.token)
		result := bulkLiveResult(t, first, 1, 1)
		require.Len(t, result.Results, 2)
		require.True(t, f.initial.AddDate(0, 0, 7).Equal(f.expiry(0)))
		f.persisted(k, 1)
		require.EqualValues(t, 0, f.redis.Exists(ctx, f.cacheKey(0)).Val())
		f.restartCoordinator() // New coordinator and repository; replay cannot come from an in-memory result.
		replay := f.request(ctx, body, k, f.token)
		bulkLiveResult(t, replay, 1, 1)
		require.Equal(t, "true", replay.Header().Get("X-Idempotency-Replayed"))
		require.JSONEq(t, first.Body.String(), replay.Body.String())
		require.Equal(t, 409, f.request(ctx, f.body("extend", f.ids[:1], 8), k, f.token).Code)
		require.True(t, f.initial.AddDate(0, 0, 7).Equal(f.expiry(0)))
	})
	t.Run("failure_after_real_write_rolls_back_only_that_item", func(t *testing.T) {
		expired := f.initial.AddDate(0, 0, -20)
		_, err := f.db.Exec(`UPDATE user_subscriptions SET expires_at=$1,status='expired' WHERE id=$2`, expired, f.ids[1])
		require.NoError(t, err)
		require.NoError(t, f.redis.Set(ctx, f.cacheKey(1), "preserve-on-rollback", time.Minute).Err())
		fail := true
		sawWritten := false
		f.client.UserSubscription.Use(func(next dbent.Mutator) dbent.Mutator {
			return dbent.MutateFunc(func(c context.Context, m dbent.Mutation) (dbent.Value, error) {
				mutation := m.(*dbent.UserSubscriptionMutation)
				id, _ := mutation.ID()
				_, status := mutation.Status()
				if fail && id == f.ids[1] && status {
					row, e := f.repo.GetByIDForUpdate(c, id)
					if e != nil {
						return nil, e
					}
					sawWritten = row.ExpiresAt.After(time.Now())
					return nil, errors.New("live test fault after SQL expiry write before status update")
				}
				return next.Mutate(c, m)
			})
		})
		before := f.expiry(0)
		k := f.key()
		body := f.body("extend", f.ids[:2], 3)
		bulkLiveResult(t, f.request(ctx, body, k, f.token), 1, 1)
		require.True(t, sawWritten, "fault must occur after actual expiry write in same transaction")
		require.True(t, before.AddDate(0, 0, 3).Equal(f.expiry(0)))
		require.True(t, expired.Equal(f.expiry(1)))
		require.EqualValues(t, 1, f.redis.Exists(ctx, f.cacheKey(1)).Val())
		f.persisted(k, 1)
		fail = false
		replay := f.request(ctx, body, k, f.token)
		bulkLiveResult(t, replay, 1, 1)
		require.Equal(t, "true", replay.Header().Get("X-Idempotency-Replayed"))
		require.True(t, expired.Equal(f.expiry(1)))
		bulkLiveResult(t, f.request(ctx, f.body("extend", f.ids[1:2], 3), f.key(), f.token), 1, 0)
		require.True(t, f.expiry(1).After(time.Now()))
		require.EqualValues(t, 0, f.redis.Exists(ctx, f.cacheKey(1)).Val())
	})
	t.Run("cancellation_after_first_SQL_write_finishes_and_replays", func(t *testing.T) {
		requestCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		canceled := false
		f.client.UserSubscription.Use(func(next dbent.Mutator) dbent.Mutator {
			return dbent.MutateFunc(func(c context.Context, m dbent.Mutation) (dbent.Value, error) {
				v, e := next.Mutate(c, m)
				if !canceled && e == nil {
					canceled = true
					cancel()
				}
				return v, e
			})
		})
		before := []time.Time{f.expiry(0), f.expiry(2)}
		body := f.body("extend", []int64{f.ids[0], f.ids[2]}, 2)
		k := f.key()
		first := f.request(requestCtx, body, k, f.token)
		bulkLiveResult(t, first, 2, 0)
		require.ErrorIs(t, requestCtx.Err(), context.Canceled)
		f.persisted(k, 1)
		for j, i := range []int{0, 2} {
			require.True(t, before[j].AddDate(0, 0, 2).Equal(f.expiry(i)))
		}
		f.restartCoordinator()
		replay := f.request(ctx, body, k, f.token)
		require.JSONEq(t, first.Body.String(), replay.Body.String())
		require.Equal(t, "true", replay.Header().Get("X-Idempotency-Replayed"))
		require.True(t, before[1].AddDate(0, 0, 2).Equal(f.expiry(2)))
	})
	t.Run("concurrent_same_key_executes_once", func(t *testing.T) {
		before := f.expiry(2)
		k := f.key()
		body := f.body("extend", f.ids[2:], 1)
		responses := make(chan *httptest.ResponseRecorder, 8)
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); responses <- f.request(ctx, body, k, f.token) }()
		}
		wg.Wait()
		close(responses)
		for r := range responses {
			if r.Code == 200 {
				bulkLiveResult(t, r, 1, 0)
			} else {
				require.Equal(t, 409, r.Code, r.Body.String())
			}
		}
		bulkLiveResult(t, f.request(ctx, body, k, f.token), 1, 0)
		require.True(t, before.AddDate(0, 0, 1).Equal(f.expiry(2)))
		f.persisted(k, 1)
	})
	t.Run("selective_quota_revoke_restore", func(t *testing.T) {
		for _, action := range []string{"reset_quota", "revoke", "restore"} {
			k := f.key()
			body := f.body(action, f.ids[2:], 0)
			first := f.request(ctx, body, k, f.token)
			bulkLiveResult(t, first, 1, 0)
			replay := f.request(ctx, body, k, f.token)
			require.Equal(t, "true", replay.Header().Get("X-Idempotency-Replayed"))
			require.JSONEq(t, first.Body.String(), replay.Body.String())
			f.persisted(k, 1)
			var daily, weekly, monthly float64
			var deleted sql.NullTime
			require.NoError(t, f.db.QueryRow(`SELECT daily_usage_usd,weekly_usage_usd,monthly_usage_usd,deleted_at FROM user_subscriptions WHERE id=$1`, f.ids[2]).Scan(&daily, &weekly, &monthly, &deleted))
			require.Zero(t, daily)
			require.Equal(t, float64(22), weekly)
			require.Equal(t, float64(33), monthly)
			require.Equal(t, action == "revoke", deleted.Valid)
		}
	})
	t.Run("workspace_context_isolates_persistent_replay_not_subscription_rows", func(t *testing.T) {
		before := f.expiry(2)
		k := f.key()
		body := f.body("extend", f.ids[2:], 1)
		for _, workspace := range []int64{890001, 890002} {
			scoped := domain.WithWorkspaceScope(ctx, workspace)
			first := f.request(scoped, body, k, f.token)
			bulkLiveResult(t, first, 1, 0)
			replay := f.request(scoped, body, k, f.token)
			require.Equal(t, "true", replay.Header().Get("X-Idempotency-Replayed"))
			require.JSONEq(t, first.Body.String(), replay.Body.String())
		}
		f.persisted(k, 2)
		require.True(t, before.AddDate(0, 0, 2).Equal(f.expiry(2)))
	})
}

// A bounded localhost-only HTTP bridge for the real Vue/axios browser test.
// It is a test binary, not a deployed application. Authentication uses the
// production JWT/admin middleware; fixture setup is gated by a random run secret.
func TestSubscriptionBulkLiveBrowserBridge(t *testing.T) {
	if os.Getenv("SUB2API_BULK_BROWSER") != "1" {
		t.Skip("explicit browser bridge mode only")
	}
	f := newBulkLiveFixture(t)
	secret := os.Getenv("SUB2API_BULK_BRIDGE_SECRET")
	require.GreaterOrEqual(t, len(secret), 32)
	router := gin.New()
	router.Use(gin.Recovery())
	done := make(chan struct{})
	var once sync.Once
	controls := router.Group("/__fixture", func(c *gin.Context) {
		if c.GetHeader("X-Fixture-Secret") != secret {
			c.AbortWithStatus(403)
			return
		}
		c.Next()
	})
	controls.GET("/state", func(c *gin.Context) {
		subs := []any{}
		for _, id := range f.ids {
			row, err := f.repo.GetByIDIncludeDeleted(c.Request.Context(), id)
			if err != nil {
				c.AbortWithStatusJSON(500, gin.H{"error": err.Error()})
				return
			}
			subs = append(subs, row)
		}
		c.JSON(200, gin.H{"ids": f.ids, "admin_id": f.adminID, "token": f.token, "user_token": f.userToken, "subscriptions": subs})
	})
	controls.POST("/stop", func(c *gin.Context) { c.Status(200); once.Do(func() { close(done) }) })
	configureBulkLiveListBridge(router, controls, f)
	api := router.Group("/api/v1/admin/subscriptions", f.auth)
	api.GET("/:id", f.handler.GetByID)
	api.POST("/bulk-action", func(c *gin.Context) {
		// Only own IDs (and a known nonexistent ID) may reach writes in browser mode.
		var input service.BulkSubscriptionActionInput
		if err := c.ShouldBindJSON(&input); err != nil {
			c.AbortWithStatus(400)
			return
		}
		for _, id := range input.SubscriptionIDs {
			allowed := id == 9223372036854775807 || id == 2147483647
			if !allowed {
				if err := f.db.QueryRowContext(c.Request.Context(), `SELECT EXISTS(SELECT 1 FROM user_subscriptions WHERE id=$1 AND group_id=$2)`, id, f.groupID).Scan(&allowed); err != nil {
					c.AbortWithStatus(500)
					return
				}
			}
			if !allowed {
				c.AbortWithStatus(403)
				return
			}
		}
		raw, _ := json.Marshal(input)
		c.Request.Body = http.NoBody
		c.Request.Body = ioNopCloser{bytes.NewReader(raw)}
		key := c.GetHeader("Idempotency-Key")
		if key != "" {
			f.keys = append(f.keys, key)
		}
		f.handler.BulkAction(c)
	})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := &http.Server{Handler: router, ReadHeaderTimeout: 5 * time.Second}
	exited := make(chan error, 1)
	go func() { exited <- server.Serve(listener) }()
	t.Logf("BULK_BRIDGE_ADDR=%s", listener.Addr())
	select {
	case <-done:
	case <-time.After(20 * time.Minute):
		t.Error("browser test did not stop bridge before deadline")
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, server.Shutdown(shutdownCtx))
	require.ErrorIs(t, <-exited, http.ErrServerClosed)
}

type ioNopCloser struct{ *bytes.Reader }

func (ioNopCloser) Close() error { return nil }

func TestSubscriptionBulkLiveLargeIDReplay(t *testing.T) {
	f := newBulkLiveFixture(t)
	body := f.body("extend", []int64{f.ids[0], 9223372036854775807}, 1)
	key := f.key()
	first := f.request(context.Background(), body, key, f.token)
	bulkLiveResult(t, first, 1, 1)
	f.restartCoordinator()
	replay := f.request(context.Background(), body, key, f.token)
	require.Equal(t, "true", replay.Header().Get("X-Idempotency-Replayed"))
	result := bulkLiveResult(t, replay, 1, 1)
	require.Equal(t, int64(9223372036854775807), result.Results[1].SubscriptionID)
	require.JSONEq(t, first.Body.String(), replay.Body.String())
}
