//go:build integration

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	_ "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent/runtime"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/runtimeenv"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/timezone"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil/testresources"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/lib/pq"
	redisclient "github.com/redis/go-redis/v9"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
)

const (
	redisImageTag    = "redis:8.4-alpine"
	postgresImageTag = "postgres:18.1-alpine3.23"
)

var (
	integrationDB        *sql.DB
	integrationEntClient *dbent.Client
	integrationRedis     *redisclient.Client
)

func TestMain(m *testing.M) {
	os.Exit(testresources.Run(func() int { return runExternalIntegration(m) }, func() int { return runManagedIntegration(m) }))
}

func runExternalIntegration(m *testing.M) int {
	ctx := context.Background()
	if err := timezone.Init("UTC"); err != nil {
		log.Print(err)
		return 1
	}
	address := os.Getenv("AFFINITY_REDIS_TEST_ADDR")
	if err := testresources.ValidateRedis(address); err != nil {
		log.Print(err)
		return 1
	}
	var err error
	integrationDB, err = testresources.OpenPostgres(ctx, os.Getenv("UPSTREAM_JOB_TEST_DSN"), "job_test")
	if err != nil {
		log.Print(err)
		return 1
	}
	defer func() { _ = integrationDB.Close() }()
	if err := ApplyMigrations(ctx, integrationDB); err != nil {
		log.Printf("failed to apply isolated db migrations: %v", err)
		return 1
	}
	integrationEntClient = dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, integrationDB)))
	defer func() { _ = integrationEntClient.Close() }()
	integrationRedis = redisclient.NewClient(&redisclient.Options{Addr: address})
	defer func() { _ = integrationRedis.Close() }()
	if err := integrationRedis.Ping(ctx).Err(); err != nil {
		log.Print("isolated Redis ping failed")
		return 1
	}
	return m.Run()
}

