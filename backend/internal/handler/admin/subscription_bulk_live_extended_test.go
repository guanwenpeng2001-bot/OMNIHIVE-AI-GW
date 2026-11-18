//go:build unit

package admin

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestSubscriptionBulkLiveAssign(t *testing.T) {
	fixture := newBulkLiveFixture(t)
	var missingUsers int
	require.NoError(t, fixture.db.QueryRow(`SELECT count(*) FROM users WHERE id=2147483647`).Scan(&missingUsers))
	require.Zero(t, missingUsers)
	call := func(userIDs []int64) *httptest.ResponseRecorder {
		body, err := json.Marshal(BulkAssignSubscriptionRequest{UserIDs: userIDs, GroupID: fixture.groupID, ValidityDays: 30})
		require.NoError(t, err)
		request := httptest.NewRequest("POST", "/api/v1/admin/subscriptions/bulk-assign", bytes.NewReader(body))
		request.Header.Set("Authorization", "Bearer "+fixture.token)
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		fixture.router.ServeHTTP(response, request)
		return response
	}
	require.Equal(t, 400, call([]int64{fixture.adminID, 0}).Code)
	assertResult := func(response *httptest.ResponseRecorder, created, reused int) {
		require.Equal(t, 200, response.Code, response.Body.String())
		var envelope struct {
			Data struct {
				SuccessCount int `json:"success_count"`
				FailedCount  int `json:"failed_count"`
				CreatedCount int `json:"created_count"`
				ReusedCount  int `json:"reused_count"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
		require.Equal(t, 1, envelope.Data.SuccessCount)
		require.Equal(t, 2, envelope.Data.FailedCount)
		require.Equal(t, created, envelope.Data.CreatedCount)
		require.Equal(t, reused, envelope.Data.ReusedCount)
	}
	assertResult(call([]int64{fixture.adminID, fixture.users[1], 2147483647}), 1, 0)
	var createdID int64
	var expiresBefore time.Time
	require.NoError(t, fixture.db.QueryRow(`SELECT id,expires_at FROM user_subscriptions WHERE user_id=$1 AND group_id=$2`, fixture.adminID, fixture.groupID).Scan(&createdID, &expiresBefore))
	t.Cleanup(func() {
		_, err := fixture.db.Exec(`DELETE FROM user_subscriptions WHERE id=$1`, createdID)
		require.NoError(t, err)
	})
	assertResult(call([]int64{fixture.adminID, fixture.users[1], 2147483647}), 0, 1)
	var count int
	var expiresAfter time.Time
	require.NoError(t, fixture.db.QueryRow(`SELECT count(*),max(expires_at) FROM user_subscriptions WHERE user_id=$1 AND group_id=$2`, fixture.adminID, fixture.groupID).Scan(&count, &expiresAfter))
	require.Equal(t, 1, count)
	require.Equal(t, expiresBefore, expiresAfter)
	require.True(t, fixture.initial.Equal(fixture.expiry(0)))
}

type bulkCrashInput struct {
	Mode    string
	Marker  string
	Body    string
	Key     string
	AdminID int64
}

type bulkCrashIdempotencyRepository struct {
	service.IdempotencyRepository
	input bulkCrashInput
}

func (repository *bulkCrashIdempotencyRepository) MarkSucceeded(ctx context.Context, id int64, status int, body string, expires time.Time) error {
	if repository.input.Mode == "after_commit_before_result" {
		bulkCrashPause(repository.input.Marker)
	}
	err := repository.IdempotencyRepository.MarkSucceeded(ctx, id, status, body, expires)
	if err == nil && repository.input.Mode == "after_result_before_response" {
		bulkCrashPause(repository.input.Marker)
	}
	return err
}

func bulkCrashPause(marker string) {
	if err := os.WriteFile(marker, []byte("ready"), 0600); err != nil {
		panic(err)
	}
	select {}
}

func TestSubscriptionBulkLiveCrashWorker(t *testing.T) {
	if os.Getenv("SUB2API_CRASH_INPUT") == "" {
		t.Skip("child process only")
	}
	require.Equal(t, "1", os.Getenv("SUB2API_BULK_LIVE"))
	require.Equal(t, "postgres://omni_fixture@127.0.0.1:55480/job_test?sslmode=disable", os.Getenv("UPSTREAM_JOB_TEST_DSN"))
	require.Equal(t, "127.0.0.1:56380", os.Getenv("ACCOUNTING_TEST_REDIS_ADDR"))
	var input bulkCrashInput
	require.NoError(t, json.Unmarshal([]byte(os.Getenv("SUB2API_CRASH_INPUT")), &input))
	database, err := sql.Open("postgres", os.Getenv("UPSTREAM_JOB_TEST_DSN"))
	require.NoError(t, err)
	defer database.Close()
	var name string
	require.NoError(t, database.QueryRow(`SELECT current_database()`).Scan(&name))
	require.Equal(t, "job_test", name)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, database)))
	if input.Mode == "before_commit" || input.Mode == "between_items" {
		writes := 0
		client.UserSubscription.Use(func(next dbent.Mutator) dbent.Mutator {
			return dbent.MutateFunc(func(ctx context.Context, mutation dbent.Mutation) (dbent.Value, error) {
				value, err := next.Mutate(ctx, mutation)
				if err == nil {
					writes++
					if input.Mode == "before_commit" || writes == 2 {
						bulkCrashPause(input.Marker)
					}
				}
				return value, err
			})
		})
	}
	subscriptions := repository.NewUserSubscriptionRepository(client)
	redisClient := redis.NewClient(&redis.Options{Addr: os.Getenv("ACCOUNTING_TEST_REDIS_ADDR")})
	defer redisClient.Close()
	cache := service.NewBillingCacheService(repository.NewBillingCache(redisClient), nil, subscriptions, nil, nil, nil, &config.Config{}, nil)
	defer cache.Stop()
	subscriptionService := service.NewSubscriptionService(repository.NewGroupRepository(client, database), subscriptions, cache, client, nil)
	defer subscriptionService.Stop()
	idempotencyRepository := &bulkCrashIdempotencyRepository{IdempotencyRepository: repository.NewIdempotencyRepository(client, database), input: input}
	configuration := service.DefaultIdempotencyConfig()
	configuration.ObserveOnly = false
	service.SetDefaultIdempotencyCoordinator(service.NewIdempotencyCoordinator(idempotencyRepository, configuration))
	router := gin.New()
	router.Use(func(ctx *gin.Context) {
		ctx.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: input.AdminID})
	})
	router.POST("/api/v1/admin/subscriptions/bulk-action", NewSubscriptionHandler(subscriptionService).BulkAction)
	request := httptest.NewRequest("POST", "/api/v1/admin/subscriptions/bulk-action", bytes.NewBufferString(input.Body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", input.Key)
	router.ServeHTTP(httptest.NewRecorder(), request)
	t.Fatal("worker unexpectedly returned before process termination")
}

func TestSubscriptionBulkLiveCrashWindows(t *testing.T) {
	for _, mode := range []string{"before_commit", "between_items", "after_commit_before_result", "after_result_before_response"} {
		t.Run(mode, func(t *testing.T) {
			fixture := newBulkLiveFixture(t)
			key := fixture.key()
			body := fixture.body("extend", fixture.ids[:1], 1)
			successCount := 1
			if mode == "between_items" {
				body = fixture.body("extend", fixture.ids[:2], 1)
				successCount = 2
			}
			marker := filepath.Join(t.TempDir(), "ready")
			input, err := json.Marshal(bulkCrashInput{Mode: mode, Marker: marker, Body: body, Key: key, AdminID: fixture.adminID})
			require.NoError(t, err)
			executable, err := os.Executable()
			require.NoError(t, err)
			command := exec.Command(executable, "-test.run=^TestSubscriptionBulkLiveCrashWorker$", "-test.timeout=90s")
			command.Env = append(os.Environ(), "SUB2API_CRASH_INPUT="+string(input))
			output := &bytes.Buffer{}
			command.Stdout, command.Stderr = output, output
			require.NoError(t, command.Start())
			terminated := false
			t.Cleanup(func() {
				if !terminated {
					_ = command.Process.Kill()
					_ = command.Wait()
				}
			})
			require.Eventually(t, func() bool { _, err := os.Stat(marker); return err == nil }, 15*time.Second, 20*time.Millisecond)
			require.NoError(t, command.Process.Kill())
			require.Error(t, command.Wait())
			terminated = true
			t.Logf("terminated pid=%d at %s", command.Process.Pid, mode)
			expectedDays := 1
			if mode == "before_commit" {
				expectedDays = 0
			}
			require.True(t, fixture.initial.AddDate(0, 0, expectedDays).Equal(fixture.expiry(0)))
			retry := fixture.request(context.Background(), body, key, fixture.token)
			if mode == "after_result_before_response" {
				bulkLiveResult(t, retry, 1, 0)
				require.Equal(t, "true", retry.Header().Get("X-Idempotency-Replayed"))
				require.True(t, fixture.initial.AddDate(0, 0, 1).Equal(fixture.expiry(0)))
				return
			}
			require.Equal(t, 409, retry.Code, retry.Body.String())
			result, err := fixture.db.Exec(`UPDATE idempotency_records SET expires_at=NOW()-INTERVAL '1 minute',locked_until=NOW()-INTERVAL '1 minute' WHERE scope='admin.subscriptions.bulk-action' AND idempotency_key_hash=$1 AND status='processing'`, service.HashIdempotencyKey(key))
			require.NoError(t, err)
			affected, err := result.RowsAffected()
			require.NoError(t, err)
			require.EqualValues(t, 1, affected)
			retry = fixture.request(context.Background(), body, key, fixture.token)
			bulkLiveResult(t, retry, successCount, 0)
			require.True(t, fixture.initial.AddDate(0, 0, 1).Equal(fixture.expiry(0)), "recovery must never repeat a committed extension")
			if mode == "between_items" {
				require.True(t, fixture.initial.AddDate(0, 0, 1).Equal(fixture.expiry(1)))
			}
		})
	}
}
