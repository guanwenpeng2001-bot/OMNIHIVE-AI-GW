//go:build unit

package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/cursor"
	"github.com/stretchr/testify/require"
)

func TestCursorMonthlyParsingParity(t *testing.T) {
	usage, err := parseCursorMonthlyUsage(strings.NewReader(`{"startOfMonth":"2026-01-31T17:01:12Z","gpt-4":{"numRequests":2,"numRequestsTotal":3,"numTokens":70,"maxTokenUsage":null,"maxRequestUsage":null}}`))
	require.NoError(t, err)
	require.Equal(t, "2026-02-28T17:01:12Z", usage.ResetsAt.Format(time.RFC3339))
	require.Nil(t, usage.Models["gpt-4"].MaxTokenUsage)
	require.Nil(t, usage.Models["gpt-4"].MaxRequestUsage)
	require.EqualValues(t, 3, usage.Models["gpt-4"].NumRequestsTotal)
	usage, err = parseCursorMonthlyUsage(strings.NewReader(`{"gpt-4":{"numRequests":2,"numTokens":70,"maxTokenUsage":100,"maxRequestUsage":10}}`))
	require.NoError(t, err)
	require.Nil(t, usage.StartOfMonth)
	require.Nil(t, usage.ResetsAt)
	require.EqualValues(t, 100, *usage.Models["gpt-4"].MaxTokenUsage)
	require.EqualValues(t, 10, *usage.Models["gpt-4"].MaxRequestUsage)
	for _, body := range []string{`{"startOfMonth":"invalid"}`, `{"gpt-4":{"numTokens":-1}}`, `{"gpt-4":{"numTokens":"invalid"}}`, `{`} {
		_, err = parseCursorMonthlyUsage(strings.NewReader(body))
		require.Error(t, err)
	}
}

type cursorUsageParityClient func(*http.Request) (*http.Response, error)

func (client cursorUsageParityClient) Do(request *http.Request) (*http.Response, error) {
	return client(request)
}

func TestCursorMonthlyFetchCacheAndErrorsParity(t *testing.T) {
	for _, scenario := range []struct {
		name    string
		status  int
		network bool
		code    string
	}{
		{"success", 200, false, ""}, {"unauthorized", 401, false, "unauthenticated"}, {"network", 0, true, "network_error"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			account := &Account{ID: 71, Platform: PlatformCursor, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test-key"}}
			sessions := NewCursorSessionProvider(newCursorMemoryCache())
			sessions.exchange = func(context.Context, string) (cursor.Session, error) {
				return cursor.Session{Token: "test-session", ExpiresAt: time.Now().Add(time.Hour)}, nil
			}
			calls := 0
			service := &AccountUsageService{cache: NewUsageCache(), cursorSessions: sessions, cursorUsageClient: cursorUsageParityClient(func(request *http.Request) (*http.Response, error) {
				calls++
				require.Equal(t, "https://api2.cursor.sh/auth/usage", request.URL.String())
				require.Equal(t, http.MethodGet, request.Method)
				require.Equal(t, "Bearer test-session", request.Header.Get("Authorization"))
				if scenario.network {
					return nil, errors.New("network failure with sensitive details")
				}
				return &http.Response{StatusCode: scenario.status, Body: io.NopCloser(strings.NewReader(`{"gpt-4":{"numRequests":2,"numTokens":4,"maxTokenUsage":null,"maxRequestUsage":null}}`))}, nil
			})}
			for range 2 {
				usage, err := service.GetUsageForAccount(context.Background(), account)
				require.NoError(t, err)
				require.Equal(t, scenario.code, usage.ErrorCode)
				require.NotContains(t, usage.Error, "sensitive")
				if scenario.code == "" {
					require.EqualValues(t, 4, usage.CursorMonthly.Models["gpt-4"].NumTokens)
				}
			}
			require.Equal(t, 1, calls)
			_, err := service.GetUsageForAccount(context.Background(), account, true)
			require.NoError(t, err)
			require.Equal(t, 2, calls)
		})
	}
}
