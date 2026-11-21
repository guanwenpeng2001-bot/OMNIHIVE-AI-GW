package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestImageTaskStoreRoundTripAndTTL(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	store := NewImageTaskStore(rdb)
	task := &service.ImageTaskRecord{
		ID:        "imgtask_123",
		UserID:    7,
		APIKeyID:  9,
		Status:    service.ImageTaskStatusProcessing,
		CreatedAt: 100,
		ExpiresAt: 200,
	}

	require.NoError(t, store.Save(context.Background(), task, 24*time.Hour))
	got, err := store.Get(context.Background(), task.ID)
	require.NoError(t, err)
	require.Equal(t, task, got)
	require.Equal(t, 24*time.Hour, mr.TTL(imageTaskKey(task.ID)))
}

func TestImageTaskStoreMissing(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	store := NewImageTaskStore(rdb)

	_, err := store.Get(context.Background(), "imgtask_missing")
	require.ErrorIs(t, err, service.ErrImageTaskNotFound)
}

// Terminal writes are version-compare-and-set: a stale writer or a repeated
// completion can never rewrite a settled receipt.
func TestImageTaskStoreTerminalWriteCAS(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	store := NewImageTaskStore(rdb)
	ctx := context.Background()

	task := &service.ImageTaskRecord{ID: "imgtask_cas", UserID: 7, APIKeyID: 9, Status: service.ImageTaskStatusProcessing, Version: 1, CreatedAt: 100, ExpiresAt: 200}
	require.NoError(t, store.Save(ctx, task, time.Hour))

	// A duplicate create-shaped write against the live task is a no-op.
	duplicate := &service.ImageTaskRecord{ID: task.ID, UserID: 7, APIKeyID: 9, Status: service.ImageTaskStatusProcessing, Version: 1, CreatedAt: 100, ExpiresAt: 200}
	require.NoError(t, store.Save(ctx, duplicate, time.Hour))
	got, err := store.Get(ctx, task.ID)
	require.NoError(t, err)
	require.Equal(t, int64(1), got.Version)
	require.Equal(t, service.ImageTaskStatusProcessing, got.Status)

	// The terminal write must present exactly the next version.
	completed := *got
	completed.Status = service.ImageTaskStatusCompleted
	completed.Version = got.Version + 2
	require.NoError(t, store.Save(ctx, &completed, time.Hour))
	stillProcessing, err := store.Get(ctx, task.ID)
	require.NoError(t, err)
	require.Equal(t, service.ImageTaskStatusProcessing, stillProcessing.Status, "a skipped version must not settle the task")

	completed.Version = got.Version + 1
	require.NoError(t, store.Save(ctx, &completed, time.Hour))
	settled, err := store.Get(ctx, task.ID)
	require.NoError(t, err)
	require.Equal(t, service.ImageTaskStatusCompleted, settled.Status)

	// A settled task can never be failed by a late writer.
	replayedFailure := *settled
	replayedFailure.Status = service.ImageTaskStatusFailed
	replayedFailure.Version = settled.Version + 1
	require.NoError(t, store.Save(ctx, &replayedFailure, time.Hour))
	after, err := store.Get(ctx, task.ID)
	require.NoError(t, err)
	require.Equal(t, service.ImageTaskStatusCompleted, after.Status)
	require.Equal(t, settled.Version, after.Version)
	activeMembers, zerr := mr.ZMembers("image_task:active")
	if zerr != nil {
		activeMembers = nil // the whole active set is gone once its last member settled
	}
	require.NotContains(t, activeMembers, task.ID, "terminal writes leave the active recovery set")
}

// Admission is bounded across instances: the active set caps in-flight tasks
// and an over-capacity create is rejected instead of acknowledged.
func TestImageTaskStoreAdmissionBoundedByActiveSet(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	store := NewImageTaskStore(rdb)
	ctx := context.Background()

	futureDeadline := float64(time.Now().Add(time.Hour).Unix())
	for i := range 1024 {
		require.NoError(t, rdb.ZAdd(ctx, "image_task:active", redis.Z{Score: futureDeadline, Member: fmt.Sprintf("held_%d", i)}).Err())
	}
	task := &service.ImageTaskRecord{ID: "imgtask_over_capacity", UserID: 7, APIKeyID: 9, Status: service.ImageTaskStatusProcessing, Version: 1, CreatedAt: 100, ExpiresAt: 200}
	require.ErrorIs(t, store.Save(ctx, task, time.Hour), service.ErrImageTaskUnavailable)
	_, err := store.Get(ctx, task.ID)
	require.ErrorIs(t, err, service.ErrImageTaskNotFound, "a rejected admission must not persist a receipt")

	// Members whose deadline elapsed are pruned before the capacity check, so
	// recovered capacity admits new work without operator intervention.
	require.NoError(t, rdb.ZAdd(ctx, "image_task:active", redis.Z{Score: float64(time.Now().Add(-time.Hour).Unix()), Member: "held_0"}).Err())
	recovered := &service.ImageTaskRecord{ID: "imgtask_after_prune", UserID: 7, APIKeyID: 9, Status: service.ImageTaskStatusProcessing, Version: 1, CreatedAt: 100, ExpiresAt: 200}
	require.NoError(t, store.Save(ctx, recovered, time.Hour))
}
