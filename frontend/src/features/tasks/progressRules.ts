import type { UiStepItem, UiTone } from '@/components/ui/types'
import { EM_DASH, formatExpirationCountdown, type ExpirationCountdown } from '@/features/console/format'
import type { ImageTaskArtifact, ImageTaskDetail, ImageTaskSummary } from './types'

export interface HonestProgressPresentation {
  mode: 'determinate' | 'unreported'
  percent: number | null
  label: string
}

export function resolveHonestTaskProgress(
  task: Pick<ImageTaskSummary, 'progress' | 'state' | 'stage'>
): HonestProgressPresentation {
  if (typeof task.progress === 'number' && Number.isFinite(task.progress)) {
    const clamped = Math.max(0, Math.min(100, Math.trunc(task.progress)))
    return {
      mode: 'determinate',
      percent: clamped,
      label: `${clamped}%`
    }
  }
  return {
    mode: 'unreported',
    percent: null,
    label: EM_DASH
  }
}

export function formatTaskStateLabel(state: string | null | undefined, isZh = false): string {
  if (!state) return EM_DASH
  if (!isZh) return state
  const map: Record<string, string> = {
    queued: '排队中',
    running: '执行中',
    succeeded: '已完成',
    failed: '执行失败',
    canceled: '已取消',
    unknown: '状态未知'
  }
  return map[state] || state
}

export function formatTaskKindLabel(kind: string | null | undefined, isZh = false): string {
  if (!kind) return EM_DASH
  const normalized = kind.toLowerCase()
  if (normalized === 'image') return isZh ? '图像' : 'Image'
  if (normalized === 'audio') return isZh ? '音频' : 'Audio'
  if (normalized === 'video') return isZh ? '视频' : 'Video'
  return kind
}

export function taskKindTone(kind: string | null | undefined): UiTone {
  const normalized = (kind || '').toLowerCase()
  if (normalized === 'audio') return 'info'
  if (normalized === 'video') return 'accent'
  return 'neutral'
}

export function inferArtifactMediaCategory(
  contentType?: string | null,
  taskKind?: string | null
): 'image' | 'audio' | 'video' | 'other' {
  const cleanMime = (contentType || '').split(';')[0].trim().toLowerCase()
  if (cleanMime.startsWith('image/')) return 'image'
  if (cleanMime.startsWith('audio/')) return 'audio'
  if (cleanMime.startsWith('video/')) return 'video'
  const normalizedKind = (taskKind || '').toLowerCase()
  if (normalizedKind === 'image' || normalizedKind === 'audio' || normalizedKind === 'video') {
    return normalizedKind
  }
  return 'other'
}

export function inferArtifactExtension(
  contentType?: string | null,
  taskKind?: string | null
): string {
  const cleanMime = (contentType || '').split(';')[0].trim().toLowerCase()
  const mimeMap: Record<string, string> = {
    'image/png': 'png',
    'image/jpeg': 'jpg',
    'image/jpg': 'jpg',
    'image/webp': 'webp',
    'image/gif': 'gif',
    'audio/mpeg': 'mp3',
    'audio/mp3': 'mp3',
    'audio/wav': 'wav',
    'audio/x-wav': 'wav',
    'audio/wave': 'wav',
    'audio/ogg': 'ogg',
    'audio/opus': 'opus',
    'audio/flac': 'flac',
    'audio/aac': 'aac',
    'audio/mp4': 'm4a',
    'video/mp4': 'mp4',
    'video/webm': 'webm',
    'video/quicktime': 'mov'
  }
  if (cleanMime && mimeMap[cleanMime]) return mimeMap[cleanMime]
  const slashParts = cleanMime.split('/')
  if (slashParts.length === 2 && /^[a-z0-9]{1,8}$/.test(slashParts[1])) {
    return slashParts[1]
  }
  const normalizedKind = (taskKind || '').toLowerCase()
  if (normalizedKind === 'image') return 'png'
  if (normalizedKind === 'audio') return 'mp3'
  if (normalizedKind === 'video') return 'mp4'
  return 'bin'
}

export function formatTaskStageLabel(stage: string | null | undefined, isZh = false): string {
  if (!stage) return EM_DASH
  if (!isZh) return stage
  const map: Record<string, string> = {
    queued: '等待调度',
    admitted: '已准入',
    dispatching: '派发中',
    generating: '生成中',
    running: '执行中',
    archiving: '产物归档中',
    partially_succeeded: '部分完成',
    completed: '已完成',
    succeeded: '已完成',
    failed: '已失败',
    canceled: '已取消',
    unknown: '未知阶段'
  }
  return map[stage] || stage
}

export function formatTaskSettlementLabel(state: string | null | undefined, isZh = false): string {
  if (!state) return EM_DASH
  if (!isZh) return state
  const map: Record<string, string> = {
    estimated: '预估',
    reserved: '已冻结',
    pending: '待结算',
    settled: '已结算',
    needs_review: '待核查',
    adjusted: '已调账',
    refunded: '已退回',
    canceled: '已取消',
    unknown: '未知'
  }
  return map[state] || state
}

