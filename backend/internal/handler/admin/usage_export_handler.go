package admin

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/handler/dto"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/logger"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/response"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/timezone"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/usagestats"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"

	"github.com/gin-gonic/gin"
)

// usageExportHeaderColumns exposes the canonical column order to the client, so
// the panel can replace the header row with localized labels and stays aligned
// with the server without duplicating the order.
const usageExportHeaderColumns = "X-Usage-Export-Columns"

// usageExportHeaderSnapshotEnd reports the frozen upper bound of the export
// range: rows created at or after this instant are outside the snapshot.
const usageExportHeaderSnapshotEnd = "X-Usage-Export-Snapshot-End"

// parseAdminUsageFilters parses the admin usage filter query once, so the list,
// the totals and the export always filter the same rows.
func parseAdminUsageFilters(c *gin.Context) (usagestats.UsageLogFilters, bool) {
	filters := usagestats.UsageLogFilters{
		ModelFilterSource: usagestats.ModelSourceRequested,
	}

	if userIDStr := c.Query("user_id"); userIDStr != "" {
		id, err := strconv.ParseInt(userIDStr, 10, 64)
		if err != nil {
			response.BadRequest(c, "Invalid user_id")
			return filters, false
		}
		filters.UserID = id
	}

	if apiKeyIDStr := c.Query("api_key_id"); apiKeyIDStr != "" {
		id, err := strconv.ParseInt(apiKeyIDStr, 10, 64)
		if err != nil {
			response.BadRequest(c, "Invalid api_key_id")
			return filters, false
		}
		filters.APIKeyID = id
	}

	if accountIDStr := c.Query("account_id"); accountIDStr != "" {
		id, err := strconv.ParseInt(accountIDStr, 10, 64)
		if err != nil {
			response.BadRequest(c, "Invalid account_id")
			return filters, false
		}
		filters.AccountID = id
	}

	if groupIDStr := c.Query("group_id"); groupIDStr != "" {
		id, err := strconv.ParseInt(groupIDStr, 10, 64)
		if err != nil {
			response.BadRequest(c, "Invalid group_id")
			return filters, false
		}
		filters.GroupID = id
	}

	filters.Model = c.Query("model")
	filters.RequestID = strings.TrimSpace(c.Query("request_id"))
	filters.BillingMode = strings.TrimSpace(c.Query("billing_mode"))
	if filters.BillingMode != "" && !service.BillingMode(filters.BillingMode).IsValidUsageFilter() {
		response.BadRequest(c, "Invalid billing_mode")
		return filters, false
	}

	if requestTypeStr := strings.TrimSpace(c.Query("request_type")); requestTypeStr != "" {
		parsed, err := service.ParseUsageRequestType(requestTypeStr)
		if err != nil {
			response.BadRequest(c, err.Error())
			return filters, false
		}
		value := int16(parsed)
		filters.RequestType = &value
	} else if streamStr := c.Query("stream"); streamStr != "" {
		val, err := strconv.ParseBool(streamStr)
		if err != nil {
			response.BadRequest(c, "Invalid stream value, use true or false")
			return filters, false
		}
		filters.Stream = &val
	}

	nativeCompactionV2, err := parseOptionalBoolDashboardFilter(c, "native_compaction_v2")
	if err != nil {
		response.BadRequest(c, "Invalid native_compaction_v2 value, use true or false")
		return filters, false
	}
	filters.NativeCompactionV2 = nativeCompactionV2

	if billingTypeStr := c.Query("billing_type"); billingTypeStr != "" {
		val, err := strconv.ParseInt(billingTypeStr, 10, 8)
		if err != nil {
			response.BadRequest(c, "Invalid billing_type")
			return filters, false
		}
		bt := int8(val)
		filters.BillingType = &bt
	}

	if raw := strings.TrimSpace(c.Query("upstream_model_mismatch")); raw != "" {
		value, err := strconv.ParseBool(raw)
		if err != nil {
			response.BadRequest(c, "Invalid upstream_model_mismatch value, use true or false")
			return filters, false
		}
		filters.UpstreamModelMismatch = &value
	}

	userTZ := c.Query("timezone")
	if startDateStr := c.Query("start_date"); startDateStr != "" {
		t, err := timezone.ParseInUserLocation("2006-01-02", startDateStr, userTZ)
		if err != nil {
			response.BadRequest(c, "Invalid start_date format, use YYYY-MM-DD")
			return filters, false
		}
		filters.StartTime = &t
	}

	if endDateStr := c.Query("end_date"); endDateStr != "" {
		t, err := timezone.ParseInUserLocation("2006-01-02", endDateStr, userTZ)
		if err != nil {
			response.BadRequest(c, "Invalid end_date format, use YYYY-MM-DD")
			return filters, false
		}
		// Use half-open range [start, end), move to next calendar day start (DST-safe).
		t = t.AddDate(0, 0, 1)
		filters.EndTime = &t
	}

	return filters, true
}

