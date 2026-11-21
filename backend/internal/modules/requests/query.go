package requests

// Anchors hold one row per logical request. Each lateral contributes at most one
// observation, even when the journal recorded multiple attempts or Ops logged
// several failure observations for the same request.
const anchors = `
WITH anchors AS (
    SELECT * FROM request_anchors
    WHERE workspace_id = $1 AND project_id = $2 AND started_at < $3`

const observations = `
), observations AS (
    SELECT a.*, COALESCE(NULLIF(a.route, ''), u.inbound_endpoint, r.usage_json->>'InboundEndpoint', o.inbound_endpoint) AS endpoint,
           COALESCE(NULLIF(a.model, ''), NULLIF(u.requested_model, ''), NULLIF(r.usage_json->>'RequestedModel', ''),
                    NULLIF(u.model, ''), NULLIF(r.usage_json->>'Model', ''), NULLIF(o.requested_model, ''), NULLIF(o.model, '')) AS requested_model,
           COALESCE(NULLIF(u.upstream_model, ''), NULLIF(r.usage_json->>'UpstreamModel', ''), NULLIF(o.upstream_model, '')) AS effective_model,
           CASE WHEN a.finished_at <= $3 THEN a.finished_at END AS observed_finish,
           attempt.count AS attempt_count, attempt.ordinal AS last_attempt_ordinal,
           attempt.outcome AS last_attempt_outcome, attempt.acceptance AS last_acceptance,
           r.id AS observed_receipt_id, r.attempt_id AS receipt_attempt_id, r.completion_state,
           CASE WHEN r.state = 'settled' AND r.settled_at > $3 THEN 'pending' ELSE r.state END AS observed_receipt_state,
           CASE WHEN r.settled_at <= $3 THEN r.settled_at END AS settled_at,
           r.pricing_state, r.billing_command, r.usage_json,
           u.id AS observed_usage_id, u.created_at AS legacy_usage_at, u.input_tokens, u.output_tokens,
           COALESCE(r.usage_json->>'DurationMs', u.duration_ms::text) AS usage_duration_ms,
           COALESCE(r.usage_json->>'FirstTokenMs', u.first_token_ms::text) AS usage_ttft_ms,
           o.duration_ms::text AS ops_duration_ms, o.time_to_first_token_ms::text AS ops_ttft_ms,
           COALESCE(NULLIF(a.legacy_id, ''), a.id) AS client_correlation_id,
           o.routing_latency_ms, o.status_code, o.upstream_status_code, o.account_id AS ops_account_id,
           o.error_owner, o.stream, o.created_at AS ops_at
    FROM anchors a
    LEFT JOIN LATERAL (
        SELECT count(*) OVER () AS count, x.ordinal, x.outcome, x.acceptance
        FROM gateway_execution_attempts x
        WHERE x.request_id = a.journal_id AND x.dispatched_at <= $3
        ORDER BY x.ordinal DESC LIMIT 1
    ) attempt ON true
    LEFT JOIN LATERAL (
        SELECT evidence.*, link.attempt_id
        FROM gateway_execution_attempts x
        JOIN gateway_execution_receipts link USING (attempt_id)
        JOIN usage_settlement_receipts evidence ON evidence.id = link.receipt_id
        WHERE x.request_id = a.journal_id AND x.dispatched_at <= $3 AND NOT x.abandoned_uncertain
          AND evidence.created_at <= $3
        UNION ALL
        SELECT evidence.*, NULL::uuid
        FROM usage_settlement_receipts evidence
        WHERE evidence.id = a.receipt_id AND evidence.created_at <= $3
        LIMIT 1
    ) r ON true
    LEFT JOIN usage_logs u ON u.id = COALESCE(a.usage_id, r.usage_log_id) AND u.created_at <= $3
        AND u.api_key_id = a.api_key_id AND u.user_id = a.funding_user_id
        AND (r.id IS NULL OR (u.request_id = r.request_id AND u.account_id = r.account_id))
        AND NOT EXISTS (
            SELECT 1 FROM project_resources p
            WHERE p.kind = 'usage_logs' AND p.resource_id = u.id
              AND (p.workspace_id, p.project_id) <> (a.workspace_id, a.project_id)
        )
        AND NOT EXISTS (SELECT 1 FROM identity_scope_quarantine q WHERE q.kind = 'usage_logs' AND q.resource_id = u.id)
    LEFT JOIN LATERAL (
        SELECT error.inbound_endpoint, error.requested_model, error.model, error.upstream_model, error.routing_latency_ms,
               error.time_to_first_token_ms, error.duration_ms, error.status_code, error.upstream_status_code,
               error.account_id, error.error_owner, error.stream, error.created_at
        FROM ops_error_logs error
        WHERE error.api_key_id = a.api_key_id AND error.user_id = COALESCE(a.owner_id, a.funding_user_id)
          AND error.created_at <= $3 AND NOT error.is_count_tokens
          AND (error.status_code >= 400 OR (error.stream AND error.status_code >= 200))
          AND (
              (a.journal_id IS NOT NULL AND error.created_at >= a.started_at
               AND (error.request_id = a.id OR (a.origin = 'request' AND error.request_id = a.legacy_id)))
              OR (a.journal_id IS NULL AND error.request_id = substring(a.legacy_id FROM '^local:(.+)$'))
          )
        ORDER BY error.created_at DESC, error.id DESC LIMIT 1
    ) o ON true
), states AS (
    SELECT *,
           CASE
               WHEN endpoint ~ '(^|/)chat/completions/?$' OR endpoint = 'chat' THEN 'openai_chat'
               WHEN endpoint ~ '(^|/)responses(/compact)?/?$' THEN 'openai_responses'
               WHEN endpoint ~ '(^|/)messages(/count_tokens)?/?$' THEN 'anthropic_messages'
               WHEN endpoint ~ ':(streamGenerateContent|generateContent)$' THEN 'gemini_generate'
               WHEN endpoint ~ ':countTokens$' THEN 'gemini_count_tokens'
               WHEN endpoint ~ ':(batchEmbedContents|embedContent)$' THEN 'gemini_embedding'
               WHEN endpoint ~ '(^|/)images/(generations|edits|tasks)/?$' THEN 'openai_images'
               WHEN endpoint ~ '(^|/)embeddings/?$' THEN 'openai_embeddings'
               WHEN endpoint ~ '(^|/)audio/(speech|transcriptions|translations)/?$' THEN 'openai_audio'
               WHEN endpoint ~ '(^|/)videos/[^/]+/?$' OR endpoint ~ '(^|/)videos/?$' THEN 'openai_video'
           END AS protocol,
           CASE
               WHEN completion_state = 'interrupted' THEN 'interrupted'
               -- BE-03 response state: 2 is semantic output, 3 is emitting the terminal,
               -- and 5 means the client terminal write failed.
               WHEN observed_finish IS NOT NULL AND response_state = 5 THEN 'interrupted'
               WHEN observed_finish IS NOT NULL AND canceled AND response_state IN (2, 3) THEN 'interrupted'
               WHEN observed_finish IS NOT NULL AND canceled THEN 'canceled'
               WHEN completion_state = 'complete' AND (journal_id IS NULL OR observed_finish IS NOT NULL) THEN 'succeeded'
               WHEN status_code = 499 THEN 'canceled'
               WHEN ops_at IS NOT NULL AND stream AND (ops_ttft_ms IS NOT NULL OR response_state = 2) THEN 'interrupted'
               WHEN ops_at IS NOT NULL THEN 'failed'
               WHEN journal_id IS NULL AND observed_usage_id IS NOT NULL THEN 'succeeded'
               WHEN journal_id IS NOT NULL AND observed_finish IS NULL AND deadline > $3 THEN
                   CASE WHEN attempt_count IS NULL THEN 'admitted' ELSE 'running' END
               WHEN observed_finish IS NOT NULL AND (last_attempt_outcome IS NULL OR last_acceptance = 'not_sent') THEN 'failed'
               ELSE 'unknown'
           END AS outcome,
           COALESCE(observed_receipt_state, 'unconfirmed') AS receipt_state,
           CASE WHEN observed_receipt_id IS NULL THEN 'unknown'
                WHEN NULLIF(billing_command->>'SubscriptionID', '') IS NOT NULL THEN 'subscription'
                ELSE 'wallet' END AS funding_source
    FROM observations
), projected AS (
    SELECT *,
           CASE
               WHEN outcome = 'interrupted' THEN 'STREAM_INTERRUPTED'
               WHEN outcome = 'canceled' THEN 'REQUEST_CANCELED'
               WHEN outcome = 'unknown' AND last_acceptance = 'unknown' THEN 'ACCEPTANCE_UNKNOWN'
               WHEN outcome = 'unknown' AND observed_receipt_id IS NULL THEN 'RECEIPT_UNCONFIRMED'
               WHEN outcome = 'failed' AND (error_owner = 'provider' OR last_attempt_outcome = 'rejected') THEN
                   CASE WHEN upstream_status_code = 429 THEN 'UPSTREAM_RATE_LIMITED' ELSE 'UPSTREAM_ERROR' END
               WHEN outcome = 'failed' AND error_owner = 'client' THEN 'REQUEST_REJECTED'
               WHEN outcome = 'failed' THEN 'GATEWAY_ERROR'
           END AS reason,
           CASE WHEN receipt_state = 'unconfirmed' THEN 'unknown'
                WHEN pricing_state IN ('unpriced', 'error') AND receipt_state <> 'settled' THEN 'needs_review'
                ELSE receipt_state END AS settlement_state,
           CASE WHEN $4 AND receipt_state = 'settled' THEN
               CASE WHEN pricing_state = 'free' THEN '0.00000000'
                    WHEN pricing_state = 'priced' THEN
                        (CASE WHEN funding_source = 'subscription' THEN billing_command->>'SubscriptionCost'
                              ELSE billing_command->>'BalanceCost' END)::numeric(28,8)::text
               END
           END AS charged_amount
    FROM states
)
`

const summaryColumns = `id, started_at, journal_id IS NOT NULL, requested_model, protocol,
outcome, reason, settlement_state, charged_amount, COALESCE(attempt_count, CASE WHEN journal_id IS NOT NULL THEN 0 END)`
