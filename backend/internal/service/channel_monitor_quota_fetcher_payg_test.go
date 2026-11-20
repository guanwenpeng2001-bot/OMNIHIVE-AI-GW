//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// 智谱/稀宇 payg 没有公开余额端点：抓取器必须不发探测请求，直接给出
// unsupported 快照（配置事实），并且不编造余额。
func TestQuotaFetcher_PayGWithoutBalanceEndpointSkipsProbe(t *testing.T) {
	for _, platform := range []string{domain.PlatformZhipu, domain.PlatformMiniMax} {
		t.Run(platform, func(t *testing.T) {
			fetcher, usage, quota, cnBalance, accounts := newQuotaFetcherTestSetup(t)
			accounts.accounts[31] = &Account{
				ID:          31,
				Platform:    platform,
				Credentials: map[string]any{"account_mode": AccountModePayG},
			}
			// 桩若被调用即证明发起了不该发的探测。
			cnBalance.result = &CNProviderBalanceResult{Success: true, Balance: 999, Currency: "CNY"}

			snapshot := fetcher.Fetch(context.Background(), 31)

			require.Zero(t, cnBalance.calls, "不适用余额探针的账号不得发起探测请求")
			require.Zero(t, quota.calls)
			require.Zero(t, usage.getCalls())
			require.False(t, snapshot.Success)
			require.True(t, snapshot.Unsupported)
			require.Equal(t, "cn_balance", snapshot.Source)
			require.Nil(t, snapshot.Balance, "不得展示假的余额")
			require.Empty(t, snapshot.Balances)
			require.False(t, snapshot.CredentialInvalid)
			require.Contains(t, snapshot.Error, "no balance endpoint")
		})
	}
}

// 无观测源是配置问题：推导为 degraded（配置问题）而不是 error（渠道故障），
// 避免「不支持」告警刷屏。
func TestDeriveQuotaCheckResult_UnsupportedSourceIsDegraded(t *testing.T) {
	snapshot := &domain.MonitorQuotaSnapshot{
		Source:      "cn_balance",
		Success:     false,
		Unsupported: true,
		Error:       "no balance endpoint for zhipu payg account; use local usage statistics",
	}
	result := deriveQuotaCheckResult(snapshot, "quota", time.Now())
	require.Equal(t, MonitorStatusDegraded, result.Status)
	require.Contains(t, result.Message, "no balance endpoint")
}

// 有余额端点的 payg 平台（kimi/deepseek）仍走真实探测，未被上面的分支误伤。
func TestQuotaFetcher_PayGWithBalanceEndpointStillProbes(t *testing.T) {
	for _, platform := range []string{domain.PlatformKimi, domain.PlatformDeepseek} {
		t.Run(platform, func(t *testing.T) {
			fetcher, _, _, cnBalance, accounts := newQuotaFetcherTestSetup(t)
			accounts.accounts[32] = &Account{
				ID:          32,
				Platform:    platform,
				Credentials: map[string]any{"account_mode": AccountModePayG},
			}
			cnBalance.result = &CNProviderBalanceResult{Success: true, Available: true, Balance: 12.5, Currency: "CNY"}

			snapshot := fetcher.Fetch(context.Background(), 32)

			require.Equal(t, 1, cnBalance.calls)
			require.True(t, snapshot.Success)
			require.False(t, snapshot.Unsupported)
			require.NotNil(t, snapshot.Balance)
			require.InDelta(t, 12.5, *snapshot.Balance, 0.001)
		})
	}
}
