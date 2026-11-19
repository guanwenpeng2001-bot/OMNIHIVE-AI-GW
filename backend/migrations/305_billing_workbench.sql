-- Live scoped request and immutable receipt evidence. Only legacy USD is
-- known from this producer. A later multi-currency producer must supply its
-- frozen currency before its amounts can join this projection.
CREATE VIEW billing_cost_evidence AS
SELECT a.workspace_id, a.project_id, a.id AS request_id, a.api_key_id,
       coalesce(a.actor_id,a.funding_user_id) AS member_id, a.funding_user_id,
       coalesce(nullif(a.model,''),nullif(r.usage_json->>'RequestedModel',''),
                nullif(r.usage_json->>'Model',''),nullif(u.requested_model,''),u.model) AS model_id,
       a.started_at AS at, 'USD'::text AS currency, a.price_version,
       r.id AS settlement_id, r.created_at AS receipt_at,r.settled_at, r.pricing_state,
       a.journal_id, u.created_at AS usage_at, h.created_at AS held_at, h.updated_at AS hold_updated_at, h.hold_amount,
       CASE WHEN r.id IS NULL THEN 'unknown'
            WHEN r.billing_command->>'SubscriptionID' IS NOT NULL THEN 'subscription' ELSE 'wallet' END AS billing_source,
       CASE WHEN r.id IS NULL AND u.id IS NOT NULL THEN 'estimated'
            WHEN r.id IS NULL THEN 'unknown'
            WHEN r.pricing_state IN ('unpriced','error') AND r.state <> 'settled' THEN 'needs_review'
            ELSE r.state END AS state,
       CASE WHEN r.state='settled' AND r.pricing_state='free' THEN 0::numeric(28,8)
            WHEN r.state='settled' AND r.pricing_state='priced' THEN
              (CASE WHEN r.billing_command->>'SubscriptionID' IS NOT NULL THEN r.billing_command->>'SubscriptionCost'
                    ELSE r.billing_command->>'BalanceCost' END)::numeric(28,8) END AS charge,
       CASE WHEN r.id IS NULL AND u.actual_cost>0 THEN u.actual_cost::numeric(28,8) END AS legacy_estimate,
       CASE WHEN a.journal_id IS NULL THEN NULL ELSE
         (SELECT count(*) FROM gateway_execution_attempts x WHERE x.request_id=a.journal_id AND x.dispatched_at IS NOT NULL)
       END AS attempts,
       CASE WHEN (r.usage_json->'token_usage_observation'->>'input_tokens_known')::boolean
                   OR (r.usage_json->>'InputTokens')::bigint > 0 THEN (r.usage_json->>'InputTokens')::bigint
            WHEN u.input_tokens>0 THEN u.input_tokens END AS input_tokens,
       CASE WHEN (r.usage_json->'token_usage_observation'->>'output_tokens_known')::boolean
                   OR (r.usage_json->>'OutputTokens')::bigint > 0 THEN (r.usage_json->>'OutputTokens')::bigint
            WHEN u.output_tokens>0 THEN u.output_tokens END AS output_tokens,
       CASE WHEN h.status='reserved' THEN h.hold_amount WHEN h.id IS NOT NULL THEN 0::numeric(28,8) END AS reserved,
       CASE WHEN h.balance_held AND h.status='settled' AND h.id=(r.billing_command->>'BudgetReservationID')::bigint
            THEN h.hold_amount ELSE 0 END AS captured_hold
FROM request_anchors a
LEFT JOIN gateway_executions e ON e.request_id=a.journal_id
LEFT JOIN LATERAL (
    SELECT link.receipt_id FROM gateway_execution_attempts x
    JOIN gateway_execution_receipts link USING(attempt_id)
    WHERE x.request_id=a.journal_id AND NOT x.abandoned_uncertain
) linked ON true
LEFT JOIN usage_settlement_receipts r ON r.id=coalesce(linked.receipt_id,a.receipt_id)
LEFT JOIN usage_logs u ON u.id=a.usage_id AND u.user_id=a.funding_user_id AND u.api_key_id=a.api_key_id
    AND NOT EXISTS (SELECT 1 FROM identity_scope_quarantine q WHERE q.kind='usage_logs' AND q.resource_id=u.id)
LEFT JOIN api_key_budget_reservations h ON h.api_key_id=a.api_key_id AND h.user_id=a.funding_user_id
    AND h.request_id=coalesce(nullif(e.reservation_ref,''),r.request_id);

