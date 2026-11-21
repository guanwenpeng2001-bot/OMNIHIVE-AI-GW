CREATE TABLE gateway_execution_recovery (
    singleton boolean PRIMARY KEY CHECK (singleton),
    observed_at timestamptz NOT NULL
);

CREATE OR REPLACE VIEW gateway_execution_reconciliation AS
SELECT e.workspace_id,e.project_id,e.request_id,a.attempt_id,a.account_id,a.ordinal,
       CASE WHEN a.abandoned_uncertain THEN 'abandoned_uncertain' ELSE 'missing_receipt' END AS reason
FROM gateway_executions e JOIN gateway_execution_attempts a USING(request_id)
WHERE a.dispatched_at IS NOT NULL AND (a.abandoned_uncertain OR
 ((e.deadline<=now() OR
    (a.outcome<>'rejected' AND a.dispatched_at <= GREATEST(pg_postmaster_start_time(),
      (SELECT observed_at FROM gateway_execution_recovery WHERE singleton)))) AND a.acceptance<>'not_sent' AND NOT EXISTS (
    SELECT 1 FROM gateway_execution_receipts l JOIN gateway_execution_attempts linked USING(attempt_id)
    WHERE linked.request_id=e.request_id
 )));
