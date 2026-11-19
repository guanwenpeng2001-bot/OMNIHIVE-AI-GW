-- Expand only. This journal has no triggers, initial data, or foreign keys
-- into the business tables. A later migration runner owns writes.
CREATE TABLE IF NOT EXISTS refactor_checkpoints (
    run_id UUID NOT NULL,
    module TEXT NOT NULL CHECK (module ~ '^[a-z][a-z0-9_]{0,63}$'),
    workspace_id BIGINT NOT NULL CHECK (workspace_id >= 0),
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    cursor JSONB NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(cursor) = 'object'),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (run_id, module, workspace_id)
);

CREATE INDEX IF NOT EXISTS refactor_checkpoints_latest
    ON refactor_checkpoints (module, workspace_id, updated_at DESC, run_id DESC);

CREATE TABLE IF NOT EXISTS refactor_mapping_exceptions (
    run_id UUID NOT NULL,
    module TEXT NOT NULL,
    workspace_id BIGINT NOT NULL,
    source_kind TEXT NOT NULL CHECK (source_kind ~ '^[a-z][a-z0-9_]{0,63}$'),
    source_ref TEXT NOT NULL CHECK (length(source_ref) BETWEEN 1 AND 256),
    reason_code TEXT NOT NULL CHECK (reason_code ~ '^[a-z][a-z0-9_]{0,63}$'),
    observed_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    resolved_at TIMESTAMPTZ CHECK (resolved_at >= observed_at),
    PRIMARY KEY (run_id, module, workspace_id, source_kind, source_ref),
    FOREIGN KEY (run_id, module, workspace_id)
        REFERENCES refactor_checkpoints (run_id, module, workspace_id)
);

CREATE INDEX IF NOT EXISTS refactor_mapping_exceptions_open
    ON refactor_mapping_exceptions (module, workspace_id, run_id)
    WHERE resolved_at IS NULL;
