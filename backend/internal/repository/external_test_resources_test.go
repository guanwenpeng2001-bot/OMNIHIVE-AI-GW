//go:build unit || integration

package repository

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/testutil/testresources"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestExternalTestResourceMode(test *testing.T) {
	if !testresources.External() {
		test.Skip("set SUB2API_TEST_EXTERNAL_SERVICES=1 and allocate isolated services")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	connectionString := os.Getenv("UPSTREAM_JOB_TEST_DSN")
	address := os.Getenv("AFFINITY_REDIS_TEST_ADDR")
	require.NoError(test, testresources.ValidatePostgres(connectionString, "job_test"))
	require.NoError(test, testresources.ValidateRedis(address))
	test.Run("reject_implicit_and_overridden_connections", func(test *testing.T) {
		for _, invalid := range []string{
			"", "dbname=job_test", "postgres://tester@localhost:5433/sub2api",
			"postgres://tester@localhost/job_test", "postgres://tester@localhost:0/job_test", "postgres://tester@localhost:65536/job_test", "postgres://tester@remote.invalid:5433/job_test",
			"postgres://tester@localhost:5433/job_test?dbname=sub2api",
			"postgres://tester@localhost:5433/job_test?host=remote.invalid",
			"postgres://tester@localhost:5433/job_test?port=5432",
			"postgres://tester@localhost:5433/job_test?options=-csearch_path=public",
			"postgres://tester@localhost:5433/job_test?search_path=public",
			"postgres://tester@localhost:5433/job_test?sslmode=disable&sslmode=require",
		} {
			require.Error(test, testresources.ValidatePostgres(invalid, "job_test"))
			connection, err := testresources.OpenPostgres(ctx, invalid, "job_test")
			require.Error(test, err)
			require.Nil(test, connection)
		}
		for _, invalid := range []string{"", "localhost", "localhost:", "localhost:0", "localhost:65536", "localhost:abc", "remote.invalid:6380", "redis://localhost:6380"} {
			require.Error(test, testresources.ValidateRedis(invalid))
		}
		require.Error(test, testresources.ValidatePostgres(connectionString, "sub2api"))
	})

	test.Run("accept_explicit_local_ports_without_connecting", func(test *testing.T) {
		// Validation only: never connect to the developer's default-port services.
		for _, valid := range []string{
			"postgres://tester@localhost:5432/job_test",
			"postgres://tester@127.0.0.1:5433/job_test?sslmode=disable",
			"postgresql://tester@localhost:15432/job_test?TimeZone=UTC&connect_timeout=5",
		} {
			require.NoError(test, testresources.ValidatePostgres(valid, "job_test"))
		}
		require.NoError(test, testresources.ValidatePostgres("postgres://tester@localhost:5432/stats_test", "stats_test"))
		for _, valid := range []string{"localhost:6379", "127.0.0.1:6380", "localhost:16379"} {
			require.NoError(test, testresources.ValidateRedis(valid))
		}
	})
	test.Run("external_lifecycle_never_enters_managed_callbacks", func(test *testing.T) {
		callbacks := map[string]int{"start": 0, "stop": 0, "create_database": 0, "drop_database": 0}
		code := testresources.Run(func() int { return 0 }, func() int {
			for operation := range callbacks {
				callbacks[operation]++
			}
			return 1
		})
		require.Zero(test, code)
		for operation, calls := range callbacks {
			require.Zero(test, calls, operation)
		}
	})
	test.Run("default_lifecycle_is_preserved", func(test *testing.T) {
		test.Setenv("SUB2API_TEST_EXTERNAL_SERVICES", "")
		managedCalls := 0
		require.Equal(test, 17, testresources.Run(func() int { test.Fatal("unexpected external callback"); return 1 }, func() int { managedCalls++; return 17 }))
		require.Equal(test, 1, managedCalls)
	})
	test.Run("reject_environment_routing_override", func(test *testing.T) {
		for _, name := range []string{"PGHOSTADDR", "PGSERVICE", "PGSERVICEFILE", "PGOPTIONS"} {
			test.Run(name, func(test *testing.T) {
				test.Setenv(name, "unsupported-override")
				require.Error(test, testresources.ValidatePostgres(connectionString, "job_test"))
			})
		}
	})
	test.Run("postgres_owns_only_its_fixture_schema", func(test *testing.T) {
		connection, err := testresources.OpenPostgres(ctx, connectionString, "job_test")
		require.NoError(test, err)
		defer func() { _ = connection.Close() }()
		neighbor := testresources.OpenSchema(test, connectionString)
		_, err = neighbor.ExecContext(ctx, "CREATE TABLE retained_fixture(value integer); INSERT INTO retained_fixture VALUES (73)")
		require.NoError(test, err)
		var schema string
		test.Run("fixture", func(test *testing.T) {
			fixture := testresources.OpenSchema(test, connectionString)
			require.NoError(test, fixture.QueryRowContext(ctx, "SELECT current_schema()").Scan(&schema))
			require.True(test, strings.HasPrefix(schema, "test_external_"))
			_, err := fixture.ExecContext(ctx, "CREATE TABLE owned_fixture(value integer); INSERT INTO owned_fixture VALUES (42)")
			require.NoError(test, err)
			var value int
			require.NoError(test, fixture.QueryRowContext(ctx, "SELECT value FROM owned_fixture").Scan(&value))
			require.Equal(test, 42, value)
		})
		var remaining int
		require.NoError(test, connection.QueryRowContext(ctx, "SELECT count(*) FROM pg_namespace WHERE nspname=$1", schema).Scan(&remaining))
		require.Zero(test, remaining)
		var retained int
		require.NoError(test, neighbor.QueryRowContext(ctx, "SELECT value FROM retained_fixture").Scan(&retained))
		require.Equal(test, 73, retained)
	})
	test.Run("redis_cleanup_preserves_neighbor_namespace", func(test *testing.T) {
		raw := redis.NewClient(&redis.Options{Addr: address})
		defer func() { _ = raw.Close() }()
		neighbor := "test:isolated-resource-adaptation:neighbor:" + uuid.NewString()
		require.NoError(test, raw.Set(ctx, neighbor, "preserve", time.Minute).Err())
		defer raw.Del(context.Background(), neighbor)
		var ownedKeys []string
		test.Run("fixture", func(test *testing.T) {
			client := testresources.RedisClient(test, &redis.Options{Addr: address})
			key := "owned:" + uuid.NewString()
			require.NoError(test, client.Set(ctx, key, "fixture", time.Minute).Err())
			var err error
			ownedKeys, err = raw.Keys(ctx, "test:isolated-resource-adaptation:*:"+key).Result()
			require.NoError(test, err)
			require.Len(test, ownedKeys, 1)
			require.NoError(test, testresources.ClearRedisNamespace(ctx, raw, strings.TrimSuffix(ownedKeys[0], key)))
			require.Equal(test, "preserve", raw.Get(ctx, neighbor).Val())
			require.Equal(test, int64(0), raw.Exists(ctx, ownedKeys...).Val())
			require.NoError(test, client.Set(ctx, key, "cleanup fixture", time.Minute).Err())
			require.Error(test, client.FlushDB(ctx).Err())
			require.Error(test, client.FlushAll(ctx).Err())
			require.Error(test, client.Do(ctx, "rename", key, "unscoped-destination").Err())
		})
		require.Equal(test, int64(0), raw.Exists(ctx, ownedKeys...).Val())
		require.Equal(test, "preserve", raw.Get(ctx, neighbor).Val())
	})
}
