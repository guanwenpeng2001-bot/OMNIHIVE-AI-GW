-- Expand only. Each row records one operator census of all supported deployments
-- for one contract. No migration of application data and no assumed zeroes.
CREATE TABLE IF NOT EXISTS public.retirement_observations (
    contract_id text NOT NULL CHECK (contract_id ~ '^[a-z][a-z0-9-]{0,79}$'),
    observed_at timestamptz NOT NULL,
    evidence_id uuid NOT NULL,
    deployed_versions text[] CHECK (cardinality(deployed_versions) > 0 AND
        array_ndims(deployed_versions) = 1 AND array_position(deployed_versions, NULL) IS NULL AND
        array_to_string(deployed_versions, ',') ~ '^[A-Za-z0-9][A-Za-z0-9._+-]{0,79}(,[A-Za-z0-9][A-Za-z0-9._+-]{0,79})*$'),
    remaining_callers bigint CHECK (remaining_callers >= 0),
    complete_census boolean NOT NULL DEFAULT false,
    coverage_from timestamptz CHECK (coverage_from <= observed_at),
    valid_until timestamptz CHECK (valid_until > observed_at),
    notice_id uuid,
    window_start timestamptz,
    window_end timestamptz CHECK (window_end > window_start),
    minimum_binary text CHECK (minimum_binary ~ '^[A-Za-z0-9][A-Za-z0-9._+-]{0,79}$'),
    minimum_schema integer CHECK (minimum_schema > 0),
    reconciliation_id uuid,
    recovery_drill_id uuid,
    PRIMARY KEY (contract_id, observed_at)
);
