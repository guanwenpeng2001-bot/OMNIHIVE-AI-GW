//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/usagestats"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

func usageExportFixtureLog(t *testing.T, tx *dbent.Tx, repo *usageLogRepository, userID, apiKeyID, accountID int64, createdAt time.Time, model string) int64 {
	t.Helper()
	ctx := context.Background()
	requestID := "req-" + createdAt.UTC().Format(time.RFC3339Nano) + "-" + model
	_, err := repo.Create(ctx, &service.UsageLog{
		UserID: userID, APIKeyID: apiKeyID, AccountID: accountID,
		RequestID: requestID,
		Model:     model, InputTokens: 1, OutputTokens: 1,
		InputCost: 0.1, OutputCost: 0.4, TotalCost: 0.5, ActualCost: 0.4,
		RateMultiplier: 1,
		CreatedAt:      createdAt,
	})
	require.NoError(t, err)
	var id int64
	rows, err := tx.QueryContext(ctx, "SELECT id FROM usage_logs WHERE request_id = $1", requestID)
	require.NoError(t, err)
	require.True(t, rows.Next())
	require.NoError(t, rows.Scan(&id))
	require.NoError(t, rows.Close())
	return id
}

func collectUsageExportIDs(t *testing.T, repo *usageLogRepository, filters usagestats.UsageLogFilters, batchSize int, between func(batchIndex int)) []int64 {
	t.Helper()
	var ids []int64
	batchIndex := 0
	err := repo.StreamUsageLogsWithFilters(context.Background(), filters, nil, batchSize, func(batch []service.UsageLog) error {
		batchIndex++
		for i := range batch {
			ids = append(ids, batch[i].ID)
		}
		if between != nil {
			between(batchIndex)
		}
		return nil
	})
	require.NoError(t, err)
	return ids
}

func TestUsageLogExportKeysetCursorNeverRepeatsOrSkipsRows(t *testing.T) {
	tx := testEntTx(t)
	client := tx.Client()
	repo := newUsageLogRepositoryWithSQL(client, tx)

	user := mustCreateUser(t, client, &service.User{Email: "export-keyset@test.com"})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-export-keyset", Name: "export-keyset"})
	account := mustCreateAccount(t, client, &service.Account{Name: "export-keyset-account"})

	base := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Millisecond)
	expected := make([]int64, 0, 25)
	for i := 0; i < 25; i++ {
		expected = append(expected, usageExportFixtureLog(t, tx, repo, user.ID, apiKey.ID, account.ID, base.Add(time.Duration(i)*time.Minute), "gpt-5.1"))
	}

	start := base.Add(-time.Minute)
	end := base.Add(time.Hour)
	ids := collectUsageExportIDs(t, repo, usagestats.UsageLogFilters{UserID: user.ID, StartTime: &start, EndTime: &end}, 10, func(batchIndex int) {
		if batchIndex != 1 {
			return
		}
		// A row that lands inside the window already walked by the cursor. Offset
		// pagination would shift the next page and repeat a row; the keyset cursor
		// must not.
		usageExportFixtureLog(t, tx, repo, user.ID, apiKey.ID, account.ID, base.Add(20*time.Minute), "gpt-5.1")
	})

	require.Len(t, ids, len(expected), "every row inside the range is exported exactly once")
	seen := map[int64]int{}
	for _, id := range ids {
		seen[id]++
	}
	for _, id := range expected {
		require.Equal(t, 1, seen[id], "row %d appears exactly once", id)
	}
	for i := 1; i < len(ids); i++ {
		require.Greater(t, ids[i-1], ids[i], "the stream keeps the stable (created_at, id) descending order")
	}
	require.NoError(t, tx.Rollback())
}

func TestUsageLogExportSnapshotRangeExcludesRowsCreatedAfterTheFreeze(t *testing.T) {
	tx := testEntTx(t)
	client := tx.Client()
	repo := newUsageLogRepositoryWithSQL(client, tx)

	user := mustCreateUser(t, client, &service.User{Email: "export-snapshot@test.com"})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-export-snapshot", Name: "export-snapshot"})
	account := mustCreateAccount(t, client, &service.Account{Name: "export-snapshot-account"})

	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)
	frozen := base.Add(10 * time.Minute)
	inside := usageExportFixtureLog(t, tx, repo, user.ID, apiKey.ID, account.ID, base.Add(time.Minute), "gpt-5.1")
	afterFreeze := usageExportFixtureLog(t, tx, repo, user.ID, apiKey.ID, account.ID, frozen.Add(time.Minute), "gpt-5.1")

	start := base
	ids := collectUsageExportIDs(t, repo, usagestats.UsageLogFilters{UserID: user.ID, StartTime: &start, EndTime: &frozen}, 10, nil)

	require.Equal(t, []int64{inside}, ids)
	require.NotContains(t, ids, afterFreeze, "rows written after the snapshot freeze stay outside the export")
	require.NoError(t, tx.Rollback())
}

