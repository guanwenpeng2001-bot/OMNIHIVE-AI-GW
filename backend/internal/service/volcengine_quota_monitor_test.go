//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVolcengineQuotaMonitorCredentialBoundary(t *testing.T) {
	for _, status := range []string{"ok", "permission_denied", "invalid_credentials", "missing_credentials", "unsupported_profile"} {
		t.Run(status, func(t *testing.T) {
			fetcher, usage, quota, balance, accounts := newQuotaFetcherTestSetup(t)
			a := volcengineUsageAccount()
			accounts.accounts[a.ID] = a
			quota.result = &CNProviderQuotaProbeResult{Success: status == "ok", Status: status, StatusCode: 403, Unsupported: status == "unsupported_profile", Tiers: []CNQuotaTier{{Window: "5h", UsedPercent: 20}, {Window: "weekly", UsedPercent: 30}, {Window: "monthly", UsedPercent: 40}}}
			snapshot := fetcher.Fetch(context.Background(), a.ID)
			require.Equal(t, 1, quota.calls)
			require.Zero(t, usage.getCalls())
			require.Zero(t, balance.calls)
			require.False(t, snapshot.CredentialInvalid)
			require.Equal(t, quota.result.Unsupported, snapshot.Unsupported)
			require.Equal(t, status == "ok", snapshot.Success)
			if snapshot.Success {
				require.Len(t, snapshot.Tiers, 3)
			}
		})
	}
}

func TestByteplusQuotaMonitorCredentialBoundary(test *testing.T) {
	for _, status := range []string{"ok", "permission_denied", "invalid_credentials", "missing_credentials", "unsupported_profile"} {
		test.Run(status, func(test *testing.T) {
			fetcher, usage, quota, balance, accounts := newQuotaFetcherTestSetup(test)
			account := byteplusUsageAccount()
			accounts.accounts[account.ID] = account
			quota.result = &CNProviderQuotaProbeResult{Success: status == "ok", Status: status, StatusCode: 403, Unsupported: status == "unsupported_profile", Tiers: []CNQuotaTier{{Window: "5h", UsedPercent: 20}, {Window: "weekly", UsedPercent: 30}, {Window: "monthly", UsedPercent: 40}}}
			snapshot := fetcher.Fetch(context.Background(), account.ID)
			require.Equal(test, 1, quota.calls)
			require.Zero(test, usage.getCalls())
			require.Zero(test, balance.calls)
			require.False(test, snapshot.CredentialInvalid)
			require.Equal(test, quota.result.Unsupported, snapshot.Unsupported)
			require.Equal(test, status == "ok", snapshot.Success)
			if snapshot.Success {
				require.Len(test, snapshot.Tiers, 3)
			}
		})
	}
}
