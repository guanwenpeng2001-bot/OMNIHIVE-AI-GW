//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/stretchr/testify/require"
)

func TestGatewayReceiptSwitchSingleFinancialProducer(t *testing.T) {
	for _, platform := range []string{PlatformAnthropic, PlatformOpenAI} {
		for _, missing := range []bool{false, true} {
			t.Run(platform+map[bool]string{false: "_priced", true: "_unpriced"}[missing], func(t *testing.T) {
				logs := &openAIRecordUsageLogRepoStub{}
				oldBilling := &openAIRecordUsageBillingRepoStub{}
				users := &openAIRecordUsageUserRepoStub{}
				receipts := &settlementCapture{}
				settlement := NewUsageSettlementService(receipts, nil, nil)
				key, user, account := &APIKey{ID: 2, Quota: 100}, &User{ID: 1}, &Account{ID: 3, Platform: platform}
				var err error
				if platform == PlatformAnthropic {
					s := newGatewayRecordUsageServiceWithBillingRepoForTest(logs, oldBilling, users, &openAIRecordUsageSubRepoStub{})
					s.usageSettlement = settlement
					model := "claude-sonnet-4"
					if missing {
						model = "missing-receipt-model"
					}
					err = s.RecordUsage(context.Background(), &RecordUsageInput{Result: &ForwardResult{RequestID: "original", Model: model, Usage: ClaudeUsage{InputTokens: 10, OutputTokens: 6}}, APIKey: key, User: user, Account: account, QuotaPlatform: platform})
				} else {
					s := newOpenAIRecordUsageServiceForTest(logs, users, &openAIRecordUsageSubRepoStub{}, nil)
					s.usageBillingRepo = oldBilling
					s.usageSettlement = settlement
					model := "gpt-5.1"
					if missing {
						model = "missing-receipt-model"
					}
					err = s.RecordUsage(context.Background(), &OpenAIRecordUsageInput{Result: &OpenAIForwardResult{RequestID: "original", Model: model, Usage: OpenAIUsage{InputTokens: 10, OutputTokens: 6}}, APIKey: key, User: user, Account: account, QuotaPlatform: platform})
				}
				if missing {
					require.ErrorIs(t, err, ErrUsageSettlementNeedsReview)
					require.Zero(t, receipts.settles)
				} else {
					require.NoError(t, err)
					require.Equal(t, 1, receipts.settles)
					require.Greater(t, receipts.receipt.Command.BalanceCost, 0.0)
				}
				require.NotNil(t, receipts.receipt)
				require.Zero(t, logs.calls, "receipt transaction owns the only formal log")
				require.Zero(t, oldBilling.calls, "legacy Apply must not also charge")
				require.Zero(t, users.deductCalls, "fallback must not also charge")
			})
		}
	}
}

func TestOpenAIFreeFastMissingPricingRetainsReceipt(t *testing.T) {
	for _, runMode := range []string{config.RunModeStandard, config.RunModeSimple} {
		t.Run(runMode, func(testingContext *testing.T) {
			assertOpenAIFreeFastMissingPricingReceipt(testingContext, runMode)
		})
	}
}

func assertOpenAIFreeFastMissingPricingReceipt(testingContext *testing.T, runMode string) {
	testingContext.Helper()
	logs := &openAIRecordUsageLogRepoStub{}
	oldBilling := &openAIRecordUsageBillingRepoStub{}
	users := &openAIRecordUsageUserRepoStub{}
	receipts := &settlementCapture{}
	gateway := newOpenAIRecordUsageServiceForTest(logs, users, &openAIRecordUsageSubRepoStub{}, nil)
	gateway.cfg.RunMode = runMode
	gateway.usageBillingRepo = oldBilling
	gateway.usageSettlement = NewUsageSettlementService(receipts, nil, nil)
	tier := "priority"
	key := &APIKey{ID: 2, Group: &Group{Platform: PlatformOpenAI, FreeOpenAIFast: true}}
	err := gateway.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
		Result: &OpenAIForwardResult{RequestID: "free-fast-unpriced", Model: "missing-receipt-model", ServiceTier: &tier, Usage: OpenAIUsage{InputTokens: 10, OutputTokens: 6}},
		APIKey: key, User: &User{ID: 1}, Account: &Account{ID: 3, Platform: PlatformOpenAI}, QuotaPlatform: PlatformOpenAI,
	})
	require.ErrorIs(testingContext, err, ErrUsageSettlementNeedsReview)
	require.NotNil(testingContext, receipts.receipt)
	require.Equal(testingContext, PricingStateUnpriced, receipts.receipt.Usage.PricingState)
	require.Equal(testingContext, UsageSettlementNeedsReview, receipts.receipt.State)
	require.Equal(testingContext, 10, receipts.receipt.Usage.InputTokens)
	require.Equal(testingContext, 6, receipts.receipt.Usage.OutputTokens)
	require.Zero(testingContext, receipts.settles)
	require.Zero(testingContext, logs.calls)
	require.Zero(testingContext, oldBilling.calls)
	require.Zero(testingContext, users.deductCalls)
}

func TestOpenAIFreeFastSimplePricedUsageRemainsUnbilled(testingContext *testing.T) {
	logs := &openAIRecordUsageLogRepoStub{inserted: true}
	oldBilling := &openAIRecordUsageBillingRepoStub{}
	users := &openAIRecordUsageUserRepoStub{}
	receipts := &settlementCapture{}
	gateway := newOpenAIRecordUsageServiceForTest(logs, users, &openAIRecordUsageSubRepoStub{}, nil)
	gateway.cfg.RunMode = config.RunModeSimple
	gateway.usageBillingRepo = oldBilling
	gateway.usageSettlement = NewUsageSettlementService(receipts, nil, nil)
	serviceTier := "priority"
	apiKey := &APIKey{ID: 2, Group: &Group{Platform: PlatformOpenAI, FreeOpenAIFast: true}}
	recordErr := gateway.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
		Result: &OpenAIForwardResult{RequestID: "free-fast-simple-priced", Model: "gpt-5.1", ServiceTier: &serviceTier, Usage: OpenAIUsage{InputTokens: 10, OutputTokens: 6}},
		APIKey: apiKey, User: &User{ID: 1}, Account: &Account{ID: 3, Platform: PlatformOpenAI}, QuotaPlatform: PlatformOpenAI,
	})
	require.NoError(testingContext, recordErr)
	require.Equal(testingContext, 1, logs.calls)
	require.Greater(testingContext, logs.lastLog.ActualCost, 0.0)
	require.Nil(testingContext, receipts.receipt)
	require.Zero(testingContext, receipts.settles)
	require.Zero(testingContext, oldBilling.calls)
	require.Zero(testingContext, users.deductCalls)
}
