import type { Schema } from './api'

const normalMilestones = ['admitted', 'first_output', 'client_completed', 'settled'] as const

function at(value: string | null | undefined): number | null {
  const result = Date.parse(value ?? '')
  return Number.isFinite(result) ? result : null
}

export function waterfall(detail: Pick<Schema['RequestsDetail'], 'timeline' | 'attempts' | 'abandoned_attempts'>) {
  const phases = [
    ...normalMilestones.slice(0, 3),
    ...detail.timeline.filter(event => event.kind === 'client_ended' || event.kind === 'execution_finished').map(event => event.kind),
    'settled',
  ]
  const origin = at(detail.timeline.find(event => event.kind === 'admitted')?.at)
  const attempts = [...detail.attempts, ...detail.abandoned_attempts]
  const lastTimes = [
    ...detail.timeline.map(event => at(event.at)),
    ...attempts.map(attempt => {
      const start = at(attempt.started_at)
      return start === null ? null : start + (attempt.duration_ms ?? 0)
    }),
  ].filter((x): x is number => x !== null)
  const span = origin === null ? null : Math.max(0, ...lastTimes.map(value => value - origin))

  function offset(value: string | null | undefined): number | null {
    const time = at(value)
    return origin === null || time === null ? null : time - origin
  }
  function percent(ms: number | null) {
    if (ms === null || span === null) return null
    return span === 0 ? 0 : Math.min(100, Math.max(0, 100 * ms / span))
  }
  function attemptRow(attempt: Schema['RequestsAttempt']) {
    const duration = attempt.duration_ms
    return {
      attempt,
      start: percent(offset(attempt.started_at)),
      width: percent(duration),
      replacement: attempt.receipt_state === 'settled' && detail.abandoned_attempts.some(earlier => earlier.number < attempt.number),
    }
  }
  return {
    span,
    phases: phases.map(kind => {
      const event = detail.timeline.find(phase => phase.kind === kind)
      const relative = offset(event?.at)
      return { kind, event, relative, position: percent(relative) }
    }),
    attempts: detail.attempts.map(attemptRow),
    abandoned: detail.abandoned_attempts.map(attemptRow),
  }
}
