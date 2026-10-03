export function isSpecialCompatibleProvider(platform: string): boolean {
  return platform === 'github_copilot' || platform === 'nous_research'
}

export function specialCompatibleKeyOptional(platform: string, draft: Record<string, unknown>): boolean {
  return platform === 'nous_research' && draft.auth_mode === 'official_proxy'
}

export function specialCompatibleModeChanged(platform: string, saved: Record<string, unknown>, draft: Record<string, unknown>): boolean {
  return isSpecialCompatibleProvider(platform) && saved.auth_mode !== draft.auth_mode
}

export function changeSpecialCompatibleMode(draft: Record<string, unknown>, mode: string): Record<string, unknown> {
  // Explicit empty patches remove the old mode's secret and endpoint on edit.
  // Keep the account-bound reference/version for the repository's CAS update.
  return { ...draft, account_mode: 'payg', auth_mode: mode, base_url: '', api_key: '', api_base_urls: {} }
}

export function validateSpecialCompatibleDraft(platform: string, draft: Record<string, unknown>): void {
  if (!isSpecialCompatibleProvider(platform)) return
  if ('refresh_token' in draft) throw new Error('Refresh credentials are unsupported')
  if (platform === 'github_copilot') {
    if (draft.auth_mode && draft.auth_mode !== 'fine_grained_pat') throw new Error('Select a fine-grained PAT')
  } else if (draft.auth_mode !== 'invocation_token' && draft.auth_mode !== 'official_proxy') {
    throw new Error('Select an authentication mode')
  }
  if (specialCompatibleKeyOptional(platform, draft) && !String(draft.base_url || '').trim()) throw new Error('Enter the official proxy base URL')
}
