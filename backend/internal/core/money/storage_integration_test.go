//go:build integration

package money

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil/testresources"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestPostgresNumericRoundTrip(t *testing.T) {
	dsn := os.Getenv("UPSTREAM_JOB_TEST_DSN")
	if dsn == "" {
		t.Skip("requires UPSTREAM_JOB_TEST_DSN")
	}
	ctx := t.Context()
	db, err := testresources.OpenPostgres(ctx, dsn, "job_test")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	for _, tc := range columns {
		t.Run(tc.sql, func(t *testing.T) {
			for _, s := range []string{tc.limit, "-" + tc.limit, "-0", "0.01"} {
				a := amount(t, s)
				v, err := tc.format.Bind(a)
				require.NoError(t, err)
				var result Amount
				require.NoError(t, db.QueryRowContext(ctx, "SELECT $1::"+tc.sql, v).Scan(tc.format.Scanner(&result)))
				require.Equal(t, 0, result.Compare(a))
			}
		})
	}
	m, err := NewMoney("USD", "9007199254740993.00000001")
	require.NoError(t, err)
	var read Money
	require.NoError(t, db.QueryRowContext(ctx, "SELECT $1::jsonb", m).Scan(&read))
	require.Equal(t, m.Currency(), read.Currency())
	require.Equal(t, m.String(), read.String())
}

func TestRedisMoneyRoundTrip(t *testing.T) {
	addr := os.Getenv("AFFINITY_REDIS_TEST_ADDR")
	if addr == "" {
		t.Skip("requires AFFINITY_REDIS_TEST_ADDR")
	}
	require.NoError(t, testresources.ValidateRedis(addr))
	client := testresources.RedisClient(t, &redis.Options{Addr: addr})
	ctx := t.Context()
	type cached struct {
		SchemaVersion string `json:"schema_version"`
		MoneyEncoding string `json:"money_encoding"`
		Balance       Money  `json:"balance"`
	}
	m, err := NewMoney("USD", "9007199254740993.00000001")
	require.NoError(t, err)
	data, err := json.Marshal(cached{"2", "decimal_string", m})
	require.NoError(t, err)
	require.NoError(t, client.Set(ctx, "balance", data, time.Minute).Err())
	bytes, err := client.Get(ctx, "balance").Bytes()
	require.NoError(t, err)
	var got cached
	require.NoError(t, json.Unmarshal(bytes, &got))
	require.Equal(t, "2", got.SchemaVersion)
	require.Equal(t, "decimal_string", got.MoneyEncoding)
	require.Equal(t, m.Currency(), got.Balance.Currency())
	require.Equal(t, m.String(), got.Balance.String())
}
