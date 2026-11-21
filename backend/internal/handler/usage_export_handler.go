package handler

import (
	"net/http"
	"strings"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/handler/dto"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/logger"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/response"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"

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
	if err == nil {
		err = stream.Close()
	}
	if err != nil {
		if !stream.Started() && !c.Writer.Written() {
			response.DropDownloadHeaders(c, "X-Usage-Export-Columns", "X-Usage-Export-Snapshot-End")
			response.ErrorFrom(c, err)
			return
		}
		// A committed CSV cannot carry a JSON error. Let net/http terminate the
		// transport so clients cannot accept a syntactically valid partial file.
		logger.FromContext(c.Request.Context()).Sugar().Errorf("usage export aborted: scope=self error_type=%T", err)
		panic(http.ErrAbortHandler)
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
