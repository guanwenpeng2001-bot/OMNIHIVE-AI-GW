CREATE TABLE IF NOT EXISTS exact_price_versions (
    scope_key text NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 256),
    version_id text NOT NULL CHECK (length(version_id) BETWEEN 1 AND 128),
    source text NOT NULL CHECK (length(source) > 0),
    effective_from timestamptz NOT NULL,
    effective_until timestamptz,
    tariff jsonb NOT NULL CHECK (jsonb_typeof(tariff) = 'object'),
    PRIMARY KEY (scope_key, version_id),
    CHECK (effective_until IS NULL OR effective_until > effective_from)
);

CREATE INDEX IF NOT EXISTS exact_price_versions_effective_idx
    ON exact_price_versions (scope_key, effective_from DESC);

CREATE OR REPLACE FUNCTION reject_exact_price_version_change() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'exact price versions are immutable';
END;
$$;

DROP TRIGGER IF EXISTS exact_price_versions_immutable ON exact_price_versions;
CREATE TRIGGER exact_price_versions_immutable
    BEFORE UPDATE OR DELETE ON exact_price_versions
    FOR EACH ROW EXECUTE FUNCTION reject_exact_price_version_change();
