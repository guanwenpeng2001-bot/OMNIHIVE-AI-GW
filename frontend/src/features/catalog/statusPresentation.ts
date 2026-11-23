import type { UiTone } from '@/components/ui/types'
import {
  formatCatalogRateBand,
  formatTokenRatePerMillion
} from '@/features/shared/pricePresentation'
import type { CatalogMetrics, CatalogRate } from './contract'
import { getCatalogCopy } from './copy'

export type CanonicalOriginKind =
  | 'reviewed_registry'
  | 'builtin_supplement'
  | 'platform_builtin'
  | 'pi_supplement'
  | 'account_discovery'
  | 'platform_preset'
  | 'channel_published'
  | 'heuristic'
  | 'unknown'

export function normalizeCatalogOriginKind(raw?: string | null): CanonicalOriginKind {
  const key = String(raw ?? '').trim().toLowerCase()
  switch (key) {
    case 'builtin_reviewed':
    case 'approved_registry':
    case 'reviewed_registry':
      return 'reviewed_registry'
    case 'builtin_supplement':
      return 'builtin_supplement'
    case 'platform_builtin':
      return 'platform_builtin'
    case 'pi_ai_reference':
    case 'pi_supplement':
    case 'pi-ai':
    case 'pi':
    case 'external_reference':
    case 'public_plaza_reference':
    case 'scoped_plaza_reference':
    case 'reference':
      return 'pi_supplement'
    case 'account_discovery':
    case 'account_declaration':
    case 'upstream_discovered':
      return 'account_discovery'
    case 'platform_preset':
      return 'platform_preset'
    case 'channel_published':
    case 'operator_config':
    case 'published':
      return 'channel_published'
    case 'heuristic':
      return 'heuristic'
    default:
      return 'unknown'
  }
}

export function formatCatalogOriginLabel(raw?: string | null, isZh = false): string {
  const copy = getCatalogCopy(isZh)
  const kind = normalizeCatalogOriginKind(raw)
  switch (kind) {
    case 'reviewed_registry':
      return copy.origins.reviewedRegistry
    case 'builtin_supplement':
      return copy.origins.builtinSupplement
    case 'platform_builtin':
      return copy.origins.platformBuiltin
    case 'pi_supplement':
      return copy.origins.communityPi
    case 'account_discovery':
      return copy.origins.accountDiscovery
    case 'platform_preset':
      return copy.origins.platformPreset
    case 'channel_published':
      return copy.origins.channelPublished
    case 'heuristic':
      return copy.origins.heuristic
    default: {
      const trimmed = String(raw ?? '').trim()
      if (!trimmed || trimmed.toLowerCase() === 'unknown') {
        return copy.origins.unknown
      }
      if (trimmed.toLowerCase().includes('pi-ai')) {
        return copy.origins.communityPi
      }
      return trimmed
    }
  }
}

export function originTone(kind: CanonicalOriginKind): UiTone {
  switch (kind) {
    case 'channel_published':
      return 'accent'
    case 'account_discovery':
      return 'info'
    case 'reviewed_registry':
      return 'success'
    case 'builtin_supplement':
    case 'platform_builtin':
    case 'pi_supplement':
    case 'platform_preset':
    case 'heuristic':
      return 'neutral'
    default:
      return 'unknown'
  }
}

export function formatPriceSourceLabel(source?: string | null, isZh = false): string {
  if (!source) return ''
  const normalized = source.trim().toLowerCase()
  switch (normalized) {
    case 'public_plaza_reference':
      return isZh ? '公开目录参考价' : 'Catalog Reference'
    case 'scoped_plaza_reference':
      return isZh ? '空间目录参考价' : 'Workspace Reference'
    case 'published':
    case 'channel_published':
      return isZh ? '已发布生效价格' : 'Published Rate'
    case 'reference':
      return isZh ? '参考价（非结算价）' : 'Reference Price'
    case 'pi-ai':
    case 'pi_ai_reference':
    case 'pi_supplement':
    case 'external_reference':
      return getCatalogCopy(isZh).origins.communityPi
    case 'builtin_reviewed':
    case 'approved_registry':
    case 'reviewed_registry':
      return getCatalogCopy(isZh).origins.reviewedRegistry
    case 'builtin_supplement':
      return getCatalogCopy(isZh).origins.builtinSupplement
    default:
      if (normalized.includes('pi-ai')) {
        return getCatalogCopy(isZh).origins.communityPi
      }
      return source
  }
}

export function formatShortVersion(version?: string | null): string {
  if (!version) return ''
  if (version.length <= 20) return version
  return `${version.slice(0, 12)}…${version.slice(-6)}`
}