-- Paging fixes the admission cutoff and applies available evidence timestamps
-- to receipt settlement, dispatch, and reservation transitions.
CREATE FUNCTION billing_cost_at(cutoff timestamptz) RETURNS TABLE (
 workspace_id bigint,project_id bigint,request_id text,api_key_id bigint,member_id bigint,
 model_id text,at timestamptz,currency text,price_version text,settlement_id bigint,
 billing_source text,state text,charge numeric,legacy_estimate numeric,attempts bigint,
 input_tokens bigint,output_tokens bigint,reserved numeric
) LANGUAGE sql STABLE AS $$
 SELECT c.workspace_id,c.project_id,c.request_id,c.api_key_id,c.member_id,c.model_id,c.at,c.currency,c.price_version,
 CASE WHEN c.receipt_at<=cutoff THEN c.settlement_id END,
 CASE WHEN c.receipt_at<=cutoff THEN c.billing_source ELSE 'unknown' END,
 CASE WHEN c.receipt_at>cutoff THEN CASE WHEN c.usage_at<=cutoff THEN 'estimated' ELSE 'unknown' END
      WHEN c.settled_at>cutoff THEN 'pending' ELSE c.state END,
 CASE WHEN c.settled_at<=cutoff THEN c.charge END,c.legacy_estimate,
 CASE WHEN c.journal_id IS NOT NULL THEN (SELECT count(*) FROM gateway_execution_attempts x
      WHERE x.request_id=c.journal_id AND x.dispatched_at<=cutoff) END,
 CASE WHEN c.receipt_at<=cutoff OR c.usage_at<=cutoff THEN c.input_tokens END,
 CASE WHEN c.receipt_at<=cutoff OR c.usage_at<=cutoff THEN c.output_tokens END,
 CASE WHEN c.held_at<=cutoff THEN CASE WHEN c.hold_updated_at>cutoff THEN c.hold_amount ELSE c.reserved END END
 FROM billing_cost_evidence c WHERE c.at<=cutoff
$$;

CREATE INDEX billing_project_admission_seek ON gateway_executions(workspace_id, project_id, admitted_at DESC,request_id DESC);
CREATE INDEX billing_wallet_currency_seek ON billing_wallet_events(funding_account_id,currency,posted_at DESC,id DESC);
CREATE INDEX billing_outbox_transaction ON billing_settlement_outbox(transaction_id);

-- Project only proven one-receipt transactions. When an earlier outbox row
-- is backfilled, never infer a wallet movement for a pre-outbox receipt.
CREATE VIEW billing_proven_movements AS
SELECT o.receipt_id,e.id AS event_id FROM billing_settlement_outbox o
JOIN usage_settlement_receipts r ON r.id=o.receipt_id
JOIN billing_funding_accounts f ON f.user_id=r.user_id AND f.currency='USD'
JOIN billing_wallet_events e ON e.transaction_id=o.transaction_id AND e.funding_account_id=f.id AND e.kind='balance_change'
WHERE NOT EXISTS(SELECT 1 FROM billing_settlement_outbox other
                 WHERE other.transaction_id=o.transaction_id AND other.receipt_id<>o.receipt_id);

CREATE TABLE billing_movement_links (
 receipt_id bigint NOT NULL REFERENCES usage_settlement_receipts(id),
 event_id bigint PRIMARY KEY REFERENCES billing_wallet_events(id)
);
CREATE INDEX billing_movement_receipt ON billing_movement_links(receipt_id);
CREATE TABLE billing_read_checkpoints (
 name text PRIMARY KEY, last_receipt bigint NOT NULL DEFAULT 0
);
INSERT INTO billing_read_checkpoints(name) VALUES ('wallet_movements');
CREATE FUNCTION billing_index_new_movements() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO billing_movement_links SELECT receipt_id,event_id FROM billing_proven_movements
 WHERE receipt_id=NEW.receipt_id ON CONFLICT DO NOTHING;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER billing_index_movements AFTER INSERT ON billing_settlement_outbox
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION billing_index_new_movements();

CREATE FUNCTION billing_backfill_movements(batch_size integer) RETURNS integer LANGUAGE plpgsql AS $$
DECLARE checkpoint bigint; ids bigint[];
BEGIN
 IF batch_size IS NULL OR batch_size NOT BETWEEN 1 AND 10000 THEN RAISE EXCEPTION 'invalid batch size'; END IF;
 SELECT last_receipt INTO STRICT checkpoint FROM billing_read_checkpoints WHERE name='wallet_movements' FOR UPDATE;
 SELECT array_agg(receipt_id ORDER BY receipt_id) INTO ids FROM
   (SELECT receipt_id FROM billing_settlement_outbox WHERE receipt_id>checkpoint ORDER BY receipt_id LIMIT batch_size) batch;
 IF ids IS NULL THEN RETURN 0; END IF;
 INSERT INTO billing_movement_links SELECT receipt_id,event_id FROM billing_proven_movements WHERE receipt_id=ANY(ids)
 ON CONFLICT DO NOTHING;
 UPDATE billing_read_checkpoints SET last_receipt=ids[cardinality(ids)] WHERE name='wallet_movements';
 RETURN cardinality(ids);
