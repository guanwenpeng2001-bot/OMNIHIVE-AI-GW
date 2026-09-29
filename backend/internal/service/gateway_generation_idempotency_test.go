package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// These tests prove local dispatch/charge deduplication, not exactly-once
// upstream effects. A vendor can execute before a connection/process fails;
// retained claims cannot undo that execution and must not regenerate it.
func TestGatewayGenerationReplayAndSettledEvidence(t *testing.T) {
	for _, status := range []int{200, 202, 400} {
		t.Run(time.Duration(status).String(), func(t *testing.T) {
			coordinator := NewIdempotencyCoordinator(newInMemoryIdempotencyRepo(), DefaultIdempotencyConfig())
			scope := GatewayGenerationScope{WorkspaceID: 8, APIKeyID: 19, Action: "openai.video.create"}
			claim, err := coordinator.BeginGatewayGeneration(context.Background(), scope, "request-one", []byte(`{"prompt":"hello"}`))
			require.NoError(t, err)
			charges := 1
			response := GatewayGenerationResponse{Status: status, ContentType: "application/json", State: "completed", Body: []byte(`{"id":"original-job"}`), ReceiptKey: UsageSettlementKey{RequestID: "receipt-one", APIKeyID: 19, UserID: 7}, ReceiptState: UsageSettlementSettled}
			require.NoError(t, claim.Complete(context.Background(), response))
			for retry := 0; retry < 3; retry++ {
				replayed, err := coordinator.BeginGatewayGeneration(context.Background(), scope, "request-one", []byte(`{"prompt":"hello"}`))
				require.NoError(t, err)
				if replayed.Replay == nil {
					charges++
				}
				require.Equal(t, response, *replayed.Replay)
			}
			require.Equal(t, 1, charges)
			_, err = coordinator.BeginGatewayGeneration(context.Background(), scope, "request-one", []byte(`{"prompt":"different"}`))
			require.ErrorIs(t, err, ErrIdempotencyKeyConflict)
		})
	}
}

func TestGatewayGenerationInflightConcurrentAndRetained(t *testing.T) {
	repo := newInMemoryIdempotencyRepo()
	coordinator := NewIdempotencyCoordinator(repo, DefaultIdempotencyConfig())
	scope := GatewayGenerationScope{APIKeyID: 19, Action: "openai.responses"}
	claim, err := coordinator.BeginGatewayGeneration(context.Background(), scope, "one", []byte("body"))
	require.NoError(t, err)
	var waiting sync.WaitGroup
	errors := make(chan error, 20)
	for attempt := 0; attempt < 20; attempt++ {
		waiting.Add(1)
		go func() {
			defer waiting.Done()
			_, err := coordinator.BeginGatewayGeneration(context.Background(), scope, "one", []byte("body"))
			errors <- err
		}()
	}
	waiting.Wait()
	close(errors)
	for err := range errors {
		require.ErrorIs(t, err, ErrIdempotencyInProgress)
	}
	deleted, err := repo.DeleteExpired(context.Background(), time.Now().AddDate(100, 0, 0), 500)
	require.NoError(t, err)
	require.Zero(t, deleted)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	require.NoError(t, claim.Complete(canceled, GatewayGenerationResponse{Status: 200, State: "interrupted"}))
	// Recreate the coordinator to model process restart and config rollback.
	coordinator = NewIdempotencyCoordinator(repo, DefaultIdempotencyConfig())
	replay, err := coordinator.BeginGatewayGeneration(context.Background(), scope, "one", []byte("body"))
	require.NoError(t, err)
	require.Equal(t, "interrupted", replay.Replay.State)
	deleted, err = repo.DeleteExpired(context.Background(), time.Now().AddDate(100, 0, 0), 500)
	require.NoError(t, err)
	require.Zero(t, deleted)
}

func TestGatewayGenerationScopeIsolationAndNoCredentials(t *testing.T) {
	repo := newInMemoryIdempotencyRepo()
	coordinator := NewIdempotencyCoordinator(repo, DefaultIdempotencyConfig())
	for _, scope := range []GatewayGenerationScope{{1, 1, "openai.responses"}, {2, 1, "openai.responses"}, {1, 2, "openai.responses"}, {1, 1, "openai.video"}} {
		claim, err := coordinator.BeginGatewayGeneration(context.Background(), scope, "raw-idempotency-key", []byte(`{"password":"request-secret"}`))
		require.NoError(t, err)
		require.Nil(t, claim.Replay)
		require.NoError(t, claim.Complete(context.Background(), GatewayGenerationResponse{Status: 200, Body: []byte(`{"access_token":"response-secret"}`)}))
		replay, err := coordinator.BeginGatewayGeneration(context.Background(), scope, "raw-idempotency-key", []byte(`{"password":"request-secret"}`))
		require.NoError(t, err)
		require.Equal(t, "response_unavailable", replay.Replay.State)
		require.Empty(t, replay.Replay.Body)
	}
	for _, record := range repo.data {
		require.NotContains(t, *record.ResponseBody, "response-secret")
		require.NotContains(t, record.RequestFingerprint, "request-secret")
		require.NotContains(t, record.IdempotencyKeyHash, "raw-idempotency-key")
	}
}

func TestGatewayGenerationStoreFailureNeverReclaims(t *testing.T) {
	var coordinator *IdempotencyCoordinator
	_, err := coordinator.BeginGatewayGeneration(context.Background(), GatewayGenerationScope{APIKeyID: 1, Action: "openai.responses"}, "one", nil)
	require.ErrorIs(t, err, ErrIdempotencyStoreUnavail)
	repo := newInMemoryIdempotencyRepo()
	coordinator = NewIdempotencyCoordinator(repo, DefaultIdempotencyConfig())
	scope := GatewayGenerationScope{APIKeyID: 1, Action: "openai.responses"}
	_, err = coordinator.BeginGatewayGeneration(context.Background(), scope, "one", nil)
	require.NoError(t, err)
	for _, record := range repo.data {
		record.Status = IdempotencyStatusFailedRetryable
		record.ExpiresAt = time.Now().Add(-time.Hour)
	}
	_, err = coordinator.BeginGatewayGeneration(context.Background(), scope, "one", nil)
	require.ErrorContains(t, err, "IDEMPOTENCY_OUTCOME_UNKNOWN")
}
