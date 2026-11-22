/** Display contract only: never use formatted values as billing inputs.
 * Ledger/export precision is eight decimal places, matching stored usage costs.
 * Unknown amounts are not zero. Currency conversion belongs to the backend.
 */
export type Amount = number | null | undefined
export const LEDGER_DIGITS = 8
export const UNKNOWN_AMOUNT = '-'

const EXACT_CURRENCY_PREFIXES: Record<string, string> = {
  USD: '$',
  EUR: '€',
  GBP: '£',
}

export function isKnownAmount(value: Amount): value is number {
  return typeof value === 'number' && Number.isFinite(value)
}

function expandDecimalString(raw: string): { negative: boolean; intPart: string; fracPart: string } | null {
  const match = /^([+-]?)(?:(\d+)(?:\.(\d*))?|\.(\d+))(?:[eE]([+-]?\d+))?$/.exec(raw.trim())
  if (!match) return null
  const [, signStr, whole = '', fracAfterWhole = '', fracOnly = '', expStr] = match
  const negative = signStr === '-'
  const intDigits = whole || '0'
  const fracDigits = fracAfterWhole || fracOnly || ''
  const exp = expStr ? Number.parseInt(expStr, 10) : 0
  if (!Number.isFinite(exp)) return null

  let combined = intDigits + fracDigits
  let pointIndex = intDigits.length + exp
  if (pointIndex <= 0) {
    combined = '0'.repeat(-pointIndex + 1) + combined
    pointIndex = 1
  } else if (pointIndex > combined.length) {
    combined = combined + '0'.repeat(pointIndex - combined.length)
  }

  const rawInt = combined.slice(0, pointIndex).replace(/^0+/, '') || '0'
  const rawFrac = combined.slice(pointIndex).replace(/0+$/, '')
  return { negative, intPart: rawInt, fracPart: rawFrac }
}

function parseAmountParts(amount: string | number | null | undefined) {
  if (amount === null || amount === undefined || amount === '') return null
  if (typeof amount === 'number') {
    if (!Number.isFinite(amount)) return null
    return expandDecimalString(amount.toString())
  }
  return expandDecimalString(String(amount))
}

export function resolveExactCurrencyPrefix(currency = 'USD'): string {
  const normalized = String(currency || 'USD').trim().toUpperCase() || 'USD'
  return EXACT_CURRENCY_PREFIXES[normalized] ?? `${normalized} `
}

/** Locale-independent value shared by ledger cells, details and CSV/XLSX. */
export function formatLedgerAmount(value: Amount): string {
  if (!isKnownAmount(value)) return UNKNOWN_AMOUNT
  const formatted = value.toFixed(LEDGER_DIGITS)
  return formatted === '-0.00000000' ? '0.00000000' : formatted
}

/** Overview only; callers should expose formatLedgerAmount as the precise value. */
export function formatBalanceAmount(value: Amount, locale = 'en-US'): string {
  if (!isKnownAmount(value)) return UNKNOWN_AMOUNT
  let digits = 2
  if (value !== 0 && Math.abs(value) < 0.01) {
    if (Math.abs(value) >= 1e-8) {
      digits = LEDGER_DIGITS
    } else {
      const parts = expandDecimalString(Math.abs(value).toString())
      digits = parts ? Math.min(20, Math.max(LEDGER_DIGITS, parts.fracPart.length)) : LEDGER_DIGITS
    }
  }
  return new Intl.NumberFormat(locale, { minimumFractionDigits: digits, maximumFractionDigits: digits }).format(value)
}

/** Exact string-preserving money formatter that retains micro-dollar precision (< 1e-8)
 * without floating-point rounding or trailing-dot truncation.
 */
export function formatExactMoney(
  amount: string | number | null | undefined,
  currency = 'USD',
  fallback = UNKNOWN_AMOUNT,
  minDecimals = 2
): string {
  const parts = parseAmountParts(amount)
  if (!parts) return fallback

  const prefix = resolveExactCurrencyPrefix(currency)
  const { negative, intPart, fracPart } = parts
  const isZero = intPart === '0' && fracPart === ''
  const normalizedMinDecimals = Math.max(0, minDecimals)
  if (isZero) {
    return normalizedMinDecimals === 0 ? `${prefix}0` : `${prefix}0.${'0'.repeat(normalizedMinDecimals)}`
  }

  const sign = negative ? '-' : ''
  const isAtLeastOneCent =
    intPart !== '0' ||
    (fracPart.length >= 1 && fracPart[0] !== '0') ||
    (fracPart.length >= 2 && fracPart[1] !== '0')
  if (isAtLeastOneCent) {
    if (normalizedMinDecimals === 0 && fracPart === '') {
      return `${sign}${prefix}${intPart}`
    }
    const paddedFrac = fracPart.length >= normalizedMinDecimals ? fracPart : fracPart.padEnd(normalizedMinDecimals, '0')
    return `${sign}${prefix}${intPart}.${paddedFrac}`
  }
  const paddedMicroFrac = fracPart.length >= normalizedMinDecimals ? fracPart : fracPart.padEnd(normalizedMinDecimals, '0')
  return `${sign}${prefix}0.${paddedMicroFrac}`
}

/** Currency codes are explicit: CNY and USD must never share an ambiguous symbol. */
export function formatMoney(value: Amount, currency = 'USD', kind: 'balance' | 'ledger' = 'balance', locale = 'en-US'): string {
  if (!isKnownAmount(value)) return UNKNOWN_AMOUNT
  return `${currency} ${kind === 'ledger' ? formatLedgerAmount(value) : formatBalanceAmount(value, locale)}`
}

export function accountBilledAmount(row: { total_cost?: Amount; account_stats_cost?: Amount; account_rate_multiplier?: Amount }): number | null {
  const base = row.account_stats_cost ?? row.total_cost
  const rate = row.account_rate_multiplier ?? 1
  return isKnownAmount(base) && isKnownAmount(rate) && Number.isFinite(base * rate) ? base * rate : null
}
