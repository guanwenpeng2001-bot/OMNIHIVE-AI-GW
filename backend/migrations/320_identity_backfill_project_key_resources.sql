-- Honor explicit v1 key bindings when backfilling project ownership.
CREATE OR REPLACE FUNCTION identity_backfill_batch(resource_kind text,batch_size integer) RETURNS integer LANGUAGE plpgsql AS $$
DECLARE last_seen bigint; first_gap bigint; processed integer; highest bigint;
BEGIN
 IF batch_size NOT BETWEEN 1 AND 10000 THEN RAISE EXCEPTION 'invalid batch size'; END IF;
 SELECT last_id INTO STRICT last_seen FROM identity_backfill_checkpoints WHERE kind=resource_kind FOR UPDATE;
 -- Sequence IDs can be committed out of order by older writers.
 IF last_seen>0 AND NOT EXISTS(SELECT 1 FROM identity_resource_owners WHERE kind=resource_kind AND id>last_seen) THEN
  SELECT min(o.id) INTO first_gap FROM identity_resource_owners o
  WHERE o.kind=resource_kind AND o.id<=last_seen
    AND NOT EXISTS(SELECT 1 FROM project_resources r WHERE r.kind=o.kind AND r.resource_id=o.id)
    AND NOT EXISTS(SELECT 1 FROM identity_scope_quarantine q WHERE q.kind=o.kind AND q.resource_id=o.id);
  IF first_gap IS NOT NULL THEN last_seen:=first_gap-1; END IF;
 END IF;
 WITH ids AS MATERIALIZED (
  SELECT DISTINCT o.id FROM identity_resource_owners o WHERE o.kind=resource_kind AND o.id>last_seen ORDER BY o.id LIMIT batch_size
 ), batch AS MATERIALIZED (
  SELECT resource_kind AS kind,ids.id,
    CASE WHEN count(*)=1 THEN min(coalesce(k.workspace_id,kr.workspace_id,p.workspace_id)) END AS workspace_id,
    CASE WHEN count(*)=1 THEN min(coalesce(k.project_id,kr.project_id,p.id)) END AS project_id
  FROM ids JOIN identity_resource_owners o ON o.id=ids.id AND o.kind=resource_kind
  LEFT JOIN projects p ON p.workspace_id=o.workspace_id AND p.is_default
  LEFT JOIN project_key_policies k ON k.api_key_id=o.key_id AND k.workspace_id=p.workspace_id
  LEFT JOIN project_resources kr ON kr.kind='api_keys' AND kr.resource_id=o.key_id AND kr.workspace_id=o.workspace_id
  GROUP BY ids.id
 ), mapped AS (
  INSERT INTO project_resources(kind,resource_id,workspace_id,project_id)
  SELECT kind,id,workspace_id,project_id FROM batch WHERE project_id IS NOT NULL ON CONFLICT DO NOTHING
 ), quarantine AS (
  INSERT INTO identity_scope_quarantine(kind,resource_id,reason)
  SELECT kind,id,'unproven_owner' FROM batch WHERE project_id IS NULL ON CONFLICT DO NOTHING
 )
 SELECT count(*),max(id) INTO processed,highest FROM batch;
 IF processed>0 THEN UPDATE identity_backfill_checkpoints SET last_id=highest WHERE kind=resource_kind; END IF;
 RETURN processed;
END $$;

-- Repair only legacy usage mappings still assigned to the default project.
-- Keep quarantined/ambiguous owners, conflicting policies, other explicit
-- projects and frozen journal scopes untouched. Update both sides of a legacy
-- receipt/usage pair together so the read model cannot acquire a scope mismatch.
WITH usage_resources AS NOT MATERIALIZED (
 SELECT 'usage_logs'::text AS kind,id,request_id,api_key_id FROM usage_logs
 UNION ALL
 SELECT 'usage_settlement_receipts',id,request_id,api_key_id FROM usage_settlement_receipts
), candidates AS MATERIALIZED (
 SELECT r.kind,r.resource_id,r.workspace_id,r.project_id AS default_project_id,
        kr.project_id,u.request_id,u.api_key_id
 FROM project_resources r
 JOIN usage_resources u ON u.kind=r.kind AND u.id=r.resource_id
 JOIN identity_resource_owners o ON o.kind=r.kind AND o.id=r.resource_id AND o.workspace_id=r.workspace_id
 JOIN projects d ON d.id=r.project_id AND d.workspace_id=r.workspace_id AND d.is_default
 JOIN project_resources kr ON kr.kind='api_keys' AND kr.resource_id=o.key_id AND kr.workspace_id=o.workspace_id
 JOIN projects target ON target.id=kr.project_id AND target.workspace_id=kr.workspace_id AND NOT target.is_default
 LEFT JOIN project_key_policies k ON k.api_key_id=o.key_id
 WHERE (k.api_key_id IS NULL OR (k.workspace_id,k.project_id)=(kr.workspace_id,kr.project_id))
 AND (SELECT count(*) FROM identity_resource_owners owners WHERE owners.kind=o.kind AND owners.id=o.id)=1
 AND NOT EXISTS (
  SELECT 1 FROM identity_scope_quarantine q
  WHERE (q.kind=r.kind AND q.resource_id=r.resource_id) OR (q.kind='api_keys' AND q.resource_id=o.key_id)
 )
), repairs AS (
 SELECT c.* FROM candidates c
 WHERE NOT EXISTS (
  -- An existing execution owns a frozen scope independently of today's key.
  SELECT 1 FROM gateway_executions e
  WHERE e.request_id::text=c.request_id AND e.api_key_id=c.api_key_id
 )
 AND NOT EXISTS (
  SELECT 1 FROM usage_resources sibling
  LEFT JOIN project_resources mapped ON mapped.kind=sibling.kind AND mapped.resource_id=sibling.id
  WHERE sibling.request_id=c.request_id AND c.request_id<>'' AND sibling.api_key_id=c.api_key_id
  AND (
   EXISTS (SELECT 1 FROM identity_scope_quarantine q WHERE q.kind=sibling.kind AND q.resource_id=sibling.id)
   OR (mapped.workspace_id IS NOT NULL AND mapped.workspace_id<>c.workspace_id)
   OR (mapped.project_id IS NOT NULL AND mapped.project_id NOT IN (c.default_project_id,c.project_id))
   -- A default-project sibling must itself be eligible for the same repair.
   OR (mapped.project_id=c.default_project_id AND NOT EXISTS (
    SELECT 1 FROM candidates other WHERE other.kind=sibling.kind AND other.resource_id=sibling.id
     AND other.workspace_id=c.workspace_id AND other.project_id=c.project_id
   ))
  )
 )
)
UPDATE project_resources r SET project_id=fix.project_id
FROM repairs fix
WHERE r.kind=fix.kind AND r.resource_id=fix.resource_id
 AND r.workspace_id=fix.workspace_id AND r.project_id=fix.default_project_id;