export function formatRateBandKind(kind?: string | null, isZh = false): string {
  if (!kind) return '—'
  const map: Record<string, { zh: string; en: string }> = {
    cache_read: { zh: '缓存读取 (Cache Read)', en: 'Cache Read' },
    cached_input: { zh: '缓存输入 (Cached Input)', en: 'Cached Input' },
    cache_write: { zh: '缓存写入 (Cache Write)', en: 'Cache Write' },
    cache_write_1h: { zh: '1小时缓存写入 (1h Cache Write)', en: '1h Cache Write' },
    reasoning: { zh: '推理思考 (Reasoning)', en: 'Reasoning Tokens' },
    input: { zh: '阶梯输入 (Tiered Input)', en: 'Tiered Input' },
    output: { zh: '阶梯输出 (Tiered Output)', en: 'Tiered Output' }
  }
  return map[kind] ? (isZh ? map[kind].zh : map[kind].en) : kind
}

export function publicValue(value?: string | null, fallback = '—'): string {
  return value && value !== 'unknown' ? value : fallback
}

export function publicTokenPrice(
  rate?: CatalogRate | null,
  _digits: 2 | 4 | 'adaptive' = 'adaptive',
  isZh = false
): string {
  return formatTokenRatePerMillion(rate, {
    minDecimals: 2,
    maxDecimals: 4,
    locale: isZh ? 'zh' : 'en',
    zeroAsFree: true
  })
}

export function formatRateBandPrice(rate?: CatalogRate | null, isZh = false): string {
  return formatCatalogRateBand(rate, {
    minDecimals: 2,
    maxDecimals: 4,
    locale: isZh ? 'zh' : 'en',
    zeroAsFree: true
  })
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
  if (!iso || iso.startsWith('0001-')) return '—'
  const parsed = new Date(iso)
  if (Number.isNaN(parsed.getTime())) return iso
  const pad = (num: number) => String(num).padStart(2, '0')
  return `${parsed.getFullYear()}-${pad(parsed.getMonth() + 1)}-${pad(parsed.getDate())} ${pad(parsed.getHours())}:${pad(parsed.getMinutes())}`
}

export function formatMetricsWindow(
  metrics?: (Partial<CatalogMetrics> & { window?: string | null }) | null,
  isZh = false
): string {
  const fallback = isZh ? '未观测' : 'Unobserved'
  if (!metrics) return fallback
  const explicit = metrics.window?.trim()
  if (explicit && explicit !== 'unknown') {
    return explicit
  }
  const startValid = Boolean(metrics.window_start && !metrics.window_start.startsWith('0001-'))
  const endValid = Boolean(metrics.window_end && !metrics.window_end.startsWith('0001-'))
  if (startValid && endValid) {
    const startText = formatPublicTimestamp(metrics.window_start)
    const endText = formatPublicTimestamp(metrics.window_end)
    const startMs = Date.parse(metrics.window_start!)
    const endMs = Date.parse(metrics.window_end!)
    if (Number.isFinite(startMs) && Number.isFinite(endMs) && endMs > startMs) {
      const hours = (endMs - startMs) / 3_600_000
      if (Math.abs(hours - 24) < 0.25) {
        return `24h (${startText} → ${endText})`
      }
    }
    return `${startText} → ${endText}`
  }
  if (startValid) return formatPublicTimestamp(metrics.window_start)
  if (endValid) return formatPublicTimestamp(metrics.window_end)
  return fallback
}

