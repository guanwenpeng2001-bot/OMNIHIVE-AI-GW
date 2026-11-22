-- Tariff content remains immutable. A separate, audited publication interval
-- owns activation; old receipts continue to contain the original tariff.
CREATE TABLE IF NOT EXISTS price_book_bindings (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    scope_key text NOT NULL,
    version_id text NOT NULL,
    effective_from timestamptz NOT NULL,
    effective_until timestamptz,
    published_by bigint NOT NULL CHECK (published_by > 0),
    published_at timestamptz NOT NULL DEFAULT now(),
    supersedes bigint REFERENCES price_book_bindings(id),
    closed_by bigint,
    closed_at timestamptz,
    FOREIGN KEY (scope_key, version_id) REFERENCES exact_price_versions(scope_key, version_id),
    UNIQUE (scope_key, effective_from),
    CHECK (effective_until IS NULL OR effective_until > effective_from),
    CHECK ((closed_by IS NULL) = (closed_at IS NULL))
);
CREATE INDEX IF NOT EXISTS price_book_bindings_effective_idx
    ON price_book_bindings(scope_key, effective_from DESC);
CREATE UNIQUE INDEX IF NOT EXISTS price_book_bindings_open_idx
    ON price_book_bindings(scope_key) WHERE effective_until IS NULL;

-- Preserve all historical source values. New records distinguish origin from
-- approval; changing this constraint never rewrites a historical assertion.
ALTER TABLE catalog_capability_evidence
    DROP CONSTRAINT IF EXISTS catalog_capability_evidence_source_check;
ALTER TABLE catalog_capability_evidence ADD CONSTRAINT catalog_capability_evidence_source_check
    CHECK (source IN ('approved_registry','legacy_discovery','connection_verification','legacy_visibility',
                     'builtin_registry','platform_preset','pi_ai','account_discovery','operator_override'));
