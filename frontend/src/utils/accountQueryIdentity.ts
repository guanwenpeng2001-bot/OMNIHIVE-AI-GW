import type { AccountListItem } from '@/types'

// JSON objects can arrive in a different property order on an ordinary row refresh.
function canonical(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(canonical)
  if (value && typeof value === 'object') {
    return Object.fromEntries(Object.entries(value).sort(([left], [right]) => left.localeCompare(right))
      .map(([key, item]) => [key, canonical(item)]))
  }
  return value
}

/** Stable admin query identity. Old DTOs retain visible configuration/revision
 * checks, but cannot identify a rotation hidden behind unchanged redaction flags.
 * Never use row timestamps or quota observations as credential versions.
 */
export function accountQueryIdentity(account: AccountListItem): string {
  return JSON.stringify(canonical([
    account.id, account.platform, account.type, account.runtime_identity || null,
    account.credentials, account.credentials_status,
    account.parent_account_id, account.proxy_id, account.proxy_group_id,
    account.extra?.custom_base_url_enabled,
    (account.extra?.upstream_model_catalog_state as Record<string, unknown> | undefined)?.identity_revision,
  ]))
}
