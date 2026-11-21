//go:build unit

package service

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyService_AuthLookupFailureBurstSharesOneRetryableError(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	var calls atomic.Int32
	repo := &authRepoStub{getByKeyForAuth: func(context.Context, string) (*APIKey, error) {
		calls.Add(1)
		once.Do(func() { close(started) })
		<-release
		return nil, context.DeadlineExceeded
	}}
	svc := NewAPIKeyService(repo, nil, nil, nil, nil, nil, &config.Config{APIKeyAuth: config.APIKeyAuthCacheConfig{LookupConcurrency: 1, Singleflight: true, L2TTLSeconds: 60}})
	const requests = 32
	start := make(chan struct{})
	errs := make(chan error, requests)
	var callers sync.WaitGroup
	callers.Add(requests)
	for range requests {
		go func() {
			defer callers.Done()
			<-start
			_, err := svc.GetByKey(context.Background(), "shared")
			errs <- err
		}()
	}
	close(start)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("repository lookup did not start")
	}
	time.Sleep(20 * time.Millisecond)
	close(release)
	callers.Wait()
	close(errs)
	for err := range errs {
		require.ErrorIs(t, err, context.DeadlineExceeded)
	}
	require.Equal(t, int32(1), calls.Load())
	require.Zero(t, svc.AuthLookupMetrics().Rejected)
}