func TestUsageLogExportAppliesTheSameFiltersAsTheList(t *testing.T) {
	tx := testEntTx(t)
	client := tx.Client()
	repo := newUsageLogRepositoryWithSQL(client, tx)

	user := mustCreateUser(t, client, &service.User{Email: "export-filters@test.com"})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-export-filters", Name: "export-filters"})
	account := mustCreateAccount(t, client, &service.Account{Name: "export-filters-account"})

	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)
	tokenMode := "token"
	imageMode := "image"
	tokenRow := usageExportFixtureLog(t, tx, repo, user.ID, apiKey.ID, account.ID, base.Add(time.Minute), "gpt-5.1")
	imageRow := usageExportFixtureLog(t, tx, repo, user.ID, apiKey.ID, account.ID, base.Add(2*time.Minute), "gpt-image-1")
	legacyImageRow := usageExportFixtureLog(t, tx, repo, user.ID, apiKey.ID, account.ID, base.Add(3*time.Minute), "gpt-image-1-legacy")
	_, err := tx.ExecContext(context.Background(), "UPDATE usage_logs SET billing_mode=$1 WHERE id=$2", tokenMode, tokenRow)
	require.NoError(t, err)
	_, err = tx.ExecContext(context.Background(), "UPDATE usage_logs SET billing_mode=$1, image_count=1, image_size='1K' WHERE id=$2", imageMode, imageRow)
	require.NoError(t, err)
	// 历史行没有 billing_mode，只有 image_count：过滤器必须与列表页同样把它算作 image。
	_, err = tx.ExecContext(context.Background(), "UPDATE usage_logs SET billing_mode=NULL, image_count=1, image_size='1K' WHERE id=$1", legacyImageRow)
	require.NoError(t, err)

	start := base
	end := base.Add(time.Hour)
	ids := collectUsageExportIDs(t, repo, usagestats.UsageLogFilters{
		UserID: user.ID, StartTime: &start, EndTime: &end, BillingMode: imageMode,
	}, 10, nil)
	require.Equal(t, []int64{legacyImageRow, imageRow}, ids, "the export applies the billing_mode filter like the table does")

	streamRequestType := int16(service.RequestTypeStream)
	ids = collectUsageExportIDs(t, repo, usagestats.UsageLogFilters{
		UserID: user.ID, StartTime: &start, EndTime: &end, RequestType: &streamRequestType,
	}, 10, nil)
	require.Empty(t, ids)
	require.NoError(t, tx.Rollback())
}

func TestUsageLogExportEndsCleanlyWhenTheLastBatchIsFull(t *testing.T) {
	tx := testEntTx(t)
	client := tx.Client()
	repo := newUsageLogRepositoryWithSQL(client, tx)

	user := mustCreateUser(t, client, &service.User{Email: "export-batch@test.com"})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-export-batch", Name: "export-batch"})
	account := mustCreateAccount(t, client, &service.Account{Name: "export-batch-account"})

	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)
	for i := 0; i < 20; i++ {
		usageExportFixtureLog(t, tx, repo, user.ID, apiKey.ID, account.ID, base.Add(time.Duration(i)*time.Minute), "gpt-5.1")
	}
	start := base
	end := base.Add(time.Hour)
	ids := collectUsageExportIDs(t, repo, usagestats.UsageLogFilters{UserID: user.ID, StartTime: &start, EndTime: &end}, 10, nil)
	require.Len(t, ids, 20, "an exactly full final batch terminates the stream without duplicates")
	require.NoError(t, tx.Rollback())
}

func TestUsageLogExportHonoursContextCancellation(t *testing.T) {
	tx := testEntTx(t)
	client := tx.Client()
	repo := newUsageLogRepositoryWithSQL(client, tx)

	user := mustCreateUser(t, client, &service.User{Email: "export-cancel@test.com"})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-export-cancel", Name: "export-cancel"})
	account := mustCreateAccount(t, client, &service.Account{Name: "export-cancel-account"})

	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)
	for i := 0; i < 20; i++ {
		usageExportFixtureLog(t, tx, repo, user.ID, apiKey.ID, account.ID, base.Add(time.Duration(i)*time.Minute), "gpt-5.1")
	}

	ctx, cancel := context.WithCancel(context.Background())
	start := base
	end := base.Add(time.Hour)
	batches := 0
	err := repo.StreamUsageLogsWithFilters(ctx, usagestats.UsageLogFilters{UserID: user.ID, StartTime: &start, EndTime: &end}, nil, 5, func(batch []service.UsageLog) error {
		batches++
		cancel()
		return nil
	})
	require.ErrorIs(t, err, context.Canceled, "a cancelled export stops before reading the next batch")
	require.Equal(t, 1, batches, "the reader is released as soon as the client goes away")
	require.NoError(t, tx.Rollback())
}

