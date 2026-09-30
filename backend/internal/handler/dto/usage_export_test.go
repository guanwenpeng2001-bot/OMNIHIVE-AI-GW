package dto

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func usageExportTestAdminLog() *AdminUsageLog {
	upstreamModel := "gpt-5.1-upstream"
	upstreamResponseModel := "gpt-5.1-2026-01-01"
	upstreamReasoning := "high"
	userAgent := "=SUM(A1:A2)"
	ipAddress := "@10.0.0.1"
	return &AdminUsageLog{
		UsageLog: UsageLog{
			ID:                    7,
			UserID:                11,
			APIKeyID:              13,
			AccountID:             17,
			RequestID:             "req-1",
			Model:                 "gpt-5.1",
			UpstreamResponseModel: &upstreamResponseModel,
			ReasoningEffort:       stringPtr("medium"),
			InboundEndpoint:       stringPtr("/v1/chat/completions"),
			UpstreamEndpoint:      stringPtr("/v1/responses"),
			InputTokens:           120,
			OutputTokens:          30,
			CacheReadTokens:       5,
			CacheCreationTokens:   6,
			InputCost:             0.0000004,
			OutputCost:            0,
			TotalCost:             0.00123456,
			ActualCost:            0.001,
			RateMultiplier:        0.5,
			RequestType:           "stream",
			BillingMode:           stringPtr("token"),
			FirstTokenMs:          intPtr(120),
			DurationMs:            intPtr(2400),
			UserAgent:             &userAgent,
			CreatedAt:             time.Date(2026, 9, 22, 10, 30, 0, 0, time.UTC),
			User:                  &User{ID: 11, Email: "owner@example.com"},
			APIKey:                &APIKey{ID: 13, Name: "primary"},
			Group:                 &Group{ID: 3, Name: "pro"},
		},
		UpstreamModel:           &upstreamModel,
		UpstreamReasoningEffort: &upstreamReasoning,
		UpstreamRequestID:       stringPtr("up-req-1"),
		AccountRateMultiplier:   floatPtr(1.5),
		AccountStatsCost:        floatPtr(0.002),
		Account:                 &AccountSummary{ID: 17, Name: "acc-1"},
		IPAddress:               &ipAddress,
	}
}

func TestUsageExportColumnContracts(t *testing.T) {
	admin := UsageExportColumns(UsageExportScopeAdmin)
	require.Equal(t, []string{
		"time", "user", "api_key", "account", "model", "upstream_model",
		"upstream_response_model", "upstream_model_mismatch",
		"requested_reasoning_effort", "reasoning_effort", "group",
		"inbound_endpoint", "upstream_endpoint", "request_type",
		"input_tokens", "output_tokens", "cache_read_tokens", "cache_creation_tokens",
		"input_cost", "output_cost", "cache_read_cost", "cache_creation_cost",
		"rate_multiplier", "account_rate_multiplier", "total_cost", "actual_cost",
		"account_billed_cost", "first_token_ms", "duration_ms", "request_id",
		"upstream_request_id", "user_agent", "ip_address",
	}, admin[:33])
	require.Equal(t, 15, len(admin)-33, "admin export keeps the fifteen billing evidence columns")

	self := UsageExportColumns(UsageExportScopeSelf)
	require.Equal(t, []string{
		"time", "api_key", "model", "upstream_response_model", "reasoning_effort", "inbound_endpoint",
		"ip_address", "request_type", "billing_mode", "input_tokens", "output_tokens",
		"cache_read_tokens", "cache_creation_tokens", "rate_multiplier",
		"actual_cost", "total_cost", "first_token_ms", "duration_ms",
	}, self[:18])
	require.Equal(t, 15, len(self)-18)
	require.NotContains(t, self, "account", "the owner export never exposes the account identity")
	require.NotContains(t, self, "account_billed_cost", "the owner export never exposes account cost")
	require.NotContains(t, self, "upstream_request_id")
	require.NotContains(t, self, "user_agent")
	require.NotContains(t, self, "user", "the owner export is already scoped to one user")
}

