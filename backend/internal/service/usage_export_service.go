package service

import (
	"context"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
)

// ErrUsageExportUnsupported is returned when the configured usage repository
// cannot stream an export (for example a test double without the capability).
var ErrUsageExportUnsupported = infraerrors.InternalServer("USAGE_EXPORT_UNSUPPORTED", "usage export is not available")

// UsageLogExportRepository is the optional repository capability behind the
// server-side usage export. It is asserted at call time so repositories without
// the capability keep working.
type UsageLogExportRepository interface {
	StreamUsageLogsWithFilters(
		ctx context.Context,
		filters usagestats.UsageLogFilters,
		after *usagestats.UsageExportCursor,
		batchSize int,
		yield func([]UsageLog) error,
	) error
}

// StreamUsageLogsForExport walks the filtered usage logs in a stable
// (created_at DESC, id DESC) keyset order and hands each batch to yield. The
// caller freezes filters.EndTime to turn the walk into a snapshot range.
func (s *UsageService) StreamUsageLogsForExport(
	ctx context.Context,
	filters usagestats.UsageLogFilters,
	yield func([]UsageLog) error,
) error {
	repo, ok := s.usageRepo.(UsageLogExportRepository)
	if !ok {
		return ErrUsageExportUnsupported
	}
	return repo.StreamUsageLogsWithFilters(ctx, filters, nil, 0, yield)
}
