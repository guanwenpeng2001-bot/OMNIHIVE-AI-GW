-- $1 and $2 bound the observation buckets. $3 is the evidence watermark,
-- which can be later than the bucket range during historical backfill.
WITH journaled AS (
    SELECT e.finished_at AS happened_at, status_gateway_component(e.route) AS component_id,
           CASE WHEN e.canceled OR o.status_code = 499 THEN 'client_cancellation'
                WHEN receipt.completion_state = 'interrupted' OR e.response_state = 5 THEN 'stream_interruption'
                WHEN o.status_code >= 400 THEN
                    CASE WHEN o.is_business_limited OR o.error_owner = 'client' THEN 'admission_denial'
                         WHEN o.stream AND (o.time_to_first_token_ms IS NOT NULL OR e.response_state = 2) THEN 'stream_interruption'
                         WHEN o.error_owner = 'provider' AND o.upstream_status_code = 429 THEN 'upstream_rate_limit'
                         WHEN o.error_owner = 'provider' THEN 'upstream_error'
                         WHEN o.error_owner = 'platform' OR o.status_code >= 500 THEN 'gateway_error'
                         ELSE 'unknown' END
                WHEN receipt.completion_state = 'complete' THEN 'succeeded'
                ELSE 'unknown' END AS outcome,
           COALESCE(receipt.stream, o.stream, false) OR e.kind = 'turn' AS stream,
           receipt.ttft_ms, 1::bigint AS weight
    FROM gateway_executions e
    LEFT JOIN LATERAL (
        SELECT r.completion_state,
               COALESCE(r.usage_json->>'Stream' = 'true' OR r.usage_json->>'RequestType' IN ('2','3','5'), false) AS stream,
               CASE WHEN r.usage_json->>'FirstTokenMs' ~ '^[0-9]{1,9}$'
                    THEN (r.usage_json->>'FirstTokenMs')::int END AS ttft_ms
        FROM gateway_execution_attempts a
        JOIN gateway_execution_receipts l USING (attempt_id)
        JOIN usage_settlement_receipts r ON r.id = l.receipt_id
        WHERE a.request_id = e.request_id AND NOT a.abandoned_uncertain AND r.created_at < $3
        ORDER BY a.ordinal DESC, r.id DESC
        LIMIT 1
    ) receipt ON true
    LEFT JOIN LATERAL (
        SELECT o.status_code, o.error_owner, o.upstream_status_code, o.is_business_limited, o.stream, o.time_to_first_token_ms
        FROM ops_error_logs o
        WHERE o.api_key_id = e.api_key_id AND o.user_id = e.owner_id
          AND o.created_at >= e.admitted_at AND o.created_at < $3
          AND o.created_at <= e.finished_at + interval '1 minute'
          AND o.status_code >= 400 AND NOT o.is_count_tokens
          AND (o.request_id = e.request_id::text
               OR (e.kind = 'request' AND
                   (o.client_request_id = substring(e.legacy_id FROM '^client:(.+)$')
                    OR o.request_id = substring(e.legacy_id FROM '^local:(.+)$'))))
        ORDER BY o.created_at DESC, o.id DESC
        LIMIT 1
    ) o ON true
    WHERE e.finished_at >= $1 AND e.finished_at < $2 AND e.kind IN ('request', 'turn')
      AND status_gateway_component(e.route) IS NOT NULL
), dispatched AS (
    SELECT a.dispatched_at, status_gateway_component(e.route),
           CASE WHEN a.decision->>'reason' = 'first_output_timeout_abandoned'
                THEN 'first_byte_timeout' ELSE 'attempt' END,
           false, NULL::int, 1::bigint
    FROM gateway_execution_attempts a
    JOIN gateway_executions e USING (request_id)
    WHERE a.dispatched_at >= $1 AND a.dispatched_at < $2 AND e.kind IN ('request', 'turn')
), legacy_receipts AS (
    SELECT r.created_at, status_gateway_component(COALESCE(NULLIF(r.usage_json->>'InboundEndpoint',''), u.inbound_endpoint)),
           CASE WHEN r.completion_state = 'complete' THEN 'succeeded' ELSE 'stream_interruption' END,
           COALESCE(r.usage_json->>'Stream' = 'true', false)
             OR COALESCE(r.usage_json->>'RequestType' IN ('2','3','5'), false)
             OR COALESCE(u.stream OR u.request_type IN (2,3,5), false),
           COALESCE(CASE WHEN r.usage_json->>'FirstTokenMs' ~ '^[0-9]{1,9}$'
                         THEN (r.usage_json->>'FirstTokenMs')::int END, u.first_token_ms),
           1::bigint
    FROM usage_settlement_receipts r
    LEFT JOIN usage_logs u ON u.id = r.usage_log_id AND u.api_key_id = r.api_key_id AND u.request_id = r.request_id
                          AND u.user_id = r.user_id AND u.account_id = r.account_id AND u.created_at < $3
    WHERE r.created_at >= $1 AND r.created_at < $2
      AND COALESCE(r.usage_json->>'RequestType', '0') <> '4'
      AND NOT EXISTS (SELECT 1 FROM gateway_execution_receipts WHERE receipt_id = r.id)
), legacy_usage AS (
    SELECT u.created_at, status_gateway_component(u.inbound_endpoint), 'succeeded',
           u.stream OR u.request_type IN (2,3,5), u.first_token_ms, 1::bigint
    FROM usage_logs u
    WHERE u.created_at >= $1 AND u.created_at < $2 AND u.request_type <> 4
      AND status_gateway_component(u.inbound_endpoint) IS NOT NULL
      AND NOT EXISTS (
          SELECT 1 FROM usage_settlement_receipts r
          WHERE r.request_id = u.request_id AND r.api_key_id = u.api_key_id AND r.user_id = u.user_id
            AND r.account_id = u.account_id AND (r.usage_log_id IS NULL OR r.usage_log_id = u.id)
            AND r.created_at < $3
      )
      AND NOT EXISTS (
          SELECT 1 FROM gateway_executions e
          WHERE e.request_id = CASE WHEN u.request_id ~ '^[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}$' THEN u.request_id::uuid END
            AND e.api_key_id = u.api_key_id AND e.funding_user_id = u.user_id
      )
), legacy_error_candidates AS (
    SELECT o.*, COALESCE('client:' || NULLIF(o.client_request_id,''), 'local:' || NULLIF(o.request_id,'')) AS correlation,
           COALESCE(status_gateway_component(o.request_path),
                    CASE WHEN o.inbound_endpoint <> '/v1beta/models' THEN status_gateway_component(o.inbound_endpoint) END) AS component_id
    FROM ops_error_logs o
    WHERE o.created_at >= $1 AND o.created_at < $2
      AND o.status_code >= 400 AND NOT o.is_count_tokens AND o.api_key_id > 0 AND o.user_id > 0
      -- A legacy socket can produce several outcomes with a single handshake correlation.
      AND COALESCE(o.request_type, 0) NOT IN (3,4,5)
), legacy_errors AS (
    SELECT DISTINCT ON (o.api_key_id, o.user_id, o.correlation)
           o.created_at, o.component_id,
           CASE WHEN o.status_code = 499 THEN 'client_cancellation'
                WHEN o.is_business_limited OR o.error_owner = 'client' THEN 'admission_denial'
                WHEN o.stream AND o.time_to_first_token_ms IS NOT NULL THEN 'stream_interruption'
                WHEN o.error_owner = 'provider' AND o.upstream_status_code = 429 THEN 'upstream_rate_limit'
                WHEN o.error_owner = 'provider' THEN 'upstream_error'
                WHEN o.error_owner = 'platform' OR o.status_code >= 500 THEN 'gateway_error'
                ELSE 'unknown' END,
           o.stream, NULL::int, 1::bigint
    FROM legacy_error_candidates o
    WHERE o.component_id IS NOT NULL AND o.correlation IS NOT NULL
      AND NOT EXISTS (
          SELECT 1 FROM gateway_executions e
          WHERE e.api_key_id = o.api_key_id AND e.owner_id = o.user_id
            AND e.admitted_at <= o.created_at AND o.created_at <= COALESCE(e.finished_at, e.deadline) + interval '1 minute'
            AND (e.request_id = CASE WHEN o.request_id ~ '^[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}$' THEN o.request_id::uuid END
                 OR (e.kind = 'request' AND e.legacy_id <> '' AND
                     (e.legacy_id IN ('client:' || o.client_request_id, 'local:' || o.request_id))))
      )
      AND NOT EXISTS (
          SELECT 1 FROM usage_settlement_receipts r
          WHERE r.api_key_id = o.api_key_id AND r.user_id = o.user_id AND r.created_at < $3
            AND r.request_id IN ('client:' || o.client_request_id, 'local:' || o.request_id)
            AND r.created_at BETWEEN o.created_at - interval '1 minute' AND o.created_at + interval '1 minute'
      )
      AND NOT EXISTS (
          SELECT 1 FROM usage_logs u
          WHERE u.api_key_id = o.api_key_id AND u.user_id = o.user_id AND u.created_at < $3
            AND u.request_id IN ('client:' || o.client_request_id, 'local:' || o.request_id)
            AND u.created_at BETWEEN o.created_at - interval '1 minute' AND o.created_at + interval '1 minute'
      )
    ORDER BY o.api_key_id, o.user_id, o.correlation, o.created_at DESC, o.id DESC
), ingress_denials AS (
    SELECT bucket_start,
           CASE WHEN protocol = 'openai' AND route_family IN ('responses', 'codex','chat_completions','embeddings') THEN 'openai_compatible'
                WHEN protocol = 'anthropic' AND route_family = 'messages' THEN 'anthropic_compatible'
                WHEN protocol = 'google' AND route_family = 'gemini' THEN 'gemini_compatible' END,
           'admission_denial', false, NULL::int, sum(request_count)::bigint
    FROM ops_ingress_reject_aggregates
    WHERE bucket_start >= $1 AND bucket_start < $2
    GROUP BY bucket_start, protocol, route_family
), observations AS (
    SELECT * FROM journaled
    UNION ALL SELECT * FROM dispatched
    UNION ALL SELECT * FROM legacy_receipts
    UNION ALL SELECT * FROM legacy_usage
    UNION ALL SELECT * FROM legacy_errors
    UNION ALL SELECT * FROM ingress_denials
)