func runManagedIntegration(m *testing.M) int {
	ctx := context.Background()

	if err := timezone.Init("UTC"); err != nil {
		log.Printf("failed to init timezone: %v", err)
		return 1
	}

	if !dockerIsAvailable(ctx) {
		// In CI we expect Docker to be available so integration tests should fail loudly.
		if os.Getenv("CI") != "" {
			log.Printf("docker is not available (CI=true); failing integration tests")
			return 1
		}
		log.Printf("docker is not available; skipping integration tests (start Docker to enable)")
		return 0
	}

	postgresImage := selectDockerImage(ctx, postgresImageTag)
	pgContainer, err := tcpostgres.Run(
		ctx,
		postgresImage,
		tcpostgres.WithDatabase("sub2api_test"),
		tcpostgres.WithUsername("postgres"),
		tcpostgres.WithPassword("postgres"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		log.Printf("failed to start postgres container: %v", err)
		return 1
	}
	defer func() { _ = pgContainer.Terminate(ctx) }()

	redisContainer, err := tcredis.Run(
		ctx,
		redisImageTag,
	)
	if err != nil {
		log.Printf("failed to start redis container: %v", err)
		return 1
	}
	defer func() { _ = redisContainer.Terminate(ctx) }()

	dsn, err := pgContainer.ConnectionString(ctx, "sslmode=disable", "TimeZone=UTC")
	if err != nil {
		log.Printf("failed to get postgres dsn: %v", err)
		return 1
	}

	integrationDB, err = openSQLWithRetry(ctx, dsn, 30*time.Second)
	if err != nil {
		log.Printf("failed to open sql db: %v", err)
		return 1
	}
	if err := ApplyMigrations(ctx, integrationDB); err != nil {
		log.Printf("failed to apply db migrations: %v", err)
		return 1
	}

	// 创建 ent client 用于集成测试
	drv := entsql.OpenDB(dialect.Postgres, integrationDB)
	integrationEntClient = dbent.NewClient(dbent.Driver(drv))

	redisHost, err := redisContainer.Host(ctx)
	if err != nil {
		log.Printf("failed to get redis host: %v", err)
		return 1
	}
	redisPort, err := redisContainer.MappedPort(ctx, "6379/tcp")
	if err != nil {
		log.Printf("failed to get redis port: %v", err)
		return 1
	}

	integrationRedis = redisclient.NewClient(&redisclient.Options{
		Addr: fmt.Sprintf("%s:%d", redisHost, redisPort.Int()),
		DB:   0,
	})
	if err := integrationRedis.Ping(ctx).Err(); err != nil {
		log.Printf("failed to ping redis: %v", err)
		return 1
	}

	code := m.Run()

	_ = integrationEntClient.Close()
	_ = integrationRedis.Close()
	_ = integrationDB.Close()

	return code
}

func dockerIsAvailable(ctx context.Context) bool {
	cmd := exec.CommandContext(ctx, "docker", "info")
	cmd.Env = os.Environ()
	return cmd.Run() == nil
}

// selectDockerImage resolves the container image for the harness.
//
// OMNIHIVE_TEST_POSTGRES_IMAGE overrides the PostgreSQL image so the suite can be
// run against the oldest documented-supported server, not just the newest. That
// matters for SQL that behaves differently across major versions: jsonpath
// .datetime() only accepts the ISO-8601 "Z" designator from PostgreSQL 17 on, so
// a suite pinned to 18 cannot observe breakage on 14-16.
//
//	OMNIHIVE_TEST_POSTGRES_IMAGE=postgres:15-alpine go test -tags integration ./internal/repository/
func selectDockerImage(ctx context.Context, preferred string) string {
	if override := strings.TrimSpace(runtimeenv.Get("OMNIHIVE_TEST_POSTGRES_IMAGE", "SUB2API_TEST_POSTGRES_IMAGE")); override != "" &&
		strings.HasPrefix(preferred, "postgres:") {
		return override
	}
	if dockerImageExists(ctx, preferred) {
		return preferred
	}

	return preferred
}

func dockerImageExists(ctx context.Context, image string) bool {
	cmd := exec.CommandContext(ctx, "docker", "image", "inspect", image)
	cmd.Env = os.Environ()
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run() == nil
}

func openSQLWithRetry(ctx context.Context, dsn string, timeout time.Duration) (*sql.DB, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error

	for time.Now().Before(deadline) {
		db, err := sql.Open("postgres", dsn)
		if err != nil {
			lastErr = err
			time.Sleep(250 * time.Millisecond)
			continue
		}

		if err := pingWithTimeout(ctx, db, 2*time.Second); err != nil {
			lastErr = err
			_ = db.Close()
			time.Sleep(250 * time.Millisecond)
			continue
		}

		return db, nil
	}

	return nil, fmt.Errorf("db not ready after %s: %w", timeout, lastErr)
}

func pingWithTimeout(ctx context.Context, db *sql.DB, timeout time.Duration) error {
	pingCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return db.PingContext(pingCtx)
}

func testTx(t *testing.T) *sql.Tx {
	t.Helper()

	tx, err := integrationDB.BeginTx(context.Background(), nil)
	require.NoError(t, err, "begin tx")
	t.Cleanup(func() {
		_ = tx.Rollback()
	})
	return tx
}

// testEntClient 返回全局的 ent client，用于测试需要内部管理事务的代码（如 Create/Update 方法）。
// 注意：此 client 的操作会真正写入数据库，测试结束后不会自动回滚。
func testEntClient(t *testing.T) *dbent.Client {
	t.Helper()
	return integrationEntClient
}

// testEntTx 返回一个 ent 事务，用于需要事务隔离的测试。
// 测试结束后会自动回滚，不会影响数据库状态。
func testEntTx(t *testing.T) *dbent.Tx {
	t.Helper()

	tx, err := integrationEntClient.Tx(context.Background())
	require.NoError(t, err, "begin ent tx")
	t.Cleanup(func() {
		_ = tx.Rollback()
	})
	return tx
}

func testRedis(t *testing.T) *redisclient.Client {
	t.Helper()
	options := *integrationRedis.Options()
	return testresources.RedisClient(t, &options)
}

func assertTTLWithin(t *testing.T, ttl time.Duration, min, max time.Duration) {
	t.Helper()
	require.GreaterOrEqual(t, ttl, min, "ttl should be >= min")
	require.LessOrEqual(t, ttl, max, "ttl should be <= max")
}

// IntegrationRedisSuite provides a base suite for Redis integration tests.
// Embedding suites should call SetupTest to initialize ctx and rdb.
type IntegrationRedisSuite struct {
	suite.Suite
	ctx context.Context
	rdb *redisclient.Client
}

// SetupTest initializes ctx and rdb for each test method.
func (s *IntegrationRedisSuite) SetupTest() {
	s.ctx = context.Background()
	s.rdb = testRedis(s.T())
}

// RequireNoError is a convenience method wrapping require.NoError with s.T().
func (s *IntegrationRedisSuite) RequireNoError(err error, msgAndArgs ...any) {
	s.T().Helper()
	require.NoError(s.T(), err, msgAndArgs...)
}

// AssertTTLWithin asserts that ttl is within [min, max].
func (s *IntegrationRedisSuite) AssertTTLWithin(ttl, min, max time.Duration) {
	s.T().Helper()
	assertTTLWithin(s.T(), ttl, min, max)
}

// IntegrationDBSuite provides a base suite for DB integration tests.
// Embedding suites should call SetupTest to initialize ctx and client.
type IntegrationDBSuite struct {
	suite.Suite
	ctx    context.Context
	client *dbent.Client
	tx     *dbent.Tx
}

// SetupTest initializes ctx and client for each test method.
func (s *IntegrationDBSuite) SetupTest() {
	s.ctx = context.Background()
	// 统一使用 ent.Tx，确保每个测试都有独立事务并自动回滚。
	tx := testEntTx(s.T())
	s.tx = tx
	s.client = tx.Client()
}

// RequireNoError is a convenience method wrapping require.NoError with s.T().
func (s *IntegrationDBSuite) RequireNoError(err error, msgAndArgs ...any) {
	s.T().Helper()
	require.NoError(s.T(), err, msgAndArgs...)
}
