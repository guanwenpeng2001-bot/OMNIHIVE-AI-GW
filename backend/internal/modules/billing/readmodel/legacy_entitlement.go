package readmodel

import (
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/timezone"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

// legacyReset adapts the existing subscription and platform quota producers
// for Store.Quotas. BE-19 replaces this adapter with entitlement evidence.
func legacyReset(source, period string, start, since, until *time.Time) *time.Time {
	if start == nil {
		return nil
	}
	if source == "subscription" {
		sub := service.UserSubscription{StartsAt: *since, ExpiresAt: *until, DailyWindowStart: start, WeeklyWindowStart: start, MonthlyWindowStart: start}
		switch period {
		case "daily":
			return sub.DailyResetTime()
		case "weekly":
			return sub.WeeklyResetTime()
		default:
			return sub.MonthlyResetTime()
		}
	}
	switch period {
	case "daily":
		return ptr(timezone.StartOfDay(*start).AddDate(0, 0, 1))
	case "weekly":
		return ptr(timezone.StartOfWeek(*start).AddDate(0, 0, 7))
	default:
		return ptr(start.Add(30 * 24 * time.Hour))
	}
}
