import { formatExactMoney as formatExactMoneyCore } from '@/utils/money'

export const EM_DASH = '—'

export function formatNullableInteger(value: number | string | null | undefined): string {
  if (value === null || value === undefined || value === '') return EM_DASH
  const parsed = typeof value === 'string' ? Number(value) : value
  if (!Number.isFinite(parsed)) return EM_DASH
  return new Intl.NumberFormat('en-US', { maximumFractionDigits: 0 }).format(parsed)
}

export function formatCompactCount(value: number | string | null | undefined): string {
  if (value === null || value === undefined || value === '') return EM_DASH
  const parsed = typeof value === 'string' ? Number(value) : value
  if (!Number.isFinite(parsed)) return EM_DASH
  const abs = Math.abs(parsed)
  if (abs >= 1_000_000_000) return `${(parsed / 1_000_000_000).toFixed(2)}B`
  if (abs >= 1_000_000) return `${(parsed / 1_000_000).toFixed(2)}M`
  if (abs >= 10_000) return `${(parsed / 1_000).toFixed(1)}K`
  return new Intl.NumberFormat('en-US', { maximumFractionDigits: 0 }).format(parsed)
}

export function formatNullablePercent(
  value: number | null | undefined,
  fractionDigits = 2
): string {
  if (value === null || value === undefined || !Number.isFinite(value)) return EM_DASH
  return `${value.toFixed(fractionDigits)}%`
}

export function formatNullableLatencyMs(value: number | null | undefined): string {
  if (value === null || value === undefined || !Number.isFinite(value)) return EM_DASH
  if (value >= 10_000) return `${(value / 1000).toFixed(2)}s`
  return `${Math.round(value)}ms`
}

export function formatExactMoney(
  amount: string | number | null | undefined,
  currency = 'USD'
): string {
  return formatExactMoneyCore(amount, currency, EM_DASH)
}

export function formatChargeObject(
  charge: { currency: string; amount: string } | null | undefined
): string {
  if (!charge || !charge.amount) return EM_DASH
  return formatExactMoney(charge.amount, charge.currency || 'USD')
}

export function formatNullableBytes(bytes: string | number | null | undefined): string {
  if (bytes === null || bytes === undefined || bytes === '') return EM_DASH
  const num = typeof bytes === 'string' ? Number(bytes) : bytes
  if (!Number.isFinite(num) || num < 0) return EM_DASH
  if (num < 1024) return `${Math.round(num)} B`
  if (num < 1024 * 1024) return `${(num / 1024).toFixed(1)} KB`
  if (num < 1024 * 1024 * 1024) return `${(num / (1024 * 1024)).toFixed(2)} MB`
  return `${(num / (1024 * 1024 * 1024)).toFixed(2)} GB`
}

export interface ExpirationCountdown {
  state: 'active' | 'expiring_soon' | 'expired' | 'none'
  labelEn: string
  labelZh: string
}

export function formatExpirationCountdown(
  expiresAtIso: string | null | undefined,
  nowMs = Date.now()
): ExpirationCountdown {
  if (!expiresAtIso) {
    return { state: 'none', labelEn: EM_DASH, labelZh: EM_DASH }
  }
  const targetMs = Date.parse(expiresAtIso)
  if (!Number.isFinite(targetMs)) {
    return { state: 'none', labelEn: EM_DASH, labelZh: EM_DASH }
  }
  const diffMs = targetMs - nowMs
  if (diffMs <= 0) {
    return { state: 'expired', labelEn: 'Expired', labelZh: '已过期' }
  }
  const totalMinutes = Math.floor(diffMs / 60_000)
  const hours = Math.floor(totalMinutes / 60)
  const minutes = totalMinutes % 60
  const days = Math.floor(hours / 24)

  const isSoon = diffMs <= 2 * 3_600_000
  if (days >= 1) {
    const remHours = hours % 24
    return {
      state: isSoon ? 'expiring_soon' : 'active',
      labelEn: `Expires in ${days}d ${remHours}h`,
      labelZh: `${days}天 ${remHours}小时后过期`
    }
  }
  if (hours >= 1) {
    return {
      state: isSoon ? 'expiring_soon' : 'active',
      labelEn: `Expires in ${hours}h ${minutes}m`,
      labelZh: `${hours}小时 ${minutes}分后过期`
    }
  }
  return {
    state: 'expiring_soon',
    labelEn: `Expires in ${Math.max(1, minutes)}m`,
    labelZh: `${Math.max(1, minutes)}分钟后过期`
  }
}

export function formatIsoTimestamp(iso: string | null | undefined): string {
  if (!iso) return EM_DASH
  const ms = Date.parse(iso)
  if (!Number.isFinite(ms)) return iso
  return new Date(ms).toISOString().replace('T', ' ').slice(0, 19) + ' UTC'
}
