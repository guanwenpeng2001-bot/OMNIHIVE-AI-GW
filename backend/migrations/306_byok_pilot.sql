-- Enrollment is explicit. Only keys created by the pilot enter this vault.
CREATE TABLE byok_pilot_providers (
    workspace_id bigint NOT NULL REFERENCES workspaces(id), provider text NOT NULL,
    enabled boolean NOT NULL DEFAULT false, service_fee_usd numeric(20,8) NOT NULL DEFAULT 0 CHECK(service_fee_usd>=0), PRIMARY KEY(workspace_id, provider)
);
CREATE TABLE byok_credentials (
    id bigint PRIMARY KEY REFERENCES accounts(id), workspace_id bigint NOT NULL,
    provider text NOT NULL, version bigint NOT NULL DEFAULT 1, secret_version bigint NOT NULL DEFAULT 1,
    state text NOT NULL CHECK (state IN ('unverified','verified','revoked')),
    fingerprint text NOT NULL, force_revoked_at timestamptz, verified_until timestamptz, verified_models text[] NOT NULL DEFAULT '{}',
    project_ids bigint[] NOT NULL, member_ids bigint[] NOT NULL, member_bindings bigint[] NOT NULL,
    key_ids bigint[] NOT NULL, model_ids text[] NOT NULL,
    priority integer NOT NULL CHECK(priority BETWEEN 0 AND 1000),
    allow_platform_fallback boolean NOT NULL DEFAULT false, include_external_in_budget boolean NOT NULL DEFAULT false,
    UNIQUE(workspace_id,id), FOREIGN KEY(workspace_id,provider) REFERENCES byok_pilot_providers(workspace_id,provider),
    CHECK ((state='verified')=(verified_until IS NOT NULL))
);
CREATE INDEX byok_credentials_workspace ON byok_credentials(workspace_id,priority,id);
CREATE TABLE byok_secret_versions (
    workspace_id bigint NOT NULL, credential_id bigint NOT NULL, version bigint NOT NULL,
    reference text NOT NULL UNIQUE, ciphertext text, retired_at timestamptz,
    PRIMARY KEY(workspace_id,credential_id,version),
    FOREIGN KEY(workspace_id,credential_id) REFERENCES byok_credentials(workspace_id,id),
    CHECK ((ciphertext IS NULL)=(retired_at IS NOT NULL))
);
CREATE TABLE byok_operations (
    id uuid PRIMARY KEY, workspace_id bigint NOT NULL, project_scope bigint NOT NULL, actor_id bigint NOT NULL,
    credential_id bigint NOT NULL, event text NOT NULL, at timestamptz NOT NULL DEFAULT now(),
    result jsonb NOT NULL, FOREIGN KEY(workspace_id,credential_id) REFERENCES byok_credentials(workspace_id,id)
);
CREATE TABLE byok_attempt_funding (
    attempt_id uuid PRIMARY KEY REFERENCES gateway_execution_attempts(attempt_id),
    workspace_id bigint NOT NULL, credential_id bigint NOT NULL, grant_version bigint NOT NULL,
    secret_version bigint NOT NULL, source text NOT NULL CHECK(source IN ('byok','wallet')),
    snapshot_id text NOT NULL, price_version text, reservation_id bigint UNIQUE REFERENCES api_key_budget_reservations(id),
    byok_external_estimate jsonb, platform_service_fee jsonb, platform_estimate jsonb,
    frozen_price jsonb NOT NULL, include_external_in_budget boolean NOT NULL,
    request_fallback_consent boolean NOT NULL, approved_wallet_candidates text[] NOT NULL,
    CHECK (source<>'wallet' OR request_fallback_consent),
    FOREIGN KEY(workspace_id,credential_id) REFERENCES byok_credentials(workspace_id,id),
    CHECK (source<>'wallet' OR (reservation_id IS NOT NULL AND price_version IS NOT NULL
          AND platform_estimate IS NOT NULL AND byok_external_estimate IS NULL AND platform_service_fee IS NULL)),
    CHECK (source<>'byok' OR (platform_estimate IS NULL AND platform_service_fee IS NOT NULL))
);
CREATE FUNCTION byok_reject_funding_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'BYOK attempt funding is immutable' USING ERRCODE='23514';
END $$;
CREATE TRIGGER byok_attempt_funding_immutable BEFORE UPDATE OR DELETE ON byok_attempt_funding
FOR EACH ROW EXECUTE FUNCTION byok_reject_funding_change();

CREATE VIEW byok_request_funding AS
SELECT e.workspace_id,e.project_id,e.request_id,e.api_key_id,e.actor_id,a.ordinal,f.attempt_id,f.credential_id,f.source,f.reservation_id,f.price_version,
       f.byok_external_estimate,f.platform_service_fee,f.platform_estimate,f.include_external_in_budget,
       CASE WHEN f.source='wallet' THEN 'not_applicable' WHEN f.byok_external_estimate IS NULL THEN 'unknown' ELSE 'estimated' END AS external_state
FROM byok_attempt_funding f JOIN gateway_execution_attempts a USING(attempt_id)
JOIN gateway_executions e USING(request_id);
