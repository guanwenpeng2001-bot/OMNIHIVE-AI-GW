package service

import (
	"context"
	"errors"
	"log/slog"
)

// Collected on the existing Ops cadence. These are backlog gauges, never usage
// charges; settled receipt/job anchors cannot also count as pending.
func (c *OpsMetricsCollector) collectBillingBacklog(ctx context.Context) error {
	if c == nil || c.db == nil {
		return errors.New("billing backlog database unavailable")
	}
	var pending, review, jobs, unknown, retries int64
	var oldestPending, oldestDue float64
	err := c.db.QueryRowContext(ctx, `SELECT
 (SELECT count(*) FROM usage_settlement_receipts WHERE state='pending'),
 (SELECT count(*) FROM usage_settlement_receipts WHERE state='needs_review'),
 (SELECT COALESCE(GREATEST(EXTRACT(EPOCH FROM NOW()-min(created_at)),0),0) FROM usage_settlement_receipts WHERE state='pending'),
 (SELECT count(*) FROM upstream_jobs WHERE status IN ('submitted','running') AND charged_usage_log_id IS NULL AND family<>'realtime_session'),
 (SELECT count(*) FROM upstream_jobs WHERE error_code='submission_unknown'),
 (SELECT count(*) FROM upstream_jobs WHERE status IN ('submitted','running') AND charged_usage_log_id IS NULL AND attempt_count>0 AND family<>'realtime_session'),
 (SELECT COALESCE(GREATEST(EXTRACT(EPOCH FROM NOW()-min(next_poll_at)),0),0) FROM upstream_jobs WHERE status IN ('submitted','running') AND charged_usage_log_id IS NULL AND family<>'realtime_session' AND next_poll_at<=NOW())`).Scan(&pending, &review, &oldestPending, &jobs, &unknown, &retries, &oldestDue)
	if err != nil {
		return err
	}
	slog.Info("billing backlog", "component", "billing", "pending_count", pending, "needs_review_count", review, "oldest_pending_age_seconds", oldestPending, "pending_jobs", jobs, "submission_unknown_count", unknown, "retry_jobs", retries, "oldest_due_age_seconds", oldestDue)
	return nil
}
