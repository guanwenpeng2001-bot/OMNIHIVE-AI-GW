/** Logical selection only: a group ID must never be serialized as a node ID. */
export type OAuthProxySelection = number | null | { proxy_group_id: number }

export function oauthProxyPayload(selection?: OAuthProxySelection): { proxy_id?: number; proxy_group_id?: number } {
  if (typeof selection === 'object' && selection) return { proxy_group_id: selection.proxy_group_id }
  return selection ? { proxy_id: selection } : {}
}

export function accountProxyPayload(proxyId?: number | null, groupId?: number | null) {
  return { proxy_id: groupId ? 0 : (proxyId || 0), proxy_group_id: groupId || 0 }
}
