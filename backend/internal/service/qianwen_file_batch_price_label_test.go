//go:build unit

package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestQianwenBatchFrozenPriceLabelCompatibility(tester *testing.T) {
	job, _ := paidBatchFixture(tester)
	usage, err := validateQianwenBatchResult(job, []byte(strings.ReplaceAll(testBatchResult, QianwenBatchTestModel, "qwen-long")))
	require.NoError(tester, err)
	job.UsageJSON, job.ResultRef = usage, "output"
	job.Billing.KeyQuota, job.Billing.KeyRateLimit, job.Billing.AccountQuota = true, true, true
	currentLog, currentCommand, err := QianwenJobCharge(job)
	require.NoError(tester, err)
	require.Equal(tester, QianwenFileBatchPriceVersion, *currentLog.PriceCardVersion)
	require.Positive(tester, currentLog.ActualCost)

	const frozenLabel = "dashscope-cn-file-batch-2026-09-14"
	job.Billing.FileBatch.PriceVersion = frozenLabel
	frozenLog, frozenCommand, err := QianwenJobCharge(job)
	require.NoError(tester, err)
	require.Equal(tester, currentLog.InputCost, frozenLog.InputCost)
	require.Equal(tester, currentLog.OutputCost, frozenLog.OutputCost)
	require.Equal(tester, currentLog.TotalCost, frozenLog.TotalCost)
	require.Equal(tester, currentLog.ActualCost, frozenLog.ActualCost)
	require.Equal(tester, currentLog.AccountStatsCost, frozenLog.AccountStatsCost)
	require.Equal(tester, currentCommand, frozenCommand)
	require.Equal(tester, frozenLabel, *frozenLog.PriceCardVersion)
}

func TestQianwenBatchFrozenPriceValidation(tester *testing.T) {
	for _, scenario := range []struct {
		name   string
		mutate func(*QianwenFileBatchSnapshot)
	}{
		{name: "input tariff mismatch", mutate: func(snapshot *QianwenFileBatchSnapshot) { snapshot.InputPrice *= 2 }},
		{name: "output tariff mismatch", mutate: func(snapshot *QianwenFileBatchSnapshot) { snapshot.OutputPrice *= 2 }},
		{name: "missing FX", mutate: func(snapshot *QianwenFileBatchSnapshot) { snapshot.FX = nil }},
		{name: "wrong FX region", mutate: func(snapshot *QianwenFileBatchSnapshot) { snapshot.FX.Region = "europe" }},
		{name: "zero input price", mutate: func(snapshot *QianwenFileBatchSnapshot) { snapshot.InputPrice = 0 }},
		{name: "negative output price", mutate: func(snapshot *QianwenFileBatchSnapshot) { snapshot.OutputPrice = -1 }},
	} {
		tester.Run(scenario.name, func(tester *testing.T) {
			job, _ := paidBatchFixture(tester)
			job.Billing.FileBatch.PriceVersion = "dashscope-cn-file-batch-2026-09-14"
			scenario.mutate(job.Billing.FileBatch)
			usageLog, command, err := chargeQianwenFileBatch(job, 20, 6)
			require.ErrorIs(tester, err, ErrUpstreamJobTariff)
			require.Nil(tester, usageLog)
			require.Nil(tester, command)
		})
	}
}
