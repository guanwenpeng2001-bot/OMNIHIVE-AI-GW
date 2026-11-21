CREATE OR REPLACE FUNCTION gateway_execution_bind_receipt() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    execution gateway_executions;
    sent gateway_execution_attempts;
    source usage_settlement_receipts;
    cache_source_id BIGINT;
BEGIN
    IF NEW.request_id !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' THEN
        RETURN NEW;
    END IF;
    SELECT * INTO execution FROM gateway_executions WHERE request_id=NEW.request_id::uuid;
    IF NOT FOUND THEN
        RETURN NEW;
    END IF;
    IF NEW.api_key_id<>execution.api_key_id OR NEW.user_id<>execution.funding_user_id THEN
        RAISE EXCEPTION 'receipt has no eligible dispatched attempt';
    END IF;
    IF COALESCE((NEW.usage_json->'BillingEvidence'->>'cache_hit')::boolean,false) THEN
        cache_source_id := (NEW.usage_json->'BillingEvidence'->>'cache_source_receipt_id')::bigint;
        SELECT * INTO source FROM usage_settlement_receipts WHERE id=cache_source_id;
        IF NOT FOUND OR source.id=NEW.id OR source.api_key_id<>NEW.api_key_id OR source.user_id<>NEW.user_id
           OR source.account_id<>NEW.account_id OR source.state NOT IN ('pending','settled')
           OR source.completion_state<>'complete' OR source.resolution_state<>'resolved'
           OR COALESCE((source.usage_json->'BillingEvidence'->>'cache_hit')::boolean,false)
           OR NOT EXISTS (SELECT 1 FROM gateway_execution_receipts linked
                          JOIN gateway_execution_attempts original USING(attempt_id)
                          WHERE linked.receipt_id=source.id AND original.dispatched_at IS NOT NULL
                          AND NOT original.abandoned_uncertain AND original.acceptance<>'not_sent') THEN
            RAISE EXCEPTION 'cache hit receipt has no eligible frozen source receipt';
        END IF;
        RETURN NEW;
    END IF;
    SELECT * INTO sent FROM gateway_execution_attempts
    WHERE request_id=execution.request_id AND account_id=NEW.account_id AND dispatched_at IS NOT NULL
    ORDER BY ordinal DESC LIMIT 1 FOR UPDATE;
    IF NOT FOUND OR sent.abandoned_uncertain OR sent.acceptance='not_sent' THEN
        RAISE EXCEPTION 'receipt has no eligible dispatched attempt';
    END IF;
    INSERT INTO gateway_execution_receipts(receipt_id,attempt_id) VALUES(NEW.id,sent.attempt_id);
    RETURN NEW;
END $$;
