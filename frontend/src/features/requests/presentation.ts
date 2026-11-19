import type { UiTone } from '@/components/ui'

export const UNKNOWN = '—'

export function timestamp(value: string | null | undefined, locale: string, timezone = 'UTC'): string {
  const at = Date.parse(value ?? '')
  if (!Number.isFinite(at)) return UNKNOWN
  return new Intl.DateTimeFormat(locale, {
    year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', second: '2-digit',
    timeZoneName: 'short', timeZone: timezone,
  }).format(at)
}

export function displayNumber(value: string | number | null | undefined, locale: string): string {
  if (value == null) return UNKNOWN
  if (typeof value === 'number') return new Intl.NumberFormat(locale).format(value)
  const match = /^(-?)(\d+)(\.\d+)?$/.exec(value)
  if (!match) return value
  const [, sign, integer, fraction = ''] = match
  const digits = new Intl.NumberFormat(locale).format(BigInt(integer))
  const separator = new Intl.NumberFormat(locale).formatToParts(1.1).find(part => part.type === 'decimal')?.value ?? '.'
  return `${sign}${digits}${fraction ? separator + fraction.slice(1) : ''}`
}

export function moneyText(value: { currency: string; amount: string } | null | undefined, locale: string): string {
  return value == null ? UNKNOWN : `${value.currency} ${displayNumber(value.amount, locale)}`
}

export function statusTone(value: string): UiTone {
  switch (value) {
    case 'settled': case 'succeeded': case 'posted': case 'resolved': case 'accepted': return 'success'
    case 'failed': case 'rejected': return 'danger'
    case 'interrupted': case 'needs_review': case 'reserved': case 'estimated': case 'excluded': return 'warning'
    case 'admitted': case 'pending': case 'running': return 'info'
    case 'unknown': case 'unconfirmed': return 'unknown'
    default: return 'neutral'
  }
}
