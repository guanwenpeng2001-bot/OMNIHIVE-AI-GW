CREATE TABLE IF NOT EXISTS gateway_executions (
    request_id UUID PRIMARY KEY,
    workspace_id BIGINT NOT NULL,
    project_id BIGINT NOT NULL,
    actor_id BIGINT NOT NULL,
    api_key_id BIGINT NOT NULL,
    owner_id BIGINT NOT NULL,
    funding_user_id BIGINT NOT NULL,
    authorization_version BIGINT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('request','turn','job')),
    route TEXT NOT NULL,
    model TEXT NOT NULL,
    legacy_id TEXT NOT NULL,
    reservation_ref TEXT NOT NULL,
    attempt_budget INTEGER NOT NULL CHECK (attempt_budget > 0),
    deadline TIMESTAMPTZ NOT NULL,
    model_version TEXT NOT NULL,
    price_version TEXT NOT NULL,
    pricing_at TIMESTAMPTZ NOT NULL,
    idempotency_hash BYTEA,
    fingerprint BYTEA,
    admitted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ,
    response_state INTEGER NOT NULL DEFAULT 0,
    canceled BOOLEAN NOT NULL DEFAULT false,
    FOREIGN KEY (workspace_id,project_id) REFERENCES projects(workspace_id,id),
    CHECK ((idempotency_hash IS NULL) = (fingerprint IS NULL)),
    CHECK (octet_length(idempotency_hash)=32 AND octet_length(fingerprint)=32)
);
CREATE UNIQUE INDEX IF NOT EXISTS gateway_execution_idempotency
    ON gateway_executions(workspace_id,project_id,actor_id,api_key_id,route,idempotency_hash) WHERE idempotency_hash IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS gateway_execution_job
    ON gateway_executions(workspace_id,api_key_id,route,legacy_id) WHERE kind='job' AND legacy_id<>'';
CREATE INDEX IF NOT EXISTS gateway_execution_expiry ON gateway_executions(deadline,request_id);

CREATE TABLE IF NOT EXISTS gateway_execution_attempts (
    attempt_id UUID PRIMARY KEY,
    request_id UUID NOT NULL REFERENCES gateway_executions(request_id),
    ordinal INTEGER NOT NULL CHECK (ordinal > 0),
    account_id BIGINT,
    dispatched_at TIMESTAMPTZ,
    acceptance TEXT NOT NULL DEFAULT 'not_sent' CHECK (acceptance IN ('not_sent','accepted','unknown')),
    outcome TEXT NOT NULL DEFAULT 'intent' CHECK (outcome IN ('intent','dispatching','not_sent','rejected','transport_response','unknown')),
    abandoned_uncertain BOOLEAN NOT NULL DEFAULT false,
    decision JSONB,
    UNIQUE (request_id,ordinal),
    CHECK ((dispatched_at IS NULL) = (outcome='intent')),
    CHECK (NOT abandoned_uncertain OR acceptance='unknown')
);

CREATE TABLE IF NOT EXISTS gateway_execution_receipts (
    receipt_id BIGINT PRIMARY KEY REFERENCES usage_settlement_receipts(id),
    attempt_id UUID NOT NULL REFERENCES gateway_execution_attempts(attempt_id)
);

CREATE INDEX gateway_execution_receipt_attempt ON gateway_execution_receipts(attempt_id);

CREATE OR REPLACE VIEW gateway_execution_reconciliation AS
SELECT e.workspace_id,e.project_id,e.request_id,a.attempt_id,a.account_id,a.ordinal,
       CASE WHEN a.abandoned_uncertain THEN 'abandoned_uncertain' ELSE 'missing_receipt' END AS reason
FROM gateway_executions e JOIN gateway_execution_attempts a USING(request_id)
WHERE a.dispatched_at IS NOT NULL AND (a.abandoned_uncertain OR
 (e.deadline<=now() AND a.acceptance<>'not_sent' AND NOT EXISTS (
    SELECT 1 FROM gateway_execution_receipts l JOIN gateway_execution_attempts linked USING(attempt_id)
    WHERE linked.request_id=e.request_id
 )));