END $$;

-- Seed a bounded first batch. Subsequent autocommit calls resume from the stored ID.
SELECT billing_backfill_movements(1000);

CREATE VIEW billing_scoped_postings AS
SELECT c.workspace_id,c.project_id,c.request_id,c.settlement_id,c.api_key_id,c.member_id,c.model_id,c.billing_source,c.state,
       e.id AS event_id,e.funding_account_id,e.currency,e.amount,e.posted_at
FROM billing_cost_evidence c
JOIN (
 SELECT receipt_id,event_id FROM billing_movement_links
 UNION ALL
 SELECT p.* FROM billing_proven_movements p WHERE NOT EXISTS
    (SELECT 1 FROM billing_movement_links l WHERE l.event_id=p.event_id)
) link ON link.receipt_id=c.settlement_id
JOIN billing_wallet_events e ON e.id=link.event_id;

CREATE TABLE billing_commands (
 id uuid PRIMARY KEY, workspace_id bigint NOT NULL, project_id bigint NOT NULL, actor_id bigint NOT NULL,
 route text NOT NULL, key_hash bytea NOT NULL, fingerprint bytea NOT NULL, result jsonb,
 created_at timestamptz NOT NULL DEFAULT now(), expires_at timestamptz NOT NULL DEFAULT now()+interval '15 minutes',
 FOREIGN KEY(workspace_id,project_id) REFERENCES projects(workspace_id,id),
 UNIQUE(workspace_id,project_id,actor_id,route,key_hash)
);
CREATE TABLE billing_adjustment_effects (
 id uuid PRIMARY KEY REFERENCES billing_commands(id), workspace_id bigint NOT NULL, project_id bigint NOT NULL,
 request_id text NOT NULL, receipt_id bigint NOT NULL REFERENCES usage_settlement_receipts(id),
 original_event_id bigint NOT NULL REFERENCES billing_wallet_events(id),
 event_id bigint NOT NULL UNIQUE REFERENCES billing_wallet_events(id),
 currency text NOT NULL, reason text NOT NULL, note text NOT NULL, posted_at timestamptz NOT NULL DEFAULT now(),
 FOREIGN KEY(workspace_id,project_id) REFERENCES projects(workspace_id,id)
);
CREATE INDEX billing_adjustment_seek ON billing_adjustment_effects(workspace_id,project_id,currency,posted_at DESC,id DESC);
CREATE TRIGGER billing_adjustment_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON billing_adjustment_effects
FOR EACH STATEMENT EXECUTE FUNCTION billing_reject_history_change();

CREATE VIEW billing_discrepancies AS
SELECT c.workspace_id,c.project_id,c.request_id,(CASE WHEN c.state='needs_review' THEN 'pricing-' ELSE 'receipt-' END)||c.settlement_id AS id,c.at,c.currency,c.settlement_id,
       CASE WHEN c.state='needs_review' THEN 'PRICING_NEEDS_REVIEW' ELSE 'WALLET_DIFFERENCE' END AS reason,
       diff.amount AS difference
FROM billing_cost_evidence c
LEFT JOIN billing_settlement_outbox o ON o.receipt_id=c.settlement_id
LEFT JOIN LATERAL (
 SELECT (c.charge - c.captured_hold + coalesce(sum(e.amount),0))::numeric(28,8) AS amount
 FROM billing_scoped_postings e WHERE e.settlement_id=c.settlement_id
) diff ON o.receipt_id IS NOT NULL AND c.billing_source='wallet' AND NOT EXISTS (
 SELECT 1 FROM billing_settlement_outbox other WHERE other.transaction_id=o.transaction_id AND other.receipt_id<>o.receipt_id)
WHERE c.state='needs_review' OR diff.amount<>0
UNION ALL
SELECT g.workspace_id,g.project_id,g.request_id::text,'attempt-'||g.attempt_id,a.dispatched_at,'USD',NULL,
       CASE WHEN g.reason='abandoned_uncertain' THEN 'ABANDONED_UNCERTAIN' ELSE 'RECEIPT_UNCONFIRMED' END,NULL
