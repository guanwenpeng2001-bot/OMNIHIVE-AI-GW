package service

import (
	"context"
	"encoding/json"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestIdempotencyReplayPreservesFinancialNumbers(t *testing.T) {
	repo := newInMemoryIdempotencyRepo()
	cfg := DefaultIdempotencyConfig()
	coordinator := NewIdempotencyCoordinator(repo, cfg)
	opts := IdempotencyExecuteOptions{Scope: "billing.precision", Method: "POST", Route: "/billing", ActorScope: "admin:1", RequireKey: true, IdempotencyKey: "large-subscription", Payload: map[string]any{"id": int64(9007199254740993)}}
	calls := 0
	execute := func(context.Context) (any, error) {
		calls++
		return map[string]any{"items": []any{map[string]any{"subscription_id": int64(9007199254740993), "max_id": int64(9223372036854775807), "amount": json.Number("0.1234567890123456789")}}, "count": 1}, nil
	}
	first, err := coordinator.Execute(context.Background(), opts, execute)
	require.NoError(t, err)
	replay, err := NewIdempotencyCoordinator(repo, cfg).Execute(context.Background(), opts, execute)
	require.NoError(t, err)
	require.True(t, replay.Replayed)
	require.Equal(t, 1, calls)
	firstJSON, err := json.Marshal(first.Data)
	require.NoError(t, err)
	replayJSON, err := json.Marshal(replay.Data)
	require.NoError(t, err)
	require.Equal(t, string(firstJSON), string(replayJSON))
}

func TestIdempotencyReplayRejectsTrailingJSON(t *testing.T) {
	coordinator := NewIdempotencyCoordinator(nil, DefaultIdempotencyConfig())
	for _, body := range []string{`{"id":1} {"id":2}`, `{"id":1} garbage`, `{"id":1} null`} {
		_, err := coordinator.decodeStoredResponse(&body)
		require.Error(t, err)
	}
}
