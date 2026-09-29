-- TPM admission budgets; 276 was the next unused prefix when continuation was authorized.
-- Additive admission-minute TPM budgets. Keep history on binary rollback.
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS tpm_limit BIGINT NOT NULL DEFAULT 0 CHECK (tpm_limit >= 0);
ALTER TABLE groups ADD COLUMN IF NOT EXISTS tpm_limit BIGINT NOT NULL DEFAULT 0 CHECK (tpm_limit >= 0);
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS tpm_limit BIGINT NOT NULL DEFAULT 0 CHECK (tpm_limit >= 0);

CREATE TABLE IF NOT EXISTS gateway_tpm_windows (
    workspace_id BIGINT NOT NULL,
    scope_kind TEXT NOT NULL CHECK (scope_kind IN ('api_key','group','account')),
    scope_id BIGINT NOT NULL,
    window_start TIMESTAMPTZ NOT NULL,
    window_end TIMESTAMPTZ NOT NULL,
    used_tokens BIGINT NOT NULL DEFAULT 0 CHECK (used_tokens >= 0),
    reserved_tokens BIGINT NOT NULL DEFAULT 0 CHECK (reserved_tokens >= 0),
    overrun_tokens BIGINT NOT NULL DEFAULT 0 CHECK (overrun_tokens >= 0),
    PRIMARY KEY (workspace_id,scope_kind,scope_id,window_start),
    CHECK (window_end = window_start + INTERVAL '60 seconds')
);
CREATE INDEX IF NOT EXISTS gateway_tpm_windows_retirement ON gateway_tpm_windows(window_end,workspace_id,scope_kind,scope_id);

-- Deliberately no cascading owner FK: deleting an identity must not erase token
-- evidence required by late receipts or revive a previously used attempt key.
CREATE TABLE IF NOT EXISTS gateway_tpm_reservations (
    workspace_id BIGINT NOT NULL,
    api_key_id BIGINT NOT NULL,
    execution_id TEXT NOT NULL,
    attempt_index INTEGER NOT NULL CHECK (attempt_index > 0),
    scope_kind TEXT NOT NULL CHECK (scope_kind IN ('api_key','group','account')),
    scope_id BIGINT NOT NULL,
    window_start TIMESTAMPTZ NOT NULL,
    reserved_tokens BIGINT NOT NULL CHECK (reserved_tokens >= 0),
    actual_tokens BIGINT CHECK (actual_tokens >= 0),
    request_fingerprint TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('reserved','unknown','settled','released')),
    expires_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (workspace_id,api_key_id,execution_id,attempt_index,scope_kind,scope_id),
    CHECK ((state = 'settled') = (actual_tokens IS NOT NULL))
);
CREATE INDEX IF NOT EXISTS gateway_tpm_reservations_expiry ON gateway_tpm_reservations(expires_at,workspace_id,api_key_id,execution_id,attempt_index) WHERE state='reserved';
-- expiry is a maintenance marker, not permission to refund unknown usage or
-- delete nonterminal/pending receipt evidence.
