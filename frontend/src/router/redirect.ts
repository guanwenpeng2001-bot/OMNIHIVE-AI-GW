const CONTROL_CHAR_RE = /[\u0000-\u001f\u007f]/

/**
 * Sanitizes an untrusted redirect target so it can only navigate to an internal
 * same-origin relative path starting with a single `/`.
 */
export function sanitizeInternalRedirect(raw: unknown, fallback = '/dashboard'): string {
  if (typeof raw !== 'string') {
    return fallback
  }

  const trimmed = raw.trim()
  if (!trimmed) {
    return fallback
  }

  if (CONTROL_CHAR_RE.test(trimmed)) {
    return fallback
  }

  if (!trimmed.startsWith('/') || trimmed.startsWith('//')) {
    return fallback
  }

  if (trimmed.includes('\\')) {
    return fallback
  }

  if (trimmed.includes('://')) {
    return fallback
  }

  try {
    const parsed = new URL(trimmed, 'http://local.invalid')
    if (parsed.origin !== 'http://local.invalid' || !parsed.pathname.startsWith('/')) {
      return fallback
    }
  } catch {
    return fallback
  }

  return trimmed
}
