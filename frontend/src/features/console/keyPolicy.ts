import type { IdentityKey } from './scope'
import type { CatalogModelItem } from './catalogApi'

/** Server lifecycle and policy fields are required; absent evidence is unknown. */
export function isUsableIdentityKey(key: IdentityKey, action?: string, now = Date.now()): boolean {
  return key.status === 'active' && Boolean(key.owner_id) && Boolean(key.platform) &&
    Array.isArray(key.actions) && (!action || key.actions.includes(action)) &&
    Array.isArray(key.excluded_models) && Boolean(key.model_access) &&
    ['inherit', 'allow_list'].includes(key.model_access.mode) &&
    Array.isArray(key.model_access.ids) &&
    (key.expires_at === null || (typeof key.expires_at === 'string' && Date.parse(key.expires_at) > now))
}

export function identityKeyAllowsModel(key: IdentityKey, model: CatalogModelItem, action: string): boolean {
  if (!isUsableIdentityKey(key, action) || (key.platform !== 'composite' && key.platform !== model.provider) ||
      model.routeState !== 'eligible' || model.accessState !== 'allowed') return false
  const native = model.slug.startsWith(`${model.provider}/`) ? model.slug.slice(model.provider.length + 1) : model.slug
  const matches = (id: string) => id === model.id || id === model.slug || id === native
  if (key.excluded_models.some(matches)) return false
  return key.model_access.mode === 'inherit' || key.model_access.ids.some(matches)
}
