-- Repeatable metadata import for BE-17 operation receipts. Run after migration 317.
-- The recorded project list reflects scope at the time of each operation.
INSERT INTO byok_lifecycle_events(workspace_id,credential_id,project_ids,actor_id,version,event,at)
SELECT o.workspace_id,o.credential_id,
       ARRAY(SELECT jsonb_array_elements_text(o.result#>'{result,scope,project_ids}')::bigint),
       o.actor_id,(o.result#>>'{result,version}')::bigint,
       'credential.'||o.event||CASE WHEN o.result->>'state'='failed' THEN '.failed' ELSE '' END,o.at
FROM byok_operations o
WHERE o.event IN ('verify','rotate','revoke') AND o.result#>>'{result,version}' ~ '^[1-9][0-9]*$'
      AND jsonb_typeof(o.result#>'{result,scope,project_ids}')='array'
ON CONFLICT(workspace_id,credential_id,version) DO NOTHING;
