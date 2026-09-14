package integration

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// Exercise the database and Redis probes together without enabling monitoring.
func TestOpsReadinessUsesDBAndRedisWithoutMonitoring(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { mock.ExpectClose(); require.NoError(t, db.Close()) })
	r := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: r.Addr(), MaxRetries: -1})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	c := service.NewOpsMetricsCollector(nil, nil, nil, nil, db, client, nil)
	mock.ExpectQuery("SELECT 1").WillReturnRows(sqlmock.NewRows([]string{"one"}).AddRow(1))
	require.True(t, c.DependenciesReady(context.Background()))
	mock.ExpectQuery("SELECT 1").WillReturnError(errors.New("FAKE_SENSITIVE_BILLING_MARKER"))
	require.False(t, c.DependenciesReady(context.Background()))
	r.SetError("FAKE_SENSITIVE_BILLING_MARKER")
	mock.ExpectQuery("SELECT 1").WillReturnRows(sqlmock.NewRows([]string{"one"}).AddRow(1))
	require.False(t, c.DependenciesReady(context.Background()))
	require.NoError(t, mock.ExpectationsWereMet())
}
