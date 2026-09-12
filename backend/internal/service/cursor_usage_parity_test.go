//go:build unit

package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
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
			var calls atomic.Int32
			service := &AccountUsageService{cache: NewUsageCache(), cursorSessions: sessions, cursorUsageClient: cursorUsageParityClient(func(request *http.Request) (*http.Response, error) {
				calls.Add(1)
				if request.URL.Path == "/auth/usage" {
					require.Equal(t, http.MethodGet, request.Method)
				} else {
					require.Contains(t, []string{"/aiserver.v1.DashboardService/GetCurrentPeriodUsage", "/aiserver.v1.DashboardService/GetUsageLimitStatusAndActiveGrants"}, request.URL.Path)
					require.Equal(t, http.MethodPost, request.Method)
					require.Equal(t, "1", request.Header.Get("Connect-Protocol-Version"))
				}
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
			require.EqualValues(t, 3, calls.Load())
			_, err := service.GetUsageForAccount(context.Background(), account, true)
			require.NoError(t, err)
			require.EqualValues(t, 6, calls.Load())
		})
	}
}

func TestCursorOfficialUsageAndPartialFailures(t *testing.T) {
	for _, scenario := range []struct {
		name                                     string
		legacyStatus, periodStatus, limitsStatus int
		periodBody                               string
	}{
		{"official", 200, 200, 200, `{"billingCycleStart":"1787936472000","billingCycleEnd":"1790614872000","planUsage":{"totalSpend":49512,"limit":2000,"autoPercentUsed":0,"apiPercentUsed":100,"totalPercentUsed":100}}`},
		{"missing fields", 200, 200, 200, `{}`},
		{"period unauthorized", 200, 401, 200, `{}`},
		{"limits unavailable", 200, 200, 503, `{}`},
		{"legacy unavailable", 503, 200, 200, `{"billingCycleEnd":"1790614872000","planUsage":{"apiPercentUsed":100}}`},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			service := &AccountUsageService{cursorUsageClient: cursorUsageParityClient(func(request *http.Request) (*http.Response, error) {
				status, body := scenario.legacyStatus, `{"gpt-4":{"numRequests":2,"numTokens":70,"maxRequestUsage":null,"maxTokenUsage":null}}`
				switch request.URL.Path {
				case "/aiserver.v1.DashboardService/GetCurrentPeriodUsage":
					status, body = scenario.periodStatus, scenario.periodBody
				case "/aiserver.v1.DashboardService/GetUsageLimitStatusAndActiveGrants":
					status, body = scenario.limitsStatus, `{"usageLimitPolicyStatus":{"isInSlowPool":true,"stage":"LIMIT_HIT_STAGE_SLOW_POOL"}}`
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			result := service.fetchCursorUsageWithToken(context.Background(), &Account{ID: 1}, "test-session")
			require.Empty(t, result.ErrorCode)
			require.NotNil(t, result.CursorMonthly)
			usage := result.CursorMonthly
			require.Equal(t, scenario.periodStatus != 200 || scenario.limitsStatus != 200, usage.DashboardPartial)
			if scenario.legacyStatus == 200 {
				require.EqualValues(t, 70, usage.Models["gpt-4"].NumTokens)
				require.Nil(t, usage.Models["gpt-4"].MaxTokenUsage)
			}
			if scenario.name == "official" {
				require.Equal(t, time.UnixMilli(1790614872000).UTC(), *usage.ResetsAt)
				require.EqualValues(t, 0, *usage.PlanUsage.AutoPercentUsed)
				require.EqualValues(t, 100, *usage.PlanUsage.APIPercentUsed)
				require.True(t, usage.LimitStatus.IsInSlowPool)
			}
			if scenario.name == "missing fields" {
				require.Nil(t, usage.PlanUsage)
				require.Nil(t, usage.ResetsAt)
			}
		})
	}
}
