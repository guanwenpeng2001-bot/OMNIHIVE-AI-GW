-- Durable success receipts, committed with the staged accounts and credentials.
-- Deliberately no expires_at or cascading foreign keys: deleting an account,
-- actor or workspace must not make an already-used import key executable again.
CREATE TABLE IF NOT EXISTS staged_account_import_receipts (
    workspace_id BIGINT NOT NULL CHECK (workspace_id > 0),
    operation VARCHAR(64) NOT NULL CHECK (operation = 'staged-account-import'),
    idempotency_key_hash VARCHAR(64) NOT NULL CHECK (idempotency_key_hash ~ '^[0-9a-f]{64}$'),
    actor_id BIGINT NOT NULL CHECK (actor_id > 0),
    receipt_version INTEGER NOT NULL CHECK (receipt_version = 1),
    sealed_receipt TEXT NOT NULL CHECK (length(sealed_receipt) > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (workspace_id, operation, idempotency_key_hash)
);

-- A key belongs permanently to one import mode and actor in a workspace.
-- Reserve in a short transaction BEFORE either mode begins business execution.
-- No payload, token, request digest, TTL, or resource-deletion cascade here.
CREATE TABLE IF NOT EXISTS account_import_key_modes (
    workspace_id BIGINT NOT NULL CHECK (workspace_id > 0),
    idempotency_key_hash VARCHAR(64) NOT NULL CHECK (idempotency_key_hash ~ '^[0-9a-f]{64}$'),
    actor_id BIGINT NOT NULL CHECK (actor_id > 0),
    import_mode VARCHAR(16) NOT NULL CHECK (import_mode IN ('staged', 'legacy')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (workspace_id, idempotency_key_hash)
);

-- Also cover receipts created with the earlier, undeployed 289 draft.
INSERT INTO account_import_key_modes (workspace_id, idempotency_key_hash, actor_id, import_mode)
SELECT workspace_id, idempotency_key_hash, actor_id, 'staged'
FROM staged_account_import_receipts
ON CONFLICT (workspace_id, idempotency_key_hash) DO NOTHING;
