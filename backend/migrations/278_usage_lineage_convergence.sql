-- Lineage convergence for migrations edited in place during branch development.
-- 258/265/271/274 kept their filenames while their contents were revised, so
-- live databases exist with earlier drafts applied (accepted via the checksum
-- compatibility whitelist in migrations_runner.go). This migration replays the
-- semantic deltas idempotently so every lineage converges to the same final
-- schema, functions and evidence, regardless of which draft was applied first.
-- 274's revision was comment-only; nothing to converge.

-- 258 (revised): writer lock protocol. KEY SHARE permits non-key dirty-watermark
-- updates and avoids the shared-to-exclusive upgrade deadlock between concurrent
-- corrections; invalidate_group_usage_rollup_state joins the same protocol.
CREATE OR REPLACE FUNCTION lock_usage_dashboard_writer() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    -- Writers coexist; publication and retention still take FOR UPDATE in this
    -- order. KEY SHARE also permits non-key dirty-watermark updates, avoiding
    -- a shared-to-exclusive lock upgrade between concurrent corrections.
    PERFORM id FROM usage_group_rollup_state WHERE id = 1 FOR KEY SHARE;
    PERFORM id FROM usage_dashboard_aggregation_watermark WHERE id = 1 FOR KEY SHARE;
    RETURN NULL;
END;
$$;

CREATE OR REPLACE FUNCTION invalidate_usage_dashboard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    affected TIMESTAMPTZ;
    group_published_before DATE;
    dashboard_published_before TIMESTAMPTZ;
BEGIN
    -- Retention is a verified projection-preserving operation, not a correction.
    IF TG_OP = 'DELETE' AND current_setting('sub2api.usage_retention', true) = 'on' THEN
        RETURN OLD;
    END IF;
    IF TG_OP = 'INSERT' THEN affected := NEW.created_at;
    ELSIF TG_OP = 'DELETE' THEN affected := OLD.created_at;
    ELSE affected := LEAST(OLD.created_at, NEW.created_at);
    END IF;
    -- Also protect direct writes to a partition, whose statement trigger differs.
    SELECT closed_before INTO group_published_before
      FROM usage_group_rollup_state WHERE id = 1 FOR KEY SHARE;
    SELECT last_aggregated_at INTO dashboard_published_before
      FROM usage_dashboard_aggregation_watermark WHERE id = 1 FOR KEY SHARE;
    -- A correction can change both states (the group INSERT trigger runs after
    -- these row triggers). Serialize corrections group-first before either
    -- non-key update, including transactions containing multiple statements.
    IF affected < dashboard_published_before
       OR (affected AT TIME ZONE current_setting('TimeZone'))::date < group_published_before THEN
        PERFORM id FROM usage_group_rollup_state WHERE id = 1 FOR NO KEY UPDATE;
    END IF;
    IF affected < dashboard_published_before THEN
        UPDATE usage_dashboard_aggregation_watermark
           SET dirty_from = LEAST(dirty_from, affected), updated_at = NOW()
         WHERE id = 1 AND affected < last_aggregated_at;
    END IF;
    IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION invalidate_group_usage_rollup_state()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    affected_date DATE;
    published_before DATE;
    configured_timezone TEXT := current_setting('TimeZone');
BEGIN
    IF TG_OP = 'DELETE' THEN
        affected_date := (OLD.created_at AT TIME ZONE configured_timezone)::date;
    ELSIF OLD.group_id IS NULL THEN
        affected_date := (NEW.created_at AT TIME ZONE configured_timezone)::date;
    ELSIF NEW.group_id IS NULL THEN
        affected_date := (OLD.created_at AT TIME ZONE configured_timezone)::date;
    ELSE
        affected_date := LEAST(
            (OLD.created_at AT TIME ZONE configured_timezone)::date,
            (NEW.created_at AT TIME ZONE configured_timezone)::date
        );
    END IF;
    SELECT closed_before INTO published_before
      FROM usage_group_rollup_state WHERE id = 1 FOR KEY SHARE;
    IF published_before > affected_date THEN
        UPDATE usage_group_rollup_state
           SET closed_before = LEAST(closed_before, affected_date), updated_at = NOW()
         WHERE id = 1;
    END IF;
    IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END;
$$;