// applyUsageExportRange fills the range an export always needs and freezes its
// upper bound at the moment the export starts. Every batch then reads the same
// snapshot range: rows written after the export began are outside the file.
func applyUsageExportRange(c *gin.Context, filters *usagestats.UsageLogFilters) time.Time {
	userTZ := c.Query("timezone")
	now := timezone.NowInUserLocation(userTZ)
	if filters.StartTime == nil {
		var start time.Time
		switch c.DefaultQuery("period", "") {
		case "today":
			start = timezone.StartOfDayInUserLocation(now, userTZ)
		case "month":
			start = now.AddDate(0, -1, 0)
		default:
			start = timezone.StartOfDayInUserLocation(now.AddDate(0, 0, -7), userTZ)
		}
		filters.StartTime = &start
	}
	snapshotEnd := now
	if filters.EndTime != nil && filters.EndTime.Before(snapshotEnd) {
		snapshotEnd = *filters.EndTime
	}
	filters.EndTime = &snapshotEnd
	return snapshotEnd
}

// Export streams the filtered usage rows as a file.
// GET /api/v1/admin/usage/export
//
// format=csv (default) streams CSV with a stable keyset cursor and a frozen
// snapshot range; format=json returns the same rows as the admin list DTO for
// the client-side Excel writer.
func (h *UsageHandler) Export(c *gin.Context) {
	filters, ok := parseAdminUsageFilters(c)
	if !ok {
		return
	}
	format := strings.ToLower(strings.TrimSpace(c.DefaultQuery("format", dto.UsageExportFormatCSV)))
	if format != dto.UsageExportFormatCSV && format != dto.UsageExportFormatJSON {
		response.BadRequest(c, "Invalid format, use csv or json")
		return
	}

	snapshotEnd := applyUsageExportRange(c, &filters)
	userTZ := c.Query("timezone")

	if format == dto.UsageExportFormatJSON {
		h.exportUsageRowsJSON(c, filters)
		return
	}
	h.exportUsageRowsCSV(c, filters, snapshotEnd, userTZ)
}

func (h *UsageHandler) exportUsageRowsCSV(c *gin.Context, filters usagestats.UsageLogFilters, snapshotEnd time.Time, userTZ string) {
	columns := dto.UsageExportColumns(dto.UsageExportScopeAdmin)
	// The download headers must reach the client with the first body byte, so the
	// read below still runs with them in place and has to drop them again when it
	// fails before writing anything.
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename=\""+dto.UsageExportFilename("usage", usageExportDate(filters.StartTime), usageExportDate(&snapshotEnd), dto.UsageExportFormatCSV)+"\"")
	c.Header(usageExportHeaderColumns, strings.Join(columns, ","))
	c.Header(usageExportHeaderSnapshotEnd, snapshotEnd.UTC().Format(time.RFC3339Nano))
	c.Header("Cache-Control", "no-store")
	flusher, _ := c.Writer.(http.Flusher)

	stream := dto.NewUsageExportCSVStream(c.Writer, columns)
	err := h.usageService.StreamUsageLogsForExport(c.Request.Context(), filters, func(batch []service.UsageLog) error {
		rows := make([][]string, 0, len(batch))
		for i := range batch {
			rows = append(rows, dto.BuildAdminUsageExportRow(dto.UsageLogFromServiceAdmin(&batch[i]), userTZ))
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
			response.DropDownloadHeaders(c, usageExportHeaderColumns, usageExportHeaderSnapshotEnd)
			response.ErrorFrom(c, err)
			return
		}
		// A committed CSV cannot carry a JSON error. Let net/http terminate the
		// transport so clients cannot accept a syntactically valid partial file.
		logger.FromContext(c.Request.Context()).Sugar().Errorf("usage export aborted: scope=admin error_type=%T", err)
		panic(http.ErrAbortHandler)
	}
	if flusher != nil {
		flusher.Flush()
	}
}

func (h *UsageHandler) exportUsageRowsJSON(c *gin.Context, filters usagestats.UsageLogFilters) {
	items := make([]dto.AdminUsageLog, 0)
	err := h.usageService.StreamUsageLogsForExport(c.Request.Context(), filters, func(batch []service.UsageLog) error {
		for i := range batch {
			items = append(items, *dto.UsageLogFromServiceAdmin(&batch[i]))
		}
		return nil
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, gin.H{
		"items":   items,
		"total":   len(items),
		"columns": dto.UsageExportColumns(dto.UsageExportScopeAdmin),
	})
}

func usageExportDate(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.Format("2006-01-02")
}
