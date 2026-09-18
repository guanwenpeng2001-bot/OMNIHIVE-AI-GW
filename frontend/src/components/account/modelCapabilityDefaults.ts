/** Empty input removes the account declaration; semantic validation is server-owned. */
export function parseModelCapabilityDefaults(text: string): Record<string, unknown> | null {
  if (!text.trim()) return null
  const value: unknown = JSON.parse(text)
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    throw new Error('model_capability_defaults: object_required')
  }
  const envelope = value as Record<string, unknown>
  if (envelope.schema_version !== 1 || typeof envelope.reason !== 'string' || !envelope.reason.trim() ||
    !envelope.models || typeof envelope.models !== 'object' || Array.isArray(envelope.models) || !Object.keys(envelope.models).length) {
    throw new Error('model_capability_defaults: invalid_envelope')
  }
  return envelope
}

/** Keep toast diagnostics bounded; the preview retains every model and field. */
export function modelCapabilityGapSummary(result: import('@/api/admin/accounts').SyncUpstreamModelsResult, t: (key: string) => string): string {
  return Object.entries(result.metadata ?? {}).flatMap(([id, metadata]) => {
    const gaps = [
      metadata.missing_fields?.length ? `${t('capabilityMessages.missing')}: ${metadata.missing_fields.join(', ')}` : '',
      metadata.estimated_fields?.length ? `${t('capabilityMessages.estimated')}: ${metadata.estimated_fields.join(', ')}` : '',
      metadata.conflicts?.length ? `${t('capabilityMessages.conflicts')}: ${metadata.conflicts.join(', ')}` : ''
    ].filter(Boolean)
    return gaps.length ? [`${id} — ${gaps.join('; ')}`] : []
  }).slice(0, 3).join('\n')
}


export function modelCapabilityRequestError(error: unknown, t: (key: string) => string): string {
  const message = (error as { message?: unknown })?.message
  return typeof message === 'string' && message.includes('model_capability_defaults') ? t('capabilityMessages.invalid') : ''
}
