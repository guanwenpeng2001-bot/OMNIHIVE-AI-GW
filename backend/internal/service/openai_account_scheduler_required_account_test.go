package service

import (
	"context"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/stretchr/testify/require"
)

func TestOpenAIGatewayService_SelectAccountWithScheduler_HonoursRequiredAccount(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	groupID := int64(91031)
	accounts := []Account{
		{ID: 71031, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 0},
		{ID: 71032, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 5},
	}
	cfg := &config.Config{}
	cfg.Gateway.Scheduling.LoadBatchEnabled = false
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
		cache:              &schedulerTestGatewayCache{},
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}
	for _, required := range []int64{71031, 71032} {
		selection, _, err := svc.SelectAccountWithScheduler(WithRequiredAccount(context.Background(), required), &groupID, "", "", "gpt-5.4", nil, OpenAIUpstreamTransportAny, false)
		require.NoError(t, err)
		require.NotNil(t, selection)
		require.Equal(t, required, selection.Account.ID, "admission pinned the verified connection")
		if selection.ReleaseFunc != nil {
			selection.ReleaseFunc()
		}
	}
	_, _, err := svc.SelectAccountWithScheduler(WithRequiredAccount(context.Background(), 79999), &groupID, "", "", "gpt-5.4", nil, OpenAIUpstreamTransportAny, false)
	require.Error(t, err, "a pinned account outside the group must not fall back to another account")
}
