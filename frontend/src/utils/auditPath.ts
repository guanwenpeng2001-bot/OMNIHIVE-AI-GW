/** Keep the recorded path byte-for-byte except authentication query fields and fragments. */
export function sanitizeAuditPath(path: string): string {
  const withoutFragment = path.split('#', 1)[0]
  const queryIndex = withoutFragment.indexOf('?')
  if (queryIndex < 0) return withoutFragment
  const pathname = withoutFragment.slice(0, queryIndex)
  const safeFields = withoutFragment.slice(queryIndex + 1).split('&').filter(field => {
    let name: string
    try { name = decodeURIComponent(field.split('=', 1)[0].replace(/\+/g, ' ')) } catch { return false }
    const normalized = name.toLowerCase().replace(/[^a-z0-9]/g, '')
    return !/(token|secret|password|passwd|authorization|credential|signature)/.test(normalized)
      && !/^(xamz|xgoog)/.test(normalized)
      && !/(api|access|subscription|private)key$/.test(normalized)
      && !['sig', 'key', 'apikey', 'xapikey', 'auth', 'code', 'jwt', 'session', 'sessionid', 'cookie', 'xamzsecuritytoken', 'xamzcredential'].includes(normalized)
  })
  return pathname + (safeFields.length ? `?${safeFields.join('&')}` : '')
}

export function auditRequestText(row: { method: string; path: string }): string {
  return `${row.method} ${sanitizeAuditPath(row.path)}`
}
