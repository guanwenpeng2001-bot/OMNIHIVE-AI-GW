//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func openCodeQuotaAccount() *Account {
	return &Account{ID: 55, Platform: PlatformOpenCodeGo, Type: AccountTypeAPIKey,
		Status: StatusActive, Schedulable: true, Credentials: map[string]any{"api_key": "test-only"}, Extra: map[string]any{}}
}

func TestOpenCodePaymentClassification(t *testing.T) {
	for _, status := range []int{402, 401} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			repo := &rateLimitAccountRepoStub{}
			limiter := NewRateLimitService(repo, nil, nil, nil, nil)
			require.True(t, limiter.HandleUpstreamError(context.Background(), openCodeQuotaAccount(), status, http.Header{}, []byte(`{"error":{"message":"Insufficient account funds"}}`)))
			if status == 402 {
				require.Zero(t, repo.setErrorCalls)
				require.Equal(t, 1, repo.tempCalls)
				require.Contains(t, repo.lastTempReason, "Insufficient account funds")
			} else {
				require.Equal(t, 1, repo.setErrorCalls)
			}
		})
	}
}

func TestOpenCodeAccountTestPaymentAllProtocols(t *testing.T) {
	for _, protocol := range []string{APIProtocolChatCompletions, APIProtocolAnthropic, APIProtocolResponses} {
		t.Run(protocol, func(t *testing.T) {
			account := openCodeGoTestAccount(55)
			account.Credentials["api_protocol"] = protocol
			if protocol == APIProtocolAnthropic {
				account.Credentials["base_url"] = "https://opencode.ai/zen/go"
			}
			before := shallowCopyMap(account.Credentials)
			svc, upstream := adaptiveCNAccountTestService(account, &http.Response{StatusCode: 402, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"Insufficient account funds"}}`))})
			repo := &rateLimitAccountRepoStub{mockAccountRepoForGemini: mockAccountRepoForGemini{accountsByID: map[int64]*Account{55: account}}}
			svc.accountRepo = repo
			ctx, recorder := newTestContext()
			require.Error(t, svc.TestAccountConnection(ctx, 55, "kimi-k3", "hello", AccountTestModeDefault))
			require.Len(t, upstream.requests, 1)
			require.Contains(t, recorder.Body.String(), "402")
			require.Contains(t, recorder.Body.String(), "Insufficient account funds")
			require.Equal(t, 1, repo.tempCalls)
			require.Zero(t, repo.setErrorCalls)
			require.Equal(t, before, account.Credentials)
		})
	}
}

func TestCommandCodeKeyChangeInvalidatesObservedPlanAndCredits(t *testing.T) {
	account := &Account{Platform: PlatformCommandCode, Extra: map[string]any{
		"commandcode_plan_level": "old plan", "commandcode_monthlyCredits": 10,
		"commandcode_purchasedCredits": 5, "commandcode_freeCredits": 0,
		"commandcode_usage_supported": true, "commandcode_usage_checked_at": "old",
		"commandcode_period_basis": "monthly", "commandcode_totalCount": 50,
		"commandcode_totalTokens": 100, "commandcode_totalCredits": 20,
		"configured_option": "preserved",
	}}
	ClearAccountRuntimeObservations(account)
	require.Equal(t, map[string]any{"configured_option": "preserved"}, account.Extra)
}

func TestOpenCodeMonthlyExhaustionIgnoresAvailableRollingWindow(t *testing.T) {
	account := openCodeQuotaAccount()
	now := time.Now().UTC().Truncate(time.Second)
	monthly := now.Add(10 * 24 * time.Hour)
	account.Extra = cnQuotaExtraUpdates(PlatformOpenCodeGo, []CNQuotaTier{
		{Window: "5h", UsedPercent: 0, ResetAt: now.Add(time.Hour).Format(time.RFC3339)},
		{Window: "weekly", UsedPercent: 0, ResetAt: now.Add(24 * time.Hour).Format(time.RFC3339)},
		{Window: "monthly", UsedPercent: 100, ResetAt: monthly.Format(time.RFC3339)},
	}, now)
	require.Equal(t, &monthly, openCodeExhaustedQuotaReset(account, now))
	require.Equal(t, &monthly, cnProviderQuotaSnapshotReset(account, now))
	require.Nil(t, openCodeExhaustedQuotaReset(account, monthly.Add(time.Second)))
}

func TestOpenCodeQuotaRecoveryPreservesOperatorAndAuthenticationState(t *testing.T) {
	now := time.Now().UTC()
	reset := now.Add(10 * 24 * time.Hour).Truncate(time.Second)
	result := &CNProviderQuotaProbeResult{Success: true, CredentialValid: true, StatusCode: 200,
		Tiers: []CNQuotaTier{{Window: "5h"}, {Window: "weekly"}, {Window: "monthly", UsedPercent: 100, ResetAt: reset.Format(time.RFC3339)}}}
	for _, test := range []struct {
		name, status, message string
		schedulable, recover  bool
	}{
		{"legacy empty error", StatusError, "", true, true},
		{"legacy payment error", StatusError, "Payment required (402): Insufficient account funds", true, true},
		{"real unauthorized", StatusError, "Authentication failed (HTTP 401)", true, false},
		{"real forbidden", StatusError, "Authentication failed (HTTP 403)", true, false},
		{"operator inactive", "inactive", "", false, false},
		{"operator paused", StatusActive, "", false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			account := openCodeQuotaAccount()
			account.Status, account.ErrorMessage, account.Schedulable = test.status, test.message, test.schedulable
			expected := *account
			ReconcileOpenCodeQuotaProbe(account, &expected, result, now)
			require.Equal(t, test.schedulable, account.Schedulable)
			if test.recover {
				require.Equal(t, StatusActive, account.Status)
				require.Equal(t, &reset, account.TempUnschedulableUntil)
			} else {
				require.Equal(t, test.status, account.Status)
				require.Equal(t, test.message, account.ErrorMessage)
			}
		})
	}
}

func TestOpenCodeQuotaRecoveryRequiresApplicableEvidence(t *testing.T) {
	now := time.Now()
	for _, test := range []struct {
		name    string
		result  CNProviderQuotaProbeResult
		changed bool
	}{
		{"failed probe", CNProviderQuotaProbeResult{StatusCode: 500}, false},
		{"empty success", CNProviderQuotaProbeResult{Success: true, CredentialValid: true}, false},
		{"newer auth error", CNProviderQuotaProbeResult{Success: true, CredentialValid: true, Tiers: []CNQuotaTier{{Window: "monthly", UsedPercent: 100}}}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			account := openCodeQuotaAccount()
			account.Status = StatusError
			expected := *account
			if test.changed {
				account.ErrorMessage = "newer 401"
			}
			ReconcileOpenCodeQuotaProbe(account, &expected, &test.result, now)
			require.Equal(t, StatusError, account.Status)
		})
	}
	account := openCodeQuotaAccount()
	until := now.Add(time.Hour)
	account.TempUnschedulableUntil, account.TempUnschedulableReason = &until, openCodePaymentReason+": insufficient funds"
	expected := *account
	available := &CNProviderQuotaProbeResult{Success: true, CredentialValid: true, Tiers: []CNQuotaTier{{Window: "5h"}, {Window: "weekly"}, {Window: "monthly"}}}
	ReconcileOpenCodeQuotaProbe(account, &expected, available, now)
	require.Equal(t, &until, account.TempUnschedulableUntil, "usage success is not generation success")
	account.TempUnschedulableReason = cnQuotaExhaustedReasonPrefix
	expected = *account
	ReconcileOpenCodeQuotaProbe(account, &expected, available, now)
	require.Nil(t, account.TempUnschedulableUntil, "fresh full quota snapshot can recover a quota-owned pause")
	account.RateLimitResetAt, account.RateLimitedAt = &until, &now
	account.Extra[cnExtraKey(PlatformOpenCodeGo, cnExtraSuffixMonthlyReset)] = until.Format(time.RFC3339Nano)
	expected = *account
	ReconcileOpenCodeQuotaProbe(account, &expected, available, now)
	require.Nil(t, account.RateLimitResetAt, "fresh available snapshot clears its own old quota rate limit")
}

func TestOpenCodeKeyChangeInvalidatesQuotaOnly(t *testing.T) {
	account := openCodeQuotaAccount()
	reset := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	account.Extra = cnQuotaExtraUpdates(PlatformOpenCodeGo, []CNQuotaTier{{Window: "monthly", UsedPercent: 100, ResetAt: reset.Format(time.RFC3339)}}, time.Now())
	account.Extra["configured_option"] = true
	account.TempUnschedulableReason, account.TempUnschedulableUntil = openCodePaymentReason, &reset
	account.RateLimitResetAt, account.RateLimitedAt = &reset, &reset
	account.Schedulable = false
	account.Status, account.ErrorMessage = StatusError, "real 401"
	oldIdentity := AccountRuntimeIdentityKey(account)
	account.Credentials["api_key"] = "replacement-test-only"
	require.NotEqual(t, oldIdentity, AccountRuntimeIdentityKey(account))
	ClearAccountRuntimeObservations(account)
	require.NotContains(t, account.Extra, "opencode_go_monthly_used_percent")
	require.Nil(t, account.TempUnschedulableUntil)
	require.Nil(t, account.RateLimitResetAt)
	require.Equal(t, true, account.Extra["configured_option"])
	require.Equal(t, StatusError, account.Status)
	require.False(t, account.Schedulable)
	account.TempUnschedulableReason, account.TempUnschedulableUntil = "operator reason", &reset
	ClearAccountRuntimeObservations(account)
	require.Equal(t, &reset, account.TempUnschedulableUntil)
}

type openCodeQuotaProbeRepo struct {
	AccountRepository
	current *Account
	writes  int
}

func (r *openCodeQuotaProbeRepo) ApplyOpenCodeQuotaProbe(_ context.Context, expected *Account, result *CNProviderQuotaProbeResult) error {
	if AccountRuntimeIdentityKey(r.current) != AccountRuntimeIdentityKey(expected) {
		return ErrAccountRuntimeIdentityChanged
	}
	r.writes++
	ReconcileOpenCodeQuotaProbe(r.current, expected, result, time.Now())
	return nil
}

func TestOpenCodeQuotaProbeUsesFreshIdentityAndPreservesHTTPFailure(t *testing.T) {
	for _, status := range []int{200, 401, 403, 402, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			account := openCodeQuotaAccount()
			account.Status = StatusError
			repo := &openCodeQuotaProbeRepo{current: account}
			body := `{"usage":{"rolling":{"percent":0},"weekly":{"percent":0},"monthly":{"percent":100,"resetsAt":"2099-10-11T21:28:32Z"}},"error":"upstream explanation"}`
			svc := NewCNProviderQuotaService(repo, nil, &cnBalanceResponseUpstream{statusCode: status, body: body}, nil)
			result, err := svc.QueryUsageForAccount(context.Background(), snapshotOAuthRefreshAccount(account))
			require.NoError(t, err)
			require.Equal(t, status, result.StatusCode)
			if status == 200 {
				require.True(t, result.Persisted)
				require.True(t, result.CredentialValid)
				require.Equal(t, StatusActive, account.Status)
				require.NotNil(t, account.TempUnschedulableUntil)
			} else {
				require.Contains(t, result.Error, "upstream explanation")
				require.Equal(t, StatusError, account.Status)
			}
			stale := snapshotOAuthRefreshAccount(account)
			account.Credentials["api_key"] = "new-key"
			before := repo.writes
			result, err = svc.QueryUsageForAccount(context.Background(), stale)
			require.NoError(t, err)
			require.False(t, result.Persisted)
			require.Equal(t, before, repo.writes)
		})
	}
}
