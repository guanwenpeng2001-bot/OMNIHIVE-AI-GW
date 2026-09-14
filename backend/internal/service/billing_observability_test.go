package service

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestBillingFailuresAreCorrelatedAndRedacted(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	secret := errors.New("FAKE_SENSITIVE_BILLING_MARKER credential=never-log")
	svc := NewUsageSettlementService(nil, nil, nil)
	err := svc.persistenceError(UsageSettlementKey{RequestID: "billing-request"}, secret)
	require.ErrorIs(t, err, secret)
	require.NotContains(t, err.Error(), "FAKE_SENSITIVE_BILLING_MARKER")
	ctx := context.WithValue(context.Background(), ctxkey.RequestID, "http-entry")
	logJobEvent(ctx, &UpstreamJob{PublicRequestID: "job-1", Family: "realtime_turn", UpstreamTaskID: "turn-1", AttemptCount: 2, Billing: UpstreamJobBilling{Realtime: &QianwenRealtimeSnapshot{SessionID: "session-1"}}}, billingFailureCategory(secret))
	for _, value := range []string{"http-entry", "job-1", "session-1", "turn-1", "dependency_unavailable", "billing-request"} {
		require.Contains(t, output.String(), value)
	}
	require.NotContains(t, output.String(), "FAKE_SENSITIVE_BILLING_MARKER")
	require.Equal(t, "conflict", billingFailureCategory(ErrUsageBillingRequestConflict))
}

func TestOpsReadinessUsesDBAndRedisWithoutMonitoring(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { mock.ExpectClose(); require.NoError(t, db.Close()) })
	r := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: r.Addr(), MaxRetries: -1})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	c := &OpsMetricsCollector{db: db, redisClient: client}
	mock.ExpectQuery("SELECT 1").WillReturnRows(sqlmock.NewRows([]string{"one"}).AddRow(1))
	require.True(t, c.DependenciesReady(context.Background()))
	mock.ExpectQuery("SELECT 1").WillReturnError(errors.New("FAKE_SENSITIVE_BILLING_MARKER"))
	require.False(t, c.DependenciesReady(context.Background()))
	r.SetError("FAKE_SENSITIVE_BILLING_MARKER")
	mock.ExpectQuery("SELECT 1").WillReturnRows(sqlmock.NewRows([]string{"one"}).AddRow(1))
	require.False(t, c.DependenciesReady(context.Background()))
	require.NoError(t, mock.ExpectationsWereMet())
}
