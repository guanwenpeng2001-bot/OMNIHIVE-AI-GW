import type { PublicRate } from './api'

export function publicValue(value?: string | null): string {
  return value && value !== 'unknown' ? value : '—'
}

export function publicTokenPrice(rate?: PublicRate | null, digits: 2 | 4 = 2): string {
  if (!rate || !['token', 'input_token', 'output_token'].includes(rate.unit)) return '—'
  const amount = /^(\d+)(?:\.(\d+))?$/.exec(rate.amount)
  if (!amount || !/^\d+$/.test(rate.per)) return '—'
  const per = BigInt(rate.per)
  if (per === 0n) return '—'

  const fraction = amount[2] ?? ''
  const units = BigInt(amount[1] + fraction)
  if (units === 0n) return 'Free'

  const scale = 10n ** BigInt(digits)
  const denominator = per * 10n ** BigInt(fraction.length)
  const numerator = units * 1_000_000n * scale
  const rounded = (2n * numerator + denominator) / (2n * denominator)
  const currency = rate.currency === 'USD' ? '$' : `${rate.currency} `
  const display = (value: bigint) =>
    `${currency}${value / scale}.${String(value % scale).padStart(digits, '0')}`
  return rounded === 0n ? `< ${display(1n)}` : display(rounded)
}
