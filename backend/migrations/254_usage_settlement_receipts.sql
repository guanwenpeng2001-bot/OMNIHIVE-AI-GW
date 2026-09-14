-- Durable local recovery input. No foreign-key cascade may erase pending usage.
CREATE TABLE IF NOT EXISTS usage_settlement_receipts (
    id BIGSERIAL PRIMARY KEY,
    request_id VARCHAR(255) NOT NULL,
    api_key_id BIGINT NOT NULL,
    user_id BIGINT NOT NULL,
    account_id BIGINT NOT NULL,
    request_fingerprint TEXT NOT NULL,
    usage_json JSONB NOT NULL,
    billing_command JSONB NOT NULL,
    pricing_state TEXT NOT NULL CHECK (pricing_state IN ('priced','free','unpriced','error')),
    pricing_snapshot TEXT NOT NULL,
    resolution_state TEXT NOT NULL CHECK (resolution_state IN ('resolved','unresolved')),
    completion_state TEXT NOT NULL CHECK (completion_state IN ('complete','interrupted')),
    state TEXT NOT NULL CHECK (state IN ('pending','settled','needs_review')),
    usage_log_id BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    settled_at TIMESTAMPTZ,
    UNIQUE (request_id, api_key_id),
    CHECK ((state = 'settled') = (settled_at IS NOT NULL)),
    CHECK (state <> 'settled' OR usage_log_id IS NOT NULL)
);
CREATE INDEX IF NOT EXISTS usage_settlement_receipts_pending_id_idx
    ON usage_settlement_receipts (id) WHERE state = 'pending';
