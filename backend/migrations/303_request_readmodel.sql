CREATE INDEX gateway_requests_seek ON gateway_executions
    (workspace_id, project_id, admitted_at DESC, (request_id::text COLLATE "C") DESC);
CREATE INDEX gateway_requests_lookup ON gateway_executions
    (workspace_id, project_id, (request_id::text COLLATE "C"));
CREATE INDEX gateway_routing_decisions_latest ON gateway_routing_decisions
    (request_id, recorded_at DESC, snapshot_id DESC);

-- Historical rows require BE-01's durable resource mapping. A receipt can
-- prove the matching usage row without a second backfill, but cannot override
-- an existing, different project assignment.
CREATE VIEW request_legacy_receipts AS
SELECT p.workspace_id, p.project_id, r.id, r.request_id, r.api_key_id, r.user_id,
       r.account_id, r.created_at, u.id AS usage_id
FROM project_resources p
JOIN usage_settlement_receipts r ON r.id = p.resource_id
LEFT JOIN usage_logs u ON u.request_id = r.request_id AND u.api_key_id = r.api_key_id
    AND u.user_id = r.user_id AND u.account_id = r.account_id
    AND (r.usage_log_id IS NULL OR r.usage_log_id = u.id)
    AND r.request_id <> ''
    AND NOT EXISTS (
        SELECT 1 FROM project_resources other
        WHERE other.kind = 'usage_logs' AND other.resource_id = u.id
          AND (other.workspace_id, other.project_id) <> (p.workspace_id, p.project_id)
    )
WHERE p.kind = 'usage_settlement_receipts'
  AND NOT EXISTS (SELECT 1 FROM gateway_execution_receipts l WHERE l.receipt_id = r.id)
  AND NOT EXISTS (SELECT 1 FROM identity_scope_quarantine q
                  WHERE q.kind = p.kind AND q.resource_id = p.resource_id);

-- Do not project linked usage a second time. A matched historical receipt
-- likewise owns its usage record even when the receipt is still pending.
CREATE VIEW request_anchors AS
SELECT e.request_id::text COLLATE "C" AS id, e.workspace_id, e.project_id, e.api_key_id,
       e.actor_id, e.owner_id, e.funding_user_id, e.kind AS origin, e.route, e.model,
       e.admitted_at AS started_at, e.finished_at, e.deadline, e.response_state, e.canceled,
       e.request_id AS journal_id, NULL::bigint AS receipt_id, NULL::bigint AS usage_id,
       NULLIF(e.legacy_id, '') AS legacy_id, NULLIF(e.price_version, '') AS price_version
FROM gateway_executions e
UNION ALL
SELECT 'legacy-receipt-' || r.id, r.workspace_id, r.project_id, r.api_key_id,
       NULL, NULL, r.user_id, 'legacy', '', '', r.created_at, NULL, NULL, 0, false,
       NULL, r.id, r.usage_id, NULLIF(r.request_id, ''), NULL
FROM request_legacy_receipts r
UNION ALL
SELECT 'legacy-usage-' || u.id, p.workspace_id, p.project_id, u.api_key_id,
       NULL, NULL, u.user_id, 'legacy', COALESCE(u.inbound_endpoint, ''),
       COALESCE(NULLIF(u.requested_model, ''), u.model),
       u.created_at, NULL, NULL, 0, false,
       NULL, NULL, u.id, NULLIF(u.request_id, ''), NULL
FROM project_resources p
JOIN usage_logs u ON u.id = p.resource_id
WHERE p.kind = 'usage_logs'
  AND NOT EXISTS (SELECT 1 FROM identity_scope_quarantine q
                  WHERE q.kind = p.kind AND q.resource_id = p.resource_id)
  AND NOT EXISTS (
      SELECT 1 FROM usage_settlement_receipts r
      LEFT JOIN gateway_execution_receipts l ON l.receipt_id = r.id
      LEFT JOIN gateway_execution_attempts a ON a.attempt_id = l.attempt_id
      LEFT JOIN gateway_executions e ON e.request_id = a.request_id
      LEFT JOIN project_resources mapped ON mapped.kind = 'usage_settlement_receipts' AND mapped.resource_id = r.id
      WHERE r.request_id = u.request_id AND r.api_key_id = u.api_key_id AND r.user_id = u.user_id
        AND r.account_id = u.account_id AND (r.usage_log_id IS NULL OR r.usage_log_id = u.id)
        AND r.request_id <> ''
        AND ((e.workspace_id, e.project_id) = (p.workspace_id, p.project_id)
             OR (l.receipt_id IS NULL AND (mapped.workspace_id, mapped.project_id) = (p.workspace_id, p.project_id)
                 AND NOT EXISTS (SELECT 1 FROM identity_scope_quarantine q
                                 WHERE q.kind = mapped.kind AND q.resource_id = mapped.resource_id)))
  );

CREATE TABLE request_metadata_exports (
    id uuid PRIMARY KEY,
    workspace_id bigint NOT NULL,
    project_id bigint NOT NULL,
    actor_id bigint NOT NULL,
    request_id text COLLATE "C" NOT NULL,
    key_hash bytea NOT NULL CHECK (octet_length(key_hash) = 32),
    sections text[] NOT NULL CHECK (cardinality(sections) BETWEEN 1 AND 5),
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL DEFAULT now() + interval '15 minutes',
    FOREIGN KEY (workspace_id, project_id) REFERENCES projects(workspace_id, id),
    UNIQUE (workspace_id, project_id, actor_id, request_id, key_hash)
);
