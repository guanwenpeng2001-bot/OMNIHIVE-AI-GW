//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGatewayCacheDiscountPreservesProviderUsageFacts(t *testing.T) {
	logs := &openAIRecordUsageLogRepoStub{inserted: true}
	gateway := newGatewayRecordUsageServiceForTest(logs, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{})
	result := &ForwardResult{RequestID: "cache-discount", Model: "claude-sonnet-4", Usage: ClaudeUsage{InputTokens: 100, OutputTokens: 10, CacheReadInputTokens: 20}}
	facts := result.Usage
	err := gateway.RecordUsage(context.Background(), &RecordUsageInput{Result: result, APIKey: &APIKey{ID: 2}, User: &User{ID: 1}, Account: &Account{ID: 3}, ForceCacheBilling: true})
	require.NoError(t, err)
	require.Equal(t, facts, result.Usage)
	require.Equal(t, facts.InputTokens, logs.lastLog.InputTokens)
	require.Equal(t, facts.CacheReadInputTokens, logs.lastLog.CacheReadTokens)
	var snapshot PricingSnapshot
	require.NoError(t, json.Unmarshal([]byte(logs.lastLog.PricingSnapshot), &snapshot))
	require.Equal(t, 100, snapshot.ProxyCacheDiscountInputTokens)
	require.Equal(t, 120, snapshot.Tokens.CacheReadTokens)
}