func TestBuildAdminUsageExportRowUsesLedgerPrecisionAndStableCodes(t *testing.T) {
	row := BuildAdminUsageExportRow(usageExportTestAdminLog(), "UTC")
	require.Len(t, row, len(UsageExportColumns(UsageExportScopeAdmin)))

	require.Equal(t, "2026-09-22 10:30:00 (UTC, GMT+00:00)", row[0])
	require.Equal(t, "owner@example.com", row[1])
	require.Equal(t, "primary", row[2])
	require.Equal(t, "acc-1", row[3])
	require.Equal(t, "gpt-5.1", row[4])
	require.Equal(t, "gpt-5.1-upstream", row[5])
	require.Equal(t, "gpt-5.1-2026-01-01", row[6])
	require.Equal(t, "", row[7], "an unobserved upstream model has no mismatch verdict")
	require.Equal(t, "Medium", row[8])
	require.Equal(t, "High", row[9])
	require.Equal(t, "pro", row[10])
	require.Equal(t, "stream", row[13])
	require.Equal(t, "120", row[14])
	require.Equal(t, "0.00000040", row[18])
	require.Equal(t, "0.00000000", row[19])
	require.Equal(t, "0.5", row[22])
	require.Equal(t, "1.5", row[23])
	require.Equal(t, "0.00123456", row[24])
	require.Equal(t, "0.00100000", row[25])
	require.Equal(t, "0.00300000", row[26], "account cost is account_stats_cost * account_rate_multiplier")
	require.Equal(t, "120", row[27])
	require.Equal(t, "2400", row[28])
	require.Equal(t, "req-1", row[29])
	require.Equal(t, "up-req-1", row[30])
	require.Equal(t, "=SUM(A1:A2)", row[31])
	require.Equal(t, "@10.0.0.1", row[32])
	require.Equal(t, usageExportUnknown, row[33], "missing billing evidence is reported as unknown, never zero")
}

func TestBuildAdminUsageExportRowReportsRequestedAndForwardedEffort(t *testing.T) {
	log := usageExportTestAdminLog()
	row := BuildAdminUsageExportRow(log, "UTC")
	require.Equal(t, "Medium", row[8])
	require.Equal(t, "High", row[9])

	log.UpstreamReasoningEffort = nil
	row = BuildAdminUsageExportRow(log, "UTC")
	require.Equal(t, "Medium", row[9], "without a forwarded value the effective effort is shown")
}

func TestBuildAdminUsageExportRowUsesReportZone(t *testing.T) {
	row := BuildAdminUsageExportRow(usageExportTestAdminLog(), "Asia/Shanghai")
	require.Equal(t, "2026-09-22 18:30:00 (Asia/Shanghai, GMT+08:00)", row[0])
}

func TestBuildSelfUsageExportRowHidesAdminColumns(t *testing.T) {
	log := usageExportTestAdminLog()
	row := BuildSelfUsageExportRow(&log.UsageLog, "UTC")
	require.Len(t, row, len(UsageExportColumns(UsageExportScopeSelf)))
	require.Equal(t, "primary", row[1])
	require.Equal(t, "gpt-5.1", row[2])
	require.Equal(t, "gpt-5.1-2026-01-01", row[3])
	require.Equal(t, "stream", row[7])
	require.Equal(t, "token", row[8])
	require.Equal(t, "0.00100000", row[14])
	require.Equal(t, "0.00123456", row[15])
	require.Equal(t, usageExportUnknown, row[18])
}

func TestBuildSelfUsageExportResponseModelUnknown(t *testing.T) {
	for _, declared := range []*string{nil, stringPtr(""), stringPtr("  ")} {
		row := BuildSelfUsageExportRow(&UsageLog{Model: "requested-model", UpstreamResponseModel: declared}, "UTC")
		require.Equal(t, "requested-model", row[2])
		require.Equal(t, usageExportUnknown, row[3])
	}
}

func TestBuildSelfUsageExportRowDerivesImageBillingMode(t *testing.T) {
	log := usageExportTestAdminLog()
	log.BillingMode = nil
	log.ImageCount = 2
	row := BuildSelfUsageExportRow(&log.UsageLog, "UTC")
	require.Equal(t, "image", row[8])

	log.BillingMode = stringPtr("video")
	row = BuildSelfUsageExportRow(&log.UsageLog, "UTC")
	require.Equal(t, "video", row[8], "an explicit mode always wins over the image heuristic")
}

