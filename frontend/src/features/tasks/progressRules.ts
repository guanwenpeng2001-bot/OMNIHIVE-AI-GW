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
      description: detail.source,
      status: submittedDone ? 'complete' : 'current'
    },
    {
      id: 'provider',
      title: isZh ? '上游处理' : 'Upstream Execution',
      description: providerState || EM_DASH,
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
      description: artifactState || EM_DASH,
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
      description: detail.settlement_state || EM_DASH,
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
