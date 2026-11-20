package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/usagestats"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

// UsageExportBatchSize is the number of rows read per export round trip. It
// bounds both the memory held by one batch and the size of a single query.
const UsageExportBatchSize = 500

// StreamUsageLogsWithFilters walks the filtered usage logs in the same
// (created_at DESC, id DESC) order for the whole stream and yields them batch by
// batch. Paging is keyset based: the cursor is the last row of the previous
// batch, so a concurrent insert cannot shift the window and produce duplicate or
// skipped rows. The caller is expected to freeze the upper time bound in
// filters.EndTime, which makes the export a snapshot range.
func (r *usageLogRepository) StreamUsageLogsWithFilters(
	ctx context.Context,
	filters UsageLogFilters,
	after *usagestats.UsageExportCursor,
	batchSize int,
	yield func([]service.UsageLog) error,
) error {
	if yield == nil {
		return fmt.Errorf("usage export requires a row consumer")
	}
	if batchSize <= 0 {
		batchSize = UsageExportBatchSize
	}
	if needsUsageSnapshot(r) {
		// One read snapshot for the whole walk: the replica/raw-coverage guard
		// runs once, and every batch observes the same published state.
		// 未显式指定 StartTime 时夹到已验证原始保留起点（与列表查询同语义）。
		end := time.Now()
		if filters.EndTime != nil {
			end = *filters.EndTime
		}
		_, err := usageReadSnapshot(ctx, r, func(snapshot *usageLogRepository, c dashboardCoverage) (struct{}, error) {
			start := c.raw
			if filters.StartTime != nil {
				start = *filters.StartTime
			}
			if err := c.requireRaw(start, end); err != nil {
				return struct{}{}, err
			}
			return struct{}{}, snapshot.StreamUsageLogsWithFilters(ctx, filters, after, batchSize, yield)
		})
		return err
	}

	cursor := after
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		batch, err := r.queryUsageLogExportBatch(ctx, filters, cursor, batchSize)
		if err != nil {
			return err
		}
		if len(batch) == 0 {
			return nil
		}
		if err := r.hydrateUsageLogAssociations(ctx, batch); err != nil {
			return err
		}
		if err := yield(batch); err != nil {
			return err
		}
		if len(batch) < batchSize {
			return nil
		}
		last := batch[len(batch)-1]
		cursor = &usagestats.UsageExportCursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
}

func (r *usageLogRepository) queryUsageLogExportBatch(ctx context.Context, filters UsageLogFilters, cursor *usagestats.UsageExportCursor, batchSize int) ([]service.UsageLog, error) {
	conditions := workspaceUsageConditions(ctx)
	args := make([]any, 0, 9)
	conditions, args = appendUsageLogListConditions(conditions, args, filters)
	if cursor != nil {
		conditions = append(conditions, fmt.Sprintf("(created_at, id) < ($%d, $%d)", len(args)+1, len(args)+2))
		args = append(args, cursor.CreatedAt, cursor.ID)
	}
	query := fmt.Sprintf(
		"SELECT %s FROM usage_logs %s ORDER BY created_at DESC, id DESC LIMIT $%d",
		usageLogSelectColumns,
		buildWhere(conditions),
		len(args)+1,
	)
	args = append(args, batchSize)
	return r.queryUsageLogs(ctx, query, args...)
}
