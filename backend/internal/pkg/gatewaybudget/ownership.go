package gatewaybudget

const UnresolvedDispatch = `EXISTS (
  SELECT 1 FROM gateway_executions e
  JOIN gateway_execution_attempts a ON a.request_id=e.request_id
  WHERE e.api_key_id=h.api_key_id AND e.funding_user_id=h.user_id AND e.reservation_ref=h.request_id
    AND a.dispatched_at IS NOT NULL AND a.acceptance<>'not_sent' AND a.outcome<>'rejected'
    AND NOT a.abandoned_uncertain AND NOT EXISTS (
      SELECT 1 FROM gateway_execution_receipts l JOIN usage_settlement_receipts r ON r.id=l.receipt_id
      WHERE l.attempt_id=a.attempt_id AND (r.state='settled' OR r.request_id=h.request_id)))`

const UnresolvedProjectDispatch = `EXISTS (
  SELECT 1 FROM gateway_executions e JOIN gateway_execution_attempts a ON a.request_id=e.request_id
  WHERE e.request_id::text=billing_project_holds.request_id AND a.dispatched_at IS NOT NULL
    AND a.acceptance<>'not_sent' AND a.outcome<>'rejected' AND NOT a.abandoned_uncertain
    AND NOT EXISTS (SELECT 1 FROM usage_settlement_receipts r WHERE r.request_id=billing_project_holds.request_id
      AND (r.state='pending' OR r.billing_command->>'project_reservation_ref'=billing_project_holds.request_id))
    AND NOT EXISTS (
      SELECT 1 FROM gateway_execution_receipts l JOIN usage_settlement_receipts r ON r.id=l.receipt_id
      WHERE l.attempt_id=a.attempt_id AND r.state='settled'))`

const ReleaseProjectHold = `UPDATE billing_project_holds SET released_at=clock_timestamp()
 WHERE workspace_id=$1 AND project_id=$2 AND currency=$3 AND request_id=$4 AND released_at IS NULL
 AND NOT ` + UnresolvedProjectDispatch
