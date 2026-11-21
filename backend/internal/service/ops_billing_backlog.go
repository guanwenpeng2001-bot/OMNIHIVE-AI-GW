package service

import (
	"context"
	"errors"
	"log/slog"
)

// Keep the candidate predicate aligned with idx_upstream_jobs_terminal_review.
// Only the exact, case-sensitive ops token classifies a manual waiver. Pricing
// state or a missing charge never implies a waiver or permission to debit.
// Partition the candidates in one scan; retain their error/status evidence.
const terminalJobsReviewCountsSQL = `SELECT
 count(*) FILTER (WHERE NOT manual_waiver) AS needs_review_count,
 count(*) FILTER (WHERE manual_waiver) AS manual_waiver_count
 FROM (
  SELECT COALESCE(error_message, '') ~ '(^|[^[:alnum:]_])MANUAL_WAIVER_NO_DEBIT($|[^[:alnum:]_])' AS manual_waiver
  FROM upstream_jobs WHERE status IN ('succeeded','failed','expired') AND error_code='settlement_needs_review' AND charged_usage_log_id IS NULL AND family<>'realtime_session'
 ) terminal_jobs`

// Collected on the existing Ops cadence. These are observation gauges, never usage
// charges; settled receipt/job anchors cannot also count as pending. Terminal
// job review and manual waiver are separate gauges from receipt review: the
// same usage can have both records, so these counts must not be added into a total.
// Manual waivers count retained marked jobs, not unresolved settlement backlog.
func (c *OpsMetricsCollector) collectBillingBacklog(ctx context.Context) error {
	if c == nil || c.db == nil {
		return errors.New("billing backlog database unavailable")
	}
	var pending, review, jobs, unknown, retries, terminalReview, terminalWaiver int64
	var oldestPending, oldestDue float64
	var recoveredAttempts int64
	err := c.db.QueryRowContext(ctx, `SELECT
 (SELECT count(*) FROM usage_settlement_receipts WHERE state='pending'),
 (SELECT count(*) FROM usage_settlement_receipts WHERE state='needs_review'),
 (SELECT COALESCE(GREATEST(EXTRACT(EPOCH FROM NOW()-min(created_at)),0),0) FROM usage_settlement_receipts WHERE state='pending'),
 (SELECT count(*) FROM upstream_jobs WHERE status IN ('submitted','running') AND charged_usage_log_id IS NULL AND family<>'realtime_session'),
 (SELECT count(*) FROM upstream_jobs WHERE error_code='submission_unknown'),
 (SELECT count(*) FROM upstream_jobs WHERE status IN ('submitted','running') AND charged_usage_log_id IS NULL AND attempt_count>0 AND family<>'realtime_session'),
 (SELECT COALESCE(GREATEST(EXTRACT(EPOCH FROM NOW()-min(next_poll_at)),0),0) FROM upstream_jobs WHERE status IN ('submitted','running') AND charged_usage_log_id IS NULL AND family<>'realtime_session' AND next_poll_at<=NOW()),
 terminal.needs_review_count, terminal.manual_waiver_count,
 (SELECT count(*) FROM gateway_execution_reconciliation)
 FROM (`+terminalJobsReviewCountsSQL+`) terminal`).Scan(&pending, &review, &oldestPending, &jobs, &unknown, &retries, &oldestDue, &terminalReview, &terminalWaiver, &recoveredAttempts)
	if err != nil {
		return err
	}
	// A missing charge anchor alone does not imply missing billing: successful
	// free resources are legitimate. Only the explicit review marker qualifies.
	// Keep this at Info: a review job is neither an automatic retry nor a debt.
	slog.Info("billing backlog", "component", "billing", "pending_count", pending, "needs_review_count", review, "oldest_pending_age_seconds", oldestPending, "pending_jobs", jobs, "submission_unknown_count", unknown, "retry_jobs", retries, "oldest_due_age_seconds", oldestDue, "terminal_jobs_needs_review_count", terminalReview, "terminal_jobs_manual_waiver_count", terminalWaiver, "execution_needs_review_count", recoveredAttempts)
	return nil
}
