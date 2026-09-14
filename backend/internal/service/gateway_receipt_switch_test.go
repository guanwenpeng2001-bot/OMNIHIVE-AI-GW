//go:build unit

package service

import (
	"context"
	"testing"

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
