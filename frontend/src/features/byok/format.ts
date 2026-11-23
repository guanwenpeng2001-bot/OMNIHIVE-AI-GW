import type { UiTone } from '@/components/ui/types'
import type { ByokCredential } from './api'

export function byokStateLabel(state: ByokCredential['state'] | string, isZh: boolean): string {
  switch (state) {
    case 'verified':
      return isZh ? '已验证可用' : 'Verified'
    case 'unverified':
      return isZh ? '待探测验证' : 'Unverified'
    case 'expired':
      return isZh ? '验证已过期' : 'Expired'
    case 'revoked':
      return isZh ? '已吊销' : 'Revoked'
    default:
      return state || (isZh ? '未知' : 'Unknown')
  }
}

export function byokStateTone(state: ByokCredential['state'] | string): UiTone {
  if (state === 'verified') return 'success'
  if (state === 'unverified' || state === 'expired') return 'warning'
  if (state === 'revoked') return 'danger'
  return 'unknown'
}

export function byokAuthTypeLabel(authType: string, isZh: boolean): string {
  switch (authType) {
    case 'api_key':
      return 'API Key'
    case 'oauth':
      return 'OAuth'
    case 'setup-token':
      return 'Setup Token'
    case 'bedrock':
      return 'AWS Bedrock'
    case 'service_account':
      return isZh ? '服务账号 (Service Account)' : 'Service Account'
    default:
      return authType
  }
}

export function byokEventLabel(type: string, isZh: boolean): string {
  const zhMap: Record<string, string> = {
    create: '创建并登记凭据',
    verify: '上游连通性与模型探测通过',
    'verify.failed': '上游凭据探测未通过',
    rotate: '完成密钥轮换',
    'rotate.failed': '密钥轮换验证未通过',
    policy: '更新作用域与调度策略',
    patch: '局部更新凭据授权范围',
    revoke: '吊销自带上游凭据',
    'revoke.failed': '吊销操作异常'
  }
  const enMap: Record<string, string> = {
    create: 'Credential enrolled',
    verify: 'Upstream verification succeeded',
    'verify.failed': 'Upstream verification failed',
    rotate: 'Secret rotated',
    'rotate.failed': 'Secret rotation failed',
    policy: 'Policy updated',
    patch: 'Scope patched',
    revoke: 'Credential revoked',
    'revoke.failed': 'Revocation failed'
  }
  return (isZh ? zhMap[type] : enMap[type]) || type
}

export function formatByokTime(iso?: string | null): string {
  if (!iso) return '—'
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return iso
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`
}

export function parseTokenList(raw: string): string[] {
  const seen = new Set<string>()
  for (const part of raw.split(/[\s,;\n]+/)) {
    const trimmed = part.trim()
    if (trimmed) seen.add(trimmed)
  }
  return [...seen]
}

export function isPositiveDecimalIdList(values: string[]): boolean {
  return values.length > 0 && values.every((item) => /^[1-9]\d*$/.test(item))
}

export function formatMoneyAmount(money?: { currency: string; amount: string } | null): string {
  if (!money) return '—'
  return money.currency === 'USD' ? `$${money.amount}` : `${money.currency} ${money.amount}`
}
