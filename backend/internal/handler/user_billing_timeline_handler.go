package handler

import (
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// GetMyBillingTimeline returns the unified billing timeline of the signed-in
// user: recharges, deductions, refunds, rebates, redeemed cards and
// administrative balance adjustments on one axis.
//
// GET /api/v1/usage/billing-timeline?page=&page_size=&start_date=&end_date=&timezone=
//
// Amounts are the facts recorded when each event happened; the endpoint never
// recomputes historical entries with current pricing.
func (h *UsageHandler) GetMyBillingTimeline(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	page, pageSize := response.ParsePagination(c)
	startAt, endAt, ok := parseBillingTimelineWindow(c)
	if !ok {
		return
	}

	timeline, err := h.usageService.UserBillingTimeline(c.Request.Context(), subject.UserID, service.UserBillingTimelineQuery{
		Page:     page,
		PageSize: pageSize,
		StartAt:  startAt,
		EndAt:    endAt,
		Timezone: strings.TrimSpace(c.Query("timezone")),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, timeline)
}

// parseBillingTimelineWindow reads the optional date range with the same
// convention as the other usage endpoints: YYYY-MM-DD bounds interpreted in the
// requested timezone, with an exclusive upper bound.
func parseBillingTimelineWindow(c *gin.Context) (*time.Time, *time.Time, bool) {
	userTimezone := strings.TrimSpace(c.Query("timezone"))

	var startAt *time.Time
	if raw := strings.TrimSpace(c.Query("start_date")); raw != "" {
		parsed, err := timezone.ParseInUserLocation("2006-01-02", raw, userTimezone)
		if err != nil {
			response.BadRequest(c, "Invalid start_date, expected YYYY-MM-DD")
			return nil, nil, false
		}
		startAt = &parsed
	}

	var endAt *time.Time
	if raw := strings.TrimSpace(c.Query("end_date")); raw != "" {
		parsed, err := timezone.ParseInUserLocation("2006-01-02", raw, userTimezone)
		if err != nil {
			response.BadRequest(c, "Invalid end_date, expected YYYY-MM-DD")
			return nil, nil, false
		}
		exclusive := parsed.AddDate(0, 0, 1)
		endAt = &exclusive
	}

	return startAt, endAt, true
}
