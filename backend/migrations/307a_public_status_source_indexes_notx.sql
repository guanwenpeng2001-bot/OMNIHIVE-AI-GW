-- Bound source scans by finalization, dispatch and receipt creation time.
CREATE INDEX CONCURRENTLY IF NOT EXISTS gateway_executions_status_finish
    ON gateway_executions (finished_at) WHERE finished_at IS NOT NULL;
CREATE INDEX CONCURRENTLY IF NOT EXISTS gateway_attempts_status_dispatch
    ON gateway_execution_attempts (dispatched_at) WHERE dispatched_at IS NOT NULL;
CREATE INDEX CONCURRENTLY IF NOT EXISTS gateway_executions_status_legacy
    ON gateway_executions (api_key_id, legacy_id) WHERE kind = 'request' AND legacy_id <> '';
CREATE INDEX CONCURRENTLY IF NOT EXISTS usage_receipts_status_created
    ON usage_settlement_receipts (created_at);
CREATE INDEX CONCURRENTLY IF NOT EXISTS ops_errors_status_client_request
    ON ops_error_logs (client_request_id, api_key_id, created_at) WHERE status_code >= 400;