export function formatMetricsSource(source?: string | null, isZh = false): string {
  const normalized = source?.trim()
  if (!normalized || normalized === 'unknown' || normalized === 'unavailable') {
    return isZh ? '未观测' : 'Unobserved'
  }
  switch (normalized) {
    case 'gateway_observations':
    case 'gateway':
      return isZh ? '网关真实流量观测' : 'Gateway live traffic'
    case 'catalog_rollup':
    case 'telemetry_rollup':
      return isZh ? '网关遥测聚合' : 'Gateway telemetry rollup'
    default:
      return normalized
  }
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
      return isZh
        ? '近期真实调用样本未达统计披露阈值，暂不展示延迟以防失真'
        : 'Insufficient live traffic samples in current window; withheld to avoid misleading metrics'
    case 'partial':
      return isZh
        ? '部分指标已就绪，其余分位仍在采样'
        : 'Partial telemetry available for current observation window'
    case 'withheld':
      return isZh
        ? '因样本稀疏或隐私聚合策略暂不公开'
        : 'Withheld due to aggregation or sample size policy'
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

export type ModalityFilterKey =
  | 'all'
  | 'text'
  | 'image'
  | 'video'
  | 'audio'
  | 'embedding'
  | 'realtime'

export interface ModalityFilterPill {
  key: ModalityFilterKey
  labelZh: string
  labelEn: string
}

export const MODALITY_FILTER_PILLS: readonly ModalityFilterPill[] = [
  { key: 'all', labelZh: '全部', labelEn: 'All' },
  { key: 'text', labelZh: '文本', labelEn: 'Text' },
  { key: 'image', labelZh: '图片生成', labelEn: 'Image Generation' },
  { key: 'video', labelZh: '视频生成', labelEn: 'Video Generation' },
  { key: 'audio', labelZh: '语音音频', labelEn: 'Audio & Speech' },
  { key: 'embedding', labelZh: '向量嵌入', labelEn: 'Embeddings' },
  { key: 'realtime', labelZh: '实时交互', labelEn: 'Realtime' }
] as const

function normalizedTokens(values?: string[] | null): string[] {
  if (!Array.isArray(values)) return []
  return values.map((v) => String(v ?? '').trim().toLowerCase()).filter(Boolean)
}

export function matchesModalityFilter(
  model: { modalities?: string[] | null; actions?: string[] | null },
  key: ModalityFilterKey
): boolean {
  if (key === 'all') return true
  const mods = normalizedTokens(model.modalities)
  const acts = normalizedTokens(model.actions)

  switch (key) {
    case 'text':
      return (
        mods.includes('text') ||
        acts.some((a) => a === 'text.chat' || a === 'text.completion' || a === 'chat')
      )
    case 'image':
      return (
        mods.includes('image') ||
        acts.some((a) => a.startsWith('image') || a.includes('image_generation'))
      )
    case 'video':
      return (
        mods.includes('video') ||
        acts.some((a) => a.startsWith('video') || a.includes('video_generation'))
      )
    case 'audio':
      return (
        mods.includes('audio') ||
        mods.includes('speech') ||
        acts.some(
          (a) =>
            a.startsWith('audio') ||
            a.includes('speech') ||
            a.includes('tts') ||
            a.includes('stt')
        )
      )
    case 'embedding':
      return (
        mods.some((m) => m === 'embedding' || m === 'embeddings') ||
        acts.some((a) => a.includes('embedding'))
      )
    case 'realtime':
      return (
        mods.includes('realtime') ||
        acts.some((a) => a.includes('realtime'))
      )
  }
}

export function extractDisplayModalities(
  model?: { modalities?: string[] | null; actions?: string[] | null } | null
): string[] {
  if (!model) return []
  const seen = new Set<string>()
  for (const mod of normalizedTokens(model.modalities)) {
    if (mod === 'embeddings') seen.add('embedding')
    else seen.add(mod)
  }
  for (const act of normalizedTokens(model.actions)) {
    if (act.startsWith('image') || act.includes('image_generation')) seen.add('image')
    else if (act.startsWith('video') || act.includes('video_generation')) seen.add('video')
    else if (
      act.startsWith('audio') ||
      act.includes('speech') ||
      act.includes('tts') ||
      act.includes('stt')
    ) {
      seen.add('audio')
    } else if (act.includes('embedding')) seen.add('embedding')
    else if (act.includes('realtime')) seen.add('realtime')
    else if (act.startsWith('text.chat') || act === 'chat') seen.add('text')
  }
  return [...seen]
}

export function modalityLabel(modality: string, isZh = false): string {
  const key = modality.trim().toLowerCase()
  switch (key) {
    case 'text':
      return isZh ? '文本' : 'Text'
    case 'image':
      return isZh ? '图片' : 'Image'
    case 'video':
      return isZh ? '视频' : 'Video'
    case 'audio':
    case 'speech':
      return isZh ? '音频' : 'Audio'
    case 'embedding':
    case 'embeddings':
      return isZh ? '向量' : 'Embedding'
    case 'realtime':
      return isZh ? '实时' : 'Realtime'
    case '3d':
      return '3D'
    case 'pdf':
      return 'PDF'
    default:
      return modality
  }
}

export function modalityTone(modality: string): UiTone {
  switch (modality.trim().toLowerCase()) {
    case 'image':
      return 'accent'
    case 'video':
      return 'info'
    case 'audio':
    case 'speech':
      return 'success'
    case 'embedding':
    case 'embeddings':
      return 'warning'
    case 'realtime':
      return 'accent'
    default:
      return 'neutral'
  }
}