CREATE OR REPLACE VIEW gateway_execution_release_obligations AS
SELECT e.workspace_id,e.project_id,e.request_id,h.id AS reservation_id,h.api_key_id,h.request_id AS reservation_ref
FROM gateway_executions e JOIN api_key_budget_reservations h
ON h.api_key_id=e.api_key_id AND h.request_id=e.reservation_ref AND h.user_id=e.funding_user_id
WHERE h.status='reserved' AND (e.finished_at IS NOT NULL OR e.deadline<=now());

CREATE FUNCTION gateway_execution_bind_receipt() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    execution gateway_executions;
    sent gateway_execution_attempts;
BEGIN
    IF NEW.request_id !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' THEN
        RETURN NEW;
    END IF;
    SELECT * INTO execution FROM gateway_executions WHERE request_id=NEW.request_id::uuid;
    IF NOT FOUND THEN
        RETURN NEW;
    END IF;
    SELECT * INTO sent FROM gateway_execution_attempts
    WHERE request_id=execution.request_id AND account_id=NEW.account_id AND dispatched_at IS NOT NULL
    ORDER BY ordinal DESC LIMIT 1 FOR UPDATE;
    IF NOT FOUND OR sent.abandoned_uncertain OR sent.acceptance='not_sent'
       OR NEW.api_key_id<>execution.api_key_id OR NEW.user_id<>execution.funding_user_id THEN
        RAISE EXCEPTION 'receipt has no eligible dispatched attempt';
    END IF;
    INSERT INTO gateway_execution_receipts(receipt_id,attempt_id) VALUES(NEW.id,sent.attempt_id);
    RETURN NEW;
END $$;

CREATE TRIGGER gateway_execution_receipt_insert AFTER INSERT ON usage_settlement_receipts
FOR EACH ROW EXECUTE FUNCTION gateway_execution_bind_receipt();

CREATE VIEW gateway_execution_usage AS
SELECT a.request_id,a.attempt_id,r.id AS receipt_id,r.state AS receipt_state,r.resolution_state,
CASE WHEN (r.usage_json->'token_usage_observation'->>'input_tokens_known')::boolean
          OR (r.usage_json->>'InputTokens')::bigint > 0
     THEN (r.usage_json->>'InputTokens')::bigint END AS input_tokens,
CASE WHEN (r.usage_json->'token_usage_observation'->>'output_tokens_known')::boolean
          OR (r.usage_json->>'OutputTokens')::bigint > 0
     THEN (r.usage_json->>'OutputTokens')::bigint END AS output_tokens
FROM gateway_execution_attempts a LEFT JOIN gateway_execution_receipts l USING(attempt_id)
LEFT JOIN usage_settlement_receipts r ON r.id=l.receipt_id;

CREATE VIEW gateway_execution_legacy_ids AS
SELECT e.workspace_id,e.project_id,e.api_key_id,e.request_id,e.kind AS legacy_kind,e.legacy_id,e.legacy_id AS legacy_request_id
FROM gateway_executions e WHERE e.legacy_id<>''
UNION ALL
SELECT e.workspace_id,e.project_id,e.api_key_id,e.request_id,'receipt',r.id::text,r.request_id
FROM gateway_execution_receipts l JOIN gateway_execution_attempts a USING(attempt_id)
JOIN gateway_executions e USING(request_id) JOIN usage_settlement_receipts r ON r.id=l.receipt_id
UNION ALL
SELECT p.workspace_id,p.project_id,r.api_key_id,NULL::uuid,'receipt',r.id::text,r.request_id
FROM project_resources p JOIN usage_settlement_receipts r ON p.kind='usage_settlement_receipts' AND p.resource_id=r.id
WHERE NOT EXISTS (SELECT 1 FROM gateway_execution_receipts l WHERE l.receipt_id=r.id)
UNION ALL
SELECT p.workspace_id,p.project_id,j.api_key_id,e.request_id,'upstream_job',j.id::text,j.public_request_id
FROM project_resources p JOIN upstream_jobs j ON p.kind='upstream_jobs' AND p.resource_id=j.id
LEFT JOIN gateway_executions e ON e.workspace_id=p.workspace_id AND e.project_id=p.project_id
AND e.api_key_id=j.api_key_id AND e.funding_user_id=j.user_id AND e.kind='job' AND e.legacy_id=j.public_request_id;