export function formatTaskSourceLabel(source: string | null | undefined, isZh = false): string {
  if (!source) return EM_DASH
  const normalized = source.toLowerCase()
  if (normalized.includes('native')) {
    return isZh ? '控制台提交' : 'Console Submit'
  }
  if (normalized.includes('gateway') || normalized.includes('openai')) {
    return isZh ? '网关接口' : 'Gateway API'
  }
  if (normalized.includes('retry')) {
    return isZh ? '付费重试' : 'Paid Retry'
  }
  return source.replace(/_v2\b/gi, '')
}

export function formatProgressSourceLabel(source: string | null | undefined, isZh = false): string {
  const key = (source || 'unreported').toLowerCase()
  if (isZh) {
    const map: Record<string, string> = {
      provider_reported: '上游回报',
      item_rollup: '子项汇总',
      unreported: '未上报'
    }
    return map[key] || source || '未上报'
  }
  return source || 'unreported'
}

export function buildTaskLifecycleSteps(detail: ImageTaskDetail, isZh = false): UiStepItem[] {
  const { state, provider_state: providerState, artifact_state: artifactState } = detail

  const submittedDone = state !== 'queued'
  const providerDone =
    providerState === 'accepted' ||
    providerState === 'completed' ||
    state === 'succeeded' ||
    artifactState === 'available'
  const artifactDone =
    artifactState === 'available' ||
    artifactState === 'expired' ||
    artifactState === 'revoked'

  const isFailed = state === 'failed'
  const isCanceled = state === 'canceled'
  const isUnknown = state === 'unknown'

  return [
    {
      id: 'admitted',
      title: isZh ? '任务准入' : 'Admitted',
      description: formatTaskSourceLabel(detail.source, isZh),
      status: submittedDone ? 'complete' : 'current'
    },
    {
      id: 'provider',
      title: isZh ? '上游处理' : 'Upstream Execution',
      description: isZh
        ? providerState === 'accepted'
          ? '上游已受理'
          : providerState === 'completed'
            ? '上游已完成'
            : providerState === 'rejected'
              ? '上游已拒绝'
              : providerState || EM_DASH
        : providerState || EM_DASH,
      status: isFailed
        ? 'error'
        : isUnknown
          ? 'warning'
          : providerDone
            ? 'complete'
            : submittedDone
              ? 'current'
              : 'upcoming'
    },
    {
      id: 'artifact',
      title: isZh ? '产物归档' : 'Artifact Capture',
      description: isZh
        ? artifactState === 'available'
          ? '可下载'
          : artifactState === 'expired'
            ? '已过期'
            : artifactState === 'revoked'
              ? '已撤销'
              : artifactState || EM_DASH
        : artifactState || EM_DASH,
      status: artifactDone
        ? 'complete'
        : state === 'running' && providerDone
          ? 'current'
          : isFailed || isCanceled
            ? 'upcoming'
            : 'upcoming'
    },
    {
      id: 'settlement',
      title: isZh ? '终态与结算' : 'Terminal & Receipt',
      description: formatTaskSettlementLabel(detail.settlement_state, isZh),
      status:
        state === 'succeeded'
          ? 'complete'
          : isFailed
            ? 'error'
            : isCanceled || isUnknown
              ? 'warning'
              : 'upcoming'
    }
  ]
}

export type ArtifactEffectiveState = 'available' | 'expired' | 'revoked' | 'unavailable'

export interface ArtifactPresentation {
  effectiveState: ArtifactEffectiveState
  canDownload: boolean
  canRevoke: boolean
  tone: UiTone
  badgeTextEn: string
  badgeTextZh: string
  countdown: ExpirationCountdown
}

export function classifyTaskArtifact(
  artifact: ImageTaskArtifact,
  allowedActions: string[],
  nowMs = Date.now()
): ArtifactPresentation {
  const countdown = formatExpirationCountdown(artifact.expires_at, nowMs)
  const rawState = (artifact.state || '').toLowerCase()

  if (rawState === 'revoked') {
    return {
      effectiveState: 'revoked',
      canDownload: false,
      canRevoke: false,
      tone: 'danger',
      badgeTextEn: 'Revoked',
      badgeTextZh: '已撤销',
      countdown
    }
  }

  if (rawState === 'expired' || countdown.state === 'expired') {
    return {
      effectiveState: 'expired',
      canDownload: false,
      canRevoke: false,
      tone: 'warning',
      badgeTextEn: 'Expired',
      badgeTextZh: '已过期',
      countdown
    }
  }

  if (rawState === 'available') {
    return {
      effectiveState: 'available',
      canDownload: allowedActions.includes('tasks.artifacts.read'),
      canRevoke: allowedActions.includes('tasks.artifacts.revoke'),
      tone: countdown.state === 'expiring_soon' ? 'warning' : 'success',
      badgeTextEn: 'Available',
      badgeTextZh: '可下载',
      countdown
    }
  }

  return {
    effectiveState: 'unavailable',
    canDownload: false,
    canRevoke: false,
    tone: 'neutral',
    badgeTextEn: artifact.state || 'Unavailable',
    badgeTextZh: artifact.state || '不可用',
    countdown
  }
}

export function taskStateTone(state: string): UiTone {
  switch (state) {
    case 'succeeded':
      return 'success'
    case 'running':
    case 'queued':
      return 'accent'
    case 'failed':
      return 'danger'
    case 'canceled':
      return 'warning'
    case 'unknown':
      return 'unknown'
    default:
      return 'neutral'
  }
}