-- 265 (revised): preserve finite, explicitly saved pre-catalog configuration on
-- upgrade. Idempotent: accounts already carrying new-format evidence win and are
-- excluded by the guard, so replaying changes nothing for them.
WITH legacy_accounts AS (
    SELECT accounts.id, accounts.credentials, COALESCE(accounts.extra, '{}'::jsonb) AS extra,
           accounts.parent_account_id,
           COALESCE((credential_owner.extra->'upstream_model_catalog_state'->>'identity_revision')::bigint, 0) AS credential_identity_revision
    FROM accounts
    LEFT JOIN accounts credential_owner ON credential_owner.id = accounts.parent_account_id
    WHERE accounts.deleted_at IS NULL
      AND (accounts.extra IS NULL OR jsonb_typeof(accounts.extra) = 'object')
      AND NOT COALESCE(accounts.extra, '{}'::jsonb) ?| ARRAY[
          'upstream_model_catalog', 'upstream_model_catalog_state',
          'model_availability', 'model_probe_budget'
      ]
      AND (accounts.parent_account_id IS NULL OR
           (credential_owner.deleted_at IS NULL AND credential_owner.platform = 'openai'
            AND credential_owner.type = 'oauth' AND credential_owner.parent_account_id IS NULL))
), legacy_models AS (
    SELECT legacy_accounts.id, mapping.value #>> '{}' AS model
    FROM legacy_accounts
    CROSS JOIN LATERAL jsonb_each(CASE
        WHEN jsonb_typeof(legacy_accounts.credentials->'model_mapping') = 'object'
        THEN legacy_accounts.credentials->'model_mapping' ELSE '{}'::jsonb END) mapping
    WHERE jsonb_typeof(mapping.value) = 'string'
), finite_models AS (
    SELECT id, jsonb_agg(jsonb_build_object(
        'id', model, 'endpoints', '[]'::jsonb,
        'evidence_kind', 'legacy-mapping-upgrade',
        'evidence_observed_at', transaction_timestamp()
    ) ORDER BY model) AS members
    FROM (SELECT DISTINCT model, id FROM legacy_models) distinct_models
    WHERE model <> '' AND model = btrim(model) AND model !~ '[*?[:cntrl:]]'
    GROUP BY id
)
UPDATE accounts SET extra = legacy_accounts.extra || jsonb_build_object(
    'upstream_model_catalog', jsonb_build_object(
        'legacy_account_id', accounts.id,
        'revision', 1, 'identity_revision', 0, 'rules_revision', 0,
        'credential_account_id', COALESCE(legacy_accounts.parent_account_id, 0),
        'credential_identity_revision', legacy_accounts.credential_identity_revision,
        'source_kind', 'legacy-mapping-upgrade', 'discovery_scope', 'legacy-mapping-upgrade',
        'observed_at', transaction_timestamp(),
        'complete', true, 'discovered_models', '[]'::jsonb,
        'eligible_models', finite_models.members, 'excluded_models', '[]'::jsonb
    ),
    'upstream_model_catalog_state', jsonb_build_object(
        'identity_revision', 0, 'rules_revision', 0, 'result', 'legacy_seeded'
    )
)
FROM legacy_accounts JOIN finite_models ON finite_models.id = legacy_accounts.id
WHERE accounts.id = legacy_accounts.id;

-- 271 (revised): tombstones are identified by deleted_at alone; custom secrets may
-- use any allowed prefix, including __deleted__. Digest every live plaintext key
-- the first draft skipped. Idempotent via ON CONFLICT DO NOTHING.
INSERT INTO api_key_credentials (workspace_id, api_key_id, credential_digest, display_suffix, status, created_at, updated_at)
SELECT a.workspace_id, a.id, encode(sha256(a.key::bytea), 'hex'), right(a.key, 4), 'current', NOW(), NOW()
FROM api_keys a
WHERE a.deleted_at IS NULL AND a.key <> ''
ON CONFLICT (credential_digest) DO NOTHING;

DO $api_key_credential_verify$
BEGIN
    IF EXISTS (
        SELECT 1 FROM api_keys a
        WHERE a.deleted_at IS NULL AND a.key <> ''
          AND NOT EXISTS (
              SELECT 1 FROM api_key_credentials c
              WHERE c.api_key_id = a.id AND c.credential_digest = encode(sha256(a.key::bytea), 'hex')
          )
    ) THEN
        RAISE EXCEPTION 'api key credential backfill incomplete; refusing to clear plaintext';
    END IF;
END;
$api_key_credential_verify$;
