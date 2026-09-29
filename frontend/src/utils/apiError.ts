/** The sole compatibility boundary for management API errors. */
export interface NormalizedApiError {
  status: number
  code: number
  reason?: string
  message: string
  metadata: Record<string, unknown>
}

const normalizedErrors = new WeakSet<object>()

function record(value: unknown): Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value)
    ? value as Record<string, unknown> : {}
}

function text(value: unknown): string | undefined {
  return typeof value === 'string' && value.trim() ? value : undefined
}

function httpStatus(value: unknown): number | undefined {
  return typeof value === 'number' && Number.isInteger(value) && value >= 100 && value <= 599
    ? value : undefined
}

/** Normalize current envelopes, legacy string code/error, Axios, and local failures once. */
export function normalizeApiError(error: unknown): NormalizedApiError {
  if (error && typeof error === 'object' && normalizedErrors.has(error)) {
    return error as NormalizedApiError
  }
  const source = record(error)
  const response = record(source.response)
  const data = record(response.data)
  const envelope = Object.keys(data).length ? data : source
  const nestedError = record(envelope.error)
  const status = httpStatus(response.status) ?? httpStatus(source.status) ?? httpStatus(envelope.code) ?? 0
  const legacyError = text(envelope.error)
  const reason = text(envelope.reason) ?? text(envelope.code) ?? text(nestedError.code)
    ?? (legacyError && /^[a-z][a-z0-9_]*$/i.test(legacyError) ? legacyError : undefined)
  const result: NormalizedApiError = {
    status,
    code: status,
    reason,
    message: text(envelope.detail) ?? text(envelope.message) ?? text(nestedError.message)
      ?? (legacyError !== reason ? legacyError : undefined) ?? text(source.message) ?? text(error) ?? '',
    metadata: record(envelope.metadata ?? envelope.details),
  }
  normalizedErrors.add(result)
  return result
}

/** Stable machine reason; numeric HTTP codes are never treated as domain reasons. */
export function extractApiErrorCode(error: unknown): string | undefined {
  return normalizeApiError(error).reason
}

export function extractApiErrorStatus(error: unknown): number | undefined {
  return normalizeApiError(error).status || undefined
}

export function extractApiErrorMetadata(error: unknown): Record<string, unknown> {
  return normalizeApiError(error).metadata
}

type TranslateFn = (key: string, params?: Record<string, unknown>) => string
type TranslateWithExistsFn = TranslateFn & { te?: (key: string) => boolean }

/**
 * Translate a value via i18n if a matching key exists, otherwise return the original.
 * Example: "certSerial" → t('admin.settings.payment.field_certSerial') → "证书序列号".
 */
function tryTranslate(t: TranslateFn, key: string, fallback: string): string {
  const translated = t(key)
  if (translated === key) return fallback
  const te = (t as TranslateWithExistsFn).te
  if (te && !te(key)) return fallback
  return translated
}

/**
 * Replace raw config field names in metadata (e.g. "certSerial") with their
 * localized UI labels (e.g. "证书序列号"), using the provider-config field i18n namespace.
 * Handles both single `key` and `/`-joined `keys` patterns used by wxpay errors.
 */
function localizeMetadata(metadata: Record<string, unknown>, t: TranslateFn): Record<string, unknown> {
  const out: Record<string, unknown> = { ...metadata }
  if (typeof out.key === 'string') {
    out.key = tryTranslate(t, `admin.settings.payment.field_${out.key}`, out.key)
  }
  if (typeof out.keys === 'string') {
    out.keys = out.keys
      .split('/')
      .map(k => tryTranslate(t, `admin.settings.payment.field_${k}`, k))
      .join(' / ')
  }
  return out
}

/**
 * Extract a localized error message from an API error by looking up
 * `<namespace>.<REASON>` in i18n and substituting metadata as placeholders.
 *
 * Config-field names in metadata (`key` / `keys`) are automatically translated
 * to their UI labels before substitution, so error messages read like
 * "缺少必填项：证书序列号" instead of "缺少必填项：certSerial".
 *
 * @param err      - The caught error
 * @param t        - Vue i18n translate function
 * @param namespace- i18n key prefix, e.g. "payment.errors"
 * @param fallback - Fallback key or plain string if no localized mapping exists
 */
export function extractI18nErrorMessage(
  err: unknown,
  t: TranslateFn,
  namespace: string,
  fallback: string,
): string {
  const code = extractApiErrorCode(err)
  if (code) {
    const key = `${namespace}.${code}`
    const rawMetadata = extractApiErrorMetadata(err) ?? {}
    const metadata = localizeMetadata(rawMetadata, t)
    const translated = t(key, metadata)
    // Vue i18n returns the key itself when missing; detect that and fall back.
    if (translated !== key) return translated
    // If the framework exposes `te`, use it to double-check.
    const te = (t as TranslateWithExistsFn).te
    if (te && te(key)) return translated
  }
  return extractApiErrorMessage(err, fallback)
}

/**
 * Extract a displayable error message from an API error.
 *
 * @param err - The caught error (unknown type)
 * @param fallback - Fallback message if none can be extracted (use t('common.error') or similar)
 * @param i18nMap - Optional map of error codes to i18n translated strings
 */
export function extractApiErrorMessage(
  err: unknown,
  fallback = 'Unknown error',
  i18nMap?: Record<string, string>,
): string {
  const error = normalizeApiError(err)
  if (error.reason && i18nMap?.[error.reason]) return i18nMap[error.reason]
  return error.message || fallback
}
