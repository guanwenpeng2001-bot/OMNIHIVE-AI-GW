
-- Persistent tombstones deliberately have no group FK. The ungrouped/global
-- catalog has its own scope and is never represented as a fabricated group.
CREATE TABLE IF NOT EXISTS model_catalog_generations (
    scope TEXT PRIMARY KEY,
    generation BIGINT NOT NULL DEFAULT 0
);
INSERT INTO model_catalog_generations(scope) VALUES ('catalog'), ('channels'), ('error_rules')
ON CONFLICT DO NOTHING;

CREATE OR REPLACE FUNCTION advance_model_catalog_generation(target_scope TEXT, details JSONB)
RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE current_generation BIGINT;
BEGIN
    INSERT INTO model_catalog_generations(scope, generation) VALUES(target_scope, 1)
    ON CONFLICT(scope) DO UPDATE SET generation = model_catalog_generations.generation + 1
    RETURNING generation INTO current_generation;
    INSERT INTO scheduler_outbox(event_type, payload)
    VALUES ('model_catalog_changed', COALESCE(details, '{}'::jsonb) ||
        jsonb_build_object('scope', target_scope, 'generation', current_generation));
    PERFORM pg_notify('model_catalog_changed', jsonb_build_object('scope', target_scope, 'generation', current_generation)::text);
END;
$$;

CREATE OR REPLACE FUNCTION propagate_model_catalog_change()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    old_row JSONB := CASE WHEN TG_OP = 'INSERT' THEN '{}'::jsonb ELSE to_jsonb(OLD) END;
    new_row JSONB := CASE WHEN TG_OP = 'DELETE' THEN '{}'::jsonb ELSE to_jsonb(NEW) END;
    affected_groups BIGINT[] := ARRAY[]::BIGINT[];
    target_account BIGINT;
    affected_accounts BIGINT[] := ARRAY[]::BIGINT[];
    target_group BIGINT;
    details JSONB;
