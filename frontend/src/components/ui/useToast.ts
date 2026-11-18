import { reactive, readonly } from 'vue'

export type UiToastTone = 'info' | 'success' | 'warning' | 'danger'

export interface UiToastItem {
  id: string
  title: string
  description?: string
  tone: UiToastTone
  requestId?: string
  durationMs: number
  actionLabel?: string
  onAction?: () => void
}

export interface PushUiToastInput {
  id?: string
  title: string
  description?: string
  tone?: UiToastTone
  requestId?: string
  durationMs?: number
  actionLabel?: string
  onAction?: () => void
}

const MAX_VISIBLE_TOASTS = 4
let toastSequence = 0

const state = reactive<{ items: UiToastItem[] }>({
  items: []
})

const timers = new Map<string, ReturnType<typeof setTimeout>>()

function clearToastTimer(id: string): void {
  const timer = timers.get(id)
  if (timer !== undefined) {
    clearTimeout(timer)
    timers.delete(id)
  }
}

export function dismissUiToast(id: string): void {
  clearToastTimer(id)
  const index = state.items.findIndex((item) => item.id === id)
  if (index !== -1) {
    state.items.splice(index, 1)
  }
}

export function pauseUiToast(id: string): void {
  clearToastTimer(id)
}

export function resumeUiToast(id: string): void {
  const target = state.items.find((item) => item.id === id)
  if (!target || target.durationMs <= 0) return
  clearToastTimer(id)
  timers.set(
    id,
    setTimeout(() => {
      dismissUiToast(id)
    }, target.durationMs)
  )
}

export function pushUiToast(input: PushUiToastInput): string {
  toastSequence += 1
  const id = input.id ?? `oh-toast-${toastSequence}`
  const durationMs = input.durationMs ?? (input.tone === 'danger' ? 7000 : 4500)

  const item: UiToastItem = {
    id,
    title: input.title,
    description: input.description,
    tone: input.tone ?? 'info',
    requestId: input.requestId,
    durationMs,
    actionLabel: input.actionLabel,
    onAction: input.onAction
  }

  const existingIndex = state.items.findIndex((candidate) => candidate.id === id)
  if (existingIndex !== -1) {
    state.items.splice(existingIndex, 1)
  }
  state.items.push(item)

  while (state.items.length > MAX_VISIBLE_TOASTS) {
    const oldest = state.items[0]
    dismissUiToast(oldest.id)
  }

  if (durationMs > 0) {
    resumeUiToast(id)
  }
  return id
}

export function clearAllUiToasts(): void {
  for (const item of [...state.items]) {
    dismissUiToast(item.id)
  }
}

export function useUiToast() {
  return {
    toasts: readonly(state.items),
    push: pushUiToast,
    dismiss: dismissUiToast,
    pause: pauseUiToast,
    resume: resumeUiToast,
    clearAll: clearAllUiToasts
  }
}
