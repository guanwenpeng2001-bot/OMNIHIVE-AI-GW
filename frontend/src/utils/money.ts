/** Display contract only: never use formatted values as billing inputs.
 * Ledger/export precision is eight decimal places, matching stored usage costs.
 * Unknown amounts are not zero. Currency conversion belongs to the backend.
 */
export type Amount = number | null | undefined
export const LEDGER_DIGITS = 8
export const UNKNOWN_AMOUNT = '-'

export function isKnownAmount(value: Amount): value is number {
  return typeof value === 'number' && Number.isFinite(value)
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
  const digits = value !== 0 && Math.abs(value) < 0.01 ? LEDGER_DIGITS : 2
  return new Intl.NumberFormat(locale, { minimumFractionDigits: digits, maximumFractionDigits: digits }).format(value)
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
