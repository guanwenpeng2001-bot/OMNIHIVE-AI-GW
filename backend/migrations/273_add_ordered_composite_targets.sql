-- Owner: routing-policy-controls. Rollback floor: code that understands ordered_targets.
-- The column is additive and the legacy single-target columns keep mirroring the
-- first enabled target, so a binary that predates it still routes (without
-- cross-platform fallback) on the first target.
ALTER TABLE composite_model_routes ADD COLUMN IF NOT EXISTS ordered_targets JSONB;

-- Idempotent backfill: every existing route becomes one ordered target whose
-- behavior is identical to the legacy single target (weight 100, default latency
-- preference, legacy default sticky TTL, no region/provider constraint). An empty
-- upstream_model keeps the legacy "use the requested model" meaning.
UPDATE composite_model_routes
SET ordered_targets = jsonb_build_array(
      jsonb_build_object(
        'position', 0,
        'target_platform', target_platform,
        'account_type', account_type,
        'upstream_model', upstream_model,
        'latency_preference', 'default',
        'scheduling_weight', 100,
        'enabled', true
      )
    )
WHERE ordered_targets IS NULL;
