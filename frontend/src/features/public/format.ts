import type { UiTone } from '@/components/ui/types'
import type { PublicRate } from './api'

const TOKEN_UNITS = new Set(['token', 'tokens', 'input_token', 'output_token', 'cached_input_token'])

export function publicValue(value?: string | null, fallback = '—'): string {
  return value && value !== 'unknown' ? value : fallback
}

export function publicTokenPrice(rate?: PublicRate | null, digits: 2 | 4 = 2, isZh = false): string {
  if (!rate || !TOKEN_UNITS.has(rate.unit)) return '—'
  const amount = /^(\d+)(?:\.(\d+))?$/.exec(rate.amount)
  if (!amount || !/^\d+$/.test(rate.per)) return '—'
  const per = BigInt(rate.per)
  if (per === 0n) return '—'

  const fraction = amount[2] ?? ''
  const units = BigInt(amount[1] + fraction)
  if (units === 0n) return isZh ? '免费' : 'Free'

  const scale = 10n ** BigInt(digits)
  const denominator = per * 10n ** BigInt(fraction.length)
  const numerator = units * 1_000_000n * scale
  const rounded = (2n * numerator + denominator) / (2n * denominator)
  const currency = rate.currency === 'USD' ? '$' : `${rate.currency} `
  const display = (value: bigint) =>
    `${currency}${value / scale}.${String(value % scale).padStart(digits, '0')}`
  return rounded === 0n ? `< ${display(1n)}` : display(rounded)
}

export function formatRateBandPrice(rate?: PublicRate | null, isZh = false): string {
  if (!rate) return '—'
  if (TOKEN_UNITS.has(rate.unit)) {
    const tokenPrice = publicTokenPrice(rate, 4, isZh)
    return tokenPrice === '—' ? '—' : `${tokenPrice} / 1M`
  }
  const currency = rate.currency === 'USD' ? '$' : `${rate.currency} `
  const perText = rate.per === '1' ? rate.unit : `${rate.per} ${rate.unit}`
  return `${currency}${rate.amount} / ${perText}`
}

export function formatContextTokens(value?: string | number | null): string {
  if (value === null || value === undefined || value === '') return '—'
  const tokens = Number(value)
  if (!Number.isFinite(tokens) || tokens <= 0) return '—'
  if (tokens >= 1_000_000) {
    const millions = tokens / 1_000_000
    return Number.isInteger(millions) ? `${millions}M` : `${millions.toFixed(1)}M`
  }
  if (tokens >= 1_000) return `${Math.round(tokens / 1_000)}K`
  return String(tokens)
}

export function formatPublicTimestamp(iso?: string | null): string {
  if (!iso) return '—'
  const parsed = new Date(iso)
  if (Number.isNaN(parsed.getTime())) return iso
  const pad = (num: number) => String(num).padStart(2, '0')
  return `${parsed.getFullYear()}-${pad(parsed.getMonth() + 1)}-${pad(parsed.getDate())} ${pad(parsed.getHours())}:${pad(parsed.getMinutes())}`
}

export function routeStateLabel(state?: string | null, isZh = false): string {
  switch (state) {
    case 'eligible':
      return isZh ? '可路由' : 'Routable'
    case 'ineligible':
      return isZh ? '暂不可用' : 'Unavailable'
    case 'requires_scope':
      return isZh ? '需项目授权' : 'Scope Required'
    default:
      return isZh ? '状态未知' : 'Unknown'
  }
}

export function routeStateTone(state?: string | null): UiTone {
  if (state === 'eligible') return 'success'
  if (state === 'ineligible') return 'warning'
  if (state === 'requires_scope') return 'info'
  return 'unknown'
}

export function maturityLabel(maturity?: string | null, isZh = false): string {
  switch (maturity) {
    case 'stable':
      return isZh ? '稳定版' : 'Stable'
    case 'experimental':
    case 'candidate':
      return isZh ? '实验性' : 'Experimental'
    case 'deprecated':
    case 'closed':
      return isZh ? '已弃用' : 'Deprecated'
    default:
      return isZh ? '未声明' : 'Undeclared'
  }
}

export function maturityTone(maturity?: string | null): UiTone {
  if (maturity === 'stable') return 'success'
  if (maturity === 'experimental' || maturity === 'candidate') return 'warning'
  if (maturity === 'deprecated' || maturity === 'closed') return 'danger'
  return 'unknown'
}

export function evidenceStateLabel(state?: string | null, isZh = false): string {
  switch (state) {
    case 'fresh':
      return isZh ? '证据有效' : 'Fresh'
    case 'stale':
      return isZh ? '证据已过期' : 'Stale'
    default:
      return isZh ? '未观测' : 'Unobserved'
  }
}

