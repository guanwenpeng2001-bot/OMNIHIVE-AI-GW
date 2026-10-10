package service

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCommandCodeUsageParsing(t *testing.T) {
	for _, tc := range []struct {
		name, window string
		count        int
		percent      float64
	}{
		{"normal", `"used":2.8,"cap":14,"resetAt":1789288541747`, 1, 20},
		{"zero", `"used":0,"cap":14`, 1, 0}, {"over cap", `"used":21,"cap":14`, 1, 150},
		{"missing used", `"cap":14`, 0, 0}, {"missing cap", `"used":2`, 0, 0}, {"zero cap", `"used":0,"cap":0`, 0, 0},
		{"negative", `"used":-1,"cap":14`, 0, 0}, {"null", `"used":null,"cap":14`, 0, 0},
		{"nan", `"used":"NaN","cap":14`, 0, 0}, {"infinite", `"used":1,"cap":"Inf"`, 0, 0},
		{"invalid reset", `"used":"7","cap":"14","resetAt":"bad"`, 1, 50},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tiers := parseCommandCodeUsageTiers([]byte(`{"windowLimits":{"limited":true,"fiveHour":{` + tc.window + `}}}`))
			require.Len(t, tiers, tc.count)
			if tc.count > 0 {
				require.InDelta(t, tc.percent, tiers[0].UsedPercent, 0.0001)
				if tc.name == "normal" {
					require.Equal(t, "2026-09-13T08:35:41Z", tiers[0].ResetAt)
				} else {
					require.Empty(t, tiers[0].ResetAt)
				}
			}
		})
	}
	for _, body := range []string{`{}`, `null`, `{broken`, `{"windowLimits":{"limited":false,"fiveHour":{"used":0,"cap":14}}}`} {
		require.Empty(t, parseCommandCodeUsageTiers([]byte(body)))
	}
}
func TestCommandCodeAlphaURL(t *testing.T) {
	u, err := commandCodeAlphaURL("https://proxy.example/prefix/provider/v1?secret=ignore", "billing/credits")
	require.NoError(t, err)
	require.Equal(t, "https://proxy.example/prefix/alpha/billing/credits", u)
}
func TestCommandCodeUnsupportedDoesNotInventQuotaOrChangeHealth(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
	}{{401, `{}`}, {403, `{}`}, {404, `{}`}, {200, `{}`}, {200, `{"windowLimits":{"limited":true,"fiveHour":{"cap":14}}}`}, {200, strings.Repeat(" ", cnQuotaMaxBodyBytes+1)}} {
		account := &Account{ID: 20, Platform: PlatformCommandCode, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test", "base_url": "https://proxy.example/provider/v1"}}
		account.Extra = map[string]any{
			"commandcode_usage_supported": true,
			"commandcode_5h_used_percent": 100.0,
			"commandcode_5h_reset_at":     time.Now().Add(time.Hour).Format(time.RFC3339),
		}
		repo := &cnBalanceProbeRepo{account: account}
		svc := NewCNProviderQuotaService(repo, nil, &commandCodeResponseUpstream{statusCode: tc.status, body: tc.body}, nil)
		result, err := svc.QueryUsage(context.Background(), 20)
		require.NoError(t, err)
		require.False(t, result.Success)
		require.True(t, result.Unsupported)
		require.Empty(t, result.Tiers)
		require.Len(t, repo.extraWrites, 1)
		require.Equal(t, false, repo.extraWrites[0]["commandcode_usage_supported"])
		require.Nil(t, repo.extraWrites[0]["commandcode_5h_used_percent"])
		for key, value := range repo.extraWrites[0] {
			account.Extra[key] = value
		}
		require.False(t, EvaluateAccountSchedulingThreshold(account, map[string]int{PlatformCommandCode: 90}, time.Now()).ShouldPause)
	}
}
func TestCommandCodePersistsOnlyObservedWindows(t *testing.T) {
	account := &Account{ID: 20, Platform: PlatformCommandCode, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test", "base_url": "https://proxy.example/provider/v1"}}
	repo := &cnBalanceProbeRepo{account: account}
	svc := NewCNProviderQuotaService(repo, nil, &commandCodeResponseUpstream{statusCode: 200, body: `{"credits":{"monthlyCredits":57},"windowLimits":{"limited":true,"weekly":{"used":7,"cap":35}}}`}, nil)
	result, err := svc.QueryUsage(context.Background(), 20)
	require.NoError(t, err)
	require.True(t, result.Success)
	require.True(t, result.Persisted)
	require.Equal(t, float64(20), repo.extraWrites[0]["commandcode_weekly_used_percent"])
	require.Nil(t, repo.extraWrites[0]["commandcode_5h_used_percent"])
	require.Nil(t, repo.extraWrites[0]["commandcode_weekly_reset_at"])
	require.Equal(t, map[string]float64{"monthlyCredits": 57}, result.Credits)
	require.Empty(t, result.PlanLevel)
}

// A transport timeout is a usage availability failure, not an account failure.
type commandCodeTimeoutUpstream struct {
	cnBalanceResponseUpstream
	t *testing.T
}

func (u *commandCodeTimeoutUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	require.True(u.t, strings.HasPrefix(req.URL.String(), "https://proxy.example/alpha/"))
	require.Equal(u.t, http.MethodGet, req.Method)
	require.Equal(u.t, "test", req.Header.Get("x-api-key"))
	deadline, ok := req.Context().Deadline()
	require.True(u.t, ok)
	require.LessOrEqual(u.t, time.Until(deadline), cnQuotaUpstreamTimeout)
	return nil, context.DeadlineExceeded
}
func TestCommandCodeTransportTimeoutIsUnsupported(t *testing.T) {
	account := &Account{ID: 20, Platform: PlatformCommandCode, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test", "base_url": "https://proxy.example/provider/v1"}}
	repo := &cnBalanceProbeRepo{account: account}
	svc := NewCNProviderQuotaService(repo, nil, &commandCodeTimeoutUpstream{t: t}, nil)
	result, err := svc.QueryUsage(context.Background(), 20)
	require.NoError(t, err)
	require.True(t, result.Unsupported)
	require.False(t, result.Success)
	require.Empty(t, result.Tiers)
	require.Equal(t, false, repo.extraWrites[0]["commandcode_usage_supported"])
}

// Serve fixtures through a fake transport: no credentials, sockets or upstream calls.
type commandCodeUsageFixture struct {
	cnBalanceResponseUpstream
	handler http.Handler
}

func (u *commandCodeUsageFixture) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	recorder := httptest.NewRecorder()
	u.handler.ServeHTTP(recorder, req)
	return recorder.Result(), nil
}

func TestCommandCodePartialObservations(t *testing.T) {
	for _, tc := range []struct {
		name, credits, subscription, summary string
		creditsStatus, subscriptionStatus    int
		wantPlan                             string
		wantCredits                          map[string]float64
		wantWindows                          int
		wantSuccess                          bool
	}{
		{name: "windows and plan", credits: `{"credits":{"monthlyCredits":18.9},"windowLimits":{"limited":true,"fiveHour":{"used":0,"cap":14},"weekly":{"used":7,"cap":35}}}`, subscription: `{"success":true,"data":{"planId":"individual-goat"}}`, wantPlan: "individual-goat", wantCredits: map[string]float64{"monthlyCredits": 18.9}, wantWindows: 2, wantSuccess: true},
		{name: "credits without windows", credits: `{"credits":{"monthlyCredits":0,"purchasedCredits":2.5,"freeCredits":null}}`, wantCredits: map[string]float64{"monthlyCredits": 0, "purchasedCredits": 2.5}, wantSuccess: true},
		{name: "subscription unavailable preserves credits", credits: `{"credits":{"monthlyCredits":11}}`, subscriptionStatus: 503, wantCredits: map[string]float64{"monthlyCredits": 11}, wantSuccess: true},
		{name: "credits unavailable preserves plan", creditsStatus: 503, subscription: `{"success":true,"data":{"planId":"individual-goat"}}`, wantPlan: "individual-goat", wantSuccess: true},
		{name: "invalid numbers stay unknown", credits: `{"credits":{"monthlyCredits":-1,"purchasedCredits":"NaN","freeCredits":true}}`, wantSuccess: false},
		{name: "empty plan stays unknown", subscription: `{"success":true,"data":{"planId":"  "}}`, wantSuccess: false},
		{name: "summary independently observed", summary: `{"periodBasis":"billing","totalCount":0,"totalTokens":12,"totalCredits":null}`, wantSuccess: true},
		{name: "unauthorized observes no health", creditsStatus: 401, subscriptionStatus: 401, wantSuccess: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := &Account{ID: 20, Platform: PlatformCommandCode, Type: AccountTypeAPIKey, Status: StatusActive, Credentials: map[string]any{"api_key": "fixture", "base_url": "https://fixture.example/provider/v1"}}
			repo := &cnBalanceProbeRepo{account: account}
			var requests atomic.Int32
			upstream := &commandCodeUsageFixture{handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				require.Equal(t, http.MethodGet, r.Method)
				require.Equal(t, "fixture", r.Header.Get("x-api-key"))
				status, body := 200, "{}"
				switch r.URL.Path {
				case "/alpha/billing/credits":
					body = tc.credits
					if tc.creditsStatus != 0 {
						status = tc.creditsStatus
					}
				case "/alpha/billing/subscriptions":
					body = tc.subscription
					if tc.subscriptionStatus != 0 {
						status = tc.subscriptionStatus
					}
				case "/alpha/usage/summary":
					body = tc.summary
				default:
					t.Fatalf("unexpected path %s", r.URL.Path)
				}
				if body == "" {
					body = "{}"
				}
				w.WriteHeader(status)
				_, _ = w.Write([]byte(body))
			})}
			result, err := NewCNProviderQuotaService(repo, nil, upstream, nil).QueryUsage(context.Background(), account.ID)
			require.NoError(t, err)
			require.Equal(t, int32(3), requests.Load())
			require.Equal(t, tc.wantSuccess, result.Success)
			require.Equal(t, !tc.wantSuccess, result.Unsupported)
			require.Equal(t, tc.wantSuccess, result.CredentialValid)
			require.Equal(t, tc.wantPlan, result.PlanLevel)
			require.Len(t, result.Tiers, tc.wantWindows)
			require.Len(t, result.Credits, len(tc.wantCredits))
			for key, value := range tc.wantCredits {
				require.Equal(t, value, result.Credits[key])
			}
			require.Equal(t, StatusActive, account.Status)
			require.Len(t, repo.extraWrites, 1)
			updates := repo.extraWrites[0]
			require.Nil(t, updates["commandcode_freeCredits"])
			require.Nil(t, updates["commandcode_5h_reset_at"])
			require.Nil(t, updates["commandcode_weekly_reset_at"])
			if tc.wantWindows == 0 {
				require.Nil(t, updates["commandcode_5h_used_percent"])
				require.Nil(t, updates["commandcode_weekly_used_percent"])
			}
			if tc.wantPlan == "" {
				require.Nil(t, updates["commandcode_plan_level"])
			}
			if tc.summary != "" {
				require.Equal(t, "billing", updates["commandcode_period_basis"])
				require.Equal(t, float64(0), updates["commandcode_totalCount"])
				require.Equal(t, float64(12), updates["commandcode_totalTokens"])
			} else {
				require.Nil(t, updates["commandcode_period_basis"])
				require.Nil(t, updates["commandcode_totalCount"])
				require.Nil(t, updates["commandcode_totalTokens"])
			}
			require.Nil(t, updates["commandcode_totalCredits"])
		})
	}
}

