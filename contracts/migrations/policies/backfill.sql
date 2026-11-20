-- Run after the BE-01 project_resources backfill. Keep the old publishers
-- serving until both the field comparison and the scoped rollback drill pass.
BEGIN ISOLATION LEVEL REPEATABLE READ;

WITH source AS (
 SELECT 'group' kind,g.id,r.workspace_id,r.project_id,
 jsonb_build_object(
 'ID',g.id,'Platform',g.platform,'AccountType',g.account_type,'Status',g.status,'IsExclusive',g.is_exclusive,
 'ClaudeCodeOnly',g.claude_code_only,'FallbackGroupID',g.fallback_group_id,
 'FallbackGroupIDOnInvalidRequest',g.fallback_group_id_on_invalid_request,
 'ModelRoutingEnabled',g.model_routing_enabled,'ModelRouting',g.model_routing,
 'ModelAllowlist',g.model_allowlist,'SupportedModelScopes',g.supported_model_scopes,
 'MCPXMLInject',g.mcp_xml_inject,'AllowMessagesDispatch',g.allow_messages_dispatch,'AllowLive',g.allow_live,
 'RequireOAuthOnly',g.require_oauth_only,'RequirePrivacySet',g.require_privacy_set,
 'DefaultMappedModel',g.default_mapped_model,'MessagesDispatchModelConfig',g.messages_dispatch_model_config,
 'ForceOpenAIFast',g.force_openai_fast,'FreeOpenAIFast',g.free_openai_fast,
 'CodexModelsManifestConfig',g.codex_models_manifest_config,'RPMLimit',g.rpm_limit,
 'MaxReasoningEffort',g.max_reasoning_effort,'MaxReasoningEffortOverLimit',g.max_reasoning_effort_over_limit,
 'ReasoningEffortMappings',g.reasoning_effort_mappings,'AllowImageGeneration',g.allow_image_generation,
 'AllowBatchImageGeneration',g.allow_batch_image_generation,'ProfitControlEnabled',g.profit_control_enabled,
 'ProfitMinMargin',g.profit_min_margin,'ProfitSafetyBuffer',g.profit_safety_buffer) fields
 FROM groups g JOIN project_resources r ON r.kind='groups' AND r.resource_id=g.id AND r.workspace_id=g.workspace_id
 WHERE g.deleted_at IS NULL AND NOT EXISTS(SELECT FROM identity_scope_quarantine q WHERE q.kind=r.kind AND q.resource_id=g.id)
 UNION ALL
 SELECT 'composite',c.id,r.workspace_id,r.project_id,
 jsonb_build_object('id',c.id,'group_id',c.group_id,'public_model',c.public_model,'match_type',c.match_type,
 'account_type',c.account_type,'target_platform',c.target_platform,'upstream_model',c.upstream_model,
 'endpoint',c.endpoint,'priority',c.priority,'enabled',c.enabled,'targets',c.ordered_targets)
 FROM composite_model_routes c JOIN groups g ON g.id=c.group_id AND g.deleted_at IS NULL
 JOIN project_resources r ON r.kind='composite_model_routes' AND r.resource_id=c.id AND r.workspace_id=g.workspace_id
 JOIN project_resources gr ON gr.kind='groups' AND gr.resource_id=g.id AND (gr.workspace_id,gr.project_id)=(r.workspace_id,r.project_id)
 WHERE c.deleted_at IS NULL AND NOT EXISTS(SELECT FROM identity_scope_quarantine q WHERE
 (q.kind=r.kind AND q.resource_id=c.id) OR (q.kind=gr.kind AND q.resource_id=g.id))
)
INSERT INTO routing_legacy_intents(source_kind,source_id,workspace_id,project_id,fields)
 SELECT kind,id,workspace_id,project_id,fields FROM source
 ON CONFLICT(source_kind,source_id) DO UPDATE SET
 workspace_id=excluded.workspace_id,project_id=excluded.project_id,fields=excluded.fields,captured_at=now()
 WHERE (routing_legacy_intents.workspace_id,routing_legacy_intents.project_id,routing_legacy_intents.fields)
 IS DISTINCT FROM (excluded.workspace_id,excluded.project_id,excluded.fields);

-- Drop superseded shadow records when a resource is deleted, quarantined or moved.
DELETE FROM routing_legacy_intents i WHERE source_kind IN ('group','composite') AND NOT EXISTS(
 SELECT FROM project_resources r JOIN groups g ON g.workspace_id=r.workspace_id AND g.deleted_at IS NULL
 LEFT JOIN composite_model_routes c ON c.group_id=g.id AND c.id=i.source_id AND c.deleted_at IS NULL
 WHERE (r.workspace_id,r.project_id)=(i.workspace_id,i.project_id)
 AND r.kind='groups' AND r.resource_id=g.id AND NOT EXISTS(SELECT FROM identity_scope_quarantine q WHERE q.kind=r.kind AND q.resource_id=g.id)
 AND (i.source_kind='group' AND i.source_id=g.id OR i.source_kind='composite' AND c.id IS NOT NULL AND EXISTS(
 SELECT FROM project_resources cr WHERE cr.kind='composite_model_routes' AND cr.resource_id=c.id AND (cr.workspace_id,cr.project_id)=(r.workspace_id,r.project_id)
 AND NOT EXISTS(SELECT FROM identity_scope_quarantine q WHERE q.kind=cr.kind AND q.resource_id=c.id))));
COMMIT;
