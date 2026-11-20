//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/usagestats"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

type keySummaryCoverageRepo struct {
	service.UsageLogRepository
	stats *usagestats.UserDashboardStats
}

func (repo *keySummaryCoverageRepo) GetAPIKeyDashboardStats(context.Context, int64) (*usagestats.UserDashboardStats, error) {
	return repo.stats, nil
}

func TestKeyUsageSummaryPreservesCoverage(test *testing.T) {
	from := time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC)
	until := from.Add(time.Hour)
	coverage := &usagestats.Coverage{RetainedComplete: true, WindowFrom: &from, WindowUntil: &until, Timezone: "UTC"}
	repo := &keySummaryCoverageRepo{stats: &usagestats.UserDashboardStats{Coverage: coverage, TotalRequests: 7}}
	handler := &GatewayHandler{usageService: service.NewUsageService(repo, nil, nil, nil)}
	payload := handler.buildUsageData(context.Background(), 1)
	require.Same(test, coverage, payload["coverage"])
	encoded, err := json.Marshal(payload)
	require.NoError(test, err)
	require.Contains(test, string(encoded), `"retained_complete":true`)
	require.Contains(test, string(encoded), `"complete":false`)
	require.Contains(test, string(encoded), `"total":{`)
	require.NotContains(test, string(encoded), `"reason"`)
	repo.stats.Coverage = nil
	legacy := handler.buildUsageData(context.Background(), 1)
	require.Nil(test, legacy["coverage"])
}