BEGIN
    IF old_row = new_row THEN RETURN NEW; END IF;
    IF TG_TABLE_NAME = 'accounts' THEN
        -- Observations and accounting counters do not affect membership.
        IF TG_OP = 'UPDATE' AND
           (old_row - ARRAY['updated_at','last_used_at','session_window_start','session_window_end','session_window_status','extra']) =
           (new_row - ARRAY['updated_at','last_used_at','session_window_start','session_window_end','session_window_status','extra'])
           AND ((old_row->'extra') - ARRAY(SELECT key FROM jsonb_object_keys(COALESCE(old_row->'extra','{}'::jsonb)) key
                WHERE key LIKE 'codex_%' OR key LIKE 'passive_usage_%' OR key IN ('cursor_usage_snapshot','cursor_usage_updated_at','session_window_utilization','grok_billing_snapshot','grok_usage_snapshot','upstream_billing_probe'))) IS NOT DISTINCT FROM
               ((new_row->'extra') - ARRAY(SELECT key FROM jsonb_object_keys(COALESCE(new_row->'extra','{}'::jsonb)) key
                WHERE key LIKE 'codex_%' OR key LIKE 'passive_usage_%' OR key IN ('cursor_usage_snapshot','cursor_usage_updated_at','session_window_utilization','grok_billing_snapshot','grok_usage_snapshot','upstream_billing_probe')))
        THEN RETURN NEW; END IF;
        target_account := COALESCE((new_row->>'id')::BIGINT, (old_row->>'id')::BIGINT);
        SELECT COALESCE(array_agg(DISTINCT group_id ORDER BY group_id), ARRAY[]::BIGINT[]) INTO affected_groups
        FROM account_groups WHERE account_id = target_account
           OR account_id IN (SELECT id FROM accounts WHERE parent_account_id = target_account);
        SELECT ARRAY(SELECT id FROM accounts WHERE id = target_account OR parent_account_id = target_account ORDER BY id) INTO affected_accounts;
        IF NOT target_account = ANY(affected_accounts) THEN affected_accounts := array_append(affected_accounts, target_account); END IF;
    ELSIF TG_TABLE_NAME = 'proxies' THEN
        SELECT ARRAY(SELECT id FROM accounts WHERE proxy_id = COALESCE((new_row->>'id')::BIGINT, (old_row->>'id')::BIGINT) ORDER BY id) INTO affected_accounts;
        SELECT COALESCE(array_agg(DISTINCT group_id ORDER BY group_id), ARRAY[]::BIGINT[]) INTO affected_groups
        FROM account_groups WHERE account_id = ANY(affected_accounts);
    ELSIF TG_TABLE_NAME = 'groups' THEN
        affected_groups := ARRAY[COALESCE((new_row->>'id')::BIGINT, (old_row->>'id')::BIGINT)];
    ELSIF TG_TABLE_NAME IN ('account_groups','composite_model_routes') THEN
        SELECT ARRAY(SELECT DISTINCT member FROM unnest(ARRAY[(old_row->>'group_id')::BIGINT, (new_row->>'group_id')::BIGINT]) member WHERE member IS NOT NULL ORDER BY member) INTO affected_groups;
        target_account := COALESCE((new_row->>'account_id')::BIGINT, (old_row->>'account_id')::BIGINT);
    ELSIF TG_TABLE_NAME = 'error_passthrough_rules' THEN
        PERFORM advance_model_catalog_generation('error_rules', jsonb_build_object('deleted', TG_OP = 'DELETE'));
        RETURN COALESCE(NEW, OLD);
    ELSIF TG_TABLE_NAME LIKE 'channel%' THEN
        PERFORM advance_model_catalog_generation('channels', jsonb_build_object('deleted', TG_OP = 'DELETE'));
    END IF;
    IF target_account IS NOT NULL AND NOT target_account = ANY(affected_accounts) THEN
        affected_accounts := array_append(affected_accounts, target_account);
    END IF;
    details := jsonb_build_object('account_id', target_account, 'account_ids', affected_accounts, 'group_ids', affected_groups,
        'deleted', TG_OP = 'DELETE' OR new_row->>'deleted_at' IS NOT NULL);
    -- A single global revision also covers Composite, proxy edits, parents and
    -- ungrouped accounts. Per-group revisions survive physical deletion.
    PERFORM advance_model_catalog_generation('catalog', details);
    FOREACH target_group IN ARRAY affected_groups LOOP
        PERFORM advance_model_catalog_generation('group:' || target_group, details);
    END LOOP;
    RETURN COALESCE(NEW, OLD);
END;
$$;

DO $$
DECLARE relation_name TEXT;
BEGIN
    FOREACH relation_name IN ARRAY ARRAY['accounts','groups','account_groups','composite_model_routes','proxies',
        'channels','channel_groups','channel_model_pricing','channel_pricing_intervals','error_passthrough_rules']
    LOOP
        EXECUTE format('DROP TRIGGER IF EXISTS model_catalog_change ON %I', relation_name);
        -- BEFORE DELETE preserves old account membership before FK cascades.
        EXECUTE format('CREATE TRIGGER model_catalog_change BEFORE INSERT OR UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION propagate_model_catalog_change()', relation_name);
    END LOOP;
END;
$$;

-- Preserve finite, explicitly saved pre-catalog configuration on upgrade.
-- This is NOT native discovery or an administrator/probe attestation. The
-- service projects the saved account protocol and still checks its adapter,
-- identity/rules revisions, credential owner, allowlists and current status.
-- Existing new-format evidence (including an empty/revoked snapshot) wins.
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
    FROM (SELECT DISTINCT id, model FROM legacy_models) distinct_models
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
        -- Complete describes this finite import, never a successful discovery.
        'complete', true, 'discovered_models', '[]'::jsonb,
        'eligible_models', finite_models.members, 'excluded_models', '[]'::jsonb
    ),
    'upstream_model_catalog_state', jsonb_build_object(
        'identity_revision', 0, 'rules_revision', 0, 'result', 'legacy_seeded'
    )
)
FROM legacy_accounts JOIN finite_models ON finite_models.id = legacy_accounts.id
WHERE accounts.id = legacy_accounts.id;