func TestUsageExportCSVWriterEscapesFormulaInjection(t *testing.T) {
	var buffer bytes.Buffer
	stream := NewUsageExportCSVStream(&buffer, []string{"time", "user_agent"})
	require.False(t, stream.Started())
	require.NoError(t, stream.WriteRows([][]string{
		{"2026-09-22 10:30:00", "=SUM(A1:A2)"},
		{"2026-09-22 10:30:01", "+1+1"},
		{"2026-09-22 10:30:02", "-2+3"},
		{"2026-09-22 10:30:03", "@cmd|calc"},
		{"2026-09-22 10:30:04", "\ttab"},
		{"2026-09-22 10:30:05", "\rcarriage"},
		{"2026-09-22 10:30:06", "plain"},
	}))
	require.True(t, stream.Started())

	records := readUsageExportCSV(t, buffer.String())
	require.Equal(t, []string{"time", "user_agent"}, records[0], "the canonical header row leads the stream")
	require.True(t, strings.HasPrefix(buffer.String(), "\uFEFF"), "the stream starts with a UTF-8 BOM")
	require.Equal(t, "'=SUM(A1:A2)", records[1][1])
	require.Equal(t, "'+1+1", records[2][1])
	require.Equal(t, "'-2+3", records[3][1])
	require.Equal(t, "'@cmd|calc", records[4][1])
	require.Equal(t, "'\ttab", records[5][1])
	require.Equal(t, "'\rcarriage", records[6][1])
	require.Equal(t, "plain", records[7][1])
}

func TestUsageExportCSVWriterKeepsQuotingAndUnicode(t *testing.T) {
	var buffer bytes.Buffer
	stream := NewUsageExportCSVStream(&buffer, []string{"model", "user_agent"})
	require.NoError(t, stream.WriteRows([][]string{
		{"含逗号,的模型", "Mozilla/5.0 \"quoted\""},
		{"multi\nline", "plain"},
	}))

	records := readUsageExportCSV(t, buffer.String())
	require.Equal(t, "含逗号,的模型", records[1][0])
	require.Equal(t, "Mozilla/5.0 \"quoted\"", records[1][1])
	require.Equal(t, "multi\nline", records[2][0])
}

func TestUsageExportCSVStreamCloseEmitsHeaderOnlyFile(t *testing.T) {
	var buffer bytes.Buffer
	stream := NewUsageExportCSVStream(&buffer, []string{"time", "model"})
	require.NoError(t, stream.Close())
	records := readUsageExportCSV(t, buffer.String())
	require.Len(t, records, 1)
	require.Equal(t, []string{"time", "model"}, records[0])
}

func TestUsageExportEvidenceValuesCarrySnapshotFacts(t *testing.T) {
	snapshot := &service.PricingSnapshot{
		Model:               "qwen3",
		Mode:                "image",
		Source:              "price_card",
		PriceCardVersion:    "2026-09-01",
		UnitPrice:           floatPtr(0.02),
		Units:               3,
		RateMultiplier:      1,
		TimeMultiplier:      1,
		ReasoningMultiplier: 1,
		ChargedAmount:       0.06,
	}
	explanation := &UsageBillingExplanation{
		PricingState:    "tokens",
		SettlementState: "settled",
		CompletionState: "complete",
		Snapshot:        snapshot,
		JobID:           "job-1",
		TurnID:          "turn-1",
	}
	values := usageExportEvidenceValues(explanation, stringPtr("session-1"))
	require.Len(t, values, len(usageExportEvidenceColumns))
	require.Equal(t, "tokens", values[0])
	require.Equal(t, "settled", values[1])
	require.Equal(t, "complete", values[2])
	require.Equal(t, "price_card", values[3])
	require.Equal(t, "2026-09-01", values[4])
	require.Equal(t, "images", values[7])
	require.Equal(t, "0.02", values[8])
	require.Equal(t, "0.06000000", values[9])
	require.Equal(t, "job-1", values[11])
	require.Equal(t, "session-1", values[12])
	require.Equal(t, "turn-1", values[13])

	conditions := map[string]any{}
	require.NoError(t, json.Unmarshal([]byte(values[5]), &conditions))
	require.Equal(t, false, conditions["long_context"])
	require.Equal(t, 1.0, conditions["rate"])

	require.Equal(t, "3", values[6])
}

func TestUsageExportFilenameKeepsRange(t *testing.T) {
	require.Equal(t, "usage_2026-09-01_to_2026-09-22.csv",
		UsageExportFilename("usage", "2026-09-01", "2026-09-22", UsageExportFormatCSV))
	require.Equal(t, "usage_2026-09-01_to_2026-09-22.json",
		UsageExportFilename("usage", "2026-09-01", "2026-09-22", UsageExportFormatJSON))
}

func readUsageExportCSV(t *testing.T, content string) [][]string {
	t.Helper()
	reader := csv.NewReader(strings.NewReader(strings.TrimPrefix(content, "\uFEFF")))
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	require.NoError(t, err)
	return records
}

func stringPtr(value string) *string { return &value }

func intPtr(value int) *int { return &value }

func floatPtr(value float64) *float64 { return &value }