export function metricsStateReason(state?: string | null, isZh = false): string {
  switch (state) {
    case 'insufficient_samples':
      return isZh ? '近期真实调用样本未达统计披露阈值，暂不展示延迟以防失真' : 'Insufficient live traffic samples in current window; withheld to avoid misleading metrics'
    case 'partial':
      return isZh ? '部分指标已就绪，其余分位仍在采样' : 'Partial telemetry available for current observation window'
    case 'withheld':
      return isZh ? '因样本稀疏或隐私聚合策略暂不公开' : 'Withheld due to aggregation or sample size policy'
    case 'unavailable':
      return isZh ? '遥测数据源暂不可用' : 'Telemetry source currently unavailable'
    case 'available':
      return isZh ? '基于网关真实流量观测计算' : 'Computed from verified gateway traffic observations'
    default:
      return isZh ? '尚未采集到足够的真实调用样本' : 'No verified live telemetry samples recorded yet'
  }
}

export function statusStateLabel(state?: string | null, isZh = false): string {
  switch (state) {
    case 'operational':
      return isZh ? '运行正常' : 'Operational'
    case 'degraded':
      return isZh ? '性能降级' : 'Degraded'
    case 'outage':
      return isZh ? '服务中断' : 'Outage'
    case 'maintenance':
      return isZh ? '维护中' : 'Maintenance'
    default:
      return isZh ? '未观测 / 样本不足' : 'Unobserved'
  }
}

export function statusStateTone(state?: string | null): UiTone {
  if (state === 'operational') return 'success'
  if (state === 'maintenance' || state === 'degraded') return 'warning'
  if (state === 'outage') return 'danger'
  return 'unknown'
}

export function statusReasonLabel(reason?: string | null, isZh = false): string {
  switch (reason) {
    case 'INSUFFICIENT_SAMPLES':
      return isZh ? '样本数未达阈值' : 'Insufficient samples'
    case 'STALE_DATA':
      return isZh ? '观测数据已过期' : 'Stale telemetry'
    case 'SOURCE_UNAVAILABLE':
      return isZh ? '遥测源不可用' : 'Source unavailable'
    case 'ROLLUP_PENDING':
      return isZh ? '聚合计算中' : 'Rollup pending'
    case 'OUTCOME_UNKNOWN':
      return isZh ? '结果未确认' : 'Outcome unknown'
    default:
      return ''
  }
}

export function incidentStateLabel(state?: string | null, isZh = false): string {
  switch (state) {
    case 'investigating':
      return isZh ? '排查中' : 'Investigating'
    case 'identified':
      return isZh ? '已定位' : 'Identified'
    case 'monitoring':
      return isZh ? '观察恢复中' : 'Monitoring'
    case 'resolved':
      return isZh ? '已恢复' : 'Resolved'
    default:
      return state || (isZh ? '未知' : 'Unknown')
  }
}

export function incidentSeverityLabel(severity?: string | null, isZh = false): string {
  switch (severity) {
    case 'outage':
      return isZh ? '严重中断' : 'Outage'
    case 'degraded':
      return isZh ? '性能降级' : 'Degraded'
    case 'maintenance':
      return isZh ? '计划维护' : 'Maintenance'
    default:
      return severity || (isZh ? '一般' : 'Notice')
  }
}

export function componentDisplayName(id: string, fallbackName: string | undefined, isZh: boolean): string {
  const zhMap: Record<string, string> = {
    inference: '核心推理转发网关',
    openai_compatible: 'OpenAI 兼容协议层 (/v1/chat & /v1/responses)',
    anthropic_compatible: 'Anthropic Messages 兼容协议层 (/v1/messages)',
    gemini_compatible: 'Google Gemini 原生协议层',
    console_auth: '控制台鉴权与作用域服务',
    async_workers: '异步媒体生成与批处理队列'
  }
  const enMap: Record<string, string> = {
    inference: 'Core Inference Gateway',
    openai_compatible: 'OpenAI Compatible API (/v1/chat & /v1/responses)',
    anthropic_compatible: 'Anthropic Messages API (/v1/messages)',
    gemini_compatible: 'Google Gemini Native API',
    console_auth: 'Console Auth & Scope Control',
    async_workers: 'Async Media & Task Workers'
  }
  if (isZh && zhMap[id]) return zhMap[id]
  if (!isZh && enMap[id]) return enMap[id]
  return fallbackName || id
}
