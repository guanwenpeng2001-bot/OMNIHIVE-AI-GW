package handler

import (
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// Export streams the current user's own usage rows as CSV.
// GET /api/v1/usage/export
//
// The export is scoped to the authenticated subject and uses the same filter
// set as the usage list, so the file always matches the table the owner sees.
// Rows are read with a keyset cursor inside a range frozen at request time.
func (h *UsageHandler) Export(c *gin.Context) {
	parsed, ok := h.parseUserUsageFilters(c, true)
	if !ok {
		return
	}
	if format := strings.ToLower(strings.TrimSpace(c.Query("format"))); format != "" && format != dto.UsageExportFormatCSV {
		response.BadRequest(c, "Invalid format, use csv")
		return
	}

	filters := parsed.Filters
	snapshotEnd := time.Now()
	if filters.EndTime != nil && filters.EndTime.Before(snapshotEnd) {
		snapshotEnd = *filters.EndTime
	}
	filters.EndTime = &snapshotEnd
	userTZ := c.Query("timezone")

	columns := dto.UsageExportColumns(dto.UsageExportScopeSelf)
	// The download headers must reach the client with the first body byte, so the
	// read below still runs with them in place and has to drop them again when it
	// fails before writing anything.
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename=\""+dto.UsageExportFilename("usage", usageExportDate(filters.StartTime), usageExportDate(&snapshotEnd), dto.UsageExportFormatCSV)+"\"")
	c.Header("X-Usage-Export-Columns", strings.Join(columns, ","))
	c.Header("X-Usage-Export-Snapshot-End", snapshotEnd.UTC().Format(time.RFC3339Nano))
	c.Header("Cache-Control", "no-store")
	flusher, _ := c.Writer.(http.Flusher)

	stream := dto.NewUsageExportCSVStream(c.Writer, columns)
	err := h.usageService.StreamUsageLogsForExport(c.Request.Context(), filters, func(batch []service.UsageLog) error {
		rows := make([][]string, 0, len(batch))
		for i := range batch {
			rows = append(rows, dto.BuildSelfUsageExportRow(dto.UsageLogFromService(&batch[i]), userTZ))
		}
		if err := stream.WriteRows(rows); err != nil {
			return err
		}
		if flusher != nil {
			flusher.Flush()
		}
		return nil
	})
	if err != nil {
		if !stream.Started() {
			// No body byte has been written, so the failure can still answer as the
			// shared JSON envelope: drop the headers that promised a CSV file.
			response.DropDownloadHeaders(c, "X-Usage-Export-Columns", "X-Usage-Export-Snapshot-End")
			response.ErrorFrom(c, err)
			return
		}
		// The body is already committed as CSV, so appending the JSON envelope
		// would corrupt the file. Abort the transfer instead: the client keeps a
		// truncated CSV and must treat a short file as a failed export.
		logger.LegacyPrintf("handler.usage", "usage export stream aborted: %v", err)
		c.Abort()
		return
	}
	if err := stream.Close(); err != nil && !stream.Started() {
		response.DropDownloadHeaders(c, "X-Usage-Export-Columns", "X-Usage-Export-Snapshot-End")
		response.ErrorFrom(c, err)
		return
	}
	if flusher != nil {
		flusher.Flush()
	}
}

func usageExportDate(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.Format("2006-01-02")
}
