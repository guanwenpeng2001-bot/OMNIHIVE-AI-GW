//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/cursor"
	"github.com/stretchr/testify/require"
)

func TestCursorAuthorizationPersistsOnceAndErasesVerifier(t *testing.T) {
	service := NewCursorAuthService()
	calls := 0
	writes := 0
	service.poll = func(context.Context, cursor.PKCE) (*cursor.OAuthTokens, error) {
		calls++
		return &cursor.OAuthTokens{AccessToken: "synthetic-access", RefreshToken: "synthetic-refresh", ExpiresAt: time.Now().Add(time.Hour)}, nil
	}
	state, err := service.Start(func(ctx context.Context, credentials map[string]any) (int64, error) {
		writes++
		require.Equal(t, "synthetic-access", credentials["access_token"])
		require.Equal(t, "synthetic-refresh", credentials["refresh_token"])
		require.Len(t, credentials["machine_id"], 64)
		require.Len(t, credentials["mac_machine_id"], 64)
		require.NotEqual(t, credentials["machine_id"], credentials["mac_machine_id"])
		return 42, nil
	})
	require.NoError(t, err)
	require.Equal(t, "waiting", state.Status)
	for range 3 {
		state, err = service.Poll(context.Background(), state.ID, false)
		require.NoError(t, err)
		require.Equal(t, "success", state.Status)
		require.Equal(t, int64(42), state.AccountID)
	}
	require.Equal(t, 1, calls)
	require.Equal(t, 1, writes)
	require.Empty(t, service.sessions[state.ID].pkce.Verifier)
	require.Nil(t, service.sessions[state.ID].persist)
}

func TestCursorAuthorizationBoundedPollingCancellationTimeoutAndFailure(t *testing.T) {
	for _, outcome := range []string{"cancelled", "timeout", "failed", "save_failed"} {
		t.Run(outcome, func(t *testing.T) {
			service := NewCursorAuthService()
			now := time.Now()
			service.now = func() time.Time { return now }
			calls := 0
			service.poll = func(context.Context, cursor.PKCE) (*cursor.OAuthTokens, error) { calls++; return nil, nil }
			state, err := service.Start(func(context.Context, map[string]any) (int64, error) { return 0, errors.New("synthetic save failure") })
			require.NoError(t, err)
			for range 3 {
				state, err = service.Poll(context.Background(), state.ID, false)
				require.NoError(t, err)
				require.Equal(t, "waiting", state.Status)
			}
			require.Equal(t, 1, calls)
			now = now.Add(cursorAuthorizationInterval)
			switch outcome {
			case "timeout":
				now = now.Add(cursorAuthorizationLifetime)
			case "failed":
				service.poll = func(context.Context, cursor.PKCE) (*cursor.OAuthTokens, error) {
					return nil, errors.New("secret must never be returned")
				}
			case "save_failed":
				service.poll = func(context.Context, cursor.PKCE) (*cursor.OAuthTokens, error) {
					return &cursor.OAuthTokens{AccessToken: "synthetic", RefreshToken: "synthetic", ExpiresAt: now.Add(time.Hour)}, nil
				}
			}
			state, err = service.Poll(context.Background(), state.ID, outcome == "cancelled")
			require.NoError(t, err)
			want := outcome
			if outcome == "save_failed" {
				want = "failed"
			}
			require.Equal(t, want, state.Status)
			require.NotContains(t, state.Message, "secret")
			require.Empty(t, service.sessions[state.ID].pkce.Verifier)
		})
	}
}

func TestCursorAuthorizationCancelsUpstreamRequest(t *testing.T) {
	service := NewCursorAuthService()
	service.poll = func(ctx context.Context, _ cursor.PKCE) (*cursor.OAuthTokens, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	state, err := service.Start(func(context.Context, map[string]any) (int64, error) {
		t.Fatal("cancelled login persisted")
		return 0, nil
	})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	state, err = service.Poll(ctx, state.ID, false)
	require.NoError(t, err)
	require.Equal(t, "cancelled", state.Status)
}
