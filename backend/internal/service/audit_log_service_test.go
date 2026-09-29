package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Unimplemented legacy methods deliberately panic if ClearAll uses them.
type auditClearRepository struct {
	AuditLogRepository
	clear func(context.Context, *AuditLog) (int64, error)
}

func (r *auditClearRepository) ClearWithTrace(ctx context.Context, trace *AuditLog) (int64, error) {
	return r.clear(ctx, trace)
}

func TestAuditClearAllRequiresTrace(t *testing.T) {
	s := &AuditLogService{repo: &auditClearRepository{}}
	deleted, err := s.ClearAll(context.Background(), nil)
	require.ErrorContains(t, err, "trace is required")
	require.Zero(t, deleted)
}

func TestAuditClearAllUsesAtomicRepository(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failure"}[fail], func(t *testing.T) {
			cause := errors.New("trace insert failed")
			trace := &AuditLog{ActorEmail: "admin@example.test", Extra: map[string]any{"reason": "cleanup"}}
			ctx := context.Background()
			called := false
			s := &AuditLogService{repo: &auditClearRepository{clear: func(gotCtx context.Context, got *AuditLog) (int64, error) {
				called = true
				require.Equal(t, ctx, gotCtx)
				require.Same(t, trace, got)
				require.Equal(t, AuditActionAuditLogClear, got.Action)
				require.WithinDuration(t, time.Now(), got.CreatedAt, time.Second)
				require.Equal(t, "admin@example.test", got.ActorEmail)
				require.Equal(t, "cleanup", got.Extra["reason"])
				if fail {
					return 0, cause
				}
				return 5, nil
			}}}
			deleted, err := s.ClearAll(ctx, trace)
			require.True(t, called)
			if fail {
				require.ErrorIs(t, err, cause)
				require.Zero(t, deleted)
			} else {
				require.NoError(t, err)
				require.EqualValues(t, 5, deleted)
			}
		})
	}
}
