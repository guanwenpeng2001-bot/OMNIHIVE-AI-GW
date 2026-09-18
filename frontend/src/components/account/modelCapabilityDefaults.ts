/** Empty input removes the account declaration; semantic validation is server-owned. */
export function parseModelCapabilityDefaults(text: string): Record<string, unknown> | null {
  if (!text.trim()) return null
  const value: unknown = JSON.parse(text)
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    throw new Error('Model capability specifications must be a JSON object / 模型规格必须是 JSON 对象')
  }
  const envelope = value as Record<string, unknown>
  if (envelope.schema_version !== 1 || typeof envelope.reason !== 'string' || !envelope.reason.trim() ||
    !envelope.models || typeof envelope.models !== 'object' || Array.isArray(envelope.models) || !Object.keys(envelope.models).length) {
    throw new Error('Model specifications require schema_version: 1, reason, and models / 模型规格必须包含 schema_version: 1、reason 和非空 models')
  }
  return envelope
}

/** Keep toast diagnostics bounded; the preview retains every model and field. */
export function modelCapabilityGapSummary(result: import('@/api/admin/accounts').SyncUpstreamModelsResult): string {
  return Object.entries(result.metadata ?? {}).flatMap(([id, metadata]) => {
    const gaps = [
      metadata.missing_fields?.length ? `missing_fields: ${metadata.missing_fields.join(', ')}` : '',
      metadata.estimated_fields?.length ? `estimated_fields: ${metadata.estimated_fields.join(', ')}` : '',
      metadata.conflicts?.length ? `conflicts: ${metadata.conflicts.join(', ')}` : ''
    ].filter(Boolean)
    return gaps.length ? [`${id} — ${gaps.join('; ')}`] : []
  }).slice(0, 3).join('\n')
}


export function modelCapabilityRequestError(error: unknown): string {
  const message = (error as { message?: unknown })?.message
  return typeof message === 'string' && message.includes('model_capability_defaults') ? message : ''
}