FROM gateway_execution_reconciliation g JOIN gateway_execution_attempts a USING(attempt_id);

CREATE TABLE billing_case_resolutions (
 id uuid PRIMARY KEY REFERENCES billing_commands(id), workspace_id bigint NOT NULL, project_id bigint NOT NULL,
 case_id text NOT NULL, request_id text NOT NULL, at timestamptz NOT NULL,currency text NOT NULL,
 reason text NOT NULL,difference numeric(28,8),receipt_id bigint REFERENCES usage_settlement_receipts(id),
 evidence_id text NOT NULL,note text NOT NULL, resolved_at timestamptz NOT NULL DEFAULT now(),
 FOREIGN KEY(workspace_id,project_id) REFERENCES projects(workspace_id,id), UNIQUE(workspace_id,project_id,case_id)
);
CREATE TRIGGER billing_resolution_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON billing_case_resolutions
FOR EACH STATEMENT EXECUTE FUNCTION billing_reject_history_change();
CREATE VIEW billing_cases AS
 SELECT d.*, 'needs_review'::text AS state,1 AS version FROM billing_discrepancies d WHERE NOT EXISTS
 (SELECT 1 FROM billing_case_resolutions r WHERE (r.workspace_id,r.project_id,r.case_id)=(d.workspace_id,d.project_id,d.id))
 UNION ALL
 SELECT workspace_id,project_id,request_id,case_id,at,currency,receipt_id,reason,difference,'resolved',2 FROM billing_case_resolutions;

CREATE TABLE billing_project_budgets (
 workspace_id bigint NOT NULL,project_id bigint NOT NULL, currency text NOT NULL CHECK(currency~'^[A-Z]{3}$'),
 mode text NOT NULL CHECK(mode IN ('soft','hard')), ceiling numeric(28,8) NOT NULL CHECK(ceiling>=0),
 period text NOT NULL CHECK(period IN ('calendar_day','calendar_week','calendar_month')),
 timezone text NOT NULL, include_external boolean NOT NULL,version bigint NOT NULL,
 PRIMARY KEY(workspace_id,project_id,currency), FOREIGN KEY(workspace_id,project_id) REFERENCES projects(workspace_id,id)
);
CREATE TABLE billing_project_holds (
 workspace_id bigint NOT NULL,project_id bigint NOT NULL,currency text NOT NULL,request_id text NOT NULL,
 amount numeric(28,8) NOT NULL CHECK(amount >= 0), admitted_at timestamptz NOT NULL,released_at timestamptz,
 PRIMARY KEY(workspace_id,project_id,currency,request_id),
 FOREIGN KEY(workspace_id,project_id,currency) REFERENCES billing_project_budgets
);
CREATE INDEX billing_hold_window ON billing_project_holds(workspace_id,project_id,currency,admitted_at) WHERE released_at IS NULL;

CREATE TABLE billing_export_rows (
 export_id uuid NOT NULL REFERENCES billing_commands(id) ON DELETE CASCADE, at timestamptz NOT NULL,
 request_id text COLLATE "C" NOT NULL, settlement_id bigint, model_id text, currency text NOT NULL,
 billing_source text NOT NULL,state text NOT NULL,charge numeric(28,8),price_version text,legacy_estimate numeric(28,8),
 PRIMARY KEY(export_id,at,request_id)
);
CREATE FUNCTION billing_money(unit text, value numeric) RETURNS jsonb
LANGUAGE sql IMMUTABLE STRICT AS $$
 SELECT jsonb_build_object('currency',unit,'amount',round(value,8)::text)
$$;
CREATE TABLE billing_budget_versions (
 LIKE billing_project_budgets INCLUDING CONSTRAINTS,
 effective_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(workspace_id,project_id,currency,version)
);
CREATE FUNCTION billing_version_budget() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO billing_budget_versions SELECT NEW.*,clock_timestamp();
 RETURN NULL;
END $$;
CREATE TRIGGER billing_budget_version AFTER INSERT OR UPDATE ON billing_project_budgets FOR EACH ROW EXECUTE FUNCTION billing_version_budget();
CREATE TRIGGER billing_budget_versions_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON billing_budget_versions
FOR EACH STATEMENT EXECUTE FUNCTION billing_reject_history_change();

-- Allows delegated finance users to inspect a selected member's personal
-- funding account, after verifying that member's project access.
INSERT INTO identity_actions(action,category) VALUES('billing.payers.read','project') ON CONFLICT DO NOTHING;