func TestCommandCodeObservationsRejectStaleIdentity(t *testing.T) {
	for _, tc := range []struct{ name, key, body string }{
		{"successful observation", "old-fixture", `{"credits":{"monthlyCredits":57}}`},
		{"unsupported observation", "old-fixture", `{}`},
		{"missing key observation", "", `{}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old := &Account{ID: 20, Platform: PlatformCommandCode, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": tc.key, "base_url": "https://fixture.example/provider/v1"}}
			current := *old
			current.Credentials = map[string]any{"api_key": "new-fixture", "base_url": "https://fixture.example/provider/v1"}
			repo := &commandCodeSnapshotFixture{cnBalanceProbeRepo: cnBalanceProbeRepo{account: &current}}
			svc := NewCNProviderQuotaService(repo, nil, &commandCodeResponseUpstream{statusCode: 200, body: tc.body}, nil)
			result, err := svc.queryCommandCodeUsage(context.Background(), old)
			require.NoError(t, err)
			require.False(t, result.Persisted)
			require.False(t, result.Success)
			require.True(t, result.Unsupported)
			require.Empty(t, result.Credits)
			require.Empty(t, result.PlanLevel)
			require.Empty(t, repo.extraWrites)
			require.Equal(t, 1, repo.attempts)
		})
	}
}

type commandCodeSnapshotFixture struct {
	cnBalanceProbeRepo
	attempts int
}

func (repo *commandCodeSnapshotFixture) UpdateAccountRuntimeSnapshot(ctx context.Context, expected *Account, updates map[string]any, _ *time.Time) error {
	repo.attempts++
	if AccountRuntimeIdentityKey(expected) != AccountRuntimeIdentityKey(repo.account) {
		return ErrAccountRuntimeIdentityChanged
	}
	return repo.UpdateExtra(ctx, expected.ID, updates)
}

func TestCommandCodeParallelFetchCreditsGate(t *testing.T) {
	for _, creditsSucceed := range []bool{false, true} {
		t.Run(fmt.Sprintf("credits_success_%t", creditsSucceed), func(t *testing.T) {
			account := &Account{ID: 20, Platform: PlatformCommandCode, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test", "base_url": "https://fixture.example/provider/v1"}}
			repo := &cnBalanceProbeRepo{account: account}
			arrived := make(chan string, 3)
			release := make(chan struct{})
			allConcurrent := make(chan bool, 1)
			go func() {
				timer := time.NewTimer(300 * time.Millisecond)
				defer timer.Stop()
				defer close(release)
				paths := make(map[string]bool)
				for len(paths) < 3 {
					select {
					case path := <-arrived:
						paths[path] = true
					case <-timer.C:
						allConcurrent <- false
						return
					}
				}
				allConcurrent <- true
			}()
			upstream := &commandCodeUsageFixture{handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				arrived <- request.URL.Path
				<-release
				// Distinct statuses expose last-writer races; the diagnostic status must
				// belong to credits regardless of which optional response finishes last.
				if strings.HasSuffix(request.URL.Path, "/credits") {
					if creditsSucceed {
						writer.WriteHeader(http.StatusOK)
						_, _ = writer.Write([]byte(`{"credits":{"monthlyCredits":12},"windowLimits":{"limited":true,"weekly":{"used":2,"cap":10}}}`))
					} else {
						writer.WriteHeader(http.StatusUnauthorized)
					}
				} else {
					writer.WriteHeader(http.StatusServiceUnavailable)
				}
			})}
			result, err := NewCNProviderQuotaService(repo, nil, upstream, nil).queryCommandCodeUsage(context.Background(), account)
			require.NoError(t, err)
			require.True(t, <-allConcurrent, "all three GETs must start before any completes")
			require.Equal(t, creditsSucceed, result.Success)
			require.Equal(t, !creditsSucceed, result.Unsupported)
			require.Len(t, repo.extraWrites, 1)
			if creditsSucceed {
				require.Equal(t, http.StatusOK, result.StatusCode)
				require.Len(t, result.Tiers, 1)
				require.Equal(t, float64(12), repo.extraWrites[0]["commandcode_monthlyCredits"])
			} else {
				require.Equal(t, http.StatusUnauthorized, result.StatusCode)
				require.Empty(t, result.Tiers)
			}
		})
	}
}

// CommandCode now calls the same transport concurrently. The balance fixture's
// mutable last-request counters are deliberately not used by these read-only GETs.
type commandCodeResponseUpstream struct {
	cnBalanceResponseUpstream
	statusCode int
	body       string
}

func (upstream *commandCodeResponseUpstream) Do(_ *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return &http.Response{
		StatusCode: upstream.statusCode,
		Body:       io.NopCloser(strings.NewReader(upstream.body)),
		Header:     make(http.Header),
	}, nil
}
