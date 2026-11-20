SELECT o.kind, count(*) AS source_rows,
 count(*) FILTER (WHERE r.resource_id IS NULL AND q.resource_id IS NULL) AS awaiting_mapping,
 count(*) FILTER (WHERE q.resource_id IS NOT NULL) AS quarantined,
 count(*) FILTER (WHERE r.resource_id IS NOT NULL AND r.workspace_id IS DISTINCT FROM o.workspace_id) AS ownership_conflicts
FROM identity_resource_owners o
LEFT JOIN project_resources r ON r.kind=o.kind AND r.resource_id=o.id
LEFT JOIN identity_scope_quarantine q ON q.kind=o.kind AND q.resource_id=o.id
GROUP BY o.kind ORDER BY o.kind;