// TestUsageLogBillingModeKeepsEveryAggregationSurfaceConsistent proves that the
// billing_mode filter means the same thing for the table totals, the trend, the
// model chart and the group chart: the stats path and the aggregation paths use
// different SQL builders, so this compares their real numbers on the same rows.
func TestUsageLogBillingModeKeepsEveryAggregationSurfaceConsistent(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	repo := newUsageLogRepositoryWithSQL(client, tx)

	user := mustCreateUser(t, client, &service.User{Email: "export-consistency@test.com"})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-export-consistency", Name: "export-consistency"})
	account := mustCreateAccount(t, client, &service.Account{Name: "export-consistency-account"})
	group := mustCreateGroup(t, client, &service.Group{Name: "export-consistency-group"})

	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)
	tokenRow := usageExportFixtureLog(t, tx, repo, user.ID, apiKey.ID, account.ID, base.Add(time.Minute), "gpt-5.1")
	imageRow := usageExportFixtureLog(t, tx, repo, user.ID, apiKey.ID, account.ID, base.Add(2*time.Minute), "gpt-image-1")
	legacyImageRow := usageExportFixtureLog(t, tx, repo, user.ID, apiKey.ID, account.ID, base.Add(3*time.Minute), "gpt-image-1-legacy")
	_, err := tx.ExecContext(ctx, "UPDATE usage_logs SET billing_mode='token', group_id=$1 WHERE id=$2", group.ID, tokenRow)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, "UPDATE usage_logs SET billing_mode='image', image_count=1, image_size='1K', group_id=$1 WHERE id=$2", group.ID, imageRow)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, "UPDATE usage_logs SET billing_mode=NULL, image_count=1, image_size='1K', group_id=$1 WHERE id=$2", group.ID, legacyImageRow)
	require.NoError(t, err)

	start := base
	end := base.Add(time.Hour)
	filters := usagestats.UsageLogFilters{
		UserID: user.ID, StartTime: &start, EndTime: &end, BillingMode: "image",
	}

	stats, err := repo.GetStatsWithFilters(ctx, filters)
	require.NoError(t, err)
	require.Equal(t, int64(2), stats.TotalRequests, "only the two image rows are in scope")

	trend, err := repo.GetUsageTrendWithUsageFilters(ctx, start, end, "hour", filters)
	require.NoError(t, err)
	models, err := repo.GetModelStatsWithUsageFiltersBySource(ctx, start, end, filters, usagestats.ModelSourceRequested)
	require.NoError(t, err)
	groups, err := repo.GetGroupStatsWithUsageFilters(ctx, start, end, filters)
	require.NoError(t, err)

	var trendRequests, modelRequests, groupRequests int64
	var trendTokens, modelTokens, groupTokens int64
	var trendCost, modelCost, groupCost float64
	for _, point := range trend {
		trendRequests += point.Requests
		trendTokens += point.TotalTokens
		trendCost += point.Cost
	}
	for _, model := range models {
		modelRequests += model.Requests
		modelTokens += model.TotalTokens
		modelCost += model.Cost
	}
	for _, groupStat := range groups {
		groupRequests += groupStat.Requests
		groupTokens += groupStat.TotalTokens
		groupCost += groupStat.Cost
	}

	require.Equal(t, stats.TotalRequests, trendRequests, "trend requests match the totals")
	require.Equal(t, stats.TotalRequests, modelRequests, "model chart requests match the totals")
	require.Equal(t, stats.TotalRequests, groupRequests, "group chart requests match the totals")
	require.Greater(t, stats.TotalCost, 0.0, "the fixture rows carry real costs")
	require.NotNil(t, stats.TotalAccountCost, "the totals report a real account cost, never a missing field")
	require.InDelta(t, stats.TotalCost, *stats.TotalAccountCost, 1e-9)
	require.Equal(t, stats.TotalTokens, trendTokens, "trend tokens match the totals")
	require.Equal(t, stats.TotalTokens, modelTokens, "model chart tokens match the totals")
	require.Equal(t, stats.TotalTokens, groupTokens, "group chart tokens match the totals")
	require.InDelta(t, stats.TotalCost, trendCost, 1e-9, "trend cost matches the totals")
	require.InDelta(t, stats.TotalCost, modelCost, 1e-9, "model chart cost matches the totals")
	require.InDelta(t, stats.TotalCost, groupCost, 1e-9, "group chart cost matches the totals")
	require.NoError(t, tx.Rollback())
}
