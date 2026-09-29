package admin

import "time"

var accountTodayStatsBatchCache = newSnapshotCache(30 * time.Second)

func buildAccountTodayStatsBatchCacheKey(accountIDs []int64, dayStart time.Time, revision string) string {
	return "accounts_today_stats:" + mustMarshalDashboardCacheKey(struct {
		AccountIDs []int64 `json:"account_ids"`
		DayStart   string  `json:"day_start"`
		Timezone   string  `json:"timezone"`
		Revision   string  `json:"revision"`
	}{
		AccountIDs: normalizeInt64IDList(accountIDs),
		DayStart:   dayStart.Format(time.RFC3339),
		Timezone:   dayStart.Location().String(),
		Revision:   revision,
	})
}
